package reference_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/reference" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	referencecontract "github.com/tesserix/helivanta/internal/modules/reference/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

var (
	do = testutil.Do
)

func setup(t *testing.T) (*gin.Engine, *tenantdb.DB, context.Context) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"tokA": testutil.TenantA, "tokB": testutil.TenantB, "tokBad": "not-a-uuid"},
		Modules: []platform.Module{reference.New()},
	})
	return r, db, ctx
}

func TestPingFullWiring(t *testing.T) {
	r, db, ctx := setup(t)

	w := do(r, "POST", "/v1/reference/ping", "tokA", `{"message":"hello"}`)
	require.Equal(t, http.StatusAccepted, w.Code)
	var resp struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.ID)

	// Tenant A sees the ping; tenant B does not.
	require.Contains(t, do(r, "GET", "/v1/reference/pings", "tokA", "").Body.String(), "hello")
	require.NotContains(t, do(r, "GET", "/v1/reference/pings", "tokB", "").Body.String(), "hello")

	// Cross-tenant probe by id → 404, not 403 (issue #2).
	require.Equal(t, http.StatusNotFound, do(r, "GET", "/v1/reference/pings/"+resp.ID, "tokB", "").Code)
	require.Equal(t, http.StatusOK, do(r, "GET", "/v1/reference/pings/"+resp.ID, "tokA", "").Code)

	// A random (but valid) uuid that was never created also 404s.
	require.Equal(t, http.StatusNotFound, do(r, "GET", "/v1/reference/pings/"+uuid.NewString(), "tokA", "").Code)

	// Event flows outbox → JetStream → consumer → receipt row, and the
	// receipt records the same ping id that was created above. The
	// receipt is tenant-scoped (Task 6), so it is only visible under
	// tenant A's GUC, not WithSystem.
	require.Eventually(t, func() bool {
		var n int64
		_ = db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM reference_ping_receipts`).Scan(&n).Error
		})
		return n == 1
	}, 20*time.Second, 200*time.Millisecond)

	var pingID string
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT ping_id FROM reference_ping_receipts LIMIT 1`).Scan(&pingID).Error
	}))
	require.Equal(t, resp.ID, pingID)
}

func TestPingValidation(t *testing.T) {
	r, _, _ := setup(t)
	require.Equal(t, http.StatusBadRequest, do(r, "POST", "/v1/reference/ping", "tokA", `{}`).Code)
	require.Equal(t, http.StatusUnauthorized, do(r, "POST", "/v1/reference/ping", "nope", `{"message":"x"}`).Code)
}

func TestPingInvalidTenantClaim(t *testing.T) {
	r, _, _ := setup(t)
	w := do(r, "POST", "/v1/reference/ping", "tokBad", `{"message":"x"}`)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Contains(t, w.Body.String(), "error")
}

// seedPings inserts n pings directly so TestPingsArePaginated controls
// exactly how many rows exist.
func seedPings(t *testing.T, db *tenantdb.DB, ctx context.Context, tenantID uuid.UUID, n int) {
	t.Helper()
	for i := range n {
		msg := fmt.Sprintf("ping-%02d", i)
		require.NoError(t, db.WithTenant(ctx, tenantID.String(), func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO reference_pings (tenant_id, message) VALUES (?, ?)`, tenantID, msg).Error
		}))
	}
}

// TestPingsArePaginated walks a seeded tenant's pings to exhaustion
// through the cursor and asserts every seeded ping appears exactly
// once — the same property proven for medicore's visits.
func TestPingsArePaginated(t *testing.T) {
	r, db, ctx := setup(t)
	tenantID := uuid.MustParse(testutil.TenantA)
	seedPings(t, db, ctx, tenantID, 7)

	seen := map[string]int{}
	cursor := ""
	for i := 0; i < 20; i++ {
		q := "/v1/reference/pings?limit=3"
		if cursor != "" {
			q += "&cursor=" + url.QueryEscape(cursor)
		}
		w := do(r, "GET", q, "tokA", "")
		require.Equal(t, http.StatusOK, w.Code)
		var body struct {
			Data []struct {
				Message string `json:"message"`
			} `json:"data"`
			Page struct {
				NextCursor *string `json:"next_cursor"`
				HasMore    bool    `json:"has_more"`
			} `json:"page"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		for _, rr := range body.Data {
			seen[rr.Message]++
		}
		if !body.Page.HasMore {
			require.Nil(t, body.Page.NextCursor)
			break
		}
		cursor = *body.Page.NextCursor
	}

	require.Len(t, seen, 7)
	for msg, n := range seen {
		require.Equal(t, 1, n, "ping %s appeared %d times across pages", msg, n)
	}
}

// TestPingsRejectAnotherTenantsCursor mirrors medicore's cursor
// tenant-mismatch test. Reference's routes are all authz.Public, so this
// also proves the cursor's tenant check runs on a Public collection
// route, not only on a permission-gated one.
func TestPingsRejectAnotherTenantsCursor(t *testing.T) {
	r, _, _ := setup(t)
	foreign := pagination.Cursor{
		TenantID:  testutil.TenantB,
		CreatedAt: time.Now().UTC(),
		ID:        uuid.New(),
	}.Encode()

	w := do(r, "GET", "/v1/reference/pings?cursor="+url.QueryEscape(foreign), "tokA", "")
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// fixedMembership is an authz.MembershipChecker whose answer is fixed
// per test, independent of subject or tenant — reference's routes are
// all authz.Public (no permission declared), so these tests need
// control over ONLY the membership dimension to prove #781: a Public
// route must consult membership even though it consults no permission.
type fixedMembership struct {
	allow bool
	err   error
}

func (f fixedMembership) IsMember(context.Context, string, string) (bool, error) {
	return f.allow, f.err
}

func harnessWithMembership(t *testing.T, member bool) *gin.Engine {
	t.Helper()
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:     map[string]string{"tok": testutil.TenantA},
		Membership: fixedMembership{allow: member},
		Modules:    []platform.Module{reference.New()},
	})
	return r
}

func harnessWithFailingMembership(t *testing.T, err error) *gin.Engine {
	t.Helper()
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:     map[string]string{"tok": testutil.TenantA},
		Membership: fixedMembership{err: err},
		Modules:    []platform.Module{reference.New()},
	})
	return r
}

// TestPublicRouteRefusesANonMember is the literal regression test for
// the defect in #781: PermissionSet.Has used to short-circuit true for
// authz.Public, so an unguarded route served a caller whose permission
// set was empty because their membership had been revoked — the set was
// never actually examined. The caller here authenticates fine and
// carries a valid tenant_id claim, but holds no role in that tenant:
// the shape of an ex-employee whose membership was revoked while their
// token was still live.
func TestPublicRouteRefusesANonMember(t *testing.T) {
	r := harnessWithMembership(t, false)

	w := testutil.Do(r, http.MethodGet, "/v1/reference/pings", "tok", "")

	require.Equal(t, http.StatusForbidden, w.Code,
		"a Public route declares no permission; it must still refuse a non-member")
}

func TestPublicRouteStillServesAMember(t *testing.T) {
	r := harnessWithMembership(t, true)

	w := testutil.Do(r, http.MethodGet, "/v1/reference/pings", "tok", "")

	require.Equal(t, http.StatusOK, w.Code)
}

func TestMembershipInfrastructureFailureIsFailClosed(t *testing.T) {
	r := harnessWithFailingMembership(t, errors.New("openfga unreachable"))

	w := testutil.Do(r, http.MethodGet, "/v1/reference/pings", "tok", "")

	require.Equal(t, http.StatusServiceUnavailable, w.Code,
		"a membership check that cannot be answered must deny, never admit")
	require.Contains(t, w.Body.String(), "authz_unavailable")
}

// tenantScopedMembership keys its answer on subject+tenant, so it can
// answer differently for the same caller depending which tenant's token
// they present — the whole point of TestMembershipRevokedInOneTenantLeavesTheOtherWorking.
type tenantScopedMembership map[string]bool

func (m tenantScopedMembership) IsMember(_ context.Context, subject, tenantID string) (bool, error) {
	return m[subject+"@"+tenantID], nil
}

// TestMembershipRevokedInOneTenantLeavesTheOtherWorking is spec T3:
// losing membership in one hospital must not end a session in another.
// This is what proves membership stayed a tenant-scoped check (spec D3)
// rather than collapsing into a subject-only decision — see the
// "prove it can fail" note on this test in the Task 3 report: changing
// RequireMembership to ignore the caller's tenant claim and consult only
// the subject makes the second assertion below fail, because the fake's
// lookup key no longer matches.
func TestMembershipRevokedInOneTenantLeavesTheOtherWorking(t *testing.T) {
	// tok-a and tok-b authenticate as two different subjects (StaticVerifier
	// derives "user-"+token), standing in for the same clinician holding
	// two separate credentials — one per hospital they work at. Membership
	// survives in B only.
	member := tenantScopedMembership{
		"user-tok-a@" + testutil.TenantA: false,
		"user-tok-b@" + testutil.TenantB: true,
	}
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:     map[string]string{"tok-a": testutil.TenantA, "tok-b": testutil.TenantB},
		Membership: member,
		Modules:    []platform.Module{reference.New()},
	})

	require.Equal(t, http.StatusForbidden,
		testutil.Do(r, http.MethodGet, "/v1/reference/pings", "tok-a", "").Code,
		"membership was revoked in tenant A")

	require.Equal(t, http.StatusOK,
		testutil.Do(r, http.MethodGet, "/v1/reference/pings", "tok-b", "").Code,
		"the same person is still employed at tenant B; revoking A must not touch B")
}

// TestForwardedPingLandsInDestinationTenant is the end-to-end proof of
// #932 through the real path: a business tx publishes to the outbox, the
// dispatcher delivers via JetStream, and the consumer writes an
// RLS-forced row into a DIFFERENT tenant, invisible to the publisher.
func TestForwardedPingLandsInDestinationTenant(t *testing.T) {
	_, db, bus, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"tokA": testutil.TenantA, "tokB": testutil.TenantB},
		Modules: []platform.Module{reference.New()},
	})

	// No wiring here: NewHarness already passed reference's
	// DirectedSubjects() into the Bus constructor, started its
	// consumers and started the dispatcher — the same three steps
	// cmd/api/main.go performs. Repeating them bound a SECOND pull
	// subscriber to the durable name "reference-forwarded", so the two
	// competed for every message, and started a second dispatcher
	// goroutine (#926 territory).

	origin, destination := uuid.NewString(), uuid.NewString()
	pingID := uuid.New()

	data, err := json.Marshal(referencecontract.PingForwardedData{
		PingID: pingID.String(), Message: "forwarded",
	})
	require.NoError(t, err)

	require.NoError(t, db.WithTenant(ctx, origin, func(tx *gorm.DB) error {
		return bus.Publish(tx, referencecontract.SubjectPingForwarded, events.Event{
			Type: "ReferencePingForwarded", Version: 1,
			TenantID: origin, DestinationTenantID: destination, Data: data,
		})
	}))

	countAs := func(tenant string) int {
		var n int
		require.NoError(t, db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM reference_forwarded_pings`).Scan(&n).Error
		}))
		return n
	}

	require.Eventually(t, func() bool { return countAs(destination) == 1 },
		30*time.Second, 200*time.Millisecond)
	require.Equal(t, 0, countAs(origin),
		"the publishing tenant can read the row it forwarded")

	var originTenant, originRecord string
	require.NoError(t, db.WithTenant(ctx, destination, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT origin_tenant_id::text, origin_record_id::text
		               FROM reference_forwarded_pings`).Row().Scan(&originTenant, &originRecord)
	}))
	require.Equal(t, origin, originTenant)
	require.Equal(t, pingID.String(), originRecord)
}

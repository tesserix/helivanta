package medicore_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/medicore" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

var (
	do = testutil.Do
)

func setup(t *testing.T) (*gin.Engine, *tenantdb.DB, context.Context) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"tokA": testutil.TenantA, "tokB": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"tokA": {medicore.PermVisitCreate, medicore.PermVisitRead},
			"tokB": {medicore.PermVisitRead},
		},
		Modules: []platform.Module{medicore.New()},
	})
	return r, db, ctx
}

func TestCreateAndListVisits(t *testing.T) {
	r, db, ctx := setup(t)

	w := do(r, "POST", "/v1/medicore/visits", "tokA", `{"patient_name":"Asha Rao","department":"OPD"}`)
	require.Equal(t, http.StatusAccepted, w.Code)
	var resp struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.ID)

	// Tenant isolation on list.
	require.Contains(t, do(r, "GET", "/v1/medicore/visits", "tokA", "").Body.String(), "Asha Rao")
	require.NotContains(t, do(r, "GET", "/v1/medicore/visits", "tokB", "").Body.String(), "Asha Rao")

	// visit_created reached the outbox and got published (peek the
	// outbox row directly — no consumer in this module). Read under
	// WithTenant(TenantA), not WithSystem: since #835 Task 1,
	// outbox_events carries the publishing tenant and is RLS-forced, so
	// a WithSystem read (no tenant GUC) now sees nothing here — that is
	// the point of the change, not a regression. Reading through
	// WithTenant also proves the row was actually tagged with this
	// tenant's id, not just that some row exists.
	require.Eventually(t, func() bool {
		var n int64
		_ = db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM outbox_events
				WHERE subject = 'helivanta.in.medicore.visit_created.v1' AND published_at IS NOT NULL`).Scan(&n).Error
		})
		return n == 1
	}, 20*time.Second, 200*time.Millisecond)
}

func TestVisitValidation(t *testing.T) {
	r, _, _ := setup(t)
	require.Equal(t, http.StatusBadRequest, do(r, "POST", "/v1/medicore/visits", "tokA", `{}`).Code)
	require.Equal(t, http.StatusBadRequest,
		do(r, "POST", "/v1/medicore/visits", "tokA", `{"patient_name":"X","department":"ICU"}`).Code)
	require.Equal(t, http.StatusUnauthorized,
		do(r, "POST", "/v1/medicore/visits", "nope", `{"patient_name":"X","department":"OPD"}`).Code)
}

// seedVisits inserts n visits directly (bypassing the HTTP create route,
// which is not under test here) so TestVisitsArePaginated controls
// exactly how many rows exist and can name each one distinctly.
func seedVisits(t *testing.T, db *tenantdb.DB, ctx context.Context, tenantID uuid.UUID, n int) []string {
	t.Helper()
	names := make([]string, n)
	for i := range n {
		name := fmt.Sprintf("visit-%02d", i)
		names[i] = name
		require.NoError(t, db.WithTenant(ctx, tenantID.String(), func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO medicore_visits (tenant_id, patient_name, department) VALUES (?, ?, 'OPD')`,
				tenantID, name).Error
		}))
	}
	return names
}

// TestVisitsArePaginated walks a seeded tenant's visits to exhaustion
// through the cursor and asserts every seeded visit appears exactly
// once — the exact-once property the keyset helper (pkg/tenantdb) and
// ListRoute's trim (internal/platform) jointly guarantee, now proven
// through the real HTTP route rather than against a fake handler.
func TestVisitsArePaginated(t *testing.T) {
	r, db, ctx := setup(t)
	tenantID := uuid.MustParse(testutil.TenantA)
	seedVisits(t, db, ctx, tenantID, 7)

	w := do(r, "GET", "/v1/medicore/visits?limit=3", "tokA", "")
	require.Equal(t, http.StatusOK, w.Code)

	var page1 struct {
		Data []struct {
			ID          string `json:"id"`
			PatientName string `json:"patient_name"`
		} `json:"data"`
		Page struct {
			NextCursor *string `json:"next_cursor"`
			HasMore    bool    `json:"has_more"`
		} `json:"page"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page1))
	require.Len(t, page1.Data, 3)
	require.True(t, page1.Page.HasMore)
	require.NotNil(t, page1.Page.NextCursor)

	seen := map[string]int{}
	for _, v := range page1.Data {
		seen[v.PatientName]++
	}
	cursor := *page1.Page.NextCursor
	for range 10 {
		w := do(r, "GET", "/v1/medicore/visits?limit=3&cursor="+url.QueryEscape(cursor), "tokA", "")
		require.Equal(t, http.StatusOK, w.Code)
		var next struct {
			Data []struct {
				PatientName string `json:"patient_name"`
			} `json:"data"`
			Page struct {
				NextCursor *string `json:"next_cursor"`
				HasMore    bool    `json:"has_more"`
			} `json:"page"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &next))
		for _, v := range next.Data {
			seen[v.PatientName]++
		}
		if !next.Page.HasMore {
			require.Nil(t, next.Page.NextCursor)
			break
		}
		cursor = *next.Page.NextCursor
	}

	require.Len(t, seen, 7)
	for name, n := range seen {
		require.Equal(t, 1, n, "visit %s appeared %d times across pages", name, n)
	}
}

// TestVisitsRejectAnotherTenantsCursor proves the route refuses a cursor
// minted for a different tenant rather than silently using it as a
// position — pagination.Decode's tenant check exercised through the real
// route, not just the unit test in pkg/pagination.
func TestVisitsRejectAnotherTenantsCursor(t *testing.T) {
	r, _, _ := setup(t)
	foreign := pagination.Cursor{
		TenantID:  testutil.TenantB,
		CreatedAt: time.Now().UTC(),
		ID:        uuid.New(),
	}.Encode()

	w := do(r, "GET", "/v1/medicore/visits?cursor="+url.QueryEscape(foreign), "tokA", "")

	require.Equal(t, http.StatusBadRequest, w.Code,
		"a cursor issued for another tenant must be refused, not applied as a position")
}

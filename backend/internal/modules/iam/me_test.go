package iam_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/modules/iam" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/internal/testutil"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/session"
)

// fakeRoleLister fakes authz.Client's ListRoles from a static
// subject→bindings map, mirroring how recordingWriter fakes TupleWriter
// in routes_test.go. Tests that exercise /me/tenants and /me/tenant
// need no OpenFGA container: membership there is resolved entirely
// through this interface, never through iam_members directly (see the
// doc comment on registerMe in me.go for why).
type fakeRoleLister struct {
	bindings map[string][]authz.RoleBinding
	err      error
}

func (f *fakeRoleLister) ListRoles(_ context.Context, subject string) ([]authz.RoleBinding, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.bindings[subject], nil
}

func TestMePermissionsReturnsResolvedSetSorted(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"doc": testutil.TenantA},
		Perms:  map[string][]authz.Permission{"doc": {"medicore.visit.read", "lab.order.read"}},
		Writer: &recordingWriter{}, Modules: []platform.Module{iam.New(nil)},
	})

	res := testutil.Do(r, "GET", "/v1/iam/me/permissions", "doc", "")
	require.Equal(t, http.StatusOK, res.Code)

	var body struct {
		Data []string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
	require.Equal(t, []string{"lab.order.read", "medicore.visit.read"}, body.Data)
}

func TestMePermissionsIsEmptyArrayNotNullForNonMember(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"nobody": testutil.TenantA},
		Perms:  map[string][]authz.Permission{"nobody": {}},
		Writer: &recordingWriter{}, Modules: []platform.Module{iam.New(nil)},
	})

	res := testutil.Do(r, "GET", "/v1/iam/me/permissions", "nobody", "")
	require.Equal(t, http.StatusOK, res.Code)
	// subject/tenant_id now travel alongside data (see
	// TestMePermissionsReturnsCallerIdentity), so this asserts on the
	// data field's shape directly rather than the whole body.
	require.JSONEq(t, `{"data":[],"subject":"user-nobody","tenant_id":"`+testutil.TenantA+`"}`, res.Body.String())
}

// The client-side permissions cache (packages/api/src/permissions-cache.ts)
// stamps its entry with who the permissions belong to, so the entry is
// self-describing for debugging and for the cache's own shape
// validation. The client cannot read that identity from the httpOnly
// session cookie, so this endpoint is where it comes from. It is not the
// mechanism that stops one user's set being painted for another — that
// is the clear-on-login/logout/tenant-switch plus the fresh response
// overwriting the entry.
func TestMePermissionsReturnsCallerIdentity(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"doc": testutil.TenantA},
		Perms:  map[string][]authz.Permission{"doc": {"medicore.visit.read"}},
		Writer: &recordingWriter{}, Modules: []platform.Module{iam.New(nil)},
	})

	res := testutil.Do(r, "GET", "/v1/iam/me/permissions", "doc", "")
	require.Equal(t, http.StatusOK, res.Code)

	var body struct {
		Data     []string `json:"data"`
		Subject  string   `json:"subject"`
		TenantID string   `json:"tenant_id"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
	require.Equal(t, []string{"medicore.visit.read"}, body.Data)
	// StaticVerifier derives the subject as "user-" + token (see
	// testutil.StaticVerifier.Verify), matching every other subject
	// assertion in this file (e.g. TestMeTenantsListsEveryMembership's
	// "user-jane").
	require.Equal(t, "user-doc", body.Subject)
	require.Equal(t, testutil.TenantA, body.TenantID)
}

func TestMeTenantsListsEveryMembership(t *testing.T) {
	roles := &fakeRoleLister{bindings: map[string][]authz.RoleBinding{
		// user-jane is the subject StaticVerifier derives from token "jane".
		"user-jane": {
			{TenantID: testutil.TenantA, Role: authz.RoleDoctor},
			{TenantID: testutil.TenantB, Role: authz.RoleNurse},
		},
	}}
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"jane": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"jane": {}},
		Writer:  &recordingWriter{},
		Roles:   roles,
		Modules: []platform.Module{iam.New(nil)},
	})

	res := testutil.Do(r, "GET", "/v1/iam/me/tenants", "jane", "")
	require.Equal(t, http.StatusOK, res.Code)

	var body struct {
		Data []struct {
			TenantID string   `json:"tenant_id"`
			Roles    []string `json:"roles"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
	require.Len(t, body.Data, 2, "a clinician working at two hospitals must see both")
}

// TestMeTenantsMarksCallersActualTenantCurrent guards the fix for the
// one-way-door bug: the shell picker used to guess the "current" tenant
// from array order (sorted by tenant id), which drifted from reality
// after any switch. The response must instead mark the entry matching
// the caller's tenant_id token claim, regardless of where it falls in
// the sorted list.
func TestMeTenantsMarksCallersActualTenantCurrent(t *testing.T) {
	roles := &fakeRoleLister{bindings: map[string][]authz.RoleBinding{
		// user-jane is the subject StaticVerifier derives from token "jane".
		"user-jane": {
			{TenantID: testutil.TenantA, Role: authz.RoleDoctor},
			{TenantID: testutil.TenantB, Role: authz.RoleNurse},
		},
	}}
	// jane's token claims TenantB even though TenantA sorts first — this
	// is what the switch flow produces: the caller has already switched
	// into TenantB, and the picker must reflect that, not TenantA.
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"jane": testutil.TenantB},
		Perms:   map[string][]authz.Permission{"jane": {}},
		Writer:  &recordingWriter{},
		Roles:   roles,
		Modules: []platform.Module{iam.New(nil)},
	})

	res := testutil.Do(r, "GET", "/v1/iam/me/tenants", "jane", "")
	require.Equal(t, http.StatusOK, res.Code)

	var body struct {
		Data []struct {
			TenantID string   `json:"tenant_id"`
			Roles    []string `json:"roles"`
			Current  bool     `json:"current"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
	require.Len(t, body.Data, 2)

	byID := map[string]bool{}
	for _, m := range body.Data {
		byID[m.TenantID] = m.Current
	}
	require.True(t, byID[testutil.TenantB], "the tenant in the caller's token claim must be marked current")
	require.False(t, byID[testutil.TenantA], "exactly one membership must be marked current")
}

func TestMeTenantsIsEmptyArrayNotNullForNonMember(t *testing.T) {
	roles := &fakeRoleLister{bindings: map[string][]authz.RoleBinding{}}
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"nobody": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"nobody": {}},
		Writer:  &recordingWriter{},
		Roles:   roles,
		Modules: []platform.Module{iam.New(nil)},
	})

	res := testutil.Do(r, "GET", "/v1/iam/me/tenants", "nobody", "")
	require.Equal(t, http.StatusOK, res.Code)
	require.JSONEq(t, `{"data":[]}`, res.Body.String())
}

func TestMeTenantsFailsClosedOnRoleListerError(t *testing.T) {
	roles := &fakeRoleLister{err: errors.New("openfga unreachable")}
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"jane": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"jane": {}},
		Writer:  &recordingWriter{},
		Roles:   roles,
		Modules: []platform.Module{iam.New(nil)},
	})

	res := testutil.Do(r, "GET", "/v1/iam/me/tenants", "jane", "")
	require.Equal(t, http.StatusServiceUnavailable, res.Code,
		"a ListRoles error must fail closed, never read as \"no memberships\"")
}

// switchHarness wires a real session.Signer/Verifier pair into
// testutil.NewHarness (SessionSigner/SessionTTL), exactly mirroring what
// cmd/api/main.go wires in production (deps.SessionSigner) minus the DB
// key material. Using a REAL signer means the tests below observe an
// actual minted session, not a fake token string — the point of
// TestSwitchTenantCarriesTheOriginalAuthTimeThrough is that the cookie
// decodes back to the caller's subject, the TARGET tenant and the
// ORIGINAL auth_time, which a hand-rolled fake could get wrong in
// exactly the way this endpoint's whole security property depends on.
func switchHarness(t *testing.T, opts testutil.HarnessOptions) (*gin.Engine, *session.Verifier) {
	t.Helper()
	signer, verifier := testutil.NewSessionSignerForTest(t)
	opts.SessionSigner = signer
	opts.SessionTTL = testutil.TestSessionTTL
	opts.SessionSecureCookie = true
	r, _, _, _ := testutil.NewHarness(t, opts)
	return r, verifier
}

// fixedAuthTimeVerifier is testutil.StaticVerifier's token->tenant lookup
// with an operator-chosen AuthTime instead of always stamping time.Now()
// at Verify time. TestSwitchTenantCarriesTheOriginalAuthTimeThrough needs
// this: with the default StaticVerifier, the ORIGINAL session's auth_time
// and a BUGGY reset-to-mint-time auth_time are both stamped "now" only
// moments apart, so asserting the re-minted claim is merely "close to
// now" cannot tell carried-through from reset apart. A deliberately
// distant, fixed AuthTime makes the two cases unmistakable.
type fixedAuthTimeVerifier struct {
	tokens   map[string]string
	authTime time.Time
}

func (f fixedAuthTimeVerifier) Verify(_ context.Context, raw string) (authn.Principal, error) {
	tenant, ok := f.tokens[raw]
	if !ok {
		return authn.Principal{}, errors.New("unknown token")
	}
	return authn.Principal{Subject: "user-" + raw, TenantID: tenant, AuthTime: f.authTime}, nil
}

// TestSwitchTenantRequiresMembership also pins gate-before-mint on the
// denial path: a non-member must not merely be told no, no re-minted
// session for the target tenant may be issued at all — no Set-Cookie on
// the response, and the caller's existing session is left untouched.
func TestSwitchTenantRequiresMembership(t *testing.T) {
	roles := &fakeRoleLister{bindings: map[string][]authz.RoleBinding{
		"user-jane": {{TenantID: testutil.TenantA, Role: authz.RoleNurse}},
	}}
	r, _ := switchHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"jane": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"jane": {}},
		Writer:  &recordingWriter{},
		Roles:   roles,
		Modules: []platform.Module{iam.New(nil)},
	})

	res := testutil.Do(r, "POST", "/v1/iam/me/tenant", "jane",
		`{"tenant_id":"`+testutil.TenantB+`"}`)
	// 404, the cross-tenant answer (docs/standards/backend.md; #838
	// reconciles this with login's identical refusal — see login.go's
	// noAccessibleTenantMessage doc comment): a 403 here would confirm
	// tenant B exists, which is exactly what withholding it should not
	// do.
	require.Equal(t, http.StatusNotFound, res.Code,
		"switching into a tenant you are not a member of must be denied, and answered as the cross-tenant case")
	require.Empty(t, sessionCookies(res),
		"no session may be re-minted for a tenant the caller is not a member of")
}

// TestSwitchTenantCarriesTheOriginalAuthTimeThrough is the whole point of
// the endpoint: the response must set a new HMS session cookie that
// decodes to the caller's own subject, the TARGET tenant, and the
// auth_time carried through UNCHANGED from the caller's original session
// — not a fresh time.Now(). Without the last of these, a switch would
// launder an old authentication into a new one and quietly defeat the
// #781 revocation watermark (spec D2/D3). Without any of the first two,
// the switch is a no-op that reports success while changing nothing.
//
// originalAuthTime is deliberately six years in the past, and asserted by
// EXACT Unix-seconds equality, not "close to now": see
// fixedAuthTimeVerifier's doc comment for why a "close to now" bound
// cannot tell "carried through" apart from "reset to the mint time" — an
// earlier version of this test used exactly that weaker bound and passed
// even when the handler was mutated to call h.signer.Mint(..., time.Now())
// instead of p.AuthTime (verified by hand while writing this test; see the
// task report's mutation-testing section for the reproduction).
func TestSwitchTenantCarriesTheOriginalAuthTimeThrough(t *testing.T) {
	originalAuthTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	roles := &fakeRoleLister{bindings: map[string][]authz.RoleBinding{
		"user-jane": {
			{TenantID: testutil.TenantA, Role: authz.RoleDoctor},
			{TenantID: testutil.TenantB, Role: authz.RoleNurse},
		},
	}}
	r, verifier := switchHarness(t, testutil.HarnessOptions{
		Verifier: fixedAuthTimeVerifier{
			tokens:   map[string]string{"jane": testutil.TenantA},
			authTime: originalAuthTime,
		},
		Perms:   map[string][]authz.Permission{"jane": {}},
		Writer:  &recordingWriter{},
		Roles:   roles,
		Modules: []platform.Module{iam.New(nil)},
	})

	res := testutil.Do(r, "POST", "/v1/iam/me/tenant", "jane",
		`{"tenant_id":"`+testutil.TenantB+`"}`)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())

	var body struct {
		TenantID string `json:"tenant_id"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
	require.Equal(t, testutil.TenantB, body.TenantID)

	claims, err := verifier.Verify(sessionCookie(t, res))
	require.NoError(t, err)
	require.Equal(t, "user-jane", claims.Subject,
		"the re-minted session must be for the caller, never for another subject")
	require.Equal(t, testutil.TenantB, claims.TenantID,
		"the re-minted session must carry the target tenant, which is what makes the switch real")
	require.Equal(t, originalAuthTime.Unix(), claims.AuthTime.Unix(),
		"auth_time must be carried through from the caller's existing session, not reset to the mint time")
}

// TestSwitchTenantFailsClosedWithoutASigner guards the deployment
// mistake: Deps.SessionSigner left nil (a test harness, a half-wired
// entrypoint). The route must refuse rather than fall back to echoing
// the tenant id, which is what made the original no-op look like a
// success.
func TestSwitchTenantFailsClosedWithoutASigner(t *testing.T) {
	roles := &fakeRoleLister{bindings: map[string][]authz.RoleBinding{
		"user-jane": {{TenantID: testutil.TenantB, Role: authz.RoleNurse}},
	}}
	// SessionSigner is deliberately left unset — its zero value (nil) is
	// what this test exercises: deps.SessionSigner must reach the handler
	// as nil, not a working signer. Built directly through
	// testutil.NewHarness, NOT switchHarness, which always wires a
	// working signer.
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"jane": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"jane": {}},
		Writer:  &recordingWriter{},
		Roles:   roles,
		Modules: []platform.Module{iam.New(nil)},
	})

	res := testutil.Do(r, "POST", "/v1/iam/me/tenant", "jane",
		`{"tenant_id":"`+testutil.TenantB+`"}`)
	require.Equal(t, http.StatusServiceUnavailable, res.Code)
	require.Empty(t, sessionCookies(res))
}

// TestSwitchTenantFailsClosedOnRoleListerError guards the switch
// endpoint's actual security gate: a ListRoles error must never be
// read as "no memberships", which is exactly what
// hasBindingForTenant(nil, ...) == false would produce if a future
// refactor let the error fall through to the normal not-a-member path.
// That would silently convert "authorization is unavailable" into
// "you are definitively not a member" — a 503 masquerading as a
// deliberate, permanent denial rather than a transient outage — so
// this must be 503, not 404, and not 200.
func TestSwitchTenantFailsClosedOnRoleListerError(t *testing.T) {
	roles := &fakeRoleLister{err: errors.New("openfga unreachable")}
	r, _ := switchHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"jane": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"jane": {}},
		Writer:  &recordingWriter{},
		Roles:   roles,
		Modules: []platform.Module{iam.New(nil)},
	})

	// The requested tenant is one jane genuinely belongs to in the real
	// world (this fake just can't say so, because ListRoles errors
	// before membership is ever consulted) — the point is that the
	// outage, not a real absence of membership, must drive the
	// response.
	res := testutil.Do(r, "POST", "/v1/iam/me/tenant", "jane",
		`{"tenant_id":"`+testutil.TenantA+`"}`)
	require.Equal(t, http.StatusServiceUnavailable, res.Code,
		"a ListRoles error must fail closed as 503, never as 404 or 200")

	var body struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
	require.Equal(t, "authz_unavailable", body.Error)
	require.Empty(t, sessionCookies(res),
		"minting must sit behind the gate: an unresolved membership check must not produce a session")
}

// TestSwitchTenantRejectsNonCanonicalTenantID guards the actual
// enforcement point for tenant-id casing on the switch endpoint. The
// handler compares req.TenantID against binding tenant ids raw, with no
// normalization — that's only safe because switchRequest.TenantID
// carries `binding:"required,uuid"`, and go-playground/validator's uuid
// rule matches go-playground/validator's uuidRegexString, which is
// lowercase-only ([0-9a-f], not [0-9a-fA-F]). An upper- or mixed-cased
// UUID is therefore rejected by Gin's binding validation with 400
// before it ever reaches hasBindingForTenant, regardless of whether a
// binding for that tenant (in any casing) exists. This test proves that
// rejection actually happens, not just that it's theoretically implied
// by the tag.
func TestSwitchTenantRejectsNonCanonicalTenantID(t *testing.T) {
	// testutil.TenantA/TenantB are all-digit UUIDs, so ToUpper on them is
	// a no-op — this test needs a UUID with actual hex letters (a-f) to
	// exercise the casing rule, so it uses one that is not a member's
	// tenant at all: the point is that binding validation rejects the
	// request before membership (or its absence) is ever consulted.
	const upperCasedUUID = "AAAAAAAA-BBBB-4CCC-8DDD-EEEEEEEEEEEE"

	roles := &fakeRoleLister{bindings: map[string][]authz.RoleBinding{
		"user-jane": {{TenantID: testutil.TenantB, Role: authz.RoleNurse}},
	}}
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"jane": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"jane": {}},
		Writer:  &recordingWriter{},
		Roles:   roles,
		Modules: []platform.Module{iam.New(nil)},
	})

	res := testutil.Do(r, "POST", "/v1/iam/me/tenant", "jane",
		`{"tenant_id":"`+upperCasedUUID+`"}`)
	require.Equal(t, http.StatusBadRequest, res.Code,
		"a non-canonical (upper/mixed-cased) tenant id must be rejected by binding validation, "+
			"before membership is ever consulted")
}

// denyAllMembership refuses every subject in every tenant — the shape of
// a caller whose membership in the tenant their token names has just
// been revoked.
type denyAllMembership struct{}

func (denyAllMembership) IsMember(context.Context, string, string) (bool, error) { return false, nil }

// TestSelfServiceRoutesServeACallerWithNoMembership is spec T4: a member
// with zero memberships (or, as here, one revoked out from under them)
// must still be able to call the self-service routes that answer "what
// can I do" and "where do I belong" — these are exactly the routes
// authz.NoTenantMembership exists for (#781). If they too required
// membership, a revoked caller would be locked out of the one endpoint
// that could tell them where they still belong.
func TestSelfServiceRoutesServeACallerWithNoMembership(t *testing.T) {
	roles := &fakeRoleLister{bindings: map[string][]authz.RoleBinding{
		"user-nurse": {{TenantID: testutil.TenantA, Role: authz.RoleNurse}},
	}}
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:     map[string]string{"nurse": testutil.TenantA},
		Perms:      map[string][]authz.Permission{"nurse": {}},
		Writer:     &recordingWriter{},
		Roles:      roles,
		Membership: denyAllMembership{},
		Modules:    []platform.Module{iam.New(nil)},
	})

	for _, path := range []string{"/v1/iam/me/permissions", "/v1/iam/me/tenants"} {
		w := testutil.Do(r, http.MethodGet, path, "nurse", "")
		require.Equal(t, http.StatusOK, w.Code,
			"%s must answer a caller with no membership — it is the endpoint that tells them where they do belong", path)
	}
}

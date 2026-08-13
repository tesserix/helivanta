package iam_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/modules/iam" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/internal/testutil"
	"github.com/tesserix/hms/pkg/authz"
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

// recordingMinter records every mint call so a test can assert not just
// what came back but whether minting happened at all — the point of the
// gate-before-mint tests is that on a denied or unresolved switch no
// credential for the target tenant is ever created, not merely that the
// response withheld one.
type recordingMinter struct {
	calls  []mintCall
	err    error
	tokens int
}

type mintCall struct {
	uid    string
	claims map[string]interface{}
}

func (m *recordingMinter) CustomTokenWithClaims(_ context.Context, uid string, claims map[string]interface{}) (string, error) {
	m.calls = append(m.calls, mintCall{uid: uid, claims: claims})
	if m.err != nil {
		return "", m.err
	}
	m.tokens++
	return "minted-token-" + uid, nil
}

func TestMePermissionsReturnsResolvedSetSorted(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"doc": testutil.TenantA},
		Perms:  map[string][]authz.Permission{"doc": {"medicore.visit.read", "lab.order.read"}},
		Writer: &recordingWriter{}, Modules: []platform.Module{iam.New()},
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
		Writer: &recordingWriter{}, Modules: []platform.Module{iam.New()},
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
		Writer: &recordingWriter{}, Modules: []platform.Module{iam.New()},
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
		Modules: []platform.Module{iam.New()},
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
		Modules: []platform.Module{iam.New()},
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
		Modules: []platform.Module{iam.New()},
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
		Modules: []platform.Module{iam.New()},
	})

	res := testutil.Do(r, "GET", "/v1/iam/me/tenants", "jane", "")
	require.Equal(t, http.StatusServiceUnavailable, res.Code,
		"a ListRoles error must fail closed, never read as \"no memberships\"")
}

// TestSwitchTenantRequiresMembership also pins gate-before-mint on the
// denial path: a non-member must not merely be told no, no custom token
// for the target tenant may be created at all. A minted token is a
// bearer credential — once it exists, the 403 is advisory.
func TestSwitchTenantRequiresMembership(t *testing.T) {
	roles := &fakeRoleLister{bindings: map[string][]authz.RoleBinding{
		"user-jane": {{TenantID: testutil.TenantA, Role: authz.RoleNurse}},
	}}
	minter := &recordingMinter{}
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"jane": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"jane": {}},
		Writer:  &recordingWriter{},
		Roles:   roles,
		Minter:  minter,
		Modules: []platform.Module{iam.New()},
	})

	res := testutil.Do(r, "POST", "/v1/iam/me/tenant", "jane",
		`{"tenant_id":"`+testutil.TenantB+`"}`)
	require.Equal(t, http.StatusForbidden, res.Code,
		"switching into a tenant you are not a member of must be denied")
	require.Empty(t, minter.calls,
		"no token may be minted for a tenant the caller is not a member of")
	require.NotContains(t, res.Body.String(), "custom_token")
}

// TestSwitchTenantMintsTokenForTargetTenant is the whole point of the
// endpoint: the response must carry a credential that actually moves the
// caller, minted for the caller's own subject and carrying the target
// tenant as its tenant_id claim. Without it the switch is a no-op that
// reports success — the shell shows a toast and the user stays put.
func TestSwitchTenantMintsTokenForTargetTenant(t *testing.T) {
	roles := &fakeRoleLister{bindings: map[string][]authz.RoleBinding{
		"user-jane": {
			{TenantID: testutil.TenantA, Role: authz.RoleDoctor},
			{TenantID: testutil.TenantB, Role: authz.RoleNurse},
		},
	}}
	minter := &recordingMinter{}
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"jane": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"jane": {}},
		Writer:  &recordingWriter{},
		Roles:   roles,
		Minter:  minter,
		Modules: []platform.Module{iam.New()},
	})

	res := testutil.Do(r, "POST", "/v1/iam/me/tenant", "jane",
		`{"tenant_id":"`+testutil.TenantB+`"}`)
	require.Equal(t, http.StatusOK, res.Code)

	require.Len(t, minter.calls, 1)
	require.Equal(t, "user-jane", minter.calls[0].uid,
		"the token must be minted for the caller, never for another subject")
	require.Equal(t, testutil.TenantB, minter.calls[0].claims["tenant_id"],
		"the token must carry the target tenant, which is what makes the switch real")

	var body struct {
		TenantID    string `json:"tenant_id"`
		CustomToken string `json:"custom_token"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
	require.Equal(t, testutil.TenantB, body.TenantID)
	require.Equal(t, "minted-token-user-jane", body.CustomToken,
		"the minted token must reach the client — it is the only thing that changes the session")
}

// TestSwitchTenantFailsClosedWhenMintingFails covers the other half of
// fail-closed: the membership decision succeeded, but the identity
// provider could not issue the credential. Reporting 200 with no usable
// token would reproduce the original bug (a switch that claims success
// and changes nothing), so this must be an error the client can see.
func TestSwitchTenantFailsClosedWhenMintingFails(t *testing.T) {
	roles := &fakeRoleLister{bindings: map[string][]authz.RoleBinding{
		"user-jane": {{TenantID: testutil.TenantB, Role: authz.RoleNurse}},
	}}
	minter := &recordingMinter{err: errors.New("gip unreachable")}
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"jane": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"jane": {}},
		Writer:  &recordingWriter{},
		Roles:   roles,
		Minter:  minter,
		Modules: []platform.Module{iam.New()},
	})

	res := testutil.Do(r, "POST", "/v1/iam/me/tenant", "jane",
		`{"tenant_id":"`+testutil.TenantB+`"}`)
	require.Equal(t, http.StatusServiceUnavailable, res.Code)
	require.Zero(t, minter.tokens)

	var body struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
	require.Equal(t, "session_unavailable", body.Error,
		"a minting outage is not an authorization outage — the codes must stay distinguishable")
	require.NotContains(t, res.Body.String(), "custom_token")
}

// TestSwitchTenantFailsClosedWithoutAMinter guards the deployment
// mistake: Deps.Tokens left nil (a test harness, a half-wired
// entrypoint). The route must refuse rather than fall back to echoing
// the tenant id, which is what made the original no-op look like a
// success.
func TestSwitchTenantFailsClosedWithoutAMinter(t *testing.T) {
	roles := &fakeRoleLister{bindings: map[string][]authz.RoleBinding{
		"user-jane": {{TenantID: testutil.TenantB, Role: authz.RoleNurse}},
	}}
	// Minter is deliberately left unset — its zero value (nil) is what
	// this test exercises: deps.Tokens must reach the handler as nil, not
	// a working stub. See HarnessOptions.Minter's doc comment.
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"jane": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"jane": {}},
		Writer:  &recordingWriter{},
		Roles:   roles,
		Modules: []platform.Module{iam.New()},
	})

	res := testutil.Do(r, "POST", "/v1/iam/me/tenant", "jane",
		`{"tenant_id":"`+testutil.TenantB+`"}`)
	require.Equal(t, http.StatusServiceUnavailable, res.Code)
	require.NotContains(t, res.Body.String(), "custom_token")
}

// TestSwitchTenantFailsClosedOnRoleListerError guards the switch
// endpoint's actual security gate: a ListRoles error must never be
// read as "no memberships", which is exactly what
// hasBindingForTenant(nil, ...) == false would produce if a future
// refactor let the error fall through to the normal not-a-member path.
// That would silently convert "authorization is unavailable" into
// "you are definitively not a member" — a 403 that looks like a
// deliberate, permanent denial rather than a transient outage — so
// this must be 503, not 403, and not 200.
func TestSwitchTenantFailsClosedOnRoleListerError(t *testing.T) {
	roles := &fakeRoleLister{err: errors.New("openfga unreachable")}
	minter := &recordingMinter{}
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"jane": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"jane": {}},
		Writer:  &recordingWriter{},
		Roles:   roles,
		Minter:  minter,
		Modules: []platform.Module{iam.New()},
	})

	// The requested tenant is one jane genuinely belongs to in the real
	// world (this fake just can't say so, because ListRoles errors
	// before membership is ever consulted) — the point is that the
	// outage, not a real absence of membership, must drive the
	// response.
	res := testutil.Do(r, "POST", "/v1/iam/me/tenant", "jane",
		`{"tenant_id":"`+testutil.TenantA+`"}`)
	require.Equal(t, http.StatusServiceUnavailable, res.Code,
		"a ListRoles error must fail closed as 503, never as 403 or 200")

	var body struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
	require.Equal(t, "authz_unavailable", body.Error)
	require.Empty(t, minter.calls,
		"minting must sit behind the gate: an unresolved membership check must not produce a credential")
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
		Modules: []platform.Module{iam.New()},
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
		Modules:    []platform.Module{iam.New()},
	})

	for _, path := range []string{"/v1/iam/me/permissions", "/v1/iam/me/tenants"} {
		w := testutil.Do(r, http.MethodGet, path, "nurse", "")
		require.Equal(t, http.StatusOK, w.Code,
			"%s must answer a caller with no membership — it is the endpoint that tells them where they do belong", path)
	}
}

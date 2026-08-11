package iam_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/modules/iam" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
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

func TestMePermissionsReturnsResolvedSetSorted(t *testing.T) {
	r, _, _, _ := testutil.ModuleHarnessWithAuthz(t,
		map[string]string{"doc": testutil.TenantA},
		map[string][]authz.Permission{"doc": {"medicore.visit.read", "lab.order.read"}},
		&recordingWriter{}, iam.New())

	res := testutil.Do(r, "GET", "/v1/iam/me/permissions", "doc", "")
	require.Equal(t, http.StatusOK, res.Code)

	var body struct {
		Data []string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
	require.Equal(t, []string{"lab.order.read", "medicore.visit.read"}, body.Data)
}

func TestMePermissionsIsEmptyArrayNotNullForNonMember(t *testing.T) {
	r, _, _, _ := testutil.ModuleHarnessWithAuthz(t,
		map[string]string{"nobody": testutil.TenantA},
		map[string][]authz.Permission{"nobody": {}},
		&recordingWriter{}, iam.New())

	res := testutil.Do(r, "GET", "/v1/iam/me/permissions", "nobody", "")
	require.Equal(t, http.StatusOK, res.Code)
	require.JSONEq(t, `{"data":[]}`, res.Body.String())
}

func TestMeTenantsListsEveryMembership(t *testing.T) {
	roles := &fakeRoleLister{bindings: map[string][]authz.RoleBinding{
		// user-jane is the subject StaticVerifier derives from token "jane".
		"user-jane": {
			{TenantID: testutil.TenantA, Role: authz.RoleDoctor},
			{TenantID: testutil.TenantB, Role: authz.RoleNurse},
		},
	}}
	r, _, _, _ := testutil.ModuleHarnessWithRoles(t,
		map[string]string{"jane": testutil.TenantA},
		map[string][]authz.Permission{"jane": {}},
		&recordingWriter{}, roles, iam.New())

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

func TestMeTenantsIsEmptyArrayNotNullForNonMember(t *testing.T) {
	roles := &fakeRoleLister{bindings: map[string][]authz.RoleBinding{}}
	r, _, _, _ := testutil.ModuleHarnessWithRoles(t,
		map[string]string{"nobody": testutil.TenantA},
		map[string][]authz.Permission{"nobody": {}},
		&recordingWriter{}, roles, iam.New())

	res := testutil.Do(r, "GET", "/v1/iam/me/tenants", "nobody", "")
	require.Equal(t, http.StatusOK, res.Code)
	require.JSONEq(t, `{"data":[]}`, res.Body.String())
}

func TestMeTenantsFailsClosedOnRoleListerError(t *testing.T) {
	roles := &fakeRoleLister{err: errors.New("openfga unreachable")}
	r, _, _, _ := testutil.ModuleHarnessWithRoles(t,
		map[string]string{"jane": testutil.TenantA},
		map[string][]authz.Permission{"jane": {}},
		&recordingWriter{}, roles, iam.New())

	res := testutil.Do(r, "GET", "/v1/iam/me/tenants", "jane", "")
	require.Equal(t, http.StatusServiceUnavailable, res.Code,
		"a ListRoles error must fail closed, never read as \"no memberships\"")
}

func TestSwitchTenantRequiresMembership(t *testing.T) {
	roles := &fakeRoleLister{bindings: map[string][]authz.RoleBinding{
		"user-jane": {{TenantID: testutil.TenantA, Role: authz.RoleNurse}},
	}}
	r, _, _, _ := testutil.ModuleHarnessWithRoles(t,
		map[string]string{"jane": testutil.TenantA},
		map[string][]authz.Permission{"jane": {}},
		&recordingWriter{}, roles, iam.New())

	res := testutil.Do(r, "POST", "/v1/iam/me/tenant", "jane",
		`{"tenant_id":"`+testutil.TenantB+`"}`)
	require.Equal(t, http.StatusForbidden, res.Code,
		"switching into a tenant you are not a member of must be denied")
}

func TestSwitchTenantSucceedsWithMembership(t *testing.T) {
	roles := &fakeRoleLister{bindings: map[string][]authz.RoleBinding{
		"user-jane": {
			{TenantID: testutil.TenantA, Role: authz.RoleDoctor},
			{TenantID: testutil.TenantB, Role: authz.RoleNurse},
		},
	}}
	r, _, _, _ := testutil.ModuleHarnessWithRoles(t,
		map[string]string{"jane": testutil.TenantA},
		map[string][]authz.Permission{"jane": {}},
		&recordingWriter{}, roles, iam.New())

	res := testutil.Do(r, "POST", "/v1/iam/me/tenant", "jane",
		`{"tenant_id":"`+testutil.TenantB+`"}`)
	require.Equal(t, http.StatusOK, res.Code)
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
	r, _, _, _ := testutil.ModuleHarnessWithRoles(t,
		map[string]string{"jane": testutil.TenantA},
		map[string][]authz.Permission{"jane": {}},
		&recordingWriter{}, roles, iam.New())

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
}

// TestSwitchTenantSucceedsAcrossTenantIDCasing guards against a real
// member being refused their own hospital because of a UUID casing
// mismatch: the binding here was granted under an upper-cased tenant
// id (simulating whatever casing happened to be in effect at grant
// time), and the switch request uses the lower-cased form. Both must
// resolve to the same tenant, and the response must echo the canonical
// (lower-cased) form regardless of what casing the caller sent.
func TestSwitchTenantSucceedsAcrossTenantIDCasing(t *testing.T) {
	roles := &fakeRoleLister{bindings: map[string][]authz.RoleBinding{
		"user-jane": {{TenantID: strings.ToUpper(testutil.TenantB), Role: authz.RoleNurse}},
	}}
	r, _, _, _ := testutil.ModuleHarnessWithRoles(t,
		map[string]string{"jane": testutil.TenantA},
		map[string][]authz.Permission{"jane": {}},
		&recordingWriter{}, roles, iam.New())

	res := testutil.Do(r, "POST", "/v1/iam/me/tenant", "jane",
		`{"tenant_id":"`+testutil.TenantB+`"}`)
	require.Equal(t, http.StatusOK, res.Code,
		"a casing difference between the request and the granted binding must not deny a real member")

	var body struct {
		TenantID string `json:"tenant_id"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
	require.Equal(t, testutil.TenantB, body.TenantID, "the response must echo the canonical (lower-cased) tenant id")
}

// TestMeTenantsMergesDifferentlyCasedBindingsForSameTenant guards the
// same casing bug on the list side: two bindings for the same tenant
// that differ only in casing (as could happen if grants were issued at
// different times with different casing) must collapse into one
// tenant entry with both roles, not appear as two separate hospitals.
func TestMeTenantsMergesDifferentlyCasedBindingsForSameTenant(t *testing.T) {
	roles := &fakeRoleLister{bindings: map[string][]authz.RoleBinding{
		"user-jane": {
			{TenantID: strings.ToUpper(testutil.TenantA), Role: authz.RoleDoctor},
			{TenantID: testutil.TenantA, Role: authz.RoleNurse},
		},
	}}
	r, _, _, _ := testutil.ModuleHarnessWithRoles(t,
		map[string]string{"jane": testutil.TenantA},
		map[string][]authz.Permission{"jane": {}},
		&recordingWriter{}, roles, iam.New())

	res := testutil.Do(r, "GET", "/v1/iam/me/tenants", "jane", "")
	require.Equal(t, http.StatusOK, res.Code)

	var body struct {
		Data []struct {
			TenantID string   `json:"tenant_id"`
			Roles    []string `json:"roles"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
	require.Len(t, body.Data, 1, "differently-cased bindings for the same tenant must merge into one entry")
	require.Equal(t, testutil.TenantA, body.Data[0].TenantID)
	require.ElementsMatch(t, []string{"doctor", "nurse"}, body.Data[0].Roles)
}

package iam_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
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

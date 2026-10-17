package supply_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

type supportApproverRoles struct{ role authz.Role }

func (r *supportApproverRoles) ListRoles(ctx context.Context, subject string) ([]authz.RoleBinding, error) {
	if subject == "user-manager" {
		return []authz.RoleBinding{{TenantID: testutil.TenantA, Role: r.role}}, nil
	}
	return (accessMembership{}).ListRoles(ctx, subject)
}

func TestSupportDiagnosticsStopsWhenApproverLosesTenantCapability(t *testing.T) {
	roles := &supportApproverRoles{role: authz.RoleSupplyAdmin}
	r, _ := supportHarnessWithRoles(t, roles)
	root, premise := supportFixture(t, r)
	path := root + "/support/" + uuid.Must(uuid.NewV7()).String()
	from := time.Now().Add(-time.Minute)
	require.Equal(t, 200, testutil.Do(r, "PUT", path, "owner", supportBody(premise, from, from.Add(time.Hour))).Code)
	require.Equal(t, 200, testutil.Do(r, "POST", path+"/decision", "manager", decisionBody(1, "approve")).Code)
	require.Equal(t, 200, testutil.Do(r, "GET", path+"/diagnostics", "auditor", "").Code)
	roles.role = authz.RoleSupplyFinance
	w := testutil.Do(r, "GET", path+"/diagnostics", "auditor", "")
	require.Equal(t, 404, w.Code, "membership alone must not preserve an approval capability: %s", w.Body.String())
	require.NotContains(t, w.Body.String(), "bin_count")
	for _, resource := range []string{path, root + "/support"} {
		w = testutil.Do(r, "GET", resource, "auditor", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"effective_status":"approval_withdrawn"`)
		require.NotContains(t, w.Body.String(), "read_diagnostics")
	}
	roles.role = authz.RoleSupplyOwner
	require.Equal(t, 200, testutil.Do(r, "GET", path+"/diagnostics", "auditor", "").Code)
	roles.role = authz.RoleTenantAdmin
	require.Equal(t, 200, testutil.Do(r, "GET", path+"/diagnostics", "auditor", "").Code)
}

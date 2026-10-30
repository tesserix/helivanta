package archtest

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestCurrentIAMAuthorityPreservesApprovedPermissionMatrix(t *testing.T) {
	_, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{iam.New(nil)}})
	reg := registry(t)
	graph, err := authz.NewClient(ctx, testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	for _, tenant := range []string{tenantA, tenantB} {
		require.NoError(t, platform.ReconcileTenant(ctx, reg, graph, tenant))
	}
	authority, err := iam.NewCurrentAuthorization(db, reg, graph)
	require.NoError(t, err)
	for _, role := range authz.SystemRoles() {
		t.Run(string(role), func(t *testing.T) {
			subject := "synthetic-current-" + string(role)
			require.NoError(t, db.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
				return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,?,?)`, tenantA, subject, role).Error
			}))
			for _, tenant := range []string{tenantA, tenantB} {
				require.NoError(t, graph.GrantTenantRole(ctx, tenant, role))
				require.NoError(t, graph.GrantRole(ctx, tenant, subject, role))
			}
			permissions, err := authority.Resolve(ctx, subject, tenantA)
			require.NoError(t, err)
			for permission, roles := range approvedPermissionMatrix {
				require.Equal(t, slices.Contains(roles, role), permissions.Has(permission), "%s / %s", role, permission)
			}
			member, err := authority.IsMember(ctx, subject, tenantA)
			require.NoError(t, err)
			require.True(t, member)
			foreignPermissions, err := authority.Resolve(ctx, subject, tenantB)
			require.NoError(t, err)
			require.Empty(t, foreignPermissions, "another tenant's unsupported graph tuple cannot confer authority")
			member, err = authority.IsMember(ctx, subject, tenantB)
			require.NoError(t, err)
			require.False(t, member)
			require.NoError(t, db.WithTenant(tenantdb.WithActor(ctx, "synthetic-owner"), tenantA, func(tx *gorm.DB) error {
				return tx.Exec(`DELETE FROM iam_members WHERE tenant_id=? AND subject=?`, tenantA, subject).Error
			}))
			permissions, err = authority.Resolve(ctx, subject, tenantA)
			require.NoError(t, err)
			require.Empty(t, permissions, "removal must deny every permission family even when all old graph tuples remain")
			member, err = authority.IsMember(ctx, subject, tenantA)
			require.NoError(t, err)
			require.False(t, member)
		})
	}
}

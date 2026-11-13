package archtest

import (
	"context"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/workforce"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestCurrentIAMAuthorityPreservesApprovedPermissionMatrix(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"matrix-owner": tenantA},
		Perms:   map[string][]authz.Permission{"matrix-owner": {iam.PermGovernanceManage}},
		Modules: []platform.Module{iam.New(nil).SetMembershipIdentityReader(matrixGovernanceIdentity{}), workforce.New()},
	})
	require.NoError(t, db.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'user-matrix-owner','tenant_admin')`, tenantA).Error
	}))
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
			seedRole := role
			if role == authz.RoleGovernanceDelegate {
				seedRole = authz.RoleStaffMember
			}
			require.NoError(t, db.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
				return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,?,?)`, tenantA, subject, seedRole).Error
			}))
			if role == authz.RoleGovernanceDelegate {
				seedMatrixGovernance(t, r, subject)
			}
			for _, tenant := range []string{tenantA, tenantB} {
				require.NoError(t, graph.GrantTenantRole(ctx, tenant, role))
				require.NoError(t, graph.GrantRole(ctx, tenant, subject, role))
			}
			var memberRow struct {
				ID    string
				Epoch int64
			}
			require.NoError(t, db.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
				return tx.Table("iam_members AS m").Select("m.id::text AS id,e.epoch").Joins("JOIN iam_subject_membership_epochs e ON e.tenant_id=m.tenant_id AND e.subject=m.subject").Where("m.tenant_id=? AND m.subject=? AND m.role_key=?", tenantA, subject, role).Take(&memberRow).Error
			}))
			require.NoError(t, graph.ProjectMembership(ctx, authz.Membership{TenantID: tenantA, ID: memberRow.ID, Subject: subject, Role: role, Epoch: memberRow.Epoch}))
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

// Only the external native identity observation is simulated; the managed-role fixture uses the actual source API.
type matrixGovernanceIdentity struct{}

func (matrixGovernanceIdentity) MembershipEligible(_ context.Context, tenant, subject string) (bool, error) {
	return tenant == tenantA && subject == "synthetic-current-governance_delegate", nil
}
func seedMatrixGovernance(t *testing.T, r *gin.Engine, subject string) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	input := iam.GovernanceGrantInput{Subject: subject, ExpectedMembershipEpoch: 1, TemplateKey: "iam.governance_auditor.v1", ScopeKind: "tenant", Reason: "Synthetic role matrix routing admission", ValidFrom: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}
	body, err := json.Marshal(input)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPut, "/v1/iam/governance-grants/"+uuid.Must(uuid.NewV7()).String(), strings.NewReader(string(body)))
	request.Header.Set("Authorization", "Bearer matrix-owner")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Helivanta-Expected-Tenant", tenantA)
	request.Header.Set("X-Helivanta-Expected-Subject", "user-matrix-owner")
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	require.Equal(t, http.StatusAccepted, response.Code, response.Body.String())
}

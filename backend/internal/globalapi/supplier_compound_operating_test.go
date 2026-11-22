package globalapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/pharmacy"
	"github.com/tesserix/helivanta/internal/modules/supply"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	fulfilment "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
)

func (f supplierCompoundFixture) assertSourceFulfilmentStaff(t *testing.T, admin operatingDutyFixture, source suppliertrade.Endpoint) {
	t.Helper()
	subject := "compound-" + source.Product + "-fulfiller"
	require.NoError(t, f.graph.GrantTenantRole(t.Context(), source.TenantID.String(), authz.RoleStaffMember))
	admin.call("POST", "/v1/iam/members", "owner", map[string]any{"subject": subject, "role_key": string(authz.RoleStaffMember)}, 202)
	require.Eventually(t, func() bool {
		member, err := f.graph.IsMember(t.Context(), subject, source.TenantID.String())
		return err == nil && member
	}, 20*time.Second, 100*time.Millisecond)
	kind, collection := "supplier", "suppliers"
	if source.Product == "pharmacy" {
		kind, collection = "pharmacy", "pharmacies"
	}
	employer := "/v1/workforce/" + collection + "/" + source.OrganizationID.String()
	admin.call("PUT", employer, "owner", map[string]any{}, 200)
	for _, manager := range []string{"compound-source-owner", "compound-source-reviewer"} {
		admin.call("PUT", employer+"/access/"+manager, "owner", map[string]any{"level": "manager"}, 200)
	}
	staff, link := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	staffPath := employer + "/staff/" + staff.String()
	admin.call("PUT", staffPath+"?directory_context=v1", "owner", map[string]any{"employee_code": "TEST-COMPOUND-" + source.Product, "display_name": "TEST " + source.Product + " Fulfilment Staff", "job_category": "other", "employment_status": "active", "expected_version": 0, "directory_context_version": "v1"}, 200)
	linkPath := staffPath + "/account-links/" + link.String()
	admin.call("PUT", linkPath, "owner", map[string]any{"expected_staff_version": 1, "subject": subject, "reason": "TEST source-owned Staff account"}, 201)
	admin.call("PUT", linkPath+"/commands/"+uuid.Must(uuid.NewV7()).String(), "reviewer", map[string]any{"expected_version": 1, "decision": "approve", "reason": "TEST independent source account review"}, 201)
	now := time.Now().UTC().Truncate(time.Second)
	principal := authn.Principal{Subject: subject, TenantID: source.TenantID.String(), AuthTime: now, IdleDeadline: now.Add(time.Hour)}
	input := iam.OperatingDutyInput{Scope: iam.OperatingDutyScope{EmployerKind: kind, EmployerID: source.OrganizationID, LocationID: source.LocationID, ExpectedEmployerVersion: 1, ExpectedLocationVersion: 1}, Identity: iam.OperatingDutyIdentity{StaffID: staff, StaffVersion: 1, LinkID: link, LinkVersion: 2}, Subject: subject, TemplateKey: source.Product + ".stock_fulfiller.v1", Reason: "TEST source reservation and dispatch duty", ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}
	duty := "/v1/iam/operating-duties/" + uuid.Must(uuid.NewV7()).String()
	admin.call("PUT", duty, "owner", input, 201)
	modules := f.registry.All()
	deps := platform.Deps{DB: f.db, OperatingAuthorizer: iam.NewOperatingAuthorizer(f.db, f.graph, platform.IndependentStaffIdentities(modules, f.db), platform.OperatingLocations(modules, f.db))}
	var adapter interface {
		Admit(context.Context, authn.Principal, fulfilment.SourceScope, func(context.Context, supplierorder.Actor) error) error
	}
	if source.Product == "pharmacy" {
		adapter = pharmacy.SupplierStock(deps)
	} else {
		adapter = supply.SupplierStock(deps)
	}
	scope := fulfilment.SourceScope{TenantID: source.TenantID, Product: source.Product, OrganizationID: source.OrganizationID, LocationID: source.LocationID, OrderID: uuid.Must(uuid.NewV7())}
	uses := 0
	use := func(current fulfilment.SourceScope) error {
		return adapter.Admit(t.Context(), principal, current, func(ctx context.Context, actor supplierorder.Actor) error {
			uses++
			require.Equal(t, subject, actor.Subject)
			require.Equal(t, staff, actor.StaffID)
			require.True(t, f.db.GuardedCommandActive(ctx))
			return nil
		})
	}
	require.ErrorIs(t, use(scope), authz.ErrScopedCommandDenied)
	require.Zero(t, uses, "unreviewed duties cannot admit stock work")
	admin.call("PUT", duty+"/commands/"+uuid.Must(uuid.NewV7()).String(), "reviewer", iam.OperatingDutyDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST independently approved fulfilment duty"}, 201)
	require.Eventually(t, func() bool {
		var current struct{ Status string }
		require.NoError(t, json.Unmarshal([]byte(admin.call("GET", duty, "owner", nil, 200)), &current))
		return current.Status == "active"
	}, 20*time.Second, 100*time.Millisecond)
	require.NoError(t, use(scope))
	require.Equal(t, 1, uses)
	for _, field := range []string{"organization", "location", "tenant"} {
		wrong := scope
		switch field {
		case "organization":
			wrong.OrganizationID = uuid.Must(uuid.NewV7())
		case "location":
			wrong.LocationID = uuid.Must(uuid.NewV7())
		case "tenant":
			wrong.TenantID = uuid.New()
		}
		require.Error(t, use(wrong), field)
		require.Equal(t, 1, uses, "wrong source cannot invoke physical owner callback")
	}
}

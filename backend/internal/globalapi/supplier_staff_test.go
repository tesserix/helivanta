package globalapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/supply"
	"github.com/tesserix/helivanta/internal/modules/workforce"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type supplierIdentityMembership struct{ targetRemoved atomic.Bool }

func (m *supplierIdentityMembership) IsMember(_ context.Context, subject, tenant string) (bool, error) {
	if subject == "user-operator" && m.targetRemoved.Load() {
		return false, nil
	}
	return (tenant == testutil.TenantB && subject == "user-foreign") || tenant == testutil.TenantA && (subject == "user-owner" || subject == "user-reviewer" || subject == "user-operator" || subject == "user-alias" || subject == "user-maker-alias"), nil
}

func TestSupplierAccountBindingNeedsIndependentReviewAndRetainsCommandRecovery(t *testing.T) {
	permissions := []authz.Permission{supply.PermPartyWrite, supply.PermOrganizationManage, "workforce.supplier.manage", "workforce.independent_login.manage", "workforce.independent_login.review"}
	membership := &supplierIdentityMembership{}
	owner, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Membership: membership, Tokens: map[string]string{"owner": testutil.TenantA, "reviewer": testutil.TenantA, "operator": testutil.TenantA, "alias": testutil.TenantA, "outsider": testutil.TenantA, "foreign": testutil.TenantB}, Perms: map[string][]authz.Permission{"owner": permissions, "reviewer": permissions, "operator": permissions, "foreign": permissions}, Modules: []platform.Module{supply.New(), workforce.New()}})
	server := httptest.NewServer(owner)
	t.Cleanup(server.Close)
	gateway, err := New(Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"supply": server.URL, "workforce": server.URL}})
	require.NoError(t, err)
	call := func(method, path, token, body string, status int) string {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		tenant := testutil.TenantA
		if token == "foreign" {
			tenant = testutil.TenantB
		}
		req.Header.Set("X-Helivanta-Expected-Tenant", tenant)
		req.Header.Set("X-Helivanta-Expected-Subject", "user-"+token)
		w := httptest.NewRecorder()
		gateway.ServeHTTP(w, req)
		require.Equal(t, status, w.Code, w.Body.String())
		return w.Body.String()
	}
	supplier := uuid.Must(uuid.NewV7()).String()
	call("PUT", "/v1/supply/parties/"+supplier, "owner", `{"expected_version":0,"code":"TEST-SUPPLIER-LINK","record_kind":"owned_organisation","legal_name":"TEST Supplier","trading_name":"TEST Supplier","business_kind":"wholesaler","country_code":"IN","jurisdiction":"IN-OD","registration_number":"TEST-REG-003","corporate_group":"","website":"","default_credit_days":30,"contacts":[],"addresses":[],"tax_registrations":[]}`, 200)
	base := "/v1/workforce/suppliers/" + supplier
	call("PUT", base, "owner", `{}`, 200)
	call("PUT", base+"/access/user-reviewer", "owner", `{"level":"manager"}`, 200)
	call("PUT", base+"/access/user-operator", "owner", `{"level":"manager"}`, 200)
	staff := uuid.Must(uuid.NewV7()).String()
	call("PUT", base+"/staff/"+staff+"?directory_context=v1", "owner", `{"employee_code":"TEST-OP-1","display_name":"TEST Supplier Operator","job_category":"other","employment_status":"active","expected_version":0,"directory_context_version":"v1"}`, 200)
	path := base + "/staff/" + staff + "/account-links/" + uuid.Must(uuid.NewV7()).String()
	input := `{"expected_staff_version":1,"subject":"user-operator","reason":"TEST checked account ownership"}`
	call("PUT", path, "owner", strings.Replace(input, "user-operator", "user-unknown", 1), 404)
	proposal := call("PUT", path, "owner", input, 201)
	require.NotContains(t, proposal, "hospital_id")
	listed := call("GET", base+"/staff/"+staff+"/account-links?limit=1", "owner", "", 200)
	require.Contains(t, listed, `"status":"pending"`)
	require.NotContains(t, listed, "hospital_id")
	call("GET", path, "foreign", "", 404)
	call("GET", path, "outsider", "", 403)
	require.JSONEq(t, proposal, call("PUT", path, "owner", input, 200))
	call("PUT", path, "reviewer", input, 409)
	require.JSONEq(t, proposal, call("GET", path+"/proposal-receipt", "owner", "", 200))
	command := path + "/commands/" + uuid.Must(uuid.NewV7()).String()
	review := `{"expected_version":1,"decision":"approve","reason":"TEST independently checked identity"}`
	call("PUT", command, "owner", review, 409)
	approved := call("PUT", command, "reviewer", review, 201)
	require.Contains(t, approved, `"status":"active"`)
	require.JSONEq(t, approved, call("GET", command, "reviewer", "", 200))
	guard := workforce.New().IndependentStaffIdentities(db)
	actorScope := authz.IndependentStaffScope{TenantID: testutil.TenantA, Subject: "user-operator", EmployerKind: "supplier", EmployerID: uuid.MustParse(supplier)}
	observe := func(commandCtx context.Context) (authz.IndependentStaffReference, error) {
		var ref authz.IndependentStaffReference
		err := guard.WithCurrentIndependentStaff(commandCtx, actorScope, func(_ context.Context, current authz.IndependentStaffReference) error { ref = current; return nil })
		return ref, err
	}
	_, unguarded := observe(tenantdb.WithActor(ctx, "user-owner"))
	require.ErrorIs(t, unguarded, authz.ErrScopedAuthorizationUnavailable)
	guardedObserve := func() (authz.IndependentStaffReference, error) {
		var ref authz.IndependentStaffReference
		err := db.WithGuardedTenantCommand(tenantdb.WithActor(ctx, "user-owner"), testutil.TenantA, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error { var err error; ref, err = observe(commandCtx); return err })
		return ref, err
	}
	current, currentErr := guardedObserve()
	require.NoError(t, currentErr)
	require.Equal(t, uuid.MustParse(staff), current.StaffID)
	require.Equal(t, int64(1), current.StaffVersion)
	require.Equal(t, int64(2), current.LinkVersion)
	reviewIdentity := func(actor, proposer string) error {
		return db.WithGuardedTenantCommand(tenantdb.WithActor(ctx, actor), testutil.TenantA, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
			return guard.WithCurrentIndependentStaffForReview(commandCtx, actorScope, proposer, func(context.Context, authz.IndependentStaffReference) error { return nil })
		})
	}
	require.NoError(t, reviewIdentity("user-reviewer", "user-owner"))
	makerStaff := uuid.Must(uuid.NewV7()).String()
	call("PUT", base+"/staff/"+makerStaff+"?directory_context=v1", "owner", `{"employee_code":"TEST-MAKER","display_name":"TEST Maker Identity","job_category":"other","employment_status":"active","expected_version":0,"directory_context_version":"v1"}`, 200)
	makerLink := base + "/staff/" + makerStaff + "/account-links/" + uuid.Must(uuid.NewV7()).String()
	call("PUT", makerLink, "reviewer", strings.Replace(input, "user-operator", "user-owner", 1), 201)
	call("PUT", makerLink+"/commands/"+uuid.Must(uuid.NewV7()).String(), "operator", review, 201)
	call("PUT", makerLink+"/commands/"+uuid.Must(uuid.NewV7()).String(), "owner", `{"expected_version":2,"decision":"revoke","reason":"TEST replaced maker login"}`, 201)
	makerAlias := base + "/staff/" + makerStaff + "/account-links/" + uuid.Must(uuid.NewV7()).String()
	call("PUT", makerAlias, "reviewer", strings.Replace(input, "user-operator", "user-maker-alias", 1), 201)
	call("PUT", makerAlias+"/commands/"+uuid.Must(uuid.NewV7()).String(), "operator", review, 201)
	require.ErrorIs(t, reviewIdentity("user-maker-alias", "user-owner"), authz.ErrScopedCommandDenied)
	require.ErrorIs(t, reviewIdentity("user-operator", "user-owner"), authz.ErrScopedCommandDenied)
	require.ErrorIs(t, reviewIdentity("user-owner", "user-owner"), authz.ErrScopedCommandDenied)
	call("GET", command, "owner", "", 404)
	call("PUT", command, "reviewer", strings.Replace(review, "independently", "differently", 1), 409)
	revoke := path + "/commands/" + uuid.Must(uuid.NewV7()).String()
	call("PUT", revoke, "owner", `{"expected_version":2,"decision":"revoke","reason":"TEST account replacement"}`, 201)
	_, cutoffErr := guardedObserve()
	require.ErrorIs(t, cutoffErr, authz.ErrScopedCommandDenied)
	membership.targetRemoved.Store(true)
	require.JSONEq(t, approved, call("PUT", command, "reviewer", review, 200))
	require.JSONEq(t, proposal, call("PUT", path, "owner", input, 200))
	membership.targetRemoved.Store(false)
	alias := base + "/staff/" + staff + "/account-links/" + uuid.Must(uuid.NewV7()).String()
	call("PUT", alias, "owner", strings.Replace(input, "user-operator", "user-alias", 1), 201)
	call("PUT", alias+"/commands/"+uuid.Must(uuid.NewV7()).String(), "operator", review, 409)
	call("PUT", alias+"/commands/"+uuid.Must(uuid.NewV7()).String(), "reviewer", review, 201)

	actorScope.Subject = "user-alias"
	require.ErrorIs(t, reviewIdentity("user-operator", "user-owner"), authz.ErrScopedCommandDenied)
	_, relinkedErr := guardedObserve()
	require.NoError(t, relinkedErr)
	call("PUT", base+"/staff/"+staff+"?directory_context=v1", "owner", `{"employee_code":"TEST-OP-1","display_name":"TEST Updated Supplier Operator","job_category":"other","employment_status":"active","expected_version":1,"directory_context_version":"v1"}`, 200)
	_, editedErr := guardedObserve()
	require.ErrorIs(t, editedErr, authz.ErrScopedCommandDenied)
	call("PUT", base+"/staff/"+staff+"?directory_context=v1", "owner", `{"employee_code":"TEST-OP-1","display_name":"TEST Updated Supplier Operator","job_category":"other","employment_status":"inactive","expected_version":2,"directory_context_version":"v1"}`, 200)
	require.Contains(t, call("GET", alias, "reviewer", "", 200), `"status":"revoked"`)
	legacyStaff := uuid.NewString()
	call("PUT", base+"/staff/"+legacyStaff+"?directory_context=v1", "owner", `{"employee_code":"TEST-LEGACY","display_name":"TEST Existing Directory Staff","job_category":"other","employment_status":"active","expected_version":0,"directory_context_version":"v1"}`, 200)
	call("PUT", base+"/staff/"+legacyStaff+"/account-links/"+uuid.Must(uuid.NewV7()).String(), "owner", strings.Replace(input, "user-operator", "user-alias", 1), 201)
}

func TestSupplierStaffUsesOwnedEmployerAndRefusesOtherFamilies(t *testing.T) {
	manage := []authz.Permission{supply.PermPartyWrite, supply.PermOrganizationManage, workforce.PermHospitalManage, "workforce.supplier.manage"}
	owner, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"owner": testutil.TenantA, "reader": testutil.TenantA, "outsider": testutil.TenantA, "foreign": testutil.TenantB}, Perms: map[string][]authz.Permission{"owner": manage, "foreign": manage, "reader": {}, "outsider": {}}, Modules: []platform.Module{supply.New(), workforce.New()}})
	server := httptest.NewServer(owner)
	t.Cleanup(server.Close)
	gateway, err := New(Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"supply": server.URL, "workforce": server.URL}})
	require.NoError(t, err)
	call := func(method, path, token, body string, status int) string {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		tenant := testutil.TenantA
		if token == "foreign" {
			tenant = testutil.TenantB
		}
		req.Header.Set("X-Helivanta-Expected-Tenant", tenant)
		req.Header.Set("X-Helivanta-Expected-Subject", "user-"+token)
		w := httptest.NewRecorder()
		gateway.ServeHTTP(w, req)
		require.Equal(t, status, w.Code, w.Body.String())
		return w.Body.String()
	}
	supplier := uuid.Must(uuid.NewV7()).String()
	call("PUT", "/v1/supply/parties/"+supplier, "owner", `{"expected_version":0,"code":"TEST-SUPPLIER-HR","record_kind":"owned_organisation","legal_name":"TEST Independent Distributor","trading_name":"TEST Distributor","business_kind":"wholesaler","country_code":"IN","jurisdiction":"IN-MH","registration_number":"TEST-REG-002","corporate_group":"","website":"","default_credit_days":30,"contacts":[],"addresses":[],"tax_registrations":[]}`, 200)
	base := "/v1/workforce/suppliers/" + supplier
	created := call("PUT", base, "owner", `{}`, 200)
	require.Contains(t, created, `"supplier_id":"`+supplier+`"`)
	require.NotContains(t, created, "hospital_id")
	call("PUT", base, "foreign", `{}`, 404)
	staff := uuid.Must(uuid.NewV7()).String()
	path := base + "/staff/" + staff + "?directory_context=v1"
	body := `{"employee_code":"TEST-SUP-1","display_name":"TEST Warehouse Operator","job_category":"other","employment_status":"active","expected_version":0,"directory_context_version":"v1"}`
	saved := call("PUT", path, "owner", body, 200)
	require.Contains(t, saved, `"supplier_id":"`+supplier+`"`)
	require.NotContains(t, saved, "hospital_id")
	call("GET", base+"/staff", "outsider", "", 404)
	call("PUT", base+"/access/user-reader", "owner", `{"level":"reader"}`, 200)
	require.Contains(t, call("GET", base+"/staff", "reader", "", 200), "TEST Warehouse Operator")
	call("GET", path, "foreign", "", 404)
	call("GET", "/v1/workforce/hospitals/"+supplier+"/staff?directory_context=v1", "owner", "", 404)
	call("GET", "/v1/workforce/pharmacies/"+supplier+"/staff", "owner", "", 404)
	call("DELETE", base+"/access/user-reader", "owner", "", 204)
	require.NotContains(t, call("GET", base+"/staff", "reader", "", 404), "TEST Warehouse Operator")
}

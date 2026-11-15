package globalapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/drugmaster"
	"github.com/tesserix/helivanta/internal/modules/iam"
	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	"github.com/tesserix/helivanta/internal/modules/supply"
	"github.com/tesserix/helivanta/internal/modules/workforce"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
)

type operatingVerifier struct{ at time.Time }

func (v operatingVerifier) Verify(_ context.Context, token string) (authn.Principal, error) {
	tenant := testutil.TenantA
	if token == "foreign" {
		tenant = testutil.TenantB
	}
	if token != "owner" && token != "reviewer" && token != "receiver" && token != "foreign" {
		return authn.Principal{}, fmt.Errorf("TEST unknown credential")
	}
	return authn.Principal{TenantID: tenant, Subject: "user-" + token, AuthTime: v.at, IdleDeadline: v.at.Add(time.Hour)}, nil
}

type operatingProjectionGate struct {
	*authz.Client
	enabled   atomic.Bool
	published chan struct{}
	release   chan struct{}
	once      sync.Once
}

func (g *operatingProjectionGate) ProjectOperatingAssignment(ctx context.Context, assignment authz.OperatingAssignment) error {
	if !g.enabled.Load() {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if err := g.Client.ProjectOperatingAssignment(ctx, assignment); err != nil {
		return err
	}
	if g.published != nil {
		g.once.Do(func() { close(g.published) })
		select {
		case <-g.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// operatingCheckGate lets a test take the authority graph away without touching the duty itself.
type operatingCheckGate struct {
	*authz.Client
	down atomic.Bool
}

func (g *operatingCheckGate) CheckOperatingAssignment(ctx context.Context, a authz.OperatingAssignment, actor string, permission authz.Permission) (bool, error) {
	if g.down.Load() {
		return false, authz.ErrScopedAuthorizationUnavailable
	}
	return g.Client.CheckOperatingAssignment(ctx, a, actor, permission)
}

type operatingDutyFixture struct {
	handler    http.Handler
	authority  authz.OperatingScopeAuthorizer
	principal  authn.Principal
	graph      *authz.Client
	projection *operatingProjectionGate
	checks     *operatingCheckGate
	db         *tenantdb.DB
	ctx        context.Context
	call       func(string, string, string, any, int) string
	input      iam.OperatingDutyInput
	duty       string
}

func newOperatingDutyFixture(t *testing.T) operatingDutyFixture {
	return newOperatingDutyFixtureWithOwners(t, nil)
}
func newOperatingDutyFixtureWithOwners(t *testing.T, documents *attachments.GCS, extra ...platform.Module) operatingDutyFixture {
	t.Helper()
	graph, err := authz.NewClient(t.Context(), testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	admin := []authz.Permission{iam.PermMemberManage, iam.PermOperatingRead, iam.PermOperatingPrepare, iam.PermOperatingReview, iam.PermOperatingRevoke, workforce.PermSupplierManage, workforce.PermIndependentLoginManage, workforce.PermIndependentLoginReview, supply.PermPartyWrite, supply.PermOrganizationManage, supply.PermPremiseWrite, drugmaster.PermStockReferenceRead}
	projection := &operatingProjectionGate{Client: graph}
	checks := &operatingCheckGate{Client: graph}
	modules := append([]platform.Module{iam.New(nil).SetMembershipIdentityReader(globalEnrollmentIdentity{}), supply.New(), workforce.New(), drugmaster.New()}, extra...)
	for _, m := range modules {
		for _, grant := range m.Permissions() {
			admin = append(admin, grant.Permission)
		}
	}
	authenticatedAt := time.Now().Add(-time.Minute)
	backend, db, _, fixtureCtx := testutil.NewHarness(t, testutil.HarnessOptions{GovernanceSources: testutil.GovernanceReviewProtocol, Documents: documents, PharmacyEvidenceEnabled: documents != nil, SourceAdministration: func(db *tenantdb.DB) authz.SourceAdministrationGuard {
		return iam.NewSourceAdministrationGuard(db, graph)
	}, Verifier: operatingVerifier{at: authenticatedAt}, Writer: projection, Roles: graph, OperatingAuthorizer: func(db *tenantdb.DB) authz.OperatingScopeAuthorizer {
		return iam.NewOperatingAuthorizer(db, checks, platform.IndependentStaffIdentities(modules, db), platform.OperatingLocations(modules, db))
	}, Tokens: map[string]string{"owner": testutil.TenantA, "reviewer": testutil.TenantA, "receiver": testutil.TenantA, "foreign": testutil.TenantB}, Perms: map[string][]authz.Permission{"owner": admin, "reviewer": admin, "foreign": admin}, Modules: modules})

	for _, m := range modules {
		for _, grant := range m.Permissions() {
			require.NoError(t, graph.GrantPermission(t.Context(), testutil.TenantA, grant.Permission, authz.RoleTenantAdmin))
			for _, role := range grant.Roles {
				require.NoError(t, graph.GrantPermission(t.Context(), testutil.TenantA, grant.Permission, role))
			}
		}
	}
	// The request fixture's resolver is not proof: install the registered admin grants in real OpenFGA.
	for _, permission := range []authz.Permission{iam.PermOperatingRead, iam.PermOperatingPrepare, iam.PermOperatingReview, iam.PermOperatingRevoke} {
		require.NoError(t, graph.GrantPermission(t.Context(), testutil.TenantA, permission, authz.RoleTenantAdmin))
	}

	for _, permission := range []authz.Permission{iam.PermOperatingRead, iam.PermOperatingReview} {
		require.NoError(t, graph.GrantPermission(t.Context(), testutil.TenantA, permission, authz.RoleSupplyCompliance))
	}
	server := httptest.NewServer(backend)
	t.Cleanup(server.Close)
	gateway, err := New(Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"platform": server.URL, "workforce": server.URL, "supply": server.URL, "pharmacy": server.URL}})
	require.NoError(t, err)
	call := func(method, path, actor string, input any, status int) string {
		body := ""
		if input != nil {
			data, err := json.Marshal(input)
			require.NoError(t, err)
			body = string(data)
		}
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+actor)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Helivanta-Expected-Subject", "user-"+actor)
		tenant := testutil.TenantA
		if actor == "foreign" {
			tenant = testutil.TenantB
		}
		req.Header.Set("X-Helivanta-Expected-Tenant", tenant)
		response := httptest.NewRecorder()
		gateway.ServeHTTP(response, req)
		require.Equal(t, status, response.Code, response.Body.String())
		return response.Body.String()
	}
	for _, actor := range []string{"owner", "reviewer", "receiver"} {
		role := "tenant_admin"
		if actor == "reviewer" {
			role = "supply_compliance"
		}
		if actor == "receiver" {
			role = "staff_member"
		}
		call("POST", "/v1/iam/members", "owner", map[string]any{"subject": "user-" + actor, "role_key": role}, 202)
	}
	require.Eventually(t, func() bool {
		member, err := graph.IsMember(t.Context(), "user-receiver", testutil.TenantA)
		return err == nil && member
	}, 20*time.Second, 50*time.Millisecond)
	party, premise, staff, link := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	source := "/v1/supply/parties/" + party.String()
	call("PUT", source, "owner", map[string]any{"expected_version": 0, "code": "TEST-DUTY", "record_kind": "owned_organisation", "legal_name": "TEST Duty Supplier", "trading_name": "", "business_kind": "distributor", "country_code": "IN", "jurisdiction": "IN-MH", "registration_number": "TEST-DUTY-REG", "corporate_group": "", "website": "", "default_credit_days": 0, "contacts": []any{}, "addresses": []any{}, "tax_registrations": []any{}}, 200)
	call("PUT", source+"/premises/"+premise.String(), "owner", map[string]any{"expected_version": 0, "code": "TEST-WAREHOUSE", "name": "TEST Duty Warehouse", "kind": "warehouse", "line1": "TEST Building", "line2": "", "city": "Pune", "region": "IN-MH", "postal_code": "411001", "country_code": "IN", "time_zone": "Asia/Kolkata", "status": "active"}, 200)
	employer := "/v1/workforce/suppliers/" + party.String()
	call("PUT", employer, "owner", map[string]any{}, 200)
	call("PUT", employer+"/access/user-reviewer", "owner", map[string]any{"level": "manager"}, 200)
	staffPath := employer + "/staff/" + staff.String()
	call("PUT", staffPath+"?directory_context=v1", "owner", map[string]any{"employee_code": "TEST-RX", "display_name": "TEST Warehouse Receiver", "job_category": "other", "employment_status": "active", "expected_version": 0, "directory_context_version": "v1"}, 200)
	linkPath := staffPath + "/account-links/" + link.String()
	call("PUT", linkPath, "owner", map[string]any{"expected_staff_version": 1, "subject": "user-receiver", "reason": "TEST verified account ownership"}, 201)
	call("PUT", linkPath+"/commands/"+uuid.Must(uuid.NewV7()).String(), "reviewer", map[string]any{"expected_version": 1, "decision": "approve", "reason": "TEST independently reviewed identity"}, 201)
	now := time.Now().UTC().Truncate(time.Second)
	input := iam.OperatingDutyInput{Scope: iam.OperatingDutyScope{EmployerKind: "supplier", EmployerID: party, LocationID: premise, ExpectedEmployerVersion: 1, ExpectedLocationVersion: 1}, Identity: iam.OperatingDutyIdentity{StaffID: staff, StaffVersion: 1, LinkID: link, LinkVersion: 2}, Subject: "user-receiver", TemplateKey: "supply.inventory_receiver.v1", Reason: "TEST scoped warehouse receiving duty", ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)}
	duty := "/v1/iam/operating-duties/" + uuid.Must(uuid.NewV7()).String()
	return operatingDutyFixture{handler: gateway, call: call, input: input, duty: duty, graph: graph, projection: projection, checks: checks, db: db, ctx: fixtureCtx, authority: iam.NewOperatingAuthorizer(db, checks, platform.IndependentStaffIdentities(modules, db), platform.OperatingLocations(modules, db)), principal: authn.Principal{TenantID: testutil.TenantA, Subject: "user-receiver", AuthTime: authenticatedAt, IdleDeadline: authenticatedAt.Add(time.Hour)}}
}
func TestIndependentOperatingDutyPreparationBindsCurrentSourceAndIdentity(t *testing.T) {
	fixture := newOperatingDutyFixture(t)
	call, input, duty := fixture.call, fixture.input, fixture.duty

	first := call("PUT", duty, "owner", input, 201)
	require.Contains(t, first, `"status":"pending_review"`)
	require.NotContains(t, first, "hospital_id")
	require.Contains(t, first, `"owner_tenant_id":"`+testutil.TenantA+`"`)

	require.Contains(t, call("GET", duty, "reviewer", nil, 200), `"status":"pending_review"`)
	require.JSONEq(t, first, call("PUT", duty, "owner", input, 200))
	require.JSONEq(t, first, call("GET", duty+"/proposal-receipt", "owner", nil, 200))
	changed := input
	changed.Reason = "TEST changed intent"
	call("PUT", duty, "owner", changed, 409)
	call("PUT", "/v1/iam/operating-duties/"+uuid.Must(uuid.NewV7()).String(), "receiver", input, 403)
	wrongKind := input
	wrongKind.Scope.EmployerKind = "pharmacy"
	call("PUT", "/v1/iam/operating-duties/"+uuid.Must(uuid.NewV7()).String(), "owner", wrongKind, 400)
}
func TestIndependentOperatingDutyProposalRecoverySurvivesExpiry(t *testing.T) {
	fixture := newOperatingDutyFixture(t)
	call, input, duty := fixture.call, fixture.input, fixture.duty
	input.ExpiresAt = time.Now().UTC().Truncate(time.Second).Add(6 * time.Second)
	first := call("PUT", duty, "owner", input, 201)
	require.Eventually(t, func() bool { return time.Now().After(input.ExpiresAt) }, 8*time.Second, 25*time.Millisecond)
	require.JSONEq(t, first, call("PUT", duty, "owner", input, 200))
	require.JSONEq(t, first, call("GET", duty+"/proposal-receipt", "owner", nil, 200))
	call("PUT", "/v1/iam/operating-duties/"+uuid.Must(uuid.NewV7()).String(), "owner", input, 400)
}
func TestIndependentOperatingDutyReviewIsIndependentAndRetainsExactCommand(t *testing.T) {
	fixture := newOperatingDutyFixture(t)
	call, input, duty := fixture.call, fixture.input, fixture.duty
	call("PUT", duty, "owner", input, 201)
	decision := iam.OperatingDutyDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST independently reviewed source duty"}
	command := duty + "/commands/" + uuid.Must(uuid.NewV7()).String()
	call("PUT", command, "owner", decision, 409)
	recorded := call("PUT", command, "reviewer", decision, 201)
	require.Contains(t, recorded, `"status":"approved_pending_activation"`)
	require.JSONEq(t, recorded, call("GET", command, "reviewer", nil, 200))
	require.JSONEq(t, recorded, call("PUT", command, "reviewer", decision, 200))
	changed := decision
	changed.Reason = "TEST different intent"
	call("PUT", command, "reviewer", changed, 409)
	call("GET", command, "owner", nil, 404)
	call("PUT", duty+"/commands/"+uuid.Must(uuid.NewV7()).String(), "reviewer", decision, 409)
	require.Contains(t, call("GET", duty, "owner", nil, 200), `"status":"approved_pending_activation"`)
}

func TestIndependentOperatingDutyProjectionRequiresExactGraphAcknowledgement(t *testing.T) {
	fixture := newOperatingDutyFixture(t)
	call, input, duty := fixture.call, fixture.input, fixture.duty
	call("PUT", duty, "owner", input, 201)
	command := duty + "/commands/" + uuid.Must(uuid.NewV7()).String()
	decision := iam.OperatingDutyDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST projection acceptance"}
	receipt := call("PUT", command, "reviewer", decision, 201)
	require.Contains(t, call("GET", duty, "owner", nil, 200), `"status":"approved_pending_activation"`)
	var row struct{ Document json.RawMessage }
	id := strings.TrimPrefix(duty, "/v1/iam/operating-duties/")
	require.NoError(t, fixture.db.WithTenant(tenantdb.WithActor(fixture.ctx, "TEST-owner-inspection"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.iam_operating_access','authorized',true)`).Error; err != nil {
			return err
		}
		return tx.Table("iam_operating_snapshots").Select("document").Where("tenant_id=? AND duty_id=? AND version=2", testutil.TenantA, id).Take(&row).Error
	}))
	var snapshot authz.OperatingAssignment
	require.NoError(t, json.Unmarshal(row.Document, &snapshot))
	allowed, err := fixture.graph.CheckOperatingAssignment(t.Context(), snapshot, "user-receiver", supply.PermOperatingWrite)
	require.NoError(t, err)
	require.False(t, allowed)
	fixture.projection.enabled.Store(true)
	// The deliberately failed delivery may have exhausted broker retries while
	// source denial was inspected. Recover through durable replay, not a larger
	// retry budget or an assumed unexpired delivery window.
	replay, err := json.Marshal(iamcontract.OperatingDutyChangedData{DutyID: id, Version: 2})
	require.NoError(t, err)
	require.NoError(t, fixture.db.WithTenantCommand(tenantdb.WithActor(t.Context(), "TEST owner projection replay"), testutil.TenantA, func(tx *gorm.DB) error {
		return events.Enqueue(tx, iamcontract.SubjectOperatingDutyChanged, events.Event{TenantID: testutil.TenantA, Type: iamcontract.SubjectOperatingDutyChanged, Version: 1, Data: replay})
	}))

	require.Eventually(t, func() bool { return strings.Contains(call("GET", duty, "owner", nil, 200), `"status":"active"`) }, 20*time.Second, 100*time.Millisecond)
	allowed, err = fixture.graph.CheckOperatingAssignment(t.Context(), snapshot, "user-receiver", supply.PermOperatingWrite)
	require.NoError(t, err)
	require.True(t, allowed)
	require.JSONEq(t, receipt, call("GET", command, "reviewer", nil, 200))
	require.JSONEq(t, receipt, call("PUT", command, "reviewer", decision, 200))
	resolved, err := fixture.graph.Resolve(t.Context(), "user-receiver", testutil.TenantA)
	require.NoError(t, err)
	require.False(t, resolved.Has(supply.PermOperatingWrite))
}

func TestIndependentOperatingDutyLateProjectionCannotReviveChangedAuthority(t *testing.T) {
	for _, change := range []string{"premise", "revocation"} {
		t.Run(change, func(t *testing.T) {
			fixture := newOperatingDutyFixture(t)
			call, input, duty := fixture.call, fixture.input, fixture.duty
			fixture.projection.published = make(chan struct{})
			fixture.projection.release = make(chan struct{})
			fixture.projection.enabled.Store(true)
			released := false
			defer func() {
				if !released {
					close(fixture.projection.release)
				}
			}()
			call("PUT", duty, "owner", input, 201)
			call("PUT", duty+"/commands/"+uuid.Must(uuid.NewV7()).String(), "reviewer", iam.OperatingDutyDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST paused projection"}, 201)
			select {
			case <-fixture.projection.published:
			case <-time.After(10 * time.Second):
				t.Fatal("graph publication did not start")
			}
			// These mutations must complete while the external graph call is paused: no database locks cross it.
			if change == "premise" {
				path := "/v1/supply/parties/" + input.Scope.EmployerID.String() + "/premises/" + input.Scope.LocationID.String()
				call("PUT", path, "owner", map[string]any{"expected_version": 1, "code": "TEST-WAREHOUSE", "name": "TEST Duty Warehouse", "kind": "warehouse", "line1": "TEST Building", "line2": "", "city": "Pune", "region": "IN-MH", "postal_code": "411001", "country_code": "IN", "time_zone": "Asia/Kolkata", "status": "inactive"}, 200)
			} else {
				call("PUT", duty+"/commands/"+uuid.Must(uuid.NewV7()).String(), "owner", iam.OperatingDutyDecisionInput{ExpectedVersion: 2, Decision: "revoke", Reason: "TEST revoke before graph acknowledgement"}, 201)
			}
			close(fixture.projection.release)
			released = true
			require.Eventually(t, func() bool { return strings.Contains(call("GET", duty, "owner", nil, 200), `"status":"revoked"`) }, 15*time.Second, 100*time.Millisecond)
			require.NoError(t, fixture.db.WithTenant(fixture.ctx, testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec(`SELECT set_config('app.iam_operating_access','authorized',true)`).Error; err != nil {
					return err
				}
				var count int64
				if err := tx.Table("iam_operating_projection_acks").Where("tenant_id=? AND duty_id=?", testutil.TenantA, strings.TrimPrefix(duty, "/v1/iam/operating-duties/")).Count(&count).Error; err != nil {
					return err
				}
				require.Zero(t, count)
				return nil
			}))
		})
	}
}

func TestIndependentOperatingAuthorityUsesCurrentStaffSourceAndAcknowledgement(t *testing.T) {
	fixture := newOperatingDutyFixture(t)
	call, input, duty := fixture.call, fixture.input, fixture.duty
	call("PUT", duty, "owner", input, 201)
	call("PUT", duty+"/commands/"+uuid.Must(uuid.NewV7()).String(), "reviewer", iam.OperatingDutyDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST source operating authority"}, 201)
	request := authz.OperatingScopeRequest{Principal: fixture.principal, EmployerKind: input.Scope.EmployerKind, EmployerID: input.Scope.EmployerID, LocationID: input.Scope.LocationID, Permission: supply.PermOperatingWrite}
	uses := 0
	use := func(request authz.OperatingScopeRequest) error {
		return fixture.authority.WithOperatingScope(tenantdb.WithActor(fixture.ctx, "user-receiver"), request, func(ctx context.Context, grant authz.OperatingScopeGrant) error {
			uses++
			require.Equal(t, input.Identity.StaffID, grant.Identity.StaffID)
			require.Equal(t, input.Scope.EmployerID, grant.Scope.EmployerID)
			return fixture.db.WithTenantCommand(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				var purpose string
				if err := tx.Raw(`SELECT COALESCE(current_setting('app.iam_operating_access',true),'')`).Scan(&purpose).Error; err != nil {
					return err
				}
				require.Empty(t, purpose)
				var count int64
				if err := tx.Table("iam_operating_duties").Count(&count).Error; err != nil {
					return err
				}
				require.Zero(t, count)
				return nil
			})
		})
	}
	require.ErrorIs(t, use(request), authz.ErrScopedCommandDenied)
	require.Zero(t, uses)
	fixture.projection.enabled.Store(true)
	require.Eventually(t, func() bool { return strings.Contains(call("GET", duty, "owner", nil, 200), `"status":"active"`) }, 20*time.Second, 100*time.Millisecond)
	require.NoError(t, use(request))
	require.Equal(t, 1, uses)
	wrong := request
	wrong.LocationID = uuid.Must(uuid.NewV7())
	require.ErrorIs(t, use(wrong), authz.ErrScopedCommandDenied)
	wrong = request
	wrong.EmployerKind = "pharmacy"
	require.ErrorIs(t, use(wrong), authz.ErrScopedCommandDenied)
	path := "/v1/workforce/suppliers/" + input.Scope.EmployerID.String() + "/staff/" + input.Identity.StaffID.String() + "?directory_context=v1"
	call("PUT", path, "owner", map[string]any{"employee_code": "TEST-RX", "display_name": "TEST Updated Receiver", "job_category": "other", "employment_status": "active", "expected_version": 1, "directory_context_version": "v1"}, 200)
	require.ErrorIs(t, use(request), authz.ErrScopedCommandDenied)
	require.Equal(t, 1, uses)
}

func TestIndependentReceiverUsesReviewedDutyAtSupplierStockAPI(t *testing.T) {
	fixture := newOperatingDutyFixture(t)
	call, input, duty := fixture.call, fixture.input, fixture.duty
	fixture.projection.enabled.Store(true)
	call("PUT", duty, "owner", input, 201)
	call("PUT", duty+"/commands/"+uuid.Must(uuid.NewV7()).String(), "reviewer", iam.OperatingDutyDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST accountable supplier intake"}, 201)
	require.Eventually(t, func() bool { return strings.Contains(call("GET", duty, "owner", nil, 200), `"status":"active"`) }, 20*time.Second, 100*time.Millisecond)
	path := "/v1/supply/parties/" + input.Scope.EmployerID.String() + "/operating-locations/" + input.Scope.LocationID.String() + "/stock/receipts/" + uuid.Must(uuid.NewV7()).String()
	count := supply.WholesaleReceiptInput{ProductVersionID: uuid.New(), LotNumber: "TEST-ROLE-LOT", ExpiresOn: "2030-12-31", UnitCode: "pack", QuantityUnits: 3, SourceReference: "TEST-DUTY-GRN-1", Reason: "TEST observed quarantined source count"}
	recorded := call("PUT", path, "receiver", count, 201)
	require.Contains(t, recorded, `"actor_subject":"user-receiver"`)
	require.Contains(t, recorded, `"stock_status":"quarantine"`)
	require.Contains(t, recorded, `"available_units":0`)
	require.JSONEq(t, recorded, call("GET", path, "receiver", nil, 200))
	require.JSONEq(t, recorded, call("PUT", path, "receiver", count, 200))
	call("PUT", duty+"/commands/"+uuid.Must(uuid.NewV7()).String(), "owner", iam.OperatingDutyDecisionInput{ExpectedVersion: 3, Decision: "revoke", Reason: "TEST immediate source cutoff"}, 201)
	call("GET", path, "receiver", nil, 404)
}

func TestIndependentOperatingCredentialCutoffPreservesStillValidBusinessDuty(t *testing.T) {
	fixture := newOperatingDutyFixture(t)
	call, input, duty := fixture.call, fixture.input, fixture.duty
	fixture.projection.enabled.Store(true)
	call("PUT", duty, "owner", input, 201)
	call("PUT", duty+"/commands/"+uuid.Must(uuid.NewV7()).String(), "reviewer", iam.OperatingDutyDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST credential cutoff"}, 201)
	require.Eventually(t, func() bool { return strings.Contains(call("GET", duty, "owner", nil, 200), `"status":"active"`) }, 20*time.Second, 100*time.Millisecond)
	request := authz.OperatingScopeRequest{Principal: fixture.principal, EmployerKind: input.Scope.EmployerKind, EmployerID: input.Scope.EmployerID, LocationID: input.Scope.LocationID, Permission: supply.PermOperatingWrite}
	uses := 0
	consume := func(request authz.OperatingScopeRequest) error {
		return fixture.authority.WithOperatingScope(tenantdb.WithActor(fixture.ctx, "user-receiver"), request, func(context.Context, authz.OperatingScopeGrant) error { uses++; return nil })
	}
	require.NoError(t, consume(request))
	revokedAt := time.Now().UTC().Truncate(time.Microsecond)
	revocations := iam.NewRevocationChecker(fixture.db)
	require.NoError(t, fixture.db.WithSystem(fixture.ctx, func(tx *gorm.DB) error {
		return revocations.RevokeTx(tx, "user-receiver", revokedAt, "sign_out", "user-receiver")
	}))
	require.ErrorIs(t, consume(request), authz.ErrScopedCredentialRevoked)
	require.Equal(t, 1, uses)
	fresh := request
	fresh.Principal.AuthTime = time.Now().UTC()
	fresh.Principal.IdleDeadline = time.Now().Add(time.Hour)
	require.NoError(t, consume(fresh))
	require.Equal(t, 2, uses)
	require.Contains(t, call("GET", duty, "owner", nil, 200), `"status":"active"`)
}

func TestIndependentReceiverReconcilesStockIdentityAgainstActualCatalogueOwner(t *testing.T) {
	fixture := newOperatingDutyFixture(t)
	call, input, duty := fixture.call, fixture.input, fixture.duty
	fixture.projection.enabled.Store(true)
	publishStockProduct(t, fixture.db, "active")
	reference := call("GET", "/v1/drugmaster/products/by-gtin/8901234567890/stock-reference", "owner", nil, 200)
	var product struct {
		VersionID uuid.UUID `json:"version_id"`
	}
	require.NoError(t, json.Unmarshal([]byte(reference), &product))
	require.NotEqual(t, uuid.Nil, product.VersionID)
	call("PUT", duty, "owner", input, 201)
	call("PUT", duty+"/commands/"+uuid.Must(uuid.NewV7()).String(), "reviewer", iam.OperatingDutyDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST identity reconciliation duty"}, 201)
	require.Eventually(t, func() bool { return strings.Contains(call("GET", duty, "owner", nil, 200), `"status":"active"`) }, 20*time.Second, 100*time.Millisecond)
	path := "/v1/supply/parties/" + input.Scope.EmployerID.String() + "/operating-locations/" + input.Scope.LocationID.String() + "/stock/receipts/" + uuid.Must(uuid.NewV7()).String()
	call("PUT", path, "receiver", supply.WholesaleReceiptInput{ProductVersionID: product.VersionID, LotNumber: "TEST-CATALOGUE-LOT", ExpiresOn: "2030-12-31", UnitCode: "pack", QuantityUnits: 3, SourceReference: "TEST-DUTY-CATALOGUE-1", Reason: "TEST observed factory packs"}, 201)
	check := path + "/identity-checks/" + uuid.Must(uuid.NewV7()).String()
	identity := supply.WholesaleIdentityInput{ExpectedVersion: 0, ProductVersionID: product.VersionID, UnitKind: "pack", ExpectedPackSize: 10, ExpectedBaseUnits: 30, Reason: "TEST independently observed pack identity"}
	recorded := call("PUT", check, "receiver", identity, 201)
	require.Contains(t, recorded, `"quantity_base_units":30`)
	require.JSONEq(t, recorded, call("GET", check, "receiver", nil, 200))
	require.JSONEq(t, recorded, call("PUT", check, "receiver", identity, 200))
	require.Contains(t, call("GET", path, "receiver", nil, 200), `"available_units":0`)
}

func TestIndependentReceiverLooksUpCatalogueOnlyWithinCurrentWarehouseDuty(t *testing.T) {
	fixture := newOperatingDutyFixture(t)
	call, input, duty := fixture.call, fixture.input, fixture.duty
	publishStockProduct(t, fixture.db, "active")
	lookup := "/v1/supply/parties/" + input.Scope.EmployerID.String() + "/operating-locations/" + input.Scope.LocationID.String() + "/stock/products/by-gtin/8901234567890"
	call("GET", lookup, "receiver", nil, 404)
	fixture.projection.enabled.Store(true)
	call("PUT", duty, "owner", input, 201)
	call("PUT", duty+"/commands/"+uuid.Must(uuid.NewV7()).String(), "reviewer", iam.OperatingDutyDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST receiving barcode duty"}, 201)
	require.Eventually(t, func() bool { return strings.Contains(call("GET", duty, "owner", nil, 200), `"status":"active"`) }, 20*time.Second, 100*time.Millisecond)
	result := call("GET", lookup, "receiver", nil, 200)
	require.Contains(t, result, `"brand_name":"TEST Stock"`)
	require.Contains(t, result, `"pack_size":10`)
	call("GET", lookup+"?scope=all", "receiver", nil, 400)
	call("GET", lookup, "foreign", nil, 404)
	call("GET", strings.Replace(lookup, input.Scope.LocationID.String(), uuid.Must(uuid.NewV7()).String(), 1), "receiver", nil, 404)
	call("GET", "/v1/drugmaster/products/by-gtin/8901234567890/stock-reference", "receiver", nil, 403)
	call("GET", strings.Replace(lookup, "8901234567890", "12345678", 1), "receiver", nil, 400)
	call("PUT", duty+"/commands/"+uuid.Must(uuid.NewV7()).String(), "owner", iam.OperatingDutyDecisionInput{ExpectedVersion: 3, Decision: "revoke", Reason: "TEST withdraw receiving duty"}, 201)
	call("GET", lookup, "receiver", nil, 404)
}

func TestIndependentOperatingDutyDirectoryIsBoundedAndEmployerPrivate(t *testing.T) {
	f := newOperatingDutyFixture(t)
	f.call("PUT", f.duty, "owner", f.input, 201)
	list := "/v1/iam/operating-employers/supplier/" + f.input.Scope.EmployerID.String() + "/staff/" + f.input.Identity.StaffID.String() + "/duties"
	first := f.call("GET", list+"?limit=1", "reviewer", nil, 200)
	require.Contains(t, first, strings.TrimPrefix(f.duty, "/v1/iam/operating-duties/"))
	require.Contains(t, first, `"has_more":false`)
	f.call("GET", list, "receiver", nil, 403)
	f.call("GET", list, "foreign", nil, 403)
	f.call("GET", list+"?subject=user-owner", "owner", nil, 400)
	f.call("GET", strings.Replace(list, f.input.Scope.EmployerID.String(), uuid.Must(uuid.NewV7()).String(), 1), "owner", nil, 404)
	f.call("PUT", f.duty+"/commands/"+uuid.Must(uuid.NewV7()).String(), "owner", iam.OperatingDutyDecisionInput{ExpectedVersion: 1, Decision: "withdraw", Reason: "TEST replaced proposal"}, 201)
	secondDuty := "/v1/iam/operating-duties/" + uuid.Must(uuid.NewV7()).String()
	f.call("PUT", secondDuty, "owner", f.input, 201)
	var page struct {
		Data []iam.OperatingDuty `json:"data"`
		Page struct {
			HasMore    bool   `json:"has_more"`
			NextCursor string `json:"next_cursor"`
		} `json:"page"`
	}
	require.NoError(t, json.Unmarshal([]byte(f.call("GET", list+"?limit=1", "owner", nil, 200)), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, strings.TrimPrefix(secondDuty, "/v1/iam/operating-duties/"), page.Data[0].ID.String())
	require.True(t, page.Page.HasMore)
	final := f.call("GET", list+"?limit=1&cursor="+page.Page.NextCursor, "owner", nil, 200)
	require.Contains(t, final, `"status":"withdrawn"`)
	require.Contains(t, final, `"has_more":false`)
	f.call("DELETE", "/v1/workforce/suppliers/"+f.input.Scope.EmployerID.String()+"/access/user-reviewer", "owner", nil, 204)
	f.call("GET", list, "reviewer", nil, 404)
}

func TestCurrentSourceAdministrationPreservesCredentialAndPrivatePurposeBoundaries(t *testing.T) {
	f := newOperatingDutyFixture(t)
	guard := iam.NewSourceAdministrationGuard(f.db, f.projection.Client)
	p := authn.Principal{TenantID: testutil.TenantA, Subject: "user-owner", AuthTime: time.Now().Add(-time.Minute), IdleDeadline: time.Now().Add(time.Hour)}
	calls := 0
	action := func(ctx context.Context) error {
		calls++
		return f.db.WithTenantCommand(ctx, p.TenantID, func(tx *gorm.DB) error {
			var access, command string
			if err := tx.Raw(`SELECT COALESCE(current_setting('app.iam_operating_access',true),''),COALESCE(current_setting('app.iam_operating_command',true),'')`).Row().Scan(&access, &command); err != nil {
				return err
			}
			require.Empty(t, access)
			require.Empty(t, command)
			return nil
		})
	}
	require.NoError(t, guard.WithSourceAdministration(t.Context(), p, iam.PermOperatingRead, action))
	require.Equal(t, 1, calls)
	denied := p
	denied.Subject = "user-receiver"
	require.ErrorIs(t, guard.WithSourceAdministration(t.Context(), denied, iam.PermOperatingRead, action), authz.ErrScopedCommandDenied)
	require.NoError(t, f.db.WithSystem(t.Context(), func(tx *gorm.DB) error {
		return iam.NewRevocationChecker(f.db).RevokeTx(tx, "user-owner", time.Now().UTC(), "sign_out", "user-owner")
	}))
	require.ErrorIs(t, guard.WithSourceAdministration(t.Context(), p, iam.PermOperatingRead, action), authz.ErrScopedCredentialRevoked)
	require.Equal(t, 1, calls)
}

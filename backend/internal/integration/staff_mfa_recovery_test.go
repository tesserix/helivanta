package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/helivanta/internal/modules/workforce"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"github.com/tesserix/onboarding-service/api/customercontract"
	"github.com/tesserix/onboarding-service/api/staffclient"
	"gorm.io/gorm"
)

const (
	staffMFATenantA = "11111111-1111-7111-8111-111111111111"
	staffMFATenantB = "22222222-2222-7222-8222-222222222222"
)

func TestStaffMFAFixtureTenantsReachTheCanonicalOwnerClient(t *testing.T) {
	for _, tenant := range []string{staffMFATenantA, staffMFATenantB} {
		t.Run(tenant, func(t *testing.T) {
			command, link := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
			in := customercontract.StaffMFARecoveryRequest{
				ActorSubject: "synthetic-admin",
				Target:       customercontract.StaffMFARecoveryTarget{TenantID: tenant, EmployerKind: "hospital", EmployerID: uuid.NewString(), StaffID: uuid.NewString(), StaffVersion: 1, LinkID: link, LinkVersion: 2, Subject: "synthetic-staff"},
				Intent:       customercontract.StaffMFAResetInput{CommandID: command, ExpectedLinkID: link, ExpectedLinkVersion: 2, ExpectedStaffVersion: 1, Reason: "Synthetic fixture contract verification"},
			}
			calls := 0
			client, err := staffclient.New(staffclient.Config{Endpoint: "http://onboarding-api.onboarding.svc.cluster.local:8080", ProductKey: "helivanta", Tokens: mfaOwnerToken{}, HTTP: &http.Client{Transport: mfaOwnerTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, "/v1/orgs/"+tenant+"/staff-mfa-recoveries/"+command, r.URL.Path)
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"commandId":"` + command + `","state":"prepared"}`))}, nil
			})}})
			require.NoError(t, err)
			receipt, err := client.PrepareMFA(t.Context(), staffclient.Scope{TenantID: uuid.MustParse(tenant), CommandID: uuid.MustParse(command), ActorSubject: in.ActorSubject}, in)
			require.NoError(t, err)
			require.Equal(t, "prepared", receipt.State)
			require.Equal(t, command, receipt.CommandID)
			require.Equal(t, 1, calls)
		})
	}
}

type mfaNativeIdentity struct{ ineligible atomic.Bool }

func (*mfaNativeIdentity) UserState(context.Context, string, string) (loginclient.UserState, error) {
	return loginclient.UserStateActive, nil
}
func (*mfaNativeIdentity) UserCredentialState(context.Context, string, string) (loginclient.CredentialStatus, error) {
	return loginclient.CredentialStatus{State: loginclient.UserStateActive, MFARecovery: &customercontract.StaffMFARecoveryRequirement{State: "none"}}, nil
}

func (n *mfaNativeIdentity) MembershipEligible(context.Context, string, string) (bool, error) {
	return !n.ineligible.Load(), nil
}

type mfaIAMModule struct {
	*iam.Module
	native *mfaNativeIdentity
}

func (m *mfaIAMModule) StaffMFARecovery(deps platform.Deps) platform.StaffMFARecoveryService {
	m.Module = iam.New(iam.NewRevocationChecker(deps.DB)).SetUserStateChecker(m.native).SetMembershipIdentityReader(m.native)
	return m.Module.StaffMFARecovery(deps)
}

type mfaOwnerToken struct{}

func (mfaOwnerToken) Token(context.Context) (string, error) { return "synthetic-workload", nil }

type mfaOwnerTransport func(*http.Request) (*http.Response, error)

func (f mfaOwnerTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type mfaOwnerFixture struct {
	t                  *testing.T
	mu                 sync.Mutex
	db                 *tenantdb.DB
	held               *customercontract.StaffMFARecoveryRequest
	state              string
	failDispatch       bool
	failPrepare        bool
	dispatches, writes int
	afterPrepare       func()
	native             *mfaNativeIdentity
}

func (o *mfaOwnerFixture) roundTrip(r *http.Request) (*http.Response, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	require.Equal(o.t, "Bearer synthetic-workload", r.Header.Get("Authorization"))
	require.Equal(o.t, "helivanta", r.Header.Get("X-Expected-Product-Key"))
	var output any
	status := 200
	switch {
	case r.Method == http.MethodPut:
		var in customercontract.StaffMFARecoveryRequest
		require.NoError(o.t, json.NewDecoder(r.Body).Decode(&in))
		if o.failPrepare {
			status = 503
			output = map[string]string{"error": "unavailable"}
			break
		}
		if o.held == nil {
			o.held = &in
			o.state = "prepared"
		} else {
			require.Equal(o.t, *o.held, in)
		}
		output = customercontract.StaffMFARecoveryReceipt{CommandID: in.Intent.CommandID, State: o.state}
		if o.afterPrepare != nil {
			o.afterPrepare()
		}
	case r.Method == http.MethodGet:
		if o.held == nil {
			status = 404
			output = map[string]string{"error": "not_found"}
			break
		}
		require.Equal(o.t, o.held.ActorSubject, r.URL.Query().Get("actor_subject"))
		output = customercontract.StaffMFARecoverySnapshot{Request: *o.held, Receipt: customercontract.StaffMFARecoveryReceipt{CommandID: o.held.Intent.CommandID, State: o.state}}
	case strings.HasSuffix(r.URL.Path, "/dispatch"):
		o.dispatches++
		var in customercontract.StaffMFADispatchRequest
		require.NoError(o.t, json.NewDecoder(r.Body).Decode(&in))
		require.NoError(o.t, in.Validate())
		require.Equal(o.t, *o.held, in.Request)
		assertMFAResetAudit(o.t, o.db, in.Request)
		require.NoError(o.t, o.db.WithTenant(r.Context(), in.Request.Target.TenantID, func(tx *gorm.DB) error {
			var rows int64
			require.NoError(o.t, tx.Table("iam_staff_mfa_fences").Where("command_id=?", in.ApplicationFenceID).Count(&rows).Error)
			require.EqualValues(o.t, 1, rows, "the application fence must commit before native dispatch")
			return nil
		}))
		if o.failDispatch {
			status = 503
			output = map[string]string{"error": "unavailable"}
		} else {
			if o.state == "prepared" {
				o.writes++
				o.state = "awaiting_enrollment"
			}
			output = customercontract.StaffMFARecoveryReceipt{CommandID: in.Request.Intent.CommandID, State: o.state}
		}
	case strings.HasSuffix(r.URL.Path, "/cancel"):
		var in customercontract.StaffMFARecoveryRequest
		require.NoError(o.t, json.NewDecoder(r.Body).Decode(&in))
		require.NoError(o.t, in.Validate())
		if o.held == nil {
			o.held = &in
			o.state = "cancelled"
		} else {
			require.Equal(o.t, *o.held, in)
		}
		if o.state != "prepared" && o.state != "cancelled" {
			status = 409
			output = map[string]string{"error": "conflict"}
		} else {
			o.state = "cancelled"
			output = customercontract.StaffMFARecoveryReceipt{CommandID: o.held.Intent.CommandID, State: o.state}
		}
	default:
		o.t.Errorf("unexpected owner request %s %s", r.Method, r.URL.Path)
		status = 500
	}
	raw, err := json.Marshal(output)
	require.NoError(o.t, err)
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
}

func newMFAResetFixture(t *testing.T) (*mfaResetFixture, *mfaOwnerFixture, customercontract.StaffMFAResetInput, string) {
	t.Helper()
	owner := &mfaOwnerFixture{t: t, native: &mfaNativeIdentity{}}
	client, err := staffclient.New(staffclient.Config{Endpoint: "http://onboarding-api.onboarding.svc.cluster.local:8080", ProductKey: "helivanta", Tokens: mfaOwnerToken{}, HTTP: &http.Client{Transport: mfaOwnerTransport(owner.roundTrip)}})
	require.NoError(t, err)
	f := newMFAWorkforceFixture(t, client, owner.native)
	owner.db = f.db
	require.NoError(t, f.db.WithTenant(t.Context(), staffMFATenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'user-manager','tenant_admin'),(?,'user-reviewer','staff_member'),(?,'user-candidate','staff_member')`, staffMFATenantA, staffMFATenantA, staffMFATenantA).Error
	}))
	link := uuid.Must(uuid.NewV7()).String()
	f.call("PUT", f.path+"/"+link, "manager", `{"subject":"user-candidate","reason":"Verified synthetic target"}`, 200)
	f.call("POST", f.path+"/"+link+"/decision", "reviewer", `{"expected_version":1,"decision":"approve","reason":"Independent identity review"}`, 200)
	intent := customercontract.StaffMFAResetInput{CommandID: uuid.Must(uuid.NewV7()).String(), ExpectedLinkID: link, ExpectedLinkVersion: 2, ExpectedStaffVersion: 1, Reason: "Synthetic lost authenticator review"}
	return f, owner, intent, f.base + "/staff/" + f.staff + "/mfa-recoveries/" + intent.CommandID
}

func mfaRequest(t *testing.T, r *gin.Engine, method, path, actor string, input any) *httptest.ResponseRecorder {
	t.Helper()
	var body io.Reader
	if input != nil {
		raw, err := json.Marshal(input)
		require.NoError(t, err)
		body = strings.NewReader(string(raw))
	}
	request := httptest.NewRequest(method, path, body)
	request.Header.Set("Authorization", "Bearer "+actor)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Helivanta-Expected-Tenant", staffMFATenantA)
	request.Header.Set("X-Helivanta-Expected-Subject", "user-"+actor)
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	return response
}

func mfaWatermark(t *testing.T, db *tenantdb.DB) time.Time {
	t.Helper()
	var at time.Time
	require.NoError(t, db.WithTenant(t.Context(), staffMFATenantA, func(tx *gorm.DB) error {
		return tx.Table("iam_credential_revocations").Select("revoked_at").Where("subject='user-candidate'").Scan(&at).Error
	}))
	return at
}

func assertMFAResetAudit(t *testing.T, db *tenantdb.DB, request customercontract.StaffMFARecoveryRequest) {
	t.Helper()
	require.NoError(t, db.WithTenant(t.Context(), request.Target.TenantID, func(tx *gorm.DB) error {
		var retained string
		if err := tx.Raw(`SELECT request::text FROM iam_staff_mfa_commands WHERE tenant_id=? AND command_id=? AND actor_subject=?`, request.Target.TenantID, request.Intent.CommandID, request.ActorSubject).Scan(&retained).Error; err != nil {
			return err
		}
		var intent customercontract.StaffMFARecoveryRequest
		require.NoError(t, json.Unmarshal([]byte(retained), &intent))
		require.Equal(t, request, intent, "the retained reason and scope must match the dispatched command")
		type auditRecord struct {
			Action, ActorSubject, ResourceType, ResourceID string
		}
		var records []auditRecord
		if err := tx.Table("audit_events").Select("action,actor_subject,resource_type,resource_id").
			Where("tenant_id=? AND resource_id=? AND action IN ?", request.Target.TenantID, request.Intent.CommandID, []string{"staff_mfa_intent_retained", "staff_mfa_reset_fenced"}).
			Order("action").Find(&records).Error; err != nil {
			return err
		}
		require.Equal(t, []auditRecord{
			{Action: "staff_mfa_intent_retained", ActorSubject: request.ActorSubject, ResourceType: "iam_staff_mfa_commands", ResourceID: request.Intent.CommandID},
			{Action: "staff_mfa_reset_fenced", ActorSubject: request.ActorSubject, ResourceType: "iam_staff_mfa_fences", ResourceID: request.Intent.CommandID},
		}, records, "each command retains one attributed intent and fence audit before native dispatch")
		return nil
	}))
}

func TestStaffMFAResetFencesBeforeDispatchAndReplaysWithoutNewRevocation(t *testing.T) {
	f, owner, input, path := newMFAResetFixture(t)
	owner.failDispatch = true
	response := mfaRequest(t, f.r, "PUT", path, "manager", input)
	require.Equal(t, 503, response.Code, response.Body.String())
	require.NoError(t, f.db.WithTenant(t.Context(), staffMFATenantA, func(tx *gorm.DB) error {
		var reason string
		if err := tx.Table("iam_credential_revocations").Select("reason").Where("subject='user-candidate'").Scan(&reason).Error; err != nil {
			return err
		}
		require.Equal(t, "admin_revoke", reason)
		return nil
	}))
	f.assertSelectionPrivate("iam_staff_mfa_commands", "iam_staff_mfa_fences")
	cutoff := mfaWatermark(t, f.db)
	require.False(t, cutoff.IsZero())
	require.Equal(t, 1, owner.dispatches)
	require.Zero(t, owner.writes)
	owner.failDispatch = false
	response = mfaRequest(t, f.r, "PUT", path, "manager", input)
	require.Equal(t, 202, response.Code, response.Body.String())
	require.JSONEq(t, `{"commandId":"`+input.CommandID+`","state":"awaiting_enrollment"}`, response.Body.String())
	require.Equal(t, cutoff, mfaWatermark(t, f.db))
	require.Equal(t, 2, owner.dispatches)
	require.Equal(t, 1, owner.writes)
	response = mfaRequest(t, f.r, "PUT", path, "manager", input)
	require.Equal(t, 202, response.Code, response.Body.String())
	require.Equal(t, 2, owner.dispatches)
	require.Equal(t, cutoff, mfaWatermark(t, f.db))
	assertMFAResetAudit(t, f.db, *owner.held)
	response = mfaRequest(t, f.r, "GET", path, "manager", nil)
	require.Equal(t, 200, response.Code, response.Body.String())
	require.NotContains(t, response.Body.String(), "user-candidate")
	for _, statement := range []string{
		`UPDATE iam_staff_mfa_fences SET fingerprint=repeat('0',64) WHERE command_id=?`,
		`DELETE FROM iam_staff_mfa_fences WHERE command_id=?`,
	} {
		err := f.db.WithTenant(t.Context(), staffMFATenantA, func(tx *gorm.DB) error { return tx.Exec(statement, input.CommandID).Error })
		require.Error(t, err, "a committed fence is immutable")
		var pg *pgconn.PgError
		require.True(t, errors.As(err, &pg))
		require.Equal(t, "23514", pg.Code)
	}
	require.NoError(t, f.db.WithTenant(t.Context(), staffMFATenantB, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("iam_staff_mfa_fences").Where("command_id=?", input.CommandID).Count(&count).Error; err != nil {
			return err
		}
		require.Zero(t, count, "fence rows must not cross verified tenants")
		if err := tx.Table("audit_events").Where("resource_id=?", input.CommandID).Count(&count).Error; err != nil {
			return err
		}
		require.Zero(t, count, "recovery audit must not cross verified tenants")
		return nil
	}))
	response = mfaRequest(t, f.r, "POST", path+"/cancel", "manager", map[string]any{})
	require.Equal(t, 409, response.Code, response.Body.String())
	require.Equal(t, 1, owner.writes)
}

func TestStaffMFAPreparedCancellationSurvivesTargetPromotionWithoutDispatch(t *testing.T) {
	f, owner, input, path := newMFAResetFixture(t)
	owner.afterPrepare = func() {
		require.NoError(t, f.db.WithTenant(t.Context(), staffMFATenantA, func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'user-candidate','tenant_admin')`, staffMFATenantA).Error
		}))
	}
	response := mfaRequest(t, f.r, "PUT", path, "manager", input)
	require.Equal(t, 403, response.Code, response.Body.String())
	require.Zero(t, owner.dispatches)
	response = mfaRequest(t, f.r, "POST", path+"/cancel", "manager", map[string]any{})
	require.Equal(t, 200, response.Code, response.Body.String())
	require.JSONEq(t, `{"commandId":"`+input.CommandID+`","state":"cancelled"}`, response.Body.String())
	require.Zero(t, owner.writes)
}

type mfaResetFixture struct {
	t                 *testing.T
	r                 *gin.Engine
	db                *tenantdb.DB
	base, staff, path string
}

func newMFAWorkforceFixture(t *testing.T, owner *staffclient.Client, native *mfaNativeIdentity) *mfaResetFixture {
	t.Helper()
	endpoint, err := url.Parse(testinfra.StartOpenFGA(t))
	require.NoError(t, err)
	proxy := httptest.NewServer(httputil.NewSingleHostReverseProxy(endpoint))
	t.Cleanup(proxy.Close)
	fga, err := authz.NewClient(t.Context(), proxy.URL, "recovery-"+uuid.NewString())
	require.NoError(t, err)
	require.NoError(t, fga.GrantTenantRole(t.Context(), staffMFATenantA, authz.RoleStaffMember))
	require.NoError(t, fga.GrantRole(t.Context(), staffMFATenantA, "user-candidate", authz.RoleStaffMember))
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{StaffAccounts: owner, GovernanceSources: testutil.GovernanceReviewProtocol,
		Tokens: map[string]string{"manager": staffMFATenantA, "reviewer": staffMFATenantA, "candidate": staffMFATenantA, "foreign": staffMFATenantB},
		Perms: map[string][]authz.Permission{
			"manager":  {workforce.PermHospitalManage, workforce.PermEnrollmentManage, workforce.PermLoginLinkManage, workforce.PermLoginLinkReview, workforce.PermIndependentLoginManage, workforce.PermIndependentLoginReview},
			"reviewer": {workforce.PermLoginLinkReview, workforce.PermIndependentLoginReview}, "candidate": {workforce.PermLoginLinkManage}, "foreign": {workforce.PermLoginLinkManage},
		}, Roles: fga, Modules: []platform.Module{&mfaIAMModule{Module: iam.New(nil), native: native}, workforce.New()},
	})
	f := &mfaResetFixture{t: t, r: r, db: db, base: "/v1/workforce/hospitals/" + uuid.NewString(), staff: uuid.NewString()}
	f.call("PUT", f.base, "manager", `{"name":"Synthetic Recovery Hospital"}`, 200)
	for _, actor := range []string{"manager", "reviewer", "candidate"} {
		f.call("PUT", f.base+"/access/user-"+actor, "manager", `{"level":"manager"}`, 200)
	}
	f.call("PUT", f.base+"/staff/"+f.staff, "manager", `{"employee_code":"MFA-01","display_name":"Synthetic Recovery Staff","job_category":"nurse","employment_status":"active"}`, 200)
	f.path = f.base + "/staff/" + f.staff + "/login-links"
	return f
}
func (f *mfaResetFixture) call(method, path, actor, body string, status int) *httptest.ResponseRecorder {
	f.t.Helper()
	var input any
	if body != "" {
		input = json.RawMessage(body)
	}
	response := mfaRequest(f.t, f.r, method, path, actor, input)
	require.Equal(f.t, status, response.Code, response.Body.String())
	return response
}

func TestStaffMFAResetRejectsStaleUnreviewedForeignAndSelfTargetsBeforeOwnerIO(t *testing.T) {
	f, owner, input, path := newMFAResetFixture(t)
	for _, tc := range []struct {
		name, actor, path string
		mutate            func(*customercontract.StaffMFAResetInput)
		status            int
	}{
		{"stale_link", "manager", path, func(v *customercontract.StaffMFAResetInput) { v.ExpectedLinkVersion++ }, 409},
		{"stale_staff", "manager", path, func(v *customercontract.StaffMFAResetInput) { v.ExpectedStaffVersion++ }, 409},
		{"unknown_link", "manager", path, func(v *customercontract.StaffMFAResetInput) { v.ExpectedLinkID = uuid.Must(uuid.NewV7()).String() }, 404},
		{"foreign_staff", "manager", strings.Replace(path, f.staff, uuid.NewString(), 1), func(*customercontract.StaffMFAResetInput) {}, 404},
		{"wrong_employer_kind", "manager", strings.Replace(path, "/hospitals/", "/pharmacies/", 1), func(*customercontract.StaffMFAResetInput) {}, 404},
		{"manager_without_administration", "candidate", path, func(*customercontract.StaffMFAResetInput) {}, 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := input
			tc.mutate(&in)
			response := mfaRequest(t, f.r, "PUT", tc.path, tc.actor, in)
			require.Equal(t, tc.status, response.Code, response.Body.String())
			require.Nil(t, owner.held)
			require.Zero(t, owner.dispatches)
		})
	}
	require.NoError(t, f.db.WithTenant(t.Context(), staffMFATenantB, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'user-foreign','tenant_admin')`, staffMFATenantB).Error
	}))
	raw, err := json.Marshal(input)
	require.NoError(t, err)
	foreignRequest := httptest.NewRequest("PUT", path, strings.NewReader(string(raw)))
	foreignRequest.Header.Set("Authorization", "Bearer foreign")
	foreignRequest.Header.Set("Content-Type", "application/json")
	foreignRequest.Header.Set("X-Helivanta-Expected-Tenant", staffMFATenantB)
	foreignRequest.Header.Set("X-Helivanta-Expected-Subject", "user-foreign")
	foreignResponse := httptest.NewRecorder()
	f.r.ServeHTTP(foreignResponse, foreignRequest)
	require.Equal(t, 404, foreignResponse.Code, foreignResponse.Body.String())
	require.Nil(t, owner.held)
	require.NoError(t, f.db.WithTenant(t.Context(), staffMFATenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'user-candidate','tenant_admin')`, staffMFATenantA).Error
	}))
	response := mfaRequest(t, f.r, "PUT", path, "candidate", input)
	require.Equal(t, 403, response.Code, response.Body.String())
	require.Nil(t, owner.held)
	response = mfaRequest(t, f.r, "PUT", path, "manager", input)
	require.Equal(t, 403, response.Code, response.Body.String())
	require.Nil(t, owner.held)
}

func TestIndependentPharmacyStaffMFARecoveryKeepsTheEmployerScope(t *testing.T) {
	f, owner, _, _ := newMFAResetFixture(t)
	employer := uuid.Must(uuid.NewV7()).String()
	require.NoError(t, f.db.WithTenant(t.Context(), staffMFATenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','user-manager',true),set_config('app.workforce_hospital',?,true)`, employer).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_hospitals(tenant_id,id,name,establishment_kind,pharmacy_ownership_type) VALUES(?,?,'Synthetic Independent Recovery Pharmacy','pharmacy','independent')`, staffMFATenantA, employer).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workforce_hospital_access(tenant_id,hospital_id,subject,level) VALUES(?,?,'user-manager','manager'),(?,?,'user-reviewer','manager')`, staffMFATenantA, employer, staffMFATenantA, employer).Error
	}))
	root := "/v1/workforce/pharmacies/" + employer
	staff := uuid.NewString()
	f.call("PUT", root+"/staff/"+staff+"?directory_context=v1", "manager", `{"employee_code":"MFA-INDEP-01","display_name":"Synthetic Independent Pharmacist","job_category":"pharmacist","employment_status":"active","expected_version":0,"directory_context_version":"v1"}`, 200)
	link := uuid.Must(uuid.NewV7()).String()
	f.call("PUT", root+"/staff/"+staff+"/account-links/"+link, "manager", `{"expected_staff_version":1,"subject":"user-candidate","reason":"Verified independent pharmacy account"}`, 200)
	f.call("PUT", root+"/staff/"+staff+"/account-links/"+link+"/commands/"+uuid.Must(uuid.NewV7()).String(), "reviewer", `{"expected_version":1,"decision":"approve","reason":"Independent employer identity review"}`, 200)
	input := customercontract.StaffMFAResetInput{CommandID: uuid.Must(uuid.NewV7()).String(), ExpectedLinkID: link, ExpectedLinkVersion: 2, ExpectedStaffVersion: 1, Reason: "Independent pharmacist lost authenticator"}
	path := root + "/staff/" + staff + "/mfa-recoveries/" + input.CommandID
	response := mfaRequest(t, f.r, "PUT", path, "manager", input)
	require.Equal(t, 202, response.Code, response.Body.String())
	require.Equal(t, "pharmacy", owner.held.Target.EmployerKind)
	require.Equal(t, employer, owner.held.Target.EmployerID)
	require.Equal(t, staff, owner.held.Target.StaffID)
	require.Equal(t, 1, owner.writes)
	response = mfaRequest(t, f.r, "GET", f.base+"/staff/"+f.staff+"/mfa-recoveries/"+input.CommandID, "manager", nil)
	require.Equal(t, 404, response.Code, response.Body.String())
	require.NotContains(t, response.Body.String(), "user-candidate")
}

func TestStaffMFAResetRechecksNativeAdministratorEligibilityBeforeDispatch(t *testing.T) {
	f, owner, input, path := newMFAResetFixture(t)
	owner.afterPrepare = func() { owner.native.ineligible.Store(true) }
	response := mfaRequest(t, f.r, "PUT", path, "manager", input)
	require.Equal(t, 401, response.Code, response.Body.String())
	require.Zero(t, owner.dispatches)
	require.Zero(t, owner.writes)
}

func TestStaffMFACancellationBeforePreparationBlocksALateReset(t *testing.T) {
	f, owner, input, path := newMFAResetFixture(t)
	response := mfaRequest(t, f.r, "POST", path+"/cancel", "manager", map[string]any{})
	require.Equal(t, 200, response.Code, response.Body.String())
	require.JSONEq(t, `{"commandId":"`+input.CommandID+`","state":"cancelled"}`, response.Body.String())
	require.Nil(t, owner.held, "cancellation before preparation must not invent a native target")
	f.assertSelectionPrivate("iam_staff_mfa_commands", "iam_staff_mfa_cancellations")
	response = mfaRequest(t, f.r, "PUT", path, "manager", input)
	require.Equal(t, 202, response.Code, response.Body.String())
	require.JSONEq(t, `{"commandId":"`+input.CommandID+`","state":"cancelled"}`, response.Body.String())
	require.Nil(t, owner.held)
	require.Zero(t, owner.dispatches)
	response = mfaRequest(t, f.r, "GET", path, "manager", nil)
	require.Equal(t, 200, response.Code, response.Body.String())
	require.JSONEq(t, `{"commandId":"`+input.CommandID+`","state":"cancelled"}`, response.Body.String())
}

func TestStaffMFACancelUnconfirmedPreparationUsesFrozenServerIntent(t *testing.T) {
	f, owner, input, path := newMFAResetFixture(t)
	owner.failPrepare = true
	response := mfaRequest(t, f.r, "PUT", path, "manager", input)
	require.Equal(t, 503, response.Code, response.Body.String())
	require.Nil(t, owner.held)
	response = mfaRequest(t, f.r, "POST", path+"/cancel", "manager", map[string]any{})
	require.Equal(t, 200, response.Code, response.Body.String())
	require.JSONEq(t, `{"commandId":"`+input.CommandID+`","state":"cancelled"}`, response.Body.String())
	require.Equal(t, input, owner.held.Intent, "browser cancellation contains no original reason or link versions")
	require.Zero(t, owner.writes)
	require.Zero(t, owner.dispatches)
	owner.failPrepare = false
	response = mfaRequest(t, f.r, "PUT", path, "manager", input)
	require.Equal(t, 202, response.Code, response.Body.String())
	require.Zero(t, owner.dispatches)
}

func (f *mfaResetFixture) assertSelectionPrivate(tables ...string) {
	f.t.Helper()
	for _, table := range tables {
		var before int64
		require.NoError(f.t, f.db.WithTenant(f.t.Context(), staffMFATenantA, func(tx *gorm.DB) error {
			return tx.Table(table).Count(&before).Error
		}))
		require.Positive(f.t, before, "ordinary reset must have retained private evidence")
		require.NoError(f.t, f.db.WithTenant(f.t.Context(), staffMFATenantA, func(tx *gorm.DB) error {
			require.NoError(f.t, tx.Exec(`SELECT set_config('app.iam_staff_selection','{"scope":"invalid"}',true)`).Error)
			var count int64
			require.NoError(f.t, tx.Table(table).Count(&count).Error)
			require.Zero(f.t, count, "Staff selectors must not disclose MFA recovery evidence")
			updated := tx.Exec("UPDATE " + table + " SET tenant_id=tenant_id")
			require.NoError(f.t, updated.Error)
			require.Zero(f.t, updated.RowsAffected)
			deleted := tx.Exec("DELETE FROM " + table)
			require.NoError(f.t, deleted.Error)
			require.Zero(f.t, deleted.RowsAffected)
			return nil
		}))
	}
}

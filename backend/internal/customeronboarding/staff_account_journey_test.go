package customeronboarding_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/bootstrap"
	"github.com/tesserix/helivanta/internal/customeronboarding"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/ratelimit"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"github.com/tesserix/onboarding-service/api/staffclienttest"
	"github.com/tesserix/onboarding-service/api/staffcontract"
)

const staffAccountTenantA = "11111111-1111-4111-8111-111111111111"
const staffAccountTenantB = "22222222-2222-4222-8222-222222222222"

type staffAccountJourney struct {
	router   *gin.Engine
	db       *tenantdb.DB
	owner    *staffclienttest.Fixture
	ctx      context.Context
	identity *loginclient.Client
}

func newStaffAccountJourney(t *testing.T) *staffAccountJourney {
	t.Helper()
	schema := os.Getenv("STAFF_IDENTITY_OWNER_SCHEMA")
	require.NotEmpty(t, schema, "CI must prepare the hash-checked canonical owner schema")
	owner := staffclienttest.New(t, testinfra.StartStaffIdentityOwnerPostgres(t), schema, uuid.MustParse(staffAccountTenantA), uuid.MustParse(staffAccountTenantB))
	token := t.TempDir() + "/workload"
	require.NoError(t, os.WriteFile(token, []byte(staffclienttest.WorkloadToken), 0600))
	broker := customeronboarding.NewOwnerIntegrationHandlers(token, owner.Router)
	identity := loginclient.New("https://unused-native.test", "TEST-login", http.DefaultClient)
	identity.SetTenantIdentityResolver(broker)
	tokens := map[string]string{"admin": staffAccountTenantA, "other-admin": staffAccountTenantA, "stale-admin": staffAccountTenantA, "restricted": staffAccountTenantA, "foreign-admin": staffAccountTenantB}
	perms := map[string][]authz.Permission{}
	for _, subject := range []string{"admin", "other-admin", "stale-admin", "foreign-admin"} {
		perms[subject] = []authz.Permission{iam.PermMemberManage}
	}
	router, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: tokens, Perms: perms, StaffAccounts: owner.Client, Modules: []platform.Module{iam.New(nil).SetMembershipIdentityReader(identity)}})
	require.NoError(t, db.WithTenant(ctx, staffAccountTenantA, func(tx *gorm.DB) error {
		return tx.Exec("INSERT INTO iam_members(tenant_id,subject,role_key)VALUES(?,'user-admin','tenant_admin'),(?,'user-other-admin','tenant_admin')", staffAccountTenantA, staffAccountTenantA).Error
	}))
	require.NoError(t, db.WithTenant(ctx, staffAccountTenantB, func(tx *gorm.DB) error {
		return tx.Exec("INSERT INTO iam_members(tenant_id,subject,role_key)VALUES(?,'user-foreign-admin','tenant_admin')", staffAccountTenantB).Error
	}))
	bootstrap.MountStaffSetup(router, broker.StaffSetup(customeronboarding.StaffSetupDeps{Limiter: ratelimit.NewMemory(100), Limit: ratelimit.Rule{Rate: 100, Burst: 100, Per: time.Minute}}))
	return &staffAccountJourney{router: router, db: db, owner: owner, ctx: ctx, identity: identity}
}
func (j *staffAccountJourney) call(t *testing.T, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		require.NoError(t, err)
	}
	request := httptest.NewRequest(method, path, bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://helivanta.app")
	request.Header.Set("Authorization", "Bearer "+token)
	tenant := staffAccountTenantA
	if token == "foreign-admin" {
		tenant = staffAccountTenantB
	}
	request.Header.Set("X-Helivanta-Expected-Tenant", tenant)
	request.Header.Set("X-Helivanta-Expected-Subject", "user-"+token)
	response := httptest.NewRecorder()
	j.router.ServeHTTP(response, request)
	return response
}

func TestProtectedStaffAccountJourneyPersistsOnceAndRequiresRecipientBeforeMembership(t *testing.T) {
	j := newStaffAccountJourney(t)
	id := uuid.Must(uuid.NewV7()).String()
	path := "/v1/iam/staff-account-commands/" + id
	contact := map[string]string{"email": "synthetic@example.invalid", "given_name": "Synthetic", "family_name": "Staff", "reason": "Authorized synthetic ordinary account"}
	for _, token := range []string{"restricted", "stale-admin"} {
		denied := j.call(t, "PUT", path, token, contact)
		require.Equal(t, 403, denied.Code, denied.Body.String())
	}
	require.Zero(t, j.owner.Creates.Load())
	first := j.call(t, "PUT", path, "admin", contact)
	require.Equal(t, 200, first.Code, first.Body.String())
	var created staffcontract.StaffIdentityReceipt
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &created))
	require.Equal(t, "created", created.CommandState)
	require.Equal(t, "user-admin", created.ActorSubject)
	require.Equal(t, uuid.MustParse(staffAccountTenantA), created.TenantID)
	require.NotEmpty(t, created.UserID)
	for _, private := range []string{contact["email"], contact["reason"], contact["given_name"]} {
		require.NotContains(t, first.Body.String(), private)
	}
	replay := j.call(t, "PUT", path, "admin", contact)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, first.Body.String(), replay.Body.String())
	require.EqualValues(t, 1, j.owner.Creates.Load())
	changed := map[string]string{"email": contact["email"], "given_name": contact["given_name"], "family_name": contact["family_name"], "reason": "Changed immutable intent"}
	require.Equal(t, 409, j.call(t, "PUT", path, "admin", changed).Code)
	for _, token := range []string{"other-admin", "foreign-admin"} {
		denied := j.call(t, "GET", path, token, nil)
		require.Equal(t, 404, denied.Code, denied.Body.String())
	}
	enrollPath := "/v1/iam/membership-enrollments/" + uuid.Must(uuid.NewV7()).String()
	enroll := map[string]string{"subject": created.UserID, "reason": "Explicit ordinary membership after private credential setup"}
	denied := j.call(t, "PUT", enrollPath, "admin", enroll)
	require.Equal(t, 409, denied.Code, denied.Body.String())
	deliveryPath := path + "/invitations/" + uuid.Must(uuid.NewV7()).String()
	for range 2 {
		sent := j.call(t, "PUT", deliveryPath, "admin", map[string]string{})
		require.Equal(t, 200, sent.Code, sent.Body.String())
		require.Contains(t, sent.Body.String(), "email_action_required")
		require.NotContains(t, sent.Body.String(), j.owner.Invitation().Ticket)
	}
	require.EqualValues(t, 1, j.owner.Mails.Load())
	recipient := func(action string) {
		in := j.owner.Invitation()
		body := map[string]string{"invitation_id": in.InvitationID.String(), "ticket": in.Ticket, "code": in.Code}
		if action == "password" {
			body["password"] = "private-fixture-password"
		}
		accepted := j.call(t, "POST", "/v1/auth/staff-setup/"+action, "admin", body)
		require.Equal(t, 200, accepted.Code, accepted.Body.String())
	}
	recipient("email")
	recipient("email")
	denied = j.call(t, "PUT", enrollPath, "admin", enroll)
	require.Equal(t, 409, denied.Code, denied.Body.String())
	password := j.call(t, "PUT", path+"/invitations/"+uuid.Must(uuid.NewV7()).String(), "admin", map[string]string{})
	require.Equal(t, 200, password.Code, password.Body.String())
	require.Contains(t, password.Body.String(), "password_action_required")
	recipient("password")
	recipient("password")
	presentation, err := j.identity.AssignmentCandidateIdentity(j.ctx, staffAccountTenantA, created.UserID)
	require.NoError(t, err)
	require.NotNil(t, presentation)
	require.Equal(t, "Synthetic Staff", presentation.DisplayName)
	require.Equal(t, "synthetic@example.invalid", presentation.Email)

	var count int64
	require.NoError(t, j.db.WithTenant(j.ctx, staffAccountTenantA, func(tx *gorm.DB) error {
		return tx.Table("iam_members").Where("subject=?", created.UserID).Count(&count).Error
	}))
	require.Zero(t, count, "private credentials must not automatically enroll or grant an account")
	accepted := j.call(t, "PUT", enrollPath, "admin", enroll)
	require.Equal(t, 202, accepted.Code, accepted.Body.String())
	require.Contains(t, accepted.Body.String(), "staff_member")
	replay = j.call(t, "PUT", enrollPath, "admin", enroll)
	require.Equal(t, 202, replay.Code, replay.Body.String())
	require.JSONEq(t, accepted.Body.String(), replay.Body.String())
	foreign := j.call(t, "PUT", "/v1/iam/membership-enrollments/"+uuid.Must(uuid.NewV7()).String(), "foreign-admin", enroll)
	require.Equal(t, 409, foreign.Code, foreign.Body.String())
	observation := j.call(t, "POST", path+"/observations", "admin", map[string]string{})
	require.Equal(t, 200, observation.Code, observation.Body.String())
	var observed staffcontract.StaffIdentityObservationReceipt
	require.NoError(t, json.Unmarshal(observation.Body.Bytes(), &observed))
	require.True(t, observed.Observation.EmailVerified)
	require.Equal(t, "active", observed.Observation.State)
	require.NoError(t, j.db.WithTenant(j.ctx, staffAccountTenantA, func(tx *gorm.DB) error {
		var roles []string
		require.NoError(t, tx.Table("iam_members").Where("subject=?", created.UserID).Pluck("role_key", &roles).Error)
		require.Equal(t, []string{"staff_member"}, roles)
		return tx.Table("audit_events").Where("resource_id=? AND action LIKE 'staff_account_%'", id).Count(&count).Error
	}))
	require.Greater(t, count, int64(5))
	require.EqualValues(t, 1, j.owner.Creates.Load())
	require.EqualValues(t, 2, j.owner.Mails.Load())
}

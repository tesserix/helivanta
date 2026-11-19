package iam

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/onboarding-service/api/customercontract"
)

const admittedTestRecovery = "019f39c2-1738-7998-b5de-6d4214f614e0"

func TestRenewRequiresCurrentAccountRecoveryAdmission(t *testing.T) {
	for _, tc := range []struct {
		name        string
		requirement *customercontract.StaffMFARecoveryRequirement
		generation  string
		status      int
	}{
		{"no_recovery", &customercontract.StaffMFARecoveryRequirement{State: "none"}, "", 200},
		{"unrecognized_admission", &customercontract.StaffMFARecoveryRequirement{State: "none"}, admittedTestRecovery, 401},
		{"pending", &customercontract.StaffMFARecoveryRequirement{State: "blocked", GenerationID: admittedTestRecovery}, "", 401},
		{"pending_matching_id", &customercontract.StaffMFARecoveryRequirement{State: "blocked", GenerationID: admittedTestRecovery}, admittedTestRecovery, 401},
		{"needs_enrollment", &customercontract.StaffMFARecoveryRequirement{State: "enrollment_required", GenerationID: admittedTestRecovery}, admittedTestRecovery, 401},
		{"completed_legacy_cookie", &customercontract.StaffMFARecoveryRequirement{State: "completed", GenerationID: admittedTestRecovery}, "", 401},
		{"completed_stale_cookie", &customercontract.StaffMFARecoveryRequirement{State: "completed", GenerationID: admittedTestRecovery}, "019f39c2-1738-7998-b5de-6d4214f614e1", 401},
		{"completed_matching_cookie", &customercontract.StaffMFARecoveryRequirement{State: "completed", GenerationID: admittedTestRecovery}, admittedTestRecovery, 200},
		{"missing_evidence", nil, "", 503},
		{"malformed_evidence", &customercontract.StaffMFARecoveryRequirement{State: "completed"}, "", 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			roles := stubRoleLister{"user-1": {{TenantID: renewTestTenantA, Role: authz.RoleNurse}}}
			provider := &stubUserState{recovery: map[string]*customercontract.StaffMFARecoveryRequirement{"user-1": tc.requirement}}
			env := newRenewEnv(t, roles, provider, renewTestTTL)
			raw, err := env.signer.Mint("user-1", renewTestTenantA, time.Now().Add(-time.Minute), time.Now().Add(time.Hour), tc.generation)
			require.NoError(t, err)
			response := doRenew(env.r, raw)
			require.Equal(t, tc.status, response.Code, response.Body.String())
			_, issued := renewSessionCookie(response)
			require.Equal(t, tc.status == 200, issued)
		})
	}
}

func TestRenewPreservesRecoveryGeneration(t *testing.T) {
	roles := stubRoleLister{"user-1": {{TenantID: renewTestTenantA, Role: authz.RoleNurse}}}
	env := newRenewEnv(t, roles, &stubUserState{recovery: map[string]*customercontract.StaffMFARecoveryRequirement{"user-1": {State: "completed", GenerationID: admittedTestRecovery}}}, renewTestTTL)
	authenticated := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	deadline := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	raw, err := env.signer.Mint("user-1", renewTestTenantA, authenticated, deadline, admittedTestRecovery)
	require.NoError(t, err)
	response := doRenew(env.r, raw)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	renewed, issued := renewSessionCookie(response)
	require.True(t, issued)
	claims, err := env.verifier.Verify(renewed)
	require.NoError(t, err)
	require.Equal(t, admittedTestRecovery, claims.RecoveryGeneration)
	require.Equal(t, authenticated, claims.AuthTime)
	require.Equal(t, deadline, claims.IdleDeadline)
}

func TestActivityPreservesRecoveryGeneration(t *testing.T) {
	env := newRenewEnv(t, nil, nil, renewTestTTL)
	h := newActivityHandlers(env.signer, renewTestTTL, true, time.Hour)
	r := gin.New()
	r.Use(requestid.Middleware(), authn.Middleware(authn.NewSessionVerifier(env.verifier), renewNeverRevoked{}))
	r.POST("/v1/auth/session/activity", h.activity)
	authenticated := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	raw, err := env.signer.Mint("user-1", renewTestTenantA, authenticated, time.Now().Add(time.Hour), admittedTestRecovery)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/session/activity", nil)
	request.AddCookie(&http.Cookie{Name: authn.SessionCookie, Value: raw})
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	renewed, issued := renewSessionCookie(response)
	require.True(t, issued)
	claims, err := env.verifier.Verify(renewed)
	require.NoError(t, err)
	require.Equal(t, admittedTestRecovery, claims.RecoveryGeneration)
	require.Equal(t, authenticated, claims.AuthTime)
}

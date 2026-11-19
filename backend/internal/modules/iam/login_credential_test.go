package iam_test

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/ratelimit"
	"github.com/tesserix/onboarding-service/api/customercontract"
)

func TestLoginCannotAdmitPasswordOnlyIntoRetainedMFARecovery(t *testing.T) {
	for _, state := range []string{"blocked", "enrollment_required", "completed"} {
		t.Run(state, func(t *testing.T) {
			roles := &fakeRoleListerLogin{bindings: map[string][]authz.RoleBinding{"user-1": {{TenantID: tenantA, Role: authz.RoleNurse}}}}
			provider := &loginCredentialState{observation: loginclient.CredentialStatus{State: loginclient.UserStateActive, MFARecovery: &customercontract.StaffMFARecoveryRequirement{State: state, GenerationID: "019f39c2-1738-7998-b5de-6d4214f614e0"}}}
			r, _ := loginHarnessWithIdentity(t, fakeZitadelVerifier{subject: "user-1", authTime: time.Now()}, roles, nil, ratelimit.Rule{}, provider)
			response := doLogin(t, r, `{"id_token":"good"}`)
			require.Equal(t, http.StatusUnauthorized, response.Code, response.Body.String())
			require.Empty(t, response.Result().Cookies(), "fresh password time alone cannot admit a recovery generation")
		})
	}
}

func TestLoginRefusesAReplayedNativeTokenAfterPasswordRecovery(t *testing.T) {
	authenticated := time.Now().UTC().Add(-2 * time.Minute).Truncate(time.Second)
	for _, tc := range []struct {
		name        string
		observation loginclient.CredentialStatus
		err         error
		status      int
	}{
		{"before_reset", loginclient.CredentialStatus{MFARecovery: &customercontract.StaffMFARecoveryRequirement{State: "none"}, State: loginclient.UserStateActive, PasswordChangedAt: authenticated.Add(time.Minute)}, nil, http.StatusUnauthorized},
		{"exact_boundary", loginclient.CredentialStatus{MFARecovery: &customercontract.StaffMFARecoveryRequirement{State: "none"}, State: loginclient.UserStateActive, PasswordChangedAt: authenticated}, nil, http.StatusUnauthorized},
		{"same_second", loginclient.CredentialStatus{MFARecovery: &customercontract.StaffMFARecoveryRequirement{State: "none"}, State: loginclient.UserStateActive, PasswordChangedAt: authenticated.Add(100 * time.Millisecond)}, nil, http.StatusUnauthorized},
		{"fresh_proof", loginclient.CredentialStatus{MFARecovery: &customercontract.StaffMFARecoveryRequirement{State: "none"}, State: loginclient.UserStateActive, PasswordChangedAt: authenticated.Add(-time.Nanosecond)}, nil, http.StatusOK},
		{"passwordless", loginclient.CredentialStatus{MFARecovery: &customercontract.StaffMFARecoveryRequirement{State: "none"}, State: loginclient.UserStateActive}, nil, http.StatusOK},
		{"inactive", loginclient.CredentialStatus{State: loginclient.UserStateInactive}, nil, http.StatusUnauthorized},
		{"unrecognized", loginclient.CredentialStatus{}, nil, http.StatusServiceUnavailable},
		{"provider_unavailable", loginclient.CredentialStatus{}, errors.New("private source failure"), http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider := &loginCredentialState{observation: tc.observation, err: tc.err}
			roles := &fakeRoleListerLogin{bindings: map[string][]authz.RoleBinding{"user-1": {{TenantID: tenantA, Role: authz.RoleNurse}}}}
			router, verifier := loginHarnessWithIdentity(t, fakeZitadelVerifier{subject: "user-1", authTime: authenticated}, roles, nil, ratelimit.Rule{}, provider)
			response := doLogin(t, router, `{"id_token":"good"}`)
			require.Equal(t, tc.status, response.Code, response.Body.String())
			require.Equal(t, 1, provider.calls)
			require.Equal(t, tenantA, provider.tenant)
			require.Equal(t, "user-1", provider.subject)
			cookies := sessionCookies(response)
			if tc.status != http.StatusOK {
				require.Empty(t, cookies)
				require.NotContains(t, response.Body.String(), "private source failure")
				return
			}
			require.Len(t, cookies, 1)
			claims, err := verifier.Verify(cookies[0].Value)
			require.NoError(t, err)
			require.Equal(t, authenticated, claims.AuthTime)
		})
	}
}

func TestLoginCannotReadCredentialEvidenceForAnInaccessibleTenant(t *testing.T) {
	provider := &loginCredentialState{observation: loginclient.CredentialStatus{MFARecovery: &customercontract.StaffMFARecoveryRequirement{State: "none"}, State: loginclient.UserStateActive}}
	roles := &fakeRoleListerLogin{bindings: map[string][]authz.RoleBinding{"user-1": {{TenantID: tenantA, Role: authz.RoleNurse}}}}
	router, _ := loginHarnessWithIdentity(t, fakeZitadelVerifier{subject: "user-1", authTime: time.Now()}, roles, nil, ratelimit.Rule{}, provider)
	response := doLogin(t, router, `{"id_token":"good","tenant_id":"`+tenantB+`"}`)
	require.Equal(t, http.StatusNotFound, response.Code)
	require.Zero(t, provider.calls)
}

func TestLoginFailsClosedWithoutCredentialEvidence(t *testing.T) {
	roles := &fakeRoleListerLogin{bindings: map[string][]authz.RoleBinding{"user-1": {{TenantID: tenantA, Role: authz.RoleNurse}}}}
	router, _ := loginHarnessWithIdentity(t, fakeZitadelVerifier{subject: "user-1", authTime: time.Now()}, roles, nil, ratelimit.Rule{}, nil)
	response := doLogin(t, router, `{"id_token":"good"}`)
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Empty(t, sessionCookies(response))
}

func TestLoginRechecksMembershipAfterNativeCredentialRead(t *testing.T) {
	roles := &fakeRoleListerLogin{bindings: map[string][]authz.RoleBinding{"user-1": {{TenantID: tenantA, Role: authz.RoleNurse}}}}
	provider := &loginCredentialState{observation: loginclient.CredentialStatus{MFARecovery: &customercontract.StaffMFARecoveryRequirement{State: "none"}, State: loginclient.UserStateActive}, during: func() { roles.bindings = nil }}
	router, _ := loginHarnessWithIdentity(t, fakeZitadelVerifier{subject: "user-1", authTime: time.Now()}, roles, nil, ratelimit.Rule{}, provider)
	response := doLogin(t, router, `{"id_token":"good"}`)
	require.Equal(t, http.StatusNotFound, response.Code)
	require.Empty(t, sessionCookies(response))
	require.Equal(t, 2, roles.calls)
}

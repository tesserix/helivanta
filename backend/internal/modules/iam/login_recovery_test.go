package iam_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/ratelimit"
	"github.com/tesserix/onboarding-service/api/customercontract"
)

type nativeRecoveryAdmission struct {
	calls int
	admit func(context.Context, customercontract.StaffMFAAdmissionInput) (customercontract.StaffMFAAdmissionReceipt, error)
}

func (f *nativeRecoveryAdmission) AdmitStaffMFASession(ctx context.Context, in customercontract.StaffMFAAdmissionInput) (customercontract.StaffMFAAdmissionReceipt, error) {
	f.calls++
	return f.admit(ctx, in)
}

func TestLoginAdmitsOnlyCurrentOwnerVerifiedNativeRecovery(t *testing.T) {
	const generation = "019f39c2-1738-7998-b5de-6d4214f614e0"
	const later = "019f39c2-1738-7998-b5de-6d4214f614e1"
	for _, tc := range []struct {
		name, state, sid string
		status, calls    int
	}{
		{"new_enrollment", "enrollment_required", "verified-native-sid", 200, 1},
		{"later_native_login", "completed", "verified-native-sid", 200, 1},
		{"missing_signed_sid", "completed", "", 401, 0},
		{"password_only", "enrollment_required", "", 401, 0},
		{"pending_reset", "blocked", "verified-native-sid", 401, 0},
		{"provider_refusal", "completed", "verified-native-sid", 401, 1},
		{"provider_outage", "completed", "verified-native-sid", 503, 1},
		{"wrong_receipt", "completed", "verified-native-sid", 503, 1},
		{"reset_during_admission", "completed", "verified-native-sid", 401, 1},
		{"reset_unconfirmed_during_admission", "completed", "verified-native-sid", 401, 1},
		{"membership_removed", "completed", "verified-native-sid", 404, 1},
		{"password_changed", "completed", "verified-native-sid", 401, 1},
		{"credential_source_lost", "completed", "verified-native-sid", 503, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authenticated := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
			roles := &fakeRoleListerLogin{bindings: map[string][]authz.RoleBinding{"user-1": {{TenantID: tenantA, Role: authz.RoleNurse}}}}
			identity := &loginCredentialState{observation: loginclient.CredentialStatus{State: loginclient.UserStateActive, MFARecovery: &customercontract.StaffMFARecoveryRequirement{State: tc.state, GenerationID: generation}}}
			owner := &nativeRecoveryAdmission{admit: func(ctx context.Context, in customercontract.StaffMFAAdmissionInput) (customercontract.StaffMFAAdmissionReceipt, error) {
				require.NoError(t, ctx.Err())
				require.NoError(t, in.Validate())
				require.Equal(t, tenantA, in.Scope.TenantID)
				require.Equal(t, "user-1", in.Scope.Subject)
				require.Equal(t, generation, in.Scope.GenerationID)
				require.Equal(t, tc.sid, in.NativeSessionID)
				identity.observation.MFARecovery = &customercontract.StaffMFARecoveryRequirement{State: "completed", GenerationID: generation}
				switch tc.name {
				case "provider_refusal":
					return customercontract.StaffMFAAdmissionReceipt{}, loginclient.ErrBadCredentials
				case "provider_outage":
					return customercontract.StaffMFAAdmissionReceipt{}, loginclient.ErrUnavailable
				case "wrong_receipt":
					return customercontract.StaffMFAAdmissionReceipt{GenerationID: later}, nil
				case "reset_during_admission":
					identity.observation.MFARecovery.GenerationID = later
				case "reset_unconfirmed_during_admission":
					identity.observation.MFARecovery.State = "blocked"
				case "membership_removed":
					roles.bindings = nil
				case "password_changed":
					identity.observation.PasswordChangedAt = authenticated.Add(time.Second)
				case "credential_source_lost":
					identity.err = loginclient.ErrUnavailable
				}
				return customercontract.StaffMFAAdmissionReceipt{GenerationID: generation}, nil
			}}
			router, verifier := loginHarnessWithIdentity(t, fakeZitadelVerifier{subject: "user-1", authTime: authenticated, nativeSessionID: tc.sid}, roles, nil, ratelimit.Rule{}, identity, owner)
			response := doLogin(t, router, `{"id_token":"good","nativeSessionId":"browser-injected","generationId":"`+later+`"}`)
			require.Equal(t, tc.status, response.Code, response.Body.String())
			require.Equal(t, tc.calls, owner.calls)
			if tc.status != http.StatusOK {
				require.Empty(t, sessionCookies(response))
				return
			}
			claims, err := verifier.Verify(sessionCookie(t, response))
			require.NoError(t, err)
			require.Equal(t, generation, claims.RecoveryGeneration)
			require.Equal(t, authenticated, claims.AuthTime)
			require.Equal(t, 2, identity.calls, "generation must be re-read after owner native I/O")
			require.Equal(t, 2, roles.calls, "membership must be re-read before signing")
			require.NotContains(t, response.Body.String(), tc.sid)
		})
	}
}

func TestLoginFailsClosedWhenRecoveryAdmissionIsNotConfigured(t *testing.T) {
	roles := &fakeRoleListerLogin{bindings: map[string][]authz.RoleBinding{"user-1": {{TenantID: tenantA, Role: authz.RoleNurse}}}}
	identity := &loginCredentialState{observation: loginclient.CredentialStatus{State: loginclient.UserStateActive, MFARecovery: &customercontract.StaffMFARecoveryRequirement{State: "completed", GenerationID: "019f39c2-1738-7998-b5de-6d4214f614e0"}}}
	router, _ := loginHarnessWithIdentity(t, fakeZitadelVerifier{subject: "user-1", authTime: time.Now(), nativeSessionID: "signed-native-session"}, roles, nil, ratelimit.Rule{}, identity)
	response := doLogin(t, router, `{"id_token":"good"}`)
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.Empty(t, sessionCookies(response))
}

func TestOrdinaryLoginDoesNotRequireRecoveryAdmission(t *testing.T) {
	roles := &fakeRoleListerLogin{bindings: map[string][]authz.RoleBinding{"user-1": {{TenantID: tenantA, Role: authz.RoleNurse}}}}
	identity := &loginCredentialState{observation: loginclient.CredentialStatus{State: loginclient.UserStateActive, MFARecovery: &customercontract.StaffMFARecoveryRequirement{State: "none"}}}
	owner := &nativeRecoveryAdmission{admit: func(context.Context, customercontract.StaffMFAAdmissionInput) (customercontract.StaffMFAAdmissionReceipt, error) {
		t.Fatal("ordinary login reached recovery owner")
		return customercontract.StaffMFAAdmissionReceipt{}, nil
	}}
	router, verifier := loginHarnessWithIdentity(t, fakeZitadelVerifier{subject: "user-1", authTime: time.Now()}, roles, nil, ratelimit.Rule{}, identity, owner)
	response := doLogin(t, router, `{"id_token":"good"}`)
	require.Equal(t, http.StatusOK, response.Code)
	claims, err := verifier.Verify(sessionCookie(t, response))
	require.NoError(t, err)
	require.Empty(t, claims.RecoveryGeneration)
	require.Zero(t, owner.calls)
}

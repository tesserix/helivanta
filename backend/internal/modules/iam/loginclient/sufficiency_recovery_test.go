package loginclient

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/onboarding-service/api/customercontract"
)

func managedRecoveryClient(t *testing.T, requirement *customercontract.StaffMFARecoveryRequirement, methods string) (*Client, *atomic.Int32) {
	t.Helper()
	var finalized atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if servePasswordCurrent(w, r) {
			return
		}
		switch {
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v2/sessions/"):
			_, _ = w.Write([]byte(`{"session":{"id":"s1","factors":{"user":{"id":"u1","organizationId":"helivanta-` + identityProjectionTenant + `"},"password":{"verifiedAt":"2026-01-02T00:00:00Z"},"totp":{"verifiedAt":"2026-01-02T00:00:01Z"}}}}`))
		case strings.HasSuffix(r.URL.Path, "/authentication_methods"):
			_, _ = w.Write([]byte(`{"authMethodTypes":` + methods + `}`))
		case r.URL.Path == "/management/v1/policies/login":
			_, _ = w.Write([]byte(nonForceMFAPolicy))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/v2/oidc/auth_requests/"):
			finalized.Add(1)
			_, _ = w.Write([]byte(`{"callbackUrl":"http://localhost:4301/api/auth/callback?code=c&state=s"}`))
		default:
			t.Errorf("unexpected native request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
		}
	})
	client.SetTenantIdentityResolver(identityResolverFunc(func(ctx context.Context, tenant, subject string, purpose IdentityReadPurpose) (IdentitySnapshot, error) {
		require.NoError(t, ctx.Err())
		require.Equal(t, identityProjectionTenant, tenant)
		require.Equal(t, "u1", subject)
		require.Equal(t, IdentityReadCredentials, purpose)
		return IdentitySnapshot{State: UserStateActive, Credentials: &customercontract.IdentityCredentialState{}, MFARecovery: requirement}, nil
	}))
	return client, &finalized
}

func TestRecoveryRequiresNewFactorDespitePermissiveNativePolicy(t *testing.T) {
	client, finalized := managedRecoveryClient(t, &customercontract.StaffMFARecoveryRequirement{State: "enrollment_required", GenerationID: "019f39c2-1738-7998-b5de-6d4214f614e0"}, `["AUTHENTICATION_METHOD_TYPE_PASSWORD"]`)
	result, err := client.CompleteIfSufficient(t.Context(), "V2_test", Session{ID: "s1", Token: "private-session-token"})
	require.NoError(t, err)
	require.Equal(t, OutcomeEnrollmentRequired, result.Outcome)
	require.Equal(t, []string{"totp"}, result.Factors)
	require.Empty(t, result.CallbackURL)
	require.Zero(t, finalized.Load())
}

func TestRecoveryFenceBlocksEveryNativeCompletionEntry(t *testing.T) {
	for _, entry := range []string{"password", "factor", "password_change"} {
		t.Run(entry, func(t *testing.T) {
			client, finalized := managedRecoveryClient(t, &customercontract.StaffMFARecoveryRequirement{State: "blocked", GenerationID: "019f39c2-1738-7998-b5de-6d4214f614e0"}, `["AUTHENTICATION_METHOD_TYPE_PASSWORD","AUTHENTICATION_METHOD_TYPE_TOTP"]`)
			session := Session{ID: "s1", Token: "private-session-token"}
			var result Result
			var err error
			switch entry {
			case "password":
				result, err = client.CompleteIfSufficient(t.Context(), "V2_test", session)
			case "factor":
				result, err = client.CompleteAfterFactor(t.Context(), "V2_test", session)
			case "password_change":
				result, err = client.CompleteAfterPasswordChange(t.Context(), "V2_test", session)
			}
			require.ErrorIs(t, err, ErrBadCredentials)
			require.Empty(t, result.CallbackURL)
			require.Zero(t, finalized.Load())
		})
	}
}

func TestManagedNativeCompletionFailsClosedWithoutOwnerEvidence(t *testing.T) {
	for _, entry := range []string{"password", "factor", "password_change"} {
		t.Run(entry, func(t *testing.T) {
			client, finalized := managedRecoveryClient(t, nil, `["AUTHENTICATION_METHOD_TYPE_PASSWORD","AUTHENTICATION_METHOD_TYPE_TOTP"]`)
			client.SetTenantIdentityResolver(nil)
			session := Session{ID: "s1", Token: "private-session-token"}
			var result Result
			var err error
			switch entry {
			case "password":
				result, err = client.CompleteIfSufficient(t.Context(), "V2_test", session)
			case "factor":
				result, err = client.CompleteAfterFactor(t.Context(), "V2_test", session)
			case "password_change":
				result, err = client.CompleteAfterPasswordChange(t.Context(), "V2_test", session)
			}
			require.ErrorIs(t, err, ErrUnavailable)
			require.Empty(t, result.CallbackURL)
			require.Zero(t, finalized.Load())
		})
	}
}

func TestRecoveredPasswordChangeCannotFinalizeAfterFactorRemoval(t *testing.T) {
	client, finalized := managedRecoveryClient(t, &customercontract.StaffMFARecoveryRequirement{State: "completed", GenerationID: "019f39c2-1738-7998-b5de-6d4214f614e0"}, `["AUTHENTICATION_METHOD_TYPE_PASSWORD"]`)
	result, err := client.CompleteAfterPasswordChange(t.Context(), "V2_test", Session{ID: "s1", Token: "private-session-token"})
	require.NoError(t, err)
	require.Equal(t, OutcomeRefused, result.Outcome)
	require.Equal(t, RefusalFactorNotVerified, result.Reason)
	require.Empty(t, result.CallbackURL)
	require.Zero(t, finalized.Load())
}

func TestManagedRecoveryAllowsOrdinaryAndVerifiedFactorProgression(t *testing.T) {
	for _, state := range []string{"none", "enrollment_required", "completed"} {
		t.Run(state, func(t *testing.T) {
			requirement := &customercontract.StaffMFARecoveryRequirement{State: state}
			if state != "none" {
				requirement.GenerationID = "019f39c2-1738-7998-b5de-6d4214f614e0"
			}
			client, finalized := managedRecoveryClient(t, requirement, `["AUTHENTICATION_METHOD_TYPE_PASSWORD","AUTHENTICATION_METHOD_TYPE_TOTP"]`)
			session := Session{ID: "s1", Token: "private-session-token"}
			result, err := client.CompleteIfSufficient(t.Context(), "V2_test", session)
			require.NoError(t, err)
			require.Equal(t, OutcomeFactorRequired, result.Outcome)
			require.Zero(t, finalized.Load())
			result, err = client.CompleteAfterFactor(t.Context(), "V2_test", session)
			require.NoError(t, err)
			require.Equal(t, OutcomeComplete, result.Outcome)
			require.EqualValues(t, 1, finalized.Load())
			result, err = client.CompleteAfterPasswordChange(t.Context(), "V2_test", session)
			require.NoError(t, err)
			require.Equal(t, OutcomeComplete, result.Outcome)
			require.EqualValues(t, 2, finalized.Load())
		})
	}
	client, finalized := managedRecoveryClient(t, &customercontract.StaffMFARecoveryRequirement{State: "none"}, `["AUTHENTICATION_METHOD_TYPE_PASSWORD"]`)
	result, err := client.CompleteIfSufficient(t.Context(), "V2_test", Session{ID: "s1", Token: "private-session-token"})
	require.NoError(t, err)
	require.Equal(t, OutcomeComplete, result.Outcome)
	require.EqualValues(t, 1, finalized.Load())
}

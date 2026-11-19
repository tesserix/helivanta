package loginclient

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/onboarding-service/api/customercontract"
)

func TestCredentialStatusRequiresExplicitNativeEvidenceWithoutUsingTheLoginToken(t *testing.T) {
	const changed = "2026-11-18T10:11:12.123456Z"
	for _, tc := range []struct {
		name        string
		snapshot    IdentitySnapshot
		unavailable bool
	}{
		{name: "password", snapshot: IdentitySnapshot{State: UserStateActive, Credentials: &customercontract.IdentityCredentialState{PasswordConfigured: true, PasswordChangedAt: changed}, MFARecovery: &customercontract.StaffMFARecoveryRequirement{State: "none"}}},
		{name: "passwordless", snapshot: IdentitySnapshot{State: UserStateActive, Credentials: &customercontract.IdentityCredentialState{}, MFARecovery: &customercontract.StaffMFARecoveryRequirement{State: "none"}}},
		{name: "inactive", snapshot: IdentitySnapshot{State: UserStateInactive}},
		{name: "missing_credential_evidence", snapshot: IdentitySnapshot{State: UserStateActive, MFARecovery: &customercontract.StaffMFARecoveryRequirement{State: "none"}}, unavailable: true},
		{name: "password_without_date", snapshot: IdentitySnapshot{State: UserStateActive, Credentials: &customercontract.IdentityCredentialState{PasswordConfigured: true}, MFARecovery: &customercontract.StaffMFARecoveryRequirement{State: "none"}}, unavailable: true},
		{name: "date_without_password", snapshot: IdentitySnapshot{State: UserStateActive, Credentials: &customercontract.IdentityCredentialState{PasswordChangedAt: changed}, MFARecovery: &customercontract.StaffMFARecoveryRequirement{State: "none"}}, unavailable: true},
		{name: "missing_recovery_evidence", snapshot: IdentitySnapshot{State: UserStateActive, Credentials: &customercontract.IdentityCredentialState{}}, unavailable: true},
		{name: "malformed_recovery_evidence", snapshot: IdentitySnapshot{State: UserStateActive, Credentials: &customercontract.IdentityCredentialState{}, MFARecovery: &customercontract.StaffMFARecoveryRequirement{State: "completed"}}, unavailable: true},
		{name: "pending_recovery", snapshot: IdentitySnapshot{State: UserStateActive, Credentials: &customercontract.IdentityCredentialState{}, MFARecovery: &customercontract.StaffMFARecoveryRequirement{State: "blocked", GenerationID: "019f39c2-1738-7998-b5de-6d4214f614e0"}}},
		{name: "inactive_credential_disclosure", snapshot: IdentitySnapshot{State: UserStateInactive, Credentials: &customercontract.IdentityCredentialState{}}, unavailable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := newTestClient(t, func(http.ResponseWriter, *http.Request) { t.Error("login token used for a management read") })
			client.SetTenantIdentityResolver(identityResolverFunc(func(_ context.Context, tenant, subject string, purpose IdentityReadPurpose) (IdentitySnapshot, error) {
				require.Equal(t, identityProjectionTenant, tenant)
				require.Equal(t, "fresh-user", subject)
				require.Equal(t, IdentityReadCredentials, purpose)
				return tc.snapshot, nil
			}))
			out, err := client.UserCredentialState(t.Context(), identityProjectionTenant, "fresh-user")
			if tc.unavailable {
				require.ErrorIs(t, err, ErrUnavailable)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.snapshot.State, out.State)
			require.Equal(t, tc.snapshot.MFARecovery, out.MFARecovery)
			if tc.name == "password" {
				date, err := time.Parse(time.RFC3339Nano, changed)
				require.NoError(t, err)
				require.Equal(t, date, out.PasswordChangedAt)
			} else {
				require.True(t, out.PasswordChangedAt.IsZero())
			}
		})
	}
}

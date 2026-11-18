package iam

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
)

func TestCredentialFreshnessDoesNotInterpretUnprovenStateAsPasswordless(t *testing.T) {
	for _, tc := range []struct {
		name          string
		observation   loginclient.CredentialStatus
		authenticated time.Time
	}{
		{"unset_authentication", loginclient.CredentialStatus{State: loginclient.UserStateActive}, time.Time{}},
		{"epoch_authentication", loginclient.CredentialStatus{State: loginclient.UserStateActive}, time.Unix(0, 0)},
		{"unknown_state", loginclient.CredentialStatus{}, time.Now()},
		{"future_state", loginclient.CredentialStatus{State: loginclient.UserState(99)}, time.Now()},
		{"inactive_with_change_evidence", loginclient.CredentialStatus{State: loginclient.UserStateInactive, PasswordChangedAt: time.Now()}, time.Now()},
		{"invalid_change_date", loginclient.CredentialStatus{State: loginclient.UserStateActive, PasswordChangedAt: time.Unix(0, 0)}, time.Now()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current, err := credentialIsCurrent(tc.observation, tc.authenticated)
			require.ErrorIs(t, err, loginclient.ErrUnavailable)
			require.False(t, current)
		})
	}
}

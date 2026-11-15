package testutil

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestAccountLinkProtocolNeverProvidesActivationAuthority(t *testing.T) {
	guard, supported := GovernanceReviewProtocol(nil).(authz.GovernanceEmploymentActivationGuard)
	require.True(t, supported, "the explicit test boundary must deny activation rather than simulate an unavailable dependency")
	called := false
	err := guard.WithGovernanceEmploymentActivation(t.Context(), authz.GovernanceSourceRequest{Capability: authz.GovernanceEmploymentActivate}, uuid.New(), func(context.Context, authz.GovernanceSourceEvidence, authz.EmploymentBasisWriter) error {
		called = true
		return nil
	})
	require.ErrorIs(t, err, authz.ErrScopedCommandDenied)
	require.False(t, called, "account-link authority must never fabricate an activation source or basis writer")
}

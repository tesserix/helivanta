package workforce

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestGovernanceOperatingEmployerGuardRequiresCurrentGuardedSource(t *testing.T) {
	module := New()
	provider := module.IndependentStaffIdentities(nil)
	guard, ok := provider.(authz.GovernedIndependentEmployerGuard)
	require.True(t, ok, "native operating source must have an explicit Workforce consumer")
	called := false
	err := guard.WithGovernedIndependentEmployerAccess(context.Background(), authz.GovernedIndependentEmployerRequest{}, func(context.Context) error { called = true; return nil })
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	require.False(t, called)
}

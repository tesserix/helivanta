package pharmacy

import (
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"testing"
)

func TestGovernanceOperatingLocationChoicesRequireGuardedSource(t *testing.T) {
	guard := New().OperatingLocations(nil)
	rows, err := guard.OperatingLocationChoices(t.Context(), authz.GovernedIndependentEmployerRequest{}, pagination.Params{Limit: 20})
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	require.Nil(t, rows)
}

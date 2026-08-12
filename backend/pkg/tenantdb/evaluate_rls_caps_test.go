package tenantdb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// evaluateRLSCaps is unexported and pure, so it's tested directly here
// (package tenantdb, not tenantdb_test) rather than through Open. The
// case that matters most — pg_roles returning zero rows for current_user —
// isn't reachable through a real Postgres connection without a role that
// vanishes mid-probe, so exercising it via a live container would require
// contorting the production code just to create a seam for the test. This
// pure function is the honest way to cover it.
func TestEvaluateRLSCaps(t *testing.T) {
	t.Run("zero rows fails closed even though caps look safe", func(t *testing.T) {
		err := evaluateRLSCaps(false, false, 0)
		require.Error(t, err)
		require.Contains(t, err.Error(), "could not determine app role capabilities")
	})

	t.Run("bypassrls fails", func(t *testing.T) {
		err := evaluateRLSCaps(true, false, 1)
		require.Error(t, err)
		require.Contains(t, err.Error(), "bypass")
	})

	t.Run("superuser fails", func(t *testing.T) {
		err := evaluateRLSCaps(false, true, 1)
		require.Error(t, err)
		require.Contains(t, err.Error(), "bypass")
	})

	t.Run("neither bypass nor super, one row: passes", func(t *testing.T) {
		require.NoError(t, evaluateRLSCaps(false, false, 1))
	})
}

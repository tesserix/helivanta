package testinfra

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestPostgresFunctionTrackingIsExplicitAndClosed(t *testing.T) {
	args, err := postgresFunctionTrackingArgs("")
	require.NoError(t, err)
	require.Empty(t, args)
	args, err = postgresFunctionTrackingArgs("1")
	require.NoError(t, err)
	require.Equal(t, []string{"-c", "track_functions=all"}, args)
	for _, value := range []string{"all", "true", "0", "1 -c other=on", " 1"} {
		args, err := postgresFunctionTrackingArgs(value)
		require.ErrorContains(t, err, "HELIVANTA_TEST_POSTGRES_TRACK_FUNCTIONS")
		require.Nil(t, args)
	}
}

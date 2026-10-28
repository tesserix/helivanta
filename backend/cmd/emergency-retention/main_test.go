package main

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRetentionCommandRefusesUnboundedWorkBeforeOpeningDatabase(t *testing.T) {
	for _, args := range [][]string{{"-batch-size=0"}, {"-batch-size=1001"}, {"-max-batches=0"}, {"-max-batches=101"}, {"unexpected"}} {
		require.Error(t, run(t.Context(), args))
	}
}

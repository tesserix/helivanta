package testinfra

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPostgresAppliesProcessBufferBudget(t *testing.T) {
	budgets := map[string]int64{"": 128 << 20, "16MB": 16 << 20, "32MB": 32 << 20, "64MB": 64 << 20, "128MB": 128 << 20}
	expected, valid := budgets[os.Getenv("HELIVANTA_TEST_POSTGRES_SHARED_BUFFERS")]
	require.True(t, valid, "use one of the documented process-wide PostgreSQL test buffer budgets")
	_, owner, _ := StartPostgres(t)
	var actual int64
	require.NoError(t, openDSN(t, owner).Raw(`SELECT pg_size_bytes(current_setting('shared_buffers'))`).Scan(&actual).Error)
	require.Equal(t, expected, actual, "the running test server must apply the selected memory budget")
}

func TestPostgresBufferBudgetRejectsUnsafeOrAmbiguousSettings(t *testing.T) {
	for _, raw := range []string{"0", "-16MB", "256MB", "16mb", "16MB -c other=on", " 16MB"} {
		t.Run(raw, func(t *testing.T) {
			_, err := parsePostgresBufferBudget(raw)
			require.ErrorContains(t, err, "HELIVANTA_TEST_POSTGRES_SHARED_BUFFERS")
		})
	}
}

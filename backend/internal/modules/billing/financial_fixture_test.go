package billing_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func financialStayTimes(now time.Time) (time.Time, time.Time) {
	local := now.In(ist)
	// Money fixtures exercise explicit charges, independently of the wall-clock census.
	started := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 1, 0, ist)
	return started, started.Add(time.Minute)
}

func TestFinancialStayDoesNotAccrueAnUnrelatedMidnightCharge(t *testing.T) {
	for _, hour := range []int{0, 1, 12, 23} {
		t.Run(time.Date(2026, time.November, 9, hour, 30, 0, 0, ist).Format("15:04"), func(t *testing.T) {
			now := time.Date(2026, time.November, 9, hour, 30, 0, 0, ist)
			started, ordered := financialStayTimes(now)
			require.True(t, started.Before(ordered))
			require.Equal(t, now.In(ist).Format(time.DateOnly), started.In(ist).Format(time.DateOnly))
			require.Equal(t, started.In(ist).Format(time.DateOnly), ordered.In(ist).Format(time.DateOnly))
			midnight := time.Date(started.Year(), started.Month(), started.Day(), 0, 0, 0, 0, ist)
			require.True(t, started.After(midnight), "starting exactly at midnight also accrues a bed day")
		})
	}
}

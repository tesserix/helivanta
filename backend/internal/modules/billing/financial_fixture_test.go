package billing_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func financialStayTimes(now time.Time) (time.Time, time.Time) {
	local := now.In(ist)
	// A completed previous-day occupancy cannot cross the live midnight census grace.
	started := time.Date(local.Year(), local.Month(), local.Day()-1, 12, 0, 0, 0, ist)
	return started, started.Add(time.Minute)
}

func TestFinancialStayDoesNotAccrueAnUnrelatedMidnightCharge(t *testing.T) {
	for _, hour := range []int{0, 1, 12, 23} {
		t.Run(time.Date(2026, time.November, 9, hour, 30, 0, 0, ist).Format("15:04"), func(t *testing.T) {
			now := time.Date(2026, time.November, 9, hour, 30, 0, 0, ist)
			started, ordered := financialStayTimes(now)
			require.True(t, started.Before(ordered))
			require.Equal(t, now.In(ist).AddDate(0, 0, -1).Format(time.DateOnly), started.In(ist).Format(time.DateOnly))
			require.Equal(t, started.In(ist).Format(time.DateOnly), ordered.In(ist).Format(time.DateOnly))
			midnight := time.Date(started.Year(), started.Month(), started.Day(), 0, 0, 0, 0, ist)
			require.True(t, started.After(midnight), "starting exactly at midnight also accrues a bed day")
		})
	}
}

func TestFinancialStayTimesAreAlreadyCompletedAtTheStartOfTheDay(t *testing.T) {
	for _, value := range []string{"2026-11-22T00:00:00+05:30", "2026-11-22T00:00:30+05:30", "2026-11-22T00:05:00+05:30", "2026-11-22T23:59:59+05:30"} {
		t.Run(value, func(t *testing.T) {
			now, err := time.Parse(time.RFC3339, value)
			require.NoError(t, err)
			started, ended := financialStayTimes(now)
			require.True(t, started.Before(ended))
			require.True(t, ended.Before(now), "financial fixture occupancy must be completed before the real current time")
			require.Equal(t, started.In(ist).Format(time.DateOnly), ended.In(ist).Format(time.DateOnly), "a money fixture must not cross a chargeable midnight")
		})
	}
}

func (w *bedWorld) financialStay(admission, patient string) (stay, time.Time) {
	started, ended := financialStayTimes(time.Now())
	bed := stay{uuid.NewString(), uuid.NewString(), "general"}
	require.NoError(w.t, w.start(admission, patient, bed, started))
	require.NoError(w.t, w.end(admission, bed, ended, "release"))
	return bed, ended
}

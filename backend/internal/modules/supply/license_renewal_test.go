package supply

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLicenceRenewalUsesThePremiseCalendarAtMidnight(t *testing.T) {
	instant := time.Date(2026, 10, 18, 18, 45, 0, 0, time.UTC)
	for _, scenario := range []struct {
		zone, day, status string
		days              int
	}{
		{"Asia/Kolkata", "2026-10-19", "expired", -1},
		{"Asia/Karachi", "2026-10-18", "expires_today", 0},
	} {
		t.Run(scenario.zone, func(t *testing.T) {
			got, err := licenceRenewal("2026-01-01", "2026-10-18", scenario.zone, instant)
			require.NoError(t, err)
			require.Equal(t, scenario.status, got.DateStatus)
			require.Equal(t, scenario.day, got.LocalDate)
			require.Equal(t, scenario.days, got.DaysRemaining)
			require.Equal(t, scenario.zone, got.TimeZone)
		})
	}
}

func TestLicenceRenewalThresholdsCountCalendarDaysAcrossDST(t *testing.T) {
	instant := time.Date(2026, 10, 4, 1, 0, 0, 0, time.UTC)
	for _, scenario := range []struct {
		days   int
		status string
	}{
		{-1, "expired"}, {0, "expires_today"}, {1, "renewal_due_30"},
		{30, "renewal_due_30"}, {31, "renewal_due_90"}, {90, "renewal_due_90"},
		{91, "renewal_due_180"}, {180, "renewal_due_180"}, {181, "current"},
	} {
		expiry := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC).AddDate(0, 0, scenario.days).Format(time.DateOnly)
		got, err := licenceRenewal("2026-01-01", expiry, "Australia/Melbourne", instant)
		require.NoError(t, err)
		require.Equal(t, scenario.status, got.DateStatus)
		require.Equal(t, scenario.days, got.DaysRemaining)
	}
	got, err := licenceRenewal("2026-10-05", "2026-10-06", "Australia/Melbourne", instant)
	require.NoError(t, err)
	require.Equal(t, "not_yet_valid", got.DateStatus)
	require.Equal(t, 2, got.DaysRemaining)
	for _, dates := range [][3]string{
		{"2026-01-01", "2026-02-30", "Asia/Kolkata"},
		{"invalid", "2026-10-06", "Asia/Karachi"},
		{"2026-10-06", "2026-10-05", "Asia/Karachi"},
		{"2026-01-01", "2026-10-06", "Unknown/Zone"},
	} {
		_, err := licenceRenewal(dates[0], dates[1], dates[2], instant)
		require.Error(t, err)
	}
}

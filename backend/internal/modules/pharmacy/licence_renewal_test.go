package pharmacy_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external tests of their own owner
)

func TestLicenceRenewalAdviceUsesTheEstablishedCalendarBands(t *testing.T) {
	now := time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC)
	id := uuid.Must(uuid.NewV7())
	for _, tc := range []struct {
		days   int
		status string
	}{{-1, "expired"}, {0, "expires_today"}, {1, "renewal_due_30"}, {30, "renewal_due_30"}, {31, "renewal_due_90"}, {90, "renewal_due_90"}, {91, "renewal_due_180"}, {180, "renewal_due_180"}, {181, "current"}} {
		t.Run(tc.status+now.AddDate(0, 0, tc.days).Format(time.DateOnly), func(t *testing.T) {
			notice, err := pharmacy.LicenceRenewal(id, "2020-01-01", now.AddDate(0, 0, tc.days).Format(time.DateOnly), "Asia/Kolkata", now)
			require.NoError(t, err)
			require.Equal(t, tc.status, notice.DateStatus)
			require.EqualValues(t, tc.days, notice.DaysRemaining)
			require.Equal(t, "2026-11-01", notice.LocalDate)
			require.Equal(t, now.Format(time.RFC3339Nano), notice.ObservedAt)
		})
	}
	future, err := pharmacy.LicenceRenewal(id, "2026-11-02", "2026-11-30", "Asia/Kolkata", now)
	require.NoError(t, err)
	require.Equal(t, "not_yet_valid", future.DateStatus)
	far, err := pharmacy.LicenceRenewal(id, "2020-01-01", "9999-12-31", "Asia/Kolkata", now)
	require.NoError(t, err)
	require.Greater(t, far.DaysRemaining, int64(1_000_000), "calendar arithmetic must not saturate at 292 years")
	_, err = pharmacy.LicenceRenewal(id, "2026-11-30", "2026-11-01", "Asia/Kolkata", now)
	require.Error(t, err)
	_, err = pharmacy.LicenceRenewal(id, "2020-01-01", "2030-01-01", "", now)
	require.Error(t, err)
}

func TestLicenceRenewalAdviceChangesAtOutletMidnight(t *testing.T) {
	id := uuid.Must(uuid.NewV7())
	before := time.Date(2026, 11, 1, 18, 29, 59, 0, time.UTC)
	old, err := pharmacy.LicenceRenewal(id, "2020-01-01", "2026-11-01", "Asia/Kolkata", before)
	require.NoError(t, err)
	require.Equal(t, "expires_today", old.DateStatus)
	next, err := pharmacy.LicenceRenewal(id, "2020-01-01", "2026-11-01", "Asia/Kolkata", before.Add(time.Second))
	require.NoError(t, err)
	require.Equal(t, "expired", next.DateStatus)
	require.EqualValues(t, -1, next.DaysRemaining)
}

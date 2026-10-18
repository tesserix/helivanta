package workforce

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCredentialStandingUsesTheHospitalCalendar(t *testing.T) {
	kolkata, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	auckland, err := time.LoadLocation("Pacific/Auckland")
	require.NoError(t, err)
	// 18:45 UTC on 30 June is already 1 July in Kolkata (00:15) and Auckland (06:45).
	instant := time.Date(2026, 6, 30, 18, 45, 0, 0, time.UTC)
	cases := []struct {
		name, from, until string
		loc               *time.Location
		now               time.Time
		status            string
		days              int
	}{
		{"utc still on the expiry day", "2025-07-01", "2026-06-30", time.UTC, instant, "expires_today", 0},
		{"kolkata already past expiry", "2025-07-01", "2026-06-30", kolkata, instant, "expired", -1},
		{"auckland already past expiry", "2025-07-01", "2026-06-30", auckland, instant, "expired", -1},
		{"valid from the local day", "2026-07-01", "2027-06-30", kolkata, instant, "valid", 364},
		{"not yet valid in utc", "2026-07-01", "2027-06-30", time.UTC, instant, "not_yet_valid", 365},
		{"leap day expiry", "2024-01-01", "2028-02-29", time.UTC, time.Date(2028, 2, 29, 23, 59, 0, 0, time.UTC), "expires_today", 0},
		{"day after leap day", "2024-01-01", "2028-02-29", time.UTC, time.Date(2028, 3, 1, 0, 0, 0, 0, time.UTC), "expired", -1},
	}
	for _, tc := range cases {
		status, days := credentialStanding(tc.from, tc.until, tc.loc, tc.now)
		require.Equal(t, tc.status, status, tc.name)
		require.NotNil(t, days, tc.name)
		require.Equal(t, tc.days, *days, tc.name)
	}
	status, days := credentialStanding("2025-07-01", "2026-06-30", nil, instant)
	require.Equal(t, "date_unknown", status)
	require.Nil(t, days)
}

func TestCredentialUUIDArrayRoundTrips(t *testing.T) {
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	parsed, err := parseUUIDArray(uuidArrayLiteral(ids))
	require.NoError(t, err)
	require.Equal(t, ids, parsed)
	empty, err := parseUUIDArray("{}")
	require.NoError(t, err)
	require.Empty(t, empty)
	for _, bad := range []string{"", "{", "x}", "{not-a-uuid}"} {
		_, err := parseUUIDArray(bad)
		require.Error(t, err, bad)
	}
}

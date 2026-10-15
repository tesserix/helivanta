package workforce

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestShiftInstantsResolveWallClockOrFailClosed(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	kolkata, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	for name, tc := range map[string]struct {
		day, start, end string
		loc             *time.Location
		from, to        string
	}{
		"plain day":               {"2026-10-12", "08:00", "16:00", kolkata, "2026-10-12T02:30:00Z", "2026-10-12T10:30:00Z"},
		"overnight":               {"2026-10-12", "22:00", "06:00", kolkata, "2026-10-12T16:30:00Z", "2026-10-13T00:30:00Z"},
		"equal ends next day":     {"2026-10-12", "08:00", "08:00", kolkata, "2026-10-12T02:30:00Z", "2026-10-13T02:30:00Z"},
		"spans spring forward":    {"2026-03-07", "22:00", "06:00", ny, "2026-03-08T03:00:00Z", "2026-03-08T10:00:00Z"},
		"spans fall back":         {"2026-10-31", "22:00", "06:00", ny, "2026-11-01T02:00:00Z", "2026-11-01T11:00:00Z"},
		"day after spring":        {"2026-03-09", "08:00", "16:00", ny, "2026-03-09T12:00:00Z", "2026-03-09T20:00:00Z"},
		"midnight on fall back":   {"2026-11-01", "00:00", "08:00", ny, "2026-11-01T04:00:00Z", "2026-11-01T13:00:00Z"},
		"ends after the gap hour": {"2026-03-08", "03:00", "11:00", ny, "2026-03-08T07:00:00Z", "2026-03-08T15:00:00Z"},
	} {
		from, to, err := shiftInstants(tc.day, tc.start, tc.end, tc.loc)
		require.NoError(t, err, name)
		require.Equal(t, tc.from, from.UTC().Format(time.RFC3339), name)
		require.Equal(t, tc.to, to.UTC().Format(time.RFC3339), name)
	}
	for name, tc := range map[string][3]string{
		"starts in spring gap": {"2026-03-08", "02:30", "10:00"},
		"ends in spring gap":   {"2026-03-07", "20:00", "02:15"},
		"starts in fall fold":  {"2026-11-01", "01:30", "09:00"},
		"ends in fall fold":    {"2026-10-31", "18:00", "01:00"},
		"bad clock":            {"2026-03-08", "2:30", "10:00"},
		"bad date":             {"2026-02-30", "08:00", "16:00"},
	} {
		_, _, err := shiftInstants(tc[0], tc[1], tc[2], ny)
		require.Error(t, err, name)
	}
}

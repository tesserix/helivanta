package attachments_test

import (
	"io"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/attachments"
)

func versionServer(t *testing.T, reply string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		command := make([]byte, 9)
		if _, err := io.ReadFull(conn, command); err != nil || string(command) != "zVERSION\x00" {
			return
		}
		_, _ = io.WriteString(conn, reply)
	}()
	return listener.Addr().String()
}

func TestClamAVRequiresFreshAttributableDefinitions(t *testing.T) {
	now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, reply string
		wantErr     bool
	}{
		{"fresh", "ClamAV 1.5.1/28000/Wed Oct 14 11:00:00 2026\x00", false},
		{"missed_daily_publish", "ClamAV 1.5.1/28000/Tue Oct 13 06:00:00 2026\x00", false},
		{"exact_age_limit", "ClamAV 1.5.1/28000/Mon Oct 12 12:00:00 2026\x00", false},
		{"stale", "ClamAV 1.5.1/28000/Mon Oct 12 11:59:59 2026\x00", true},
		{"future", "ClamAV 1.5.1/28000/Wed Oct 14 12:05:01 2026\x00", true},
		{"missing_database", "ClamAV 1.5.1\x00", true},
		{"invalid_version", "ClamAV 1.5.1/no-version/Wed Oct 14 11:00:00 2026\x00", true},
		{"zero_version", "ClamAV 1.5.1/0/Wed Oct 14 11:00:00 2026\x00", true},
		{"truncated", "ClamAV 1.5.1/28000/Wed Oct 14 11:00:00 2026", true},
		{"unknown", "UNKNOWN COMMAND\x00", true},
		{"error", "scanner unavailable ERROR\x00", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scanner, err := attachments.NewClamAV(versionServer(t, tc.reply), time.Second, 1024)
			require.NoError(t, err)
			status, err := scanner.Status(t.Context(), now)
			if tc.wantErr {
				require.Error(t, err)
				require.Empty(t, status.EngineVersion)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "1.5.1", status.EngineVersion)
			require.EqualValues(t, 28000, status.DefinitionVersion)
			require.LessOrEqual(t, now.Sub(status.DefinitionsUpdatedAt), attachments.MaxDefinitionAge)
		})
	}
}

func TestClamAVReadinessFailsBeforeScansRefuse(t *testing.T) {
	stamp := func(age time.Duration) string {
		return "ClamAV 1.5.1/28000/" + time.Now().UTC().Add(-age).Format(time.ANSIC) + "\x00"
	}
	for _, tc := range []struct {
		name      string
		age       time.Duration
		ready     bool
		scannable bool
	}{
		{"fresh", time.Hour, true, true},
		{"just_inside_ready", attachments.ReadyDefinitionAge - time.Minute, true, true},
		{"ageing", attachments.ReadyDefinitionAge + time.Minute, false, true},
		{"stale", attachments.MaxDefinitionAge + time.Minute, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scanner, err := attachments.NewClamAV(versionServer(t, stamp(tc.age)), time.Second, 1024)
			require.NoError(t, err)
			require.Equal(t, tc.ready, scanner.Ready(t.Context()) == nil)
			scanner, err = attachments.NewClamAV(versionServer(t, stamp(tc.age)), time.Second, 1024)
			require.NoError(t, err)
			_, err = scanner.Status(t.Context(), time.Now().UTC())
			require.Equal(t, tc.scannable, err == nil)
		})
	}
	scanner, err := attachments.NewClamAV("127.0.0.1:1", time.Second, 1024)
	require.NoError(t, err)
	require.Error(t, scanner.Ready(t.Context()))
}

package attachments_test

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/attachments"
)

func TestClamAVRejectsDefinitionsReloadedDuringScan(t *testing.T) {
	stamp := time.Now().UTC().Add(-time.Hour).Format(time.ANSIC)
	for _, reply := range []string{"stream: OK\x00", "stream: Eicar-Test-Signature FOUND\x00"} {
		t.Run(reply, func(t *testing.T) {
			scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, reply, "ClamAV 1.5.1/28000/"+stamp+"\x00", "ClamAV 1.5.1/28001/"+stamp+"\x00"), time.Second, 1024)
			require.NoError(t, err)
			verdict, err := scanner.Scan(t.Context(), strings.NewReader("synthetic attachment"))
			require.Error(t, err)
			require.False(t, verdict.Clean)
		})
	}
}

func TestClamAVBindsVersionAndStreamToOneServerConnection(t *testing.T) {
	address, connections := testinfra.CountedClamAVProtocol(t, "stream: OK\x00")
	scanner, err := attachments.NewClamAV(address, time.Second, 1024)
	require.NoError(t, err)
	verdict, err := scanner.Scan(t.Context(), strings.NewReader("synthetic attachment"))
	require.NoError(t, err)
	require.True(t, verdict.Clean)
	require.EqualValues(t, 1, connections(), "probes on different connections can attest different scanner replicas")
}

func TestClamAVCannotScanWithStaleOrUnknownDefinitions(t *testing.T) {
	for _, version := range []string{
		"ClamAV 1.5.1/28000/" + time.Now().UTC().Add(-25*time.Hour).Format(time.ANSIC) + "\x00",
		"ClamAV 1.5.1\x00",
	} {
		scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00", version), time.Second, 1024)
		require.NoError(t, err)
		verdict, err := scanner.Scan(t.Context(), strings.NewReader("synthetic attachment"))
		require.Error(t, err)
		require.False(t, verdict.Clean)
	}
}

func TestClamAVOnlyAcceptsExplicitCompleteCleanVerdict(t *testing.T) {
	for _, tc := range []struct {
		name, reply string
		clean       bool
		wantErr     bool
	}{
		{"clean", "stream: OK\x00", true, false},
		{"infected", "stream: Eicar-Test-Signature FOUND\x00", false, false},
		{"error", "stream: scan unavailable ERROR\x00", false, true},
		{"truncated", "stream: OK", false, true},
		{"unknown", "UNKNOWN COMMAND\x00", false, true},
		{"ambiguous", "stream: OK FOUND\x00", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, tc.reply), time.Second, 1024)
			require.NoError(t, err)
			verdict, err := scanner.Scan(context.Background(), strings.NewReader("synthetic attachment"))
			if tc.wantErr {
				require.Error(t, err)
				require.False(t, verdict.Clean)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.clean, verdict.Clean)
			}
		})
	}
}

func TestClamAVRejectsOversizeAndCancelledScans(t *testing.T) {
	scanner, err := attachments.NewClamAV(testinfra.ClamAVProtocol(t, "stream: OK\x00"), time.Second, 4)
	require.NoError(t, err)
	verdict, err := scanner.Scan(context.Background(), strings.NewReader("oversize"))
	require.Error(t, err)
	require.False(t, verdict.Clean)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	verdict, err = scanner.Scan(ctx, strings.NewReader("x"))
	require.Error(t, err)
	require.False(t, verdict.Clean)
}

func TestClamAVRejectsAnUnresponsiveScanner(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = io.Copy(io.Discard, conn)
	}()
	scanner, err := attachments.NewClamAV(listener.Addr().String(), 100*time.Millisecond, 1024)
	require.NoError(t, err)
	verdict, err := scanner.Scan(context.Background(), strings.NewReader("synthetic"))
	require.Error(t, err)
	require.False(t, verdict.Clean)
}

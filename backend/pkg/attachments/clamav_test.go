package attachments_test

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/attachments"
)

func scannerServer(t *testing.T, reply string, versions ...string) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	defaultVersion := "ClamAV 1.5.1/28000/" + time.Now().UTC().Add(-time.Hour).Format(time.ANSIC) + "\x00"
	versionCalls := 0
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			func() {
				defer func() { _ = conn.Close() }()
				_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
				var command []byte
				for {
					var b [1]byte
					if _, err = io.ReadFull(conn, b[:]); err != nil {
						return
					}
					command = append(command, b[0])
					if b[0] == 0 || len(command) > 16 {
						break
					}
				}
				if string(command) == "zVERSION\x00" {
					version := defaultVersion
					if len(versions) > 0 {
						version = versions[min(versionCalls, len(versions)-1)]
					}
					versionCalls++
					_, _ = io.WriteString(conn, version)
					return
				}
				if string(command) != "zINSTREAM\x00" {
					return
				}
				for {
					var size uint32
					if err = binary.Read(conn, binary.BigEndian, &size); err != nil {
						return
					}
					if size == 0 {
						break
					}
					if _, err = io.CopyN(io.Discard, conn, int64(size)); err != nil {
						return
					}
				}
				_, _ = io.WriteString(conn, reply)
			}()
		}
	}()
	return listener.Addr().String()
}

func TestClamAVRejectsDefinitionsReloadedDuringScan(t *testing.T) {
	stamp := time.Now().UTC().Add(-time.Hour).Format(time.ANSIC)
	scanner, err := attachments.NewClamAV(scannerServer(t, "stream: OK\x00", "ClamAV 1.5.1/28000/"+stamp+"\x00", "ClamAV 1.5.1/28001/"+stamp+"\x00"), time.Second, 1024)
	require.NoError(t, err)
	verdict, err := scanner.Scan(t.Context(), strings.NewReader("synthetic attachment"))
	require.Error(t, err)
	require.False(t, verdict.Clean)
}

func TestClamAVCannotScanWithStaleOrUnknownDefinitions(t *testing.T) {
	for _, version := range []string{
		"ClamAV 1.5.1/28000/" + time.Now().UTC().Add(-25*time.Hour).Format(time.ANSIC) + "\x00",
		"ClamAV 1.5.1\x00",
	} {
		scanner, err := attachments.NewClamAV(scannerServer(t, "stream: OK\x00", version), time.Second, 1024)
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
			scanner, err := attachments.NewClamAV(scannerServer(t, tc.reply), time.Second, 1024)
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
	scanner, err := attachments.NewClamAV(scannerServer(t, "stream: OK\x00"), time.Second, 4)
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

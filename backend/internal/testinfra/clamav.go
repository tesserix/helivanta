package testinfra

import (
	"encoding/binary"
	"github.com/stretchr/testify/require"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ClamAVProtocol serves bounded scanner protocol replies without a production daemon.
func ClamAVProtocol(t *testing.T, reply string, versions ...string) string {
	address, _ := CountedClamAVProtocol(t, reply, versions...)
	return address
}

// CountedClamAVProtocol reports connections observed by the vendor endpoint.
func CountedClamAVProtocol(t *testing.T, reply string, versions ...string) (string, func() int64) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	defaultVersion := "ClamAV 1.5.1/28000/" + time.Now().UTC().Add(-time.Hour).Format(time.ANSIC) + "\x00"
	versionCalls := 0
	var connections atomic.Int64
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			connections.Add(1)
			func() {
				defer func() { _ = conn.Close() }()
				_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
				session, id := false, 0
				for {
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
					if string(command) == "zIDSESSION\x00" {
						session = true
						continue
					}
					if string(command) == "zEND\x00" {
						return
					}
					id++
					writeReply := func(response string) {
						if session {
							response = strconv.Itoa(id) + ": " + response
						}
						_, _ = io.WriteString(conn, response)
					}
					if string(command) == "zVERSION\x00" {
						version := defaultVersion
						if len(versions) > 0 {
							version = versions[min(versionCalls, len(versions)-1)]
						}
						versionCalls++
						writeReply(version)
						if !session || !strings.HasSuffix(version, "\x00") {
							return
						}
						continue
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
					writeReply(reply)
					if !session || !strings.HasSuffix(reply, "\x00") {
						return
					}
				}
			}()
		}
	}()
	return listener.Addr().String(), connections.Load
}

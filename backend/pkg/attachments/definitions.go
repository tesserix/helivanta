package attachments

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ScannerStatus is attributable scanner and signature-database provenance.
type ScannerStatus struct {
	EngineVersion        string
	DefinitionVersion    int64
	DefinitionsUpdatedAt time.Time
}

// MaxDefinitionAge tolerates one missed roughly daily upstream publish; older definitions cannot release files.
const MaxDefinitionAge = 48 * time.Hour

// ReadyDefinitionAge fails readiness well before MaxDefinitionAge so the owner is alerted while scans still work.
const ReadyDefinitionAge = 36 * time.Hour

var engineVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

// Status requires a complete VERSION response with definitions at most MaxDefinitionAge old.
func (s *ClamAV) Status(ctx context.Context, now time.Time) (ScannerStatus, error) {
	if now.IsZero() {
		return ScannerStatus{}, errors.New("document scanner time is invalid")
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", s.address)
	if err != nil {
		return ScannerStatus{}, errors.New("document scanner unavailable")
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return ScannerStatus{}, errors.New("document scanner deadline failed")
	}
	if _, err := io.WriteString(conn, "zVERSION\x00"); err != nil {
		return ScannerStatus{}, errors.New("document scanner version request failed")
	}
	reply, err := bufio.NewReader(io.LimitReader(conn, 1025)).ReadString(0)
	if err != nil || len(reply) > 1024 || ctx.Err() != nil {
		return ScannerStatus{}, errors.New("document scanner version incomplete")
	}
	return parseScannerStatus(reply, now)
}

func parseScannerStatus(reply string, now time.Time) (ScannerStatus, error) {
	parts := strings.Split(strings.TrimSuffix(reply, "\x00"), "/")
	if len(parts) != 3 || !strings.HasPrefix(parts[0], "ClamAV ") {
		return ScannerStatus{}, errors.New("document scanner version invalid")
	}
	version := strings.TrimPrefix(parts[0], "ClamAV ")
	number, err := strconv.ParseInt(parts[1], 10, 64)
	if !engineVersion.MatchString(version) || err != nil || number < 1 {
		return ScannerStatus{}, errors.New("document scanner definitions invalid")
	}
	updated, err := time.ParseInLocation(time.ANSIC, parts[2], time.UTC)
	// Unverifiable or stale definitions cannot authorize a clean release.
	if err != nil || now.Sub(updated) > MaxDefinitionAge || updated.Sub(now) > 5*time.Minute {
		return ScannerStatus{}, errors.New("document scanner definitions are not current")
	}
	return ScannerStatus{EngineVersion: version, DefinitionVersion: number, DefinitionsUpdatedAt: updated}, nil
}

// Ready reports the scanner unready once definitions pass ReadyDefinitionAge; workers keep running.
func (s *ClamAV) Ready(ctx context.Context) error {
	now := time.Now().UTC()
	status, err := s.Status(ctx, now)
	if err != nil {
		return err
	}
	if now.Sub(status.DefinitionsUpdatedAt) > ReadyDefinitionAge {
		return errors.New("document scanner definitions are ageing")
	}
	return nil
}

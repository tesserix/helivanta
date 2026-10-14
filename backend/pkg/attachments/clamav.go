package attachments

import (
	"bufio"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

type ScanVerdict struct {
	Clean bool
	ScannerStatus
}

type ClamAV struct {
	address  string
	timeout  time.Duration
	maxBytes int64
}

func NewClamAV(address string, timeout time.Duration, maxBytes int64) (*ClamAV, error) {
	host, port, err := net.SplitHostPort(address)
	n, parseErr := strconv.Atoi(port)
	if err != nil || parseErr != nil || host == "" || n < 1 || n > 65535 || timeout <= 0 || timeout > 5*time.Minute || maxBytes < 1 || maxBytes > 100<<20 {
		return nil, errors.New("invalid document scanner configuration")
	}
	return &ClamAV{address: address, timeout: timeout, maxBytes: maxBytes}, nil
}

// Scan uses INSTREAM without sending filenames or logging scanner responses.
func (s *ClamAV) Scan(ctx context.Context, content io.Reader) (ScanVerdict, error) {
	if content == nil {
		return ScanVerdict{}, errors.New("document content is required")
	}
	status, err := s.Status(ctx, time.Now().UTC())
	if err != nil {
		return ScanVerdict{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", s.address)
	if err != nil {
		return ScanVerdict{}, errors.New("document scanner unavailable")
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	deadline, _ := ctx.Deadline()
	if err = conn.SetDeadline(deadline); err != nil {
		return ScanVerdict{}, errors.New("document scanner deadline failed")
	}
	if _, err = io.WriteString(conn, "zINSTREAM\x00"); err != nil {
		return ScanVerdict{}, errors.New("document scanner request failed")
	}
	r := io.LimitReader(content, s.maxBytes+1)
	buffer := make([]byte, 32<<10)
	var total int64
	var header [4]byte
	for {
		n, readErr := r.Read(buffer)
		total += int64(n)
		if total > s.maxBytes {
			return ScanVerdict{}, errors.New("document exceeds scanner size limit")
		}
		if n > 0 && n <= 32<<10 {
			binary.BigEndian.PutUint32(header[:], uint32(n))
			if _, err = conn.Write(header[:]); err != nil {
				return ScanVerdict{}, errors.New("document scanner stream failed")
			}
			if _, err = conn.Write(buffer[:n]); err != nil {
				return ScanVerdict{}, errors.New("document scanner stream failed")
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return ScanVerdict{}, errors.New("document content could not be read")
		}
		if ctx.Err() != nil {
			return ScanVerdict{}, errors.New("document scan cancelled")
		}
	}
	if _, err = conn.Write([]byte{0, 0, 0, 0}); err != nil {
		return ScanVerdict{}, errors.New("document scanner stream failed")
	}
	reply, err := bufio.NewReader(io.LimitReader(conn, 1025)).ReadString(0)
	if err != nil || len(reply) > 1024 || ctx.Err() != nil {
		return ScanVerdict{}, errors.New("document scanner verdict incomplete")
	}
	message := strings.TrimSuffix(reply, "\x00")
	if message == "stream: OK" {
		current, err := s.Status(ctx, time.Now().UTC())
		// A database reload during the stream cannot establish one scan provenance.
		if err != nil || current != status {
			return ScanVerdict{}, errors.New("document scanner definitions changed during scan")
		}
		return ScanVerdict{Clean: true, ScannerStatus: status}, nil
	}
	if strings.HasPrefix(message, "stream: ") && strings.HasSuffix(message, " FOUND") && message != "stream: OK FOUND" {
		return ScanVerdict{ScannerStatus: status}, nil
	}
	return ScanVerdict{}, errors.New("document scanner verdict invalid")
}

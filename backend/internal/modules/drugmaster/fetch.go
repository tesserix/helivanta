package drugmaster

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

const maxFeedBytes int64 = 256 << 20

func publicFeedIP(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsGlobalUnicast() && !addr.IsPrivate() && !addr.IsLoopback() &&
		!addr.IsLinkLocalUnicast() && !netip.MustParsePrefix("100.64.0.0/10").Contains(addr) &&
		!netip.MustParsePrefix("168.63.129.16/32").Contains(addr)
}

// FetchRelease retrieves a licensed canonical release. It never publishes it.
func FetchRelease(ctx context.Context, endpoint, source, licence, token string) ([]byte, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return nil, errors.New("feed must use a public HTTPS endpoint without credentials, query or fragment")
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil && !publicFeedIP(ip) {
		return nil, errors.New("feed must use a public HTTPS endpoint")
	}
	transport := &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12},
		ResponseHeaderTimeout: 30 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, errors.New("invalid feed address")
			}
			ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
			if err != nil || len(ips) == 0 {
				return nil, errors.New("feed DNS unavailable")
			}
			// Validate every answer and dial the validated IP, avoiding DNS rebinding.
			for _, ip := range ips {
				if !publicFeedIP(ip) {
					return nil, errors.New("feed DNS resolved to a non-public address")
				}
			}
			var dialer net.Dialer
			dialer.Timeout = 10 * time.Second
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
		}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Minute,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return fetchRelease(ctx, client, endpoint, source, licence, token, maxFeedBytes)
}

func fetchRelease(ctx context.Context, client *http.Client, endpoint, source, licence, token string, limit int64) ([]byte, error) {
	if strings.TrimSpace(source) == "" || strings.TrimSpace(licence) == "" {
		return nil, errors.New("configured source and licence are required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, errors.New("invalid feed request")
	}
	req.Header.Set("Accept", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("licensed feed request failed")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("licensed feed returned HTTP %d", response.StatusCode)
	}
	if response.ContentLength > limit {
		return nil, errors.New("licensed feed exceeds byte limit")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, errors.New("licensed feed is incomplete or exceeds byte limit")
	}
	f, err := ParseRelease(raw)
	if err != nil {
		return nil, errors.New("licensed feed is not a valid canonical release")
	}
	if f.Source.Name != source || f.Source.LicenceRef != licence {
		return nil, errors.New("licensed feed provenance does not match configuration")
	}
	return raw, nil
}

// StageOnce returns the existing sealed release when a scheduled fetch is replayed.
// A rejected file stays rejected; changing its decision requires a corrected release.
func StageOnce(ctx context.Context, db *tenantdb.DB, raw []byte, by string) (uuid.UUID, error) {
	id, err := Stage(ctx, db, raw, by)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23505" || pg.ConstraintName != "drugmaster_releases_file_sha256_key" {
		return id, err
	}
	sum := sha256.Sum256(raw)
	var found struct{ ID uuid.UUID }
	err = db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT id FROM drugmaster_releases WHERE file_sha256=?`, hex.EncodeToString(sum[:])).Scan(&found).Error
	})
	if err != nil {
		return uuid.Nil, err
	}
	if found.ID == uuid.Nil {
		return uuid.Nil, errors.New("staged release replay could not be verified")
	}
	return found.ID, nil
}

// Package organizationidentity consumes the onboarding authority's live product-scoped proof.
package organizationidentity

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrUnavailable = errors.New("organisation identity unavailable")
	ErrIneligible  = errors.New("organisation identity ineligible")
	sourceID       = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)
)

// TokenSource supplies only the dedicated product machine's credential.
type TokenSource interface {
	Token(context.Context) (string, error)
}

// Client verifies canonical identity without retaining an eligibility cache.
type Client struct {
	base, product string
	http          http.Client
	tokens        TokenSource
}

// New binds an explicit HTTPS authority and product to a machine token source.
func New(baseURL, productKey string, httpClient *http.Client, tokens TokenSource) (*Client, error) {
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || !sourceID.MatchString(productKey) || httpClient == nil || tokens == nil {
		return nil, ErrUnavailable
	}
	client := *httpClient
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{base: strings.TrimRight(u.String(), "/"), product: productKey, http: client, tokens: tokens}, nil
}

// Verify performs a fresh scoped read and fails closed on missing or unsafe facts.
func (c *Client) Verify(ctx context.Context, tenantID uuid.UUID) (Proof, error) {
	empty := Proof{}
	if tenantID == uuid.Nil || ctx.Err() != nil {
		return empty, ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	started := time.Now().UTC()
	token, err := c.tokens.Token(ctx)
	if err != nil || strings.TrimSpace(token) == "" || ctx.Err() != nil {
		return empty, ErrUnavailable
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/v1/orgs/"+tenantID.String()+"/identity", nil)
	if err != nil {
		return empty, ErrUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Accept", "application/json")
	response, err := c.http.Do(req)
	if err != nil {
		return empty, ErrUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == http.StatusNotFound {
		return empty, ErrIneligible
	}
	if response.StatusCode != http.StatusOK {
		return empty, ErrUnavailable
	}
	const limit = 8 << 10
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(raw) > limit || ctx.Err() != nil {
		return empty, ErrUnavailable
	}
	var proof Proof
	if json.Unmarshal(raw, &proof) != nil || proof.SchemaVersion != 1 || proof.TenantID != tenantID || proof.ProductKey != c.product || !sourceID.MatchString(proof.OrganizationID) || !sourceID.MatchString(proof.ProjectID) || !sourceID.MatchString(proof.GrantID) || proof.VerifiedAt.Before(started.Add(-2*time.Second)) || proof.VerifiedAt.After(time.Now().UTC().Add(2*time.Second)) {
		return empty, ErrUnavailable
	}
	for _, seq := range []string{proof.SourceRevisions.Organization, proof.SourceRevisions.Metadata, proof.SourceRevisions.Project, proof.SourceRevisions.Grant} {
		n, e := strconv.ParseUint(seq, 10, 64)
		if e != nil || n == 0 || strconv.FormatUint(n, 10) != seq {
			return empty, ErrUnavailable
		}
	}
	switch proof.State {
	case "active":
		return proof, nil
	case "inactive":
		return empty, ErrIneligible
	default:
		return empty, ErrUnavailable
	}
}

package organizationidentity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

type tokenFunc func(context.Context) (string, error)

func (f tokenFunc) Token(ctx context.Context) (string, error) { return f(ctx) }

var testTenant = uuid.MustParse("11111111-1111-4111-8111-111111111111")

func serverURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, e := url.Parse(raw)
	if e != nil {
		t.Fatal(e)
	}
	return u
}

func sourceIdentity() map[string]any {
	return map[string]any{"schema_version": 1, "tenant_id": testTenant.String(), "organization_id": "org-1", "project_id": "project-1", "grant_id": "grant-1", "product_key": "hms", "state": "active", "verified_at": time.Now().UTC().Format(time.RFC3339Nano), "source_revisions": map[string]any{"organization": "101", "metadata": "102", "project": "103", "grant": "104"}, "billingEmail": "PRIVATE@example.invalid"}
}
func testTokens() tokenFunc {
	return func(context.Context) (string, error) { return "TEST-machine-token", nil }
}

func TestClientUsesTheScopedMachineAndFreshCanonicalContract(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/orgs/"+testTenant.String()+"/identity" || r.URL.RawQuery != "" {
			t.Error("unexpected identity target")
		}
		if r.Header.Get("Authorization") != "Bearer TEST-machine-token" || r.Header.Get("Cookie") != "" || r.Header.Get("Cache-Control") != "no-cache" {
			t.Error("caller credentials or stale request escaped scope")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(sourceIdentity())
	}))
	defer server.Close()
	httpClient := server.Client()
	httpClient.Jar, _ = cookiejar.New(nil)
	httpClient.Jar.SetCookies(serverURL(t, server.URL), []*http.Cookie{{Name: "human_session", Value: "PRIVATE", Path: "/"}})
	client, err := New(server.URL+"/api", "hms", httpClient, testTokens())
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		proof, e := client.Verify(context.Background(), testTenant)
		if e != nil {
			t.Fatal(e)
		}
		if proof.TenantID != testTenant || proof.ProductKey != "hms" || proof.OrganizationID != "org-1" || proof.ProjectID != "project-1" || proof.GrantID != "grant-1" || proof.Revisions.Metadata != "102" || proof.VerifiedAt.IsZero() {
			t.Fatal("canonical source provenance lost")
		}
		raw, e := json.Marshal(proof)
		if e != nil || strings.Contains(string(raw), "PRIVATE") || strings.Contains(string(raw), "billingEmail") {
			t.Fatal("private source fields retained in identity proof")
		}
	}
	if calls.Load() != 2 {
		t.Fatal("eligibility observation was cached")
	}
}

func TestClientRejectsUntrustedOrStaleIdentity(t *testing.T) {
	cases := []struct {
		name   string
		change func(map[string]any)
	}{
		{"tenant", func(m map[string]any) { m["tenant_id"] = uuid.NewString() }},
		{"product", func(m map[string]any) { m["product_key"] = "other" }},
		{"schema", func(m map[string]any) { m["schema_version"] = 2 }},
		{"unknown state", func(m map[string]any) { m["state"] = "unknown" }},
		{"missing org", func(m map[string]any) { delete(m, "organization_id") }},
		{"unsafe source ref", func(m map[string]any) { m["grant_id"] = "../other" }},
		{"missing provenance", func(m map[string]any) { m["source_revisions"] = map[string]string{"organization": "101"} }},
		{"rounded sequence", func(m map[string]any) { m["source_revisions"].(map[string]any)["grant"] = float64(104) }},
		{"zero sequence", func(m map[string]any) { m["source_revisions"].(map[string]any)["metadata"] = "0" }},
		{"padded sequence", func(m map[string]any) { m["source_revisions"].(map[string]any)["metadata"] = "0102" }},
		{"overflowing sequence", func(m map[string]any) { m["source_revisions"].(map[string]any)["metadata"] = "18446744073709551616" }},
		{"stale", func(m map[string]any) { m["verified_at"] = time.Now().Add(-time.Hour).Format(time.RFC3339Nano) }},
		{"future", func(m map[string]any) { m["verified_at"] = time.Now().Add(time.Hour).Format(time.RFC3339Nano) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				m := sourceIdentity()
				tc.change(m)
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(m)
			}))
			defer server.Close()
			c, e := New(server.URL, "hms", server.Client(), testTokens())
			if e != nil {
				t.Fatal(e)
			}
			proof, e := c.Verify(context.Background(), testTenant)
			if !errors.Is(e, ErrUnavailable) || proof.TenantID != uuid.Nil {
				t.Fatalf("untrusted observation established identity: %+v %v", proof, e)
			}
		})
	}
}

func TestClientBoundsAnUnresponsiveAuthority(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		<-r.Context().Done()
	}))
	defer server.Close()
	c, err := New(server.URL, "hms", server.Client(), testTokens())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, e := c.Verify(ctx, testTenant); result <- e }()
	select {
	case err := <-result:
		if !errors.Is(err, ErrUnavailable) || calls.Load() != 1 {
			t.Fatal("unresponsive identity authority established a proof or retried")
		}
	case <-time.After(4 * time.Second):
		cancel()
		t.Fatal("identity lookup exceeded its own bounded deadline")
	}
}

func TestClientRejectsMissingConfigurationAndMalformedSuccess(t *testing.T) {
	for _, config := range []struct {
		product string
		client  *http.Client
		tokens  TokenSource
	}{{"", http.DefaultClient, testTokens()}, {"hms", nil, testTokens()}, {"hms", http.DefaultClient, nil}} {
		if _, err := New("https://onboard.tesserix.app", config.product, config.client, config.tokens); !errors.Is(err, ErrUnavailable) {
			t.Fatal("missing source configuration accepted")
		}
	}
	for _, raw := range []string{`not-json`, `null`, `{}`, `{"tenant_id":"bad-uuid"}`, `{"schema_version":1} {"state":"active"}`} {
		t.Run(raw, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(raw)) }))
			defer server.Close()
			c, err := New(server.URL, "hms", server.Client(), testTokens())
			if err != nil {
				t.Fatal(err)
			}
			proof, err := c.Verify(context.Background(), testTenant)
			if !errors.Is(err, ErrUnavailable) || proof.TenantID != uuid.Nil {
				t.Fatal("malformed source established identity")
			}
		})
	}
}

func TestClientDeniesInactiveAndUnavailableSourcesWithoutFallback(t *testing.T) {
	for _, code := range []int{200, 401, 403, 404, 429, 500, 503} {
		t.Run(http.StatusText(code), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(code)
				m := sourceIdentity()
				if code == 200 {
					m["state"] = "inactive"
				}
				_ = json.NewEncoder(w).Encode(m)
			}))
			defer server.Close()
			c, e := New(server.URL, "hms", server.Client(), testTokens())
			if e != nil {
				t.Fatal(e)
			}
			proof, e := c.Verify(context.Background(), testTenant)
			expected := ErrUnavailable
			if code == 200 || code == 404 {
				expected = ErrIneligible
			}
			if !errors.Is(e, expected) || proof.TenantID != uuid.Nil {
				t.Fatalf("source failure established identity: %+v %v", proof, e)
			}
		})
	}
}

func TestClientDoesNotFollowCredentialRedirectsOrUnboundedBodies(t *testing.T) {
	var leaked atomic.Bool
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Store(true) }))
	defer target.Close()
	source := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	c, e := New(source.URL, "hms", source.Client(), testTokens())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Verify(context.Background(), testTenant); !errors.Is(e, ErrUnavailable) || leaked.Load() {
		t.Fatal("identity credential followed redirect")
	}
	large := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(make([]byte, 16<<10))
	}))
	defer large.Close()
	c, e = New(large.URL, "hms", large.Client(), testTokens())
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Verify(context.Background(), testTenant); !errors.Is(e, ErrUnavailable) {
		t.Fatal("unbounded identity body accepted")
	}
}

func TestClientRequiresAnExplicitSecureSourceAndRespectsCancellation(t *testing.T) {
	for _, base := range []string{"", "http://onboard.tesserix.app", "https://user:PRIVATE@onboard.tesserix.app", "https://onboard.tesserix.app?token=PRIVATE", "https://onboard.tesserix.app#PRIVATE"} {
		if _, e := New(base, "hms", http.DefaultClient, testTokens()); e == nil {
			t.Fatal("unsafe identity authority accepted")
		}
	}
	var calls atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer server.Close()
	c, e := New(server.URL, "hms", server.Client(), tokenFunc(func(ctx context.Context) (string, error) { return "", errors.New("PRIVATE_SOURCE_TOKEN") }))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = c.Verify(context.Background(), testTenant); !errors.Is(e, ErrUnavailable) || e.Error() != "organisation identity unavailable" || calls.Load() != 0 {
		t.Fatal("source credential failure leaked or made a request")
	}
	c, e = New(server.URL, "hms", server.Client(), testTokens())
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e = c.Verify(ctx, testTenant); !errors.Is(e, ErrUnavailable) || calls.Load() != 0 {
		t.Fatal("cancelled identity request reached source")
	}
	if _, e = c.Verify(context.Background(), uuid.Nil); !errors.Is(e, ErrUnavailable) || calls.Load() != 0 {
		t.Fatal("empty tenant reached source")
	}
}

package globalapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRoutesProductAndPreservesContract(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, "/v1/pharmacy/outlets?limit=2", r.URL.RequestURI())
		require.Equal(t, "Bearer synthetic", r.Header.Get("Authorization"))
		require.Empty(t, r.Header.Get("X-Tenant-ID"))
		require.Equal(t, "192.0.2.1", r.Header.Get("X-Forwarded-For"))
		require.Equal(t, "tenant-precondition", r.Header.Get("X-Helivanta-Expected-Tenant"))
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Retry-After", "3")
		w.WriteHeader(http.StatusConflict)
		_, err := w.Write([]byte("owner conflict"))
		require.NoError(t, err)
	}))
	t.Cleanup(upstream.Close)
	handler, err := New(Config{Upstreams: map[string]string{"pharmacy": upstream.URL}, Development: true, Timeout: time.Second})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/v1/pharmacy/outlets?limit=2", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer synthetic")
	req.Header.Set("X-Tenant-ID", "forged")
	req.Header.Set("X-Forwarded-For", "forged")
	req.Header.Set("X-Helivanta-Expected-Tenant", "tenant-precondition")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	require.Equal(t, http.StatusConflict, res.Code)
	require.Equal(t, "owner conflict", res.Body.String())
	require.Equal(t, "text/csv", res.Header().Get("Content-Type"))
	require.Equal(t, "3", res.Header().Get("Retry-After"))
	require.Equal(t, "no-store", res.Header().Get("Cache-Control"))
	require.Contains(t, res.Header().Get("Via"), "helivanta-global-api")
	require.Equal(t, 1, calls)
}

func TestUnconfiguredAndAmbiguousRoutesNeverReachUpstream(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(200) }))
	t.Cleanup(upstream.Close)
	handler, err := New(Config{Upstreams: map[string]string{"pharmacy": upstream.URL}, Development: true, Timeout: time.Second})
	require.NoError(t, err)
	for _, path := range []string{"/v1/mediconnect/records", "/v1/pharmacyevil", "/v1/pharmacy/../iam", "/v1/pharmacy/%2e%2e/iam", "/v1/pharmacy//outlets", "/v1/pharmacy/%2fiam", "/metrics"} {
		t.Run(path, func(t *testing.T) {
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
			require.Contains(t, []int{400, 404, 503}, res.Code)
		})
	}
	require.Zero(t, calls)
}

func TestRejectsUnsafeUpstreams(t *testing.T) {
	for _, origin := range []string{"", "http://api.internal", "https://user:secret@api.internal", "https://api.internal/v1", "https://api.internal?tenant=a", "https://api.internal#x"} {
		_, err := New(Config{Upstreams: map[string]string{"pharmacy": origin}, Timeout: time.Second})
		require.Error(t, err, origin)
	}
	_, err := New(Config{Upstreams: map[string]string{"unknown": "https://api.internal"}, Timeout: time.Second})
	require.Error(t, err)
}

func TestUnavailableOwnerDoesNotRetryOrExposeAddress(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	upstream.Close()
	handler, err := New(Config{Upstreams: map[string]string{"pharmacy": upstream.URL}, Development: true, Timeout: time.Second})
	require.NoError(t, err)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/v1/pharmacy/outlets", strings.NewReader(`{}`)))
	require.Equal(t, 502, res.Code)
	require.NotContains(t, res.Body.String(), upstream.URL)
	require.Contains(t, res.Body.String(), "upstream_unavailable")
}

func TestRequestBodyLimitBeforeForwarding(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, err := io.Copy(io.Discard, r.Body)
		require.NoError(t, err)
		w.WriteHeader(200)
	}))
	t.Cleanup(upstream.Close)
	handler, err := New(Config{Upstreams: map[string]string{"pharmacy": upstream.URL}, Development: true, Timeout: time.Second, MaxBodyBytes: 4})
	require.NoError(t, err)
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/v1/pharmacy/outlets", strings.NewReader("12345")))
	require.Equal(t, 413, res.Code)
	require.Zero(t, calls)
}

func TestTimeoutCancelsOwnerRequest(t *testing.T) {
	cancelled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done(); close(cancelled) }))
	t.Cleanup(upstream.Close)
	gateway, err := New(Config{Development: true, Timeout: 100 * time.Millisecond, Upstreams: map[string]string{"pharmacy": upstream.URL}})
	require.NoError(t, err)
	res := httptest.NewRecorder()
	gateway.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/v1/pharmacy/outlets", nil))
	require.Equal(t, 504, res.Code)
	select {
	case <-cancelled:
	case <-time.After(time.Second):
		t.Fatal("owner request was not cancelled")
	}
}

func TestCrossSiteMutationIsNotForwarded(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(200) }))
	t.Cleanup(upstream.Close)
	gateway, err := New(Config{Development: true, Timeout: time.Second, Upstreams: map[string]string{"pharmacy": upstream.URL}})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/v1/pharmacy/outlets", nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	res := httptest.NewRecorder()
	gateway.ServeHTTP(res, req)
	require.Equal(t, 403, res.Code)
	require.Zero(t, calls)
}

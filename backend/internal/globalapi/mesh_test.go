package globalapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestMeshTransportOnlyAcceptsServiceDNS(t *testing.T) {
	for _, origin := range []string{"http://helivanta-api.helivanta.svc.cluster.local:8080", "http://example.com", "http://169.254.169.254", "http://api.svc.cluster.local.evil.test"} {
		_, err := New(Config{Upstreams: map[string]string{"platform": origin}, Timeout: time.Second, TransportSecurity: "istio"})
		if origin == "http://helivanta-api.helivanta.svc.cluster.local:8080" {
			require.NoError(t, err)
		} else {
			require.Error(t, err)
		}
	}
	_, err := New(Config{Upstreams: map[string]string{"platform": "https://api.example"}, Timeout: time.Second, TransportSecurity: "insecure"})
	require.Error(t, err)
}

func TestInternalEnrolmentUsesPlatformAndRejectsOtherInternalPaths(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, "/internal/v1/hospital-enrolments", r.URL.Path)
		require.Equal(t, "Bearer workload-token", r.Header.Get("Authorization"))
		w.WriteHeader(401)
	}))
	t.Cleanup(upstream.Close)
	gateway, err := New(Config{Development: true, Timeout: time.Second, Upstreams: map[string]string{"platform": upstream.URL}})
	require.NoError(t, err)
	for _, p := range []string{"/internal/v1/hospital-enrolments", "/internal/v1/unknown"} {
		r := httptest.NewRequest(http.MethodPost, p, nil)
		r.Header.Set("Authorization", "Bearer workload-token")
		w := httptest.NewRecorder()
		gateway.ServeHTTP(w, r)
		if p == "/internal/v1/hospital-enrolments" {
			require.Equal(t, 401, w.Code)
		} else {
			require.Equal(t, 404, w.Code)
		}
	}
	require.Equal(t, 1, calls)
}

func TestReadinessRequiresPlatformButLivenessDoesNot(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/readyz", r.URL.Path)
		w.WriteHeader(503)
	}))
	t.Cleanup(upstream.Close)
	gateway, err := New(Config{Development: true, Timeout: time.Second, Upstreams: map[string]string{"platform": upstream.URL}})
	require.NoError(t, err)
	for _, row := range []struct {
		path   string
		status int
	}{{"/healthz", 200}, {"/readyz", 503}} {
		w := httptest.NewRecorder()
		gateway.ServeHTTP(w, httptest.NewRequest(http.MethodGet, row.path, nil))
		require.Equal(t, row.status, w.Code)
	}
}

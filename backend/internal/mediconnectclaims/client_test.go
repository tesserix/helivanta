package mediconnectclaims_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/mediconnectclaims"
	mediconnectcontract "github.com/tesserix/helivanta/internal/modules/mediconnect/contract"
)

func TestOwnerClientReloadsProjectedCredentialAndPreservesFalseObservation(t *testing.T) {
	tokenFile := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(tokenFile, []byte("first-token"), 0600))
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, mediconnectcontract.CurrentClaimPath, r.URL.Path)
		require.Empty(t, r.Header.Get("Authorization"))
		require.Empty(t, r.Header.Get("Cookie"))
		call := calls.Add(1)
		if call == 1 {
			require.Equal(t, "Bearer first-token", r.Header.Get(mediconnectcontract.ClaimWorkloadHeader))
		} else {
			require.Equal(t, "Bearer second-token", r.Header.Get(mediconnectcontract.ClaimWorkloadHeader))
		}
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(`{"current":false}`))
		require.NoError(t, err)
	}))
	t.Cleanup(server.Close)
	client, err := mediconnectclaims.New(server.URL, tokenFile, "", nil)
	require.NoError(t, err)
	current, err := client.Current(context.Background(), mediconnectcontract.ClaimCheckRequest{TenantID: "11111111-1111-4111-8111-111111111111", RequestID: "019a1b52-0000-7000-8000-000000000001", HospitalID: "22222222-2222-4222-8222-222222222222", PatientID: "33333333-3333-4333-8333-333333333333", Subject: "synthetic-subject"})
	require.NoError(t, err)
	require.False(t, current)
	require.NoError(t, os.WriteFile(tokenFile, []byte("second-token\n"), 0600))
	current, err = client.Current(context.Background(), mediconnectcontract.ClaimCheckRequest{TenantID: "11111111-1111-4111-8111-111111111111", RequestID: "019a1b52-0000-7000-8000-000000000001", HospitalID: "22222222-2222-4222-8222-222222222222", PatientID: "33333333-3333-4333-8333-333333333333", Subject: "synthetic-subject"})
	require.NoError(t, err)
	require.False(t, current)
	require.EqualValues(t, 2, calls.Load())
}

func TestOwnerClientRejectsMalformedOrNonJSONObservations(t *testing.T) {
	for _, tc := range []struct{ name, body, contentType string }{
		{"missing", "{}", "application/json"},
		{"null", `{"current":null}`, "application/json"},
		{"numeric", `{"current":1}`, "application/json"},
		{"string", `{"current":"true"}`, "application/json"},
		{"duplicate", `{"current":false,"current":true}`, "application/json"},
		{"unknown", `{"current":true,"extra":false}`, "application/json"},
		{"trailing", `{"current":true} {}`, "application/json"},
		{"array", `[{"current":true}]`, "application/json"},
		{"oversize", `{"current":true}` + strings.Repeat(" ", 1025), "application/json"},
		{"wrong media", `{"current":true}`, "text/plain"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "token")
			require.NoError(t, os.WriteFile(file, []byte("projected-token"), 0600))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", tc.contentType)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(server.Close)
			client, err := mediconnectclaims.New(server.URL, file, "", nil)
			require.NoError(t, err)
			current, err := client.Current(t.Context(), mediconnectcontract.ClaimCheckRequest{})
			require.ErrorIs(t, err, mediconnectclaims.ErrUnavailable)
			require.False(t, current)
		})
	}
}

func TestOwnerClientRejectsRedirectsWithoutLeakingWorkloadCredentials(t *testing.T) {
	file := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(file, []byte("projected-token"), 0600))
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		targetCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"current":true}`))
	}))
	t.Cleanup(target.Close)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(source.Close)
	client, err := mediconnectclaims.New(source.URL, file, "", nil)
	require.NoError(t, err)
	current, err := client.Current(t.Context(), mediconnectcontract.ClaimCheckRequest{})
	require.ErrorIs(t, err, mediconnectclaims.ErrUnavailable)
	require.False(t, current)
	require.Zero(t, targetCalls.Load())
}

func TestOwnerClientRejectsUnprotectedOrAmbiguousConfiguration(t *testing.T) {
	file := filepath.Join(t.TempDir(), "token")
	for _, origin := range []string{"", "/relative", "http://owner.example", "http://127.0.0.1.evil.example", "http://owner.helivanta.svc.cluster.local", "https://user:private@owner.example", "https://owner.example?tenant=selected", "https://owner.example#fragment", "https://owner.example/alternate", "file:///tmp/token"} {
		_, err := mediconnectclaims.New(origin, file, "", nil)
		require.ErrorIs(t, err, mediconnectclaims.ErrUnavailable, origin)
	}
	_, err := mediconnectclaims.New("https://owner.example", "relative-token", "", nil)
	require.ErrorIs(t, err, mediconnectclaims.ErrUnavailable)
	_, err = mediconnectclaims.New("http://owner.helivanta.svc.cluster.local", file, "istio-mtls", nil)
	require.NoError(t, err)
}

func TestOwnerClientDoesNotAllowObservationAfterCallerCancellation(t *testing.T) {
	file := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(file, []byte("projected-token"), 0600))
	started, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"current":true}`))
	}))
	t.Cleanup(server.Close)
	client, err := mediconnectclaims.New(server.URL, file, "", nil)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type result struct {
		current bool
		err     error
	}
	done := make(chan result, 1)
	go func() {
		current, err := client.Current(ctx, mediconnectcontract.ClaimCheckRequest{})
		done <- result{current, err}
	}()
	<-started
	cancel()
	close(release)
	got := <-done
	require.ErrorIs(t, got.err, mediconnectclaims.ErrUnavailable)
	require.False(t, got.current)
}

func TestOwnerClientRejectsEveryNonSuccessStatus(t *testing.T) {
	file := filepath.Join(t.TempDir(), "token")
	require.NoError(t, os.WriteFile(file, []byte("projected-token"), 0600))
	for _, status := range []int{201, 204, 400, 401, 403, 404, 429, 500, 503} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"current":true}`))
		}))
		client, err := mediconnectclaims.New(server.URL, file, "", nil)
		require.NoError(t, err)
		current, err := client.Current(t.Context(), mediconnectcontract.ClaimCheckRequest{})
		server.Close()
		require.ErrorIs(t, err, mediconnectclaims.ErrUnavailable)
		require.False(t, current)
	}
}

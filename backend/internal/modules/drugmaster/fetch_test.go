package drugmaster

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLicensedFeedRejectsUntrustedEndpoints(t *testing.T) {
	for _, endpoint := range []string{"http://example.com/feed", "https://u:p@example.com/feed", "https://example.com/feed?key=secret", "https://127.0.0.1/feed", "https://[::1]/feed", "https://example.com/feed#fragment"} {
		_, err := FetchRelease(t.Context(), endpoint, "vendor", "licence", "")
		require.Error(t, err, endpoint)
		require.NotContains(t, err.Error(), "secret")
	}
}

func TestLicensedFeedValidatesProvenanceAndNeverFollowsRedirects(t *testing.T) {
	good := `{"country":"IN","source":{"name":"licensed-vendor","licence_ref":"licence-1","version":"v1"},"molecules":[{"inn_name":"Synthetic molecule"}],"formulations":[],"products":[]}`
	for _, tc := range []struct {
		name, body string
		status     int
		wantErr    bool
	}{
		{"valid", good, 200, false},
		{"wrong source", strings.ReplaceAll(good, "licensed-vendor", "another-vendor"), 200, true},
		{"wrong licence", strings.ReplaceAll(good, "licence-1", "licence-2"), 200, true},
		{"truncated", good[:len(good)-4], 200, true},
		{"redirect", good, 302, true},
		{"unavailable", good, 503, true},
		{"unknown metadata", strings.Replace(good, `"country":"IN"`, `"unexpected":true,"country":"IN"`, 1), 200, true},
		{"missing safety flag", `{"country":"IN","source":{"name":"licensed-vendor","licence_ref":"licence-1","version":"v1"},"formulations":[{"code":"F","valid_from":"2026-11-19","label":"Synthetic","dose_form":"tablet","route":"oral","regulatory_class":"none","ingredients":[]}]}`, 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				w.Header().Set("Location", "https://example.com/leak")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client := server.Client()
			client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
			raw, err := fetchRelease(context.Background(), client, server.URL, "licensed-vendor", "licence-1", "test-token", 4096)
			if tc.wantErr {
				require.Error(t, err)
				require.Empty(t, raw)
			} else {
				require.NoError(t, err)
				require.Equal(t, good, string(raw))
			}
			require.NotContains(t, fmtError(err), "test-token")
		})
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(good)) }))
	defer server.Close()
	_, err := fetchRelease(t.Context(), server.Client(), server.URL, "licensed-vendor", "licence-1", "", 10)
	require.Error(t, err)
}

func fmtError(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

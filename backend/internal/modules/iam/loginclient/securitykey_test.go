package loginclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSecurityKeyContinuationRequiresNativeProofAndAnActiveMatchingRequest(t *testing.T) {
	for _, tc := range []struct {
		name, password, request string
		status                  int
		want                    error
	}{
		{"missing password", `null`, `{"id":"V2_req","clientId":"app","redirectUri":"https://helivanta.app/api/auth/callback"}`, 200, ErrUnavailable},
		{"empty password proof", `{}`, `{"id":"V2_req","clientId":"app","redirectUri":"https://helivanta.app/api/auth/callback"}`, 200, ErrUnavailable},
		{"malformed proof", `{"verifiedAt":"not-a-time"}`, `{"id":"V2_req","clientId":"app","redirectUri":"https://helivanta.app/api/auth/callback"}`, 200, ErrUnavailable},
		{"foreign client", `{"verifiedAt":"2026-11-11T07:00:00Z"}`, `{"id":"V2_req","clientId":"other-app","redirectUri":"https://other.example.test/callback"}`, 200, ErrAuthRequestInvalid},
		{"mismatched request", `{"verifiedAt":"2026-11-11T07:00:00Z"}`, `{"id":"V2_other","clientId":"app","redirectUri":"https://helivanta.app/api/auth/callback"}`, 200, ErrUnavailable},
		{"incomplete request", `{"verifiedAt":"2026-11-11T07:00:00Z"}`, `{"id":"V2_req"}`, 200, ErrUnavailable},
		{"expired request", `{"verifiedAt":"2026-11-11T07:00:00Z"}`, `{}`, 404, ErrAuthRequestInvalid},
		{"identity unavailable", `{"verifiedAt":"2026-11-11T07:00:00Z"}`, `{}`, 503, ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			native := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Error("continuation issued a mutation")
					w.WriteHeader(500)
					return
				}
				switch r.URL.Path {
				case "/v2/sessions/pending":
					_, _ = fmt.Fprintf(w, `{"session":{"factors":{"user":{"id":"owner","loginName":"owner@example.test"},"password":%s}}}`, tc.password)
				case "/v2/oidc/auth_requests/V2_req":
					w.WriteHeader(tc.status)
					_, _ = fmt.Fprintf(w, `{"authRequest":%s}`, tc.request)
				default:
					t.Error("unexpected native request")
					w.WriteHeader(500)
				}
			}))
			t.Cleanup(native.Close)
			c := New(native.URL, "synthetic", native.Client())
			target, err := c.SecurityKeyLoginURL(context.Background(), "V2_req", Session{ID: "pending", Token: "private"}, "app")
			require.Empty(t, target)
			require.True(t, errors.Is(err, tc.want), "%v", err)
		})
	}
}

func TestSecurityKeyContinuationRejectsUnsafeIssuerBeforeAnyRequest(t *testing.T) {
	for _, issuer := range []string{"http://auth.example.test", "https://user:pass@auth.example.test", "https://auth.example.test/unexpected", "https://auth.example.test?redirect=evil", "https://auth.example.test#fragment"} {
		t.Run(issuer, func(t *testing.T) {
			c := New(issuer, "synthetic", http.DefaultClient)
			target, err := c.SecurityKeyLoginURL(t.Context(), "V2_req", Session{ID: "pending", Token: "private"}, "app")
			require.ErrorIs(t, err, ErrUnavailable)
			require.Empty(t, target)
		})
	}
}

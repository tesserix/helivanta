package iam

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
)

func TestPasswordOffersExplicitNativeSecurityKeyContinuationWithoutFinalizing(t *testing.T) {
	mux := http.NewServeMux()
	finalized := false
	mux.HandleFunc("POST /v2/sessions", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"sessionId":"native-s","sessionToken":"private-native-token"}`))
	})
	mux.HandleFunc("GET /v2/sessions/native-s", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"session":{"factors":{"user":{"id":"owner","organizationId":"owner-org","loginName":"owner@example.test"},"password":{"verifiedAt":"2026-11-11T07:00:00Z"}}}}`))
	})
	mux.HandleFunc("GET /v2/users/owner/authentication_methods", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"authMethodTypes":["AUTHENTICATION_METHOD_TYPE_U2F","AUTHENTICATION_METHOD_TYPE_PASSWORD","AUTHENTICATION_METHOD_TYPE_IDP"]}`))
	})
	mux.HandleFunc("GET /v2/oidc/auth_requests/"+loginUITestAuthRequestID, func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"authRequest": map[string]any{"id": loginUITestAuthRequestID, "clientId": "helivanta-web", "redirectUri": "https://helivanta.app/api/auth/callback"}})
	})
	mux.HandleFunc("POST /v2/oidc/auth_requests/"+loginUITestAuthRequestID, func(w http.ResponseWriter, _ *http.Request) { finalized = true; w.WriteHeader(500) })
	native := httptest.NewTLSServer(mux)
	t.Cleanup(native.Close)
	res := postPassword(t, loginclient.New(native.URL, "synthetic", native.Client()), "owner@example.test", "synthetic-password")
	require.Equal(t, 200, res.Code, res.Body.String())
	var body struct {
		SecurityKey struct {
			URL string `json:"url"`
		} `json:"security_key_required"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
	target, err := url.Parse(body.SecurityKey.URL)
	require.NoError(t, err)
	require.Equal(t, native.URL, target.Scheme+"://"+target.Host)
	require.Equal(t, "/ui/v2/login/password", target.Path)
	require.Equal(t, "oidc_"+loginUITestAuthRequestID, target.Query().Get("requestId"))
	require.Equal(t, "owner@example.test", target.Query().Get("loginName"))
	require.False(t, finalized)
	require.False(t, strings.Contains(res.Body.String(), "callback_url"))
	require.False(t, strings.Contains(res.Body.String(), "private-native-token"))
}

func TestPasswordKeepsUnsupportedAndMixedFactorsClosed(t *testing.T) {
	for _, methods := range []string{
		`["AUTHENTICATION_METHOD_TYPE_U2F","AUTHENTICATION_METHOD_TYPE_TOTP"]`,
		`["AUTHENTICATION_METHOD_TYPE_PASSWORD","AUTHENTICATION_METHOD_TYPE_PASSKEY"]`,
		`["AUTHENTICATION_METHOD_TYPE_U2F","AUTHENTICATION_METHOD_TYPE_NEW_UNKNOWN"]`,
	} {
		t.Run(methods, func(t *testing.T) {
			native := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v2/oidc/auth_requests/" + loginUITestAuthRequestID:
					_ = json.NewEncoder(w).Encode(map[string]any{"authRequest": map[string]any{"id": loginUITestAuthRequestID, "clientId": "helivanta-web", "scope": []string{"openid"}}})
				case "/v2/sessions":
					_, _ = w.Write([]byte(`{"sessionId":"pending","sessionToken":"private"}`))
				case "/v2/sessions/pending":
					_, _ = w.Write([]byte(`{"session":{"factors":{"user":{"id":"owner","organizationId":"org","loginName":"owner@example.test"},"password":{"verifiedAt":"2026-11-11T07:00:00Z"}}}}`))
				case "/v2/users/owner/authentication_methods":
					_, _ = w.Write([]byte(`{"authMethodTypes":` + methods + `}`))
				default:
					t.Error("unsupported factors reached a continuation or callback")
					w.WriteHeader(500)
				}
			}))
			t.Cleanup(native.Close)
			res := postPassword(t, loginclient.New(native.URL, "synthetic", native.Client()), "owner@example.test", "synthetic")
			require.Equal(t, http.StatusForbidden, res.Code, res.Body.String())
			require.NotContains(t, res.Body.String(), "security_key_required")
			require.NotContains(t, res.Body.String(), "callback_url")
		})
	}
}

package globalapi

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/session"
)

type revocationState struct{ after time.Time }

func (s revocationState) RevokedAfter(context.Context, string) (time.Time, error) {
	return s.after, nil
}

func TestProxyPreservesRealSessionVerification(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := session.NewSigner(private, "test", "synthetic", time.Hour)
	require.NoError(t, err)
	verifier, err := session.NewVerifier(public, "test", "synthetic")
	require.NoError(t, err)
	now := time.Now().Truncate(time.Second)
	token, err := signer.Mint("synthetic-user", "00000000-0000-4000-8000-000000000001", now, now.Add(time.Hour))
	require.NoError(t, err)
	for _, revoked := range []bool{false, true} {
		rev := revocationState{}
		if revoked {
			rev.after = now
		}
		owner := gin.New()
		owner.Use(authn.Middleware(authn.NewSessionVerifier(verifier), rev))
		owner.GET("/v1/pharmacy/outlets", func(c *gin.Context) {
			principal, _, ok := authn.TenantPrincipal(c)
			if !ok {
				return
			}
			c.JSON(200, gin.H{"tenant": principal.TenantID})
		})
		upstream := httptest.NewServer(owner)
		t.Cleanup(upstream.Close)
		gateway, err := New(Config{Development: true, Timeout: time.Second, Upstreams: map[string]string{"pharmacy": upstream.URL}})
		require.NoError(t, err)
		for _, credential := range []string{"", "forged", token} {
			for _, cookie := range []bool{false, true} {
				req := httptest.NewRequest(http.MethodGet, "/v1/pharmacy/outlets", nil)
				if credential != "" {
					if cookie {
						req.AddCookie(&http.Cookie{Name: authn.SessionCookie, Value: credential})
					} else {
						req.Header.Set("Authorization", "Bearer "+credential)
					}
				}
				req.Header.Set("X-Tenant-ID", "another-tenant")
				res := httptest.NewRecorder()
				gateway.ServeHTTP(res, req)
				if credential == token && !revoked {
					require.Equal(t, 200, res.Code)
					require.JSONEq(t, `{"tenant":"00000000-0000-4000-8000-000000000001"}`, res.Body.String())
				} else {
					require.Equal(t, 401, res.Code)
				}
			}
		}
	}
}

func TestLoginCookieAndNoWriteRetry(t *testing.T) {
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		http.SetCookie(w, &http.Cookie{Name: authn.SessionCookie, Value: "synthetic", Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
		w.WriteHeader(201)
	}))
	t.Cleanup(upstream.Close)
	gateway, err := New(Config{Development: true, Timeout: time.Second, Upstreams: map[string]string{"platform": upstream.URL}})
	require.NoError(t, err)
	res := httptest.NewRecorder()
	gateway.ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/v1/auth/login", nil))
	require.Equal(t, 201, res.Code)
	cookies := res.Result().Cookies()
	require.Len(t, cookies, 1)
	require.True(t, cookies[0].Secure)
	require.True(t, cookies[0].HttpOnly)
	require.Equal(t, http.SameSiteLaxMode, cookies[0].SameSite)
	require.Equal(t, 1, calls)
}

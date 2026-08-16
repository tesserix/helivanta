package bootstrap_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/bootstrap"
	"github.com/tesserix/hms/internal/config"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/ratelimit"
	"github.com/tesserix/hms/pkg/session"
)

// chainNeverRevoked stands in for iam.RevocationChecker: this file is
// about which TOKEN bootstrap.V1Chain accepts on the request path, not
// about revocation, which pkg/authn and internal/modules/iam test
// directly.
type chainNeverRevoked struct{}

func (chainNeverRevoked) RevokedAfter(context.Context, string) (time.Time, error) {
	return time.Time{}, nil
}

// chainAllowResolver grants every permission to every subject, so a
// request that reaches authz.Middleware never gets refused there —
// this file's assertions are about whether a request reaches the
// handler at all, decided by authn.Middleware, not about what it can
// then do.
type chainAllowResolver struct{}

func (chainAllowResolver) Resolve(context.Context, string, string) (authz.PermissionSet, error) {
	return authz.PermissionSet{}, nil
}

// newChainHarness mounts a trivial handler behind bootstrap.V1Chain
// wired EXACTLY the way cmd/api/main.go wires it for production traffic:
// authn.NewSessionVerifier over a real session.Verifier, never a raw
// Zitadel verifier. This is the seam that closes the gap chain.go's own
// doc comment warns about — a hand-copied "equivalent chain" in a test
// can pass while production wiring drifts from it — by using the SAME
// V1Chain function main.go calls, with the SAME verifier construction
// main.go uses (authn.NewSessionVerifier), rather than a bespoke fake
// TokenVerifier a wiring regression could slip past.
func newChainHarness(t *testing.T) (*gin.Engine, *session.Signer) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	const (
		kid    = "hms-session-v1"
		issuer = "https://hms.test"
	)
	signer, err := session.NewSigner(priv, kid, issuer, 15*time.Minute)
	require.NoError(t, err)
	sessionVerifier, err := session.NewVerifier(pub, kid, issuer)
	require.NoError(t, err)
	requestVerifier := authn.NewSessionVerifier(sessionVerifier)

	cfg := config.Load()
	limiter := ratelimit.NewMemory(10_000)

	gin.SetMode(gin.TestMode)
	e := gin.New()
	v1 := e.Group("/v1", bootstrap.V1Chain(requestVerifier, chainNeverRevoked{}, limiter, cfg, chainAllowResolver{})...)
	v1.GET("/whoami", func(c *gin.Context) {
		p, _ := authn.PrincipalFrom(c)
		c.JSON(http.StatusOK, gin.H{"subject": p.Subject, "tenant_id": p.TenantID})
	})
	return e, signer
}

// TestV1ChainAcceptsAnHMSSession is the positive control: the exact
// composition main.go wires up must still let a genuine HMS session
// through, and must resolve the tenant it was minted for.
func TestV1ChainAcceptsAnHMSSession(t *testing.T) {
	e, signer := newChainHarness(t)
	// An hour of idle window: this test is about which TOKEN the chain
	// accepts, not about the idle timeout, so the deadline is set
	// comfortably beyond any plausible run of this test and the
	// authn.Middleware idle check (#848) never fires here. The idle check
	// itself is pinned by pkg/authn's own tests.
	token, err := signer.Mint("user-1", "11111111-1111-1111-1111-111111111111", time.Now(), time.Now().Add(time.Hour))
	require.NoError(t, err)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	e.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "11111111-1111-1111-1111-111111111111")
}

// TestV1ChainRefusesRawZitadelToken is the mandatory Task 4 proof, at
// the same wiring level main.go actually runs: a raw Zitadel ID
// token — never minted by HMS, never touching session.Signer — must be
// refused by the /v1 chain. Unlike pkg/authn's own
// TestMiddleware_RefusesRawZitadelToken (same claim, narrower scope:
// authn.Middleware + authn.NewSessionVerifier only), this test also
// pins that bootstrap.V1Chain — the function main.go actually calls —
// does not somehow reintroduce acceptance further down the chain
// (ratelimit, authz).
func TestV1ChainRefusesRawZitadelToken(t *testing.T) {
	e, _ := newChainHarness(t)
	zitadelShaped := forgeRS256Token(t)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/whoami", nil)
	req.Header.Set("Authorization", "Bearer "+zitadelShaped)
	e.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnauthorized, w.Code,
		"a raw Zitadel-shaped token must never authenticate an ordinary /v1 request")
}

func forgeRS256Token(t *testing.T) string {
	t.Helper()
	header := map[string]any{"alg": "RS256", "typ": "JWT", "kid": "zitadel-kid-1"}
	payload := map[string]any{
		"sub":       "zitadel-user-1",
		"iss":       "https://zitadel.local",
		"auth_time": time.Now().Add(-time.Minute).Unix(),
		"iat":       time.Now().Unix(),
		"exp":       time.Now().Add(time.Hour).Unix(),
	}
	h, err := json.Marshal(header)
	require.NoError(t, err)
	p, err := json.Marshal(payload)
	require.NoError(t, err)
	// The signature segment is irrelevant: session.Verifier's keyfunc
	// refuses to hand back a key at all for a non-Ed25519 method, so no
	// signature could ever pass regardless of what bytes fill it in.
	return base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(p) + "."
}

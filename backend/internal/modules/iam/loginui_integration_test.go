package iam_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/modules/iam" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/hms/internal/modules/iam/loginclient"
	"github.com/tesserix/hms/pkg/ratelimit"
)

// This file proves iam.LoginUIHandlers end to end against the REAL local
// dev Zitadel (plan #854 Task 5) — every other test in this package
// (loginui_test.go) drives it against a fake HTTP server standing in for
// Zitadel, which pins response-mapping and timing behaviour but can never
// catch a genuine wire-shape drift between what loginclient.Client
// expects and what Zitadel v4.15.3 actually sends. This is the ONE place
// in the repo that closes that gap for the login-client API specifically
// (docs/superpowers/spikes/2026-08-16-zitadel-login-client.md already
// closed it once, by hand, to write client.go in the first place; this
// test keeps it closed on every future run rather than trusting the spike
// stayed accurate).
//
// # Guard: skip cleanly, never fail, when the dev stack is absent
//
// This repo has no existing "skip when the real dev stack is absent" Go
// test to mirror byte-for-byte (internal/testinfra's containers.go starts
// its OWN ephemeral Postgres/NATS/OpenFGA via testcontainers rather than
// depending on `make up` already being running) — the login-client API
// has no ephemeral/testcontainer equivalent available, so this test
// depends on the real `docker-compose.dev.yml` stack the way
// scripts/zitadel-verify-login.mjs and the e2e suite already do. The
// guard below follows the SAME shape those non-Go callers use: read the
// PAT and client id from the files Zitadel/zitadel-bootstrap.mjs write to
// dev/zitadel/secrets (falling back to the env vars the API itself reads,
// so CI or a developer can override without those files existing), then
// probe the issuer's own healthz before doing anything else. Any failure
// at this stage is `t.Skip`, never `t.Fatal` — this test's job is to
// prove login-client BEHAVIOUR when the stack is up, not to force every
// `go test ./...` run to require it.
func skipUnlessDevStackIsUp(t *testing.T) integrationEnv {
	t.Helper()

	issuer := getenvOrDefault("ZITADEL_ISSUER_URL", "http://localhost:20080")
	clientID := getenvOrDefault("ZITADEL_CLIENT_ID", "")
	if clientID == "" {
		clientID = readSecretFile(t, "zitadel.env", "ZITADEL_CLIENT_ID")
	}
	token := os.Getenv("ZITADEL_LOGIN_CLIENT_TOKEN")
	if token == "" {
		token = readFileTrimmed(t, "login-client.pat")
	}
	if clientID == "" || token == "" {
		t.Skip("dev Zitadel stack secrets not found (dev/zitadel/secrets/{zitadel.env,login-client.pat} " +
			"absent and ZITADEL_CLIENT_ID/ZITADEL_LOGIN_CLIENT_TOKEN unset) — run `make dev-infra` " +
			"to provision the local stack before running this test")
	}

	probeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(probeCtx, http.MethodGet, issuer+"/debug/healthz", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Skipf("dev Zitadel at %s is unreachable (%v) — run `make dev-infra` first", issuer, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Skipf("dev Zitadel at %s answered healthz with HTTP %d — stack is up but not ready", issuer, resp.StatusCode)
	}

	return integrationEnv{
		issuer:      issuer,
		clientID:    clientID,
		token:       token,
		redirectURI: getenvOrDefault("HMS_DEV_REDIRECT_URI", "http://localhost:4301/api/auth/callback"),
	}
}

type integrationEnv struct {
	issuer      string
	clientID    string
	token       string
	redirectURI string
}

func getenvOrDefault(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// secretsDir resolves dev/zitadel/secrets relative to this test file's
// package (backend/internal/modules/iam), walking up to the repo root the
// same fixed number of levels every time — this file's own location is
// stable, so a fixed relative path is simpler and no less robust than
// searching upward for a marker file.
const secretsDirFromPackage = "../../../../dev/zitadel/secrets/"

func readFileTrimmed(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(secretsDirFromPackage + name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// readSecretFile pulls one KEY=value line out of dev/zitadel/secrets/name
// — used for zitadel.env's ZITADEL_CLIENT_ID line, written by
// scripts/zitadel-bootstrap.mjs.
func readSecretFile(t *testing.T, name, key string) string {
	t.Helper()
	contents := readFileTrimmed(t, name)
	if contents == "" {
		return ""
	}
	for _, line := range strings.Split(contents, "\n") {
		if v, ok := strings.CutPrefix(line, key+"="); ok {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// b64url matches scripts/lib/zitadel.mjs's own b64url helper: standard
// base64 with '+'/'/' remapped to '-'/'_' and padding stripped, per
// RFC 7636's PKCE encoding.
func b64url(b []byte) string {
	return strings.TrimRight(
		strings.NewReplacer("+", "-", "/", "_").Replace(base64.StdEncoding.EncodeToString(b)),
		"=",
	)
}

// newAuthRequest drives the REAL /oauth/v2/authorize endpoint, the same
// way a browser's PKCE authorization-code flow does (mirroring
// scripts/lib/zitadel.mjs's hostedUILoginOnce, minus the actual hosted-UI
// page load): a well-formed request there 302s to the login UI with
// ?authRequest=V2_… in the Location header, before any credential is
// checked. That id is what iam.LoginUIHandlers.AuthRequest/Password
// operate on, so this is the one real Zitadel call this test needs to
// make outside the handlers under test, to get a real id to hand them.
//
// The PKCE code_verifier is generated but deliberately unused past this
// call: this test only exercises the login-client PASSWORD path
// (loginclient.Client.CreatePasswordSession + CompleteIfSufficient), not
// the browser's own eventual GET /oauth/v2/token code exchange — the
// callback_url Password returns already contains a fresh code and state
// (asserted below), and completing THAT exchange too would be testing
// Zitadel's OIDC token endpoint, not HMS's login-client wiring.
func newAuthRequest(t *testing.T, env integrationEnv) string {
	t.Helper()

	verifier := make([]byte, 32)
	_, err := rand.Read(verifier)
	require.NoError(t, err)
	challenge := sha256.Sum256([]byte(b64url(verifier)))
	state := make([]byte, 16)
	_, err = rand.Read(state)
	require.NoError(t, err)

	authURL, err := url.Parse(env.issuer + "/oauth/v2/authorize")
	require.NoError(t, err)
	q := authURL.Query()
	q.Set("client_id", env.clientID)
	q.Set("redirect_uri", env.redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", "openid profile email")
	q.Set("code_challenge", b64url(challenge[:]))
	q.Set("code_challenge_method", "S256")
	q.Set("state", b64url(state))
	authURL.RawQuery = q.Encode()

	// A client that does NOT follow redirects: the 302 itself, not
	// wherever it points, is what this call needs — see
	// scripts/lib/zitadel.mjs's hostedUILoginOnce comment on the same
	// choice (there made by intercepting the request event instead,
	// since that caller drives an actual browser).
	noRedirect := &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       10 * time.Second,
	}
	resp, err := noRedirect.Get(authURL.String())
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Truef(t, resp.StatusCode >= 300 && resp.StatusCode < 400,
		"GET /oauth/v2/authorize = HTTP %d, want a redirect", resp.StatusCode)

	loc, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	id := loc.Query().Get("authRequest")
	require.NotEmpty(t, id, "redirect Location %q carried no authRequest query param", resp.Header.Get("Location"))
	return id
}

// newIntegrationRouter wires the SAME three routes bootstrap.MountUnauthenticated
// mounts in production, backed by a REAL loginclient.Client pointed at
// env's Zitadel — the router-building half of what main.go's run() does,
// minus the DB/NATS/OpenFGA construction this test has no need for.
func newIntegrationRouter(env integrationEnv) (*gin.Engine, *iam.LoginUIHandlers) {
	client := loginclient.New(env.issuer, env.token, http.DefaultClient)
	// nil limiter: this test makes a handful of calls total, nowhere near
	// any real budget, and a nil limiter fails OPEN per Password's own
	// doc comment — exercising that same fail-open path other unit tests
	// already cover directly (TestPasswordAdmitsWhenLimiterUnavailable)
	// is not this test's job.
	handlers := iam.NewLoginUIHandlers(client, "http://zitadel.invalid/ui/v2/login", nil, ratelimit.Rule{})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/auth/login/password", handlers.Password)
	return r, handlers
}

func postPasswordReal(t *testing.T, r *gin.Engine, authRequestID, loginName, password string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{
		"auth_request_id": authRequestID,
		"login_name":      loginName,
		"password":        password,
	})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login/password", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// devSeededEmail/devSeededPassword are the account
// docs/superpowers/plans/2026-08-16-hms-login-client.md and this task's
// own brief name as already seeded into the local dev stack
// (scripts/seed-dev.mjs) — this test does not seed it itself.
const (
	devSeededEmail    = "test@hms.dev"
	devSeededPassword = "HmsDev123!"
)

// TestIntegration_PasswordSuccess_ReturnsCallbackURLWithCodeAndState is
// step 1-3 of this task's brief: create a real auth request, check the
// real seeded credential against it through HMS's own handler, and
// assert the resulting callback_url is a genuine, freshly-minted
// authorization response — not just that SOME string came back.
func TestIntegration_PasswordSuccess_ReturnsCallbackURLWithCodeAndState(t *testing.T) {
	env := skipUnlessDevStackIsUp(t)
	r, _ := newIntegrationRouter(env)

	authRequestID := newAuthRequest(t, env)
	w := postPasswordReal(t, r, authRequestID, devSeededEmail, devSeededPassword)

	require.Equalf(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var body struct {
		CallbackURL string `json:"callback_url"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	callback, err := url.Parse(body.CallbackURL)
	require.NoErrorf(t, err, "callback_url %q did not parse as a URL", body.CallbackURL)
	q := callback.Query()
	require.NotEmptyf(t, q.Get("code"), "callback_url %q carried no code param", body.CallbackURL)
	require.NotEmptyf(t, q.Get("state"), "callback_url %q carried no state param", body.CallbackURL)
}

// TestIntegration_PasswordFailures_WrongPasswordAndUnknownUserAreByteIdentical
// is step 4 of this task's brief, run against the REAL Zitadel rather
// than loginui_test.go's fake server — proving spec D5's identical-
// refusal property survives contact with Zitadel's actual wire shapes for
// COMMAND-3M0fs (wrong password) and QUERY-Dfbg2 (unknown user), not just
// the shapes the unit tests hand-wrote from the spike.
//
// # Why this makes exactly two failed attempts, not more
//
// MinFailedLoginDuration's own doc comment (loginui.go) records a
// MEASURED finding against this same dev Zitadel: failed attempts 1-9
// against ONE login name land in a tight ~0.8s band, but Zitadel applies
// its own escalating per-account backoff from attempt 10 onward (+1s per
// 5 further attempts), reset only by a SUCCESSFUL login. A test that
// hammered many failures here would not just run slowly — past attempt
// ~10 the wrong-password path's real latency would exceed whatever
// MinFailedLoginDuration is set to, reopening (in miniature, per that
// same doc comment) the exact timing oracle this test exists to help
// prove closed. This test makes exactly ONE wrong-password attempt
// against devSeededEmail and ONE unknown-user attempt against a distinct,
// never-registered login name — nowhere near that threshold, and no
// wrong-password attempt this test makes is ever repeated across test
// runs against the SAME login name in a way that could accumulate within
// one run — it is a real, cumulative risk ACROSS repeated runs over time
// (Zitadel's counter is per-account and persists in its own database, not
// per-process), which is exactly why this stays at one attempt rather
// than being tempted into a loop for "more confidence": every additional
// run of this test file adds one more entry to devSeededEmail's failure
// history. This is a deliberate design choice per this task's brief ("do
// not fight this; design the test around it"), not an oversight.
func TestIntegration_PasswordFailures_WrongPasswordAndUnknownUserAreByteIdentical(t *testing.T) {
	env := skipUnlessDevStackIsUp(t)
	r, _ := newIntegrationRouter(env)

	// Both failing attempts reuse ONE fresh auth request: Password
	// returns before ever calling CompleteIfSufficient on either failure
	// path (loginclient.ErrBadCredentials / ErrUserNotFound), so the auth
	// request itself is never consumed by a failed attempt — see
	// Password's own doc comment on exactly where that early return sits.
	authRequestID := newAuthRequest(t, env)

	wrongStart := time.Now()
	wrong := postPasswordReal(t, r, authRequestID, devSeededEmail, "definitely-the-wrong-password")
	wrongElapsed := time.Since(wrongStart)

	unknownStart := time.Now()
	unknown := postPasswordReal(t, r, authRequestID,
		fmt.Sprintf("nobody-%d@hms.dev", time.Now().UnixNano()), "irrelevant")
	unknownElapsed := time.Since(unknownStart)

	require.Equal(t, wrong.Code, unknown.Code, "status differs: wrong=%d unknown=%d", wrong.Code, unknown.Code)
	require.Equal(t, wrong.Body.String(), unknown.Body.String(), "refusal body differs between wrong password and unknown user")
	require.NotContains(t, wrong.Body.String(), "failedAttempts", "response leaks Zitadel's internal attempt counter")

	// Both must also clear MinFailedLoginDuration's floor — the SAME
	// timing property loginui_test.go's
	// TestPasswordFailureTimingIsEqualised already proves against fake
	// servers, checked here once against the real one for good measure.
	require.GreaterOrEqualf(t, wrongElapsed, iam.MinFailedLoginDuration,
		"wrong-password path returned in %v, faster than the floor", wrongElapsed)
	require.GreaterOrEqualf(t, unknownElapsed, iam.MinFailedLoginDuration,
		"unknown-user path returned in %v, faster than the floor", unknownElapsed)
}

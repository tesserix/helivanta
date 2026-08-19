package iam_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/bootstrap"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/helivanta/pkg/ratelimit"
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
		redirectURI: getenvOrDefault("HELIVANTA_DEV_REDIRECT_URI", "http://localhost:4301/api/auth/callback"),
	}
}

type integrationEnv struct {
	issuer      string
	clientID    string
	token       string
	redirectURI string
}

// skipUnlessSeedPATIsAvailable resolves the helivanta-seed-bot IAM_OWNER PAT
// (dev/zitadel/secrets/helivanta-seed.pat, same file scripts/lib/zitadel.mjs's
// readMachinePAT reads) — the credential org-level policy ADMINISTRATION
// calls need. This is deliberately a SEPARATE credential from
// integrationEnv.token: the login-client PAT can READ the login policy
// (loginclient.Client.LoginPolicy already proves that) but has no
// permission to WRITE it — confirmed live: POST/DELETE
// /management/v1/policies/login with the login-client PAT answers
// AUTH-5mWD2 "No matching permissions found", the same permission
// boundary IAM_LOGIN_CLIENT is scoped by design. Using the seed PAT here,
// not a widened login-client PAT, keeps that boundary real rather than
// papering over it for one test's convenience.
//
// Only the one test that mutates org policy
// (TestIntegration_ForceMFAPolicy_HandsOffInsteadOfCompleting) calls
// this — every other integration test in this file needs no
// administrative access at all, and should not skip just because this
// separate credential happens to be missing.
func skipUnlessSeedPATIsAvailable(t *testing.T) string {
	t.Helper()
	token := os.Getenv("ZITADEL_SEED_TOKEN")
	if token == "" {
		token = readFileTrimmed(t, "helivanta-seed.pat")
	}
	if token == "" {
		t.Skip("dev/zitadel/secrets/helivanta-seed.pat not found and ZITADEL_SEED_TOKEN unset — " +
			"run `make dev-infra` to provision the local stack before running this test")
	}
	return token
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
// Zitadel's OIDC token endpoint, not Helivanta's login-client wiring.
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

// newIntegrationRouter mounts the login routes by CALLING
// bootstrap.MountUnauthenticated — the same function cmd/api/main.go
// calls — backed by a REAL loginclient.Client pointed at env's Zitadel.
// It is the router-building half of what main.go's run() does, minus the
// DB/NATS/OpenFGA construction this test has no need for. Only
// *gin.Engine is returned: every caller in this file drives Password
// through HTTP requests against the router, never the handlers value
// directly, so there is nothing for a second return to be used for.
//
// It calls the real function rather than hand-registering
// `r.POST("/v1/auth/login/password", handlers.Password)`, which is what
// an earlier version of this helper did while its comment claimed it
// "wires the SAME route bootstrap.MountUnauthenticated mounts". That was
// a replica: a change to the production path string, the HTTP method, or
// the argument ORDER of MountUnauthenticated (which takes five
// interchangeable gin.HandlerFunc values, so swapping two compiles
// cleanly) would leave this test green while production served the wrong
// handler on the wrong route. This repo has an explicit lesson on
// exactly that failure — test the wiring, not a replica — and this is a
// live integration test, the one place a replica costs the most.
//
// The `login` argument is POST /v1/auth/login (iam.LoginHandlers), which
// this file never exercises; it is passed a handler that FAILS the test
// if reached, rather than a silent no-op, so a future argument-order
// mistake surfaces as a failure here instead of a mystery elsewhere.
// This only detects a swap that involves the login slot — an
// authRequest↔handoff swap (the other two positions) would still
// compile and leave this file green, because only the password route
// is driven here. MountUnauthenticated panics on a nil, by design, so
// it cannot be omitted.
func newIntegrationRouter(t *testing.T, env integrationEnv) *gin.Engine {
	t.Helper()
	client := loginclient.New(env.issuer, env.token, http.DefaultClient)
	// nil limiter: this test makes a handful of calls total, nowhere near
	// any real budget, and a nil limiter fails OPEN per allowedByLimiter's
	// doc comment — exercising that same fail-open path other unit tests
	// already cover directly (TestPasswordAdmitsWhenLimiterUnavailable)
	// is not this test's job. nil db: this file never exercises the
	// OutcomeFactorRequired path (the dev-seeded user is password-only —
	// devSeededEmail/devSeededPassword below), so LoginUIHandlers' store
	// is never touched.
	handlers := iam.NewLoginUIHandlers(client, "http://zitadel.invalid/ui/v2/login", nil, nil, ratelimit.Rule{}, ratelimit.Rule{})

	gin.SetMode(gin.TestMode)
	r := gin.New()
	notExercised := func(c *gin.Context) {
		t.Errorf("POST /v1/auth/login was reached; this file only drives the login-UI routes — "+
			"check bootstrap.MountUnauthenticated's argument order (%s %s)", c.Request.Method, c.Request.URL.Path)
		c.Status(http.StatusInternalServerError)
	}
	bootstrap.MountUnauthenticated(r, notExercised,
		handlers.AuthRequest, handlers.Password, handlers.Handoff, handlers.Factor)
	return r
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
// docs/superpowers/plans/2026-08-16-helivanta-login-client.md and this task's
// own brief name as already seeded into the local dev stack
// (scripts/seed-dev.mjs) — this test does not seed it itself.
const (
	devSeededEmail    = "test@helivanta.dev"
	devSeededPassword = "HmsDev123!"
)

// TestIntegration_PasswordSuccess_ReturnsCallbackURLWithCodeAndState is
// step 1-3 of this task's brief: create a real auth request, check the
// real seeded credential against it through Helivanta's own handler, and
// assert the resulting callback_url is a genuine, freshly-minted
// authorization response — not just that SOME string came back. Shares
// its assertion body with assertLoginSucceeds (used again by the
// forceMfa/forceMfaLocalOnly tests below to prove login still works
// after a policy restore) rather than duplicating it.
func TestIntegration_PasswordSuccess_ReturnsCallbackURLWithCodeAndState(t *testing.T) {
	env := skipUnlessDevStackIsUp(t)
	assertLoginSucceeds(t, env, devSeededEmail, devSeededPassword, "")
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
	r := newIntegrationRouter(t, env)

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
		fmt.Sprintf("nobody-%d@helivanta.dev", time.Now().UnixNano()), "irrelevant")
	unknownElapsed := time.Since(unknownStart)

	// Asserting the two are merely EQUAL to each other is not enough — two
	// identical 503s (e.g. Zitadel entirely unreachable) would pass that
	// check while login is completely broken, not "correctly refusing a
	// bad credential". Pin the CONCRETE expected shape (401,
	// passwordFailureMessage's exact body) on each side individually,
	// THEN compare them to each other — that is what actually proves
	// spec D5's identical-refusal property, not just "these two calls
	// happened to fail the same way".
	wantBody := `{"error":"invalid_credentials","message":"email or password is incorrect"}`
	require.Equalf(t, http.StatusUnauthorized, wrong.Code, "wrong-password body: %s", wrong.Body.String())
	require.JSONEq(t, wantBody, wrong.Body.String())
	require.Equalf(t, http.StatusUnauthorized, unknown.Code, "unknown-user body: %s", unknown.Body.String())
	require.JSONEq(t, wantBody, unknown.Body.String())
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

// managementAPICall makes one machine-authenticated call against
// Zitadel's management API — the same convention
// scripts/lib/zitadel.mjs's managementAPI helper uses, reimplemented
// here rather than shared across the Go/JS boundary. Used only by the
// org-login-policy administration below; every other call in this file
// goes through loginclient.Client or a direct OIDC endpoint, neither of
// which this credential (the seed PAT, not the login-client PAT) is
// for.
//
// orgID scopes the call to a specific org via the x-zitadel-orgid header
// (#913 Task 4) — the SAME header name loginclient.withOrgID sets on the
// production path (client.go's `do`), reused here rather than
// reinvented, so a wire-level drift in that header name would break both
// the production code and this test's setup in the same way rather than
// only one of them silently. An empty orgID sends no header at all,
// preserving every call site that existed before Task 4 byte-for-byte —
// this was previously "unscoped, always"; it is now "unscoped when the
// caller passes no org", which is the same behaviour for every existing
// caller. Zitadel resolves an unscoped seed-PAT call against the seed
// bot's own org (TESSERIX in this dev stack), exactly as before.
func managementAPICall(t *testing.T, env integrationEnv, seedToken, orgID, method, path string, body map[string]any) map[string]any {
	t.Helper()
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		require.NoError(t, err)
		reqBody = strings.NewReader(string(b))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, env.issuer+path, reqBody)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+seedToken)
	req.Header.Set("Content-Type", "application/json")
	if orgID != "" {
		req.Header.Set("x-zitadel-orgid", orgID)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	var parsed map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&parsed))
	require.Truef(t, resp.StatusCode >= 200 && resp.StatusCode < 300,
		"%s %s (org=%q) = HTTP %d: %v", method, path, orgID, resp.StatusCode, parsed)
	return parsed
}

const loginPolicyPath = "/management/v1/policies/login"

// baseCustomLoginPolicy is the full field set Zitadel's own policy object
// carries (verified live, per this task's fix-round report) — the
// starting point every custom policy this file writes overrides fields
// onto. A partial body risks Zitadel filling unset fields with ITS OWN
// zero values rather than preserving the previous (default) policy's
// values, which would leave the org in a different state than "default
// plus the one field under test" after restore.
func baseCustomLoginPolicy() map[string]any {
	return map[string]any{
		"allowUsernamePassword":      true,
		"allowRegister":              true,
		"allowExternalIdp":           true,
		"forceMfa":                   false,
		"forceMfaLocalOnly":          false,
		"passwordlessType":           "PASSWORDLESS_TYPE_ALLOWED",
		"passwordCheckLifetime":      "864000s",
		"externalLoginCheckLifetime": "864000s",
		"mfaInitSkipLifetime":        "2592000s",
		"secondFactorCheckLifetime":  "64800s",
		"multiFactorCheckLifetime":   "43200s",
	}
}

// setOrgLoginPolicy writes a CUSTOM org login policy: baseCustomLoginPolicy
// with override applied on top (e.g. {"forceMfa": true} or
// {"forceMfaLocalOnly": true}) — shared by both the forceMfa and
// forceMfaLocalOnly integration tests so the two do not maintain two
// near-duplicate field lists that could silently drift apart.
//
// orgID scopes the write to a specific org (#913 Task 4,
// TestIntegration_ForceMFAPolicyInAnotherOrg_HandsOffInsteadOfCompleting);
// "" preserves every pre-Task-4 caller's behaviour of writing the seed
// PAT's own org's policy. Whether the seed PAT's ADMINISTRATION
// permission — proven live against its OWN org (this file's header
// comment on skipUnlessSeedPATIsAvailable) — also lets it write ANOTHER
// org's policy via x-zitadel-orgid is inferred from symmetry with the
// login-client PAT's confirmed READ behaviour on the same header (design
// doc "What was unknown, and is not any more" #1), not separately
// observed for a WRITE with THIS credential. If it is refused, this call
// fails loudly here (managementAPICall's non-2xx require.Truef), before
// TestIntegration_ForceMFAPolicyInAnotherOrg_HandsOffInsteadOfCompleting
// ever reaches its own assertions — a clear, attributable failure rather
// than a confusing one three calls downstream.
func setOrgLoginPolicy(t *testing.T, env integrationEnv, seedToken, orgID string, override map[string]any) {
	t.Helper()
	policy := baseCustomLoginPolicy()
	for k, v := range override {
		policy[k] = v
	}
	managementAPICall(t, env, seedToken, orgID, http.MethodPost, loginPolicyPath, policy)
}

// resetOrgLoginPolicy restores the org's default login policy, and is
// deliberately IDEMPOTENT: DELETE on an already-default policy answers
// 404 "Login Policy not found" (verified live), so a naive
// unconditional DELETE would itself fail the SECOND time this is called
// — which matters here because every caller below invokes this BOTH
// explicitly mid-test (to prove a login succeeds again, Finding 5) AND
// via t.Cleanup (as the safety net if the explicit call is never
// reached). GET-first avoids ever issuing that DELETE on nothing.
//
// The DELETE path is still followed by a GET asserting isDefault:true —
// this call VERIFIES the restore, not just trusts a 200 — so a restore
// that silently no-ops or leaves a different custom policy behind fails
// loudly here rather than being discovered by the next developer's login
// mysteriously requiring MFA.
//
// orgID scopes both the GET and the DELETE to a specific org (#913 Task
// 4), matching setOrgLoginPolicy's own orgID parameter — see that
// function's doc comment for the same live-write assumption this shares.
func resetOrgLoginPolicy(t *testing.T, env integrationEnv, seedToken, orgID string) {
	t.Helper()
	got := managementAPICall(t, env, seedToken, orgID, http.MethodGet, loginPolicyPath, nil)
	if policy, _ := got["policy"].(map[string]any); policy != nil {
		if isDefault, _ := policy["isDefault"].(bool); isDefault {
			return
		}
	}

	managementAPICall(t, env, seedToken, orgID, http.MethodDelete, loginPolicyPath, nil)

	got = managementAPICall(t, env, seedToken, orgID, http.MethodGet, loginPolicyPath, nil)
	policy, _ := got["policy"].(map[string]any)
	isDefault, _ := policy["isDefault"].(bool)
	require.Truef(t, isDefault, "org login policy (org=%q) after DELETE %s is not back to default: %v", orgID, loginPolicyPath, got)
}

// assertHandsOffWithoutCallback is the shared assertion both the
// forceMfa and forceMfaLocalOnly tests below make: a real login against
// a policy that requires MFA must return 200 + handoff_url, and must
// NEVER return callback_url — the latter would mean CompleteIfSufficient
// finalized a password-only session under a policy that said not to,
// which is the MFA bypass this whole fix round exists to prevent.
func assertHandsOffWithoutCallback(t *testing.T, w *httptest.ResponseRecorder, policyField string) {
	t.Helper()
	require.Equalf(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	require.Containsf(t, w.Body.String(), "handoff_url", "%s=true must hand off, body: %s", policyField, w.Body.String())
	require.NotContainsf(t, w.Body.String(), "callback_url",
		"%s=true completed the login (callback_url present) instead of handing off — MFA bypass: %s",
		policyField, w.Body.String())
}

// assertLoginSucceeds is the OTHER half of Finding 5's fix: a test that
// only proves "MFA policy → handoff" says nothing about whether the
// policy read itself still WORKS — handoff is also what an UNREADABLE
// policy produces (CompleteIfSufficient's own fail-closed branch,
// sufficiency.go), so a regression that broke the anchor
// (passwordCheckLifetime renamed, say) would make this file's MFA tests
// keep passing while EVERY login on the system silently degraded to
// hosted-UI handoff. Calling this in the SAME test, after the policy is
// restored, closes that gap: a real callback_url with code/state proves
// the policy read path still resolves to "recognized, MFA off" for the
// default policy, not just "produced a handoff for some reason".
//
// loginName/password generalize this beyond devSeededEmail/devSeededPassword
// (#913 Task 4) — TestIntegration_ForceMFAPolicyInAnotherOrg_HandsOffInsteadOfCompleting
// calls this against a second-org user, not the dev-seeded one, to prove
// spec D5's other half: that the ORG-SCOPED policy read genuinely
// resolves to "recognized, MFA off" for a real org, not just for the
// login client's own org.
//
// failureContext is prepended, verbatim, to every require message this
// function raises — empty for the pre-Task-4 callers (their failure is
// self-explanatory: "login broke"), but load-bearing for the cross-org
// call: see that test's own doc comment on why a failure here can mean
// something very different from "the feature is broken" and must say so
// at the point of failure, not just in a comment two hundred lines away
// that nobody reads mid-CI-run.
func assertLoginSucceeds(t *testing.T, env integrationEnv, loginName, password, failureContext string) {
	t.Helper()
	r := newIntegrationRouter(t, env)
	authRequestID := newAuthRequest(t, env)
	w := postPasswordReal(t, r, authRequestID, loginName, password)

	require.Equalf(t, http.StatusOK, w.Code, "%sbody: %s", failureContext, w.Body.String())
	var body struct {
		CallbackURL string `json:"callback_url"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	callback, err := url.Parse(body.CallbackURL)
	require.NoErrorf(t, err, "%scallback_url %q did not parse as a URL", failureContext, body.CallbackURL)
	q := callback.Query()
	require.NotEmptyf(t, q.Get("code"), "%scallback_url %q carried no code param after policy restore", failureContext, body.CallbackURL)
	require.NotEmptyf(t, q.Get("state"), "%scallback_url %q carried no state param after policy restore", failureContext, body.CallbackURL)
}

// TestIntegration_ForceMFAPolicy_HandsOffInsteadOfCompleting is Finding
// 2 of this task's first review round: a unit fixture can pin what
// loginclient.LoginPolicy DOES with a given body, but it cannot catch a
// REAL Zitadel upgrade that renames or re-casts forceMfa on the wire,
// because whoever makes that change is also the one who would update the
// fixture. This test closes that gap by reading the ACTUAL live policy
// object through the ACTUAL decode path client.go uses, with forceMfa
// genuinely set to true on the org — so a rename upstream breaks THIS
// test's assertions (login completes when it must not) regardless of
// what any fixture says.
//
// Finding 5 of the SECOND review round: this test now also proves login
// still WORKS once the policy is restored (assertLoginSucceeds below,
// called in the SAME test rather than left as an implicit dependency on
// another test elsewhere in the file/package running afterward) — see
// assertLoginSucceeds's own doc comment for why "hands off" alone is not
// enough evidence that the policy READ path, as opposed to just the
// MFA-enforcement branch, is healthy.
//
// # This test MUTATES shared Zitadel instance state
//
// Setting the org login policy is instance-wide within this dev org, not
// scoped to one test — every other integration test in this file (and
// any other real login against this dev stack, including a developer's
// own `make dev-api`/`dev-web` session running at the same time) reads
// the SAME policy object. Safeguards, all load-bearing:
//
//  1. t.Cleanup is registered IMMEDIATELY after the policy is set (before
//     ANY assertion below), so a failed assertion or an unexpected panic
//     still restores the default policy — the alternative (restoring
//     only at the end of the test body) would leave forceMfa on,
//     silently, for every subsequent login on the machine, which reads
//     as a broken app rather than a broken test.
//  2. The test ALSO calls resetOrgLoginPolicy explicitly, mid-body,
//     before assertLoginSucceeds — resetOrgLoginPolicy is idempotent
//     (see its own doc comment), so this and the t.Cleanup call do not
//     conflict; the explicit call is what makes assertLoginSucceeds's
//     precondition (default policy) true within THIS test rather than
//     relying on t.Cleanup having already run, which it has not at that
//     point in the test body.
//  3. This package's tests are NOT parallel: nothing in this file (or
//     loginui_test.go) calls t.Parallel(), so Go's default sequential
//     execution within one package is what actually serializes this
//     against TestIntegration_PasswordSuccess_ReturnsCallbackURLWithCodeAndState
//     and TestIntegration_PasswordFailures_WrongPasswordAndUnknownUserAreByteIdentical
//     above — both of which log in against the SAME org and would
//     otherwise race a forceMfa flip mid-test. If a future change adds
//     t.Parallel() anywhere in this package, this test's isolation
//     assumption breaks silently; nothing currently enforces that beyond
//     this comment.
func TestIntegration_ForceMFAPolicy_HandsOffInsteadOfCompleting(t *testing.T) {
	env := skipUnlessDevStackIsUp(t)
	seedToken := skipUnlessSeedPATIsAvailable(t)

	setOrgLoginPolicy(t, env, seedToken, "", map[string]any{"forceMfa": true})
	t.Cleanup(func() { resetOrgLoginPolicy(t, env, seedToken, "") })

	r := newIntegrationRouter(t, env)
	authRequestID := newAuthRequest(t, env)
	w := postPasswordReal(t, r, authRequestID, devSeededEmail, devSeededPassword)
	assertHandsOffWithoutCallback(t, w, "forceMfa")

	resetOrgLoginPolicy(t, env, seedToken, "")
	assertLoginSucceeds(t, env, devSeededEmail, devSeededPassword, "")
}

// TestIntegration_ForceMFALocalOnlyPolicy_HandsOffInsteadOfCompleting is
// Finding 4 of the second review round: forceMfaLocalOnly is a REAL
// Zitadel login-policy field the unit tests now cover with fixtures, but
// — for the exact same reason TestIntegration_ForceMFAPolicy_... exists
// alongside the forceMfa unit fixtures — only a live read through the
// real decode path can prove Helivanta actually treats a genuinely-configured
// forceMfaLocalOnly:true org as requiring MFA, not just that a
// hand-written fixture says it should. See loginclient.LoginPolicy's
// "forceMfaLocalOnly — a second, REAL field" doc comment for the
// fold-together decision this test is proving live.
//
// Same MUTATES-shared-state safeguards as
// TestIntegration_ForceMFAPolicy_HandsOffInsteadOfCompleting above
// (t.Cleanup registered immediately, resetOrgLoginPolicy's idempotency,
// and this package's tests never running in parallel) — not repeated
// here in full; see that test's doc comment.
func TestIntegration_ForceMFALocalOnlyPolicy_HandsOffInsteadOfCompleting(t *testing.T) {
	env := skipUnlessDevStackIsUp(t)
	seedToken := skipUnlessSeedPATIsAvailable(t)

	// forceMfa stays false — that is the whole point: this policy must
	// require MFA through forceMfaLocalOnly ALONE, the exact shape
	// Finding 4 found reachable through supported Zitadel configuration.
	setOrgLoginPolicy(t, env, seedToken, "", map[string]any{"forceMfa": false, "forceMfaLocalOnly": true})
	t.Cleanup(func() { resetOrgLoginPolicy(t, env, seedToken, "") })

	r := newIntegrationRouter(t, env)
	authRequestID := newAuthRequest(t, env)
	w := postPasswordReal(t, r, authRequestID, devSeededEmail, devSeededPassword)
	assertHandsOffWithoutCallback(t, w, "forceMfaLocalOnly")

	resetOrgLoginPolicy(t, env, seedToken, "")
	assertLoginSucceeds(t, env, devSeededEmail, devSeededPassword, "")
}

const importUserPath = "/management/v1/users/human/_import"

// createImportedUser creates a fresh human user via the ONE endpoint on
// v4.15.3 that accepts a password at creation time and lets a caller set
// passwordChangeRequired — POST /management/v1/users/human/_import. Task
// 8's spike work found that POST /management/v1/users/human SILENTLY
// DROPS unrecognized fields including passwordChangeRequired and answers
// 200 anyway; this sibling endpoint is the one that actually took the
// value (verified live: GET /v2/users/{id} echoed
// human.passwordChangeRequired back). loginName is made unique per call
// (embeds time.Now().UnixNano()) so repeated test runs never collide on
// an existing username. t.Cleanup deletes the user via deleteAndVerify,
// the same "verify the restore, don't trust the 200" discipline
// resetOrgLoginPolicy already applies to policy state — this repo's
// practice, not just this file's.
//
// orgID scopes the import to a specific org (#913 Task 4) via
// managementAPICall's own orgID parameter; "" preserves every pre-Task-4
// caller's behaviour of importing into the seed PAT's own org. The seed
// PAT is IAM_OWNER-scoped (skipUnlessSeedPATIsAvailable's doc comment),
// which per Zitadel's own permission model is an INSTANCE-wide role, not
// an org-membership one — importing into an org other than the seed
// bot's own is expected to be within that role's reach, but (like
// setOrgLoginPolicy's org-scoped WRITE) this specific combination was
// not separately observed live before this change; a refusal here fails
// loudly via managementAPICall's own non-2xx check.
func createImportedUser(t *testing.T, env integrationEnv, seedToken, orgID string) (userID, loginName string) {
	t.Helper()
	loginName = fmt.Sprintf("task8-test-%d@helivanta.dev", time.Now().UnixNano())
	resp := managementAPICall(t, env, seedToken, orgID, http.MethodPost, importUserPath, map[string]any{
		"userName": loginName,
		"profile":  map[string]any{"firstName": "Task8", "lastName": "IntegrationTest"},
		"email":    map[string]any{"email": loginName, "isEmailVerified": true},
		"password": devSeededPassword,
	})
	id, _ := resp["userId"].(string)
	require.NotEmptyf(t, id, "user import response carried no userId: %v", resp)
	t.Cleanup(func() { deleteAndVerifyUser(t, env, seedToken, id) })
	return id, loginName
}

// deleteAndVerifyUser deletes a user (DELETE /v2/users/{id}, verified
// live 2026-08-16 to exist and succeed on v4.15.3) and then reads it back
// to PROVE the delete took, not just that it answered 200 — the same
// verify-the-restore discipline resetOrgLoginPolicy applies to policy
// state, applied here to the test users this file creates so none of
// them are left in the shared dev org afterward.
func deleteAndVerifyUser(t *testing.T, env integrationEnv, seedToken, userID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, env.issuer+"/v2/users/"+userID, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+seedToken)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equalf(t, http.StatusOK, resp.StatusCode, "DELETE /v2/users/%s did not succeed", userID)

	verifyReq, err := http.NewRequestWithContext(ctx, http.MethodGet, env.issuer+"/v2/users/"+userID, nil)
	require.NoError(t, err)
	verifyReq.Header.Set("Authorization", "Bearer "+seedToken)
	verifyResp, err := http.DefaultClient.Do(verifyReq)
	require.NoError(t, err)
	_ = verifyResp.Body.Close()
	require.NotEqualf(t, http.StatusOK, verifyResp.StatusCode,
		"user %s still readable (HTTP %d) after DELETE: cleanup did not take", userID, verifyResp.StatusCode)
}

// TestIntegration_UserEnrolledFactor_HandsOffInsteadOfCompleting is #854
// Task 8's second unknown, resolved: GET
// /v2/users/{id}/authentication_methods (found by reading the v4.15.3
// proto's google.api.http annotations and confirmed live with the
// login-client PAT — the earlier-guessed
// /v2/users/{id}/authentication_factors and
// /management/v1/users/{id}/auth_factors both answered 404) reflects a
// user's own enrolled second factor even when the ORG policy does not
// force MFA. This test creates a throwaway user, enrolls OTP email
// (POST /v2/users/{id}/otp_email — the one factor type that needs no
// separate verification step, unlike TOTP/U2F/passkeys, so this test
// can enroll it in one call), and proves a password-only login for that
// user hands off rather than completing — the exact bypass
// loginclient.CompleteIfSufficient's KNOWN LIMITATIONS §1 used to warn
// about before this task closed it.
//
// The default org login policy is untouched by this test (no forceMfa
// anywhere here) — the handoff below is entirely the per-user factor
// check's doing, which is the point.
func TestIntegration_UserEnrolledFactor_HandsOffInsteadOfCompleting(t *testing.T) {
	env := skipUnlessDevStackIsUp(t)
	seedToken := skipUnlessSeedPATIsAvailable(t)

	userID, loginName := createImportedUser(t, env, seedToken, "")
	managementAPICall(t, env, seedToken, "", http.MethodPost, "/v2/users/"+userID+"/otp_email", map[string]any{})

	r := newIntegrationRouter(t, env)
	authRequestID := newAuthRequest(t, env)
	w := postPasswordReal(t, r, authRequestID, loginName, devSeededPassword)
	assertHandsOffWithoutCallback(t, w, "per-user enrolled factor (otp_email)")
}

// createOrgPath and orgPathPrefix are Zitadel's v1 management-API org
// endpoints (#913 Task 4). Every other resource this file creates
// (import user, login policy) uses the SAME /management/v1/... family,
// so org create/delete/read follow it too rather than mixing in the v2
// endpoints deleteAndVerifyUser uses for users — that split exists for
// users because Task 8's own spike work found a SPECIFIC reason
// (v1's human/_import is the only endpoint that accepts a password at
// creation time; v2's DELETE /v2/users/{id} was separately confirmed
// live) to prefer v2 for the delete half. No equivalent reason is known
// for orgs, so this stays uniformly v1 rather than inventing a second
// split with no evidence behind it.
//
// # UNVERIFIED WIRE SHAPE — this is a guess, not an observed fact
//
// Nothing else in this repo creates a Zitadel org: this dev stack's ONE
// org (TESSERIX, the seed PAT's own resource owner) is provisioned by
// Zitadel itself at first boot (FirstInstance.Org, docker-compose.dev.yml),
// never by application code — so, unlike every other endpoint this file
// touches, there is no prior scripts/*.mjs or *_test.go call this borrows
// its shape from BYTE FOR BYTE. What follows is inferred by PATTERN from
// a call this repo DOES make and has observed: scripts/zitadel-bootstrap.mjs's
// `POST /management/v1/projects` (a sibling v1 top-level resource,
// created the same way) returns its new id as `created.id` — read there
// (this file's directory listing was checked to confirm that usage
// exists, not merely remembered) — so `POST /management/v1/orgs` is
// assumed to follow the SAME response shape: {"id": "..."}. If Zitadel
// actually nests it (e.g. {"org": {"id": "..."}}) or names it
// differently ("orgId"), the require.NotEmptyf below fails LOUDLY with
// the full decoded body, not silently proceeding with an empty orgID
// that would make every downstream call in this test target "no org"
// (today's unscoped behaviour) rather than the org this test just tried
// to create — which would make
// TestIntegration_ForceMFAPolicyInAnotherOrg_HandsOffInsteadOfCompleting
// pass or fail for a reason that has nothing to do with #913.
const (
	createOrgPath = "/management/v1/orgs"
	orgPathPrefix = "/management/v1/orgs/"
)

// createOrgWithUser provisions a throwaway second org plus one human user
// inside it (#913 Task 4, design doc D5 steps 1-2): a second-org user is
// exactly what proves the login-client PAT's policy read is scoped to
// the AUTHENTICATING USER's org, not the login client's own org — a
// forceMfa flip on the seed PAT's own org (what every OTHER test in this
// file mutates) cannot distinguish "scoped correctly" from "always reads
// the same org" the way a genuinely different org can.
//
// t.Cleanup for the org is registered BEFORE createImportedUser runs, so
// it is the LAST cleanup to fire (Go's t.Cleanup is LIFO): the imported
// user's own t.Cleanup (registered inside createImportedUser, after this
// one) deletes the user first, then this one deletes the now-empty org —
// the order a human operator would use by hand, and the one least likely
// to leave an orphaned user behind if the org's own delete were to fail
// partway for an unrelated reason.
func createOrgWithUser(t *testing.T, env integrationEnv, seedToken string) (orgID, loginName string) {
	t.Helper()

	orgName := fmt.Sprintf("helivanta-task4-second-org-%d", time.Now().UnixNano())
	resp := managementAPICall(t, env, seedToken, "", http.MethodPost, createOrgPath, map[string]any{"name": orgName})
	orgID, _ = resp["id"].(string)
	require.NotEmptyf(t, orgID,
		"POST %s response carried no \"id\" field — the org-creation wire shape guessed in this "+
			"helper's doc comment is wrong; inspect this body to find the real field and fix "+
			"createOrgWithUser before re-running: %v", createOrgPath, resp)
	t.Cleanup(func() { deleteAndVerifyOrg(t, env, seedToken, orgID) })

	_, loginName = createImportedUser(t, env, seedToken, orgID)
	return orgID, loginName
}

// deleteAndVerifyOrg deletes a throwaway org and reads it back to PROVE
// the delete took, matching deleteAndVerifyUser's and
// resetOrgLoginPolicy's "verify the restore, never trust the 200"
// discipline (#913 Task 4).
//
// The verify step's exact PASS condition is itself an UNVERIFIED guess:
// this repo has never observed what GET /management/v1/orgs/{id}
// returns for a deleted org. Two shapes are plausible from general
// Zitadel v1 behaviour — a 404 (matching deleteAndVerifyUser's v2
// observation) or a 200 with the org's state field reporting removal —
// so this checks for EITHER rather than committing to one guess and
// risking a false failure on a live run. What it refuses to accept
// silently is a 200 with a state this code does not recognize as
// "removed": that path fails loudly with the full body printed, rather
// than treating an unrecognized 200 as success by default — the same
// "recognize it or fail closed" posture LoginPolicy's own rename/re-cased
// guard takes in client.go, applied here to a different unverified shape.
func deleteAndVerifyOrg(t *testing.T, env integrationEnv, seedToken, orgID string) {
	t.Helper()
	managementAPICall(t, env, seedToken, "", http.MethodDelete, orgPathPrefix+orgID, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, env.issuer+orgPathPrefix+orgID, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+seedToken)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// The deleteAndVerifyUser shape: gone means gone, no body worth
		// decoding.
		return
	}
	var parsed map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&parsed) // best-effort, for the failure message only
	org, _ := parsed["org"].(map[string]any)
	state, _ := org["state"].(string)
	require.Equalf(t, "ORG_STATE_REMOVED", state,
		"org %s still readable (HTTP 200, state %q) after DELETE %s — either cleanup did not take, "+
			"or this helper's guess at the removed-org state name/shape is wrong (see deleteAndVerifyOrg's "+
			"doc comment): %v", orgID, state, orgPathPrefix+orgID, parsed)
}

// secondOrgPolicyReadFailureContext is prepended to every assertLoginSucceeds
// failure message in TestIntegration_ForceMFAPolicyInAnotherOrg_HandsOffInsteadOfCompleting's
// second half — see that test's own doc comment for why THIS assertion,
// not the handoff one before it, is what actually proves the org-scoped
// policy read resolved to a real value rather than an error.
const secondOrgPolicyReadFailureContext = "SECOND-ORG LOGIN DID NOT SUCCEED AFTER forceMfa WAS RESET TO false ON " +
	"THAT ORG. This is design doc D5's load-bearing assertion, not a redundant check: if the login-client " +
	"PAT is refused when it tries to read another org's policy via x-zitadel-orgid (open question (a) in " +
	"the #913 Task 4 report), the read errors, CompleteIfSufficient's fail-closed branch hands off " +
	"REGARDLESS of the actual policy value, and the EARLIER assertHandsOffWithoutCallback call in this same " +
	"test would have passed for the WRONG reason — proving nothing about #913's fix, only that errors fail " +
	"closed (which was already true before the fix). A genuinely completed login here is the only thing " +
	"that proves the scoped read resolved forceMfa's real value instead of merely erroring. "

// TestIntegration_ForceMFAPolicyInAnotherOrg_HandsOffInsteadOfCompleting is
// design doc D5's live proof for #913: every other MFA test in this file
// (TestIntegration_ForceMFAPolicy_HandsOffInsteadOfCompleting,
// TestIntegration_ForceMFALocalOnlyPolicy_HandsOffInsteadOfCompleting)
// flips the login CLIENT's own org's policy and logs in as
// devSeededEmail, a user IN that same org — which can prove the policy
// read reaches Zitadel and is decoded correctly, but CANNOT prove the
// read is scoped to the authenticating user's org rather than always
// resolving to the login client's own org, because in those two tests
// the two orgs are the same org. This test makes them different orgs on
// purpose:
//
//  1. createOrgWithUser provisions a throwaway second org and a human
//     user inside it.
//  2. setOrgLoginPolicy(..., secondOrgID, ...) sets forceMfa:true on
//     THAT org — the login client's own org (TESSERIX) is left entirely
//     untouched, still at its default forceMfa:false.
//  3. The second-org user logs in through the real
//     POST /v1/auth/login/password. On the PRE-#913 code (the unscoped
//     GET /management/v1/policies/login, no x-zitadel-orgid at all) this
//     read resolves against the login client's OWN org — sees
//     forceMfa:false — and completes the password-only session: a real
//     callback_url for a user whose own org says MFA is mandatory. THAT
//     is the exact authentication-bypass design doc D5 exists to catch
//     end to end, not just in a fixture. assertHandsOffWithoutCallback
//     below is what fails on that unpatched behaviour — a handoff_url
//     with no callback_url is the ONLY passing shape once #913's fix
//     (LoginPolicyForOrg scoped by the session's own
//     factors.user.organizationId) is in place.
//  4. The org's policy is then reset to forceMfa:false and the SAME user
//     logs in again — assertLoginSucceeds, with
//     secondOrgPolicyReadFailureContext explaining exactly what a
//     failure here would mean (see that const's doc comment, and D5's
//     own "Two live unknowns" section): without this half, a login-client
//     PAT that is refused when reading ANOTHER org's policy would still
//     make step 3 pass — fail-closed, but for the wrong reason, and
//     silently non-functional for every real second-org user on this
//     instance.
//
// # This test MUTATES a THROWAWAY org's policy, not the shared default org
//
// Unlike TestIntegration_ForceMFAPolicy_HandsOffInsteadOfCompleting (whose
// own doc comment explains at length why mutating the SHARED default
// org's policy needs three separate safeguards), this test's
// setOrgLoginPolicy call targets an org t.Cleanup deletes at the end of
// this test — no other test, and no developer's own concurrent dev
// session, reads or writes this org's policy at any point, so none of
// those three safeguards apply here. t.Cleanup is still registered
// immediately after the policy is set (before any assertion), on the
// same "a failed assertion must not skip cleanup" principle, but its
// job is narrower: leaving one fewer throwaway org behind, not protecting
// shared instance state.
//
// # Two live unknowns this test cannot resolve from this environment
//
// Both are documented in depth on the helpers this test calls
// (managementAPICall, setOrgLoginPolicy, createImportedUser,
// createOrgWithUser) and in the #913 Task 4 report (docs
// .superpowers/sdd/2026-08-20-login-policy-org-scope/task-4-report.md):
// this test was written and go-vet-verified, but NEVER RUN, because the
// local dev Zitadel stack could not be started in this environment
// (Docker was unavailable). Whoever runs this first should watch for:
//
//   - The login-client PAT being refused when it reads the SECOND org's
//     policy (secondOrgPolicyReadFailureContext explains the exact
//     failure mode this produces, and why assertion 4 — not 3 — is what
//     catches it).
//   - Whether the Helivanta OIDC app's project admits a user from a
//     second org at all. If Zitadel's project settings require an
//     explicit org grant before a second-org user can sign in to this
//     app, this test does NOT provision one — no live call in this repo
//     has ever exercised the project-grant endpoint, and guessing its
//     request shape blind (unlike the org-creation guess above, which at
//     least has a same-family sibling call to pattern-match against)
//     risks adding a SECOND wrong guess on top of a real unknown rather
//     than surfacing it clearly. If that is the blocker, it surfaces as
//     an assertHandsOffWithoutCallback failure with a body that is
//     neither "handoff_url present" nor a clean password-mismatch shape
//     — read the printed body, not just the require message, to tell
//     that apart from an MFA-related failure, and provision the grant
//     (POST /management/v1/projects/{id}/grants against the Helivanta
//     project, granting it to secondOrgID — verify the exact body
//     against Zitadel's own API reference, not this comment) before
//     re-running.
func TestIntegration_ForceMFAPolicyInAnotherOrg_HandsOffInsteadOfCompleting(t *testing.T) {
	env := skipUnlessDevStackIsUp(t)
	seedToken := skipUnlessSeedPATIsAvailable(t)

	secondOrgID, loginName := createOrgWithUser(t, env, seedToken)

	setOrgLoginPolicy(t, env, seedToken, secondOrgID, map[string]any{"forceMfa": true})
	t.Cleanup(func() { resetOrgLoginPolicy(t, env, seedToken, secondOrgID) })

	r := newIntegrationRouter(t, env)
	authRequestID := newAuthRequest(t, env)
	w := postPasswordReal(t, r, authRequestID, loginName, devSeededPassword)
	assertHandsOffWithoutCallback(t, w, "forceMfa (second org, not the login client's own org)")

	resetOrgLoginPolicy(t, env, seedToken, secondOrgID)
	assertLoginSucceeds(t, env, loginName, devSeededPassword, secondOrgPolicyReadFailureContext)
}

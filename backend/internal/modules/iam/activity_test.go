package iam_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/modules/iam" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/internal/testutil"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/ratelimit"
	"github.com/tesserix/hms/pkg/session"
)

// activitySession is a snapshot of the identity a request into
// POST /v1/auth/session/activity carried, or the identity the response
// re-minted — the same struct shape serves both directions so
// TestActivityCarriesSubjectTenantAndAuthTimeUnchanged can compare one
// against the other directly.
type activitySession struct {
	Subject  string
	TenantID string
	AuthTime time.Time
	Deadline time.Time
}

// activityEnv wires a real session.Signer/Verifier pair into
// testutil.NewHarness (mirroring me_test.go's switchHarness/meEnv), plus
// deps.IdleTimeout and deps.Limiter/ActivityRateLimit so the activity
// route under test is dependency-complete — a nil signer or a zero
// IdleTimeout would make every one of these tests fail for a reason
// that has nothing to do with what each one asserts.
type activityEnv struct {
	r           *gin.Engine
	verifier    *session.Verifier
	principal   *authn.Principal
	idleTimeout time.Duration
}

func newActivityEnv(t *testing.T) *activityEnv {
	t.Helper()
	p := &authn.Principal{
		Subject:  "user-jane",
		TenantID: testutil.TenantA,
		// Deliberately far in the past, mirroring
		// me_test.go's fixedAuthTimeVerifier/originalAuthTime: with
		// auth_time stamped moments before the request (as
		// testutil.StaticVerifier's default does), "carried through"
		// and "reset to the mint time" are both "close to now" and the
		// assertion below could not tell them apart. See
		// TestActivityDoesNotResetAuthTime.
		AuthTime: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	signer, verifier := testutil.NewSessionSignerForTest(t)
	roles := &fakeRoleLister{bindings: map[string][]authz.RoleBinding{
		"user-jane": {{TenantID: testutil.TenantA, Role: authz.RoleNurse}},
	}}
	const idleTimeout = 15 * time.Minute
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		// idleDeadlineVerifier is defined in me_test.go: it reports *p,
		// so activityEnv.sessionWithDeadline can mutate the principal's
		// IdleDeadline AFTER the harness (and its middleware chain) is
		// already built — the same trick TestTenantSwitchCarriesTheIdleDeadlineForward
		// relies on.
		Verifier:      idleDeadlineVerifier{p: p},
		Tokens:        map[string]string{"jane": testutil.TenantA},
		Perms:         map[string][]authz.Permission{"jane": {}},
		Writer:        &recordingWriter{},
		Roles:         roles,
		SessionSigner: signer,
		// TestSessionTTL, not idleTimeout: the token's own exp and the
		// idle_deadline it separately carries are independent clocks
		// (spec D2) and must be seen to be independent — sharing one
		// duration here would make this test suite unable to tell a bug
		// that conflates them from one that doesn't.
		SessionTTL:          testutil.TestSessionTTL,
		SessionSecureCookie: true,
		IdleTimeout:         idleTimeout,
		Limiter:             ratelimit.NewMemory(1000),
		// Generous budget: the point of these tests is the deadline
		// arithmetic and the 401 refusal, not the rate limiter — a
		// tight budget here would make an unrelated test flaky.
		ActivityRateLimit: ratelimit.Rule{Rate: 1000, Burst: 1000, Per: time.Minute},
		Modules:           []platform.Module{iam.New(nil)},
	})
	return &activityEnv{r: r, verifier: verifier, principal: p, idleTimeout: idleTimeout}
}

// sessionWithDeadline sets the deadline the caller's session carries
// INTO the activity request (via the shared *authn.Principal
// idleDeadlineVerifier reports) and returns a snapshot of the identity
// that session belongs to, truncated to whole seconds — the resolution
// idle_deadline actually round-trips at (pkg/session's tokenClaims), so
// a sub-second component here could never survive a mint/verify cycle
// and comparing against it would fail for a reason unrelated to what
// each test is about.
func (e *activityEnv) sessionWithDeadline(t *testing.T, deadline time.Time) activitySession {
	t.Helper()
	deadline = deadline.UTC().Truncate(time.Second)
	e.principal.IdleDeadline = deadline
	return activitySession{
		Subject:  e.principal.Subject,
		TenantID: e.principal.TenantID,
		AuthTime: e.principal.AuthTime,
		Deadline: deadline,
	}
}

// post issues the actual POST /v1/auth/session/activity request. The
// session argument is not read here — the identity and deadline it
// carries already reached the harness through e.principal, mutated by
// sessionWithDeadline above — but it is still a parameter, not
// discarded at the call site, so every call site reads as "post THIS
// session" rather than leaving the dependency implicit.
func (e *activityEnv) post(t *testing.T, _ activitySession) *httptest.ResponseRecorder {
	t.Helper()
	return testutil.Do(e.r, http.MethodPost, "/v1/auth/session/activity", "jane", "")
}

// deadlineFromBody decodes the response body's idle_deadline field —
// the ONLY channel carrying the new deadline back to a caller that
// cannot read the (httpOnly) session cookie itself.
func (e *activityEnv) deadlineFromBody(t *testing.T, rec *httptest.ResponseRecorder) time.Time {
	t.Helper()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var body struct {
		IdleDeadline time.Time `json:"idle_deadline"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body.IdleDeadline
}

// reissued decodes the re-minted session cookie the response set, using
// a REAL session.Verifier (not a fake) — the point of every "carried
// through unchanged" assertion below is that the actual signed token
// says so, not a handler-internal value a bug could diverge from what
// it minted.
func (e *activityEnv) reissued(t *testing.T, rec *httptest.ResponseRecorder) activitySession {
	t.Helper()
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	claims, err := e.verifier.Verify(sessionCookie(t, rec))
	require.NoError(t, err)
	return activitySession{
		Subject:  claims.Subject,
		TenantID: claims.TenantID,
		AuthTime: claims.AuthTime,
		Deadline: claims.IdleDeadline,
	}
}

// TestActivityExtendsTheIdleDeadline is spec D4's whole point: a
// deliberate activity call must push the deadline OUT, not merely
// leave it where it was (that is every other re-mint's job — login's
// renewal and tenant switch — and this is the one exception).
//
// before is 3 minutes out and idleTimeout is 15 minutes (activityEnv),
// so "got.After(before)" is true by roughly 12 minutes — comfortably
// outside any wall-clock jitter this test's own execution could
// introduce, unlike login.go's renewal test, which needed an injectable
// clock because its two "now + IdleTimeout" computations could land in
// the same second.
func TestActivityExtendsTheIdleDeadline(t *testing.T) {
	env := newActivityEnv(t)
	before := time.Now().Add(3 * time.Minute)
	rec := env.post(t, env.sessionWithDeadline(t, before))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	got := env.deadlineFromBody(t, rec)
	if !got.After(before) {
		t.Errorf("idle_deadline = %v, want later than %v", got, before)
	}
}

// TestActivityCarriesSubjectTenantAndAuthTimeUnchanged proves this
// handler re-mints for the SAME identity it was called with — moving
// only the deadline, laundering nothing else.
func TestActivityCarriesSubjectTenantAndAuthTimeUnchanged(t *testing.T) {
	env := newActivityEnv(t)
	before := env.sessionWithDeadline(t, time.Now().Add(3*time.Minute))
	after := env.reissued(t, env.post(t, before))
	if after.Subject != before.Subject || after.TenantID != before.TenantID || !after.AuthTime.Equal(before.AuthTime) {
		t.Errorf("activity changed identity: got %+v, want sub/tenant/auth_time from %+v", after, before)
	}
}

// TestActivityDoesNotResetAuthTime asserts the auth_time claim alone,
// isolated from the identity check above: auth_time must never be
// laundered into a fresh one — the #781 revocation watermark compares
// against exactly that value, so resetting it here would let an
// "I'm active" signal quietly walk a session through a revocation made
// after the original sign-in.
//
// before.AuthTime is 2020-01-01 (activityEnv), six years in the past —
// deliberately far from "now" so a mutation that reset auth_time to
// h.now() at mint time is unmistakable, not merely "close to now" (see
// me_test.go's fixedAuthTimeVerifier doc comment on why a near-now
// fixture cannot discriminate this).
func TestActivityDoesNotResetAuthTime(t *testing.T) {
	env := newActivityEnv(t)
	before := env.sessionWithDeadline(t, time.Now().Add(3*time.Minute))
	after := env.reissued(t, env.post(t, before))
	if !after.AuthTime.Equal(before.AuthTime) {
		t.Errorf("auth_time changed: got %v, want %v (unchanged)", after.AuthTime, before.AuthTime)
	}
}

// TestActivityRefusesAnAlreadyIdleSession is the load-bearing refusal:
// an already-idle-expired session must not be revivable by claiming
// activity, or the whole #848 control is one request away from
// bypassed. This handler never even runs for such a request —
// authn.Middleware refuses it first (session_idle, 401) — which is WHY
// activity.go carries no idle-deadline check of its own; see Step 5 of
// the task report for the mutation that proves this test actually
// depends on that upstream refusal.
func TestActivityRefusesAnAlreadyIdleSession(t *testing.T) {
	env := newActivityEnv(t)
	rec := env.post(t, env.sessionWithDeadline(t, time.Now().Add(-1*time.Second)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 — an expired session must not be revivable by claiming activity", rec.Code)
	}
}

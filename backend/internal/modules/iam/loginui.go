package iam

import (
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/ratelimit"
)

// MinFailedLoginDuration is the floor every FAILED password attempt must
// take before responding, wrong password or unknown user alike (spec
// D5). It exists to close a timing oracle, not just a status/body one: a
// wrong password reaches Zitadel's password hash, an unknown user never
// does, and the gap between them tells an attacker which login names
// have accounts at all — for a hospital, that is itself sensitive
// information (who works there) — regardless of what the status code or
// body say.
//
// MEASURED 2026-08-16 against the real dev Zitadel (v4.15.3, PAT-backed
// login-client API, `go run` a throwaway harness over
// loginclient.Client.CreatePasswordSession — see this task's report for
// the program): 25 wrong-password attempts against test@helivanta.dev,
// spaced 4s apart so the sample reflects independent single attempts,
// not sustained hammering. The first 9 landed in a tight 809-845ms
// band (mean ~830ms) — consistent with the original spike's 0.72-0.78s,
// slightly higher on this host/network. That is the number this floor
// has to clear, and it is why the ORIGINAL 900ms was wrong: only ~55-90ms
// (~7-11%) of headroom over an observed ceiling is not "comfortable" —
// it is exactly the margin p99 network jitter (something an attacker can
// induce by loading the endpoint) eats first, which would let
// wrong-password drift back above the floor while unknown-user stays
// pinned exactly at it, reopening the same oracle in miniature. 1500ms
// clears the measured 845ms ceiling with ~650ms (~77%) of margin. There
// is no real UX cost to this being generous: the floor only ever
// extends a FAILED login, and a wrong password that already takes
// ~830ms to compute costing 1.5s total is not a tradeoff worth making
// against the oracle it closes.
//
// A SEPARATE, undefeated finding from the same measurement: attempts
// 10-25 escalated in lockstep — ~1.83s at #10-14, ~2.83s at #15-19,
// ~3.85s at #20-24, ~4.86s at #25 — identically whether attempts were
// back-to-back or spaced 4s apart, and it reset after one SUCCESSFUL
// login (verified separately: a correct-password call afterwards
// returned in ~850ms, session minted). That is Zitadel's own
// per-account brute-force backoff, keyed on accumulated failed
// attempts, not on request rate or elapsed time — and it is NOT
// something a static floor can track: past roughly the 10th consecutive
// failure against the SAME login name, the real wrong-password latency
// exceeds whatever this constant is set to, and this file's sleep-until-
// floor logic (respondEqualisedFailure) only ever waits UP to the floor,
// never truncates a slower real answer down to it — so from that point
// the two paths ARE distinguishable again by timing. This is accepted
// as a known, undefeated residual, not silently assumed away, for two
// reasons: (1) it only manifests after roughly ten failed attempts
// against one specific login name, which the endpoint's own IP-keyed
// rate limiter (Password's own budget, see NewLoginUIHandlers' doc
// comment) is expected to have refused well before, and (2) the signal
// it eventually leaks — "this account has accumulated recent failed
// attempts" — is far weaker than the oracle spec D5 targets in the
// first place ("this account exists at all"), since triggering it
// requires the attacker to already be sustaining an attack against a
// specific, chosen login name rather than sweeping many.
//
// MUST be revisited if Zitadel's password hash cost OR its lockout
// backoff schedule changes (a different algorithm, a higher work
// factor, a retuned lockout policy) — this constant is pinned to one
// measurement, not derived from anything self-adjusting.
const MinFailedLoginDuration = 1500 * time.Millisecond

// passwordFailureMessage is the ONE refusal body a wrong password and an
// unknown user both produce (spec D5). Equalising the status code and
// this message without also equalising timing (MinFailedLoginDuration)
// would look fixed while leaving the oracle fully intact — see this
// file's package-level tests.
const passwordFailureMessage = "email or password is incorrect"

// authRequestExpiredMessage answers ErrAuthRequestInvalid: the auth
// request id Zitadel was given either never existed, already completed,
// or expired. It is deliberately distinct from passwordFailureMessage —
// there is no enumeration risk in admitting an auth request has gone
// stale, and collapsing the two would just make a routine "your tab sat
// open too long" case unnecessarily confusing.
const authRequestExpiredMessage = "this sign-in attempt has expired; start again"

// zitadelUnavailableMessage answers ErrUnavailable: Zitadel could not be
// reached or answered with a server error. It is retryable, and
// intentionally NOT the same message as a credential refusal — telling a
// caller their password was wrong when the real cause is an outage would
// send them down the wrong remediation path (resetting a password that
// was never checked).
const zitadelUnavailableMessage = "sign-in is temporarily unavailable; please try again"

// LoginUIHandlers backs the three routes Helivanta's own login form drives
// directly against Zitadel's login-client API (plan #854 Task 4, spec
// D5): reading an auth request, checking a password, and handing off to
// Zitadel's hosted UI when Helivanta cannot complete the login itself. Like
// LoginHandlers (login.go), all three are mounted OUTSIDE the
// authenticated /v1 chain via bootstrap.MountUnauthenticated — there is
// no Helivanta session, and for Password specifically no verified subject at
// all, until AFTER it succeeds.
type LoginUIHandlers struct {
	client             *loginclient.Client
	hostedLoginBaseURL string
	limiter            ratelimit.Limiter
	limit              ratelimit.Rule
}

// NewLoginUIHandlers wires LoginUIHandlers' dependencies. client is
// Task 2's loginclient.Client, authenticated with the login-client PAT.
// hostedLoginBaseURL is Zitadel's own hosted login origin+path (e.g.
// http://localhost:20080/ui/v2/login per Task 1's finding that Zitadel
// APPENDS to whatever baseUri is configured) — Handoff and a Password
// call that resolves to OutcomeHandoff both build their handoff_url from
// it. limiter and limit are the budget shared by ALL THREE of this
// type's routes (#854 Task 4, spec D2: "all three sit behind the
// existing unauthenticated limiter"): reused from the SAME
// ratelimit.Limiter instance bootstrap.V1Chain and LoginHandlers already
// share, per #851 and docs/standards on not inventing a second limiter,
// each keyed under its OWN prefix (see allowedByLimiter) so no route can
// bleed into another's bucket. A nil limiter fails OPEN, mirroring
// LoginHandlers.Login's documented direction: this is a capacity
// control, and a limiter that cannot decide must not take sign-in down
// for a hospital.
func NewLoginUIHandlers(client *loginclient.Client, hostedLoginBaseURL string, limiter ratelimit.Limiter, limit ratelimit.Rule) *LoginUIHandlers {
	return &LoginUIHandlers{client: client, hostedLoginBaseURL: hostedLoginBaseURL, limiter: limiter, limit: limit}
}

// Rate-limit bucket prefixes, one per route. All three routes share ONE
// ratelimit.Limiter instance and ONE Rule (see NewLoginUIHandlers), but
// each gets its own key prefix so a flood against one cannot spend
// another's budget: a browser reading an auth request is not the same
// traffic as a browser guessing passwords, and neither should be able to
// lock the other out. The prefixes are also distinct from
// LoginHandlers.Login's "login:" bucket and ratelimit.Middleware's
// principal bucket, so none of the five can collide.
const (
	authRequestRateBucket = "login_auth_request:"
	passwordRateBucket    = "login_password:"
	handoffRateBucket     = "login_handoff:"
)

// allowedByLimiter is spec D2's "all three sit behind the existing
// unauthenticated limiter", applied identically by every route on this
// type. It answers false and has ALREADY written the 429 when the caller
// is over budget, so a handler's only job is to return.
//
// # Why all three, not just Password
//
// The three routes are mounted on the raw gin engine
// (bootstrap.MountUnauthenticated), OUTSIDE V1Chain — so
// ratelimit.Middleware never runs for them and they get no budget at all
// unless it is applied here. An earlier version of this file limited only
// Password, which left GET /v1/auth/login/request/:id and
// POST /v1/auth/login/handoff/:id unauthenticated AND unlimited. That is
// not merely a missing control on a cheap route: AuthRequest makes an
// unauthenticated Zitadel round trip per request, spending the
// INSTANCE-LEVEL login-client PAT's budget on a Zitadel shared with the
// whole Tesserix fleet (docs/standards/backend.md, "The login-client
// credential"), so an unlimited flood there degrades sign-in for every
// product on the instance, not just Helivanta. #851 landed for exactly this
// class of gap on POST /v1/auth/login.
//
// # Keying
//
// There is no verified subject on any of these routes — that is what the
// flow is establishing — so the budget is keyed on client IP, the same
// reasoning that rules out ratelimit.Middleware's tenant/principal
// buckets here.
//
// A nil limiter fails OPEN (admits, warns), mirroring
// LoginHandlers.Login: per docs/standards/engineering-principles.md §3
// this is a capacity control, and a limiter that cannot decide must not
// take sign-in down for a hospital.
func (h *LoginUIHandlers) allowedByLimiter(c *gin.Context, bucket string) bool {
	if h.limiter == nil {
		requestid.Logger(c).WarnContext(c.Request.Context(), "login: rate limiter unavailable, admitting (fail open)",
			"bucket", bucket)
		return true
	}
	d := h.limiter.Allow(bucket+c.ClientIP(), h.limit, time.Now())
	if d.Allowed {
		return true
	}
	requestid.Logger(c).WarnContext(c.Request.Context(), "login rate limited",
		"bucket", bucket, "client_ip", c.ClientIP(), "retry_after_ms", d.RetryAfter.Milliseconds())
	respond.TooManyRequests(c,
		"too many sign-in attempts; retry in "+d.RetryAfter.Round(time.Second).String(),
		d.RetryAfter, d.Limit, d.Remaining)
	return false
}

// authRequestResponse is what GET /v1/auth/login/request/:id answers
// with: enough for Helivanta's own login form to render (which OIDC client is
// asking, where it will redirect, which scopes) without the form itself
// having to speak Zitadel's wire protocol.
type authRequestResponse struct {
	ID          string   `json:"id"`
	ClientID    string   `json:"client_id"`
	RedirectURI string   `json:"redirect_uri"`
	Scope       []string `json:"scope"`
}

// AuthRequest backs GET /v1/auth/login/request/:id: the login form's
// first call, reading the OIDC auth request Zitadel created when the
// browser hit /oauth/v2/authorize (Task 1), so the form knows what it is
// signing the caller into before it renders anything. It cannot require
// a Helivanta session — there is nothing yet to check one against, and this
// call is what tells the form whether the id it was given even makes
// sense.
//
// It DOES take a rate-limit budget (spec D2): every call makes an
// unauthenticated Zitadel round trip on the instance-level login-client
// PAT — see allowedByLimiter's doc comment on why leaving this route
// unlimited was a fleet-wide exposure, not a local one.
func (h *LoginUIHandlers) AuthRequest(c *gin.Context) {
	if !h.allowedByLimiter(c, authRequestRateBucket) {
		return
	}

	id := c.Param("id")

	ar, err := h.client.AuthRequest(c.Request.Context(), id)
	if err != nil {
		h.respondLoginClientError(c, err, "auth_request")
		return
	}

	requestid.Logger(c).InfoContext(c.Request.Context(), "login: auth request read",
		"auth_request_id", ar.ID)
	respond.OK(c, authRequestResponse{
		ID:          ar.ID,
		ClientID:    ar.ClientID,
		RedirectURI: ar.RedirectURI,
		Scope:       ar.Scope,
	})
}

// passwordRequest is the body POST /v1/auth/login/password accepts:
// which auth request this check is for, and the credential to check
// against Zitadel. LoginName is Zitadel's own term (spike §1/§3) for
// what Helivanta's login form collects as an email address — kept as the wire
// name Zitadel expects, rather than renamed to "email", so a reader
// tracing this field into loginclient.Client.CreatePasswordSession does
// not have to reconcile two names for the same value.
type passwordRequest struct {
	AuthRequestID string `json:"auth_request_id" binding:"required"`
	LoginName     string `json:"login_name" binding:"required"`
	Password      string `json:"password" binding:"required"`
}

// passwordSuccessResponse is OutcomeComplete's shape: the callback URL
// Zitadel computed, which the login form redirects the browser to
// verbatim (spike §1) — the SAME callback
// apps/shell/app/api/auth/callback/page.tsx already handles.
type passwordSuccessResponse struct {
	CallbackURL string `json:"callback_url"`
}

// passwordHandoffResponse is OutcomeHandoff's shape: nowhere for Helivanta to
// redirect the caller to except Zitadel's own hosted login UI, which can
// collect whatever Helivanta's own form cannot (an enrolled second factor,
// today; see loginclient.CompleteIfSufficient's KNOWN LIMITATIONS for
// what that currently excludes).
type passwordHandoffResponse struct {
	HandoffURL string `json:"handoff_url"`
}

// Password backs POST /v1/auth/login/password: the login form's
// credential check, and the ONLY place in this file that can complete a
// login (loginclient.CompleteIfSufficient — see its own doc comment on
// why finalize is unreachable any other way).
//
// A wrong password (loginclient.ErrBadCredentials) and an unknown user
// (loginclient.ErrUserNotFound) MUST answer identically — same status,
// same body, same MinFailedLoginDuration timing floor — per spec D5.
// They stay distinct sentinels all the way up from loginclient (see
// ErrBadCredentials' doc comment) specifically so this handler can still
// log which one actually happened, in "outcome" only, never in a field
// that could rebuild the same oracle in the log file: the login name
// itself is NEVER logged on a failed attempt, and failedAttempts never
// leaves loginclient at all.
func (h *LoginUIHandlers) Password(c *gin.Context) {
	// start is captured before ANYTHING else — including request
	// binding and the rate-limit check — because it is the floor for
	// the WHOLE handler's wall-clock latency that
	// TestPasswordFailureTimingIsEqualised measures, not just the
	// Zitadel round trip. A start point taken later would let an early,
	// cheap return (e.g. a rate-limit refusal) escape the floor, which
	// is fine for a 429 but would silently exempt any failure path
	// added later that returns before the network call.
	start := time.Now()

	var req passwordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, err)
		return
	}

	// This is a password-attempt surface, so it gets its OWN bucket
	// (passwordRateBucket) rather than sharing LoginHandlers' "login:"
	// one or either sibling route's — a flood here is
	// credential-guessing traffic, a different threat from a flood of
	// already-verified renewals or of auth-request reads. See
	// allowedByLimiter for the keying and fail-open reasoning all three
	// routes share.
	if !h.allowedByLimiter(c, passwordRateBucket) {
		return
	}

	session, err := h.client.CreatePasswordSession(c.Request.Context(), req.LoginName, req.Password)
	if err != nil {
		if errors.Is(err, loginclient.ErrBadCredentials) || errors.Is(err, loginclient.ErrUserNotFound) {
			// The distinction lives ONLY in this log line — never the
			// login name, never failedAttempts (which never even
			// reaches this package; see loginclient's zitadelError doc
			// comment), and never anything that varies the response.
			requestid.Logger(c).WarnContext(c.Request.Context(), "login password attempt failed",
				"auth_request_id", req.AuthRequestID, "outcome", failureOutcome(err))
			h.respondEqualisedFailure(c, start)
			return
		}
		h.respondLoginClientError(c, err, "password")
		return
	}

	result, err := h.client.CompleteIfSufficient(c.Request.Context(), req.AuthRequestID, session)
	if err != nil {
		h.respondLoginClientError(c, err, "complete")
		return
	}

	if result.Outcome == loginclient.OutcomeComplete {
		requestid.Logger(c).InfoContext(c.Request.Context(), "login password succeeded",
			"auth_request_id", req.AuthRequestID, "outcome", "complete")
		respond.OK(c, passwordSuccessResponse{CallbackURL: result.CallbackURL})
		return
	}

	// OutcomeHandoff: the session Helivanta built is insufficient (or the
	// policy could not be read — CompleteIfSufficient fails closed
	// either way), so hand the browser to Zitadel's own hosted login to
	// finish what Helivanta cannot. This is NOT a failure — no timing floor,
	// no equalised message, no ambiguity about whether the password was
	// even right, because it was.
	requestid.Logger(c).InfoContext(c.Request.Context(), "login password succeeded but session insufficient",
		"auth_request_id", req.AuthRequestID, "outcome", "handoff")
	respond.OK(c, passwordHandoffResponse{HandoffURL: h.handoffURL(req.AuthRequestID)})
}

// failureOutcome names which sentinel a failed password check actually
// was, for the log line ONLY — see Password's doc comment on why the
// response itself must never let this leak.
func failureOutcome(err error) string {
	if errors.Is(err, loginclient.ErrBadCredentials) {
		return "bad_credentials"
	}
	return "user_not_found"
}

// respondEqualisedFailure is spec D5's timing half: it waits until
// MinFailedLoginDuration has elapsed since start, THEN responds with the
// one fixed refusal both wrong-password and unknown-user answer with.
// Waiting before checking the elapsed time would double the wait on a
// call that was already slow (e.g. the real wrong-password path, ~0.83s
// on its own); waiting the REMAINDER keeps every failure landing at
// approximately the same total latency regardless of how long the
// Zitadel call itself took, which is the property the timing oracle
// needs to be closed.
//
// The wait races against c.Request.Context() being cancelled (the
// caller disconnecting or the request timing out) rather than a bare
// time.Sleep, so an abandoned attempt does not hold a goroutine open for
// the full floor. This cannot become a new timing signal of its own:
// BOTH failure paths reach this exact same select, so whether it returns
// early depends only on the CALLER's own disconnect, never on which
// sentinel (bad credentials vs unknown user) occurred — the two remain
// indistinguishable either way. Nothing is written to c on the
// cancelled branch: the caller is already gone, so there is nobody left
// to observe a response at all.
func (h *LoginUIHandlers) respondEqualisedFailure(c *gin.Context, start time.Time) {
	if elapsed := time.Since(start); elapsed < MinFailedLoginDuration {
		timer := time.NewTimer(MinFailedLoginDuration - elapsed)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-c.Request.Context().Done():
			return
		}
	}
	respond.Error(c, http.StatusUnauthorized, "invalid_credentials", passwordFailureMessage)
}

// respondLoginClientError maps every OTHER loginclient sentinel this
// file's handlers can see: ErrAuthRequestInvalid (a stale/expired/
// unknown auth request id) and ErrUnavailable (Zitadel unreachable or
// erroring). Neither gets MinFailedLoginDuration's floor —
// ErrUnavailable explicitly must not (an outage should not be slowed
// further), and ErrAuthRequestInvalid is not a credential refusal at
// all, so the oracle the floor exists to close does not apply to it.
//
// stage names which call produced the error, for the log line only —
// the response body never varies by stage.
func (h *LoginUIHandlers) respondLoginClientError(c *gin.Context, err error, stage string) {
	switch {
	case errors.Is(err, loginclient.ErrAuthRequestInvalid):
		requestid.Logger(c).WarnContext(c.Request.Context(), "login: auth request invalid",
			"stage", stage, "err", err)
		respond.Error(c, http.StatusBadRequest, "auth_request_invalid", authRequestExpiredMessage)
	case errors.Is(err, loginclient.ErrUnavailable):
		requestid.Logger(c).ErrorContext(c.Request.Context(), "login: zitadel unavailable",
			"stage", stage, "err", err)
		respond.Error(c, http.StatusServiceUnavailable, "zitadel_unavailable", zitadelUnavailableMessage)
	default:
		// Not one of loginclient's documented sentinels at all — an
		// encode/decode failure inside the client, say. Genuinely
		// unexpected, so it gets InternalErr's 500 rather than being
		// folded into ErrUnavailable's retryable 503.
		respond.InternalErr(c, err, "sign-in could not be completed")
	}
}

// handoffURL builds the URL a caller is sent to when Helivanta cannot complete
// their login itself: h.hostedLoginBaseURL (an origin+path with no query
// string of its own — Task 1's finding that Zitadel APPENDS to whatever
// baseUri is configured) plus the SAME authRequest id Zitadel's own
// /oauth/v2/authorize redirect used, so its hosted UI resumes the exact
// auth request Helivanta started checking rather than minting a new one.
func (h *LoginUIHandlers) handoffURL(authRequestID string) string {
	return h.hostedLoginBaseURL + "?authRequest=" + url.QueryEscape(authRequestID)
}

// Handoff backs POST /v1/auth/login/handoff/:id: an explicit "hand this
// auth request to Zitadel's hosted login" call, for a caller that
// already knows Helivanta's own form cannot finish it (e.g. a "use another
// sign-in method" affordance) rather than discovering that only after a
// Password attempt. It makes no Zitadel call of its own — there is
// nothing to check, only a URL to build — so it has no failure mode
// beyond a missing id, which gin's route match already guarantees is
// non-empty for a matched :id segment.
//
// It still takes a rate-limit budget (spec D2). Being cheap for Helivanta to
// serve is not a reason to leave an unauthenticated route unlimited:
// this one is mounted on the raw engine, so nothing else applies a
// budget to it, and an entry in bootstrap.UnauthenticatedRoutes that is
// reachable by anyone with no principal is exactly the shape #851 closed
// on POST /v1/auth/login. See allowedByLimiter.
func (h *LoginUIHandlers) Handoff(c *gin.Context) {
	if !h.allowedByLimiter(c, handoffRateBucket) {
		return
	}

	id := c.Param("id")
	requestid.Logger(c).InfoContext(c.Request.Context(), "login: handed off to hosted login",
		"auth_request_id", id)
	respond.OK(c, passwordHandoffResponse{HandoffURL: h.handoffURL(id)})
}

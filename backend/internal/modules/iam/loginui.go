package iam

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/ratelimit"
	"github.com/tesserix/helivanta/pkg/tenantdb"
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

// Refusal codes and messages (#947 spec D2). Each answers a sign-in whose
// password Zitadel ACCEPTED but which Helivanta still cannot complete. They are
// Helivanta's own words — the browser is never redirected to Zitadel's hosted
// login for any of them. None joins spec D5's equalised credential refusal:
// each is reachable only with the correct password, exactly as the deleted
// handoff_url was, so it discloses nothing a wrong-password probe can learn.
const (
	refusalCodeMethodUnsupported    = "sign_in_method_unsupported"
	refusalMessageMethodUnsupported = "your account uses a sign-in method Helivanta does not support yet; " +
		"contact your administrator"

	refusalCodeEnrollmentRequired    = "mfa_enrollment_required"
	refusalMessageEnrollmentRequired = "your organisation requires two-step verification, which is not set up " +
		"for your account yet; contact your administrator"

	refusalCodeIncomplete    = "sign_in_incomplete"
	refusalMessageIncomplete = "this sign-in could not be completed; start again"
)

// loginAttemptTTL is the window after which a login_attempt row is
// treated as EXPIRED ON READ — spec D6: "expires_at is short — the auth
// request itself expires, and a pending factor step that outlives it is
// unusable anyway." Five minutes matches Helivanta's own
// SESSION_TTL-adjacent human-timescale windows elsewhere in this
// codebase and is generous enough that a clinician reading a rolling
// TOTP code off an authenticator app never races it.
//
// # This does NOT bound how long the row — or the live Zitadel session token it holds — actually survives in Postgres (#867 fix round 2, Finding I2)
//
// loginAttemptStore.Get (loginattempt.go) deletes an expired row only
// when THAT SAME auth_request_id is read again and found past
// expires_at — it is expiry-on-read, not a background sweep. A row
// nobody ever reads again (a browser that abandons the factor step —
// closes the tab, the clinician is pulled away mid-shift before
// submitting a code) is NEVER read, so it is never deleted: the row, and
// the live Zitadel session token it carries, remains in Postgres
// indefinitely past loginAttemptTTL, not merely for it. An EARLIER
// version of this comment claimed the TTL "bounds how long a stolen or
// abandoned row keeps a usable Zitadel session token alive in the
// database" — that was false; expiry-on-read makes the row unusable for
// completing a login past the TTL (Get's own doc comment, spec D6), but
// says nothing about how long the ROW ITSELF, or the token inside it,
// persists at rest.
//
// login_attempt_expires_at_idx (0004_iam migration, module.go) exists as
// if a periodic sweep were meant to use it, but none was ever written —
// filed as #869 rather than built here, since a sweeper is its own
// piece of work (cadence, verification, mirroring the existing
// bus.RunPruner pattern in cmd/api/main.go) and this task's scope is the
// HTTP layer, not background jobs.
const loginAttemptTTL = 5 * time.Minute

// LoginUIHandlers backs the four routes Helivanta's own login form drives
// directly against Zitadel's login-client API (plan #854 Task 4, spec
// D5; #867 Task 4 adds Factor): reading an auth request, checking a
// password, and checking a native TOTP factor when the password step
// alone was not enough. When Helivanta cannot complete a login it REFUSES it
// in its own words; it never hands the browser to Zitadel's hosted login
// (#947). Like LoginHandlers (login.go), all three are mounted OUTSIDE the authenticated /v1 chain via
// bootstrap.MountUnauthenticated — there is no Helivanta session, and for
// Password/Factor specifically no verified subject at all, until AFTER
// they succeed.
type LoginUIHandlers struct {
	client  *loginclient.Client
	store   *loginAttemptStore
	limiter ratelimit.Limiter
	limit   ratelimit.Rule
	// factorLimit is POST /v1/auth/login/factor's OWN budget — see
	// factorRateBucket's doc comment on why a six-digit guessing
	// endpoint cannot share limit (Password/AuthRequest's
	// budget) without silently narrowing the whole login surface's
	// capacity to whatever the guessing endpoint alone would tolerate.
	factorLimit ratelimit.Rule
}

// NewLoginUIHandlers wires LoginUIHandlers' dependencies. client is
// Task 2's loginclient.Client, authenticated with the login-client PAT.
//
// db backs the login_attempt store (Task 1, loginattempt.go): the
// Zitadel session Password stashes between the password step and the
// factor step (spec D2 — the browser is never handed a Zitadel token). A
// nil db is fine for a caller that never exercises the FactorRequired
// path or Factor itself (most of this file's own unit tests construct
// one this way) — newLoginAttemptStore does no I/O at construction time,
// only when a store method is actually called.
//
// limiter and limit are the budget shared by AuthRequest and Password
// (#854 Task 4, spec D2: "all three sit behind the existing
// unauthenticated limiter"): reused from the SAME ratelimit.Limiter
// instance bootstrap.V1Chain and LoginHandlers already share, per #851
// and docs/standards on not inventing a second limiter, each keyed under
// its OWN prefix (see allowedByLimiter) so no route can bleed into
// another's bucket. factorLimit is Factor's OWN, separately sized budget
// (#867 — see factorRateBucket's doc comment on why a six-digit
// code-guessing endpoint must not share limit with its siblings) drawn
// from the SAME limiter instance, just a different Rule. A nil limiter
// fails OPEN for every route, mirroring LoginHandlers.Login's documented
// direction: this is a capacity control, and a limiter that cannot
// decide must not take sign-in down for a hospital.
func NewLoginUIHandlers(client *loginclient.Client, db *tenantdb.DB, limiter ratelimit.Limiter, limit, factorLimit ratelimit.Rule) *LoginUIHandlers {
	return &LoginUIHandlers{
		client:      client,
		store:       newLoginAttemptStore(db),
		limiter:     limiter,
		limit:       limit,
		factorLimit: factorLimit,
	}
}

// Rate-limit bucket prefixes, one per route. All four routes share ONE
// ratelimit.Limiter instance (see NewLoginUIHandlers), but each gets its
// own key prefix so a flood against one cannot spend another's budget: a
// browser reading an auth request is not the same traffic as a browser
// guessing passwords, and neither should be able to lock the other out.
// The prefixes are also distinct from LoginHandlers.Login's "login:"
// bucket and ratelimit.Middleware's principal bucket, so none of the six
// can collide.
//
// factorRateBucket gets its OWN Rule (factorLimit), not the shared limit
// every other route here uses — see allowedByFactorLimiter's doc
// comment: POST /v1/auth/login/factor is a six-digit code-guessing
// surface, a materially different threat shape from "load the login
// form" or "check one password", and #867's plan explicitly calls out
// that it must not share a budget with anything else, including its own
// siblings in this file.
const (
	authRequestRateBucket = "login_auth_request:"
	passwordRateBucket    = "login_password:"
	factorRateBucket      = "login_factor:"
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
// Password, which left GET /v1/auth/login/request/:id (and a since-deleted
// hosted-login handoff route) unauthenticated AND unlimited. That is
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
// rule is the caller's own budget — h.limit for AuthRequest/Password
// (shared, per this type's doc comment) or h.factorLimit for
// Factor (its own, per factorRateBucket's doc comment). Threading it as
// a parameter, rather than a second near-identical method, keeps the
// admit/refuse/log/fail-open logic in exactly one place regardless of
// which budget a route draws from.
func (h *LoginUIHandlers) allowedByLimiter(c *gin.Context, bucket string, rule ratelimit.Rule) bool {
	if h.limiter == nil {
		requestid.Logger(c).WarnContext(c.Request.Context(), "login: rate limiter unavailable, admitting (fail open)",
			"bucket", bucket)
		return true
	}
	d := h.limiter.Allow(bucket+c.ClientIP(), rule, time.Now())
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

// authPoliciesResponse is the provider-neutral policy subset spec D5
// wants exposed to the login form — @tesserix/web's AuthPolicies shape,
// mapped here IN GO rather than by a TypeScript adapter reading
// Zitadel's raw policy object. Only four fields cross the wire, on
// purpose:
//
//   - AllowPassword and SecondFactors are NOT read from
//     loginclient.LoginPolicy — that type deliberately models only
//     ForceMFA (see its own doc comment: "the one field sufficiency.go
//     needs"), and this task does not extend loginclient (out of Task
//     4's scope; #867). They are fixed to Helivanta's own current, actual
//     configuration instead of Zitadel's: password is always the first
//     factor Helivanta's form collects (AllowPassword: true), and TOTP
//     is the only second factor Helivanta can natively collect today
//     (SecondFactors: ["totp"], spec D1's scope) — a client rendering
//     from these values renders exactly what this file's handlers are
//     actually capable of driving, never a Zitadel-side option Helivanta
//     has no code path for.
//   - IgnoreUnknownUsernames is fixed false: Helivanta does not implement
//     Zitadel's "pretend unknown users don't exist at the username step"
//     option, and spec D5 of the login-client spec already equalises
//     wrong-password and unknown-user refusals structurally
//     (respondEqualisedFailure) — that guarantee does not depend on this
//     field, so a stale or invented value here cannot weaken it.
//   - RequireMFA IS read from LoginPolicy.ForceMFA — the one field the
//     enforcer (loginclient.CompleteIfSufficient / CompleteAfterFactor)
//     actually consults. It is NOT, however, the same READ: this handler
//     has no authenticated user yet, so it fetches the policy unscoped
//     via InstanceLoginPolicyForDisplay (loginclient/client.go), which
//     resolves against the login-client PAT's own resource owner, not
//     any particular org. The enforcer scopes its own read to the
//     authenticating user's org via LoginPolicyForOrg once a session
//     exists (design spec D1/D2/D3,
//     docs/superpowers/specs/2026-08-20-login-policy-org-scope-design.md).
//     On a multi-org instance the two reads can disagree — this value is
//     advisory only, a hint for what the form renders before anything is
//     known, and enforces nothing.
//
// requireMfaLocalOnly has NO field here at all, deliberately — see spec
// D5: LoginPolicy already folds it into ForceMFA
// (forceMfa || forceMfaLocalOnly), and unfolding it into a second neutral
// field the enforcer never separately consults would let the login form
// render from a value nothing enforces. TestAuthRequestDoesNotExpose
// RequireMFALocalOnly pins that no such key appears in the response body
// at all, not merely that it is false.
type authPoliciesResponse struct {
	AllowPassword          bool     `json:"allow_password"`
	RequireMFA             bool     `json:"require_mfa"`
	SecondFactors          []string `json:"second_factors"`
	IgnoreUnknownUsernames bool     `json:"ignore_unknown_usernames"`
}

// authRequestResponse is what GET /v1/auth/login/request/:id answers
// with: enough for Helivanta's own login form to render (which OIDC client is
// asking, where it will redirect, which scopes, and which policies (D5)
// govern this org's login) without the form itself having to speak
// Zitadel's wire protocol.
type authRequestResponse struct {
	ID          string               `json:"id"`
	ClientID    string               `json:"client_id"`
	RedirectURI string               `json:"redirect_uri"`
	Scope       []string             `json:"scope"`
	Policies    authPoliciesResponse `json:"policies"`
}

// AuthRequest backs GET /v1/auth/login/request/:id: the login form's
// first call, reading the OIDC auth request Zitadel created when the
// browser hit /oauth/v2/authorize (Task 1), so the form knows what it is
// signing the caller into before it renders anything. It cannot require
// a Helivanta session — there is nothing yet to check one against, and this
// call is what tells the form whether the id it was given even makes
// sense.
//
// It also reads A login policy (#867, spec D5) so the form can decide up
// front whether to advertise an MFA step, without that decision ever
// being the thing that actually enforces one —
// loginclient.CompleteIfSufficient / CompleteAfterFactor remain the only
// enforcers, unchanged by this read. That read is deliberately UNSCOPED
// (InstanceLoginPolicyForDisplay, not LoginPolicyForOrg): no login name
// has been typed yet at this point in the flow, so there is no
// authenticated user and therefore no org to scope the read to — not an
// omission, there is no correct value to pass here (design spec D3,
// docs/superpowers/specs/2026-08-20-login-policy-org-scope-design.md).
// On a multi-org instance this means the hint this endpoint renders CAN
// be wrong: it may tell the form "no MFA" for a login name that, once
// typed, turns out to belong to an org that forces it. That is a
// cosmetic wrong hint, not a bypass — enforcement never depends on it.
// An unreadable policy still fails the same way an unreadable policy
// fails everywhere else in this file (respondLoginClientError,
// ErrUnavailable → 503): there is no safe default to render when
// Helivanta cannot tell whether MFA is required, and a form that
// rendered "MFA not required" on a read failure would be exactly the
// fail-OPEN spec D4 (2026-08-16 login-client design) exists to prevent,
// just moved one call earlier.
//
// It DOES take a rate-limit budget (spec D2): every call makes an
// unauthenticated Zitadel round trip on the instance-level login-client
// PAT — see allowedByLimiter's doc comment on why leaving this route
// unlimited was a fleet-wide exposure, not a local one.
func (h *LoginUIHandlers) AuthRequest(c *gin.Context) {
	if !h.allowedByLimiter(c, authRequestRateBucket, h.limit) {
		return
	}

	id := c.Param("id")

	ar, err := h.client.AuthRequest(c.Request.Context(), id)
	if err != nil {
		h.respondLoginClientError(c, err, "auth_request")
		return
	}

	policy, err := h.client.InstanceLoginPolicyForDisplay(c.Request.Context())
	if err != nil {
		h.respondLoginClientError(c, err, "auth_request_policy")
		return
	}

	requestid.Logger(c).InfoContext(c.Request.Context(), "login: auth request read",
		"auth_request_id", ar.ID)
	respond.OK(c, authRequestResponse{
		ID:          ar.ID,
		ClientID:    ar.ClientID,
		RedirectURI: ar.RedirectURI,
		Scope:       ar.Scope,
		Policies: authPoliciesResponse{
			AllowPassword:          true,
			RequireMFA:             policy.ForceMFA,
			SecondFactors:          []string{"totp"},
			IgnoreUnknownUsernames: false,
		},
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

// factorRequiredResponse is OutcomeFactorRequired's shape (#867, spec
// D8): which factor kind(s) the login form must now collect natively,
// via POST /v1/auth/login/factor. Deliberately carries no CallbackURL —
// this is neither success nor failure (see
// this type's own doc note in the spec: "the page must treat
// factor_required as neither"), so it must not be structurally
// confusable with its success sibling above.
type factorRequiredResponse struct {
	Factors []string `json:"factor_required"`
}

// nonNilFactors guards against ever emitting {"factor_required":null}
// (#867 fix round 1, Minor 4): encoding/json marshals a nil []string as
// JSON `null`, not `[]`, and a naive browser-side `for (const f of
// factor_required)` over `null` throws rather than iterating zero
// times. loginclient.CompleteIfSufficient always sets Factors to a
// real, non-empty slice (["totp"]) for OutcomeFactorRequired today —
// see its own doc comment — so this never actually fires against the
// current implementation; it exists so a FUTURE loginclient change that
// leaves Factors nil for some new case fails safe (an empty array a
// client can always range over) rather than reintroducing this
// untested edge silently. TestNonNilFactorsNeverReturnsNil pins the
// function directly, and TestFactorRequiredResponseJSONNeverEmitsNull
// pins that the marshaled response actually reflects it.
func nonNilFactors(factors []string) []string {
	if factors == nil {
		return []string{}
	}
	return factors
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
	if !h.allowedByLimiter(c, passwordRateBucket, h.limit) {
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

	switch result.Outcome {
	case loginclient.OutcomeComplete:
		requestid.Logger(c).InfoContext(c.Request.Context(), "login password succeeded",
			"auth_request_id", req.AuthRequestID, "outcome", "complete")
		respond.OK(c, passwordSuccessResponse{CallbackURL: result.CallbackURL})

	case loginclient.OutcomeFactorRequired:
		// The Zitadel session (id + CURRENT token) must be stashed
		// server-side (spec D2 — the browser never sees a Zitadel
		// token) so Factor can resume it after collecting a TOTP code.
		// Subject is req.LoginName: the login name this password check
		// was FOR, the only per-caller identity this handler has —
		// stored for audit/correlation only, never returned to the
		// browser and never itself a credential.
		err := h.store.Put(c.Request.Context(), loginAttempt{
			AuthRequestID: req.AuthRequestID,
			SessionID:     session.ID,
			SessionToken:  session.Token,
			Subject:       req.LoginName,
			ExpiresAt:     time.Now().Add(loginAttemptTTL),
		})
		if err != nil {
			respond.InternalErr(c, err, "sign-in could not be completed")
			return
		}
		requestid.Logger(c).InfoContext(c.Request.Context(), "login password succeeded, factor required",
			"auth_request_id", req.AuthRequestID, "outcome", "factor_required")
		respond.OK(c, factorRequiredResponse{Factors: nonNilFactors(result.Factors)})

	default:
		// OutcomeRefused (and anything unrecognised, which fails closed the
		// same way): the password was right, but Helivanta cannot complete
		// this sign-in. Refused in Helivanta's own words (#947) — no timing
		// floor and no equalised message, because the credential was correct.
		h.respondRefusal(c, req.AuthRequestID, "password", result)
	}
}

// respondRefusal answers a loginclient refusal (#947 spec D2) and logs WHY,
// with the enrolled method types when the reason is an unsupported factor
// (spec D5) — the line that answers "why could this clinician not sign in"
// without opening Zitadel's console. stage names the handler, for the log
// only.
//
// A Result whose Outcome is not OutcomeRefused reaching here is a switch that
// did not handle a new Outcome; it is refused, not completed, and logged as
// such rather than guessed at.
func (h *LoginUIHandlers) respondRefusal(c *gin.Context, authRequestID, stage string, result loginclient.Result) {
	reason := result.Reason
	if result.Outcome != loginclient.OutcomeRefused {
		reason = loginclient.RefusalUnspecified
	}
	attrs := []any{
		"auth_request_id", authRequestID,
		"stage", stage,
		"outcome", "refused",
		"refusal_reason", reason.String(),
	}
	if len(result.EnrolledMethods) > 0 {
		attrs = append(attrs, "enrolled_methods", result.EnrolledMethods)
	}
	if reason == loginclient.RefusalUnspecified {
		attrs = append(attrs, "result_outcome", result.Outcome.String())
		requestid.Logger(c).ErrorContext(c.Request.Context(), "login refused with no reason: a loginclient outcome is unhandled", attrs...)
	} else {
		requestid.Logger(c).WarnContext(c.Request.Context(), "login refused", attrs...)
	}

	switch reason {
	case loginclient.RefusalFactorUnsupported:
		respond.Error(c, http.StatusForbidden, refusalCodeMethodUnsupported, refusalMessageMethodUnsupported)
	case loginclient.RefusalMFAEnrollmentRequired:
		respond.Error(c, http.StatusForbidden, refusalCodeEnrollmentRequired, refusalMessageEnrollmentRequired)
	default:
		respond.Error(c, http.StatusForbidden, refusalCodeIncomplete, refusalMessageIncomplete)
	}
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

// waitUntilFailureFloor blocks until MinFailedLoginDuration has elapsed
// since start, THEN returns true — or returns false early if
// c.Request.Context() is cancelled first (the caller disconnecting or
// the request timing out), in which case the caller must write nothing
// to c: the requester is already gone, so there is nobody left to
// observe a response at all.
//
// Shared by respondEqualisedFailure (spec D5's wrong-password/
// unknown-user/wrong-TOTP-code refusal) AND respondAttemptExpired
// (#867 fix round 1, Finding/Minor 3): an unknown or expired
// auth_request_id answers IMMEDIATELY with no Zitadel round trip, while
// a genuinely EXHAUSTED attempt answers only after a real VerifyTOTP
// call plus a BumpAndGet — both currently land on the identical
// attempt-expired body, but without this shared floor they would
// arrive at measurably different latencies, and that latency gap is
// itself the same class of id-existence oracle D5's body equality
// exists to deny (a prober who cannot see the body can still time it).
// Applying the SAME floor, via the SAME helper, to both
// respondEqualisedFailure and respondAttemptExpired is what keeps a
// credential-adjacent refusal and an attempt-expired refusal from
// drifting into two different timing profiles even as their bodies stay
// deliberately different (spec D5's OTHER guarantee: attempt-expired
// must not read as a credential refusal — see respondAttemptExpired's
// own doc comment).
//
// Waiting before checking the elapsed time would double the wait on a
// call that was already slow (e.g. the real wrong-password path, ~0.83s
// on its own); waiting the REMAINDER keeps every failure landing at
// approximately the same total latency regardless of how long the
// Zitadel call itself took, which is the property the timing oracle
// needs to be closed. The wait races against context cancellation
// rather than a bare time.Sleep so an abandoned attempt does not hold a
// goroutine open for the full floor — this cannot become a new timing
// signal of its own, because every caller of this function reaches the
// exact same select regardless of which sentinel or which internal
// store state produced the call.
func (h *LoginUIHandlers) waitUntilFailureFloor(c *gin.Context, start time.Time) bool {
	if elapsed := time.Since(start); elapsed < MinFailedLoginDuration {
		timer := time.NewTimer(MinFailedLoginDuration - elapsed)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-c.Request.Context().Done():
			return false
		}
	}
	return true
}

// respondEqualisedFailure is spec D5's timing half: it waits until
// MinFailedLoginDuration has elapsed since start (waitUntilFailureFloor),
// THEN responds with the one fixed refusal wrong-password, unknown-user,
// AND wrong-TOTP-code all answer with.
func (h *LoginUIHandlers) respondEqualisedFailure(c *gin.Context, start time.Time) {
	if !h.waitUntilFailureFloor(c, start) {
		return
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

// factorRequest is the body POST /v1/auth/login/factor accepts (#867,
// spec D8): the auth request this check resumes, which factor kind is
// being submitted, and the code itself. Factor is required and checked
// explicitly (not just bound) because "totp" is the only value this
// handler — and loginclient.VerifyTOTP beneath it — knows how to check;
// silently accepting anything else and forwarding it to VerifyTOTP would
// misreport a client bug as a wrong code.
type factorRequest struct {
	AuthRequestID string `json:"auth_request_id" binding:"required"`
	Factor        string `json:"factor" binding:"required"`
	Code          string `json:"code" binding:"required"`
}

// Factor backs POST /v1/auth/login/factor: the second half of the
// OutcomeFactorRequired flow Password's handler starts (#867, spec D8).
// It resumes the Zitadel session Password stashed in login_attempt,
// checks the submitted TOTP code against it (loginclient.VerifyTOTP),
// and — only on success — finalizes the login
// (loginclient.CompleteAfterFactor), the SAME structural guarantee
// Password's finalize path relies on: this file never calls finalize
// itself, only through one of loginclient's two `sufficient`-producing
// functions.
//
// # The rotated token, threaded before finalize — spec D3, the highest-risk defect this task exists to avoid
//
// VerifyTOTP returns a Session carrying a NEW token, not the one this
// handler read out of the store. That returned Session — never the one
// read from h.store.Get — is what UpdateToken persists and what
// CompleteAfterFactor is called with. Passing the stale, pre-verification
// token to CompleteAfterFactor would make finalize fail AFTER a CORRECT
// code, which reads to the clinician as "my TOTP was rejected" — see
// loginclient.VerifyTOTP's own doc comment for the same failure spelled
// out from the client's side.
//
// # A wrong code and an unknown auth_request_id must be indistinguishable — spec D5, extended
//
// A wrong code answers the EXACT SAME status/body/timing as a wrong
// password (h.respondEqualisedFailure, reused rather than duplicated —
// see this function's body). An unknown or expired auth_request_id
// answers attempt-expired instead, NEVER the shared refusal: refusing
// with "wrong credentials" would confirm an attempt for that id once
// existed, which is itself a signal an attacker probing random ids
// should never get. See respondAttemptExpired.
//
// # BumpAndGet runs ONLY after Zitadel rejects a code
//
// Per loginAttemptStore.BumpAndGet's own doc comment, bumping before
// verifying would silently cut the real five-guess budget (spec D6) to
// four, because the eventual correct code would still consume one of the
// five bumps BumpAndGet hands out. So the increment happens in the
// ErrBadCredentials branch only, after VerifyTOTP has already answered.
func (h *LoginUIHandlers) Factor(c *gin.Context) {
	// start is captured before binding and the rate-limit check for the
	// exact same reason Password's is — see that handler's doc comment.
	// Both respondEqualisedFailure (wrong code) and respondAttemptExpired
	// (unknown/expired/exhausted attempt — Finding/Minor 3) read it, so
	// EVERY attempt-expired-or-refusal path below lands at the same
	// floor; success, a reasoned refusal, "unsupported factor", and "Zitadel
	// unavailable" are deliberately NOT held to it, the same asymmetry
	// Password observes.
	start := time.Now()

	var req factorRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, err)
		return
	}
	if req.Factor != "totp" {
		// Not a credential refusal — the caller asked this handler to
		// check a factor kind it does not implement. A 400, like any
		// other malformed-request case, not the equalised failure: this
		// reveals nothing about a login name or a code, only that the
		// CALLER sent a value this endpoint does not support.
		respond.BadRequest(c, fmt.Errorf("unsupported factor %q", req.Factor))
		return
	}

	// Factor's OWN budget (factorRateBucket, h.factorLimit) — never
	// h.limit, the budget AuthRequest/Password share. See
	// factorRateBucket's doc comment: a six-digit code-guessing endpoint
	// sharing a bucket with "load the login form" would let a guessing
	// flood exhaust the budget legitimate traffic needs, or a generous
	// budget sized for page loads would hand a guesser far more than
	// five guesses per minute.
	if !h.allowedByLimiter(c, factorRateBucket, h.factorLimit) {
		return
	}

	attempt, err := h.store.Get(c.Request.Context(), req.AuthRequestID)
	if err != nil {
		// errAttemptNotFound covers BOTH an id this store never saw AND
		// one whose row has expired (loginAttemptStore.Get folds the
		// two together) — deliberately: distinguishing "never existed"
		// from "existed once" in the response would itself be the
		// enumeration signal this function's doc comment warns about.
		requestid.Logger(c).WarnContext(c.Request.Context(), "login factor: no pending attempt",
			"auth_request_id", req.AuthRequestID)
		h.respondAttemptExpired(c, start)
		return
	}

	verified, err := h.client.VerifyTOTP(c.Request.Context(),
		loginclient.Session{ID: attempt.SessionID, Token: attempt.SessionToken}, req.Code)
	if err != nil {
		if errors.Is(err, loginclient.ErrBadCredentials) {
			h.bumpFactorAttempt(c, req.AuthRequestID, start)
			return
		}
		h.respondLoginClientError(c, err, "verify_totp")
		return
	}

	// The rotated token — see this function's doc comment's D3 section.
	// Persisted BEFORE CompleteAfterFactor is called, so a caller that
	// crashes or times out between these two lines leaves the STORE
	// holding the token that will actually work on a retried Factor
	// call, never the superseded one.
	if err := h.store.UpdateToken(c.Request.Context(), req.AuthRequestID, verified.Token); err != nil {
		respond.InternalErr(c, err, "sign-in could not be completed")
		return
	}

	// respondLoginClientError's ErrAuthRequestInvalid branch answers
	// WITHOUT the MinFailedLoginDuration floor (see its own doc comment:
	// that branch is not a credential refusal, so the oracle the floor
	// exists to close does not apply to it in general) — worth
	// confirming that reasoning actually holds AT THIS SPECIFIC call
	// site too, not just in the abstract (#867 fix round 2, Finding
	// M5). It does: CompleteAfterFactor can only reach
	// ErrAuthRequestInvalid through finalize (loginclient's own
	// sufficiency.go), and finalize is only ever called from THIS branch
	// after VerifyTOTP has ALREADY returned success two lines above.
	// VerifyTOTP itself never produces ErrAuthRequestInvalid — a 4xx
	// from PATCH /v2/sessions/{id} maps to ErrBadCredentials, handled in
	// the branch above and always floored via bumpFactorAttempt. So
	// reaching this unfloored fast path requires the caller to have
	// ALREADY submitted the correct TOTP code; it is not a path an
	// attacker who is still guessing codes can reach at all, and
	// therefore not a timing oracle over the code itself the way an
	// unfloored VerifyTOTP failure would be.
	result, err := h.client.CompleteAfterFactor(c.Request.Context(), req.AuthRequestID, verified)
	if err != nil {
		h.respondLoginClientError(c, err, "complete_after_factor")
		return
	}

	switch result.Outcome {
	case loginclient.OutcomeComplete:
		h.deleteAttempt(c, req.AuthRequestID)
		requestid.Logger(c).InfoContext(c.Request.Context(), "login factor succeeded",
			"auth_request_id", req.AuthRequestID, "outcome", "complete")
		respond.OK(c, passwordSuccessResponse{CallbackURL: result.CallbackURL})
	default:
		// OutcomeRefused (or anything unrecognised, failing closed the same
		// way). CompleteAfterFactor re-runs the SAME uncollectible/enrolled
		// checks CompleteIfSufficient did (its own doc comment, Finding 2)
		// and can still refuse after a CORRECT code — e.g. the user's
		// enrolment changed between the password step and this one. That
		// is not a failure of THIS code check, so it is not the equalised
		// refusal either. The local attempt is over either way, so the row
		// is deleted.
		h.deleteAttempt(c, req.AuthRequestID)
		h.respondRefusal(c, req.AuthRequestID, "factor", result)
	}
}

// bumpFactorAttempt records a wrong TOTP code against authRequestID —
// ONLY called after Zitadel has already rejected one, per
// loginAttemptStore.BumpAndGet's own doc comment — and answers either
// attempt-expired (the fifth wrong code: the login_attempt ROW is
// deleted, so this server can no longer resume the attempt) or the
// shared equalised refusal (every wrong code before the fifth).
//
// "The row is deleted" is NOT the same claim as "the Zitadel session is
// destroyed" (#867 fix round 2, Finding I3) — this file never calls
// DELETE /v2/sessions/{id} or any equivalent, on exhaustion or anywhere
// else. Deleting the row makes the session UNREACHABLE from this
// server (there is nothing left holding its id/token), which is what
// spec D6's body accurately calls the session being "abandoned" — but
// the session itself keeps whatever lifetime Zitadel's own session
// policy already gives it. An earlier version of this comment (and spec
// D6's heading) said "the row and the Zitadel session are both gone",
// which overstates what exhaustion actually does.
func (h *LoginUIHandlers) bumpFactorAttempt(c *gin.Context, authRequestID string, start time.Time) {
	requestid.Logger(c).WarnContext(c.Request.Context(), "login factor attempt failed",
		"auth_request_id", authRequestID)

	_, err := h.store.BumpAndGet(c.Request.Context(), authRequestID)
	switch {
	case errors.Is(err, errAttemptsExhausted):
		requestid.Logger(c).WarnContext(c.Request.Context(), "login factor attempts exhausted",
			"auth_request_id", authRequestID)
		h.respondAttemptExpired(c, start)
	case errors.Is(err, errAttemptNotFound):
		// The row expired or was otherwise removed between Get and this
		// bump (e.g. a concurrent request on the same auth_request_id
		// already exhausted it). Same answer as any other missing
		// attempt.
		h.respondAttemptExpired(c, start)
	case err != nil:
		respond.InternalErr(c, err, "sign-in could not be completed")
	default:
		// Reuses Password's own helper (spec D5) rather than a second,
		// independently-worded copy — see respondEqualisedFailure's doc
		// comment on why that duplication is exactly what reopens the
		// oracle it closes.
		h.respondEqualisedFailure(c, start)
	}
}

// deleteAttempt removes authRequestID's login_attempt row once Factor no
// longer needs it (finalized, or refused) and logs rather than fails
// the request if the delete itself errors: the caller already has their
// callback_url or refusal, and a login_attempt row that outlives its
// usefulness by loginAttemptTTL is cleaned up on its own next read
// (loginAttemptStore.Get's expiry handling) even if this delete is lost.
func (h *LoginUIHandlers) deleteAttempt(c *gin.Context, authRequestID string) {
	if err := h.store.Delete(c.Request.Context(), authRequestID); err != nil {
		requestid.Logger(c).WarnContext(c.Request.Context(), "login factor: failed to delete completed attempt",
			"auth_request_id", authRequestID, "err", err)
	}
}

// respondAttemptExpired answers exactly the way respondLoginClientError
// already answers loginclient.ErrAuthRequestInvalid — same status, same
// code, same authRequestExpiredMessage — because "the auth request id is
// stale/unknown" and "the login_attempt row for it is stale/unknown" are
// the same user-facing situation: start the sign-in over. Reusing the
// exact literal keeps the two from drifting into two different wordings
// for what is, from the browser's point of view, one failure mode.
//
// It also takes the SAME MinFailedLoginDuration floor
// (waitUntilFailureFloor) every Factor caller of this function reaches —
// #867 fix round 1, Finding/Minor 3. Without it, an unknown
// auth_request_id (store.Get misses immediately, no Zitadel call at
// all) and a genuinely EXHAUSTED one (reached only after a real
// VerifyTOTP round trip plus a BumpAndGet) would answer the identical
// body at measurably different latencies — and that gap is itself an
// id-existence oracle of exactly the shape spec D5's body-equality
// guarantee exists to deny, just moved from the response bytes to the
// response clock. This is a DELIBERATE cost on the common case (a
// browser resuming a stale tab now waits out the floor too, not just a
// credential refusal) accepted specifically to close that gap, not an
// oversight.
func (h *LoginUIHandlers) respondAttemptExpired(c *gin.Context, start time.Time) {
	if !h.waitUntilFailureFloor(c, start) {
		return
	}
	respond.Error(c, http.StatusBadRequest, "auth_request_invalid", authRequestExpiredMessage)
}

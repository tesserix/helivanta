package iam

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tesserix/hms/internal/platform/requestid"
	"github.com/tesserix/hms/internal/platform/respond"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/session"
)

// activityResponse is the ONLY channel that carries the new idle
// deadline back to the browser. The session cookie holding it is
// httpOnly (see Login in login.go: same flags, same reasoning), so
// client-side script cannot read the token to learn when the window
// closes. Without this field in the body, the client that calls this
// endpoint would have no way to compute when to show the two-minute
// warning (spec D5) — it would have moved its own deadline and have no
// way to find out by how much.
type activityResponse struct {
	IdleDeadline time.Time `json:"idle_deadline"`
}

// activityHandlers backs POST /v1/auth/session/activity (spec D4,
// #848) — the ONE endpoint allowed to move a session's idle_deadline
// forward. Every other re-mint in this codebase carries the existing
// deadline through UNCHANGED: silent renewal (login.go's
// idleDeadlineFor) and tenant switch (me.go's switchTenant) both do,
// on purpose, per spec D3. This handler is the deliberate exception,
// reached only when the browser has observed genuine human interaction
// (a debounced pointerdown/keydown/scroll, per D4) — never on a timer.
//
// This handler carries no rate-limiter field of its own: the route is
// registered through platform.Router (see Module.registerActivity)
// inside the authenticated /v1 chain, so it already passes through
// ratelimit.Middleware, which applies bootstrap.RateLimitConfig's Tight
// entry for "POST /v1/auth/session/activity" — a narrower, separately
// keyed budget than the Principal rule every other authenticated route
// draws from, with no handler-level plumbing needed. See that Tight
// entry's doc comment for the exact numbers and why this route gets one.
type activityHandlers struct {
	signer       *session.Signer
	ttl          time.Duration
	secureCookie bool
	// idleTimeout is cfg.IdleTimeout — the SAME window a genuine login
	// opens (see iam.LoginDeps.IdleTimeout). Every call this handler
	// serves sets idle_deadline = now + idleTimeout; there is exactly
	// one window duration in this system, and it governs both "how long
	// after signing in can silence go unnoticed" and "how long after the
	// last click can silence go unnoticed".
	idleTimeout time.Duration
	// now is the clock this handler reads when computing the fresh
	// deadline, time.Now in production. Injectable for the same one
	// reason LoginHandlers.now is (see its doc comment): idle_deadline
	// travels as a Unix-SECOND timestamp, so a test that mints the
	// "before" session and calls activity within the same wall-clock
	// second would compute an identical value for both a correct
	// implementation and one mutated to leave the deadline unmoved —
	// exactly the false-positive the #848 Task 3 report documents. An
	// injectable clock lets a test step time forward deterministically
	// instead of sleeping.
	now func() time.Time
}

func newActivityHandlers(signer *session.Signer, ttl time.Duration, secureCookie bool,
	idleTimeout time.Duration,
) *activityHandlers {
	return &activityHandlers{
		signer:       signer,
		ttl:          ttl,
		secureCookie: secureCookie,
		idleTimeout:  idleTimeout,
		now:          time.Now,
	}
}

// activity re-mints the caller's session with idle_deadline pushed out
// to now + idleTimeout, carrying sub, tenant_id and auth_time through
// UNCHANGED.
//
// This route is registered INSIDE the authenticated /v1 chain (see
// Module.Routes), so authn.Middleware has already refused any request
// whose session is missing, invalid, revoked, or already past its OWN
// idle_deadline (session_idle, 401) before this handler ever runs. That
// refusal is load-bearing, not incidental: an idle-expired session must
// never be revivable by a single "I'm active" claim, or the whole
// control this endpoint exists to serve would be one request away from
// bypassed. This handler therefore has no idle-deadline check of its
// own to write — the absence is deliberate, not an omission. Rate
// limiting is likewise not this handler's job to enforce (see the type
// doc comment): ratelimit.Middleware, upstream of this handler in the
// chain, has already refused an over-budget request before this code
// runs.
func (h *activityHandlers) activity(c *gin.Context) {
	p, ok := authn.PrincipalFrom(c)
	if !ok {
		respond.Unauthenticated(c, "missing principal")
		return
	}

	if h.signer == nil {
		requestid.Logger(c).ErrorContext(c.Request.Context(),
			"session activity: no session signer configured; refusing to mint")
		respond.Error(c, http.StatusServiceUnavailable,
			"session_unavailable", "could not extend the session")
		return
	}

	// p.AuthTime is carried through UNMODIFIED, never h.now(). This is
	// the load-bearing line in this handler for the SAME reason it is in
	// login.go's Login and me.go's switchTenant: the #781 revocation
	// watermark compares a session's auth_time against a per-subject
	// revoked-after mark, and resetting it here would launder an old
	// authentication into a fresh one — an "I'm active" signal has
	// nothing to do with whether a human has re-authenticated, and must
	// never be treated as if it did.
	//
	// idleDeadline IS deliberately h.now().Add(h.idleTimeout) — this is
	// the one handler in the whole codebase allowed to compute a fresh
	// one rather than carry the existing value forward (spec D3/D4): a
	// caller reached this route only because the browser observed
	// genuine interaction, which is exactly the fact that justifies
	// opening a new window.
	//
	// Truncated to whole seconds before minting: idle_deadline travels
	// as a Unix-second integer claim (pkg/session's tokenClaims), so a
	// sub-second value here would round-trip through the token as
	// something other than what this response body reports — the two
	// would disagree about the deadline the client is told to warn
	// against, for no reason a client could act on. See
	// TestActivityBodyDeadlineMatchesTheMintedCookie for the assertion
	// that keeps this true.
	deadline := h.now().Add(h.idleTimeout).UTC().Truncate(time.Second)
	token, err := h.signer.Mint(p.Subject, p.TenantID, p.AuthTime, deadline)
	if err != nil {
		requestid.Logger(c).ErrorContext(c.Request.Context(), "session activity: mint session failed", "err", err)
		respond.Error(c, http.StatusServiceUnavailable,
			"session_unavailable", "could not extend the session")
		return
	}

	// Same cookie flags iam.LoginHandlers.Login and meHandlers.switchTenant
	// set: httpOnly and sameSite=Lax fixed, secure from config.
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(authn.SessionCookie, token, int(h.ttl.Seconds()), "/", "", h.secureCookie, true)

	// The one record that a session was ever extended: without this line
	// there is nothing anywhere — this handler's own logging or
	// otherwise — that answers "why was I signed out mid-shift" with
	// "you weren't; your last extension was at 14:32 and expired at
	// 14:47" during an incident review. Debug, not Info: this fires on
	// every genuine interaction (D4's 60s debounce), so at Info it would
	// dominate the log volume of every other authenticated route
	// combined for an active shift.
	requestid.Logger(c).DebugContext(c.Request.Context(), "session activity: extended idle deadline",
		"subject", p.Subject, "idle_deadline", deadline)

	respond.OK(c, activityResponse{IdleDeadline: deadline})
}

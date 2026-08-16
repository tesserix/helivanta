package iam

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tesserix/hms/internal/platform/requestid"
	"github.com/tesserix/hms/internal/platform/respond"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/ratelimit"
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
	// limiter and limit are this endpoint's own rate budget (#848 Task
	// 4), mirroring #841's login budget in shape though not in
	// placement: the SAME ratelimit.Limiter instance cmd/api/main.go
	// builds once for bootstrap.V1Chain and for iam.LoginDeps.Limiter —
	// reused, never a second construction — keyed under its own
	// "activity:" prefix. That prefix matters for the same reason
	// login's "login:" prefix does: this route is ALSO registered
	// through platform.Router inside the authenticated /v1 chain, so it
	// already passes through ratelimit.Middleware's "subject:" Principal
	// bucket — the budget every other authenticated route (products,
	// orders, the works) draws from for that caller. A clinician's
	// browser calls this endpoint roughly once a minute while active
	// (D4's 60s debounce, shared across tabs via BroadcastChannel), and
	// folding that traffic into the shared Principal bucket would mean
	// ordinary API calls and idle-keepalive calls compete for the same
	// allowance — an inactive-looking clinician mid heavy-use could find
	// their OWN activity calls throttled by their OWN product-list
	// requests, or vice versa. A distinct "activity:" bucket keeps this
	// endpoint's cheap, predictable traffic shape (~1/min) from ever
	// touching that budget, in either direction.
	//
	// A nil limiter fails OPEN (admits, warns), the same direction
	// login.go's Login takes and for the identical reason
	// (docs/standards/engineering-principles.md §3): this is a capacity
	// control, not a data/identity one, and a limiter that cannot decide
	// must not be the thing that lets a clinician's session lapse
	// mid-shift.
	limiter ratelimit.Limiter
	limit   ratelimit.Rule
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
	idleTimeout time.Duration, limiter ratelimit.Limiter, limit ratelimit.Rule,
) *activityHandlers {
	return &activityHandlers{
		signer:       signer,
		ttl:          ttl,
		secureCookie: secureCookie,
		idleTimeout:  idleTimeout,
		limiter:      limiter,
		limit:        limit,
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
// own to write — the absence is deliberate, not an omission.
func (h *activityHandlers) activity(c *gin.Context) {
	p, ok := authn.PrincipalFrom(c)
	if !ok {
		respond.Unauthenticated(c, "missing principal")
		return
	}

	// Budget sits here, mirroring login.go's placement rationale: after
	// the caller is known (there is no subject to key on before
	// authn.Middleware ran) and before the one expensive-ish operation
	// this handler performs (minting and signing a new token). See the
	// field doc comment on activityHandlers.limiter/limit for the
	// bucket's shape and why it is distinct from the Principal bucket
	// this route ALSO passes through via ratelimit.Middleware.
	if h.limiter != nil {
		if d := h.limiter.Allow("activity:"+p.Subject, h.limit, time.Now()); !d.Allowed {
			requestid.Logger(c).WarnContext(c.Request.Context(), "session activity rate limited",
				"subject", p.Subject, "retry_after_ms", d.RetryAfter.Milliseconds())
			respond.TooManyRequests(c,
				"too many activity signals; retry in "+d.RetryAfter.Round(time.Second).String(),
				d.RetryAfter, d.Limit, d.Remaining)
			return
		}
	} else {
		requestid.Logger(c).WarnContext(c.Request.Context(), "session activity: rate limiter unavailable, admitting (fail open)")
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
	// Truncated to whole seconds before minting: idle_deadline travels
	// as a Unix-second integer claim (pkg/session's tokenClaims), so a
	// sub-second value here would round-trip through the token as
	// something other than what this response body reports — the two
	// would disagree about the deadline the client is told to warn
	// against, for no reason a client could act on.
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
	respond.OK(c, activityResponse{IdleDeadline: deadline})
}

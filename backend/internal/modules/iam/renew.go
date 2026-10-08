package iam

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/session"
)

// UserStateChecker re-checks a subject's Zitadel state on every renewal
// (design spec D3, docs/superpowers/specs/2026-08-20-server-side-session-renewal-design.md).
// Task 1's *loginclient.Client satisfies it as-is, with no adapter; an
// interface exists purely so renew_test.go can substitute a fake without
// a live Zitadel.
type UserStateChecker interface {
	UserState(ctx context.Context, id string) (loginclient.UserState, error)
}

// renewalFraction is the divisor renewResponse.RenewAt uses: renew when
// 1/3 of THIS session's own TTL has elapsed. 3 is not arbitrary, but it
// is INHERITED rather than evidenced: it is the ratio the codebase
// already shipped (a 5-minute browser renewal interval against
// SESSION_TTL's 15-minute default: 15/5 = 3). Being exact about what
// that does and does not establish matters here — the browser-driven
// renewal the ratio came from never once succeeded in production (see
// renewalHandlers' doc comment below: the cross-site SameSite=Lax round
// trip always failed), so nothing about 3 was ever proved workable by
// use. What recommends it is that it leaves two thirds of the TTL as
// margin for a renewal to fail and be retried before the session lapses,
// and that keeping the shipped ratio changes one thing at a time.
//
// What IS new is expressing it as a ratio of the server's own SessionTTL
// rather than as a fixed client-side duration, so it keeps holding at
// whatever SESSION_TTL an operator configures — the exact coupling spec
// D5 calls for, closing the gap the retired client-side constant
// (RENEWAL_INTERVAL_MS in apps/shell/lib/renew.ts, deleted by this
// branch; its bounded successor is FALLBACK_RENEWAL_INTERVAL_MS) left
// open by having nothing tying it to SESSION_TTL at all.
const renewalFraction = 3

// renewAtFloor bounds renewResponse.RenewAt from below.
//
// renewalFraction's derivation is only sound for a plausible TTL — a
// SESSION_TTL=3s (a "3m" typo) would otherwise produce renew_at =
// now+1s, and every connected client would poll this endpoint, and
// therefore Zitadel's core API, once a second forever.
//
// cmd/api can no longer boot with such a TTL: config.RequireSessionTTL
// refuses anything under config.MinSessionTTL (#921), and that minimum is
// DERIVED from this constant — renewalFraction × renewAtFloor, the
// smallest TTL at which this floor never engages. renew_test.go pins the
// derivation at compile time, so raising this constant without raising
// config.MinSessionTTL does not build. The floor stays as defence in
// depth: renewAtFor's contract holds for any ttl it is handed,
// independent of how one binary validates its configuration.
const renewAtFloor = 30 * time.Second

// renewResponse is POST /v1/auth/renew's success body. RenewAt is the
// ONE channel (spec D5) that tells the browser when to call this
// endpoint again; the client obeys it rather than a constant of its own,
// which is what makes the coupling structural instead of a comment
// describing a gap.
//
// ExpiresAt (#941) is when the session this response just minted stops
// being honoured. The browser needs it to bound its RETRY cadence after a
// failed renewal: a fixed retry interval longer than the session's
// remaining lifetime means one failed renewal signs the clinician out. See
// expiresAtFor for why it can only ever be at or before the cookie's real
// exp, never after.
type renewResponse struct {
	TenantID  string    `json:"tenant_id"`
	RenewAt   time.Time `json:"renew_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// renewalHandlers backs POST /v1/auth/renew (#916, design spec D1/D3):
// the server-side replacement for the browser-driven silent renewal
// (oidc-client-ts's signinSilent through a hidden iframe) that spec D1
// retires — that mechanism depended on Zitadel's session cookie
// travelling on a cross-site iframe request, which a SameSite=Lax
// cookie never does (spec "The defect, observed in production").
//
// This endpoint takes NO body and NO token: the caller's EXISTING
// Helivanta session cookie is the only credential, the same cookie
// authn.Middleware has already verified, checked against the #781
// revocation watermark, and refused if idle-expired, before this
// handler ever runs. See Module.registerRenewal for why that placement
// — inside the authenticated /v1 chain, exactly like POST
// /v1/auth/session/activity — is what satisfies the design brief's
// first refusal condition ("absent, invalid, expired, or past
// idle_deadline") with no verification code duplicated here.
//
// OPERATIONAL NOTE (Review Round 1, "Also fix"): the Zitadel state
// check below fails CLOSED, on purpose (spec D3). That means a Zitadel
// outage lasting longer than SESSION_TTL logs out every clinician mid-
// consultation — the SAME user-visible harm #916 exists to prevent, from
// a different cause (Zitadel unreachable rather than a cookie that never
// travels). This is not a bug this task introduces or a behaviour this
// task should change: D3 is explicit that a refused renewal is the
// correct answer to an unreadable Zitadel response. It is recorded here
// so Task 4's e2e coverage and the eventual runbook can acknowledge it
// as a known, deliberate failure mode rather than rediscover it during
// an incident.
type renewalHandlers struct {
	signer       *session.Signer
	roles        platform.RoleLister
	userState    UserStateChecker
	ttl          time.Duration
	secureCookie bool
	// now is the clock this handler reads for RenewAt. time.Now in
	// production; renew_test.go injects it directly (same package) to
	// assert an EXACT RenewAt value rather than one within a tolerance
	// window.
	now func() time.Time
}

func newRenewalHandlers(signer *session.Signer, roles platform.RoleLister,
	userState UserStateChecker, ttl time.Duration, secureCookie bool,
) *renewalHandlers {
	return &renewalHandlers{
		signer: signer, roles: roles, userState: userState,
		ttl: ttl, secureCookie: secureCookie, now: time.Now,
	}
}

// renew re-checks everything a renewal must prove and, only past every
// check, re-mints the caller's session. Order matches the design brief:
// credential shape (the cookie, and only the cookie), then Zitadel
// state, then OpenFGA membership, then mint — the same "check to
// completion, mint last" shape Login's own doc comment insists on and for
// the same reason: minting first and refusing after would hand out
// exactly the session each check exists to withhold.
func (h *renewalHandlers) renew(c *gin.Context) {
	p, tenantUUID, ok := authn.TenantPrincipal(c)
	if !ok {
		return // TenantPrincipal already wrote the 401.
	}
	tenantID := tenantUUID.String()
	logger := requestid.Logger(c)

	// --- THE SHAPE CONTROL (Review Round 1 CRITICAL fix; made structural
	// in the final whole-branch review). The cookie IS this endpoint's
	// credential, by definition — see the type doc comment. But
	// authn.Middleware (pkg/authn/authn.go) authenticates from
	// `Authorization: Bearer` IN PREFERENCE TO the cookie whenever both
	// are present, and it accepts a Helivanta session JWT there too. So a
	// bearer-presented token still produces a valid, idle-deadline-checked
	// authn.Principal, and a caller replaying an exfiltrated session token
	// as a bearer header on a timer would be renewing on a credential this
	// endpoint never meant to serve.
	//
	// An earlier version of this check tested only that a session COOKIE
	// was present and claimed that "closes that shape outright". It did
	// not, and the reviewer proved it: `Authorization: Bearer <token>`
	// alongside any junk `Cookie: helivanta_session=...` satisfied a
	// presence test and returned 200 with a re-minted cookie. Presence of
	// one credential says nothing about WHICH credential authenticated the
	// request. What actually decides that is authn.Middleware's preference
	// order, so the refusal has to be stated against the bearer header
	// itself.
	//
	// Two checks, doing two different jobs, and it is worth keeping them
	// apart rather than collapsing them:
	//
	//   - This one is the SHAPE control: a bearer-authenticated renewal is
	//     not a shape this endpoint serves, so it is refused outright
	//     rather than served-but-narrowed. Refusing an unrecognised shape
	//     is the fail-closed direction (engineering-principles.md §3), and
	//     no legitimate caller is affected: the only client of this route
	//     is the browser's own same-origin POST, which never sets an
	//     Authorization header (apps/shell/lib/renew.ts), and every
	//     internal caller of the API uses the routes it was built for, not
	//     a session-renewal endpoint.
	//   - The CORRECTNESS control is further down: the re-mint carries
	//     idle_deadline forward from p.IdleDeadline, the already-verified
	//     principal. That is what makes the window unextendable no matter
	//     which credential got here, and it is the control that makes the
	//     residual risk small — this refusal narrows the surface, it is not
	//     what makes the surface safe.
	if strings.HasPrefix(c.GetHeader("Authorization"), "Bearer ") {
		// Review Round 2, N3: no "subject" field here — requestid.Logger(c)
		// (PrincipalMiddleware) already attaches it to every line this
		// logger emits; a second, explicit "subject" field would just
		// duplicate the key in the emitted JSON.
		logger.WarnContext(c.Request.Context(),
			"renew: Authorization: Bearer presented; refusing (the session cookie is this endpoint's only credential)")
		respond.Unauthenticated(c, "renewal requires the session cookie")
		return
	}

	// The positive half of the same rule: the cookie must actually be
	// here. Kept as its own check rather than folded into the one above —
	// a request with neither credential cannot have authenticated at all
	// today (authn.Middleware would have refused it), but a future auth
	// source (mTLS, a signed header from an internal mesh) would reach
	// this handler with a principal and no cookie, and "this endpoint
	// renews the cookie it was presented" should refuse that too rather
	// than mint one out of nothing.
	if _, err := c.Cookie(authn.SessionCookie); err != nil {
		logger.WarnContext(c.Request.Context(),
			"renew: no session cookie presented; refusing (the session cookie is this endpoint's only credential)")
		respond.Unauthenticated(c, "renewal requires the session cookie")
		return
	}

	// --- Spec D3: is the subject still active upstream in Zitadel? ---
	//
	// FAIL CLOSED on an unreadable answer, deliberately, per spec D3's
	// own words: "If Zitadel cannot be reached to answer, renewal is
	// refused rather than granted. A refused renewal costs a re-login;
	// a granted one on an unreadable answer is exactly the bound this
	// decision exists to hold." Concretely: this check exists to bound
	// how long a clinician deactivated DIRECTLY in Zitadel (no
	// corresponding Helivanta action) keeps a working session. Reading a
	// transient Zitadel error as "must still be active" collapses that
	// bound to infinity — an unreachable Zitadel would let every
	// deactivated subject keep renewing, silently, for exactly as long
	// as the very outage this check exists to be robust against. A
	// refused renewal costs one re-login the next time Zitadel answers;
	// there is no symmetric way back from granting the other direction.
	// See the type doc comment's OPERATIONAL NOTE for the corollary this
	// direction accepts.
	if h.userState == nil {
		logger.ErrorContext(c.Request.Context(),
			"renew: no zitadel user-state checker configured; refusing to renew")
		respond.Error(c, http.StatusServiceUnavailable,
			"identity_unavailable", "could not verify account status")
		return
	}
	state, err := h.userState.UserState(c.Request.Context(), p.Subject)
	if err != nil {
		// Same N3 reasoning as above: no redundant "subject" field —
		// requestid.Logger(c) already carries it.
		logger.ErrorContext(c.Request.Context(), "renew: zitadel user state unreadable, refusing (fail closed)",
			"err", err)
		respond.Error(c, http.StatusServiceUnavailable,
			"identity_unavailable", "could not verify account status")
		return
	}
	if !state.IsActive() {
		logger.WarnContext(c.Request.Context(), "renew: subject no longer active upstream, refusing")
		respond.Unauthenticated(c, "account is no longer active")
		return
	}

	// --- Re-check OpenFGA membership for the cookie's tenant, reusing
	// platform.RoleLister.ListRoles + hasBindingForTenant — the EXACT
	// mechanism Login uses (login.go), not a second implementation of
	// "is this subject still a member of this tenant". See
	// TestLogin_RenewalForSinceRevokedMemberIsRefused (login_test.go)
	// for the proof this mirrors, and Module.registerRenewal for why
	// this route is marked NoTenantMembership rather than Public so
	// this is the ONLY membership check it runs. ---
	if h.roles == nil {
		logger.ErrorContext(c.Request.Context(), "renew: no role lister configured; refusing to renew")
		respond.Error(c, http.StatusServiceUnavailable,
			"authz_unavailable", "authorization is temporarily unavailable")
		return
	}
	bindings, err := h.roles.ListRoles(c.Request.Context(), p.Subject)
	if err != nil {
		logger.ErrorContext(c.Request.Context(), "renew: list roles failed", "err", err)
		respond.Error(c, http.StatusServiceUnavailable,
			"authz_unavailable", "authorization is temporarily unavailable")
		return
	}
	if !hasBindingForTenant(bindings, tenantID) {
		respondNoAccessibleTenant(c)
		return
	}

	// --- Re-mint, carrying idle_deadline forward from THE ALREADY-
	// VERIFIED PRINCIPAL — p.IdleDeadline — rather than by independently
	// re-reading and re-verifying c.Cookie a second time (Review Round 1
	// CRITICAL fix; this file used to call login.go's
	// resolveIdleDeadline, which read the raw cookie on its own). p is
	// the SAME claim, off the SAME token that authn.Middleware already
	// verified and already proved is still in the future (the idle gate
	// above it in the chain would have refused it otherwise) — reading
	// it from the principal is strictly more correct than a second,
	// independent read that can silently disagree with what actually
	// authenticated this request, and it removes this handler's
	// dependency on *session.Verifier entirely. Login's own
	// idleDeadlineFor is NOT reused here: that function exists to
	// discriminate a genuine login from a renewal by comparing a FRESH
	// Zitadel principal against an OLD cookie — a distinction that does
	// not exist on this endpoint, which never sees a fresh IdP
	// credential at all. p.IdleDeadline is simply the deadline this
	// session is already running against; carrying it through
	// unconditionally IS the correct behaviour here, not an
	// approximation of Login's richer decision. ---
	now := h.now()
	if h.signer == nil {
		logger.ErrorContext(c.Request.Context(), "renew: no session signer configured; refusing to mint")
		respond.Error(c, http.StatusServiceUnavailable,
			"session_unavailable", "could not renew the session")
		return
	}
	token, err := h.signer.Mint(p.Subject, tenantID, p.AuthTime, p.IdleDeadline)
	if err != nil {
		logger.ErrorContext(c.Request.Context(), "renew: mint session failed", "err", err)
		respond.Error(c, http.StatusServiceUnavailable,
			"session_unavailable", "could not renew the session")
		return
	}

	// Same cookie flags Login/activity set: httpOnly and sameSite=Lax
	// fixed, secure from config.
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(authn.SessionCookie, token, int(h.ttl.Seconds()), "/", "", h.secureCookie, true)

	respond.OK(c, renewResponse{
		TenantID:  tenantID,
		RenewAt:   renewAtFor(now, h.ttl),
		ExpiresAt: expiresAtFor(now, h.ttl),
	})
}

// renewAtFor is THE one place the answer to "when should this client
// call POST /v1/auth/renew next" is computed, for every endpoint that
// mints a session. It is a package-level function rather than a method
// on renewalHandlers precisely so LoginHandlers.Login can call the
// SAME code (#916 Task 4, F3) instead of a second implementation that
// happens to agree today.
//
// That matters more than it looks. Spec D5 says the coupling between
// the client's renewal cadence and the server's SESSION_TTL is
// "structural instead of a comment describing a gap" — but before this
// function existed, the arithmetic lived inline in renew() and Login
// returned no schedule AT ALL, so the client's FIRST renewal came from
// a hardcoded five-minute client constant
// (apps/shell/lib/renew.ts's FALLBACK_RENEWAL_INTERVAL_MS). D5 was
// therefore closed for renewals 2..n and wide open for renewal 1: with
// SESSION_TTL below ~5 minutes every session died before its first
// renewal ever fired. (Since #921 config.RequireSessionTTL refuses a
// TTL under config.MinSessionTTL at boot, but 90s..5m remains valid and
// is exactly the range this coupling protects.) That is verbatim
// the sentence D5 was written to eliminate, and no test could see it
// because no test ran a short TTL.
//
// TestLoginAndRenewAgreeOnRenewAt (renew_test.go) asserts the two
// endpoints produce the byte-identical value for the same clock and
// TTL, which is what makes this shared rather than merely parallel.
func renewAtFor(now time.Time, ttl time.Duration) time.Time {
	renewAfter := ttl / renewalFraction
	if renewAfter < renewAtFloor {
		renewAfter = renewAtFloor
	}
	return now.Add(renewAfter).UTC()
}

// expiresAtFor is the session lifetime the browser is told about (#941):
// now + ttl. now is read BEFORE signer.Mint runs, and Mint stamps the token's
// own exp from a later clock read, so this value is at or slightly before
// the real exp — never after. That is the safe direction: a client that
// believes its session ends a little early retries a failed renewal a
// little early; one that believed it ended late could wait past expiry.
//
// The activity endpoint (activity.go) also re-mints the cookie, with a later
// exp, without telling the browser. The stored value is then earlier than
// the real one — the same safe direction.
//
// Truncated to the second because the token's exp is a JWT NumericDate,
// which jwt.NewNumericDate truncates to whole seconds. Without it, a now with
// a sub-second part would put this value up to a second AFTER the real exp —
// the unsafe direction. TestRenewExpiresAtIsNeverAfterTheCookiesExp pins it
// against a real minted cookie.
func expiresAtFor(now time.Time, ttl time.Duration) time.Time {
	return now.Add(ttl).Truncate(time.Second).UTC()
}

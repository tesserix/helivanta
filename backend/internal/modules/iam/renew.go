package iam

import (
	"context"
	"net/http"
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
// 1/3 of THIS session's own TTL has elapsed. 3 is not arbitrary — it
// reproduces the margin the codebase already shipped and proved workable
// (a 5-minute browser renewal interval against SESSION_TTL's 15-minute
// default: 15/5 = 3), expressed as a ratio of the server's own SessionTTL
// rather than as a fixed client-side duration, so it keeps holding at
// whatever SESSION_TTL an operator configures — the exact coupling spec
// D5 calls for, closing the gap RENEWAL_INTERVAL_MS
// (apps/shell/lib/renew.ts:16, a hardcoded constant with nothing tying it
// to SESSION_TTL) left open.
const renewalFraction = 3

// renewAtFloor bounds renewResponse.RenewAt from below. SESSION_TTL has
// no boot-time floor (config.go's getenvDuration + RequireIdleTimeout's
// own comment on why a bad value must not stop the API booting) and
// renewalFraction's derivation is only sound for a plausible TTL — a
// misconfigured SESSION_TTL=3s (a "3m" typo an operator could plausibly
// make) would otherwise produce renew_at = now+1s, and every connected
// client would poll this endpoint, and therefore Zitadel's core API,
// once a second forever. Fixed Review Round 1 finding "Also fix": floor
// at the point of use rather than at config load, because THIS is the
// one place a too-small interval actually causes harm (a hot polling
// loop against an external, rate-limited dependency), not the TTL value
// itself.
const renewAtFloor = 30 * time.Second

// renewResponse is POST /v1/auth/renew's success body. RenewAt is the
// ONE channel (spec D5) that tells the browser when to call this
// endpoint again; the client obeys it rather than a constant of its own,
// which is what makes the coupling structural instead of a comment
// describing a gap.
type renewResponse struct {
	TenantID string    `json:"tenant_id"`
	RenewAt  time.Time `json:"renew_at"`
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
// cookie presence, then Zitadel state, then OpenFGA membership, then
// mint — the same "check to completion, mint last" shape Login's own
// doc comment insists on and for the same reason: minting first and
// refusing after would hand out exactly the session each check exists
// to withhold.
func (h *renewalHandlers) renew(c *gin.Context) {
	p, tenantUUID, ok := authn.TenantPrincipal(c)
	if !ok {
		return // TenantPrincipal already wrote the 401.
	}
	tenantID := tenantUUID.String()
	logger := requestid.Logger(c)

	// --- Review Round 1 CRITICAL fix: the cookie IS this endpoint's
	// credential, by definition (see the type doc comment). But
	// authn.Middleware (pkg/authn/authn.go) authenticates from
	// Authorization: Bearer IN PREFERENCE TO the cookie when both are
	// present, and accepts a Helivanta session JWT there too. A bearer-
	// presented token still produces a valid, idle-deadline-checked
	// authn.Principal — so without this check, a caller could replay an
	// exfiltrated session token as a bearer header on a timer and renew
	// forever, because (before this fix) the carry-forward logic below
	// read idle_deadline from a SEPARATE re-read of c.Cookie, which is
	// simply absent for a bearer-only request and so fell through to a
	// FRESH now+idleTimeout window on every call — the exact "stolen
	// cookie" case authn.go's own idle-timeout comment calls out as
	// something that "fails closed here", silently not doing so for this
	// one route. Refusing a renewal that presents no session cookie at
	// all closes that shape outright, independently of the fix below
	// that stops trusting a second, independently-read credential for
	// the deadline in the first place.
	if _, err := c.Cookie(authn.SessionCookie); err != nil {
		// Review Round 2, N3: no "subject" field here — requestid.Logger(c)
		// (PrincipalMiddleware) already attaches it to every line this
		// logger emits; a second, explicit "subject" field would just
		// duplicate the key in the emitted JSON.
		logger.WarnContext(c.Request.Context(),
			"renew: no session cookie presented; refusing (bearer-only renewal is not a shape this endpoint serves)")
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

	renewAfter := h.ttl / renewalFraction
	if renewAfter < renewAtFloor {
		renewAfter = renewAtFloor
	}
	respond.OK(c, renewResponse{
		TenantID: tenantID,
		RenewAt:  now.Add(renewAfter).UTC(),
	})
}

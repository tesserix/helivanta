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
type renewalHandlers struct {
	sessions     *session.Verifier
	signer       *session.Signer
	roles        platform.RoleLister
	userState    UserStateChecker
	ttl          time.Duration
	idleTimeout  time.Duration
	secureCookie bool
	// now is the clock this handler reads for RenewAt and for
	// resolveIdleDeadline's "fresh" fallback branch — time.Now in
	// production, injectable for the same one reason
	// LoginHandlers.now is (see its doc comment): a test needs to
	// observe time moving between a login and a renewal.
	now func() time.Time
}

func newRenewalHandlers(sessions *session.Verifier, signer *session.Signer, roles platform.RoleLister,
	userState UserStateChecker, ttl, idleTimeout time.Duration, secureCookie bool,
) *renewalHandlers {
	return &renewalHandlers{
		sessions: sessions, signer: signer, roles: roles, userState: userState,
		ttl: ttl, idleTimeout: idleTimeout, secureCookie: secureCookie, now: time.Now,
	}
}

// renew re-checks everything a renewal must prove and, only past every
// check, re-mints the caller's session. Order matches the design brief:
// Zitadel state, then OpenFGA membership, then mint — the same "check to
// completion, mint last" shape Login's own doc comment insists on and
// for the same reason: minting first and refusing after would hand out
// exactly the session each check exists to withhold.
func (h *renewalHandlers) renew(c *gin.Context) {
	p, tenantUUID, ok := authn.TenantPrincipal(c)
	if !ok {
		return // TenantPrincipal already wrote the 401.
	}
	tenantID := tenantUUID.String()
	logger := requestid.Logger(c)

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
	if h.userState == nil {
		logger.ErrorContext(c.Request.Context(),
			"renew: no zitadel user-state checker configured; refusing to renew")
		respond.Error(c, http.StatusServiceUnavailable,
			"identity_unavailable", "could not verify account status")
		return
	}
	state, err := h.userState.UserState(c.Request.Context(), p.Subject)
	if err != nil {
		logger.ErrorContext(c.Request.Context(), "renew: zitadel user state unreadable, refusing (fail closed)",
			"err", err, "subject", p.Subject)
		respond.Error(c, http.StatusServiceUnavailable,
			"identity_unavailable", "could not verify account status")
		return
	}
	if !state.IsActive() {
		logger.WarnContext(c.Request.Context(), "renew: subject no longer active upstream, refusing",
			"subject", p.Subject)
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

	// --- Re-mint, carrying idle_deadline forward through the SAME
	// resolveIdleDeadline Login uses (login.go), never a second copy of
	// the carry-forward rule (spec D4 point 1, #848 spec D3). The
	// principal passed in is THIS renewal's own subject/auth_time —
	// identical to what the cookie already carries, since there is no
	// fresh Zitadel round trip on this path to produce anything else —
	// so resolveIdleDeadline's carry branch is the one that always fires
	// here; its other branches exist for Login's genuine-login case and
	// are deliberately left unreachable rather than special-cased away,
	// which is what makes this a true reuse and not a rewrite. ---
	if h.signer == nil || h.sessions == nil {
		logger.ErrorContext(c.Request.Context(), "renew: no session signer/verifier configured; refusing to mint")
		respond.Error(c, http.StatusServiceUnavailable,
			"session_unavailable", "could not renew the session")
		return
	}
	now := h.now()
	deadline := resolveIdleDeadline(c, h.sessions, h.idleTimeout,
		authn.Principal{Subject: p.Subject, AuthTime: p.AuthTime}, now)
	token, err := h.signer.Mint(p.Subject, tenantID, p.AuthTime, deadline)
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
		TenantID: tenantID,
		RenewAt:  now.Add(h.ttl / renewalFraction).UTC(),
	})
}

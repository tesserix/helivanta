package iam

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/internal/platform/requestid"
	"github.com/tesserix/hms/internal/platform/respond"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/ratelimit"
	"github.com/tesserix/hms/pkg/session"
)

// loginRequest is the body POST /v1/auth/login accepts: a Zitadel ID
// token to exchange for an HMS session, and an optional tenant_id
// choosing which tenant to mint the session for when the subject belongs
// to more than one. Omitted, the first tenant — by ListRoles' own
// deterministic tenant-then-role sort — is used; the caller can switch
// afterwards through POST /v1/iam/me/tenant (plan Task 5) without a new
// IdP round trip.
type loginRequest struct {
	IDToken  string `json:"id_token" binding:"required"`
	TenantID string `json:"tenant_id" binding:"omitempty,uuid"`
}

// noAccessibleTenantMessage is returned, byte-for-byte, whether the
// subject has never held a role anywhere in HMS or once held one and
// lost it, and also when the caller named a specific tenant_id it does
// not belong to. All three are indistinguishable at the one signal this
// handler has — OpenFGA's ListRoles returning bindings that do not cover
// the case — and the message must stay that way rather than leaking
// which one happened: this endpoint runs before any HMS session exists,
// so its refusal is the only information a caller who does not belong
// anywhere ever gets about their own account.
//
// The status code is 404, not 403 (#838 Task 5, reconciling this with
// docs/standards/backend.md's cross-tenant rule: "404, never 403 ... 403
// would confirm the subject exists somewhere"). A caller naming a real
// tenant_id it is not a member of is the same shape as any other
// cross-tenant lookup in this codebase, and a 403 here would confirm
// that tenant exists — precisely the disclosure the byte-identical body
// above already goes out of its way to avoid for the "no account at all"
// case. Answering 403 for one sub-case and 404 for the other would leak
// through the status code what the body deliberately hides, so both
// collapse to 404 for the same reason they already collapse to one
// message: this handler has exactly one signal and must not let any
// channel of the response — body OR status — distinguish what that
// signal cannot.
const noAccessibleTenantMessage = "no accessible hospital for this account"

// LoginHandlers backs POST /v1/auth/login, spec D1
// (docs/superpowers/specs/2026-08-15-zitadel-auth-design.md): verify a
// Zitadel ID token once, resolve tenant membership from OpenFGA, mint an
// HMS session.
//
// This is deliberately NOT registered through Module.Routes /
// platform.Router the way every other iam route is: it is the one
// endpoint that runs BEFORE an HMS session exists, so it can be gated by
// neither authn.Middleware (there is nothing yet to verify — the
// credential presented here is a Zitadel token, not an HMS session) nor
// authz.RequireMembership (membership is exactly what this handler
// itself determines, and minting is the very thing the check gates).
// cmd/api/main.go mounts Login directly on the raw engine, outside
// bootstrap.V1Chain, with a comment explaining why that placement is
// load-bearing rather than an oversight.
type LoginHandlers struct {
	verifier authn.TokenVerifier
	roles    platform.RoleLister
	signer   *session.Signer
	ttl      time.Duration
	// secureCookie mirrors the `secure` cookie flag apps/shell's
	// app/api/session/route.ts used to set (secure only outside
	// development) — see this field's use in Login for the exact
	// mapping. The API now sets this cookie itself (spec D1: HMS mints
	// its own session, and it is the only thing holding the signing
	// key), where the shell used to.
	secureCookie bool
	// limiter and limit are #841's budget on this endpoint, keyed on the
	// verified Zitadel subject once Login has one — see Login's own
	// comment on exactly where the check runs and why. limiter may be
	// nil (see Login's fail-open comment at the check itself);
	// docs/superpowers/specs/2026-08-16-login-rate-limit-design.md is
	// the full design.
	limiter ratelimit.Limiter
	limit   ratelimit.Rule
}

// NewLoginHandlers wires Login's dependencies: verifier authenticates
// the caller against Zitadel (pkg/authn.NewZitadelVerifier — the SAME
// verifier instance Task 3 built, reused rather than duplicated), roles
// resolves tenant membership from OpenFGA exactly the way
// meHandlers.tenants does (see roles' doc comment on Deps.Roles), and
// signer mints the HMS session itself. secureCookie is threaded from
// config rather than decided here, so this file has no direct
// environment dependency to get wrong. limiter is the SAME
// ratelimit.Limiter instance cmd/api/main.go builds for bootstrap.V1Chain
// — reused, not duplicated, per #841 — and limit is
// bootstrap.LoginRateLimitRule(cfg).
func NewLoginHandlers(verifier authn.TokenVerifier, roles platform.RoleLister, signer *session.Signer, ttl time.Duration, secureCookie bool, limiter ratelimit.Limiter, limit ratelimit.Rule) *LoginHandlers {
	return &LoginHandlers{verifier: verifier, roles: roles, signer: signer, ttl: ttl, secureCookie: secureCookie, limiter: limiter, limit: limit}
}

// Login verifies req.IDToken as a Zitadel ID token, resolves the
// caller's HMS tenant memberships from OpenFGA, and — provided the
// caller belongs to at least one tenant, and to the requested one if
// named — mints an HMS session and sets it as the response's session
// cookie.
//
// Every step before the mint is the gate: the membership check runs to
// completion, and only past a conclusive pass does anything get signed.
// Minting first and checking after would hand out exactly the session
// the check exists to withhold.
//
// bindings is resolved through platform.RoleLister.ListRoles — the same
// OpenFGA call meHandlers.tenants (me.go) and meHandlers.switchTenant
// already use to answer "which tenants does this subject belong to" —
// rather than a second, differently-scoped lookup. See roles' field doc
// comment on why iam_members (RLS-forced, single-tenant-scoped) cannot
// answer this question at all: OpenFGA is the only accessor that can.
func (h *LoginHandlers) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, err)
		return
	}

	principal, err := h.verifier.Verify(c.Request.Context(), req.IDToken)
	if err != nil {
		requestid.Logger(c).WarnContext(c.Request.Context(), "login: zitadel token verification failed", "err", err)
		respond.Unauthenticated(c, "invalid credentials")
		return
	}

	// #841: the budget sits HERE — after Verify succeeded (there is no
	// subject to key on before it) and before ListRoles (the OpenFGA
	// call every tenant's login traffic shares; refusing after it would
	// let a flood exhaust OpenFGA before anything was refused, the exact
	// mistake #689's TestThrottledRequestMakesNoOpenFGACall exists to
	// catch for the /v1 chain — TestLoginThrottledMakesNoRolesListCall is
	// this endpoint's equivalent). Keyed "login:"+subject, deliberately
	// NOT the plain "subject:"+subject bucket ratelimit.Middleware's
	// Principal rule reads: sharing it would let logging in drain the
	// budget every other authenticated route reads from, and vice versa.
	//
	// A nil limiter fails OPEN (admits, warns) rather than refusing to
	// mint a session: per
	// docs/standards/engineering-principles.md §3, this is a capacity
	// control, and a limiter that cannot decide must not take sign-in
	// down for a hospital — denying every login here would time out
	// every open tab's renewal within one SessionTTL, a worse outage than
	// the flood this budget exists to bound.
	// TestLoginAdmitsWhenLimiterUnavailable pins this direction; see
	// design spec D3 for why there is currently no other "cannot decide"
	// path (pkg/ratelimit.Memory.Allow has no error return).
	if h.limiter != nil {
		if d := h.limiter.Allow("login:"+principal.Subject, h.limit, time.Now()); !d.Allowed {
			slog.WarnContext(c.Request.Context(), "login rate limited",
				"subject", principal.Subject, "retry_after_ms", d.RetryAfter.Milliseconds())
			respond.TooManyRequests(c,
				"too many sign-in attempts; retry in "+d.RetryAfter.Round(time.Second).String(),
				d.RetryAfter, d.Limit, d.Remaining)
			return
		}
	} else {
		requestid.Logger(c).WarnContext(c.Request.Context(), "login: rate limiter unavailable, admitting (fail open)")
	}

	bindings, err := h.roles.ListRoles(c.Request.Context(), principal.Subject)
	if err != nil {
		requestid.Logger(c).ErrorContext(c.Request.Context(), "login: list roles failed", "err", err)
		respond.Error(c, http.StatusServiceUnavailable,
			"authz_unavailable", "authorization is temporarily unavailable")
		return
	}
	if len(bindings) == 0 {
		respondNoAccessibleTenant(c)
		return
	}

	// Default to the first binding's tenant — ListRoles returns bindings
	// sorted by tenant then role, so this is deterministic across calls
	// for the same subject/binding-set, not an arbitrary map-iteration
	// pick. A caller that already knows which tenant it wants (returning
	// from a previous /v1/iam/me/tenants call, say) may name it
	// explicitly; hasBindingForTenant is the SAME membership check
	// switchTenant uses, reused rather than re-implemented, and its
	// failure is folded into the identical noAccessibleTenantMessage so
	// naming a tenant the caller does not belong to discloses nothing
	// beyond "not this session".
	tenantID := bindings[0].TenantID
	if req.TenantID != "" {
		if !hasBindingForTenant(bindings, req.TenantID) {
			respondNoAccessibleTenant(c)
			return
		}
		tenantID = req.TenantID
	}

	// authTime is principal.AuthTime — Zitadel's auth_time, verified and
	// parsed by h.verifier, NEVER time.Now(). See session.Signer.Mint's
	// doc comment and spec D2: resetting it here would launder this
	// login into a fresh authentication no different from a genuine one,
	// which is exactly what would let a re-mint walk through a
	// revocation watermark set between the original Zitadel
	// authentication and this call.
	token, err := h.signer.Mint(principal.Subject, tenantID, principal.AuthTime)
	if err != nil {
		requestid.Logger(c).ErrorContext(c.Request.Context(), "login: mint session failed", "err", err)
		respond.Error(c, http.StatusServiceUnavailable,
			"session_unavailable", "could not issue a session")
		return
	}

	// httpOnly and sameSite=Lax are unchanged from what
	// apps/shell/app/api/session/route.ts used to set on this same
	// cookie name (authn.SessionCookie) before this endpoint existed;
	// secure now comes from config (h.secureCookie) rather than a
	// NODE_ENV check, but resolves the same way — true outside
	// development. The cookie's value changes from a raw Zitadel ID
	// token to an HMS session token, and it is now the API, not the
	// Next.js shell, that sets it: HMS is the only thing holding the
	// signing key (spec D1), so minting and cookie-setting belong in the
	// same place.
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(authn.SessionCookie, token, int(h.ttl.Seconds()), "/", "", h.secureCookie, true)
	respond.OK(c, gin.H{"tenant_id": tenantID})
}

// respondNoAccessibleTenant is the one refusal shape both "no bindings
// anywhere" and "named a tenant not in the bindings" collapse to — see
// noAccessibleTenantMessage's doc comment for why the status code, not
// just the body, must not distinguish the two. respond.NotFound is not
// used directly because it appends " not found" to a resource name,
// which would either name the tenant (disclosing it exists) or read
// oddly for the no-bindings-at-all case; this keeps the exact fixed
// message instead.
func respondNoAccessibleTenant(c *gin.Context) {
	respond.Error(c, http.StatusNotFound, "not_found", noAccessibleTenantMessage)
}

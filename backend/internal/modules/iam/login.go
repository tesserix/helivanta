package iam

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/ratelimit"
	"github.com/tesserix/helivanta/pkg/session"
)

// loginRequest is the body POST /v1/auth/login accepts: a Zitadel ID
// token to exchange for a Helivanta session, and an optional tenant_id
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
// subject has never held a role anywhere in Helivanta or once held one and
// lost it, and also when the caller named a specific tenant_id it does
// not belong to. All three are indistinguishable at the one signal this
// handler has — OpenFGA's ListRoles returning bindings that do not cover
// the case — and the message must stay that way rather than leaking
// which one happened: this endpoint runs before any Helivanta session exists,
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
// Helivanta session.
//
// This is deliberately NOT registered through Module.Routes /
// platform.Router the way every other iam route is: it is the one
// endpoint that runs BEFORE a Helivanta session exists, so it can be gated by
// neither authn.Middleware (there is nothing yet to verify — the
// credential presented here is a Zitadel token, not a Helivanta session) nor
// authz.RequireMembership (membership is exactly what this handler
// itself determines, and minting is the very thing the check gates).
// cmd/api/main.go mounts Login directly on the raw engine, outside
// bootstrap.V1Chain, with a comment explaining why that placement is
// load-bearing rather than an oversight.
type LoginHandlers struct {
	verifier authn.TokenVerifier
	roles    platform.RoleLister
	signer   *session.Signer
	// sessions verifies the caller's EXISTING Helivanta session cookie, if it
	// sent one. It is not an authentication gate — this endpoint has no
	// session to authenticate — it is how Login tells a silent renewal
	// apart from a genuine sign-in, which is the whole of spec D3. See
	// idleDeadlineFor.
	sessions *session.Verifier
	ttl      time.Duration
	// idleTimeout is cfg.IdleTimeout, the window a GENUINELY NEW login
	// opens. It is deliberately not read anywhere on the renewal path:
	// renewal carries the deadline it already had (D3).
	idleTimeout time.Duration
	// now is the clock idleDeadlineFor reads, time.Now in production.
	//
	// It exists for ONE reason, and it is not general testability: the D3
	// regression test has to observe a renewal that happens LATER than the
	// login it renews. idle_deadline travels as a Unix timestamp, so a
	// test that logs in and renews within the same wall-clock second
	// computes the identical "now + IdleTimeout" for both — and therefore
	// PASSES against an implementation that resets the deadline on every
	// renewal, which is precisely the bug it exists to catch. That was
	// observed, not theorised (see the task report for #848 Task 3): the
	// first version of this handler was mutated to reset unconditionally
	// and the test still went green. A sleep would work and would make the
	// suite slower and flakier for no gain; an injectable clock makes the
	// test deterministic and the assertion real.
	now func() time.Time
	// secureCookie mirrors the `secure` cookie flag apps/shell's
	// app/api/session/route.ts used to set (secure only outside
	// development) — see this field's use in Login for the exact
	// mapping. The API now sets this cookie itself (spec D1: Helivanta mints
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

// LoginDeps are Login's dependencies, passed as a struct rather than a
// positional argument list.
//
// That is not a style preference. #848 added a SECOND time.Duration
// (IdleTimeout) beside the existing TTL, and the two default to the same
// value — 15 minutes — for entirely unrelated reasons (config.go's
// IdleTimeout doc comment). Positionally, transposing them compiles,
// passes every test, and silently couples the two clocks spec D3 requires
// to stay independent: exactly the class of mistake this codebase asks to
// be made unrepresentable rather than remembered
// (docs/standards/engineering-principles.md — "compile error > boot
// failure > CI failure > documented convention"). Named fields make the
// transposition impossible to write.
type LoginDeps struct {
	// Zitadel authenticates the caller (pkg/authn.NewZitadelVerifier —
	// the SAME verifier instance #838 Task 3 built, reused rather than
	// duplicated).
	Zitadel authn.TokenVerifier
	// Roles resolves tenant membership from OpenFGA exactly the way
	// meHandlers.tenants does (see roles' doc comment on Deps.Roles).
	Roles platform.RoleLister
	// Signer mints the Helivanta session itself.
	Signer *session.Signer
	// Sessions verifies the caller's existing session cookie — the
	// renewal-vs-login discriminator (spec D3, see idleDeadlineFor).
	// Login refuses to mint without it rather than defaulting to "treat
	// everything as a genuine login", which would reset the idle deadline
	// on every renewal and disable the timeout entirely.
	Sessions *session.Verifier
	// TTL is this token's own lifetime (cfg.SessionTTL).
	TTL time.Duration
	// IdleTimeout is the window a genuinely new login opens
	// (cfg.IdleTimeout). NEVER applied to a renewal.
	IdleTimeout time.Duration
	// SecureCookie is threaded from config rather than decided here, so
	// this file has no direct environment dependency to get wrong.
	SecureCookie bool
	// Limiter is the SAME ratelimit.Limiter instance cmd/api/main.go
	// builds for bootstrap.V1Chain — reused, not duplicated, per #841 —
	// and Limit is bootstrap.LoginRateLimitRule(cfg).
	Limiter ratelimit.Limiter
	Limit   ratelimit.Rule
	// Now is the clock, defaulting to time.Now when nil. Production
	// leaves it unset; see LoginHandlers.now for the one reason it is
	// injectable at all.
	Now func() time.Time
}

// NewLoginHandlers wires Login's dependencies. See LoginDeps for what
// each one is and why the parameter is a struct.
func NewLoginHandlers(d LoginDeps) *LoginHandlers {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &LoginHandlers{
		now:          now,
		verifier:     d.Zitadel,
		roles:        d.Roles,
		signer:       d.Signer,
		sessions:     d.Sessions,
		ttl:          d.TTL,
		idleTimeout:  d.IdleTimeout,
		secureCookie: d.SecureCookie,
		limiter:      d.Limiter,
		limit:        d.Limit,
	}
}

// Login verifies req.IDToken as a Zitadel ID token, resolves the
// caller's Helivanta tenant memberships from OpenFGA, and — provided the
// caller belongs to at least one tenant, and to the requested one if
// named — mints a Helivanta session and sets it as the response's session
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

	// Fail closed with no session verifier: without it this handler
	// cannot tell a renewal from a genuine login (idleDeadlineFor), so
	// every renewal would be treated as a genuine one and re-open a full
	// idle window — an untouched tab would renew itself forever and the
	// #848 timeout would never fire, while everything looked healthy.
	// The unwired-dependency case is a deployment mistake, and the same
	// direction meHandlers.switchTenant takes for a nil signer: refuse to
	// issue, rather than issue something whose security property is
	// silently absent.
	if h.sessions == nil {
		requestid.Logger(c).ErrorContext(c.Request.Context(),
			"login: no session verifier configured; refusing to mint")
		respond.Error(c, http.StatusServiceUnavailable,
			"session_unavailable", "could not issue a session")
		return
	}

	// authTime is principal.AuthTime — Zitadel's auth_time, verified and
	// parsed by h.verifier, NEVER time.Now(). See session.Signer.Mint's
	// doc comment and spec D2: resetting it here would launder this
	// login into a fresh authentication no different from a genuine one,
	// which is exactly what would let a re-mint walk through a
	// revocation watermark set between the original Zitadel
	// authentication and this call.
	//
	// idleDeadline is likewise NOT unconditionally time.Now() +
	// IdleTimeout — see idleDeadlineFor, which is the whole of spec D3.
	// ONE read of the clock, used for both the idle deadline and the
	// renew_at hint below, so the two cannot describe different instants
	// in the same response.
	now := h.now()
	token, err := h.signer.Mint(principal.Subject, tenantID, principal.AuthTime,
		h.idleDeadlineFor(c, principal, now))
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
	// token to a Helivanta session token, and it is now the API, not the
	// Next.js shell, that sets it: Helivanta is the only thing holding the
	// signing key (spec D1), so minting and cookie-setting belong in the
	// same place.
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(authn.SessionCookie, token, int(h.ttl.Seconds()), "/", "", h.secureCookie, true)
	// renew_at tells the browser when to call POST /v1/auth/renew for
	// the FIRST time (#916 Task 4, F3). Computed by renewAtFor
	// (renew.go) — the same function the renewal endpoint itself uses,
	// not a second copy of the arithmetic — so the two cannot drift;
	// TestLoginAndRenewAgreeOnRenewAt pins that.
	//
	// Before this field existed, spec D5's "the client obeys the
	// server's schedule" held for every renewal EXCEPT the first, which
	// came from a hardcoded 5-minute constant in the browser
	// (apps/shell/lib/renew.ts's FALLBACK_RENEWAL_INTERVAL_MS). Any
	// deployment with SESSION_TTL under ~5 minutes therefore logged
	// every clinician out before their first renewal ever fired —
	// silently, since config.go applies no minimum to SESSION_TTL. This
	// is additive: a client that ignores the field behaves exactly as
	// before.
	respond.OK(c, gin.H{"tenant_id": tenantID, "renew_at": renewAtFor(now, h.ttl)})
}

// idleDeadlineFor decides the idle_deadline this mint carries. It is the
// load-bearing decision of #848 — spec D3, "the single most important
// rule in this spec, and the one most likely to be got wrong, because
// everything looks like it works when it is broken".
//
// Review Round 1 note (#916 Task 2): this was briefly split into a
// package-level resolveIdleDeadline so POST /v1/auth/renew could reuse
// it. That reuse was reverted — renew.go carries idle_deadline forward
// from the ALREADY-VERIFIED authn.Principal (p.IdleDeadline) instead,
// because this function's job is discriminating a genuine login from a
// renewal by comparing a FRESH Zitadel principal against an OLD cookie,
// a distinction POST /v1/auth/renew never has (it never sees a fresh IdP
// credential at all) — see renewalHandlers.renew's own comment. Back to
// a method with a single caller, LoginHandlers.Login.
//
// THE PROBLEM. Silent renewal (spec D4a) is not a separate endpoint: it
// is THIS handler, POST /v1/auth/login, run again every 5 minutes with a
// freshly-obtained Zitadel ID token. Route, method and body are identical
// to a first sign-in, so nothing about the REQUEST distinguishes them. If
// a re-mint set idle_deadline = now + IdleTimeout, an untouched tab would
// renew itself forever, the timeout would never fire, and every test
// asserting "renewal works" would still pass.
//
// THE DISCRIMINATOR is the caller's own existing Helivanta session cookie. A
// renewal always carries one (the browser attaches it automatically; it
// is httpOnly and same-origin), and that cookie already holds the
// deadline this session is running against. So:
//
//   - cookie present, genuine, and this same subject's, with no newer
//     authentication behind this request  ⇒  RENEWAL. Carry its
//     idle_deadline forward UNCHANGED.
//   - anything else  ⇒  GENUINE NEW LOGIN. now + IdleTimeout.
//
// Carrying forward can only ever keep or SHORTEN the window, never
// extend it: every deadline this system writes is minted as
// now + IdleTimeout at a genuine login (or at the activity endpoint,
// spec D4), so a carried value is by construction never later than the
// fresh one it replaces. There is therefore nothing here for a caller to
// abuse by presenting a cookie.
//
// THE THREE WAYS OUT of the carry branch, each deliberate:
//
//  1. No cookie, or one that does not verify. Nothing to carry.
//
//  2. The cookie's subject is not the subject Zitadel just verified. A
//     DIFFERENT human is signing in at a terminal where the previous one
//     never signed out — precisely the scenario in the issue. They get
//     their own full window; inheriting a stranger's remaining seconds
//     would sign them out mid-consultation. This can only ever WIDEN the
//     window for someone who has genuinely just authenticated as another
//     person, so it is not a way to extend one's own session.
//
//  3. principal.AuthTime is strictly after the session's auth_time — the
//     human has authenticated against Zitadel AGAIN since this session
//     was minted. That is a person standing at the keyboard, so a fresh
//     window is correct. A prompt=none renewal cannot reach this branch:
//     Zitadel returns the ORIGINAL auth_time on a silent re-authorization
//     (the same property the #781 revocation watermark relies on, see
//     authn.Principal.AuthTime), so a renewal's auth_time EQUALS the
//     one already in the session. Equality carries — the conservative
//     direction, mirroring the watermark check's own "not-after"
//     reasoning: within one second of clock granularity the two cases are
//     ambiguous, and the safe reading of an ambiguous renewal is that no
//     human was involved.
//
// WHY AN ALREADY-LAPSED DEADLINE IS CARRIED, NOT REFRESHED. This is the
// one place this implementation is deliberately stricter than the task
// brief, which expected a lapsed session to fall through to the
// genuine-login branch. It must not, and the reason is the same failure
// D3 exists to prevent, one layer down: a tab whose deadline has lapsed
// is STILL renewing every 5 minutes, and if a lapsed deadline earned a
// fresh window, the session would simply resurrect itself at the next
// renewal — dead for a few minutes, then alive again, forever. So a
// lapsed deadline is carried through verbatim and the re-minted session
// stays refused by authn.Middleware. session.Signer.Mint accepts a past
// deadline for exactly this reason (see its doc comment): the middleware
// stays the single place that decides "idle-expired". A human genuinely
// signing back in after a timeout still gets a fresh window — through
// case 3 above, on the strength of their new Zitadel authentication,
// which is the fact that actually means "someone is here".
//
// NOT COVERED BY THIS SLICE, tracked as #859: a cookie whose `exp` has
// lapsed (a machine that stopped renewing for longer than SessionTTL and
// was later woken) fails h.sessions.Verify, so its deadline cannot be
// read at all and the request takes case 1 — a fresh window on what may
// be a silent renewal with no human present. Closing it needs a
// pkg/session accessor that surfaces a deadline from a token the
// Verifier refuses, structurally unusable as an authentication result;
// that is a design decision rather than a patch, which is why it is a
// separate issue and not a TODO here.
func (h *LoginHandlers) idleDeadlineFor(c *gin.Context, principal authn.Principal, now time.Time) time.Time {
	fresh := now.Add(h.idleTimeout)

	raw, err := c.Cookie(authn.SessionCookie)
	if err != nil || raw == "" {
		return fresh
	}
	claims, err := h.sessions.Verify(raw)
	if err != nil {
		// Not logged at error level and never surfaced to the caller: an
		// unverifiable cookie on a login request is ordinary (a session
		// from a rotated key, a stale cookie after a redeploy), and the
		// request is about to succeed or fail on its own merits anyway.
		return fresh
	}
	if claims.Subject != principal.Subject {
		return fresh
	}
	if principal.AuthTime.After(claims.AuthTime) {
		return fresh
	}
	return claims.IdleDeadline
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

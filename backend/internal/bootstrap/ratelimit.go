package bootstrap

import (
	"time"

	"github.com/tesserix/helivanta/internal/config"
	"github.com/tesserix/helivanta/pkg/ratelimit"
)

// RateLimitConfig builds the rate-limiting policy from cfg. cmd/api calls
// this rather than constructing ratelimit.Config inline so the exemption
// and tight-route allowlists live in exactly one place: the arch test
// (internal/archtest/ratelimit_test.go) calls this same function to pin
// them, rather than a hand-copied map that would only prove someone
// transcribed the routes correctly, not that the route names are real.
//
// Burst is Rate/6 for the tenant and principal rules — ten seconds' worth
// — matching the design spec's reasoning that a burst smaller than a
// page load throttles ordinary navigation (a dashboard resolves
// permissions, then a zone list, then a panel, in quick succession). The
// mint rule's burst is fixed at 3 rather than derived the same way:
// Rate/6 at the default of 10/min would floor to 1, which is smaller
// than the burst the tenant-switch flow itself needs.
// `POST /v1/iam/me/tenant` was Tight's one entry through #838, budgeted
// tighter than every other route because it minted a GIP custom token
// per call against Identity Platform's project-wide quota — exhausting
// that quota broke sign-in for every hospital, not just the caller's.
// #838 replaced that mint with an in-process Helivanta session re-mint (an
// Ed25519 signature, no network call) plus one OpenFGA membership
// check — the SAME shape of cost every other authenticated route
// already pays through authz.Middleware's own Resolve call. There is no
// longer a shared external resource that route uniquely threatens, so
// the rationale for a tighter-than-default budget on it is gone; it was
// left off the Tight map rather than carried forward with a stale
// comment, per spec D3's explicit note that this budget "should be
// revisited when this lands" and "this spec does not silently inherit
// its reasoning." It still gets the ordinary Tenant/Principal budgets
// below, same as any other route.
//
// POST /v1/auth/session/activity (#848 Task 4) is Tight's current
// entry, for a DIFFERENT reason than the tenant-switch one above: it is
// not a shared-external-resource concern, but a traffic-shape one. Spec
// D4 debounces a browser's activity calls to at most once per 60
// seconds, shared across every open tab via BroadcastChannel, so one
// clinician's legitimate traffic is roughly 1/min regardless of tab
// count — nothing like the burst a dashboard's several concurrent
// panel-load requests need, which is what the Principal rule's Burst is
// actually sized for. Folding this route into the shared Principal
// bucket would mean ordinary API calls and idle-keepalive calls compete
// for the same allowance: an active clinician's own page-load traffic
// could throttle their own activity calls, or vice versa. Tight gives
// it its own "subject:...:route" bucket (see
// pkg/ratelimit.Middleware) at Rate=cfg.RateLimitActivityPerMin
// (default 10/min, an order of magnitude above the ~1/min sustained
// rate) and Burst=3 (comfortably covers the BroadcastChannel-unavailable
// fallback D4 describes — "per-tab timers ... more requests, same
// behaviour" — for a small number of tabs firing within the same
// second), while staying far below the shared Principal budget
// (RateLimitPrincipalPerMin, default 120/min) every other request from
// the same subject also draws from.

// LoginRateLimitRule builds the budget for POST /v1/auth/login (#841),
// keyed on the verified Zitadel subject rather than folded into Principal
// above. It is applied inline in iam.LoginHandlers.Login, not through
// ratelimit.Middleware — that route runs entirely outside V1Chain (there
// is no authn.Principal yet to key Middleware's Principal bucket on; see
// login.go's own doc comment on why it is mounted directly on the
// engine) — so it needs its own Rule, not a Tight/Exempt entry: Tight and
// Exempt only affect routes that actually pass through
// ratelimit.Middleware, and TestRateLimitPolicyRoutesAreRegistered
// verifies every Tight/Exempt key against routes registered through
// platform.Router, which POST /v1/auth/login (registered via
// bootstrap.MountUnauthenticated) is not one of — adding it to either map
// would be silently dead configuration, not a working control.
//
// Burst is fixed at 10, not derived from Rate/6 the way Tenant/Principal
// above are. See
// docs/superpowers/specs/2026-08-16-login-rate-limit-design.md D2 for the
// full arithmetic: Rate/6 sizes a burst to "one page load's worth of
// parallel requests", which is not this endpoint's traffic shape. This
// endpoint's legitimate bursts come from #838 D4a's silent renewal
// (RENEWAL_INTERVAL_MS, 5 minutes) firing from every open shell tab —
// tabs opened close together renew in a synchronized cluster every 5
// minutes for as long as they stay open, and 10 is a generous bound on
// how many tabs one clinician has open at once. Rate=20/min (default)
// refills a fully-drained 10-token burst in 30s, comfortably inside that
// 5-minute gap, while staying an order of magnitude above the ~2/min a
// heavy 10-tab clinician actually sustains.
func LoginRateLimitRule(cfg config.Config) ratelimit.Rule {
	return ratelimit.Rule{Rate: cfg.RateLimitLoginPerMin, Burst: 10, Per: time.Minute}
}

// FactorRateLimitRule builds the budget for POST /v1/auth/login/factor
// (#867 Task 4), keyed on client IP the same way LoginRateLimitRule and
// iam.LoginUIHandlers' other three routes are (iam.allowedByLimiter's
// doc comment: there is no verified subject on any of these routes —
// that is what the flow is establishing). It is its OWN Rule, drawn from
// its OWN "login_factor:" bucket (iam/loginui.go), never folded into
// LoginRateLimitRule's shared budget — see that constant's doc comment:
// this is a PRIMARY control, not a secondary one, because
// loginAttemptStore's five-guess-per-ATTEMPT counter (spec D6) only
// bounds one Zitadel session's guesses. An attacker who already holds a
// password can always start a fresh OIDC flow to mint a fresh
// auth_request_id — and therefore a fresh five-guess budget — for as
// many attempts as the ROUTE will admit. What actually bounds the total
// number of attempts an attacker gets is this limiter, not the per-row
// counter underneath it.
//
// # The arithmetic — what this Rule actually buys, stated honestly
//
// A TOTP code is six digits, a 10^6 space. Zitadel applies a delay to
// repeated failures rather than a hard lockout on this endpoint (spec
// D6), so nothing upstream of this limiter bounds a sustained guessing
// campaign except this Rule and the five-per-attempt counter it sits on
// top of. RATE_LIMIT_FACTOR_PER_MIN defaults to 10/min — half
// RateLimitLoginPerMin's 20/min default, deliberately tighter, because
// this route's legitimate traffic is a SINGLE TOTP submission per login
// (occasionally two, for an honest mistype) rather than the multi-tab
// renewal traffic LoginRateLimitRule's 20/min accommodates. Burst is
// fixed at 5, matching maxFactorAttempts (loginattempt.go) — exactly one
// login attempt's worth of wrong-code submissions in a single burst, no
// more: a clinician legitimately mistyping a rolling code a few times in
// a row must not be refused mid-attempt by the ROUTE limiter before
// loginAttemptStore's own five-guess counter would have stopped them
// anyway, but there is no legitimate reason for a burst larger than the
// per-attempt budget itself.
//
// An EARLIER version of this comment claimed 10/min "bounds the total
// number of attempts an attacker gets" and framed the cost as 10^6/10 ≈
// 69 days to EXHAUST the code space. Both statements were wrong, or at
// least badly incomplete, and are corrected here rather than left to
// mislead the next reader:
//
//  1. 69 days measures FULL enumeration (trying all 10^6 codes). An
//     attacker does not need to try all of them — they need ONE hit.
//     Zitadel's own 30-second TOTP validity window (with a small
//     clock-skew tolerance either side) means at any instant there are
//     effectively a handful of VALID codes out of 10^6, so a single
//     random guess succeeds with probability on the order of a few in
//     10^6. Expected time-to-success for a Bernoulli process is roughly
//     HALF of full-enumeration time in the naive "one shot per code, no
//     repeats" framing this comment originally used — the 69-day figure
//     was never the right number to reason about attacker success with
//     in the first place, and citing it as if it were the "cost" of an
//     attack materially overstated how long this Rule holds.
//  2. At the sustained rate this Rule actually admits — 10/min per IP,
//     i.e. 600/hr, i.e. 14,400/day — an attacker running ONE IP against
//     a 10^6 code space is attempting roughly 14,400/10^6 ≈ 1.4% of the
//     space PER DAY. Over a month (~30 days) that is ≈ 1 − (1 −
//     14,400/10^6)^30 ≈ 35% cumulative probability of a hit, not a
//     69-day floor on success. This Rule slows a single-source attacker
//     by roughly an order of magnitude versus no limiter at all; it does
//     not make the attack impractical on its own.
//  3. Because the bucket is keyed on c.ClientIP() (see
//     iam.allowedByLimiter — there is no verified subject on this route
//     to key on instead, until AFTER a factor check succeeds), this Rule
//     is a PER-IP budget, not a per-account one. A distributed attacker
//     running the SAME campaign from a hundred IPs gets roughly a
//     hundred times the daily attempt budget against the SAME account —
//     day-scale success probability for that attacker, not month-scale.
//     This Rule does not, and structurally cannot on its own, "bound the
//     total number of attempts an attacker gets" against one account; it
//     only bounds the rate from any ONE source.
//
// # What would close the distributed-attacker gap, and why it is not built here
//
// loginAttemptStore's rows carry Subject (the login name a factor
// attempt is FOR — loginui.go's Password handler), which is enough
// information to key a SECOND, per-subject budget across auth_request_ids
// — the one thing this per-IP Rule structurally cannot do. That is
// deliberately NOT implemented in this fix round. #855 ("No account
// lockout: password attempts against a known account are unlimited")
// already covers exactly this failure class for the PASSWORD step —
// #855's own acceptance criteria explicitly weigh "further attempts
// refused regardless of source" against "a locked-out clinician needs a
// documented, tested path back in during a shift" as a genuine,
// unresolved product/clinical-workflow decision, not a mechanical one.
// The TOTP factor step has the identical trade-off: a per-subject bound
// or delay closes the distributed-guessing gap this comment now states
// honestly, but it also hands an attacker who merely knows a clinician's
// login name (no password, no factor — the login name alone) a way to
// keep that SPECIFIC clinician locked out of a hospital system for as
// long as they sustain traffic, which is a patient-safety-relevant
// denial of service, not a security improvement, for a system with no
// account-lockout policy by design (#855) and where Zitadel's OWN chosen
// mitigation is a growing delay, never a hard lockout (MFA spike §3).
// Building a per-subject control ad hoc inside this fix round, without
// the same lockout-vs-availability design decision #855 already flags as
// needing deliberate product input, would risk introducing a NEW
// clinical-availability incident to close a guessing-speed gap — this
// repository's engineering principles put "fail closed, correctness
// over speed" ahead of quick patches, but do not license shipping an
// under-designed control for a decision this consequential either. This
// is an ACCEPTED, DOCUMENTED residual for #867 Task 4, not an oversight:
// #855 is the right place to decide (and implement) an account-level
// bound or delay that spans BOTH the password and the TOTP steps
// consistently, and this comment now says so instead of implying the
// per-IP Rule above already closes the gap.
func FactorRateLimitRule(cfg config.Config) ratelimit.Rule {
	return ratelimit.Rule{Rate: cfg.RateLimitFactorPerMin, Burst: 5, Per: time.Minute}
}

func RateLimitConfig(cfg config.Config) ratelimit.Config {
	return ratelimit.Config{
		Tenant: ratelimit.Rule{
			Rate: cfg.RateLimitTenantPerMin, Burst: cfg.RateLimitTenantPerMin / 6, Per: time.Minute,
		},
		Principal: ratelimit.Rule{
			Rate: cfg.RateLimitPrincipalPerMin, Burst: cfg.RateLimitPrincipalPerMin / 6, Per: time.Minute,
		},
		Tight: map[string]ratelimit.Rule{
			"POST /v1/auth/session/activity": {
				Rate: cfg.RateLimitActivityPerMin, Burst: 3, Per: time.Minute,
			},
		},
		Exempt: map[string]string{
			"POST /v1/iam/me/sign-out":              "a clinician on a shared ward terminal must always be able to end their session",
			"POST /v1/iam/subjects/:subject/revoke": "incident response; an attacker looping requests is exactly what would trip the limiter",
		},
	}
}

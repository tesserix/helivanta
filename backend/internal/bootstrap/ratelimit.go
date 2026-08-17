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

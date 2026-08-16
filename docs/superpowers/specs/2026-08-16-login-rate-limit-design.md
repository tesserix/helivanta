# Login exchange rate limit — design

**Issue:** [#841](https://github.com/tesserix/hms/issues/841) — rate limit
`POST /v1/auth/login`.
**Status:** implemented 2026-08-16.
**Related:** #689 (the general per-tenant/per-principal limiter this reuses),
#49 (unauthenticated/edge flooding — explicitly NOT this spec's scope), #838
(introduced the login exchange and its renewal reuse, D1/D4a).

---

## Problem, corrected

#841's original description claimed an unauthenticated attacker could force
"unbounded signature verifications **and** OpenFGA queries" through
`POST /v1/auth/login`. Reading `internal/modules/iam/login.go` disproves the
second half — the handler verifies the caller-presented Zitadel ID token
(`h.verifier.Verify`, line ~118) **before** it calls OpenFGA's `ListRoles`
(line ~125). A caller with no valid token 401s at verification and never
reaches OpenFGA.

The threat is really two, and they need different controls:

1. **Unauthenticated flood** — burns local CPU on signature verification
   (`go-oidc` caches the JWKS, so there is not even a per-request network
   call). Real, but this is edge/WAF territory #689 already carved out, owned
   by #49. An in-process IP limit needs trusted-proxy handling this codebase
   does not have (Cloudflare **and** Istio sit in front), and behind a
   hospital NAT one egress IP covers hundreds of staff — a budget tight
   enough to stop an attacker throttles a shift change. Out of scope here,
   deliberately, per the issue's correction comment.
2. **Authenticated flood** — a caller holding a *valid* Zitadel token
   hammering the endpoint genuinely reaches OpenFGA's `ListRoles`, which
   every tenant's traffic shares. **This is what this spec closes.**

## Decision — D1: limit keyed on the verified subject, between verify and ListRoles

Placement is exact, mirroring #689's `ratelimit.Middleware` ordering
argument, applied inline because this route runs entirely outside
`bootstrap.V1Chain` (spec D1, #838): it has no `authn.Principal` yet for
`ratelimit.Middleware` to key on.

- **After verification** — there is no subject to key on before it, and
  fabricating one (e.g. from an unverified claim) would be exactly the kind
  of "inventing an identity" this codebase avoids.
- **Before `ListRoles`** — OpenFGA is the shared resource. Limiting after the
  call would let a flood exhaust it before anything was refused, the same
  mistake #689's `TestThrottledRequestMakesNoOpenFGACall` guards against for
  the `/v1` chain. This spec adds the equivalent proof for the login path:
  `TestLoginThrottledMakesNoRolesListCall`.

Reuses `pkg/ratelimit.Limiter`/`Memory` — the same limiter instance
`cmd/api/main.go` already constructs for the `/v1` chain — rather than a
second implementation. The bucket key is namespaced `"login:" + subject`,
**not** the plain `"subject:" + subject` bucket `ratelimit.Middleware` uses
for the Principal rule: sharing one bucket would let logging in drain the
budget every other authenticated route reads from and vice versa, the exact
failure `Tight`'s per-route key already avoids (see
`backend/pkg/ratelimit/middleware.go`'s comment on tight-rule keys).

## Decision — D2: the budget

**RENEWAL, not login, is the sizing constraint.** A human logs in once per
shift; per D4a (#838) renewal re-runs the same exchange every
`RENEWAL_INTERVAL_MS` (5 minutes, one third of the 15-minute `SessionTTL`) for
every shell tab a clinician has open, for as long as that tab stays open —
this is the sustained, recurring load, not the occasional first sign-in.

Each shell tab's renewal timer starts on mount, independent of any other
tab's. A clinician who opens several tabs close together (a common real
pattern — one tab per patient, or a ward terminal left with the shift's
worklist open in multiple tabs) gets renewal timers that fire within the
same second of each other, and stay synchronized every 5 minutes afterward
for as long as those tabs live. That is a legitimate cluster, not an attack,
and the budget must never throttle it.

- **Burst = 10.** Ten tabs opened together, all renewing in the same instant,
  is a generous bound for one clinician — comfortably above what's normal
  (2-3 tabs) while still bounded. `10` is picked directly rather than derived
  from `Rate/6` (the general Tenant/Principal convention): that convention
  sizes a burst to "one page load's worth of parallel requests", a shape
  that does not describe this endpoint's actual traffic (bursts driven by
  synchronized renewal timers across tabs, not by one page's own subresource
  fan-out), so inheriting it would size the burst by an unrelated arithmetic
  and get either bound wrong.
- **Rate = 20/min** (one token every 3s). This refills a fully-drained
  10-token burst in 30 seconds — comfortably inside the 5-minute gap before
  the next synchronized renewal cluster — while still bounding the endpoint
  well below what an authenticated flood could otherwise throw at
  `ListRoles`. Sustained legitimate draw for even a heavy 10-tab clinician is
  10 requests / 5 min = 2/min, an order of magnitude under the 20/min
  budget, so no legitimate renewal pattern observed in the codebase's own
  `RENEWAL_INTERVAL_MS`/`SessionTTL` arithmetic can trip it.
- Configurable via `RATE_LIMIT_LOGIN_PER_MIN` (env, `getenvInt`, default 20),
  matching the existing `RATE_LIMIT_TENANT_PER_MIN`/`RATE_LIMIT_PRINCIPAL_PER_MIN`
  pattern (`internal/config/config.go`) — capacity genuinely differs between
  a laptop running the e2e suite and a production hospital, but 20/min with
  burst 10 is deliberately left as the dev default too (no Makefile
  override): each e2e spec logs in under its own account (subject-keyed
  bucket), and even the sign-in retry loop the e2e login helper runs
  (`MAX_SIGN_IN_ATTEMPTS = 4`) fits well inside burst 10, so nothing in the
  existing suite is at risk of tripping this budget by accident. The
  restored `ratelimit.spec.ts` trips it on purpose, from the same account,
  by looping past the burst.

## Decision — D3: fails open, argued at the decision point

Per `docs/standards/engineering-principles.md` §3, this is a **capacity**
control, not a data or identity control — the same classification #689
already gives the `/v1` chain's limiter, and for the identical reason: a
limiter that cannot decide must not take sign-in down for a hospital.
Denying every login because rate-limit accounting is confused is a worse
outage than the flood the limiter exists to prevent — it would deny even
legitimate renewal, timing out every open tab within one `SessionTTL`.

`pkg/ratelimit.Memory.Allow` has no error return today (no backing store to
be unavailable), so there is no decision-can't-be-made path to fail open
*from* yet. The one place this endpoint's wiring genuinely can "not decide"
is a nil `Limiter` — an unwired dependency, the shape a future refactor or a
partially-constructed test double could produce. `LoginHandlers.Login`
treats that the same as engineering-principles §3's #7 Redis note already
promises: **admit the request and log a warning**, rather than refuse to
mint a session. `TestLoginAdmitsWhenLimiterUnavailable` pins this — the
inverse (denying on a nil limiter) is the mutation §5 of this spec's
implementation plan proves breaks it.

## Explicitly not covered

- **Unauthenticated flooding of this endpoint stays with #49.** Nothing in
  this change limits by IP or before token verification. No code comment
  written here implies otherwise.
- **A Redis-backed limiter (#7).** Same interface-first deferral #689
  already made; this spec adds no new decision on that axis.

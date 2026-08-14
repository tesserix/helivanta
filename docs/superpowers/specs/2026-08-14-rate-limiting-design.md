# Per-tenant rate limiting — design

**Issue:** [#689](https://github.com/tesserix/hms/issues/689) — Per-tenant rate
limiting & quotas.
**Status:** approved 2026-08-14.
**Related:** #447 (the product story), #47, #774 Tier 2, ADR-0004, ADR-0005,
PR #815 (the fail-safe carve-out this depends on).

---

## Problem

Nothing on the platform limits anything. Two exposures are real today, both
verified in the code on 2026-08-14:

1. **`POST /v1/iam/me/tenant` mints a GIP custom token on every call**
   (`internal/modules/iam/me.go:179`). The call rate is unbounded, and Google
   Identity Platform quota is **project-wide** — exhausting it breaks sign-in
   for every tenant, not just the caller's. This is the one exposure that
   reaches a shared external resource.
2. **`SetMaxOpenConns(5)` is hardcoded** (`pkg/tenantdb/db.go:55`). Five
   concurrent slow queries occupy the entire pool for the whole process, so one
   tenant's expensive workload stalls every other tenant's requests.

Beyond those, the general noisy-neighbour case: one busy hospital must not
degrade another.

### Scope, and why this spec is much narrower than the issues

#689 and #447 together describe a subsystem, not a feature. #447 lists
per-route-class token buckets, per-tenant concurrency caps, connection-pool
partitioning, statement-timeout profiles, JetStream consumer limits, Redis
memory accounting, an EmergencyConnect protected reservation, tier-driven
configuration through the subscription model, and soft-limit warnings emitting
`TenantQuotaWarning`.

Most of that depends on things that do not exist: subscription tiers,
EmergencyConnect, a deployment (#7), a metrics sink (#679). This spec covers
what is real now and defers the rest explicitly rather than gesturing at it.

**#689's "Redis-backed" premise is also re-examined rather than inherited.** It
belongs to the `[Go SDK]` series that ADR-0004 records as describing a topology
HMS does not have — a separately-versioned module consumed by ~30 services. HMS
is one Go module with one deployable.

---

## Decisions

### D1 — In-process counters now, behind an interface Redis can implement later

A token bucket in memory, reached through a `Limiter` interface. No new runtime
dependency, no network call on the request path, nothing new to operate.

**Why an interface with one implementation.** Normally that is speculative
generality. Here it is the specific thing that makes the deferral honest.
ADR-0005's position is: build the boundary now, pay for the distributed version
when a trigger justifies it. The trigger is #7 landing with a known replica
count. Without the interface, "we will swap in Redis later" is exactly the
unfunded promise ADR-0005 exists to prevent; with it, the swap is one
constructor.

**The cost, stated rather than buried.** With N replicas the effective global
limit is N × configured.

- For the **connection pool** this is *correct*: the pool is per-process, so a
  per-process limit is the right shape.
- For **GIP quota** it is *approximate*: the limit bounds each replica's mint
  rate, not the project's. This is why the tight budget on the mint path is set
  low enough that several replicas together stay well inside project quota.

*Rejected: Redis now, as #689 specifies.* Accurate globally, but adds a required
runtime dependency no Go code uses today, a network call per limited request, a
new request-path failure mode, and operational surface (#7, #45) that does not
exist. It also front-runs a decision better made when the deployment shape is
known.

*Rejected: Postgres-backed counters.* No new dependency, but it puts a write on
the request path against the very pool this work protects — close to
self-defeating — and a shared instance is the wrong place for high-frequency
counter updates.

### D2 — A uniform default, plus a tight rule for shared external resources

```
                          rate           burst
default   per tenant      600 req/min    100
          per principal   120 req/min     20

tight     POST /v1/iam/me/tenant
          per principal    10 req/min      3
```

**Burst is specified, not left to the implementation.** A token bucket has two
parameters and giving only the rate is the kind of gap that gets filled by
whoever writes the code first. Burst is the bucket's capacity: how many requests
may arrive at once before the refill rate governs.

The values are one-tenth of the per-minute rate, roughly ten seconds' worth.
That is deliberate: a clinical screen loading fires several requests together
(the dashboard resolves permissions, then a zone list, then a panel), so a burst
smaller than a page load would throttle normal navigation. A burst equal to the
full minute's budget would let one client consume a tenant's entire allowance
instantly, which defeats the point.

**Applied at the `/v1` group only.** `/healthz` and `/readyz` are outside it and
stay unlimited — a limiter that can throttle a readiness probe can take a
healthy replica out of service, which is the failure mode a capacity control
must not cause.

The default applies to every `/v1` route, so no route can be forgotten — the
same "enforce structurally" reasoning as §4. The tight map holds exactly one
entry today, and adding to it is a visible decision rather than config sprawl.

*Rejected: route classes (read/write/expensive) declared per route.* More
expressive and closer to what #447 eventually wants, but every route gains a
second mandatory declaration and the class boundaries are guesswork until there
is production traffic to calibrate against.

*Rejected: limiting only the expensive endpoints.* Smallest change, but it
provides no noisy-neighbour protection — which is #689's actual purpose — and
every future expensive endpoint must be remembered.

### D3 — The limiter runs before `authz`, after `authn`

```
authn.Middleware              verify signature + revocation
requestid.PrincipalMiddleware
ratelimit.Middleware          ← NEW
authz.Middleware              resolve permissions via OpenFGA
```

`authz.Middleware` makes an OpenFGA `ListObjects` call on every request — the
most expensive step in the chain and itself a shared resource. Limiting after it
would let a flood exhaust OpenFGA before the limiter refused anything. Limiting
before means a throttled request costs one map lookup.

It cannot run before `authn`, because the tenant comes from the verified token.
The consequence is a real gap: `authn` performs RSA signature verification per
request, so an **unauthenticated flood still pays that cost** and is not limited
here at all. That is edge/WAF territory, out of scope per #689, and recorded in
Limitations rather than left implicit.

### D4 — Two buckets per request, both must allow

- `tenant:<tenantID>` — protects other hospitals from a noisy one
- `subject:<subject>` — protects a hospital from one of its own users, or from a
  compromised credential looping

The denial names which bucket was exhausted. "You are rate limited" without
saying whether the cause is you or your whole hospital is a support ticket
rather than an answer.

### D5 — An arch-test-pinned exemption list

Declared by registered route pattern (`c.FullPath()`), with a reason per entry,
pinned by an arch test — the same shape as `NoTenantMembership` in #781.

| Route | Reason |
|---|---|
| `POST /v1/iam/me/sign-out` | A clinician on a shared ward terminal must always be able to end their session. |
| `POST /v1/iam/subjects/:subject/revoke` | Incident response. An attacker looping requests is precisely what would trip the limiter, at the moment an administrator most needs to cut the credential off. |

**Exempt from the limit, not from the counter.** Usage is still recorded and
logged, so an exempt route being hammered is visible rather than invisible.

*Rejected: no exemptions, set limits high enough.* Makes the availability of a
security control a function of a tuning number, and the failure surfaces during
an incident — the worst moment to learn the threshold was wrong.

*Rejected: exempt by permission rather than route.* Survives renames and travels
with authorization, but the mapping is coarse and it couples two systems that
are currently independent.

### D6 — Limits from env, with production defaults

Following `LOG_LEVEL`'s existing pattern. Unlike pagination's page-size
constants — code-only because tuning them widens a security window — a rate
limit is capacity, and capacity genuinely differs per environment.

Per-plan overrides (#689) are deferred: they need subscription tiers.

### D7 — The E2E suite is a load test, and the limiter must survive it

The pagination spec creates **55 visits in a tight loop as a single principal**,
and the whole suite runs at four workers in under 20 seconds. A 120/min
per-principal limit would throttle our own tests.

**Decision: keep the limiter enabled everywhere with production values, give the
E2E suite a generous per-run budget via env, and add a dedicated E2E that
deliberately trips the limiter and asserts the 429, `Retry-After` and recovery.**

The limiter is then exercised on the path developers actually run; the suite
does not fight it; and there is a test whose failure means the limiter stopped
working rather than that a threshold drifted.

*Rejected: disable in dev.* The limiter would never be exercised where
developers work, so its first real exercise would be production.

*Rejected: raise the limit until the tests pass.* Tuning a control to fit the
tests, which is backwards.

---

## Architecture

```
pkg/ratelimit
  Limiter interface { Allow(key string, now time.Time) Decision }
  Decision { Allowed bool; RetryAfter time.Duration; Limit, Remaining int; Reset time.Time }
  NewMemory(rules Rules) *Memory      ← ships now
  // redisLimiter later, when replica count is known (#7)
```

A token bucket per key, bounded by an LRU so a key space driven by tenant and
subject IDs cannot grow without limit — the same bound the revocation cache
needed in #781, for the same reason.

**`now time.Time` is a parameter, not `time.Now()` inside.** Rate limiters are
the classic case where tests either sleep or go flaky. Passing the clock makes
refill behaviour exactly assertable — 119 allowed, the 120th denied, advance
500ms, one allowed — with no timing dependence.

### The response

`respond` gains a 429 helper, so this status has exactly one shape like every
other:

```
429 Too Many Requests
{"error": "rate_limited", "message": "too many requests for this <tenant|principal>; retry in 12s"}

Retry-After: 12
RateLimit-Limit: 120
RateLimit-Remaining: 0
RateLimit-Reset: 12
```

`Retry-After` matters most: the mobile clients in the backlog are offline-first,
and a client that backs off rather than retrying immediately is the difference
between a limiter that sheds load and one that amplifies it.

---

## Error handling

An in-memory limiter has **no backing store and therefore cannot be
unavailable**, so the §3 capacity carve-out from PR #815 does not bite yet. It
becomes load-bearing when the Redis implementation lands: a limiter whose store
is unreachable must fail **open** with an alert, because denying every request
over quota accounting causes exactly the outage the limiter exists to prevent.
That reasoning is recorded here so the Redis implementation inherits it rather
than re-deriving it.

Every other failure is a client error and shaped as one: a request over its
budget is 429 with `Retry-After`, never a 500 and never a silent delay.

---

## Testing

Per `docs/standards/engineering-principles.md` §5, every assertion is proven able
to fail before it is trusted.

| ID | Test | Why it exists |
|---|---|---|
| T1 | A throttled request makes **no OpenFGA call** | Proves D3's placement. If the limiter drifts after `authz`, a flood exhausts OpenFGA before being refused, and nothing else would catch the move. |
| T2 | Token bucket refills exactly, with an injected clock | 119 allowed, 120th denied, advance 500ms, one allowed. No sleeps. |
| T3 | Tenant bucket exhausted → 429 even when the principal's is fine, and vice versa | Both buckets enforced, not just whichever is checked first. |
| T4 | The denial names which bucket was exhausted | "You" vs "your whole hospital". |
| T5 | `POST /v1/iam/me/tenant` trips at its tight budget, not the default | The tight rule is applied, not merely declared. |
| T6 | An exempt route is never throttled, at 100× the limit | Sign-out and revoke must work during the incident that trips the limiter. |
| T7 | An exempt route is still counted and logged | Exempt from the limit, not from visibility. |
| T8 | Key space stays bounded under many distinct tenants and subjects | An unbounded map keyed by subject is a memory leak. |
| T9 | `Retry-After` and `RateLimit-*` headers present and correct | Offline-first clients back off on these. |
| T10 | Arch test: the exemption list is exactly two routes | Stops the unsafe set growing silently. |
| T11 | Concurrent requests under `-race` | The bucket map is mutated on every request. |
| T12 | E2E: deliberately exceed, assert 429 + `Retry-After`, then recover | Exercises the limiter on the path developers actually run. |

---

## Observability

A denial logs at warn with `tenant_id`, `subject`, route and which bucket — the
fields an operator needs to answer "one user, one hospital, or everyone"
without adding instrumentation mid-incident.

An in-process counter mirrors `logging.RedactionCount()`'s existing pattern,
ready for #679's metrics sink. It is deliberately not wired to anything: #679
does not exist, and a fake sink would be worse than none.

---

## Limitations

- **The effective global limit is N × configured** until the Redis
  implementation lands. Correct for the connection pool, approximate for GIP
  quota (D1).
- **Unauthenticated floods are not limited.** `authn` runs first and does RSA
  verification per request. An IP-keyed limiter ahead of authentication is
  edge/WAF work, out of scope per #689.
- **No per-plan overrides.** Needs subscription tiers.
- **No concurrency caps, statement-timeout profiles, JetStream consumer limits,
  Redis memory accounting, or EmergencyConnect reservation.** All #447, all
  dependent on things that do not exist.
- **`SetMaxOpenConns(5)` is not changed here.** This bounds the request rate
  that reaches the pool; it does not partition the pool per tenant, which is a
  #447 item and a #774 Tier 2 item in its own right.
- **The exemption list is a documented convention guarded by CI**, which §4
  ranks below "impossible to express". Nothing prevents an entry being added
  without justification beyond review.
- **Limits are guesses.** 600/120/10 per minute, and the burst values, are
  chosen to sit comfortably above plausible clinical usage and well inside GIP
  quota, with no production traffic to calibrate against. They should be
  revisited against real data, and the first revision will probably be the
  burst values rather than the rates — burst is what a page load hits.
- **Health probes are deliberately unlimited**, being outside `/v1`. If a
  readiness probe ever moves under `/v1`, it must be added to the exemption
  list or a throttled probe will take a healthy replica out of service.

---

## Out of scope

- Global DDoS protection and edge/WAF — #689 says so explicitly.
- Billing on usage — Subscriptions.
- The rest of #447, as enumerated above.
- Changing the connection-pool ceiling or partitioning it per tenant.

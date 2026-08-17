# Per-Tenant Rate Limiting Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop one hospital, one user or one loop from exhausting a shared resource — Identity Platform quota, the connection pool, or OpenFGA — without ever blocking a clinician from signing out or an administrator from revoking a compromised credential.

**Architecture:** A token bucket in memory behind a `Limiter` interface, so the Redis implementation is a constructor swap when #7 gives a known replica count. Middleware sits between `authn` and `authz` — after the tenant is known, before the expensive OpenFGA call. Two buckets per request (tenant and principal), both must allow. An arch-test-pinned exemption list covers the security controls that must work during the incident that would trip a limiter.

**Tech Stack:** Go 1.26, Gin, `golang.org/x/time/rate` or a hand-rolled bucket, testcontainers, Playwright.

**Spec:** `docs/superpowers/specs/2026-08-14-rate-limiting-design.md`
**Issue:** #689. Branch: `feat/689-rate-limiting`.

## Global Constraints

- `docs/standards/engineering-principles.md` is binding. Especially §1 (no minimal solutions), §3 **including the capacity carve-out** — a control protecting capacity fails **open** with an alert, unlike a control protecting data — §4 (compile error > boot failure > CI failure > convention), §5 (prove every assertion can fail).
- **Every new assertion must be observed failing before it passes.**
- `make lint-go` clean; `cd backend && go test -count=1 -race ./...` green; `cd backend && ./scripts/coverage-gate.sh` green — **run it unpiped**; a pipe returns the pipe's exit status and has masked a real failure on this repo before.
- `pnpm turbo lint type-check test build` green.
- **Limits and bursts:**

  | Scope | Rate | Burst |
  |---|---|---|
  | default, per tenant | 600/min | 100 |
  | default, per principal | 120/min | 20 |
  | `POST /v1/iam/me/tenant`, per principal | 10/min | 3 |

- **`now time.Time` is a parameter to `Allow`, never `time.Now()` inside.** This is what makes refill exactly assertable without sleeps.
- Applied at the `/v1` group only. `/healthz` and `/readyz` stay unlimited — a throttled readiness probe takes a healthy replica out of service.
- Limits come from env with production defaults, following `LOG_LEVEL`'s pattern in `internal/config`.
- Migrations: none. Frontend: none, except the E2E in Task 6.

---

## File Structure

**New:**

| File | Responsibility |
|---|---|
| `backend/pkg/ratelimit/ratelimit.go` | `Limiter` interface, `Decision`, `Rule`, `Rules` |
| `backend/pkg/ratelimit/memory.go` | The in-memory token bucket + LRU bound |
| `backend/pkg/ratelimit/memory_test.go` | Refill with an injected clock, bounds, races |
| `backend/pkg/ratelimit/middleware.go` | Gin middleware: two buckets, exemptions, headers |
| `backend/pkg/ratelimit/middleware_test.go` | Both buckets, denial naming, exemptions, headers |
| `backend/internal/archtest/ratelimit_test.go` | The exemption allowlist arch test |
| `e2e/tests/ratelimit.spec.ts` | Deliberately trip the limiter, assert 429 and recovery |

**Modified:**

| File | Change |
|---|---|
| `backend/internal/platform/respond/respond.go` | Add `TooManyRequests` |
| `backend/internal/config/config.go` | Rate-limit settings from env |
| `backend/cmd/api/main.go` | Construct the limiter, insert the middleware |
| `docs/standards/backend.md` | The rule |
| `Makefile` / `dev/.env` | The E2E's generous budget |

---

## Task 1: the limiter

**Files:**
- Create: `backend/pkg/ratelimit/ratelimit.go`, `memory.go`, `memory_test.go`

**Interfaces:**
- Produces:
  - `type Decision struct { Allowed bool; RetryAfter time.Duration; Limit int; Remaining int; Reset time.Duration }`
  - `type Rule struct { Rate int; Burst int; Per time.Duration }` — `Rate` events per `Per`
  - `type Limiter interface { Allow(key string, r Rule, now time.Time) Decision }`
  - `func NewMemory(maxKeys int) *Memory`
  - `func (*Memory) Allow(key string, r Rule, now time.Time) Decision`
  - `func (*Memory) Len() int` — for the bound test
- Consumes: nothing.

**Why `Rule` is a parameter to `Allow` rather than baked into the limiter:** the same key space carries different budgets (a principal has a default rule on most routes and a tight rule on the mint path). Passing the rule keeps one bucket store and lets the caller decide the budget per request.

- [ ] **Step 1: Write the failing refill test**

Create `backend/pkg/ratelimit/memory_test.go`:

```go
package ratelimit_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/pkg/ratelimit"
)

var defaultRule = ratelimit.Rule{Rate: 120, Burst: 20, Per: time.Minute}

// TestBucketAllowsBurstThenRefillsAtRate pins both parameters of the
// bucket. A rate-only implementation passes half of this and throttles a
// clinical screen on load, because a dashboard fires several requests at
// once; a burst-only implementation passes the other half and never
// throttles anything after the first second.
//
// The clock is injected precisely so this can assert refill exactly
// rather than sleeping. A rate-limiter test that sleeps is a flaky test
// waiting to happen, and a flaky test in a capacity control gets
// disabled rather than fixed.
func TestBucketAllowsBurstThenRefillsAtRate(t *testing.T) {
	l := ratelimit.NewMemory(1000)
	now := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)

	// Burst is 20: the first 20 go through with no time passing at all.
	for i := range 20 {
		d := l.Allow("subject:alice", defaultRule, now)
		require.True(t, d.Allowed, "request %d of the burst must be allowed", i+1)
	}

	// The 21st has no token and no time has passed.
	d := l.Allow("subject:alice", defaultRule, now)
	require.False(t, d.Allowed, "burst is 20; the 21st request in the same instant must be refused")
	require.Positive(t, d.RetryAfter, "a refusal must tell the client when to come back")

	// 120/min is one token every 500ms. Advance exactly that.
	d = l.Allow("subject:alice", defaultRule, now.Add(500*time.Millisecond))
	require.True(t, d.Allowed, "at 120/min one token refills every 500ms")

	// And immediately after, empty again.
	d = l.Allow("subject:alice", defaultRule, now.Add(500*time.Millisecond))
	require.False(t, d.Allowed, "the refilled token was just consumed")
}

// TestBucketsAreIndependentPerKey is the whole point of per-tenant
// limiting: one hospital exhausting its budget must not refuse another's
// requests.
func TestBucketsAreIndependentPerKey(t *testing.T) {
	l := ratelimit.NewMemory(1000)
	now := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)

	for range 20 {
		require.True(t, l.Allow("tenant:a", defaultRule, now).Allowed)
	}
	require.False(t, l.Allow("tenant:a", defaultRule, now).Allowed)

	require.True(t, l.Allow("tenant:b", defaultRule, now).Allowed,
		"tenant b must be unaffected by tenant a exhausting its bucket")
}

// TestDecisionReportsRemainingAndReset covers what the client sees. The
// mobile apps in the backlog are offline-first and back off on these
// headers; wrong numbers are worse than none, because a client trusts them.
func TestDecisionReportsRemainingAndReset(t *testing.T) {
	l := ratelimit.NewMemory(1000)
	now := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)

	d := l.Allow("subject:alice", defaultRule, now)
	require.Equal(t, 120, d.Limit)
	require.Equal(t, 19, d.Remaining, "one of the 20 burst tokens was just consumed")

	for range 19 {
		l.Allow("subject:alice", defaultRule, now)
	}
	d = l.Allow("subject:alice", defaultRule, now)
	require.False(t, d.Allowed)
	require.Equal(t, 0, d.Remaining)
	require.Equal(t, 500*time.Millisecond, d.RetryAfter,
		"the next token is 500ms away at 120/min")
}

// TestKeySpaceIsBounded: keys are tenant and subject ids, so an
// unbounded map is a memory leak reachable by anyone who can obtain
// tokens for many subjects. Same bound, same reason, as the revocation
// cache in #781.
func TestKeySpaceIsBounded(t *testing.T) {
	const max = 100
	l := ratelimit.NewMemory(max)
	now := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)

	for i := range max * 5 {
		l.Allow(fmt.Sprintf("subject:%d", i), defaultRule, now)
	}
	require.LessOrEqual(t, l.Len(), max, "the bucket store must stay bounded")
}
```

- [ ] **Step 2: Run and confirm failure**

```bash
cd backend && go test ./pkg/ratelimit/ -run TestBucket -v
```

Expected: FAIL to compile — package does not exist.

- [ ] **Step 3: Implement `backend/pkg/ratelimit/ratelimit.go`**

```go
// Package ratelimit bounds how fast one tenant or one principal may
// consume shared resources.
//
// What it protects, concretely (#689): Identity Platform quota, which is
// project-wide, so one caller looping POST /v1/iam/me/tenant can break
// sign-in for every hospital; the connection pool, capped at 5 per
// process; and OpenFGA, which the authorization middleware calls on
// every request.
//
// It is a CAPACITY control, not a data control, and that decides its
// failure direction: per docs/standards/engineering-principles.md §3, a
// limiter that cannot decide must fail OPEN with an alert. Denying every
// request because quota accounting is confused causes exactly the outage
// the limiter exists to prevent. The in-memory implementation here has
// no backing store and so cannot be unavailable; the rule is stated for
// the Redis implementation that replaces it when replica count is known
// (#7).
package ratelimit

import "time"

// Rule is one budget: Rate events per Per, with Burst available at once.
//
// Burst is not optional and not a detail. A clinical screen loading
// fires several requests together — permissions, then a zone list, then
// a panel — so a burst smaller than a page load throttles ordinary
// navigation. A burst equal to the whole period's budget lets one client
// drain a tenant's allowance instantly.
type Rule struct {
	Rate  int
	Burst int
	Per   time.Duration
}

// Decision is the answer for one request, carrying everything the
// response headers need.
type Decision struct {
	Allowed    bool
	RetryAfter time.Duration
	Limit      int
	Remaining  int
	Reset      time.Duration
}

// Limiter decides whether one key may proceed under one rule.
//
// now is a parameter rather than time.Now() inside, so refill behaviour
// is exactly assertable without sleeping. A rate-limiter test that
// sleeps is flaky, and a flaky test in a capacity control gets disabled
// rather than fixed.
//
// One method, and an interface with a single implementation today, on
// purpose: the in-memory limiter's global accuracy depends on replica
// count (with N replicas the effective limit is N × configured). When #7
// lands and that number is known, a Redis implementation is a
// constructor swap rather than a rewrite. Per ADR-0005, build the
// boundary now and pay for the distributed version when a trigger
// justifies it.
type Limiter interface {
	Allow(key string, r Rule, now time.Time) Decision
}
```

- [ ] **Step 4: Implement `backend/pkg/ratelimit/memory.go`**

```go
package ratelimit

import (
	"container/list"
	"sync"
	"time"
)

// bucket is one key's token state. tokens is fractional so a rate that
// does not divide evenly into the period does not drift.
type bucket struct {
	tokens float64
	last   time.Time
	elem   *list.Element // position in the LRU
}

// Memory is an in-process token-bucket limiter, bounded by an LRU.
//
// The bound is not defensive: keys are tenant and subject identifiers,
// so an unbounded map grows with every distinct caller and is a memory
// leak reachable by anyone able to obtain tokens for many subjects. An
// evicted key simply starts with a full burst, which is the safe
// direction for a capacity control — it admits, it does not deny.
type Memory struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	order   *list.List // front = most recently used
	maxKeys int
}

func NewMemory(maxKeys int) *Memory {
	return &Memory{
		buckets: make(map[string]*bucket, maxKeys/4+1),
		order:   list.New(),
		maxKeys: maxKeys,
	}
}

func (m *Memory) Allow(key string, r Rule, now time.Time) Decision {
	m.mu.Lock()
	defer m.mu.Unlock()

	perToken := r.Per / time.Duration(r.Rate) // e.g. 500ms at 120/min

	b, ok := m.buckets[key]
	if !ok {
		b = &bucket{tokens: float64(r.Burst), last: now}
		b.elem = m.order.PushFront(key)
		m.buckets[key] = b
		m.evictLocked()
	} else {
		m.order.MoveToFront(b.elem)
		if elapsed := now.Sub(b.last); elapsed > 0 {
			b.tokens += float64(elapsed) / float64(perToken)
			if b.tokens > float64(r.Burst) {
				b.tokens = float64(r.Burst)
			}
			b.last = now
		}
	}

	if b.tokens < 1 {
		// How long until one whole token exists.
		missing := 1 - b.tokens
		retry := time.Duration(missing * float64(perToken))
		return Decision{
			Allowed: false, RetryAfter: retry,
			Limit: r.Rate, Remaining: 0, Reset: retry,
		}
	}
	b.tokens--
	return Decision{
		Allowed: true, Limit: r.Rate,
		Remaining: int(b.tokens),
		Reset:     time.Duration((float64(r.Burst) - b.tokens) * float64(perToken)),
	}
}

// Len reports how many keys are held. Exported for the bound test, which
// cannot otherwise observe eviction.
func (m *Memory) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.buckets)
}

// evictLocked drops the least recently used key while over the bound.
func (m *Memory) evictLocked() {
	for len(m.buckets) > m.maxKeys {
		oldest := m.order.Back()
		if oldest == nil {
			return
		}
		m.order.Remove(oldest)
		delete(m.buckets, oldest.Value.(string))
	}
}
```

- [ ] **Step 5: Run and confirm the tests pass**

```bash
cd backend && go test -race ./pkg/ratelimit/
```

Expected: PASS.

- [ ] **Step 6: Prove the burst assertion can fail**

Change `b = &bucket{tokens: float64(r.Burst), …}` to `tokens: 1`. Run:

```bash
cd backend && go test ./pkg/ratelimit/ -run TestBucketAllowsBurstThenRefills -v
```

Expected: FAIL at "request 2 of the burst must be allowed". Restore.

Then change the refill to ignore elapsed time (delete the `b.tokens += …` line) and confirm the same test fails at "at 120/min one token refills every 500ms". Restore.

Both halves of the bucket are now proven load-bearing.

- [ ] **Step 7: Prove the bound can fail**

Change `evictLocked` to return immediately. Confirm `TestKeySpaceIsBounded` fails with a length of 500. Restore.

- [ ] **Step 8: Add the concurrency test**

```go
func TestMemoryIsRaceFree(t *testing.T) {
	l := ratelimit.NewMemory(50)
	now := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)
	var wg sync.WaitGroup
	for i := range 200 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l.Allow(fmt.Sprintf("subject:%d", i%20), defaultRule, now.Add(time.Duration(i)*time.Millisecond))
		}()
	}
	wg.Wait()
	require.LessOrEqual(t, l.Len(), 50)
}
```

Run with `-race`, then remove `m.mu.Lock()` from `Allow` and confirm the race detector reports a data race. Restore.

- [ ] **Step 9: Commit**

```bash
cd backend && go vet ./... && cd .. && make lint-go
git add backend/pkg/ratelimit/
git commit -m "feat: add an in-memory token bucket with an injected clock and a bounded key space (#689)"
```

---

## Task 2: the 429 response helper

**Files:**
- Modify: `backend/internal/platform/respond/respond.go`, `respond_test.go`

**Interfaces:**
- Produces: `func TooManyRequests(c *gin.Context, message string, d ratelimit.Decision)`.

Wait — `respond` importing `ratelimit` would put a `pkg/` dependency into `internal/platform`, which is the direction the codebase already has (`pkg/authz` imports `internal/platform/respond`, an inversion the foundation audit flagged). **Do not deepen it.** Take primitives instead:

`func TooManyRequests(c *gin.Context, message string, retryAfter time.Duration, limit, remaining int)`

- [ ] **Step 1: Write the failing test**

```go
func TestTooManyRequestsCarriesTheBackoffHeaders(t *testing.T) {
	w := run(func(c *gin.Context) {
		respond.TooManyRequests(c, "too many requests for this principal; retry in 12s",
			12*time.Second, 120, 0)
	})

	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.JSONEq(t, `{"error":"rate_limited","message":"too many requests for this principal; retry in 12s"}`, w.Body.String())

	// Retry-After is seconds per RFC 7231, and it is the header the
	// offline-first mobile clients back off on. A client that retries
	// immediately turns a limiter into an amplifier.
	require.Equal(t, "12", w.Header().Get("Retry-After"))
	require.Equal(t, "120", w.Header().Get("RateLimit-Limit"))
	require.Equal(t, "0", w.Header().Get("RateLimit-Remaining"))
	require.Equal(t, "12", w.Header().Get("RateLimit-Reset"))
}

// TestTooManyRequestsRoundsSubSecondRetryUp: Retry-After has
// second granularity, so a 400ms wait must render as 1, not 0. A client
// told to retry after 0 seconds retries immediately, which is the
// amplification this header exists to prevent.
func TestTooManyRequestsRoundsSubSecondRetryUp(t *testing.T) {
	w := run(func(c *gin.Context) {
		respond.TooManyRequests(c, "slow down", 400*time.Millisecond, 120, 0)
	})
	require.Equal(t, "1", w.Header().Get("Retry-After"))
}
```

`run` already exists in `respond_test.go`.

- [ ] **Step 2: Run and confirm failure**

```bash
cd backend && go test ./internal/platform/respond/ -run TestTooManyRequests -v
```

Expected: FAIL to compile.

- [ ] **Step 3: Implement**

```go
// TooManyRequests refuses a request that exceeded its rate budget.
//
// Retry-After is seconds (RFC 7231) and is rounded UP: a sub-second wait
// must never render as "0", because a client told to retry after zero
// seconds retries immediately and turns a limiter into an amplifier.
// The offline-first mobile clients in the backlog back off on this
// header, so it is load-bearing rather than informational.
//
// Takes primitives rather than a ratelimit.Decision on purpose: this
// package is under internal/platform, and importing pkg/ratelimit here
// would deepen the pkg/ -> internal/ inversion the foundation audit
// flagged rather than leaving it where it is.
func TooManyRequests(c *gin.Context, message string, retryAfter time.Duration, limit, remaining int) {
	secs := int(math.Ceil(retryAfter.Seconds()))
	if secs < 1 {
		secs = 1
	}
	c.Header("Retry-After", strconv.Itoa(secs))
	c.Header("RateLimit-Limit", strconv.Itoa(limit))
	c.Header("RateLimit-Remaining", strconv.Itoa(remaining))
	c.Header("RateLimit-Reset", strconv.Itoa(secs))
	Error(c, http.StatusTooManyRequests, "rate_limited", message)
}
```

- [ ] **Step 4: Run, then prove the rounding can fail**

```bash
cd backend && go test ./internal/platform/respond/
```

Expected: PASS. Then change `math.Ceil` to `math.Floor` and confirm `TestTooManyRequestsRoundsSubSecondRetryUp` fails with `"0"`. Restore.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/platform/respond/
git commit -m "feat: add a 429 helper whose Retry-After never rounds down to zero (#689)"
```

---

## Task 3: the middleware

**Files:**
- Create: `backend/pkg/ratelimit/middleware.go`, `middleware_test.go`

**Interfaces:**
- Consumes: `Limiter`, `Rule`, `Decision` (Task 1); `respond.TooManyRequests` (Task 2); `authn.PrincipalFrom`.
- Produces:
  - `type Config struct { Tenant, Principal Rule; Tight map[string]Rule; Exempt map[string]string }`
  - `func Middleware(l Limiter, cfg Config) gin.HandlerFunc`

- [ ] **Step 1: Write the failing tests**

```go
// TestBothBucketsAreEnforced: a tenant within budget whose principal is
// exhausted must still be refused, and vice versa. Checking only
// whichever comes first would leave one of the two protections
// decorative.
func TestBothBucketsAreEnforced(t *testing.T) {
	t.Run("principal exhausted, tenant fine", func(t *testing.T) {
		r := harness(t, Config{
			Tenant:    Rule{Rate: 600, Burst: 100, Per: time.Minute},
			Principal: Rule{Rate: 120, Burst: 2, Per: time.Minute},
		})
		require.Equal(t, http.StatusOK, do(r, "/v1/things", "alice", "tenant-a").Code)
		require.Equal(t, http.StatusOK, do(r, "/v1/things", "alice", "tenant-a").Code)
		w := do(r, "/v1/things", "alice", "tenant-a")
		require.Equal(t, http.StatusTooManyRequests, w.Code)
		require.Contains(t, w.Body.String(), "principal",
			"the denial must name which bucket was exhausted")
	})

	t.Run("tenant exhausted, principal fine", func(t *testing.T) {
		r := harness(t, Config{
			Tenant:    Rule{Rate: 600, Burst: 2, Per: time.Minute},
			Principal: Rule{Rate: 120, Burst: 100, Per: time.Minute},
		})
		// Two different principals in the same tenant drain the tenant bucket.
		require.Equal(t, http.StatusOK, do(r, "/v1/things", "alice", "tenant-a").Code)
		require.Equal(t, http.StatusOK, do(r, "/v1/things", "bob", "tenant-a").Code)
		w := do(r, "/v1/things", "carol", "tenant-a")
		require.Equal(t, http.StatusTooManyRequests, w.Code)
		require.Contains(t, w.Body.String(), "tenant",
			"a tenant-level denial must say so — 'you' and 'your whole hospital' are different answers")
	})
}

// TestTightRuleAppliesToItsRoute proves the tight budget is applied and
// not merely declared. Without this, the map could be ignored entirely
// and every other test would still pass.
func TestTightRuleAppliesToItsRoute(t *testing.T) {
	r := harness(t, Config{
		Tenant:    Rule{Rate: 600, Burst: 100, Per: time.Minute},
		Principal: Rule{Rate: 120, Burst: 100, Per: time.Minute},
		Tight:     map[string]Rule{"POST /v1/mint": {Rate: 10, Burst: 1, Per: time.Minute}},
	})
	require.Equal(t, http.StatusOK, doPost(r, "/v1/mint", "alice", "tenant-a").Code)
	require.Equal(t, http.StatusTooManyRequests, doPost(r, "/v1/mint", "alice", "tenant-a").Code,
		"the tight rule's burst of 1 must govern, not the default burst of 100")

	// The default route is untouched by the tight rule.
	require.Equal(t, http.StatusOK, do(r, "/v1/things", "alice", "tenant-a").Code)
}

// TestExemptRouteIsNeverThrottled: sign-out and admin revoke must work
// during the incident that trips the limiter. An attacker looping
// requests is exactly what exhausts a bucket, at the moment an
// administrator most needs to cut the credential off.
func TestExemptRouteIsNeverThrottled(t *testing.T) {
	r := harness(t, Config{
		Tenant:    Rule{Rate: 600, Burst: 1, Per: time.Minute},
		Principal: Rule{Rate: 120, Burst: 1, Per: time.Minute},
		Exempt:    map[string]string{"POST /v1/escape": "test exemption"},
	})
	for i := range 100 {
		require.Equal(t, http.StatusOK, doPost(r, "/v1/escape", "alice", "tenant-a").Code,
			"exempt request %d must not be throttled even at 100x the burst", i+1)
	}
}

// TestExemptRouteIsStillRecorded: exempt from the LIMIT, not from
// visibility. An exempt route is one an attacker may hammer without
// being refused, so the one thing that must not also be true is that
// nobody can see it happening. Without this, "exempt" quietly means
// "invisible" and the exemption list becomes a blind spot rather than a
// considered trade.
func TestExemptRouteIsStillRecorded(t *testing.T) {
	var logged int
	r := harnessCapturingLogs(t, Config{
		Tenant:    Rule{Rate: 600, Burst: 1, Per: time.Minute},
		Principal: Rule{Rate: 120, Burst: 1, Per: time.Minute},
		Exempt:    map[string]string{"POST /v1/escape": "test exemption"},
	}, &logged)

	for range 5 {
		require.Equal(t, http.StatusOK, doPost(r, "/v1/escape", "alice", "tenant-a").Code)
	}
	require.Equal(t, 5, logged,
		"every exempt request must be recorded; an exemption nobody can observe is a blind spot")
}

// TestThrottledRequestNeverReachesTheHandler is the placement proof in
// miniature: a refused request must not do the work it was refused for.
// Task 4 proves the same property against the real OpenFGA middleware.
func TestThrottledRequestNeverReachesTheHandler(t *testing.T) {
	reached := 0
	r := harnessCounting(t, Config{
		Tenant:    Rule{Rate: 600, Burst: 100, Per: time.Minute},
		Principal: Rule{Rate: 120, Burst: 1, Per: time.Minute},
	}, &reached)

	require.Equal(t, http.StatusOK, do(r, "/v1/things", "alice", "tenant-a").Code)
	require.Equal(t, http.StatusTooManyRequests, do(r, "/v1/things", "alice", "tenant-a").Code)
	require.Equal(t, 1, reached, "the throttled request must not have run the handler")
}

// TestAllowedRequestCarriesRemainingHeader: clients pace themselves on
// these on the happy path, not only when refused.
func TestAllowedRequestCarriesRemainingHeader(t *testing.T) {
	r := harness(t, Config{
		Tenant:    Rule{Rate: 600, Burst: 100, Per: time.Minute},
		Principal: Rule{Rate: 120, Burst: 20, Per: time.Minute},
	})
	w := do(r, "/v1/things", "alice", "tenant-a")
	require.Equal(t, "120", w.Header().Get("RateLimit-Limit"))
	require.Equal(t, "19", w.Header().Get("RateLimit-Remaining"))
}
```

Write `harness`, `harnessCounting`, `do` and `doPost` in the test file: build a `gin` engine with a middleware that sets `authn.Principal{Subject: subject, TenantID: tenant}` on the context, then `Middleware(NewMemory(1000), cfg)`, then routes `/v1/things` (GET), `/v1/mint` (POST) and `/v1/escape` (POST) returning 200.

- [ ] **Step 2: Run and confirm failure**

```bash
cd backend && go test ./pkg/ratelimit/ -run 'TestBoth|TestTight|TestExempt|TestThrottled|TestAllowed' -v
```

Expected: FAIL to compile — `Middleware` and `Config` undefined.

- [ ] **Step 3: Implement `backend/pkg/ratelimit/middleware.go`**

```go
package ratelimit

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tesserix/hms/internal/platform/respond"
	"github.com/tesserix/hms/pkg/authn"
)

// Config is the whole limiting policy.
//
// Tight and Exempt are keyed by "METHOD /path" using gin's REGISTERED
// route pattern (c.FullPath()), not the request URI: the pattern is
// stable and finite, where a URI carries path parameters and would make
// every /iam/subjects/<uid>/revoke a distinct key.
type Config struct {
	Tenant    Rule
	Principal Rule
	// Tight overrides Principal for specific routes that consume a
	// shared external resource.
	Tight map[string]Rule
	// Exempt maps a route to the reason it must never be throttled.
	// The reason is stored, not just the key, so the arch test can
	// require one and a reader can see why without archaeology.
	Exempt map[string]string
}

// Middleware refuses requests over budget.
//
// Placement is load-bearing and is asserted by a test rather than left
// to convention: it must run AFTER authn (the tenant comes from the
// verified token) and BEFORE authz (which calls OpenFGA on every
// request — the most expensive step in the chain and itself a shared
// resource). Limiting after authz would let a flood exhaust OpenFGA
// before anything was refused.
//
// Two buckets, both must allow: the tenant bucket protects other
// hospitals from a noisy one, the principal bucket protects a hospital
// from one of its own users or from a compromised credential looping.
func Middleware(l Limiter, cfg Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := authn.PrincipalFrom(c)
		if !ok {
			// No principal means authn did not run or did not admit the
			// caller. That is not this middleware's decision to make.
			respond.Unauthenticated(c, "missing principal")
			return
		}

		route := c.Request.Method + " " + c.FullPath()
		now := time.Now()

		if reason, exempt := cfg.Exempt[route]; exempt {
			// Exempt from the LIMIT, not from the record: an exempt
			// route being hammered must be visible rather than
			// invisible, or the exemption becomes a blind spot.
			slog.DebugContext(c.Request.Context(), "rate limit exempt route",
				"route", route, "reason", reason, "tenant_id", p.TenantID, "subject", p.Subject)
			c.Next()
			return
		}

		if d := l.Allow("tenant:"+p.TenantID, cfg.Tenant, now); !d.Allowed {
			deny(c, "tenant", p, route, d)
			return
		}

		rule := cfg.Principal
		if tight, ok := cfg.Tight[route]; ok {
			rule = tight
		}
		d := l.Allow("subject:"+p.Subject, rule, now)
		if !d.Allowed {
			deny(c, "principal", p, route, d)
			return
		}

		c.Header("RateLimit-Limit", itoa(d.Limit))
		c.Header("RateLimit-Remaining", itoa(d.Remaining))
		c.Next()
	}
}

// deny refuses and says which bucket ran out. "You are rate limited"
// without naming the bucket is a support ticket rather than an answer:
// the caller cannot tell whether they are the problem or their whole
// hospital is.
func deny(c *gin.Context, bucket string, p authn.Principal, route string, d Decision) {
	slog.WarnContext(c.Request.Context(), "rate limited",
		"bucket", bucket, "route", route,
		"tenant_id", p.TenantID, "subject", p.Subject,
		"retry_after_ms", d.RetryAfter.Milliseconds())
	denials.Add(1)
	respond.TooManyRequests(c,
		"too many requests for this "+bucket+"; retry in "+d.RetryAfter.Round(time.Second).String(),
		d.RetryAfter, d.Limit, d.Remaining)
}
```

Add `itoa` (a `strconv.Itoa` wrapper or use `strconv` directly) and a package-level `denials atomic.Uint64` with an exported `DenialCount() uint64`, mirroring `logging.RedactionCount()`. Wire it to nothing — #679 does not exist, and a fake sink is worse than none.

- [ ] **Step 4: Run and confirm the tests pass**

```bash
cd backend && go test -race ./pkg/ratelimit/
```

- [ ] **Step 5: Prove three assertions can fail**

1. Delete the tenant-bucket check. `TestBothBucketsAreEnforced/tenant exhausted` must fail. Restore.
2. Delete the `cfg.Tight` lookup so the default rule always applies. `TestTightRuleAppliesToItsRoute` must fail. Restore.
3. Delete the `cfg.Exempt` check. `TestExemptRouteIsNeverThrottled` must fail on the second request. Restore.

Report each. Assertion 3 is the one whose real-world failure is an administrator unable to revoke a credential mid-incident.

- [ ] **Step 6: Commit**

```bash
cd backend && go test -race ./pkg/ratelimit/... && cd .. && make lint-go
git add backend/pkg/ratelimit/
git commit -m "feat: limit per tenant and per principal, exempting the controls that must work during an incident (#689)"
```

---

## Task 4: wire it, and prove the placement

**Files:**
- Modify: `backend/internal/config/config.go`, `backend/cmd/api/main.go`
- Create: `backend/internal/archtest/ratelimit_test.go`

**Interfaces:**
- Consumes: `ratelimit.Middleware`, `ratelimit.Config`, `ratelimit.NewMemory`.
- Produces: `config.Config` gains `RateLimitTenantPerMin`, `RateLimitPrincipalPerMin`, `RateLimitMintPerMin int`.

- [ ] **Step 1: Add the config fields**

In `backend/internal/config/config.go`, following the existing `getenv` pattern:

```go
	// Rate limits are env-configurable, unlike the pagination page-size
	// constants: a page size bounds a query, but a rate limit bounds
	// capacity, and capacity genuinely differs between a laptop running
	// the e2e suite and a hospital in production.
	RateLimitTenantPerMin:    getenvInt("RATE_LIMIT_TENANT_PER_MIN", 600),
	RateLimitPrincipalPerMin: getenvInt("RATE_LIMIT_PRINCIPAL_PER_MIN", 120),
	RateLimitMintPerMin:      getenvInt("RATE_LIMIT_MINT_PER_MIN", 10),
```

Add `getenvInt(k string, def int) int` beside `getenv`, returning `def` when unset **or unparseable** — a mistyped limit must not stop a hospital's API from booting, the same call `LOG_LEVEL` already makes. Log a warning when it falls back, so the mistype is visible.

- [ ] **Step 2: Wire the middleware in `cmd/api/main.go`**

Between `requestid.PrincipalMiddleware()` and `authz.Middleware(fga)`:

```go
	limiter := ratelimit.NewMemory(10_000)
	limits := ratelimit.Config{
		Tenant:    ratelimit.Rule{Rate: cfg.RateLimitTenantPerMin, Burst: cfg.RateLimitTenantPerMin / 6, Per: time.Minute},
		Principal: ratelimit.Rule{Rate: cfg.RateLimitPrincipalPerMin, Burst: cfg.RateLimitPrincipalPerMin / 6, Per: time.Minute},
		Tight: map[string]ratelimit.Rule{
			// Mints a GIP custom token per call. Identity Platform quota
			// is project-wide, so exhausting it breaks sign-in for every
			// hospital, not just this caller's.
			"POST /v1/iam/me/tenant": {Rate: cfg.RateLimitMintPerMin, Burst: 3, Per: time.Minute},
		},
		Exempt: map[string]string{
			"POST /v1/iam/me/sign-out":              "a clinician on a shared ward terminal must always be able to end their session",
			"POST /v1/iam/subjects/:subject/revoke": "incident response; an attacker looping requests is exactly what would trip the limiter",
		},
	}

	api := platform.NewRouter(srv.Engine.Group("/v1",
		authn.Middleware(verifier, revocationChecker),
		requestid.PrincipalMiddleware(),
		ratelimit.Middleware(limiter, limits),
		authz.Middleware(fga),
	), fga)
```

Burst is `rate/6` — ten seconds' worth — matching the spec's reasoning that a burst smaller than a page load throttles ordinary navigation.

- [ ] **Step 3: Write the failing placement test (spec T1)**

This is the assertion the design rests on. Create `backend/internal/archtest/ratelimit_test.go`:

```go
// TestThrottledRequestMakesNoOpenFGACall proves the middleware ORDER,
// which no unit test can: authz.Middleware calls OpenFGA on every
// request, and it is the most expensive step in the chain. If the
// limiter ever moves after it, a flood exhausts OpenFGA before anything
// is refused — the limiter would still return 429 and every other test
// would still pass.
func TestThrottledRequestMakesNoOpenFGACall(t *testing.T) {
	var resolves atomic.Int64
	counting := countingResolver{inner: allowAllResolver{}, n: &resolves}

	r := chainHarness(t, counting, ratelimit.Config{
		Tenant:    ratelimit.Rule{Rate: 600, Burst: 100, Per: time.Minute},
		Principal: ratelimit.Rule{Rate: 120, Burst: 1, Per: time.Minute},
	})

	require.Equal(t, http.StatusOK, doChain(r, "alice", testTenant).Code)
	require.Equal(t, int64(1), resolves.Load())

	require.Equal(t, http.StatusTooManyRequests, doChain(r, "alice", testTenant).Code)
	require.Equal(t, int64(1), resolves.Load(),
		"a throttled request must not reach authz: it would exhaust OpenFGA before the limiter refused anything")
}
```

`chainHarness` builds the **same chain as `cmd/api/main.go`** — a static verifier, `requestid.PrincipalMiddleware()`, `ratelimit.Middleware`, `authz.Middleware(counting)` — and registers one route. `countingResolver` wraps an `authz.Resolver` and counts `Resolve` calls.

- [ ] **Step 4: Run, then prove it can fail**

```bash
cd backend && go test ./internal/archtest/ -run TestThrottledRequestMakesNoOpenFGACall -v
```

Expected: PASS. Then swap the order in `chainHarness` so `authz.Middleware` comes before `ratelimit.Middleware`, and confirm the test fails with `resolves == 2`. Restore.

Without this mutation the test proves nothing about ordering — it would pass with the middlewares in either order if the limiter simply happened to run.

- [ ] **Step 5: Write the exemption arch test (spec T10)**

```go
// rateLimitExemptAllowlist is every route permitted to bypass rate
// limiting, with the reason. Adding an entry is a decision a reviewer
// sees: an exempt route is one an attacker may hammer without being
// refused, so the set must stay small and justified.
var rateLimitExemptAllowlist = map[string]string{
	"POST /v1/iam/me/sign-out":              "a clinician on a shared ward terminal must always be able to end their session",
	"POST /v1/iam/subjects/:subject/revoke": "incident response; an attacker looping requests is exactly what would trip the limiter",
}

func TestRateLimitExemptionsAreAllowlisted(t *testing.T) {
	// config.Load() reads env with production defaults; only the Exempt
	// map is under test here, and it does not depend on the limits.
	got := bootstrap.RateLimitConfig(config.Load()).Exempt
	require.Equal(t, rateLimitExemptAllowlist, got,
		"a route exempt from rate limiting must be added to rateLimitExemptAllowlist with a reason")
}
```

Note the argument: `RateLimitConfig` takes a `config.Config` (Step 5's signature below), so the test passes one rather than calling a no-arg function.

This requires the limit config to be reachable from a test rather than built inline in `main.go`. Move its construction to `internal/bootstrap` as `func RateLimitConfig(cfg config.Config) ratelimit.Config`, and have `main.go` call it — the same shape as `bootstrap.NewRegistry`. **Do this rather than duplicating the map in the test**, which would assert only that someone copied it correctly.

- [ ] **Step 6: Prove it can fail**

Add a third entry to the config's `Exempt` map without adding it to the allowlist. Confirm the test fails naming it. Restore.

- [ ] **Step 7: Full gates and commit**

```bash
cd backend && go build ./... && go vet ./... && go test -count=1 -race ./...
cd backend && ./scripts/coverage-gate.sh          # unpiped
cd /Users/Mahesh.Sangawar/personal/tesserix-new/hms && make lint-go
git add backend/
git commit -m "feat: limit before the OpenFGA call, with exemptions pinned by an arch test (#689)"
```

---

## Task 5: the standards entry

**Files:**
- Modify: `docs/standards/backend.md`

- [ ] **Step 1: Document the rule**

Add to the routes section:

```markdown
**Every `/v1` route is rate limited**, per tenant and per principal, by
`ratelimit.Middleware` in the `/v1` chain — placed after `authn` (the tenant
comes from the verified token) and **before `authz`** (which calls OpenFGA on
every request; limiting after it would let a flood exhaust OpenFGA before
anything was refused). `TestThrottledRequestMakesNoOpenFGACall` pins that order.

Routes consuming a shared **external** resource get a tighter budget in
`RateLimitConfig`'s `Tight` map — today only `POST /v1/iam/me/tenant`, which
mints a GIP custom token per call against project-wide quota.

Routes that must never be throttled go in `Exempt` **with a reason**, pinned by
`TestRateLimitExemptionsAreAllowlisted`. The bar is high: an exempt route is one
an attacker may hammer without being refused. The two entries today are
sign-out and admin revoke — security controls whose whole purpose is to work
during the incident that would trip a limiter.

Rate limiting is a **capacity** control, so per
`docs/standards/engineering-principles.md` §3 it fails **open**, unlike
authorization or tenant scoping. The in-memory limiter cannot be unavailable;
when a Redis-backed one replaces it (#7), a limiter that cannot reach its store
must admit the request and alert, never deny.
```

- [ ] **Step 2: Commit**

```bash
git add docs/standards/backend.md
git commit -m "docs: record the rate limiting contract and its fail-open direction (#689)"
```

---

## Task 6: the E2E, and not fighting our own suite

**Files:**
- Create: `e2e/tests/ratelimit.spec.ts`
- Modify: `Makefile` or `dev/.env` (whichever holds the dev environment for `make up`)

**The problem this task exists to solve:** the pagination spec creates 55 visits in a tight loop as one principal, and the whole suite runs at four workers in under 20 seconds. A 120/min per-principal limit throttles our own tests.

**The decision (spec D7):** keep the limiter enabled everywhere with production values, give the dev/E2E environment a generous budget via env, and add a dedicated spec that deliberately trips it. The limiter is then exercised where developers work, the suite does not fight it, and a failure means the limiter broke rather than that a threshold drifted.

- [ ] **Step 1: Give the dev stack a generous budget**

Wherever `make up` sets the API environment, add:

```
RATE_LIMIT_TENANT_PER_MIN=100000
RATE_LIMIT_PRINCIPAL_PER_MIN=100000
RATE_LIMIT_MINT_PER_MIN=1000
```

with a comment saying why: the e2e suite is a load test, these values keep it from fighting the limiter, and `ratelimit.spec.ts` proves the limiter still works by setting its own much lower budget for one route.

**If the limiter cannot be configured per-route at runtime, the E2E must instead assert against the mint route's tight budget**, which stays low enough (1000/min) to be reachable in a loop. Decide which during implementation and say which you chose.

- [ ] **Step 2: Write the E2E**

```ts
import { expect, test } from "@playwright/test";
import { login } from "./support/login";

// #689: the limiter must actually refuse, and the client must be told
// when to come back. This spec exists because a rate limiter that is
// configured but never exercised is indistinguishable from one that is
// broken — and the dev stack deliberately runs generous limits so the
// rest of the suite is not throttled, which would otherwise mean the
// limiter is never exercised at all before production.
test("exceeding the budget is refused with a Retry-After, and recovers", async ({ page }) => {
  await login(page);

  const result = await page.evaluate(async () => {
    // The tenant-switch route mints a GIP custom token per call and
    // carries the tightest budget on the platform.
    const attempt = () =>
      fetch("/api/v1/iam/me/tenant", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ tenant_id: "11111111-1111-1111-1111-111111111111" }),
      });

    for (let i = 0; i < 2000; i++) {
      const res = await attempt();
      if (res.status === 429) {
        return { status: 429, retryAfter: res.headers.get("Retry-After"), attempts: i + 1 };
      }
    }
    return { status: 0, retryAfter: null, attempts: 2000 };
  });

  expect(result.status, `never refused after ${result.attempts} attempts`).toBe(429);
  expect(result.retryAfter, "a refusal must tell the client when to retry").toBeTruthy();
  expect(Number(result.retryAfter)).toBeGreaterThan(0);
});
```

This spec belongs in the **`bulk` Playwright project**, sequenced after the others like the pagination spec, so its request flood cannot throttle a concurrently-running spec sharing the tenant bucket.

- [ ] **Step 3: Run the full stack and the suite**

```bash
export HELIVANTA_PG_PORT=15432 HELIVANTA_NATS_PORT=14222 HELIVANTA_NATS_MONITOR_PORT=18222 \
  HELIVANTA_REDIS_PORT=16379 HELIVANTA_OPENFGA_PORT=18090 HELIVANTA_GIP_PORT=19099 \
  HELIVANTA_API_PORT=18080 NODE_AUTH_TOKEN=$(gh auth token)
make up && ./scripts/verify-local.sh
pnpm --filter e2e exec playwright test --reporter=list
```

From the repo root. `verify-local.sh` must pass first. `pnpm turbo build` overwrites the dev servers' `.next` output, so build before starting the stack, not after. Run the suite **twice** — one green run does not disprove a race.

- [ ] **Step 4: Prove the E2E can fail**

Set `RATE_LIMIT_MINT_PER_MIN=1000000`, restart the API, and confirm `ratelimit.spec.ts` fails with "never refused after 2000 attempts". Restore.

- [ ] **Step 5: Commit**

```bash
git add e2e/ Makefile dev/
git commit -m "test: prove the limiter refuses and says when to retry, without the suite fighting it (#689)"
```

---

## Task 7: verification and PR

- [ ] **Step 1: Full gates**

```bash
cd backend && go build ./... && go vet ./... && go test -count=1 -race ./...
cd backend && ./scripts/coverage-gate.sh          # unpiped — a pipe masks the exit status
cd /Users/Mahesh.Sangawar/personal/tesserix-new/hms && make lint-go
pnpm turbo lint type-check test build
pnpm --filter e2e exec playwright test --reporter=list    # twice
```

- [ ] **Step 2: Confirm the two real exposures are actually bounded**

With the stack running and production-ish limits (`RATE_LIMIT_MINT_PER_MIN=10`), loop `POST /v1/iam/me/tenant` by hand and confirm it refuses at roughly the tenth call within a minute, with `Retry-After`. This is the exposure the issue names first; assert it end to end rather than trusting the unit tests.

- [ ] **Step 3: Update the spec status and open the PR**

Change the spec header from `approved` to `implemented`, and correct anything the implementation decided differently.

PR body must carry: link to #689; the two verified exposures (`me.go:179` minting per call against project-wide GIP quota, `db.go:55`'s pool of 5); why in-process behind an interface rather than Redis, citing ADR-0004 and ADR-0005 and stating the **N × configured** cost plainly; the placement argument and the test that pins it; the exemption list and why those two routes; the §3 capacity carve-out and that this fails open; the E2E-as-load-test problem and how D7 resolves it; the plan's limitations; and which assertions were observed failing.

Close with `Closes #689`. **Do not merge.**

---

## Known limitations (carry into the PR body)

- **Effective global limit is N × configured** until a Redis implementation lands. Correct for the connection pool (per-process), approximate for GIP quota.
- **Unauthenticated floods are not limited** — `authn` runs first and does RSA verification per request. Edge/WAF work, out of scope per #689.
- **No per-plan overrides** — needs subscription tiers.
- **No concurrency caps, statement timeouts, JetStream quotas or EmergencyConnect reservation** — all #447.
- **`SetMaxOpenConns(5)` is unchanged.** This bounds the rate reaching the pool; it does not partition it per tenant.
- **The exemption list is CI-guarded convention**, which §4 ranks below "impossible to express".
- **The limits are guesses.** No production traffic exists to calibrate against; burst values will likely need revising before the rates, because burst is what a page load hits.
- **Health probes are unlimited**, being outside `/v1`. If one moves under `/v1` it must be exempted or a throttled probe will take a healthy replica out of service.

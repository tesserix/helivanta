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

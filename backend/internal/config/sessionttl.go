package config

import (
	"fmt"
	"time"
)

// DefaultSessionTTL is the session lifetime a process boots with when
// SESSION_TTL is unset or unparseable — see Config.SessionTTL's doc
// comment for why 15 minutes. Exported so tests assert against the
// documented value rather than restating it.
const DefaultSessionTTL = 15 * time.Minute

// MinSessionTTL is the shortest SESSION_TTL RequireSessionTTL accepts.
//
// It is DERIVED, not chosen: it is renewalFraction × renewAtFloor in
// internal/modules/iam/renew.go (3 × 30s), the smallest TTL at which
// renewAtFor still answers its designed TTL/3 rather than its floor.
// Below it the floor engages and the margin the renewal schedule was
// designed around shrinks; at or below 30s the first renewal is
// scheduled at or after the cookie's own exp, so every session dies
// before it is ever renewed (#921).
//
// iam cannot be imported from here, so the coupling is enforced from
// the other side: renew_test.go carries a compile-time assertion that
// MinSessionTTL/renewalFraction >= renewAtFloor. Raising either iam
// constant without raising this one fails to compile.
const MinSessionTTL = 90 * time.Second

// ErrInvalidSessionTTL is returned by RequireSessionTTL when SESSION_TTL
// resolved to a duration below MinSessionTTL (non-positive included).
var ErrInvalidSessionTTL = fmt.Errorf("config: SESSION_TTL must be at least %s", MinSessionTTL)

// RequireSessionTTL returns the configured session lifetime, or refuses
// with ErrInvalidSessionTTL when it is below MinSessionTTL.
//
// Same shape and reasoning as RequireIdleTimeout (idletimeout.go). A
// too-small SESSION_TTL — "5s", "20s", "0" — parses cleanly, so
// getenvDuration's mistyped-value fallback never sees it. A positive one
// then boots cleanly and signs out every clinician in the estate: the
// renewal schedule cannot fire before the cookie expires. A non-positive
// one was already refused, but only by session.NewSigner, after the
// databases, NATS and Zitadel had all been contacted, and with an error
// naming the signing key rather than SESSION_TTL.
//
// Fails CLOSED. A session lifetime is the upper bound on how long a
// user deactivated in Zitadel keeps working, which makes it an access
// control, not merely a capacity one (engineering-principles.md §3);
// getenvDuration's fail-open fallback for an UNPARSEABLE value is
// deliberately unchanged, because that path substitutes the documented
// default rather than an operator's explicit wrong number.
//
// Deliberately NOT clamped to MinSessionTTL: silently substituting a
// different lifetime for the one an operator declared would hide the
// mistake and leave the deployed policy differing from the declared
// one.
func (c Config) RequireSessionTTL() (time.Duration, error) {
	if c.SessionTTL < MinSessionTTL {
		return 0, fmt.Errorf(
			"%w, got %s: refusing to boot — the session renewal schedule fires at "+
				"SESSION_TTL/3 with a 30s floor, so a shorter lifetime expires sessions "+
				"before they can be renewed and signs out every clinician; unset "+
				"SESSION_TTL to take the %s default, or set it to a Go duration of at "+
				"least %s",
			ErrInvalidSessionTTL, c.SessionTTL.String(), DefaultSessionTTL.String(), MinSessionTTL.String(),
		)
	}
	return c.SessionTTL, nil
}

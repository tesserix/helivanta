package config

import (
	"errors"
	"fmt"
	"time"
)

// DefaultIdleTimeout is the idle window a process boots with when
// IDLE_TIMEOUT is unset — spec D1's clinical judgement, not a technical
// one (see Config.IdleTimeout's doc comment). It is exported so a test
// can assert against the documented value rather than re-deriving it,
// and so the default and its documentation cannot drift apart silently.
const DefaultIdleTimeout = 15 * time.Minute

// ErrInvalidIdleTimeout is returned by RequireIdleTimeout when
// IDLE_TIMEOUT resolved to a non-positive duration. See the function's
// doc comment for why that refuses to boot rather than running.
var ErrInvalidIdleTimeout = errors.New("config: IDLE_TIMEOUT must be positive")

// RequireIdleTimeout returns the configured idle window, or refuses with
// ErrInvalidIdleTimeout when it is not positive.
//
// This mirrors RequireZitadelLoginClientToken's shape and reasoning
// (zitadelloginclient.go), applied to a value rather than a secret. A
// non-positive IDLE_TIMEOUT — "0", "-5m", "0s" — parses cleanly, so
// getenvDuration's mistyped-value fallback never sees it, and it then
// fails CLOSED at the worst possible granularity: every session is
// minted already past its idle deadline, so authn.Middleware refuses
// EVERY request from EVERY clinician with session_idle, immediately and
// permanently. That is a total API outage produced by one character in a
// deploy manifest.
//
// Failing closed is the right direction for this control (#848 is an
// access-control property, not a capacity one — engineering-principles.md
// §3), which is exactly why the misconfiguration must not be reachable:
// there is no partially-degraded mode to fall back to, so the only
// remaining question is WHERE the failure surfaces. A boot refusal puts
// it at the deploy, loud, in front of the operator who just changed the
// value; without it, the same defect surfaces as a hospital-wide
// sign-out discovered by the first clinician who cannot open a chart.
// engineering-principles.md ranks boot failure above documented
// convention for precisely this reason, and this replaces what was
// previously only a warning in Config.IdleTimeout's doc comment.
//
// Deliberately NOT clamped to the default: silently substituting 15
// minutes for an operator's explicit "0" would hide the mistake and
// leave the deployed policy differing from the declared one, which is
// how a session-lifetime setting comes to mean nothing.
func (c Config) RequireIdleTimeout() (time.Duration, error) {
	if c.IdleTimeout <= 0 {
		return 0, fmt.Errorf(
			"%w, got %s: refusing to boot — a non-positive idle window mints every "+
				"session already past its idle deadline, so authn.Middleware would "+
				"refuse every request from every clinician (session_idle) rather than "+
				"degrading one feature; unset IDLE_TIMEOUT to take the %s default, or "+
				"set it to a positive Go duration (e.g. 15m)",
			ErrInvalidIdleTimeout, c.IdleTimeout.String(), DefaultIdleTimeout.String(),
		)
	}
	return c.IdleTimeout, nil
}

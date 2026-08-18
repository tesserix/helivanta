package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// ErrTrustedProxyCIDRsNotSet is returned by RequireTrustedProxyCIDRs when
// TRUSTED_PROXY_CIDRS is unset or empty outside HELIVANTA_ENV=dev. See that
// function's doc comment for why an unset value cannot simply fall back to
// "trust no proxy" everywhere.
var ErrTrustedProxyCIDRsNotSet = errors.New("config: TRUSTED_PROXY_CIDRS is not set")

// ErrTrustedProxyCIDRsAllMalformed is returned by RequireTrustedProxyCIDRs
// when TRUSTED_PROXY_CIDRS is set outside dev but every entry in it fails
// net.ParseCIDR — DELIBERATELY a distinct sentinel from
// ErrTrustedProxyCIDRsNotSet, so an operator who mistyped their only CIDR is
// told their value is invalid rather than sent looking for a variable that
// is, in fact, present.
var ErrTrustedProxyCIDRsAllMalformed = errors.New("config: TRUSTED_PROXY_CIDRS has no valid CIDR entries")

// trustedProxyCIDRsNoneSentinel is the ONLY accepted opt-out value for
// TRUSTED_PROXY_CIDRS — not "off", not "false", not an empty-looking
// string. It exists so an operator whose deployment genuinely has no proxy
// in front of it (a bare VM, a local reverse proxy not in a CIDR-shaped
// network) can say so explicitly, preserving today's fail-closed raw-peer
// behaviour, without being forced to invent a CIDR that misdescribes their
// network just to satisfy this guard. Compared case-insensitively after
// trimming.
const trustedProxyCIDRsNoneSentinel = "none"

// RequireTrustedProxyCIDRs refuses to boot when TRUSTED_PROXY_CIDRS is
// unset, empty, or entirely malformed outside HELIVANTA_ENV=dev (#824 Task
// 1, deployment slice 1b).
//
// # Why this exists: Config.TrustedProxyCIDRs' doc comment explains the mechanism —
// gin's default trusts every proxy, so without a configured trust boundary
// ClientIP() honours a caller-supplied X-Forwarded-For and every IP-keyed
// rate limiter (login, TOTP factor, and the three sibling IAM routes) is
// bypassable by sending a fresh header per request. #870 fixed the code to
// fail closed to the raw TCP peer when the value is empty, which is SAFE
// but COARSE: behind an ingress that collapses every caller into ONE
// bucket, turning RATE_LIMIT_LOGIN_PER_MIN into a hospital-wide budget any
// single caller can exhaust — an unauthenticated availability attack on
// clinician sign-in. This guard exists so a deployment cannot silently run
// in that coarse mode: it must be a DELIBERATE choice (the "none"
// sentinel), not a default nobody configured.
//
// # Reads the raw env var itself, not c.TrustedProxyCIDRs
//
// Load() has already parsed TRUSTED_PROXY_CIDRS through getenvCIDRList,
// which drops individually malformed entries and can arrive at an empty
// slice from either an unset variable OR a value that was set but entirely
// garbage. Those two cases must produce DIFFERENT boot-refusal messages (an
// operator who mistyped their only CIDR must not be told the variable is
// missing), so this function reads os.Getenv("TRUSTED_PROXY_CIDRS")
// directly rather than trusting the already-collapsed slice on c.
//
// # The four-way behaviour (see #824 Task 1 brief for the source table)
//
//	TRUSTED_PROXY_CIDRS   HELIVANTA_ENV=dev        otherwise
//	unset or empty        allowed (raw TCP peer)   refuse boot
//	"none"                allowed (raw TCP peer)   allowed (raw TCP peer)
//	valid CIDR list       allowed (trusted)        allowed (trusted)
//	every entry malformed allowed (empty list)     refuse boot, distinct message
//
// Dev is exempted because a developer's local stack has no proxy in front
// of it at all — the same reasoning DevHelivantaWebOrigin and
// DevSessionSigningKey rely on for their own guards.
func (c Config) RequireTrustedProxyCIDRs() error {
	raw := strings.TrimSpace(os.Getenv("TRUSTED_PROXY_CIDRS"))

	if strings.EqualFold(raw, trustedProxyCIDRsNoneSentinel) {
		return nil
	}

	if raw == "" {
		if c.IsDev() {
			return nil
		}
		return fmt.Errorf(
			"%w: refusing to boot without one outside HELIVANTA_ENV=dev — an unset value "+
				"leaves gin trusting the raw TCP peer only, which collapses every caller "+
				"behind a shared ingress into ONE rate-limit bucket (RATE_LIMIT_LOGIN_PER_MIN "+
				"becomes a hospital-wide budget any single caller can exhaust); set it to the "+
				"deployment's trusted proxy CIDR(s), or to %q if this deployment genuinely has "+
				"no proxy in front of it",
			ErrTrustedProxyCIDRsNotSet, trustedProxyCIDRsNoneSentinel,
		)
	}

	if !c.IsDev() && len(c.TrustedProxyCIDRs) == 0 {
		return fmt.Errorf(
			"%w: TRUSTED_PROXY_CIDRS=%q is set but every entry failed net.ParseCIDR — "+
				"refusing to boot rather than silently falling back to the raw-TCP-peer "+
				"trust boundary; fix the CIDR syntax (e.g. 10.20.0.0/16, not a bare IP), or "+
				"set it to %q if this deployment genuinely has no proxy in front of it",
			ErrTrustedProxyCIDRsAllMalformed, raw, trustedProxyCIDRsNoneSentinel,
		)
	}

	return nil
}

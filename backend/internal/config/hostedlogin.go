package config

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// DevHelivantaWebOrigin is the origin HELIVANTA_WEB_ORIGIN defaults to ONLY inside
// Config.IsDev() (see resolveHelivantaWebOrigin) — the local dev stack's shell
// origin, matching scripts/lib/zitadel.mjs's DEV_REDIRECT_URI so a fresh
// clone's two independently-configured defaults still agree with each
// other. Mirrors DevSessionSigningKey's shape (signingkey.go): a
// well-known, committed, dev-only value that must never silently answer
// for a real one outside dev.
const DevHelivantaWebOrigin = "http://localhost:4301"

// ErrHostedLoginOriginMatchesWebOrigin is returned by
// RequireDistinctHostedLoginOrigin when ZitadelHostedLoginURL and
// HelivantaWebOrigin resolve to the same origin. See that function's doc
// comment for what breaks if this is ever true.
var ErrHostedLoginOriginMatchesWebOrigin = errors.New(
	"config: ZITADEL_HOSTED_LOGIN_URL shares an origin with HELIVANTA_WEB_ORIGIN",
)

// ErrNoHelivantaWebOrigin is returned by resolveHelivantaWebOrigin when HELIVANTA_WEB_ORIGIN
// is unset outside dev. See resolveHelivantaWebOrigin's doc comment for why an
// unset value cannot simply fall back to DevHelivantaWebOrigin everywhere.
var ErrNoHelivantaWebOrigin = errors.New("config: HELIVANTA_WEB_ORIGIN is not set")

// resolveHelivantaWebOrigin returns HelivantaWebOrigin, trimmed, or DevHelivantaWebOrigin
// when it is unset AND c.IsDev() — never outside dev.
//
// This is the fix for a review finding on an EARLIER version of this
// file, which had HelivantaWebOrigin default to DevHelivantaWebOrigin
// unconditionally via Load()'s getenv(). That made
// RequireDistinctHostedLoginOrigin inert exactly where it is needed
// most: an unset HELIVANTA_WEB_ORIGIN in PRODUCTION compared the real
// ZitadelHostedLoginURL against "http://localhost:4301", found no
// collision (a real hosted-login URL is never literally
// localhost:4301), and booted — the redirect loop this guard exists to
// make unrepresentable stayed fully possible, with nothing anywhere
// reporting it. That is the identical shape SessionSigningKeySeed
// (signingkey.go) and RequireZitadelLoginClientToken
// (zitadelloginclient.go) already refuse to allow for their own
// secrets: a control that looks present and does nothing under the
// conditions that matter is not a control.
//
// So, mirroring those two functions' own env-loading style (plain
// os.Getenv in Load(), the default applied HERE instead of there):
// unset is accepted ONLY inside Config.IsDev(), where DevHelivantaWebOrigin is
// a known, committed, safe value never mistaken for a real deployment's
// origin. Outside dev, unset is ErrNoHelivantaWebOrigin — a boot refusal, not
// a warning, because (docs/standards/engineering-principles.md §4)
// compile error > BOOT FAILURE > CI failure > convention, and the
// alternative is exactly the silent-passthrough this fix exists to
// close.
//
// HELIVANTA_WEB_ORIGIN is consequently a NEW PRODUCTION PREREQUISITE as of
// this fix: whatever deploys Helivanta's API (this repo does not own that —
// see ZitadelLoginClientToken's PRODUCTION NOTE for the sibling case,
// #45/tesserix-infra) must set it to Helivanta's real public origin
// alongside ZITADEL_LOGIN_CLIENT_TOKEN, or the API refuses to start.
func (c Config) resolveHelivantaWebOrigin() (string, error) {
	origin := strings.TrimSpace(c.HelivantaWebOrigin)
	if origin != "" {
		return origin, nil
	}
	if c.IsDev() {
		return DevHelivantaWebOrigin, nil
	}
	return "", fmt.Errorf(
		"%w: refusing to boot without one outside HELIVANTA_ENV=dev — "+
			"RequireDistinctHostedLoginOrigin cannot tell a real ZITADEL_HOSTED_LOGIN_URL "+
			"apart from one misconfigured to loop every MFA-enrolled clinician back to "+
			"Helivanta's own login page without HELIVANTA_WEB_ORIGIN to compare it against; set it to "+
			"Helivanta's own public origin (scheme + host, no path — e.g. https://hms.example.org)",
		ErrNoHelivantaWebOrigin,
	)
}

// RequireDistinctHostedLoginOrigin refuses to boot if
// ZitadelHostedLoginURL — the handoff target LoginUIHandlers.Handoff
// sends a browser to whenever Helivanta's own login form cannot finish a
// sign-in itself (spec D3/D4: MFA required by org policy,
// forceMfaLocalOnly, a user's own voluntarily enrolled second factor, a
// federated hospital IdP, or a policy the API could not read and so
// fails closed on) — and HelivantaWebOrigin, the origin Helivanta's OWN /login page
// is served from, resolve to the SAME origin.
//
// A forced password change is NOT one of those cases, though an earlier
// version of this comment listed it. Verified live 2026-08-16 (#854
// Task 8, spike §5): Zitadel signals passwordChangeRequired to a login
// client nowhere in the flow — the session create, session read and
// finalize are byte-identical in shape to a normal user's — so Helivanta
// COMPLETES those logins rather than handing off. Tracked as #856; it
// changes nothing about this guard, which is about the handoff target's
// origin, not about which cases reach it.
//
// # Why this is worth a boot guard rather than "just configure it right"
//
// Spec D4 makes Helivanta's login page the thing that decides whether a
// password check is sufficient authentication, and hands off to
// Zitadel's hosted login whenever it decides it is not. That design is
// safe only because the handoff target is a genuinely DIFFERENT page
// than the one that just made that decision. If ZitadelHostedLoginURL
// were ever misconfigured to point back at Helivanta's own origin, the
// browser would be sent to `/login?authRequest=…` — the EXACT route
// that already decided this auth request needs a handoff — which reads
// the same auth request, renders the same form, decides "insufficient"
// again, and hands off again. Every MFA-enrolled clinician loops
// forever, and — this is the part that makes it worth a structural
// control rather than a comment — NOTHING anywhere reports an error:
// every individual step (read the auth request, render the form, POST a
// password, decide insufficiency, redirect) succeeds on its own. There
// is no failed request, no non-2xx response, no log line above INFO for
// an operator's alerting to ever catch. The only observable symptom is a
// clinician noticing the URL bar repeating, which is not a signal any
// dashboard in this codebase watches for.
//
// This is deliberately a BOOT failure, not a request-time check on the
// hot path (compile error > BOOT FAILURE > CI failure > convention,
// docs/standards/engineering-principles.md §4): it makes the loop
// UNREPRESENTABLE in a running process rather than merely unlikely, and
// it is a configuration defect an operator can fix in seconds — the
// same class RequireZitadelLoginClientToken (zitadelloginclient.go) and
// SessionSigningKeySeed (signingkey.go) already refuse to boot without
// resolving, immediately alongside this check in cmd/api/main.go.
//
// # Why scheme+host, not the full URL
//
// Compares SCHEME + HOST only (net/url's ordinary notion of "origin"),
// never the path: ZitadelHostedLoginURL legitimately carries a path
// (typically /ui/v2/login) that HelivantaWebOrigin never does, so comparing
// full URLs would either false-positive on that expected path
// difference or require HelivantaWebOrigin to be configured with a path it
// has no reason to carry. Two URLs on the same scheme+host are the same
// origin for this purpose regardless of path — a browser sent to either
// one lands on the same server, which is the property that actually
// matters here.
func (c Config) RequireDistinctHostedLoginOrigin() error {
	helivantaWebOrigin, err := c.resolveHelivantaWebOrigin()
	if err != nil {
		return err
	}
	hosted, err := url.Parse(c.ZitadelHostedLoginURL)
	if err != nil {
		return fmt.Errorf("config: ZITADEL_HOSTED_LOGIN_URL is not a valid URL: %w", err)
	}
	web, err := url.Parse(helivantaWebOrigin)
	if err != nil {
		return fmt.Errorf("config: HELIVANTA_WEB_ORIGIN is not a valid URL: %w", err)
	}
	if hosted.Scheme == web.Scheme && hosted.Host == web.Host {
		return fmt.Errorf(
			"%w: ZITADEL_HOSTED_LOGIN_URL=%q and HELIVANTA_WEB_ORIGIN=%q resolve to the "+
				"same origin — refusing to boot: an MFA-enrolled clinician handed off "+
				"from Helivanta's own login form would be sent right back to it, looping "+
				"forever with no error anywhere; point ZITADEL_HOSTED_LOGIN_URL at "+
				"Zitadel's hosted login origin instead",
			ErrHostedLoginOriginMatchesWebOrigin, c.ZitadelHostedLoginURL, helivantaWebOrigin,
		)
	}
	return nil
}

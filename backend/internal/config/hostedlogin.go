package config

import (
	"errors"
	"fmt"
	"net/url"
)

// ErrHostedLoginOriginMatchesWebOrigin is returned by
// RequireDistinctHostedLoginOrigin when ZitadelHostedLoginURL and
// HMSWebOrigin resolve to the same origin. See that function's doc
// comment for what breaks if this is ever true.
var ErrHostedLoginOriginMatchesWebOrigin = errors.New(
	"config: ZITADEL_HOSTED_LOGIN_URL shares an origin with HMS_WEB_ORIGIN",
)

// RequireDistinctHostedLoginOrigin refuses to boot if
// ZitadelHostedLoginURL — the handoff target LoginUIHandlers.Handoff
// sends a browser to whenever HMS's own login form cannot finish a
// sign-in itself (spec D3/D4: MFA required, forceMfaLocalOnly, a forced
// password change, a federated hospital IdP, or a policy the API could
// not read and so fails closed on) — and HMSWebOrigin, the origin HMS's
// OWN /login page is served from, resolve to the SAME origin.
//
// # Why this is worth a boot guard rather than "just configure it right"
//
// Spec D4 makes HMS's login page the thing that decides whether a
// password check is sufficient authentication, and hands off to
// Zitadel's hosted login whenever it decides it is not. That design is
// safe only because the handoff target is a genuinely DIFFERENT page
// than the one that just made that decision. If ZitadelHostedLoginURL
// were ever misconfigured to point back at HMS's own origin, the
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
// (typically /ui/v2/login) that HMSWebOrigin never does, so comparing
// full URLs would either false-positive on that expected path
// difference or require HMSWebOrigin to be configured with a path it
// has no reason to carry. Two URLs on the same scheme+host are the same
// origin for this purpose regardless of path — a browser sent to either
// one lands on the same server, which is the property that actually
// matters here.
func (c Config) RequireDistinctHostedLoginOrigin() error {
	hosted, err := url.Parse(c.ZitadelHostedLoginURL)
	if err != nil {
		return fmt.Errorf("config: ZITADEL_HOSTED_LOGIN_URL is not a valid URL: %w", err)
	}
	web, err := url.Parse(c.HMSWebOrigin)
	if err != nil {
		return fmt.Errorf("config: HMS_WEB_ORIGIN is not a valid URL: %w", err)
	}
	if hosted.Scheme == web.Scheme && hosted.Host == web.Host {
		return fmt.Errorf(
			"%w: ZITADEL_HOSTED_LOGIN_URL=%q and HMS_WEB_ORIGIN=%q resolve to the "+
				"same origin — refusing to boot: an MFA-enrolled clinician handed off "+
				"from HMS's own login form would be sent right back to it, looping "+
				"forever with no error anywhere; point ZITADEL_HOSTED_LOGIN_URL at "+
				"Zitadel's hosted login origin instead",
			ErrHostedLoginOriginMatchesWebOrigin, c.ZitadelHostedLoginURL, c.HMSWebOrigin,
		)
	}
	return nil
}

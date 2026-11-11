# Explicit native security-key continuation

Accepted implementation decision, 2026-11-11. Partially supersedes the blanket
no-hosted-login rule in the original login-client specification: this exception
is an explicit security-key action after an accepted password, never an automatic
redirect or a completed Helivanta login.

## Evidence and boundary

Live auth requests V2_399099301264885779 and V2_399104842544448514 accepted the
password then refused U2F/PASSWORD/IDP. The original owner has a ready legacy U2F
credential on Zitadel's authentication domain; the two new QA tenant owners do
not. A credential registered on that relying-party domain cannot simply be used
from helivanta.app. A live, new Helivanta OIDC request was successfully opened by
the existing native Login V2 at `/ui/v2/login/login?authRequest=...`, reaching its
login-name page without an error. Its two-linked-IdP identifier-selection branch
returned no next action for the original owner. A direct native password-page
continuation was then tested with the authorized original credential, reaching
`/ui/v2/login/u2f` and “Verify 2-Factor” without a credential error. No key was
removed or re-registered.

Zitadel owns credentials, MFA, organisation authentication policy and native OIDC
completion. Helivanta owns application sessions, verified tenant membership and
resource authority. A security-key continuation grants none of those rights.
Each signup still creates its own deterministic `helivanta-<tenant UUID>` native
organisation. Existing verified people retain their home identity and receive
explicit owner membership in the new organisation; email existence is not a
reason to delete or move their identity or to merge tenant organisations.

## Decision

Keep Helivanta password/TOTP/enrolment/change flows. When the accepted password
has exactly a U2F additional method (PASSWORD and linked IDP are neutral), offer
`security_key_required: {url}` as an additive password-response alternative.
The server independently confirms a password-verified native session and rereads
the same active native auth request. The URL uses only the configured HTTPS
issuer origin, fixed native Login V2 password path, the native canonical login
name from the same password-verified session, and `requestId=oidc_<same ID>`.
The native password route supports user discovery without a prior native UI
cookie; it rechecks the password and existing key. This avoids the broken
multiple-IdP identifier selector without changing the shared login service.
The login name follows the native UI query convention and is never logged;
native cross-origin referrer policy limits disclosure. No return URL,
credential, native session token or signed-in session is supplied by a browser.
Other unsupported methods and mixed additional methods retain the refusal.

The UI validates issuer origin, HTTPS, fixed path and exact request ID and bounded nonempty native login-name query again,
clears credential state, explains the domain transition and requires an explicit
click. It never redirects from a refusal or automatically on this response.
Zitadel collects the password/security key and applies its policy on its own
origin. Its OIDC callback returns to the same existing Helivanta callback; normal
state/PKCE, fresh-auth signup binding, token verification, revocation checks and
membership/tenant authorization remain required. No custom password-only session
is finalized, and no assurance or clinical permission is inferred from an org,
email or the continuation response. Expired/unreadable requests and malformed
URLs refuse or return a retryable dependency failure, never a callback.

## Capacity, cost and recovery

No new service, database, schema, credential or instance-wide routing setting.
Per qualifying attempt add two bounded native reads (session factors and auth
request); response is under 1 KiB and no new Helivanta table or attempt row is added at either the
12- or 36-month horizon. Native authentication sessions and audit events retain
the existing identity-service lifecycle. Existing IP/password rate limits and ten-second native
request timeouts apply. Peak is bounded by the configured login limiter rather
than a new queue. No availability SLO is changed; the extra native dependency
fails closed. Human security-key latency is outside machine request latency.
Marginal infrastructure cost is existing login traffic and two reads per such
attempt. Audit a continuation as pending, not authenticated; log no credentials,
native tokens, callback codes or URL query values beyond existing request IDs.

Removing the security key weakens the user's enrolled protection; finalizing the
password-only session bypasses it. Registering a replacement on another origin
changes credentials. A new cross-origin WebAuthn bridge needs another trusted
surface and more state. These are rejected in favour of the already deployed
native UI and original relying-party domain. This exception intentionally shows
the native UI rather than silently breaking its origin contract.

Rollback is the prior Helivanta image through normal GitOps. Native identities,
organisations, credentials and completed actions remain intact. Validate wrong
password/unknown user, no verified password, unsupported/mixed methods, inactive
or mismatched auth request, unsafe issuer/URL, no premature callback, explicit
click only, original state/PKCE and native Login V2 continuation. A physical
security-key touch remains the account owner's action; do not simulate their key.

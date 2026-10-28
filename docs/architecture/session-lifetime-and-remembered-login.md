# Session lifetime and remembered login

The requested policy on 2026-10-14 changes the session and inactivity defaults
from fifteen minutes to twelve hours. This supersedes the default durations
in the earlier session and idle-timeout design documents; their authorization,
revocation and activity rules still apply.

`SESSION_TTL` controls the signed cookie's lifetime and browser expiry.
`IDLE_TIMEOUT` independently controls how long a session can remain inactive.
Both default to `12h`. Explicit environment overrides remain supported, and
invalid non-positive values still refuse startup.

The browser follows the server's renewal schedule. Long sessions renew every
five minutes; short explicitly configured sessions renew after a third of their
lifetime, subject to the existing thirty-second minimum. Renewal rechecks
Zitadel user state and OpenFGA tenant membership. It preserves the original
authentication time and inactivity deadline; background requests do not count
as human activity. User activity can extend the inactivity deadline. Logout,
revocation watermarks, cryptographic verification, tenant authorization and
forced database RLS remain enforced.

Ordinary protected requests also recheck current identity through IAM's mandatory
guard; renewal is not the sole enforcement point. Known non-active identity
observations commit a global credential watermark and invalidation together.
Reactivation requires a fresh authentication and does not restore old cookies.
Unknown/unavailable identity state denies with 503, while self sign-out retains
its ability to end the caller's own credentials. Administrative revocation still
requires live identity because it changes another user's access. See
[ADR-0047](../adr/0047-current-identity-on-protected-requests.md) for the request
budget, fresh watermark check, outage consequences and employment-lifecycle limits.

Newly issued sessions receive the new window. An already expired session must
sign in again; deployment does not resurrect an expired or revoked cookie.

The credential form enables browser autofill with `autocomplete="on"`,
`username` and `current-password`. Submission reads the displayed input values
before validating them because password managers can fill inputs without React
change events.

“Remember my username” is enabled initially. The browser stores only the
selected preference and username under `helivanta.login.remembered-username`.
Opting out clears the stored username. Corrupt or blocked browser storage does
not prevent sign-in. The remembered name never authenticates a user or changes
their tenant membership. Password saving and autofill belong to the browser's
password manager and its user-controlled preferences.

Verification includes a virtual-clock check of validity until twelve hours and
expiry at that boundary, frequent renewal/access checks, existing logout and
revocation tests, remembered-name reload and opt-out, storage failures, and
autofill without React change events. Short-duration idle/renewal E2E fixtures
retain explicit overrides to exercise timeout handling quickly.

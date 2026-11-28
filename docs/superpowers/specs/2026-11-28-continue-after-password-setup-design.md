# Continue sign-in after setting the password

**Issue:** #1778. Part of #1767. It covers owners (#1763) and staff (#1777).

## Problem

After setting a password on `/signup/verify` or `/staff/accept`, the page starts
a fresh sign-in with `prompt=login` and `max_age=0`. The person lands on the
`/login` card and types the password they chose seconds earlier. Only then do
they reach authenticator setup.

## Goal

Set your password → authenticator setup on the same card → dashboard. No second
password entry.

## Decisions

### D1 Park a verified session server-side (option A of the issue)

The set-password request already carries the password through Helivanta's
backend to the identity owner.

- **Creating the session.** Once the owner answers success, the same request
  creates a Zitadel password session for the exact native subject, by `userId`
  and the password just accepted. It then reads the session back and requires
  the expected user and organisation.
- **Where it is parked.** The session goes into the existing `login_attempt`
  table, at a new `parked` stage (migration `0059_iam_parked_sessions` widens
  the stage check). Its key is `parked:` followed by the SHA-256 of a random
  32-byte handle. This reuses the table's 5-minute expiry, its sweep, its
  global-mutation guard and its no-RLS reasoning. Each step reads only rows at
  its own stage, so no factor, enrolment or password-change endpoint can resume
  a parked session.
- **Why not a new table.** A new global `iam_` table would need its own guard
  trigger and a re-pinned operating-governance fingerprint, for no added
  isolation. This decision is the builder's call.
- **The cookie.** The handle goes to the browser once, in a
  `helivanta_setup_handle` cookie:
  - `HttpOnly`, `Secure`, `SameSite=Strict`;
  - `Path=/`, the same as the other auth cookies (their gateway path is not
    `/api/v1`);
  - `Max-Age` 5 minutes.
- **The password** is used for that one Zitadel call and kept nowhere.

Option B (authorize first) is rejected. It would carry the emailed code across
the authorize redirect in browser storage.

### D2 Where the subject comes from

- **Owners.** The `verify-email` and `password` proof already names `userId`
  (`owner-<tenant>`). The identity owner has just proven the code for exactly
  that user, so it is authoritative after a successful response.
- **Staff.** The browser never knows the subject.
  - The identity owner's private staff-setup response gains an optional
    `subject` and `organization_id`, present only on `complete`
    (onboarding-service contract change).
  - Helivanta's proxy consumes both fields and never forwards them to the
    browser. The receipt the browser gets is unchanged.

### D3 Resume on `/login`

- **The call.** The login page can't see the HttpOnly cookie. So the setup
  pages also mark the tab in `sessionStorage` (`helivanta.setupResume`, which
  holds no secret). When `/login` opens an auth request with that mark, it
  consumes the mark and calls `POST /api/v1/auth/login/resume` with
  `{auth_request_id}`.
- **Consuming the row.** In one statement the server deletes the row whose hash
  matches and whose expiry is in the future, returning it (single use), and
  clears the cookie.
- **Checking the auth request.** The server requires that the request is this
  deployment's web client. If the request is organisation-scoped, its
  organisation must equal the parked organisation; otherwise the request is
  refused and the session is not used.
- **Continuing.** The session then goes through `continueWithSession`, exactly
  as a password step does. The answer has today's shapes
  (`enrollment_required`, `factor_required`, `complete`, `password_change`), so
  the card renders the authenticator step in place.
- **Fallback.** A missing, expired, replayed or foreign handle answers 404, and
  the page shows the normal email card. No error is shown, because this is only
  a shortcut. A malformed organisation-scoped request answers 400, as it does
  on the password step.
- **Password change.** If a password change is due, the step starts with no
  current password, because none was typed on this page. A password that was
  set seconds earlier is never due under the current policies.

### D4 MFA is unchanged

Finalising still happens only through `CompleteIfSufficient` and its
`sufficient` witness. A parked session is password-only, so under `forceMfa`, or
an account that requires recovery, the person must enrol or prove a second
factor. A test pins that no resume path finalises on the password alone (checked
by mutation).

### D5 Failure never blocks setup

If creating or parking the session fails, the set-password response is still a
success. The browser gets no cookie, and the page falls back to today's sign-in
start. The failure is logged by stage only, never with the subject, password or
token.

## Not covered

- Creating a passkey on this card (#422).
- The two-step signup form (#1767 D3).
- Account choice: the parked session is for one known subject, so there is
  nothing to choose.

## Tests

- **Go:**
  - Park and resume complete through `enrollment_required` under `forceMfa`.
  - A replayed handle gets 404.
  - An expired handle gets 404, as does a different browser (no cookie).
  - A foreign-organisation auth request is refused, with the session unused.
  - A wrong or expired emailed code creates no session and parks nothing.
  - The password, the token and the handle appear in no log line.
  - Mutation: removing the sufficiency call fails a test.
- **Vitest:**
  - After setup the page starts the authorize without a password field.
  - `/login` with a resume answer renders the authenticator step.
  - A 404 renders the email card.
- **e2e:** `customer-signup.spec.ts` asserts that the tab is marked for resume
  before the sign-in redirect. That is all e2e can check. CI's signup owner
  is route-stubbed and its staff owner is a fixture, so neither can set a real
  password that a parked session could check. Parking and resuming are covered
  by Go tests (real Postgres, fake Zitadel) and Vitest. The `userId` session
  check is the same call the scoped password step already makes against real
  Zitadel.

## Delivery

1. onboarding-service: the staff-setup `complete` response carries `subject`
   and `organization_id` (D2).
2. helivanta: in one PR,
   - the migration;
   - parking in both setup proxies;
   - the resume route;
   - the login page;
   - the tests.

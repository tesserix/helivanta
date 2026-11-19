# One sign-in screen: email and password together, account resolved after the password

**Issue:** [#1751](https://github.com/tesserix/helivanta/issues/1751)
**Depends on:** [#1750](https://github.com/tesserix/helivanta/issues/1750) (ships first) and
[tesserix/onboarding-service#46](https://github.com/tesserix/onboarding-service/issues/46).
**Refines:** [ADR-0070](../../adr/0070-tenant-owned-login-identities.md),
[tenant-account-chooser](../../design/tenant-account-chooser.md),
[login failure taxonomy](2026-10-08-login-failure-taxonomy-design.md),
[no unscoped MFA hint](2026-10-08-no-unscoped-mfa-hint-design.md).

## The problem, stated precisely

Today a person on a new device sees four screens before they are in: email,
emailed code, account choice, then a card asking for the password (with the
email again until #1750). mark8ly and the Tesserix console show one
email-plus-password card. The product owner asked for the same here on
2026-11-19.

The split is not a UI choice. Helivanta keeps one Zitadel account per tenant,
in a per-tenant organisation, with a tenant-unique username; the email is a
delivery address (ADR-0070). `CreatePasswordSessionForRequest` therefore needs
the organisation from the auth request's scope before it can map the typed
email to a user. Discovery exists to find that organisation first, and it needs
mailbox proof because an unauthenticated email must not reveal accounts.

Two facts make a single screen possible without reversing ADR-0070:

1. The password is stronger proof than the mailbox code. Once a password has
   been verified for an account, showing that account's name discloses nothing
   the person did not already prove they own.
2. The unscoped branch already exists. `CreatePasswordSessionForRequest` with no
   organisation scope falls through to `CreatePasswordSession(loginName,
password)`. The change is what that branch does, not a new entry point.

mark8ly avoids all of this by having one Zitadel account per person and picking
the tenant after login. That model was rejected for Helivanta on 2026-11-12 and
this spec does not reopen it.

## Decisions

### D1 — One card, unscoped auth request

`/login` with no `workspace` and no remembered workspace starts
`signinRedirect` with the default scope (no `urn:zitadel:iam:org:id:` entry) and
`state: { purpose: "email-login" }`. Zitadel bounces back to
`/login?authRequest=…` and the existing credential card renders with the labels
"Email or username" and "Password". Discovery is no longer the first screen.

The start is automatic when neither `SIGNED_OUT_MARK` nor `IDLE_ENDED_MARK` is
present and storage could be read; otherwise the landing shows the existing
button and wording. #847's race was the post-logout arrival, which still gets
the button. The remaining #847 argument, that sign-in should be an act on a
shared terminal, is satisfied by `prompt=login`: nothing is granted until a
password is typed. #1431's `autoStart` branch becomes this rule and stops being
gated on `workspace`.

### D2 — Resolve candidates by email, privately, after submit

`Password` on an unscoped request:

1. If the login name contains `@`, call the identity owner's new
   `login-targets` lookup (onboarding-service#46) with the email. It returns at
   most five `{tenantId, organizationId, subject, name, account}` entries under
   the existing service credential. Nothing from this call reaches the browser
   before step 4.
2. If the login name has no `@`, or the lookup returns no entries, run today's
   `CreatePasswordSession(loginName, password)` by Zitadel login name, so a
   tenant-unique username still works and an email with no Helivanta account
   costs one password check like any other refusal.
3. For each candidate, `createPasswordSession({userId: subject}, password)`
   followed by the existing `sessionSubject` check that the session's
   organisation equals the candidate's. Run concurrently with a bound of five;
   wall time is one check, not N.
4. Zero successes: `respondEqualisedFailure`, the same body, status and floor as
   a wrong password today. One success: continue exactly as the scoped path does
   (`CompleteIfSufficient`, security key, factor, enrolment, password change).
   More than one success: D3.

The lookup is a dependency failure like Zitadel being down: `ErrUnavailable`
maps to the existing 503, never to a refusal and never to a fallback that
guesses.

### D3 — Chooser after proof, with parked sessions

When more than one candidate accepted the same password, the handler answers
`200` with `{ "account_choice_required": [ { "tenant_id", "name", "account" } ] }`,
the same "200 whose only key names the next step" shape as `factor_required`,
and parks one row per matched session in a new table `login_account_choice`,
primary key `(auth_request_id, tenant_id)` (migration
`0053_iam_login_account_choice`). A separate table rather than a `tenant_id`
column on `login_attempt`: every existing attempt query is keyed on
`auth_request_id` alone, and changing that key would touch each of them for a
row shape only this step uses. Same pre-tenant, no-RLS allowlisting as
`login_attempt`. Writing the choices deletes any attempt or earlier choices for
the auth request in one transaction, as a fresh password step already
supersedes a pending attempt.

A new `POST /v1/auth/login/account` with `{ auth_request_id, tenant_id }`
deletes every choice for the auth request and returns them in ONE statement,
guarded so that it deletes nothing unless `tenant_id` is a live choice: two
concurrent picks cannot both resume a session, and a wrong pick does not
destroy the chooser. It terminates the sibling sessions through Zitadel's
`DELETE /v2/sessions/{id}` (failures are logged; the tokens never left
Helivanta), then continues the chosen session exactly as a single match does.
An unknown, expired or used pick is `auth_request_invalid` at the failure
floor. The sweeper deletes expired choices with expired attempts.

The chooser shows the names from the lookup, not from the browser. It is an
`accountChoice` step inside `LoginFlow`, a sibling of `otp`, `enroll` and
`passwordChange`; every outcome of the pick goes through the same `advance`
the credential card uses.

### D4 — Tenant at session exchange

The callback already lets `exchangeIdToken` omit `tenant_id`; `Login` then picks
the subject's single binding. A per-tenant Zitadel user has exactly one tenant,
so the unscoped path needs no new resolution. `customerSignupSigninStateContract`
gains the `email-login` purpose with no tenant.

### D5 — Enumeration and timing

- The refusal body for "email unknown", "wrong password" and "password matched
  no candidate" is `passwordFailureMessage`, at the `MinFailedLoginDuration`
  floor. The chooser is only ever reachable after a verified password.
- Success timing is not a leak: a success means the caller holds the password.
- The failure-floor residual recorded in `loginui.go` (an account with many
  recent failures answers slower than the floor) now applies per candidate.
  Concurrency keeps the sum from leaking the count; the slowest candidate
  bounds the response. Re-measure the floor with five concurrent checks before
  merge and record the number.

### D6 — Lockout amplification, accepted with controls

A failed email-plus-password attempt now counts against every account behind
that email in Zitadel's lockout policy (at most five, all the same person).
Controls:

- A second rate bucket keyed on `sha256(lowercase email)`, so a spray on one
  email is throttled even across IPs: five at once, then one every three
  minutes. Each attempt reserves a token before the check, so parallel
  attempts cannot all pass it, and a right password refunds its token
  (`ratelimit.Limiter.Refund`), so only wrong passwords spend the budget and a
  clinician signing in again after an idle timeout never meets it. The hash is never logged. Zitadel's lockout counts consecutive
  failures with no window, so the bucket slows a sprayer and keeps any burst
  below the lockout; it cannot make the lockout unreachable.
- The five-candidate bound in the identity owner and again in the handler.
- Production's lockout, read 2026-11-19 from
  `tesserix-k8s/charts/apps/zitadel-bootstrap/values.yaml`: instance-wide
  `maxPasswordAttempts: "10"` and `maxOtpAttempts: "10"`, declared in git and
  reconciled by the bootstrap's drift check. Helivanta's per-tenant
  organisations set no own lockout policy, so they inherit it. Worst case for
  this design: ten wrong passwords for one email lock up to five accounts of
  the same person, instead of one. The per-email bucket is sized below ten per
  lockout window so a single sprayer reaches its rate limit before the lockout.
  An organisation that later sets its own lower policy must re-open this
  decision.

### D7 — Discovery becomes the fallback

The emailed-code flow stays, reachable from a "Find my accounts by email" link
under the card and from recovery (#425). The chooser it reaches is unchanged.
A remembered workspace keeps starting a scoped request, which skips the
candidate lookup entirely; the lookup is for devices with no hint.

### D8 — What the frontend does not do

No client-side lookup, no prefetch of accounts, no caching of candidates. The
card submits exactly what it submits today to the same route; only the auth
request's scope differs.

### D9 — More than five accounts behind one email

The identity owner returns at most five candidates; past five it returns none
and sets `truncated`, so no binding the handler must not use reaches it. When
the flag is set the handler does not check any password: it answers the
distinct, non-enumerating `400 too_many_accounts` with "Use your workspace link
or find your accounts by email", the same way an expired auth request gets its
own wording. Checking an arbitrary five would silently lock a person out of the
rest; the refusal is only reachable after the caller already typed an email
that genuinely has more than five accounts, which is not an oracle worth
protecting against.

### D10 — A tenant that signs in through its own identity provider

**Not in slice 1.** Slice 1 checks every candidate's password; an account whose
organisation disallows passwords refuses it like a wrong one, which is what the
scoped card does for it today. The rest of this section is the follow-up.

Helivanta reads `allowUsernamePassword` for each candidate's organisation itself,
through the existing `loginclient.LoginPolicyForOrg` (the per-org read #950
added), so the identity owner's contract stays identity-only. A failed policy
read is `ErrUnavailable` (503), never a skip: skipping would turn an outage into
a silent refusal. Candidates with
`password_allowed: false` are skipped, never checked. If every candidate is
skipped and the login name had an `@`, the handler answers `202
{ "idp_required": [ { "tenant_id", "name" } ] }` only when the submitted
password is non-empty and at least one skipped candidate exists; the card then
offers "Continue with your organisation's sign-in" per tenant, which starts a
scoped request for that organisation exactly like a workspace link. Disclosure:
this reveals that the email belongs to a federated organisation without a
password check. Accepted because those organisations already advertise their
SSO on their own login surface and the alternative, a silent refusal, strands
every federated user on the new card. Record this in the threat table of
`tenant-account-chooser.md`.

### D11 — Emailed code on a new device, authenticator as the second factor

Clarified by the product owner on 2026-11-19: the emailed code is a new-device
check, not discovery proof, and the authenticator app remains the second
factor. Confirmed the same day: the code is asked only on a new device **and**
when the account has no enrolled authenticator, never on every new device. Order after a verified password, for the resolved account:

1. The organisation's own policy runs first, unchanged: `factorRequired`
   (authenticator code), `enrollmentRequired`, `passwordChangeRequired`,
   security-key hand-off.
2. Only when the account has **no enrolled second factor** and the browser is
   **not a known device** for that account does the handler answer
   `202 { "email_code_required": { "sent_to": "d•••@example.test" } }` and store
   the attempt at a new stage `stageEmailCode`. A new
   `POST /v1/auth/login/email-code` with `{ auth_request_id, code }` verifies it
   (eight digits, ten minutes, five attempts, the same budget as discovery)
   and then completes the sign-in. An enrolled authenticator is stronger than a
   mailbox and is never doubled with an emailed code.
3. A known device is a per-account, `HttpOnly`, `Secure`, `SameSite=Strict`
   cookie `helivanta_device`, holding a random 256-bit id, minted on the first
   fully completed sign-in on that browser and matched against a
   `login_device(account_subject, device_hash, last_seen)` row; 90-day lifetime,
   renewed on each successful sign-in. Private browsing and cleared storage
   are a new device by definition. The code is delivered through the identity
   owner's existing mailbox delivery used by discovery, never by Helivanta's
   own mailer; the masked address is the only disclosure and it follows a
   verified password.

Effect on the earlier boundary: a stolen password alone no longer completes a
sign-in on an unknown browser, whether or not the tenant forces MFA.

Orgs that force MFA are unaffected: enrolment still runs before anything else
and an enrolled authenticator skips the code. Recovery (#425) keeps its own
code flow.

## Not in scope

One account per person (reopening ADR-0070), passkeys (#422), recovery (#425),
return to the requested page after sign-in, and the remaining findings of the
2026-11-19 login audit.

## Contracts changed

- `POST /v1/auth/login/password`: new `200` outcome `account_choice_required`, `400 too_many_accounts`.
- `POST /v1/auth/login/account`: new, public like its siblings, rate-limited.
- `POST /v1/auth/login/email-code`: new, same shape and budget as
  `/factor`; `login_device` table and the `stageEmailCode` attempt stage,
  migration `NNNN_iam`.
- Onboarding `customercontract`: `LoginTargetsInput`, `LoginCandidate` and
  `LoginTargets` with `truncated` (onboarding-service#46, PR #47). Helivanta
  consumes the generated type; no copied wire model. `password_allowed` is
  not in it: see D10.
- `release-capabilities.json`: the new route is classified with the other
  login routes.
- `docs/api/domains/platform.md` and `go run ./cmd/contractdocs` outputs.

## Verification

Go (the `password_allowed` and `email_code_required` rows belong to the D10 and
D11 slices):

- Unscoped request, email with one candidate, correct password: completes;
  wrong password: equalised refusal body and floor.
- Two candidates, same password: `200` chooser; `POST /v1/auth/login/account`
  completes the chosen one and deletes the other session.
- Two candidates, password matches one: completes that one, no chooser.
- Lookup answers `truncated`: `400 too_many_accounts`, no password check made.
- All candidates `password_allowed: false`: `202 idp_required`, no password
  check made; one allowed candidate among them: only that one is checked.
- No enrolled factor, no device cookie: `202 email_code_required`; the right
  code completes and sets the device cookie; a wrong code five times expires
  the attempt. Enrolled factor: no code, authenticator step as today. Valid
  device cookie: no code.
- Login name without `@`: falls through to the login-name check.
- Lookup unavailable: 503, no refusal, no session created.
- Per-email bucket refuses after the rule's limit across two client IPs.
- Timing test: five concurrent candidates all refusing answer within the floor
  plus the measured slack.

Frontend (Vitest, `renderWithProviders`):

- Bare `/login` with no marks starts an unscoped `signinRedirect` once under
  Strict Mode; with a mark it shows the button.
- `account_choice_required` renders the chooser; choosing posts to
  `/auth/login/account` and navigates on `callback_url`.
- The discovery link still reaches the emailed-code flow.

Playwright: `e2e/tests/support/login.ts` drops the workspace link and signs in
through the single card; `account-discovery.spec.ts` starts from the link.

Gates: `pnpm turbo lint type-check test build format:check`, `make lint-go`,
`go test -race ./...`, `./scripts/coverage-gate.sh`, `go run ./cmd/contractdocs
-check`, release-capabilities regeneration.

## Slices

Slice 1 (D1–D9) ships the single card, the lookup, the chooser and the
per-email bucket. D10's federated hand-off and D11's new-device code are later
slices; neither weakens slice 1, which adds no path that skips a step the
scoped card takes. Slice 1 needs onboarding-service #47 deployed before
Helivanta: until then the lookup answers 404, which is unavailable, so an
unscoped email sign-in would answer 503 rather than guess.

## Open before implementation

1. ~~onboarding-service#46 merged and its `customercontract` version bumped here.~~
   Done: PR #47, pinned at `f47d44da`.
2. ~~Production Zitadel lockout policy value recorded (D6).~~ Done: ten, see D6.
3. Floor re-measured with concurrent checks (D5). Slice 1 proves concurrency
   with a stub (`TestEmailSessionsChecksEveryCandidateConcurrently`: five
   300 ms checks finish under 600 ms), so the floor still bounds one real check
   (about 0.7 s measured in #854) rather than five. A measurement against live
   Zitadel with five accounts is still owed before production acceptance.

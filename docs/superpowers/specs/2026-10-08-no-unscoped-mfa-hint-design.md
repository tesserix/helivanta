# The login form reads no unscoped policy, because it renders no MFA hint

**Issue:** [#917](https://github.com/tesserix/helivanta/issues/917)
**Amends:**
- `2026-08-20-login-policy-org-scope-design.md` (D3 and its residual)
- `2026-08-17-native-mfa-auth-components-design.md` (D5's `requireMfa`)

## The problem, stated precisely

`GET /v1/auth/login/request/:id` answers the login form with `policies`.
`policies.require_mfa` comes from `InstanceLoginPolicyForDisplay`, a login
policy read that is not scoped to any org. Zitadel resolves it against the
login-client PAT's own org, because no user has identified themselves yet.
On a multi-org instance it can disagree with what is enforced.

#917 asks for the hint to be made org-correct. Two facts decide how.

1. **Nothing renders the hint.** The shell maps `require_mfa` onto
   `@tesserix/web`'s `AuthMethodPolicy.requireMfa`, and no component in
   `@tesserix/web` 2.2.1 reads that field. `AuthCredentialForm` reads only
   `allowPassword`, `hidePasswordReset` and `allowRegister`, plus the
   login-name description. The only code that mentions `requireMfa` is the
   Zitadel adapter that produces it. So every form load spends an unscoped
   Zitadel round trip on the instance-level login-client PAT, which is shared
   by the whole Tesserix fleet, for a value no user ever sees.
2. **Every step after the password is already decided on the right org.**
   Since #947, #948 and #856, the next screen is chosen server-side after the
   password check: a TOTP code, an enrolment, a password change, a refusal,
   or completion. That choice uses `LoginPolicyForOrg` on the session's real
   org. The form never needs to know in advance.

## Decisions

### D1: Remove the read and the field; do not re-read by login name

`AuthRequest` stops reading a login policy. `require_mfa` is removed from the
response and from the shell's types in the same change. A field that cannot
be computed correctly before identification, and that nothing displays, is
deleted rather than corrected.

The issue's suggested fix was to re-read the policy once the login name is
known, before the password. It is rejected because it would build an
**account-enumeration oracle**. An endpoint that answers "this login name's
org forces MFA" before any credential is checked tells an unauthenticated
caller:
- whether a login name exists at all (an unknown name has no org to read);
- which org's policy a known name falls under.

Spec D5 of the login-client design equalises wrong-password and unknown-user
answers precisely so that "who works at this hospital" cannot be probed. A
per-login-name policy read would reopen that, in exchange for a hint nothing
renders.

### D2: The unscoped reader is deleted, not just unused

- `loginclient.InstanceLoginPolicyForDisplay` is deleted.
- `loginPolicy` now requires an org id, so every policy read in the package
  is org-scoped.
- `LoginPolicyForOrg` stays exactly as it is, with its refusal of an empty
  org id.

An unscoped login policy read is therefore no longer expressible through the
client, rather than merely avoided by its one caller.

The archtest `TestSufficiencyNeverReferencesInstanceLoginPolicyForDisplay`
guarded against the enforcer reaching for that method. With the method gone,
it is replaced by a stronger check,
`TestLoginClientExposesOnlyAnOrgScopedPolicyRead`. By reflection, the only
`*loginclient.Client` method that returns a `LoginPolicy` must be
`LoginPolicyForOrg`. Adding a second, unscoped reader anywhere in the package
then fails CI, not just one referenced from `sufficiency.go`.

### D3: What the form renders before identification

It renders a neutral credential form: login name and password. That is
exactly what it rendered before, because `require_mfa` never changed the
markup. The other `policies` fields (`allow_password`, `second_factors`,
`ignore_unknown_usernames`) are constants describing Helivanta's own
capabilities, not any org's policy. They stay.

### D4: Fail-closed behaviour is unchanged where it still applies

An unreadable auth request still fails the form (`respondLoginClientError`).
There is no longer a policy read at this step to fail. The fail-closed rule
for an unreadable policy lives where the policy is consulted:
`CompleteIfSufficient` and `CompleteAfterPasswordChange`, which answer a
retryable 503 and never "MFA not required". They are untouched.

## Not covered

- Enforcement (`LoginPolicyForOrg`, `CompleteIfSufficient`). It is out of
  #917's scope and unchanged.
- Multi-org discovery or org selection.

## Tests

- `AuthRequest` makes no policy read. The fake Zitadel's policy route fails
  the test if it is hit, and the response carries no `require_mfa` key.
- The archtest above. It is proven to fail by re-adding an unscoped reader
  method.
- `TestCompleteIfSufficientScopesThePolicyReadToTheSessionsOrg` is unchanged.
  It still pins that enforcement scopes to the session's org.
- Shell: `AuthPoliciesInfo` no longer has `require_mfa`. The type checker
  then refuses any reader of it, and the existing form tests pass on a
  response without it.

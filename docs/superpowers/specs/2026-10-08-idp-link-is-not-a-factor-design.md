# A linked external IdP is not a second factor

**Issue:** [#950](https://github.com/tesserix/helivanta/issues/950)
**Amends:** the "no external IdP is configured" premise recorded in D4 of
`2026-08-16-hms-login-client-design.md` and restated in D6 of
`2026-08-17-native-mfa-auth-components-design.md`; the IdP entry in D3 of the
former, D1 of the latter, and the `RefusalFactorUnsupported` row of D1 in
`2026-10-07-no-hosted-login-handoff-design.md`. Each is corrected in the same
change to point here.

## The problem, stated precisely

Observed in production on 2026-10-08, the first morning after #949 deployed.
A user with a correct password is refused with `sign_in_method_unsupported`.
The API log line that D5 of the 2026-10-07 spec added says why:

```
login refused refusal_reason=factor_unsupported
  enrolled_methods=[AUTHENTICATION_METHOD_TYPE_PASSWORD, AUTHENTICATION_METHOD_TYPE_IDP]
```

`loginclient.classifyEnrolledMethods` treats every method type other than
`PASSWORD` and `TOTP` as a factor Helivanta cannot collect, and both
`CompleteIfSufficient` and `CompleteAfterFactor` refuse on it. A linked
external identity provider, `AUTHENTICATION_METHOD_TYPE_IDP`, is in that
bucket.

The premise behind the bucket was recorded twice: "every Helivanta user is
local today — no external IdP is configured." It is false. Helivanta shares
the `auth.tesserix.app` Zitadel instance with the Tesserix console and
mark8ly, both of which offer Google sign-in, and the instance login policy
has `AllowExternalIDP: true`. Any account that has ever signed in to another
Tesserix product with Google carries an IdP link, and after #949 that account
cannot sign in to Helivanta at all.

## D1 — An IdP link is an alternative first factor, not an uncollectible second one

Zitadel's `authentication_methods` list answers one question: *which ways can
this user authenticate?* `PASSWORD`, `PASSKEY` and `IDP` are first factors;
`TOTP`, `U2F`, `OTP_SMS` and `OTP_EMAIL` are second factors. Whether a second
factor is **required** on a given session is decided by the org login policy
(`forceMfa`, `forceMfaLocalOnly`) and by what the user enrolled; whether a
first factor other than the one used is **also** required is decided by
nothing. After a successful password check, Zitadel finalizes the auth
request without asking for the IdP as well.

So the classification changes:

| Enrolled alongside `PASSWORD`                  | Before (#949)               | Now                                                                      |
| ---------------------------------------------- | --------------------------- | ------------------------------------------------------------------------ |
| `IDP`                                          | `RefusalFactorUnsupported`  | **neutral**: treated exactly like a password-only account                |
| `IDP` + `TOTP`                                 | `RefusalFactorUnsupported`  | **`OutcomeFactorRequired`**: native TOTP prompt, same as `TOTP` alone    |
| `IDP` + `TOTP`, after a verified code          | `RefusalFactorUnsupported`  | **complete**                                                             |
| `IDP` + any of `PASSKEY`/`U2F`/`OTP_*`         | `RefusalFactorUnsupported`  | unchanged: refused, enrolled methods logged                              |
| `PASSKEY`, `U2F`, `OTP_SMS`, `OTP_EMAIL`       | `RefusalFactorUnsupported`  | unchanged                                                                |
| nothing, `forceMfa` on                         | `RefusalMFAEnrollmentRequired` | unchanged                                                             |

**Why `PASSKEY` stays refused while `IDP` does not, when both are first
factors.** The distinction is not what Zitadel requires but what the product
has decided. The 2026-10-07 spec records the product owner's choice that an
account with a passkey must have it removed in Zitadel until native passkeys
(#422) ship; this spec does not reopen that. A linked IdP is different in one
practical way: it is created *by another product* on a shared instance, with
no action in Helivanta and nothing the clinician can see or undo from
Helivanta, so refusing on it locks users out of one product for having used
another. Passkeys and U2F keys are enrolled deliberately, by the user, in a
Zitadel UI.

**What the fail-closed direction is here.** Neutral is the correct direction,
not the lenient one: the control that decides whether a session is
sufficient is the policy-and-second-factor check, and that check runs
identically for an IdP-linked account. Nothing is bypassed. The alternative,
keeping `IDP` in the refusal bucket, does not close any bypass; it only
refuses a password Zitadel already accepted.

## D2 — The `forceMfaLocalOnly` fold survives

D4 of the 2026-08-16 spec folds `forceMfaLocalOnly` into `forceMfa` on the
assumption that every session Helivanta evaluates is local. That assumption
is about the **session**, not the account, and it remains true: Helivanta
only ever creates password sessions. A user whose account also carries an IdP
link, but who signed in here with a password, is a local sign-in for the
purposes of `forceMfaLocalOnly`, and the fold gives the right answer. The
"no external IdP is configured" wording in both specs is withdrawn and
replaced with this session-based statement. The fold must still be revisited
when Helivanta signs users in *with* an IdP (#423).

## D3 — Proof

- Unit, `CompleteIfSufficient`: `PASSWORD`+`IDP` under a non-forcing policy
  completes and finalize is called; `PASSWORD`+`IDP`+`TOTP` answers
  `OutcomeFactorRequired` with `["totp"]`; `PASSWORD`+`IDP`+`OTP_EMAIL` is
  still refused with the methods carried.
- Unit, `CompleteAfterFactor`: `PASSWORD`+`IDP`+`TOTP` with TOTP verified on
  the session completes; `PASSWORD`+`IDP`+`TOTP`+`OTP_EMAIL` is still refused
  and finalize is not called.
- Integration, real Zitadel: a throwaway user is given an IdP link
  (`POST /v2/users/{id}/links` against a throwaway Google-shaped provider
  created for the test and deleted after it), and `POST
  /v1/auth/login/password` answers 200 with a `callback_url` carrying `code`
  and `state`.
- Each new assertion was run against the pre-change code and failed.

## What this slice does not cover

- **Signing in with an external IdP from Helivanta's form** (#423 Enterprise
  SSO). Until it ships, an IdP-linked user signs in with their password.
- **Forcing a federated org to use its IdP.** The control for that is the org
  login policy (`allowUsernamePassword: false`), not a per-user link, and is
  part of #423.
- **Native passkeys/U2F** (#422), **email/SMS OTP** (#35), **TOTP enrolment**
  (#948). Their refusals are unchanged.

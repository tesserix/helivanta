# Spike — can a Zitadel login client drive MFA? (v4.15.3)

Run against the live dev stack on 2026-08-17, before any design was written,
for [#867](https://github.com/tesserix/helivanta/issues/867).

Every response below was **observed**. Where a call failed, the failure is
recorded as it came back rather than interpreted — this document exists because
the previous login-client spike was twice contradicted by live behaviour, once
because an absent field was written down as an observed `false`.

Stack: `ghcr.io/zitadel/zitadel:v4.15.3`, org `Helivanta`
(`386629565042196487`), login-client PAT at
`dev/zitadel/secrets/login-client.pat`, seed/IAM_OWNER PAT at
`dev/zitadel/secrets/helivanta-seed.pat`.

---

## The question

Helivanta's login-client design (`2026-08-16-hms-login-client-design.md`) D3
**hands off to Zitadel's hosted page** whenever a second factor is involved,
because Helivanta had no native screens for it. `@tesserix/web` 2.2.1 now ships
those screens. Adopting them is only possible if the **login-client PAT** — the
sole Zitadel credential Helivanta holds in production — can verify a second
factor against a session.

Nothing in the earlier spike covered this, and D4's finding makes guessing
dangerous: Zitadel does not enforce `forceMfa` for a login client and does not
signal a missing factor, so an unverifiable flow would fail silently rather
than loudly.

## 1. The answer: yes — `PATCH /v2/sessions/{id}` accepts a factor check

```
POST  /v2/sessions                       {checks:{user,password}}     → 201
PATCH /v2/sessions/386642608740368391    {sessionToken, checks:{totp:{code}}}
                                                                      → 200
GET   /v2/sessions/386642608740368391                                 → 200
```

Both the create and the PATCH authenticate with the **login-client PAT**, not
the seed PAT. That is the result that makes native MFA feasible at all: the
credential Helivanta actually has in production is sufficient.

The session then reports the factor:

```json
"factors": {
  "user":     {"verifiedAt": "...", "id": "386642607465299975", "loginName": "spike-mfa2@helivanta.dev"},
  "password": {"verifiedAt": "2026-08-17T07:49:14.209668Z"},
  "totp":     {"verifiedAt": "2026-08-17T07:49:14.218086Z"}
}
```

So `sufficiency.go` can read back which factors were verified rather than
inferring it.

## 2. **The session token ROTATES on every check, and this will break a naive implementation**

`PATCH` returns a **new** `sessionToken`:

```
create → sessionToken  eyJhbGciOiJBMjU2R0NNS1ci…TThyLw0dDPSwGHFkA9YOAuD1mNl-bDSYsie_h9L663Y…
PATCH  → sessionToken  eyJhbGciOiJBMjU2R0NNS1ci…Bv-uIdq9g9Ca35E-1wbGNyJegCcDRSWWlmK0_51Opjs…
```

The finalize call (`POST /v2/oidc/auth_requests/{id}`) takes
`session:{sessionId, sessionToken}`. **It must be given the token from the most
recent check, not the one from session creation.** A flow that stores the token
once at creation and reuses it after adding a factor is carrying a superseded
token — and the failure mode is a finalize rejection *after* the user has
already entered a correct code, which reads to the clinician as "my TOTP was
wrong".

This is the single most likely defect in implementing MFA and is the reason it
is recorded here in its own section.

## 3. `POST` to a session id is 405 — the method is `PATCH`

```
POST  /v2/sessions/386642608740368391 → 405 {"code":12,"message":"Method Not Allowed"}
PATCH /v2/sessions/386642608740368391 → 200
```

Recorded because it is a cheap mistake to make and the error is unambiguous.

## 4. Password and TOTP can also be supplied in ONE create call

```
POST /v2/sessions {checks:{user, password, totp:{code}}} → 201
```

Works with the login-client PAT. Not obviously useful for a real UI — the user
supplies the code only after the password is accepted — but it means the
one-shot shape is available if a flow ever wants it (e.g. a re-authentication
prompt that collects both at once).

## 5. TOTP verification is `/totp/verify` — **NOT** `/totp/_verify`

```
POST /v2/users/{id}/totp/_verify → 404 {"code":5,"message":"Not Found"}
POST /v2/users/{id}/totp/verify  → 200
```

Found by probing, after `_verify` was guessed from the underscore convention
the rest of this API uses (`/_search`, `/_import`, `/authentication_factors/_search`).
TOTP registration verification does not follow it. Guessing cost the first
spike run its entire result: with the factor left unverified, the subsequent
session checks returned 400 and would have been misread as "the login client
cannot verify factors" — the opposite of §1.

Enrolment itself: `POST /v2/users/{id}/totp` → 200, returning both `uri` and
`secret`, so a native enrolment screen has what it needs to render a QR code.

After verification, `GET /v2/users/{id}/authentication_methods` reports:

```json
"authMethodTypes": ["AUTHENTICATION_METHOD_TYPE_TOTP", "AUTHENTICATION_METHOD_TYPE_PASSWORD"]
```

which is the same endpoint `CompleteIfSufficient` already reads.

## 6. The login-client PAT can CREATE users but NOT delete them

```
POST   /v2/users/human        (login-client PAT) → 200
DELETE /v2/users/{id}         (login-client PAT) → 403 {"AUTH-AWfge","No matching permissions found"}
DELETE /v2/users/{id}         (seed PAT)         → 200, GET → 404
```

Not needed by any Helivanta flow, but recorded for two reasons. First, it
bounds what a leaked login-client PAT can do — it can mint users on any org of
the instance, which is a *write* capability worth knowing about given D2 already
notes the credential is instance-level. Second, it is why this spike's first run
left a user behind: cleanup was attempted with the login-client PAT, got 403,
and the `GET` that was supposed to confirm a 404 returned 200 instead. All three
spike users were subsequently deleted with the seed PAT and confirmed absent
(three 404s and an email search returning zero results).

## 7. Not checked

Stated explicitly rather than assumed, per this repo's practice:

- **Passkeys / WebAuthn.** Deliberately out of scope: MFA first, passkeys as a
  follow-on ([#422](https://github.com/tesserix/helivanta/issues/422)). The
  session API exposes a `webAuthN` check, but nothing about challenge issuance,
  the browser assertion round trip, or whether the login-client PAT suffices for
  it has been observed.
- **Email and SMS OTP delivery.** `POST /v2/users/{id}/otp_email` enrols (200,
  observed), but no code was requested or delivered — the dev stack has no SMTP,
  and the send/verify round trip is unexercised. A design that offers
  `emailCode` or `smsCode` as a factor rests on an unverified path.
- **Finalize with a two-factor session.** The PATCH'd session was not carried
  through `POST /v2/oidc/auth_requests/{id}`. §2's token rotation makes this the
  first thing an implementation must prove, and it needs a real auth request id
  rather than a bare session.
- **Whether Zitadel rejects a finalize when the org policy requires MFA and the
  session lacks it.** The earlier spike's §2 says it does not for password-only;
  that finding is unchanged and untested in the presence of a verified factor.

## Consequences for the design

1. **Native MFA is feasible with the credential Helivanta already holds.** D3's
   handoff can be replaced for the TOTP case.
2. **The token must be threaded, not stored.** `loginclient.Session` currently
   carries one token from creation; any factor check has to replace it, and the
   finalize path must read the latest. This is a change to a struct that
   `sufficiency.go` guards, so it interacts with the arch test pinning the
   single finalize call site.
3. **Sufficiency stays server-side and gets stronger, not weaker.** The session
   read in §1 lets `CompleteIfSufficient` assert on verified factors instead of
   inferring from policy alone.
4. **Scope the first slice to TOTP.** `emailCode`/`smsCode` need §7's unverified
   delivery path, and passkeys need a protocol nobody here has exercised.

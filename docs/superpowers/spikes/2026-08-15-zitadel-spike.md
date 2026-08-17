# Spike: Zitadel locally, observed tokens and verification behaviour

- **Issue:** [#838](https://github.com/tesserix/helivanta/issues/838)
- **Supports:** [ADR-0006](../../adr/0006-zitadel-not-gip.md)
- **Date:** 2026-08-15
- **Rule followed:** record what was actually observed, not what the docs
  claim. Every answer below is backed by a command and its real output, run
  against a real local Zitadel. Nothing here is copied from Zitadel's docs
  without being independently produced.

## What was stood up

`ghcr.io/zitadel/zitadel:v2.65.1`
(`sha256:013d23b69aa681f03d36a7fd61e4837a7b049a7e22bd7215eb3e98e9dbf5543c`),
`start-from-init` mode, backed by its own `postgres:16-alpine`, alongside the
existing `hms-dev` stack — not inside it. Compose file:
[`spike/zitadel-838/docker-compose.zitadel.yml`](../../../spike/zitadel-838/docker-compose.zitadel.yml).
Ports follow the Helivanta shifted-port convention and do not collide with
`hms-dev` (postgres 15432, GIP 19099):

| Service | Port |
|---|---|
| Zitadel HTTP | 20080 |
| Zitadel's own Postgres | 15433 |

The `hms-dev` stack (postgres 15432, GIP 19099, nats/redis/openfga) was left
running throughout and was confirmed untouched (`docker ps` before/after
matched). This compose file is **not** referenced by `make up` or
`make dev-infra` — it is spike scaffolding only.

Bootstrap identity, `.env`-equivalent config, all inline in the compose file
for reproducibility (throwaway masterkey, throwaway admin password — this is
a local, single-use instance, deleted after this spike):

- Org: `helivanta-spike`
- Bootstrap human admin: `spike-admin@helivanta-spike.localhost` /
  `SpikeAdminPassw0rd!`

---

## P0-1 — Does Zitadel run locally in Docker alongside the existing dev stack?

**Answer: yes.** Clean start from empty Postgres to serving traffic:

```
02:28:43Z  "initialization started"
02:28:56Z  "server is listening on [::]:8080"
```

**13 seconds** from DB init to listening, on a machine with the image
already pulled. `docker compose ps` while running:

```
NAME                             IMAGE                             STATUS
helivanta-spike-zitadel-zitadel-1      ghcr.io/zitadel/zitadel:v2.65.1   Up (healthy)
helivanta-spike-zitadel-zitadel-db-1   postgres:16-alpine                Up (healthy)
```

`hms-dev` containers, queried at the same time, unaffected:

```
hms-dev-firebase-auth-1   0.0.0.0:19099->9099/tcp
hms-dev-postgres-1        0.0.0.0:15432->5432/tcp
hms-dev-nats-1             ...
hms-dev-redis-1            ...
hms-dev-openfga-1          ...
```

**One real gotcha hit and recorded, not smoothed over:** the masterkey must
be exactly 32 bytes. A 31-byte string produced a clean, non-obvious failure —
`migration failed ... masterkey must be 32 bytes, but is 31` — and the
container crash-looped (`Restarting (1)`) rather than failing fast at boot.
Worth a comment at the decision point if this is ever templated for real:
generate the masterkey with something that can't silently be off by one
(`openssl rand -base64 24` truncated/padded to exactly 32 raw bytes), and
verify its length in whatever script writes it.

## P0-2 — Does OIDC discovery work?

**Answer: yes.** `curl http://localhost:20080/.well-known/openid-configuration`:

```json
{
  "issuer": "http://localhost:20080",
  "jwks_uri": "http://localhost:20080/oauth/v2/keys",
  "grant_types_supported": [
    "authorization_code", "implicit", "refresh_token", "client_credentials",
    "urn:ietf:params:oauth:grant-type:jwt-bearer",
    "urn:ietf:params:oauth:grant-type:device_code"
  ],
  "claims_supported": [
    "sub","aud","exp","iat","iss","auth_time","nonce","acr","amr",
    "c_hash","at_hash","act","scopes","client_id","azp",
    "preferred_username","name","family_name","given_name","locale",
    "email","email_verified","phone_number","phone_number_verified"
  ],
  "id_token_signing_alg_values_supported": ["RS256"]
}
```

`auth_time` is advertised in `claims_supported` before a single token was
even issued — and it is present in every token observed below (Q3).

There is no `password` (Resource Owner Password Credentials) grant. This
mattered operationally: it's why getting a real token required driving the
actual hosted login UI rather than a one-line curl, both for the bootstrap
admin and for the test user below.

## P0-3 — Complete a real login for a test user and capture the raw ID token

**Answer: done, twice** — once incidentally for the bootstrap admin (SSO
carried the browser session straight through `/oauth/v2/authorize` on the
first attempt), and once deliberately for a fresh test user
(`test-clinician@helivanta-spike.localhost`), created via the Management API
(see Q6) and logged in via `prompt=login` to force a real credential
challenge rather than reuse the admin's session.

Full flow, all real: authorize (PKCE, `code_challenge_method=S256`) → login
form → password → (skipped optional MFA-setup prompt — Zitadel offers MFA
setup on first login, confirming MFA is a Zitadel-native feature as ADR-0006
assumed) → redirect to a throwaway local callback listener on `:9999` with a
real `code` → `POST /oauth/v2/token` exchanged with `code_verifier` → real
`id_token` back.

**Full decoded claim set of the test user's ID token** (JWT header + payload,
verbatim, nothing redacted — this is a throwaway local user with a throwaway
password):

```json
// header
{
  "alg": "RS256",
  "kid": "386320694599745539",
  "typ": "JWT"
}

// payload
{
  "iss": "http://localhost:20080",
  "sub": "386320787679739907",
  "aud": ["386320778469113859", "386320771120627715"],
  "exp": 1786804439,
  "iat": 1786761239,
  "auth_time": 1786761226,
  "amr": ["pwd"],
  "azp": "386320778469113859",
  "client_id": "386320778469113859",
  "at_hash": "bgnsZd3J56eYyQj6tNT4QQ",
  "sid": "V1_386320944781590531"
}
```

Answering the three sub-questions directly:

- **Is `auth_time` present?** Yes — `1786761226`, a Unix timestamp,
  distinct from `iat` (`1786761239`). `principalFromToken` in
  `backend/pkg/authn/gip.go` refuses any token with `AuthTime == 0`; this
  claim is present and non-zero. **The load-bearing premise holds.**
- **What is `sub`, and is it stable/opaque?** `386320787679739907` — a
  Zitadel-internal Snowflake-style numeric ID, opaque, matches the user's
  `userId` returned at creation time (Q6) and stayed identical across two
  separate logins. Structurally equivalent to GIP's `uid`: an opaque
  provider-assigned subject with no semantic content (not a UUID, but
  `principalFromToken` only requires it be a stable string — no UUID
  assumption is made on `Subject`, only on `tenant_id`, which per the ADR's
  decision no longer comes from the token at all).
- **What else is there Helivanta could use?** Notably little in the ID token
  itself: `amr` (`["pwd"]` — auth method reference, useful for step-up/MFA
  policy later), `sid` (session ID, useful for back-channel logout), `azp`.
  **No `email`, `name`, or `profile` claims landed in the ID token**, even
  though `scope=openid profile email` was requested — see the finding below.

**Finding not predicted by the ADR, worth flagging plainly:** `profile` and
`email` scopes do **not** populate the ID token by default in this Zitadel
version/config. They only show up via the userinfo endpoint:

```
$ curl http://localhost:20080/oidc/v1/userinfo -H "Authorization: Bearer $AT"
{
  "sub": "386320787679739907",
  "name": "Test Clinician",
  "given_name": "Test",
  "family_name": "Clinician",
  "nickname": "spike-test-user",
  "preferred_username": "test-clinician@helivanta-spike.localhost",
  "email": "test-clinician@helivanta-spike.localhost",
  "email_verified": true
}
```

This is standard OIDC behaviour (userinfo is allowed to carry claims the ID
token doesn't), but it means: **if the eventual design wants email/name in
the Helivanta session without an extra round trip to `/oidc/v1/userinfo`, that
needs an explicit Zitadel instance/app setting** (there is one —
"always add default claims to id_token" — **not verified this session**,
time-boxed out). Not a blocker, since ADR-0006 already decided identity
claims Helivanta needs (subject, tenant, auth_time) don't depend on this; flagging
so the design spec doesn't assume email arrives for free.

## P0-4 — Verify the ID token from Go using `github.com/coreos/go-oidc/v3`, no vendor SDK

**This is the single most load-bearing question — verified, cleanly, with a
throwaway Go program at `/tmp/zitadel-verify-spike` (not part of the repo;
not committed).**

```go
provider, _ := oidc.NewProvider(ctx, "http://localhost:20080")
verifier := provider.Verifier(&oidc.Config{ClientID: clientID})
idToken, err := verifier.Verify(ctx, rawIDToken)
```

Real output against the test user's real ID token from Q3:

```
VERIFY OK
Issuer: http://localhost:20080
Subject: 386320787679739907
Audience: [386320778469113859 386320771120627715]
Expiry: 2026-08-16 00:33:59 +1000 AEST
IssuedAt: 2026-08-15 12:33:59 +1000 AEST
auth_time present: true
auth_time value: 1.786761226e+09
```

`oidc.NewProvider` performed real discovery (`/.well-known/openid-configuration`)
and fetched the real JWKS (`/oauth/v2/keys`); `Verify` did a real RS256
signature check against that JWKS, plus issuer/audience/expiry checks — no
Zitadel SDK, no Firebase-equivalent client, nothing vendor-specific.

**Proved the verifier does real work, not a pass-through**, with two
negative cases against the same token:

```
$ ./verify $IDT "wrong-client-id-999"
VERIFY FAILED: oidc: expected audience "wrong-client-id-999" got [...]

$ ./verify $TAMPERED_IDT 386320778469113859      # last char of the JWT flipped
VERIFY FAILED: failed to verify signature: failed to verify id token signature
```

**Conclusion: the premise of ADR-0006 holds exactly as stated.** Standard
`go-oidc` verification against Zitadel's JWKS works, with no vendor SDK, and
rejects both wrong-audience and tampered tokens correctly. This is
independently reproduced, not taken from documentation.

---

## P1-5 — Revocation: what happens to an already-issued token when the user is deactivated?

Deactivated the test user via the Management API
(`POST /management/v1/users/{id}/_deactivate`, admin bearer token), then
re-checked the **same, already-issued** tokens from Q3/Q4 without asking for
new ones.

**The ID token still verifies locally — unchanged, still `VERIFY OK`.**
This is expected and important to state plainly: local JWT signature
verification is a pure function of the token bytes and the (still-valid,
unrotated) signing key. It has no way to reflect an account state change
that happened after issuance. **This is exactly why ADR-0006's decision that
Helivanta must own a revocation watermark from its own session is correct, not
just convenient** — Zitadel's ID token gives no live signal, by design (same
as any stateless JWT, same as GIP's ID tokens before Helivanta's own watermark
existed).

**The access token, checked against Zitadel server-side via userinfo, was
cut off:**

```
$ curl http://localhost:20080/oidc/v1/userinfo -H "Authorization: Bearer $AT"
{"error":"access_denied","error_description":"access token invalid"}
HTTP 401
```

So Zitadel *does* enforce deactivation — but only for calls that touch it
live (userinfo, and presumably the authorization/token endpoints for future
logins), never for a bare local JWT check. The gap between "token still
parses as valid" and "account no longer has standing" is real and is exactly
the gap Helivanta's own watermark already closes for GIP; it will close it exactly
the same way for Zitadel once #838's design lands.

**Introspection was attempted and blocked for a structural reason, not a
revocation one:** the discovery document lists
`introspection_endpoint_auth_methods_supported: [client_secret_basic,
private_key_jwt]` — no `none`. A public PKCE client (which is what a browser
app must be) cannot call introspection at all:

```
$ curl -X POST .../oauth/v2/introspect -d "token=$AT&client_id=$CLIENT_ID"
{"error":"invalid_client","error_description":"client must be authenticated"}
HTTP 400
```

**NOT VERIFIED:** whether Zitadel exposes a webhook/event stream on user
deactivation (Zitadel does document "Actions"/event triggers) that Helivanta could
subscribe to as an alternative/complement to userinfo polling. Not tested —
time-boxed out. If the eventual design wants a push signal rather than
learning about deactivation only when a token happens to be checked live,
this needs a follow-up spike.

## P1-6 — Programmatic user creation for e2e (replacing `scripts/seed-dev.mjs`)

**Answer: yes, and the specific mechanism was run end-to-end, twice —
once via a human bearer token (convenience during bootstrap), once via the
actual intended mechanism (machine user + Personal Access Token, no browser
involved at all):**

1. Create a machine (service account) user:
   `POST /management/v1/users/machine` → returns a `userId`.
2. **A freshly created machine user has zero permissions by default** — the
   first attempt to create a human user with its PAT failed with
   `membership not found (AUTHZ-cdgFk)`. Grant it a role:
   `POST /management/v1/orgs/me/members` with `roles: ["ORG_OWNER"]`.
3. Mint a PAT for it: `POST /management/v1/users/{machineUserId}/pats` →
   returns the raw token once (`4gwmH_3q...`, never retrievable again —
   matches standard PAT hygiene).
4. Using **only** that machine PAT (no admin session, no browser, no human
   credential in the loop), create a real human user with a password:
   `POST /management/v1/users/human` → `201`, real `userId` returned.

This is the mechanism that would replace `scripts/seed-dev.mjs`'s Firebase
Admin SDK calls: a bootstrapped machine PAT (created once, stored as a dev
secret) driving the Management API's `users/human` endpoint. No browser, no
interactive login, fully scriptable. The one operational wrinkle worth
carrying into the design: **user creation via the API leaves the user in a
"not verified" / `Activate User` state requiring an email code**, even when
`isEmailVerified: true` and `changeRequired: false` are set on the create
call — this only affects *interactive login*, not the API-driven creation
itself, but it means `seed-dev.mjs`'s replacement must also either suppress
that state via a Management API field not yet found (not verified — time-boxed
out) or pre-verify some other way. With no SMTP configured, the verification
code appeared directly in the container's stdout logs
(`level=warning ... Code:BJTNYZ ...`) — that is a dev-only escape hatch, not
a mechanism to design around.

---

## P2-7 — Operational observations (time permitting)

Time remained for a brief look; not a thorough capacity study.

- **Memory, idle, right after boot:** Zitadel ~128 MiB, its Postgres
  ~34 MiB (`docker stats --no-stream`). Comfortably fits alongside the rest
  of the `hms-dev` stack on a laptop; **not evidence either way for
  `db-f1-micro` in production**, which is a different question (persistent
  load, concurrent projections, real traffic) that this spike did not probe.
- **CPU, idle:** ~21% on one core, apparently background projection/event
  handler activity even with zero real traffic. Worth watching, not
  alarming, at spike scale.
- **Cold-start time:** ~13 seconds from `initialization started` to
  `server is listening`, on a clean database, image already pulled. First
  boot including image pull was materially longer (~30s of layer downloads).
  For CI, this means: cache the image layer, and budget on the order of
  15-20s health-check wait, comparable to what `dev-infra`'s existing
  `until curl ...` loop already does for the GIP emulator.
- **Own database, mandatory:** Zitadel will not run against a datastore it
  shares with anything else without its own schema/user — confirmed by
  setting it up with a dedicated `zitadel` Postgres user/database from the
  start (this spike never attempted sharing `hms-dev`'s Postgres, per the
  task brief, so "will not" here reflects Zitadel's documented architecture,
  not something independently forced and observed failing — **partially
  NOT VERIFIED**, flagged honestly).
- **Crash-loop on bad masterkey, not fail-fast-with-clear-exit:** already
  covered under Q1 — worth restating here because it is exactly the kind of
  "painful in CI" failure mode P2 asks about: a wrong 31-byte secret produces
  a *look-transient* restart loop rather than an immediate, unambiguous exit.
  A CI health-check with a short timeout would report this as "service never
  became healthy," not "masterkey is wrong" — someone would have to go read
  logs to find the real cause.

---

## Summary: what was and wasn't verified

| # | Question | Verified? |
|---|---|---|
| 1 | Runs locally in Docker alongside `hms-dev` | **Yes** |
| 2 | OIDC discovery works | **Yes** |
| 3 | Real login, full claim set, `auth_time` present | **Yes** |
| 4 | `go-oidc` verifies against JWKS, no vendor SDK | **Yes** — the load-bearing one |
| 5 | Revocation: ID token unaffected, userinfo cut off, introspection needs a confidential client | **Yes** (webhook/event alternative: **NOT VERIFIED**) |
| 6 | Programmatic user creation via machine PAT | **Yes** (email-verification-state suppression: **NOT VERIFIED**) |
| 7 | CI/budget concerns | Partially — memory/CPU/cold-start observed; production-scale behaviour and dedicated-DB necessity **NOT independently proven**, only configured-and-worked |

## Findings that should reach the design spec directly

1. **The ADR's central premise is confirmed, not just claimed:** standard
   `go-oidc` verification works against Zitadel with zero vendor SDK, and it
   correctly rejects tampered/wrong-audience tokens. This was the one
   finding worth stopping the migration over if it had failed — it didn't.
2. **`auth_time` is present and distinct from `iat`** in every token
   observed. `principalFromToken`'s contract survives unchanged.
3. **`sub` is an opaque Snowflake-style string, not a UUID** — fine for
   `Principal.Subject` (no format assumed there), but don't assume it's
   parseable as anything else.
4. **`profile`/`email` scopes don't land in the ID token by default** — they
   require a call to `/oidc/v1/userinfo`. If the design wants those claims
   in the Helivanta session, decide now whether that round trip is acceptable or
   whether the "always include profile claims" instance setting needs
   turning on (unverified in this spike).
5. **Deactivation does not invalidate an already-issued ID token by local
   verification** — it only affects calls that touch Zitadel live. This is
   exactly why the ADR's own-watermark decision is necessary, not optional.
6. **Introspection requires a confidential client** — a detail to carry into
   any design that considers introspection as a revocation-checking
   mechanism; it cannot be called from a public/browser client.
7. **Machine PAT is the clean mechanism for scripted user creation** — but
   API-created users still need an email-verification step resolved before
   they can log in interactively, and the mechanism to suppress that
   wasn't found in the time available.

---

## Task 0 — the seeding wrinkle, resolved

**Continuing from Q6.** Ran against a fresh instance of the same
`spike/zitadel-838/docker-compose.zitadel.yml` stack (`hms-dev` confirmed
untouched throughout, per the same `docker ps` check as before). Every call
below is a real request against that instance; every login below is a real
browser drive of the real hosted login UI, not a mock.

### The root cause, found by reading the wire format, not guessing

Q6 reported the wrinkle as "email-verification-pending blocks login even with
`isEmailVerified: true`". That framing turned out to be **wrong, and worth
correcting**: `isEmailVerified: true` was working exactly as asked the whole
time. The actual blocker was a **second, independent gate — a forced
password-change step** — that Q6's request never controlled, because the
field it used to try to control it does not exist on that endpoint.

Proof, extracted from ZITADEL Console's own compiled protobuf definitions
(`/ui/console/main.<hash>.js`, fetched and grepped, not guessed):

```
proto.zitadel.management.v1.AddHumanUserRequest.toObject = function(e,i){
  return {
    userName: ...(field 1),
    profile:  ...(field 2),
    email:    ...(field 3),
    phone:    ...(field 4),
    initialPassword: ...(field 5),
  }
}
```

`POST /management/v1/users/human` (`AddHumanUserRequest`) has **five fields,
full stop** — there is no `passwordChangeRequired`/`changeRequired` field on
it at all. Any such key in the JSON body is silently dropped by the
grpc-gateway (no error, no rejection — it just isn't a field on that
message), which is exactly the "set it and it didn't help" experience Q6
recorded. Sending `"password"` (rather than the real field, `"initialPassword"`)
on this endpoint is silently dropped the same way, leaving the user with no
password at all — which is a different, more severe version of the same
trap: it *looks* like it worked (`201`, a `userId` back) and the user even
reads back as `USER_STATE_ACTIVE`, but there is no credential to log in with.

The field that controls this, `passwordChangeRequired`, exists only on
**`ImportHumanUserRequest`** (field 7, confirmed the same way from the
bundle) — a sibling message, not a variant of the same one. Reached via a
different endpoint: `POST /management/v1/users/human/_import`.

### The working recipe

Bootstrap (once per environment — see caveat below), then two calls per
seeded user, no browser involved in either:

```bash
# 1. Machine user (once)
POST /management/v1/users/machine
  {"userName":"helivanta-seed-bot","name":"Helivanta Seed Bot","accessTokenType":"ACCESS_TOKEN_TYPE_BEARER"}
  → userId

# 2. Grant it an org role — a fresh machine user has none (Q6, still true)
POST /management/v1/orgs/me/members
  {"userId":"<machineUserId>","roles":["ORG_OWNER"]}

# 3. Mint its PAT (once, stored as a dev secret from here on)
POST /management/v1/users/{machineUserId}/pats
  {"expirationDate":"2027-01-01T00:00:00Z"}
  → token   # returned once, never retrievable again

# 4. Per user — the actual recipe — using ONLY the machine PAT:
POST /management/v1/users/human/_import
Authorization: Bearer <machine PAT>
{
  "userName": "e2e-<spec>-admin@hms.dev",
  "profile": {"firstName": "...", "lastName": "..."},
  "email": {"email": "e2e-<spec>-admin@hms.dev", "isEmailVerified": true},
  "password": "Password123!",
  "passwordChangeRequired": false
}
```

Both flags are load-bearing — dropping either one reproduces a documented
failure mode below, not a partial success.

Confirmed the equivalent also exists on the **v2** resource API (the plan
explicitly asked to compare v1 vs v2 — they behave the same once the right
fields are used, they just spell them differently):

```bash
POST /v2/users/human
Authorization: Bearer <machine PAT>
{
  "username": "e2e-<spec>-admin@hms.dev",
  "profile": {"givenName": "...", "familyName": "..."},
  "email": {"email": "e2e-<spec>-admin@hms.dev", "isVerified": true},
  "password": {"password": "Password123!", "changeRequired": false}
}
```

Both produced a user that read back as `USER_STATE_ACTIVE` **and** logged in
immediately with no extra step — the two-endpoint gap Q6 hit is closed on
either API surface. `/_import` was used for the bulk of this verification
because it is the v1-parity call the rest of Q6's recipe already used.

### The proof: a completed login, not a 201

Every login below drove the **real** hosted login UI
(`/oauth/v2/authorize` → `/ui/login/loginname` → `/ui/login/password`,
server-rendered HTML forms, not mocked) with Playwright (`chromium`,
headless, from `e2e/`'s own installed browser — no new dependency added).
Zitadel's first-party Console SPA is itself a PKCE client of the same
`/oauth/v2/authorize` endpoint the real Helivanta login page will use, so driving
it end to end and reading the token it stores is a real, complete
authorization-code-plus-PKCE exchange, the same mechanism `e2e/tests/support/login.ts`
drives against apps/shell — just captured from Console's `sessionStorage`
after its own internal exchange completes, rather than from a callback this
spike's own redirect_uri wasn't registered for.

Decoded ID token from one such login (`e2e-signoutspec-pharmacist@hms.dev`,
created via the recipe above, header + payload, verbatim):

```json
// header
{
  "alg": "RS256",
  "kid": "386323422205968387",
  "typ": "JWT"
}

// payload
{
  "iss": "http://localhost:20080",
  "sub": "386324150303588355",
  "aud": ["386322847301107715", "386322847301173251", "386322847301238787",
          "386322847301304323", "386322845724180483"],
  "exp": 1786806361,
  "iat": 1786763161,
  "auth_time": 1786763159,
  "nonce": "YX5wb3VTZGRNc3I5ci1XMGVQMnJaVUppMUEzZGx1VkozT1dwQmRYZ0tpdWpt",
  "amr": ["pwd"],
  "azp": "386322847301304323",
  "client_id": "386322847301304323",
  "at_hash": "DFD4x0mTmDVGKNFeSHR-CA",
  "sid": "V1_386324187767111683"
}
```

`auth_time` (`1786763159`) is present, non-zero, and distinct from `iat`
(`1786763161`) — the same load-bearing shape confirmed in Q3, now confirmed
for a user created entirely through the non-interactive recipe, with no
human ever touching a keyboard for this user's creation.

### Confirmed for several distinct users, not just one

Seven users total were created via the recipe (six via `_import` mirroring
the real `e2e-<spec>-<kind>@hms.dev` naming scheme, one via `/v2/users/human`
to prove the v2 path too) and **every one of them completed a real hosted-UI
login on the first attempt**, no retry, no extra screen:

| Email | `sub` | `auth_time` |
|---|---|---|
| `e2e-loginspec-admin@hms.dev` | `386324143089385475` | `1786763144` |
| `e2e-loginspec-pharmacist@hms.dev` | `386324144549003267` | `1786763147` |
| `e2e-tenantswitchspec-admin@hms.dev` | `386324145975066627` | `1786763150` |
| `e2e-tenantswitchspec-pharmacist@hms.dev` | `386324147451461635` | `1786763153` |
| `e2e-signoutspec-admin@hms.dev` | `386324148843970563` | `1786763156` |
| `e2e-signoutspec-pharmacist@hms.dev` | `386324150303588355` | `1786763159` |
| `e2e-v2attempt-admin@hms.dev` (v2 API) | `386324213000044547` | `1786763183` |

Each `sub` is distinct and matches the `userId` the creation call returned —
no collision, no reused subject, which is exactly what #781's per-spec-file
isolation requires.

### What did NOT work, and what happened instead

- **`POST /management/v1/users/human` (`AddHumanUserRequest`) with
  `"password"` + `"passwordChangeRequired": false` in the body** — the exact
  shape Q6 used. `201`, `userId` back, user reads back as
  `USER_STATE_ACTIVE`. **Login through the hosted UI never completes**:
  `/ui/login/password` accepts the (nonexistent, since `"password"` isn't a
  real field) attempted password with a "Password must contain upper case"
  — because there was never a password on the account to accept, the
  endpoint doesn't have a field for one under that name.
- **Same endpoint with the correct field name, `"initialPassword"`, but no
  `passwordChangeRequired` control (because the field doesn't exist here)**
  — creation succeeds, state reads `USER_STATE_ACTIVE`, but the hosted UI
  **stops at a forced `/ui/login/password` "Change Password" screen**
  (old/new/confirm) before it will issue a token. Not a hard block — a
  human (or a second scripted step, not attempted here since the goal was
  *immediate* login) could complete it — but it fails the plan's bar of
  completing login **immediately**, and it is silent about why: nothing in
  the `201` response or the `GetUser` read-back says a change is pending.
- **Setting `isEmailVerified: false` (or omitting it) on `_import` even with
  `passwordChangeRequired: false`** — reproduces the original
  email-verification-pending block on its own: `GetUser` reads back
  `USER_STATE_INITIAL`. Confirms `isEmailVerified: true` was never the
  wrong half of Q6's fix — it was always necessary, just not sufficient.

### Still NOT VERIFIED

- **A fully non-interactive path to the *first* machine PAT.** Steps 1–3
  above (machine user → org role → PAT) were driven using a bearer token
  obtained by scripting a real Playwright login as the bootstrap admin
  (`ZITADEL_FIRSTINSTANCE_ORG_HUMAN_*`) — a browser was involved for that one,
  first, environment-bootstrap step, not for any per-user seeding after it.
  `zitadel start-from-init --steps <file>` accepts declarative provisioning
  files and plausibly can create a machine user + PAT at first boot with no
  login at all, which would remove even that one browser step — **not
  attempted, time-boxed out**. Task 6 should decide whether closing this
  matters: the PAT this produces is a long-lived dev secret minted once per
  environment, not a per-test-run credential, so a one-time scripted browser
  bootstrap is a materially smaller problem than what Q6 left blocking.
- **Whether `passwordChangeRequired` has a different name on
  `AddHumanUserRequest` in a newer Zitadel version.** This spike is pinned to
  `v2.65.1`; not checked against latest.
- **Password complexity policy sensitivity.** `Password123!` satisfies this
  instance's default complexity policy; a production/dev-stack instance with
  a different policy could reject it — worth a length/charset comment where
  Task 6's seed script hard-codes the seeded password, mirroring
  `scripts/seed-dev.mjs`'s existing `PASSWORD` constant.

### Read: is this fragile?

**No — once the two fields are right, it is not fragile; it was
undocumented, not unreliable.** All seven creations behaved identically and
every login succeeded on the first attempt with no retry logic needed
(contrast `e2e/tests/support/login.ts`'s `MAX_SIGN_IN_ATTEMPTS` retry, which
exists for a real distributed-system race — the revocation watermark — not
for the seeding recipe). The actual risk is **the exact trap this section
documents**: `AddHumanUserRequest`'s silent-drop-of-unknown-fields behavior
means a future edit to Task 6's seed script that "helpfully" switches back
to the plainer-looking `/management/v1/users/human` endpoint, or that
reintroduces a `"password"` typo for `"initialPassword"`, will not error —
it will produce users that pass a code review reading only the `201` and
silently cannot log in, exactly as Q6 first found. Task 6 should either
comment this trap loudly at the call site or (better) have the seed script
assert the created user's state is `USER_STATE_ACTIVE` via a `GetUser`
read-back before declaring success, so a regression fails the seed step
itself rather than surfacing later as a mysteriously-failing e2e login.

---

## v4.15.3 re-verification (2026-08-15)

**Trigger:** production runs `ghcr.io/zitadel/zitadel:v4.15.3`
(`auth.tesserix.app`, GKE `tesseract-prod-in-gke`, namespace `zitadel`) — a
major-version gap from this spike's v2.65.1. v4 also splits the interactive
login UI into a separate `zitadel-login` service, and the org runs a
customised build of it in a private registry
(`tesserix/third-party/zitadel-login:v4.15.3-aurora.1`). Everything below is
re-run against a **local** `v4.15.3` stack this session stood up itself.
**No writes were made to production** — the only production contact was two
unauthenticated `GET`s of public OIDC metadata
(`https://auth.tesserix.app/.well-known/openid-configuration` and
`/oauth/v2/keys`), both read-only, both fine under the constraint. Everything
else — every `POST`, every login, every container — ran against
`spike/zitadel-838/docker-compose.zitadel-v4.yml`, a fresh local stack, `hms-dev`
confirmed untouched throughout by the same `docker ps` check as the original
spike.

### What was stood up

`ghcr.io/zitadel/zitadel:v4.15.3`
(`sha256:2356d646340724b3d843c024d589314adb9b54dbd6452ebc34008b011e023eff`),
`start-from-init`, its own `postgres:17-alpine` (v4 requires Postgres 17 —
CockroachDB support was dropped; the v2.65.1 stack used `postgres:16-alpine`,
a real difference, not a copy-paste artifact), plus a `caddy:2-alpine` reverse
proxy and, for the P1 run, `ghcr.io/zitadel/zitadel-login:v4.15.3` (stock
upstream). Compose file:
[`spike/zitadel-838/docker-compose.zitadel-v4.yml`](../../../spike/zitadel-838/docker-compose.zitadel-v4.yml),
plus [`Caddyfile.v4-login`](../../../spike/zitadel-838/Caddyfile.v4-login).
Ports shifted again to avoid colliding with both `hms-dev` and the v2.65.1
spike stack if all three were ever brought up together:

| Service | Port |
|---|---|
| Zitadel (via Caddy proxy) | 20081 |
| Zitadel's own Postgres | 15434 |

**The exact 31-byte masterkey trap the original spike warned about was hit
again, live, while writing this section** — `SpikeMasterKeyV4X32BytesLongXXX`
(31 bytes) crash-looped with the identical error the original spike
predicted: `migration failed ... masterkey must be 32 bytes, but is 31`. Fixed
to a verified-32-byte key
(`python3 -c "print(len('SpikeMasterKeyV4Exactly32BytesXX'))"` → `32`) before
retrying. Recorded here exactly because the original spike said this failure
mode was easy to reproduce by not checking, and it was.

**New v4-specific operational gotcha, not present in the v2.65.1 write-up:**
the compose healthcheck `/app/zitadel ready --config /dev/null` — the same
command the v2.65.1 file used and reported "Up (healthy)" for — reports
`Error: not ready` (exit 1) against this v4.15.3 instance **even while it is
demonstrably serving real traffic** (`curl .../debug/healthz` returns `ok`
throughout, discovery and every API call below worked). Root cause **NOT
VERIFIED** — the image is distroless (no `sh`, no `curl`, no `wget` in the
container, confirmed by a failed `docker exec ... sh`), so there is no
straightforward alternative healthcheck command to substitute. This spike
polled `/debug/healthz` from the host instead of trusting `docker compose ps`
health status, and the compose file's healthcheck is left in place but
documented as unreliable rather than silently "fixed" by deleting it.

### P0-1 — Does the v2.65.1 recipe still work on v4.15.3?

**Answer: yes, unchanged, both API surfaces, both proved with a completed
login — not a 201.**

**`POST /management/v1/users/human/_import` still exists and still works
exactly as documented in Task 0 above.** Real call against the running v4
instance, using a machine PAT (same bootstrap mechanism as before — machine
user → org role → PAT, all reproduced fresh against v4, not assumed):

```
POST /management/v1/users/human/_import
Authorization: Bearer <machine PAT>
{
  "userName": "e2e-v4spike-admin@hms.dev",
  "profile": {"firstName": "V4", "lastName": "Spike"},
  "email": {"email": "e2e-v4spike-admin@hms.dev", "isEmailVerified": true},
  "password": "Password123!",
  "passwordChangeRequired": false
}
→ HTTP 200 {"userId":"386363744248135683", ...}
```

Read-back confirms `"state": "USER_STATE_ACTIVE"` immediately, same as
v2.65.1.

**`POST /v2/users/human` also still exists and still works**, same shape as
Task 0 documented:

```
POST /v2/users/human
Authorization: Bearer <machine PAT>
{
  "username": "e2e-v4v2-admin@hms.dev",
  "profile": {"givenName": "V4", "familyName": "V2Test"},
  "email": {"email": "e2e-v4v2-admin@hms.dev", "isVerified": true},
  "password": {"password": "Password123!", "changeRequired": false}
}
→ HTTP 200 {"userId":"386363965455728643", ...}
```

**Both failure modes from Task 0 reproduce identically on v4.15.3** — this
matters because it means the *trap*, not just the recipe, survived the
version bump unchanged:

- `POST /management/v1/users/human` with `"password"` +
  `"passwordChangeRequired": false"` (Q6's original mistake) — `HTTP 200`,
  `userId` back, but the field is silently dropped exactly as before. Proven
  by driving the real hosted login: the account has **no credential at all**,
  so the login UI does not even prompt for an old password — it jumps
  straight to a bare "New Password" / "Confirm Password" screen with no "Old
  Password" field (`getByLabel(/Password/i)` in the throwaway Playwright
  script resolved to exactly those two fields, nothing else — a Playwright
  strict-mode error is itself evidence here, not just friction). Login does
  not complete.
- `POST /management/v1/users/human` with the correct `initialPassword` field
  but no `passwordChangeRequired` control (because the field doesn't exist on
  this message, same as v2.65.1) — creation succeeds
  (`USER_STATE_ACTIVE`), but the hosted UI stops at a forced **"Change
  Password"** screen (Old Password / New Password / Password confirmation),
  screenshot captured, before it will issue a token.
- **No `Deprecation` response header and no server-side deprecation log line**
  on any v1 call, checked directly (`curl -sD -` and `docker logs | grep -i
  deprecat`, both empty). Whatever migration-guide language exists for v1
  human-user endpoints (ZITADEL's public docs mark `AddHumanUser` and
  `ImportHumanUser` as deprecated in favour of a unified v2 create endpoint)
  is **doc-only** — nothing at runtime signals it. This is the same "looks
  fine, isn't" shape as the original `_import` vs `/users/human` trap:
  silence, not an error, is what a caller sees either way.

**The proof: two completed real logins, not two 201s.** Both driven through
the actual hosted login UI with a real Playwright browser (`chromium`,
headless), authorization-code-plus-PKCE against the real `/oauth/v2/authorize`
endpoint, tokens read back from Console's own `sessionStorage` after its
internal exchange completed — the same method the original spike used, same
justification (Console is itself a PKCE client of the identical endpoint
`e2e/tests/support/login.ts` drives against apps/shell).

**Decoded ID token, `_import`-created user (`e2e-v4spike-admin@hms.dev`),
header + payload, verbatim:**

```json
// header
{ "alg": "RS256", "kid": "386363123994722307", "typ": "JWT" }

// payload
{
  "iss": "http://localhost:20081",
  "sub": "386363744248135683",
  "aud": ["386363123994460163", "386363123994525699", "386363123994591235",
          "386363123994656771", "386363122585305091"],
  "exp": 1786829973,
  "iat": 1786786773,
  "auth_time": 1786786771,
  "nonce": "YjU0NmhtNmN2bl8xelJvUDdIWTJmLkYtWX54cG40SEFHamJrRDU3M2w1cTBi",
  "amr": ["pwd"],
  "azp": "386363123994656771",
  "client_id": "386363123994656771",
  "at_hash": "kiofy6tci_s1qP672qQEQg",
  "sid": "V1_386363801240338435"
}
```

`auth_time` (`1786786771`) present, non-zero, distinct from `iat`
(`1786786773`) — exactly the shape `principalFromToken` in
`backend/pkg/authn/gip.go` (per ADR-0006) requires. **`sub`
(`386363744248135683`) matches the `userId` the `_import` call returned.**

**Decoded ID token, `/v2/users/human`-created user
(`e2e-v4v2-admin@hms.dev`):** `sub` `386363965455728643`, `iat` `1786786878`,
`auth_time` `1786786876` — present, distinct, `sub` matches the v2 call's
returned `userId`.

**Conclusion — P0 is fully closed:** the v2.65.1 recipe is unchanged on
v4.15.3, on both API surfaces, traps included. Nothing in Task 0's guidance
needs revision for v4; it can be carried into Task 6's seed script verbatim,
with the same "loudly comment the trap or assert `USER_STATE_ACTIVE`
read-back" recommendation as before.

### P1-4 — What does the separate `zitadel-login` service mean for us?

**Answer, empirically, not from docs: a v4 instance with
`LOGINV2_REQUIRED=true` cannot serve a login on its own — the separate
service is mandatory once that flag is set, and a reverse proxy merging both
under one origin is mandatory too.** Three states tested, in order:

1. **`LOGINV2_REQUIRED=false` (the default in this spike's compose file) —
   core alone serves everything, `zitadel-login` never runs.** This is what
   P0 above ran against. The bundled `/ui/login` (same legacy UI as
   v2.65.1) handled both the bootstrap admin's login and every seeded
   user's login end to end, MFA-setup skip included. **No separate service
   needed in this mode** — this is the materially simpler option for local
   dev/CI if it's acceptable to diverge from production's `LOGINV2_REQUIRED`
   setting.

2. **`LOGINV2_REQUIRED=true`, `zitadel-login` NOT running — proved broken,
   not assumed:**

   ```
   $ curl -s -w "\nHTTP %{http_code}\n" \
       "http://localhost:20081/ui/v2/login/login?authRequest=V2_386364093532995587"
   {"code":5, "message":"Not Found"}
   HTTP 404
   ```

   Confirmed independently: an actual browser drive of `/ui/console` with
   this flag set redirects to `/ui/v2/login/login?authRequest=...` and then
   times out waiting for a page that 404s. **Core genuinely cannot serve a
   login on its own once this flag is set** — this is not a config nuance,
   it is a hard split.

3. **`LOGINV2_REQUIRED=true`, `zitadel-login` running, fronted by a reverse
   proxy on one origin — completed logins, both for the bootstrap admin and
   for an `_import`-seeded user.** The official ZITADEL compose file
   (`github.com/zitadel/zitadel` `deploy/compose/docker-compose.yml`, fetched
   and read directly — not paraphrased from a summary) uses Traefik to route
   `/ui/v2/login/*` (and `/`) to `zitadel-login` and everything else to core,
   because **the browser's redirect target is built from
   `ZITADEL_EXTERNALDOMAIN`/`ZITADEL_EXTERNALPORT` with no way to express two
   backend processes** — both must appear to live at the same origin. This
   spike replicated that with a minimal Caddy config
   (`spike/zitadel-838/Caddyfile.v4-login`) instead of Traefik. The
   login-service container needs, at minimum: `ZITADEL_API_URL` (reaching
   core over the docker network), `NEXT_PUBLIC_BASE_PATH=/ui/v2/login`, and
   `ZITADEL_SERVICE_USER_TOKEN_FILE` pointing at a PAT for a machine user
   holding the **instance-level** `IAM_LOGIN_CLIENT` role (granted via
   `POST /admin/v1/members`, not the org-level `/management/v1/orgs/me/members`
   used for `ORG_OWNER` — a different endpoint, easy to get wrong by pattern
   matching on the org-role call already in Task 0's recipe).

**Decoded ID token, bootstrap admin, logged in through the separate
`zitadel-login` service via the Caddy-fronted origin:**

```json
{
  "iss": "http://localhost:20081",
  "sub": "386363122585763843",
  "aud": ["386363123994460163", "386363123994525699", "386363123994591235",
          "386363123994656771", "386363122585305091"],
  "exp": 1786830432,
  "iat": 1786787232,
  "auth_time": 1786787231,
  "amr": ["pwd"],
  "azp": "386363123994656771",
  "client_id": "386363123994656771",
  "at_hash": "kUo5531AVnKQkKA3r9f6lA",
  "sid": "386364569368395779"
}
```

**Decoded ID token, an `_import`-seeded user
(`e2e-v4loginsvc-admin@hms.dev`), same separate-service path — proving the
seeding recipe and the split-login architecture compose cleanly, not just
each in isolation:**

```json
{
  "iss": "http://localhost:20081",
  "sub": "386364596883030019",
  "aud": ["386363123994460163", "386363123994525699", "386363123994591235",
          "386363123994656771", "386363122585305091"],
  "exp": 1786830462,
  "iat": 1786787262,
  "auth_time": 1786787261,
  "amr": ["pwd"],
  "azp": "386363123994656771",
  "client_id": "386363123994656771",
  "at_hash": "n5R9vIVrH70a8fO8lrZaqw",
  "sid": "386364623139373059"
}
```

Both have `auth_time` present and distinct from `iat`. One observed cosmetic
difference worth a note, not a concern: `sid` under the separate login
service is a bare numeric string (`386364569368395779`); under the legacy
bundled login it carries a `V1_` prefix (`V1_386363801240338435`). Nothing in
Helivanta's design depends on `sid`'s shape.

**Read: what should Helivanta actually run locally/in CI?** `LOGINV2_REQUIRED=false`
(core-only, legacy `/ui/login`) is the materially simpler option and this
spike proved it still works end-to-end on v4.15.3, traps and all. The
tradeoff, stated plainly: it means local/CI would exercise a **different**
login UI than what production's `auth.tesserix.app` actually serves (both
`zitadel-login` itself vs core-bundled, and separately the org's *own*
customised `zitadel-login` build, see below) — a real environment-parity gap
for the e2e suite, not a reason to avoid the simpler setup, but something
Task 6 / the design spec should decide consciously rather than by default.

### P1-5 — v1 management API deprecation in v4

Covered above under P0-1: **no runtime deprecation signal** (no header, no
log line) on any v1 human-user call in this v4.15.3 instance. ZITADEL's
public API reference marks the v1 `AddHumanUser` and `ImportHumanUser`
methods as deprecated in favour of v2's endpoints (checked via
`zitadel.com/docs`, not re-quoted verbatim here since the fetched summaries
were themselves paraphrased and this spike did not independently confirm the
exact wording against the raw `.proto`/OpenAPI source the way Task 0 did for
the `AddHumanUserRequest` field list) — but deprecated-in-docs is not the
same claim as broken-in-v4.15.3, and this spike's own `curl`/log evidence
above is the stronger, independently-produced claim: **the v1 endpoints are
fully functional on v4.15.3 today.** Task 6 should still prefer the v2
surface (`/v2/users/human`) for new code, both because it's the
forward-looking API and because this spike proved it works identically — but
should not treat "still works" as "safe indefinitely"; a future major version
could remove what v4.15.3 merely deprecates.

### What did NOT work / was NOT VERIFIED this session

- **The org's actual custom login image
  (`tesserix/third-party/zitadel-login:v4.15.3-aurora.1`) — NOT VERIFIED, and
  not attempted.** It lives in a private registry this session had no
  credentials for and was explicitly told not to fight. Everything in the
  P1 section above used the **stock** `ghcr.io/zitadel/zitadel-login:v4.15.3`
  image. This is a real, material gap for the e2e suite: the actual login UI
  our specs will drive in any environment running the custom build is
  untested by this spike. If the custom build changes routes, field names,
  button labels, or timing behaviour relative to stock — all things this
  spike had to hand-discover for stock (`getByLabel(/Login Name/i)` vs
  `Loginname`, `Continue` vs `Next`, the "Old Password" field appearing or
  not depending on account state) — `e2e/tests/support/login.ts`'s selectors
  could silently stop matching. **Recorded as NOT VERIFIED, not glossed
  over**, per the task brief.
- **Root cause of the `/app/zitadel ready` healthcheck reporting "not ready"
  while serving real traffic.** Worked around by polling `/debug/healthz`
  from the host instead; not diagnosed further, time-boxed out.
- **Exact wording of ZITADEL's own v1→v2 deprecation guidance** — this
  spike's `curl`/log evidence (no runtime signal) is solid; the doc-side
  claim about which v2 method officially supersedes which v1 method is
  paraphrased from fetched summaries, not independently re-derived from the
  `.proto` source the way Task 0's `AddHumanUserRequest` field list was.
  If Task 6 needs the exact API-reference wording, re-derive it from the
  primary source rather than trusting this note.
- **Whether the official Traefik-based compose's exact routing rules (this
  spike used a minimal Caddy substitute covering only the one path split
  that mattered) hide any other split-origin subtlety** — e.g. the official
  file also routes bare `/` to `zitadel-login` with a path rewrite, which
  this spike's Caddyfile does not replicate because nothing in the recipe
  or the login flow needed it. Not a gap in the *proof* (every URL actually
  exercised in this spike's login flows was proxied correctly), but a gap in
  *completeness* of the reverse-proxy config if it were ever promoted beyond
  spike scaffolding.
- **Whether `ZITADEL_FIRSTINSTANCE_LOGINCLIENTPATPATH`** (the official
  compose's first-boot shortcut for minting the login-client PAT without any
  interactive step) works as documented — this spike instead reused the
  same "browser-drive the bootstrap admin, use their session to mint the PAT"
  approach Task 0 already flagged as its one remaining non-interactive gap.
  That gap is therefore **still open on v4**, unchanged from Task 0's
  finding, and still judged low-priority for the same reason: it is a
  one-time environment-bootstrap step, not a per-test-run credential.

### Read: how much of the existing spike/design now needs revisiting?

**Very little of substance — the P0 answer is unchanged, which was the one
finding that could have forced a design rework.** Task 0's recipe, both API
surfaces, both traps, and the underlying `auth_time`/`sub` token shape all
survive the v2.65.1 → v4.15.3 jump intact. What *does* need to reach the
design spec, concretely:

1. **A conscious decision on `LOGINV2_REQUIRED` for local/CI**, now that
   both options are proven to work: simpler-but-diverges-from-prod
   (`false`) vs matches-prod-topology-but-needs-a-proxy-and-a-second-service
   (`true`). This spike does not recommend one over the other — it makes the
   tradeoff visible with working recipes for both, which is what was missing
   before this session.
2. **The custom login image gap is the one real open risk**, and it is
   entirely orthogonal to the seeding recipe (P0) — seeding will work
   regardless of which login UI serves the resulting user. It affects
   `e2e/tests/support/login.ts`'s selectors, not `scripts/seed-dev.mjs`'s
   replacement. Task 6 can proceed on the seeding recipe with confidence;
   whoever owns the e2e login flow itself should budget time to get
   credentials for the private registry and re-verify selectors against the
   actual `aurora.1` build before trusting `login.ts` in an environment that
   runs it.
3. **Nothing about Postgres 17, the healthcheck quirk, or the missing
   deprecation header changes any design decision** — they're operational
   footnotes for whoever templates this for real CI, not architectural
   findings.

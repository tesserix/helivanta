# Spike: Zitadel locally, observed tokens and verification behaviour

- **Issue:** [#838](https://github.com/tesserix/hms/issues/838)
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
Ports follow the HMS shifted-port convention and do not collide with
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

- Org: `hms-spike`
- Bootstrap human admin: `spike-admin@hms-spike.localhost` /
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
hms-spike-zitadel-zitadel-1      ghcr.io/zitadel/zitadel:v2.65.1   Up (healthy)
hms-spike-zitadel-zitadel-db-1   postgres:16-alpine                Up (healthy)
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
(`test-clinician@hms-spike.localhost`), created via the Management API
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
- **What else is there HMS could use?** Notably little in the ID token
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
  "preferred_username": "test-clinician@hms-spike.localhost",
  "email": "test-clinician@hms-spike.localhost",
  "email_verified": true
}
```

This is standard OIDC behaviour (userinfo is allowed to carry claims the ID
token doesn't), but it means: **if the eventual design wants email/name in
the HMS session without an extra round trip to `/oidc/v1/userinfo`, that
needs an explicit Zitadel instance/app setting** (there is one —
"always add default claims to id_token" — **not verified this session**,
time-boxed out). Not a blocker, since ADR-0006 already decided identity
claims HMS needs (subject, tenant, auth_time) don't depend on this; flagging
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
HMS must own a revocation watermark from its own session is correct, not
just convenient** — Zitadel's ID token gives no live signal, by design (same
as any stateless JWT, same as GIP's ID tokens before HMS's own watermark
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
the gap HMS's own watermark already closes for GIP; it will close it exactly
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
deactivation (Zitadel does document "Actions"/event triggers) that HMS could
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
   in the HMS session, decide now whether that round trip is acceptable or
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

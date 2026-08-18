# Helivanta boot secrets — inventory and provisioning

Governing design: `docs/superpowers/specs/2026-08-17-secrets-management-design.md`
(referenced below as D1–D7). Issue: [#45](https://github.com/tesserix/helivanta/issues/45).

This runbook covers Helivanta's two **boot secrets** only — the values `config.Load`
refuses to start without. Per-tenant integration credentials, scheduled
rotation, and the revocation/audit runbook are explicitly out of scope; see
"What this runbook does not cover" below.

**Status: the delivery chain below is the intended path, not a working one.**
Helivanta has no Dockerfile, chart, or ArgoCD app yet (owned by
[#824](https://github.com/tesserix/helivanta/issues/824)), so nothing in this
runbook can be *delivered* to a running pod today. `SecretStore/openbao-helivanta-api`
already exists in the `helivanta` namespace on the production cluster and reports
`Valid`/`Ready=True`, but that status is **not proof the grant works** — the
`read-helivanta` OpenBao policy and the `app-helivanta_helivanta-api` auth role it depends on
have not been created (they are minted from the secret-service console, and
that step has not happened). Whether the grant actually works is settled by
a real read under it, which is a separate, not-yet-done task (see D7).

## Inventory

| Env var | OpenBao path | What it authorises | Blast radius if leaked |
|---|---|---|---|
| `SESSION_SIGNING_KEY` | `kv/data/helivanta/helivanta-api/session-signing-key` | Signs and verifies every Helivanta session token (Ed25519 seed) | Credential forgery — holder can mint valid sessions for any user, indistinguishable from a real login, until the key is rotated |
| `ZITADEL_LOGIN_CLIENT_TOKEN` | `kv/data/helivanta/helivanta-api/zitadel-login-client-token` | PAT for Helivanta's `IAM_LOGIN_CLIENT` machine user; lets Helivanta's own login form check credentials and finalise sign-ins against production Zitadel | Instance-level (D2): a leaked PAT can finalise an OIDC auth request for **any** app on the shared Zitadel instance, not just Helivanta. Helivanta holding its own machine user buys independent revocation, attribution, and rotation — it does not narrow what the credential can do once read |
| `HELIVANTA_WEB_ORIGIN` | **not in OpenBao — see below** | The public origin Helivanta's own frontend is served from | N/A — see below |

**`HELIVANTA_WEB_ORIGIN` is config, not a secret (D1).** It refuses boot when unset,
which is why it is easy to mistake for one, but it is a public origin string —
visible in every browser address bar — not a confidential value. It belongs
in the deployment environment (#824), not under `kv/data/helivanta/*`. It is
recorded in this table only so the next reader who sees it refuse boot does
not file it into OpenBao alongside the two real secrets: everything under
`kv/data/helivanta/*` being a credential is the property that keeps that path easy
to reason about, and mixing in a config string would quietly weaken it.

## Delivery chain (D4)

```
OpenBao → ESO (as the helivanta-api ServiceAccount) → k8s Secret → env → config.Load
```

Nothing after ESO knows what a vault is — Helivanta keeps reading `os.Getenv`,
unchanged. Switching secret-store backends (the open fleet decision in
[tesserix-k8s#208](https://github.com/tesserix/tesserix-k8s/issues/208)) is a
`ClusterSecretStore` edit, not a Helivanta code change.

## Provisioning: `SESSION_SIGNING_KEY`

```bash
make secret-session-key            # prints the key, nothing else
```

Verified: run on this checkout, prints a single line, 44 characters, that
decodes (`base64 -d | wc -c`) to exactly 32 bytes — the base64-standard-
encoded Ed25519 seed `SessionSigningKeySeed` accepts (D6). Do not generate
this with `openssl` or any other tool — the guard in
`backend/internal/config/signingkey.go` will refuse a PEM block or a wrong-
length value at boot, against the shared production instance, which is the
worst place to debug a base64 length mismatch.

> **Handle the printed value like a live credential — because it is one.**
> `make secret-session-key` prints the key to your terminal. From the moment
> it renders on screen it can end up in shell history, terminal scrollback
> you later paste somewhere, a log file, a CI job's captured output, or a
> chat/AI transcript — none of which are places a session-signing key
> belongs.
>
> Prefer piping it so it never renders at all:
> ```bash
> make secret-session-key | pbcopy                # macOS — copies, doesn't print
> ```
> or pipe it directly into the console client you're writing it with, instead
> of printing it and retyping/pasting it by hand.
>
> **If it leaks anyway:** regenerate and discard the old one. This is cheap
> right now — nothing has been provisioned yet, so there is no live value to
> invalidate. Once this key is provisioned, discarding a leaked value costs a
> pod restart (rotation is restart-only, per D4 above). Do the cheap thing
> while it's still cheap.

Then write the printed value to `kv/data/helivanta/helivanta-api/session-signing-key` via the
secret-service console at `secret-service.tesserix.app` (create/update only —
by design the console is not meant to hold `read` on `kv/data`, so writing a
value there is not meant to create a way to read it back; this is D7
assertion (3) and **has still not been independently verified** — the console's
own UI asserts it, which is not the same as having tested it. Treat it as
design intent, not a proven property). This write-back step is Task 4 and has not been
executed as of this writing.

## The grant, and what has actually been proven about it

**The path shape is not free — it is dictated by the store.** The `openbao`
chart generates one policy per whitelisted app, scoped to
`kv/data/<namespace>/<app-name>/*`, and names the policy and Kubernetes auth
role `app-<namespace>_<app-name>`. Helivanta's only whitelisted app is
`helivanta-api`, so **the sole readable prefix is
`kv/data/helivanta/helivanta-api/*`**. The app segment cannot be chosen freely:
a secret written to `kv/data/helivanta/api/...` or `kv/data/helivanta/postgres/...`
is invisible to the pod.

**This runbook gave the wrong paths until 2026-08-18.** They read
`kv/data/helivanta/api/*`. Following it exactly would have written both boot
secrets where nothing can read them — and nothing would have failed at write
time, because the console accepts any path. The first symptom would have been an
opaque External Secrets permission error during deployment, with the runbook
apparently followed correctly.

In the `secret-service` console the three fields map to the path as
**Namespace** / **Apps** / **Secret name**, so the values are `helivanta`,
`helivanta-api`, and the secret's own name. Selecting the existing
`helivanta-api` app needs no whitelisting pull request; if the console offers to
open one, the app segment is wrong.

Two of D7's three assertions were proven during #824 slice 1a, using the
Postgres role passwords rather than these boot secrets:

1. **A read under the grant succeeds** — three ExternalSecrets reached
   `SecretSynced` with real values projected. ✅
2. **A read outside the grant is refused** — `kv/data/helivanta/scope-probe/denial`
   returned `403 permission denied`. The path *existed* (a disposable canary was
   written there and its whitelisting PR deliberately left unmerged), so the
   refusal is a policy decision and not a 404. ✅ This is the assertion that can
   silently be wrong: an over-broad policy passes every positive test
   identically.
3. **The console cannot read values back** — still unverified. ❌

One further trap, found the same way: an ExternalSecret's `remoteRef` needs
`property: <key>` as well as `key: <path>`. Without it, ESO returns the entire
KV payload, so the projected value is `{"password":"..."}` rather than the
secret. That is non-empty and reports `SecretSynced`, so a presence check passes
while the credential is unusable.

## Provisioning: `ZITADEL_LOGIN_CLIENT_TOKEN`

This value comes from a **machine user created on production Zitadel**
(`auth.tesserix.app`), specific to Helivanta. It is a one-time, human-executed act
against a live shared instance (D6) — deliberately not automated, because
automating a step that mints instance-level credentials would create a
second thing able to do so.

1. Confirm the open item in D2 first: whether creating a machine user on the
   shared production Zitadel instance needs the platform team's sign-off.
   This spec's position is that provisioning one is Helivanta's call because it is
   additive and affects no other product's configuration — but confirm
   before executing, not after.
2. In the Zitadel console for the instance backing `auth.tesserix.app`,
   create a new machine user scoped to Helivanta (e.g. `helivanta-login-client`), not a
   shared one.
3. Grant it the `IAM_LOGIN_CLIENT` role at the instance level. Zitadel offers
   no narrower role — see the residual risk below before proceeding.
4. Generate a personal access token (PAT) for that machine user.
5. Write the PAT to `kv/data/helivanta/helivanta-api/zitadel-login-client-token` via the
   secret-service console at `secret-service.tesserix.app`, the same way as
   the session signing key above.

**Not run.** Steps 2–4 require admin credentials on a live shared production
Zitadel instance and were not executed as part of this task — creating a
machine user is a privileged, one-time act on a system this task must not
touch. They are recorded as the procedure to follow, not as something
verified to work as written.

### Residual risk (D2) — read before provisioning

> The `IAM_LOGIN_CLIENT` role is **instance-level**. A holder can finalise an
> OIDC auth request for any app on this Zitadel instance, including other
> Tesserix products. Helivanta having its own machine user buys independent
> revocation, attribution, and independent rotation — it does **not** narrow
> what the credential can do once read. Zitadel offers no narrower role.
> Storing it under a Helivanta path bounds who can read it, not what it can do.

In plain terms: giving Helivanta its own machine user is worth doing — it means
revoking Helivanta's PAT does not sign out every other Tesserix product's users,
and Zitadel's audit trail can attribute a given finalised auth request to
Helivanta specifically. But if Helivanta's PAT leaks, the blast radius is exactly the
same as if the platform-wide PAT leaked: the holder can finalise a login for
any app on the instance. Path-scoping this secret under `kv/data/helivanta/*`
(D3) controls who inside Tesserix can *read* it; it does nothing to shrink
what it can *do* once someone has it. This residual is accepted, not closed
— closing it needs a capability Zitadel does not currently offer.

## Rotation

Rotating either secret today requires a **pod restart**: both are read once
at boot via `os.Getenv` (D4), and Helivanta keeps no live connection to OpenBao
that could pick up a changed value without one. This is accepted for these
two boot secrets — a restart is cheap and the values change rarely — and is
explicitly **not** the model for the per-tenant integration credentials
#45 also describes, which need rotation without downtime and are why that
work is a separate, not-yet-built slice.

## What this runbook does not cover

- **Per-tenant integration credentials** — the write-only API, the metadata
  table, tenant-scoped OpenBao paths. Depends on a live backend API from Go
  and, transitively, on #208. Remaining scope of #45.
- **Scheduled/automatic rotation** — dual-credential overlap, orchestrated
  rotation. Retrofittable per D6 (`session.Signer` already stamps `kid` into
  every token; supporting a second key later needs no token-format
  migration), but not built. Remaining scope of #45.
- **Emergency revocation runbook and the `SecretRevoked` audit trail** —
  belongs with [#54](https://github.com/tesserix/helivanta/issues/54), which has
  its own unresolved audit-identity-key decision.

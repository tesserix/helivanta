# Helivanta's boot secrets live in OpenBao, and nothing in Helivanta knows that

**Issue:** [#45](https://github.com/tesserix/helivanta/issues/45)
**Scopes down:** #45 as filed. This slice covers Helivanta's **boot secrets** only.
Per-tenant integration credentials, Temporal-orchestrated rotation, the
metadata table and the credential API are explicitly **not** in it — see
"Out of scope".
**Corrects:** #45's `hms-in/{env}/{scope}/{name}` naming convention (D3), which
was written for GCP Secret Manager and is incompatible with the store the fleet
actually runs.
**Depends on:** [#824](https://github.com/tesserix/helivanta/issues/824) (container
images and deployment artefacts) for delivery. Nothing here can be *delivered*
until Helivanta is deployable.
**Adjacent:** [#713](https://github.com/tesserix/helivanta/issues/713) owns the broad
security-scanning pipeline; D5 takes only the secret-scanning gate.
**Fleet decision:** [tesserix/tesserix-k8s#208](https://github.com/tesserix/tesserix-k8s/issues/208)
(OpenBao vs GCP Secret Manager) is still open. **D4 is what makes that
irrelevant to Helivanta.**

Helivanta refuses to boot without `SESSION_SIGNING_KEY` and
`ZITADEL_LOGIN_CLIENT_TOKEN`, and only developer-machine paths exist for
either. This spec gives them a production home, a way to be provisioned, and a
gate that stops them re-entering source control — without adding a single line
of secret-store code to Helivanta.

---

## Observed state, 2026-08-17

Recorded because the design rests on it, and because two of these contradict
what the handoff assumes. Each was checked live, not read from documentation.

| Claim | How it was checked |
|---|---|
| OpenBao is live and healthy in `tesseract-prod-in-gke` | `kubectl get pods -n openbao`: `openbao-0/1/2` all `1/1 Running`, bootstrap Job `Completed` |
| Its per-app pattern works today | `openbao-secret-store` (ClusterSecretStore) plus four namespaced SecretStores — `homechef-api`, `qdrant`, two `cloudflared` — all `Valid`/`Ready` |
| A production Zitadel exists | `zitadel` namespace: 3 replicas `Running`, a `zitadel-login` (Login V2) deployment, its own ingress gateway, a bootstrap CronJob reconciling every ~30 min |
| **There is no dev cluster** | The `tesseract-devtest-gke` kubeconfig context is stale: `dial tcp 34.151.129.108:443: connect: network is unreachable`. Confirmed by the product owner: prod only. |
| **Helivanta is not deployed anywhere** | No Dockerfile in this repo; no chart, ArgoCD app, namespace or `namespaceWhitelist` entry for Helivanta in `tesserix-k8s` |
| Helivanta's dev PATs are not in git | `git check-ignore` resolves `dev/zitadel/secrets/` from `.gitignore:21`; `git ls-files dev/` lists only `init-db.sql` and `Caddyfile` |
| Both boot guards already refuse correctly | `config.SessionSigningKeySeed` refuses absent, malformed, and the committed dev key outside `HELIVANTA_ENV=dev`; `config.RequireZitadelLoginClientToken` refuses absent |

The last row is why this spec adds **no boot enforcement**: it already exists
and is correct. What does not exist is where the values come from.

The "no dev cluster" row is the one that shapes D7 most: every claim this spec
makes about OpenBao must be proven against the **production** store, because
there is no other one.

---

## D1 — Two secrets, not three

The handoff counts three boot-refusing values. Only two are **secrets**:

| Value | Kind | Home |
|---|---|---|
| `SESSION_SIGNING_KEY` | secret — mints every session; a leak is credential forgery | OpenBao |
| `ZITADEL_LOGIN_CLIENT_TOKEN` | secret — instance-level; see D2 | OpenBao |
| `HELIVANTA_WEB_ORIGIN` | **config** — a public origin string | deployment env (#824) |

`HELIVANTA_WEB_ORIGIN` refuses boot when unset, which is why it reads like a secret,
but refusing to boot without a value and *being confidential* are different
properties. It is the origin Helivanta's own frontend is served from — visible in
every browser address bar. Filing it as a secret would add a rotation surface
and an access grant for a public string, and would quietly weaken the property
that makes `kv/data/helivanta/*` easy to reason about: **everything in there is a
credential**. A path that mixes credentials with configuration is one a reviewer
stops reading carefully.

## D2 — Helivanta provisions its own login-client machine user

`IAM_LOGIN_CLIENT` is an **instance-level** Zitadel role. A holder can finalise
an OIDC auth request for any app on the instance, including other Tesserix
products. The spike established Zitadel offers nothing narrower.

Helivanta therefore gets its **own** machine user with its own PAT, rather than
sharing one with other products on the instance.

**What this buys, stated precisely:**

- **Independent revocation.** Revoking Helivanta's PAT after a suspected leak does not
  sign every other Tesserix product's users out.
- **Attribution.** Zitadel's audit trail names which product's credential
  finalised a given auth request. With a shared PAT that question has no answer.
- **Independent rotation.** Helivanta can rotate on its own schedule without a
  cross-product coordination window.

**What this does NOT buy, and must never be described as buying it.** The role
is still instance-level. A leaked Helivanta PAT can still finalise an auth request for
any product on the instance. Path scoping (D3) bounds *who can read the secret*;
it does nothing to bound *what the secret can do once read*. Those are different
guarantees and conflating them is how a system acquires a control that is
decorative.

The residual is accepted, not closed, and the reason is that closing it needs a
capability Zitadel does not have. It is recorded at the decision point in code
and in the provisioning runbook, so the next person to read either learns it
without having to re-derive it from Zitadel's role model.

**Open item.** Whether creating a machine user on the *shared* production
instance needs the platform team's sign-off is unresolved. This spec's position:
provisioning one is Helivanta's call, because it is additive and affects no other
product's configuration — whereas flipping `loginV2.required` is explicitly not
Helivanta's call, per the login-client spec's D1 and the upstream issue behind it.
Confirm before the runbook is executed, not after.

## D3 — The path convention follows the live fleet model

```
kv/data/helivanta/helivanta-api/session-signing-key
kv/data/helivanta/helivanta-api/zitadel-login-client-token

policy  app-helivanta_helivanta-api  → read on kv/data/helivanta/helivanta-api/*
                                     → read+list on kv/metadata/helivanta/helivanta-api/*
role    app-helivanta_helivanta-api  → ServiceAccount helivanta-api in namespace helivanta
store   SecretStore openbao-helivanta-api in namespace helivanta
```

**Corrected 2026-08-18, after the grant was exercised for the first time
during #824 slice 1a.** This block originally gave the paths as
`kv/data/helivanta/api/*` and the policy as `read-hms` granting
`kv/data/helivanta/*`. **All three were wrong**, and the error was invisible
because nothing had ever read the path.

The `openbao` chart does not take a hand-written policy. It generates one per
whitelisted app from `namespaceWhitelist`, scoped to
`kv/data/<namespace>/<app-name>/*`
(`charts/thirdparty/openbao/templates/bootstrap-configmap.yaml`), and names both
the policy and the Kubernetes auth role `app-<namespace>_<app-name>`. The only
app whitelisted for this namespace is `helivanta-api`, and the live
`SecretStore/openbao-helivanta-api` authenticates with role
`app-helivanta_helivanta-api` — so the sole readable prefix is
`kv/data/helivanta/helivanta-api/*`. The string `helivanta` appears nowhere else
in the chart's policy block; there is no broader grant to fall back on.

**Consequence had this not been caught:** secrets written per the old paths
would have been unreadable by the pod. Nothing would have failed at write time
— the console accepts any path — and the failure would have surfaced during
deployment as an opaque External Secrets permission error, with the runbook
appearing to have been followed correctly.

**Proven, not inferred:** a read under the grant succeeds (three ExternalSecrets
`SecretSynced` with real values), and a read of `kv/data/helivanta/scope-probe/denial`
— a path that exists, whose whitelisting PR was deliberately left unmerged — is
refused with `403 permission denied`. The denial is a policy decision rather
than a 404, which is the distinction D7 insists on.

This **supersedes #45's `hms-in/{env}/{scope}/{name}`**. That convention was
written when GCP Secret Manager was assumed, where a name is a flat string and
the slashes are decoration. Under OpenBao they are not decoration: the leading
path segment is the thing a policy grants on, so `kv/data/helivanta/*` is what makes a
secret Helivanta's and nothing else's. Carrying #45's shape across would put `hms-in`
in the policy-bearing position and `{env}` where the app segment belongs,
breaking the correspondence every other app in the fleet follows
(`kv/data/homechef/api/db`, `kv/data/ai-database/...`).

**No `{env}` segment, deliberately.** The fleet separates environments by
cluster and namespace, and there is exactly one cluster (observed above). A
future staging environment becomes an `hms-staging` namespace with its own
policy and its own role — *not* a `/staging/` segment inserted mid-path.
Recorded explicitly because inserting an env segment later would silently
invalidate every policy prefix at once, and the failure mode is a policy that
matches nothing while still applying cleanly.

## D4 — No secret-store code enters Helivanta

The delivery chain is:

```
OpenBao  →  ESO (as the hms-api ServiceAccount)  →  k8s Secret  →  env  →  config.Load
```

Nothing after ESO knows what a vault is. Helivanta keeps reading `os.Getenv`, exactly
as it does today. **No vendor SDK, no `secrets` package, no resolution layer.**

This is the decision that makes the slice backend-agnostic *in fact* rather than
as a claim. #208 may still choose GCP Secret Manager fleet-wide; if it does,
Helivanta changes nothing, because switching backends is a `ClusterSecretStore` edit.
An in-process resolution abstraction would invert that: it would be the one
place a backend choice *did* reach Helivanta's code, while being justified as the
thing that prevents it.

It is also the YAGNI call. A resolution layer with one backend and one consumer
is speculative structure. [#677](https://github.com/tesserix/helivanta/issues/677)
owns a config-and-secrets SDK package and can take it up when a second real case
exists — noting that #677's title also says "GCP Secret Manager" and will need
the same correction D3 applies here.

**The cost, stated:** rotation requires a pod restart, because env vars are read
once at boot. That is acceptable for this slice and is not hidden — see "Out of
scope", and D6's note on why rotation stays retrofittable.

## D5 — A gitleaks gate, with one deliberate exception

A `gitleaks` job in `.github/workflows/ci.yml`, failing the build on any
finding. Helivanta has no secret scanning today and is about to hold its most
privileged credential.

**It needed an explicit exception, and that exception is the interesting
part.** This repository *deliberately commits a real Ed25519 key* —
`config.DevSessionSigningKey`. The scanner was run (`zricethezav/gitleaks`,
2026-08-17, 309 commits) and it **does** flag it, along with the PHI-redaction
test fixtures in `pkg/logging` — 9 findings total, all benign. The exception
is encoded in `.gitleaks.toml` as a *value*-scoped allowlist on the specific
finding regexes, never a rule/path allowlist: each entry is anchored to the
exact known-benign literal gitleaks extracted, so nothing else committed to
those same files or lines is exempted.

The reason to be strict about how the exception is expressed is in the handoff:
GitGuardian fires on the *word* "password" in field names and comments and
produced 11 false positives on a single PR. **A scanner that cries wolf is a
scanner nobody reads, and a real leak looks exactly like the noise.** A gate
whose exceptions are broad is worse than no gate, because it produces the
paperwork of control without the control.

Scope boundary: this is the secret-scanning gate only. SAST, dependency and
container scanning stay with #713.

## D6 — Provisioning is tooling, not prose

A make target mints the Ed25519 seed in exactly the format
`SessionSigningKeySeed` accepts — base64-standard-encoded 32-byte seed, not PEM,
not the 64-byte expanded key.

A runbook step reading "generate a key with openssl" is how an operator produces
a PEM block or a 31-byte value. That failure does not surface at generation
time; it surfaces as a boot refusal at deploy time, against the shared
production instance, at the moment an operator is least able to debug a base64
length mismatch. The guard in `signingkey.go` catches it — catching it is not
the same as it being cheap.

The Zitadel machine-user creation stays a runbook rather than tooling: it is a
one-time act against a live instance, it requires a human decision (D2's open
item), and automating a one-time privileged provisioning step would create a
second thing able to mint instance-level credentials.

**Rotation is out of scope but stays retrofittable, and this was checked rather
than assumed.** `session.Signer` already stamps `kid` into every token header
and `session.Verifier` already reads it. Supporting a second key later is a pure
code change — a verifier accepting a set of kids — with no token-format
migration and no invalidation of tokens minted today. This is worth stating
because the comparable question for [#54](https://github.com/tesserix/helivanta/issues/54)
(audit-trail identity) genuinely *is* decided-now-or-never; this one is not, and
a reader who has internalised #54's constraint would reasonably assume it
applies here too.

## D7 — What gets proven, and against what

Go side: in-repo tests, no OpenBao required — that is D4 working as intended.

OpenBao side, three assertions against the **production** store, because
(observed above) there is no other one:

1. A read of `kv/data/helivanta/helivanta-api/*` under Helivanta's grant **succeeds**.
2. A read of another namespace's path (e.g. `kv/data/homechef/*`) under Helivanta's
   grant is **denied**.
3. The `secret-service` console's policy **cannot read** Helivanta's values — it holds
   `create`/`update`/`delete` on `kv/data/*` and metadata access, deliberately no
   `read`.

**(2) is the assertion that matters and the one that can silently be wrong.** An
over-broad policy behaves identically to a correct one on every positive test;
only the denial distinguishes them. (1) failing is loud and self-announcing; (2)
failing is invisible until someone reads another namespace's secrets.

Per this repo's practice, each assertion is **proven able to fail** before it is
trusted — and the proof must move a value the assertion actually reads. For (2)
that means demonstrating the read succeeds under a deliberately widened policy
and is refused under the real one, not merely observing a denial that a typo in
the path would have produced identically. A denial from a malformed path is not
evidence of a working policy.

### Production changes this requires

All additive; nothing existing is modified. Each needs explicit confirmation
before it is executed, and each is separately reversible:

- an `hms` namespace and `hms-api` ServiceAccount (#824 needs both regardless)
- a `read-hms` policy and an `app-hms_hms-api` Kubernetes auth role
- a `namespaceWhitelist` entry, by pull request to `tesserix-k8s`
- Helivanta's two secret values written into OpenBao
- a Helivanta login-client machine user on the production Zitadel (D2's open item)

---

## Errors and failure handling

- **A secret is absent at boot.** Unchanged: the process refuses to start, with
  the existing messages naming the variable. This is the intended failure and it
  is loud, at the deploy, where an operator is already looking.
- **ESO cannot reach OpenBao.** The projected Kubernetes Secret retains its last
  value; a pod that is already running is unaffected. A pod starting fresh finds
  no Secret and refuses to boot — correct, and preferable to booting with a
  stale or empty credential.
- **The grant is wrong or missing.** The SecretStore reports `Ready: False` and
  no Secret is projected, so the failure is visible in the store's status before
  any pod consumes it.
- **A secret value is wrong** (a valid-looking but incorrect PAT). Not
  detectable at boot: `RequireZitadelLoginClientToken` checks presence, not
  authenticity. It surfaces as Zitadel rejecting the login-client calls. Stated
  rather than fixed — verifying a PAT at boot means a network call in the boot
  path, which trades a clear startup failure for a new startup dependency.
- **A secret must never be logged.** Not newly enforced here.
  [#840](https://github.com/tesserix/helivanta/issues/840) records that log redaction
  currently screens no secrets, which means this property rests on the
  convention that no call site formats these values — the login-client code is
  written that way today. Naming it here so it is not mistaken for something
  this slice closed.

## Testing

- **Go:** the existing boot-guard tests already cover absent, malformed and
  dev-key-outside-dev. This slice adds no boot enforcement, so it adds no tests
  there — a new test asserting behaviour that already has coverage would be
  padding.
- **Provisioning tooling:** the generated seed is asserted to be accepted by
  `SessionSigningKeySeed` — the real function, not a reimplementation of its
  rules. A test that re-derives "base64, 32 bytes" independently would pass
  after the real validator diverged.
- **gitleaks gate:** proven to fail. A commit containing a synthetic
  credential must be observed being rejected, and the committed dev key must be
  observed *not* being flagged — whether that takes an allowlist entry or is
  already true is D5's open question. A gate nobody has watched reject anything
  is not known to be a gate.
- **OpenBao:** D7's three assertions, each proven able to fail.

## Risks

- **Every proof runs against production.** There is no dev cluster. Mitigated by
  making every change additive and separately reversible, and by confirming each
  before execution — but the risk is real and is the direct consequence of the
  infrastructure as it stands.
- **D2's residual.** A leaked Helivanta PAT is still instance-wide. Not closable with
  Zitadel's current role model.
- **Rotation needs a pod restart** (D4). Acceptable for two boot secrets;
  it would not be acceptable for the per-tenant credentials #45 also describes,
  which is part of why they are a separate slice.
- **D3 diverges from #45's written convention.** Anyone reading the issue rather
  than this spec will find a different naming scheme. The issue should be
  updated to point here.

## Out of scope

- **Delivery** — the chart, image and ArgoCD app: #824.
- **Per-tenant integration credentials** — the write-only API, the metadata
  table, the fingerprint read-back, tenant-scoped paths. These need a live
  backend API from Go and so genuinely depend on #208; that dependency is the
  reason they are not here.
- **Rotation** — scheduled rotation, dual-credential overlap, Temporal
  orchestration. Retrofittable (D6).
- **Broader security scanning** — SAST, dependencies, containers: #713.
- **The manifest lint** rejecting literal `stringData`: it has nothing to lint
  until #824 creates manifests.
- **Emergency revocation runbook and `SecretRevoked` audit trail** — the audit
  half belongs with #54, which has its own unresolved identity-key decision.

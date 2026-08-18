# Helivanta becomes deployable, and the rate limiter stops guessing who called

**Issue:** [#824](https://github.com/tesserix/helivanta/issues/824)
**Unblocks:** `TRUSTED_PROXY_CIDRS` (nowhere to be set), the unconfirmed XFF
assumption (no pod to confirm it against), and
[#45](https://github.com/tesserix/helivanta/issues/45) Task 4 — though see D8,
which corrects the ordering the handoff assumes.
**Scopes down:** #824 as filed. This spec covers slices **1a** and **1b**: the
platform dependencies, then the API and shell images with a real sign-in. The
three product zones are slice 2.
**Confirms:** ADR-0001's split — "Deployment manifests live in tesserix-k8s,
not here" — so the artefact-location question #824 raises is already decided,
not open.
**Adjacent:** [#7](https://github.com/tesserix/helivanta/issues/7) (GitOps,
Kyverno admission), [#709](https://github.com/tesserix/helivanta/issues/709)
(CI templates), [#715](https://github.com/tesserix/helivanta/issues/715)
(preview environments), [#435](https://github.com/tesserix/helivanta/issues/435)
(supply chain), [#713](https://github.com/tesserix/helivanta/issues/713)
(security scanning).

Helivanta has no Dockerfile, no chart and no ArgoCD application. Every IP-keyed
rate limit therefore runs in a coarse one-bucket-per-ingress mode, because the
value that would fix it has nowhere to live. This spec gives Helivanta a
deployed home and closes that gap.

---

## Observed state, 2026-08-17

Each checked live. Four of these contradict what #824 or the handoff assumes.

| Claim | How it was checked |
|---|---|
| Helivanta has no build or deployment artefacts | No `Dockerfile`, `charts/` or ArgoCD app in this repo; `grep -rln helivanta` across `tesserix-k8s` returns only `charts/thirdparty/openbao/values.yaml` and `argocd/prod/projects/security.yaml` |
| The `helivanta` namespace holds exactly one object | `kubectl get all,externalsecret,secretstore -n helivanta` → only `SecretStore/openbao-helivanta-api`, `Valid`/`Ready` |
| **Nothing verifies image signatures** | `kubectl get clusterpolicy` on `tesseract-prod-in-gke` → only `tig-audit-breakglass`, `tig-deny-unauthorized-delete`, `tig-deny-unauthorized-writes`. No `verifyImages` policy exists; Kyverno policies live in the separate `tesserix-governance` repo |
| Images are pushed to ghcr.io and pulled through a GAR mirror | `charts/apps/homechef-api/values.yaml`: `asia-south1-docker.pkg.dev/tesseracthub-480811/ghcr-remote/tesserix/home-chef-app/homechef-api` |
| **The API hard-requires NATS and OpenFGA at boot** | `backend/cmd/api/main.go:171` (`events.NewBus`), `:214` (`authz.NewClient`); `/readyz` gates on both (`:239-240`) |
| **Next bakes rewrites into the build** | `apps/shell/.next/routes-manifest.json` contains the literal `http://localhost:4302/medicore`. `MEDICORE_URL`/`API_URL` are build-time, not runtime |
| `NEXT_PUBLIC_*` are inlined and the build refuses without them | `apps/shell/lib/env.ts` — `defineEnv` with `z.string().url()` / `.min(1)`, and its own comment: "NEXT_PUBLIC_ vars are inlined at build time" |
| SIGTERM is already handled | `main.go:136` `signal.NotifyContext(…SIGINT, SIGTERM)`; `:419` `httpSrv.Shutdown` |
| `/readyz` already runs real checks | `internal/httpserver/server.go:75-84` iterates `readyChecks` and returns 503 naming the failed one. Not a static 200 |
| `migrate` depends only on Postgres | `backend/cmd/migrate/main.go` header — written so seeding and CI need not boot NATS and OpenFGA |
| **`helivanta.app` resolves to nothing** | `dig helivanta.app NS` → `algin/nola.ns.cloudflare.com` (same zone account as `tesserix.app`), but `dig A` is empty, and `k8s/cluster/cloudflared/configmap.yaml` routes only `tesserix.app`, `*.tesserix.app`, `mark8ly.com`, `*.mark8ly.com`, then `http_status:404` |
| A precise precedent exists, 18 days old | `argocd/prod/apps/dwellm8/` — postgres (CNPG), nats, openfga, db-schema-bootstrap, secrets, network-policies, api, temporal, under an app-of-apps |

The "nothing verifies signatures" row is why D5 says what it says. The
`routes-manifest.json` row is why D4 exists at all.

---

## D1 — Three slices, cut on dependency edges

| Slice | Repo | Delivers |
|---|---|---|
| **1a** | `tesserix-k8s` only | CNPG Postgres, NATS, OpenFGA, db-schema-bootstrap, network policies, app-of-apps |
| **1b** | `helivanta` + `tesserix-k8s` | Dockerfiles, CI, charts, Kargo, cloudflared/DNS, the D7 boot guard — **a real sign-in** |
| **2** | both | `medicore`, `pharmacy`, `lab` zones |

The 1a/1b cut is not arbitrary. It falls on a repo boundary *and* on a real
dependency edge: the API cannot become ready until all three of its
dependencies are healthy (observed above), so standing them up first means the
first Helivanta pod that fails to start has **one** candidate cause rather than
nine — image, chart, ingress, secret, network policy, CNPG bootstrap, the
`hms_app` role, NATS, OpenFGA.

Slice 2 is cheap precisely because 1b settles the pattern: the three zone
Dockerfiles are structural repeats of the shell's, differing only in `basePath`
and port.

## D2 — The build structure is HomeChef's, without its carry-forward machinery

`homechef-api-build.yml` is the fleet's mature shape and is adopted almost
verbatim: `ghcr.io/tesserix/helivanta/{helivanta-api,helivanta-shell}`, tags
`main-<sha7>`, **no `:latest`**, buildx registry cache, and a Trivy gate that
`load`s the image locally on pull requests and fails on a fixable
CRITICAL/HIGH before merge, uploading SARIF only on pushes.

`console` (`tesserix-home`) was rejected as the model. It pins `tag: "latest"`
in `charts/apps/console/values.yaml`, which is exactly what HomeChef's own
comment forbids, and it means what runs is not what any release tag names.

**What is deliberately NOT adopted: path filters and an `ensure-image-tags`
equivalent.** HomeChef path-filters its builds, which means a commit touching
only one service leaves the others without a `main-<sha>` tag — so it needs a
carry-forward workflow to re-tag unchanged images before advancing the `deploy`
branch. That workflow's own header documents three separate ways it has wedged
production: a GAR pull-through mirror caching a `tag→digest` mapping for an
image that was then re-pointed, a `cancel-in-progress: false` concurrency
deadlock that silently stopped advancing `deploy` with nothing failing to say
so, and negative-cached 404s for not-yet-pushed tags.

Helivanta builds **every** image on **every** main commit instead. That deletes
the entire failure class rather than managing it. It is affordable here and
would not be at HomeChef's scale: ADR-0001 states one PR spans frontend and
backend, so most commits would rebuild both regardless, and slice 1b has two
images.

Promotion is Kargo via the `deploy` branch and the git commit graph — the same
model, and for the same recorded reason: image-tag discovery broke at HomeChef
because Artifact Registry paginates tag lists at 100, so `NewestBuild` promoted
a stale page and flapped production backwards.

## D3 — Istio owns path routing; the shell never proxies a sibling zone

In production the ingress routes `/api/*` to `helivanta-api` (stripping the
`/api` prefix, because the backend serves `/v1/*`) and everything else to
`helivanta-shell`. Slice 2 adds `/medicore`, `/pharmacy` and `/lab` to the
zone Services directly.

The alternative — ingress sends everything to the shell, which forwards to
zones exactly as it does locally — was rejected on availability. It puts a
Next.js hop in front of every zone request and makes the shell a single point
of failure for all four products: a shell rollout would take pharmacy and lab
down with it. Local dev keeps its rewrites; production simply never reaches
them.

**The consequence must be stated, because it is a live trap.** The shell image
carries baked `localhost` rewrites that are unreachable in production only
because the ingress routes those paths away first. If ingress path
configuration ever drifts, a request falls through to the shell and it attempts
to proxy to `localhost:4302` — producing a confusing 502 rather than a clean
404. Zone URLs are therefore built as in-cluster Service DNS
(`http://helivanta-medicore.helivanta.svc.cluster.local:4302`), so the fallback
path works rather than lying. This costs nothing and removes the trap.

## D4 — The shell image is environment-specific, and that is recorded rather than engineered around

`NEXT_PUBLIC_ZITADEL_ISSUER_URL` and `NEXT_PUBLIC_ZITADEL_CLIENT_ID` are
inlined into the client bundle at build time and `lib/env.ts` refuses to build
without them. They are supplied as CI build arguments. **One image is therefore
valid for exactly one environment.**

This blocks #7's "promotion by digest" and #715's preview environments, and it
is named here so neither issue discovers it. It is not fixed here for the same
reason #45's D4 refused a secret-resolution layer: there is exactly one
environment — the handoff and #45's own observed-state table record that the
`tesseract-devtest-gke` context is stale and production is the only cluster —
so a runtime-config indirection would be speculative structure serving a second
environment that does not exist.

The alternative considered was a runtime config endpoint: the shell fetches the
issuer and client ID from the API before starting OIDC, making one image valid
everywhere. It was rejected as premature, and there is a second cost worth
recording — it moves a security-relevant value onto a network path that must
then be trusted, in exchange for a property nothing currently needs.

**When a second environment appears, this is the decision to revisit first.**

## D5 — Sign and generate an SBOM; claim nothing about either

Cosign keyless signing (GitHub OIDC) and an SBOM at build time, attached to
every pushed image.

**Nothing verifies either, and both the spec and #7 say so explicitly.** The
observed-state table records that the production cluster runs three TIG
governance policies and no `verifyImages` policy at all. Helivanta would be the
first signed image in the fleet — `grep -rli cosign` across `tesserix-k8s`
returns nothing.

Signing anyway is justified by one asymmetry, not by the appearance of rigour:
a signature binds to a digest, so an image promoted unsigned cannot be signed
retroactively — it must be rebuilt, and a rebuilt digest is a **different
image** than the one that was tested and promoted. Deferring costs a rebuild of
every running digest the day #7 lands.

The risk this takes on is the one this repository keeps meeting: a control that
reads as enforced while enforcing nothing. It is mitigated only by naming it —
in this spec, in the workflow comment at the signing step, and in a comment on
#7. A reader who finds a Cosign signature and infers admission control is
protecting them would be wrong, and nothing structural prevents that inference.

## D6 — `helivanta.app` from the start, at the cost of touching a shared file

The zone already exists in the same Cloudflare account (observed above), so
this is additive configuration, not a registration: `helivanta.app` and
`*.helivanta.app` are added to `k8s/cluster/cloudflared/configmap.yaml`
alongside the existing `tesserix.app` and `mark8ly.com` entries, with a DNS
record pointing at the tunnel.

`helivanta.tesserix.app` was the alternative — it needs no DNS work at all,
resolving through the existing `*.tesserix.app` wildcard — and was rejected
because it means setting `HELIVANTA_WEB_ORIGIN` and the Zitadel redirect URIs
twice, the second time as a live reconfiguration once real sign-ins exist.

**The accepted risk is that the cloudflared configmap is fleet-wide.** A
mistake there affects `tesserix.app` and `mark8ly.com` traffic, not just
Helivanta's. The change is therefore purely additive — new hostname entries
above the existing `http_status:404` fallback, no edit to any existing rule —
and is verified by confirming the existing hostnames still resolve **after**
the change, not merely that the new one does.

## D7 — `TRUSTED_PROXY_CIDRS` refuses boot outside dev, with an explicit opt-out

| Value | `HELIVANTA_ENV=dev` | Otherwise |
|---|---|---|
| unset or empty | empty list, raw TCP peer | **refuse boot** |
| `none` | raw TCP peer | raw TCP peer |
| valid CIDR list | trusted | trusted |
| every entry malformed | empty list | **refuse boot**, message distinct from unset |

This raises the control from the repository's weakest rung to its second —
`docs/standards/engineering-principles.md` §4 ranks "impossible to express" >
"fails at boot" > "fails in CI" > "documented convention" — and matches how
`HELIVANTA_WEB_ORIGIN` already behaves.

**The guard is a separate method, not a change to `config.Load()`.**
`Load()` returns no error anywhere in this codebase; the comparable refusal is
`cfg.RequireDistinctHostedLoginOrigin()`, called from `main.go:111` and aborting
boot on its error. `cfg.RequireTrustedProxyCIDRs()` follows that shape exactly.
This is not cosmetic — it is what keeps `Load()`'s parsing behaviour, and the
four existing `TrustedProxyCIDRs` tests, unchanged.

**The reason is stronger than the ladder alone.** The handoff calls the current
coarse mode "safe, not correct". That understates it: one bucket per ingress
means `RATE_LIMIT_LOGIN_PER_MIN` is a hospital-wide budget, so any single
caller can exhaust every clinician's login allowance. That is an
unauthenticated availability attack on sign-in — the same class of harm #867
declined a per-subject lockout over, and it is reachable today from a
misconfiguration that looks completely healthy.

**The `none` sentinel is what makes the refusal honest.** Without it, an
operator whose deployment genuinely has no proxy in front — a bare-port test,
a future non-Istio topology — can satisfy the guard only by inventing a CIDR
that does not describe their network. A guard that forces a false configuration
value is worse than the convention it replaced, because the false value then
looks authoritative to the next reader. `none` is greppable, and it means the
same thing the empty list means today.

**The all-malformed case contradicts an existing deliberate test, and the
conflict is resolved rather than ignored.** `getenvCIDRList` drops malformed
entries individually with a warning and can arrive at empty from all-invalid
input. `TestTrustedProxyCIDRsAllMalformedYieldsEmptyNotError`
(`config_test.go:157-165`) asserts exactly that, and its comment says the
all-garbage case lands on the same fail-closed answer as unset "rather than a
boot failure".

D7 preserves that test's *principle* — all-garbage still resolves identically
to unset — while changing what "unset" means outside dev. Because the guard is
a separate method, `Load()` still returns an empty list and the test still
passes untouched. What must change is its **comment**, which will otherwise
assert something false the moment `RequireTrustedProxyCIDRs` exists;
`CLAUDE.md` requires superseded claims be corrected in the same change, and a
false claim in a comment about a security control is worse than no comment.

One consequence follows from the split: `Load()` has already discarded the raw
string, so the guard must read `os.Getenv("TRUSTED_PROXY_CIDRS")` itself to
tell an unset variable from one whose only entry was a typo. Without that it
cannot produce the two distinct messages this section requires, and an operator
who mistyped a CIDR would be told the variable was missing and go looking in
the wrong place.

It lands in slice 1b, in the same change as the chart that sets it, so it never
blocks a deployment on a value with nowhere to live.

## D8 — #45 Task 4 is a step *inside* 1b, not a thing 1b unblocks

The handoff and #824 both record Task 4 as blocked on this issue. That is the
wrong way round, and following it would deadlock.

The actual order is:

1. A Helivanta application and login-client machine user are provisioned on the
   **production** Zitadel. The rebrand spec observed that production Zitadel has
   no Helivanta app at all — the dev one is created by
   `scripts/lib/zitadel.mjs` at first boot. This mints the client ID D4 bakes
   into the shell image, so it must happen **before** the image is built.
2. The two boot secrets are written to `kv/data/helivanta/api/*` (#45 Task 4).
   This is a write to OpenBao; it needs no pod, only the grant that already
   exists.
3. The deployment consumes them via the existing `SecretStore`.

Only step 3 needed a deployment. Steps 1 and 2 are prerequisites of building a
working image, not consequences of having one.

**Step 1 carries #45's unresolved D2 open item** — whether creating a machine
user on the shared production instance needs the platform team's sign-off. It
must be confirmed before execution, not after, and it is on the critical path
of slice 1b.

**No secret value passes through an agent session.** Generation and write
happen in one piped step, or in the `secret-service.tesserix.app` console
directly, per the runbook.

## D9 — The database is named `helivanta`; only the RLS-bound role keeps `hms`

| Identifier | Production | Why |
|---|---|---|
| database | `helivanta` | No migration references it |
| owner role (`ADMIN_DATABASE_URL`) | `helivanta` | Named only by bootstrap SQL, which this slice writes |
| application role (`APP_DATABASE_URL`) | `hms_app` | **Unchanged** — migrations and every RLS policy name it |
| RLS predicate | `hms_tenant_visible()` | **Unchanged** — same reason |

The rebrand's D5 kept `hms_app` and `hms_tenant_visible()` because migrations
are append-only, so renaming a role means a new migration mutating roles on
live data plus every policy that names them. That argument is sound and it
binds those two identifiers. **It does not reach the database or owner name**,
which no migration mentions — `dev/init-db.sql` names the owner only in its
`ALTER DEFAULT PRIVILEGES FOR ROLE hms` clause, which this slice rewrites
anyway.

`dev/init-db.sql` and `docker-compose.dev.yml` are updated in the same slice so
dev and prod never diverge. Leaving dev on `hms` was rejected: a connection
string copied between environments would then fail in a way that looks like bad
credentials, and the handoff already records "presents as the app is broken" as
the dominant local-dev failure mode. Changing dev is free — dev databases are
disposable and `make reset` rebuilds them.

This is the only moment the name is free. There is no data and no deployment;
afterwards, renaming a database means a dump and restore against live patient
records.

**Database credentials come from OpenBao, not GCP Secret Manager.** dwellm8 —
the precedent this slice otherwise follows closely — sources every CNPG role
password from GCP Secret Manager via `gcpSecretName`. Helivanta cannot: #45's
D4 fixed OpenBao as its store and `SecretStore/openbao-helivanta-api` is
already `Valid`/`Ready` in the namespace. Role passwords therefore live under
`kv/data/helivanta/helivanta-api/postgres-{app,api,openfga}`.

**Corrected 2026-08-17, during slice 1a.** This paragraph first said the
passwords live under `kv/data/helivanta/postgres/*`, "which the existing
`read-helivanta` grant on `kv/data/helivanta/*` already covers". **No such grant
exists.** The `openbao` chart generates one policy per whitelisted app, scoped to
`kv/data/<namespace>/<app-name>/*`
(`charts/thirdparty/openbao/templates/bootstrap-configmap.yaml:166`), and the
live `SecretStore/openbao-helivanta-api` authenticates with role
`app-helivanta_helivanta-api`. The only readable prefix is therefore
`kv/data/helivanta/helivanta-api/*`; `helivanta` appears nowhere else in the
policy block. Secrets written to the original path would have synced to nothing.

**The same error is in #45's shipped runbook and in that spec's D3, and it is
worse there.** Both give the boot secrets as `kv/data/helivanta/api/session-signing-key`
and `.../api/zitadel-login-client-token`. Under the real policy the app segment
must be `helivanta-api`, not `api` — so #45's Task 4, executed exactly as
documented, writes both boot secrets where the pod cannot read them. Because
nothing consumes them until a deployment exists, the failure would first appear
during slice 1b as an opaque ESO permission error, with the runbook appearing to
have been followed correctly. `docs/runbooks/secrets.md` and that spec's D3 must
be corrected before #45 Task 4 is executed.

The general lesson is the one this repository keeps relearning: a path
convention written in prose enforces nothing, and this one disagreed with the
template that actually mints the policy for a full day without anything
noticing.

The divergence from dwellm8 is deliberate and is recorded here because a
reviewer comparing the two charts will otherwise read it as an oversight.
Fleet decision [tesserix-k8s#208](https://github.com/tesserix/tesserix-k8s/issues/208)
(OpenBao vs GCP Secret Manager) remains open; per #45's D4 that is irrelevant
to Helivanta, because switching stores is a `SecretStore` edit and no Helivanta
code knows which one is behind it.

---

## Errors and failure handling

- **A dependency is unhealthy.** `/readyz` returns 503 naming the failed check
  (`server.go:75-84`), so the pod stays out of the Service and the failing
  dependency is named in the probe response rather than inferred from logs.
- **A migration fails.** The `migrate` binary runs as a Helm `pre-upgrade` hook
  Job, so the release fails and the previous ReplicaSet keeps serving. An
  initContainer was rejected: it would crash-loop a new pod while reporting the
  failure only in container logs.
- **SIGTERM during a request.** Already handled (`main.go:136`, `:419`).
  `terminationGracePeriodSeconds` is set above the server's shutdown timeout,
  because a grace period shorter than the drain makes the graceful shutdown
  code decorative.
- **`TRUSTED_PROXY_CIDRS` absent in production.** The pod refuses to start with
  a message naming the variable, at the deploy, where an operator is looking.
- **A wrong Zitadel client ID.** Not caught by any build — D6 of the rebrand
  spec records that `next build` succeeds with these unset and the failure is a
  runtime 500 on `/login`. Caught only by an actual sign-in.
- **Image pull fails after a Kargo promotion.** Building every image on every
  commit means the tag always exists; the negative-cached-404 mode D2 describes
  is not reachable.

## Testing

Every control is proven by mutation. A control nobody has watched fail is not
known to be a control.

- **The boot guard (D7):** deploy with the variable removed and observe the pod
  refuse to start; deploy with `none` and observe it start — the second is what
  distinguishes a working sentinel from a guard that refuses everything; deploy
  with a single malformed entry and observe the message differ from the unset
  message.
- **XFF, the assumption this slice exists to settle:** read the actual
  `X-Forwarded-For` string at a live app pod with a real request, then send a
  request with a forged `X-Forwarded-For` and confirm it does **not** get a
  fresh rate-limit budget. Observing the header alone proves nothing about the
  limiter; the forged-header test is the assertion that matters. The inferred
  `10.20.0.0/16` and the two-in-cluster-hop model are confirmed or **corrected**
  against what the pod actually sees.
- **The Trivy gate:** observed failing a pull request against a deliberately
  vulnerable pinned base image.
- **Readiness:** scale NATS to zero and observe `/readyz` return 503 naming
  `nats`, and the pod leave the Service. Asserted against the running system,
  not the code path.
- **The cloudflared change (D6):** `tesserix.app` and `mark8ly.com` confirmed
  still resolving after the edit, not only `helivanta.app`.
- **Sign-in:** a real OIDC round trip in a browser against the deployed stack,
  because D4's baked client ID fails at runtime and passes every build.
- **Graceful shutdown:** a rollout under load, asserting no request is dropped —
  the property #788's batch worker will depend on.

## Risks

- **The XFF inference may be wrong.** Everything about the trusted CIDR follows
  from a request path nobody has observed end to end. The forged-header test
  above is what converts it from inference to fact, and it is the reason the
  slice exists.
- **A signature nothing verifies** (D5). Mitigated only by saying so, in three
  places.
- **The cloudflared configmap is shared** (D6). Additive change, and existing
  hostnames are verified after.
- **Resource requests and limits are guesses until measured.** #824 asks for
  them sized against measured usage; nothing has ever run, so slice 1b sets
  conservative starting values and records that they are unmeasured. Writing a
  number and calling it "sized" would be the decorative version.
- **`tig-deny-unauthorized-writes` may reject our namespace writes.** A live
  Kyverno policy whose scope has not been read. Checked before 1a is applied,
  not discovered during it.
- **The prod Zitadel machine user needs platform sign-off** (D8), and it sits
  on the critical path.

## Out of scope

- **The three product zones** — slice 2.
- **ArgoCD promotion policy, Kyverno admission, image verification** — #7.
- **Reusable CI workflow templates** — #709. Slice 1b writes Helivanta's two
  workflows; generalising them is #709's.
- **Preview environments** — #715, which D4 records a blocker for.
- **SAST, dependency scanning, broader container policy** — #713.
- **Terraform for cloud resources** — #669.
- **`login_attempt` sweeping** — #869, unchanged by this slice.

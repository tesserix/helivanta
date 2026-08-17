# Deployment slice 1a — platform dependencies Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up Helivanta's three boot dependencies — CNPG Postgres, NATS
with JetStream, and OpenFGA — in the `helivanta` namespace on
`tesseract-prod-in-gke`, so that slice 1b's first API pod has exactly one
candidate cause when it fails to become ready.

**Architecture:** All work lands in the **`tesserix-k8s`** repository, per
ADR-0001 ("Deployment manifests live in tesserix-k8s, not here"). It mirrors
`argocd/prod/apps/dwellm8/` — an app-of-apps over per-component Applications,
ordered by `sync-wave`, with charts under `charts/apps/`. Nothing in the
`helivanta` repository changes except `dev/init-db.sql` and
`docker-compose.dev.yml` (D9), which move dev onto the same database and owner
names as production.

**Tech Stack:** ArgoCD, Helm, CloudNativePG, NATS (JetStream), OpenFGA,
External Secrets Operator against OpenBao, Kyverno (TIG policies, pre-existing).

## Global Constraints

- **Repository for all manifests:** `tesserix-k8s`. Never this repo (ADR-0001).
- **Cluster:** `gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`. There
  is no dev cluster — the `tesseract-devtest-gke` context is stale. Every change
  is applied against production, so every change is additive and separately
  reversible.
- **Namespace:** `helivanta`. **ServiceAccount:** `helivanta-api`. Both already
  exist. `SecretStore/openbao-helivanta-api` exists and is `Valid`/`Ready` — do
  not recreate it.
- **Secret store is OpenBao, never GCP Secret Manager** (spec D9). dwellm8's
  charts use `gcpSecretName`; Helivanta's must not.
- **OpenBao paths:** `kv/data/helivanta/helivanta-api/postgres-{app,api,openfga}`.
  **Corrected 2026-08-17** from `kv/data/helivanta/postgres/*`, which nothing
  grants. The `openbao` chart generates each app's policy as
  `kv/data/<namespace>/<app-name>/*` (`charts/thirdparty/openbao/templates/bootstrap-configmap.yaml:166`),
  and the live `SecretStore/openbao-helivanta-api` authenticates with role
  `app-helivanta_helivanta-api` — so the ONLY readable prefix is
  `kv/data/helivanta/helivanta-api/*`. Verified against the live SecretStore
  spec and the whole policy block; no broader `helivanta` grant exists.
- **Database:** `helivanta`. **Owner role:** `helivanta`. **Application role:**
  `hms_app` — unchanged, because migrations and every RLS policy name it
  (spec D9, rebrand spec D5). **RLS predicate:** `hms_tenant_visible()`,
  unchanged.
- **No secret value passes through an agent session.** Generate and write in one
  piped step, or use the `secret-service.tesserix.app` console directly.
- **Every control is proven by mutation.** A verification step that only observes
  a healthy state does not count; each task names the mutation that must make it
  fail.
- **ArgoCD `Synced/Healthy` can describe a stale revision.** Always check
  `.status.sync.revision` against `origin/main` before believing a sync.

---

### Task 1: Confirm the Kyverno write policy does not block the namespace

The spec lists `tig-deny-unauthorized-writes` as an unread live policy. It is
checked **before** anything is applied, not discovered during a sync. This task
writes no manifests; it produces a recorded answer.

**Files:**
- Create: none
- Modify: none

**Interfaces:**
- Produces: a recorded verdict — either "TIG does not restrict `helivanta`" or
  the exact subject/resource restriction Tasks 2-7 must satisfy. Every later
  task assumes this verdict.

- [ ] **Step 1: Read the three live policies**

```bash
K=gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke
for p in tig-deny-unauthorized-writes tig-deny-unauthorized-delete tig-audit-breakglass; do
  echo "=================== $p"
  kubectl --context $K get clusterpolicy $p -o yaml
done
```

- [ ] **Step 2: Extract what they match on**

Read each policy's `spec.rules[].match` and `.exclude`. Answer three questions
in writing:
1. Does any rule match `namespaces: [helivanta]` or a wildcard covering it?
2. Does any rule restrict by `subjects`/`clusterRoles` in a way that excludes
   the ArgoCD application controller's ServiceAccount?
3. Does any rule apply to the kinds this slice creates — `Cluster` (CNPG),
   `ExternalSecret`, `NetworkPolicy`, `Job`, `Deployment`, `StatefulSet`?

- [ ] **Step 3: Prove the answer with a dry run rather than by reading**

A policy read is an inference; an admission decision is a fact. Apply a
throwaway object of the most restricted kind through a **server-side** dry run,
which runs the full admission chain:

```bash
K=gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke
kubectl --context $K -n helivanta apply --dry-run=server -f - <<'EOF'
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: tig-admission-probe
spec:
  podSelector: {}
  policyTypes: [Ingress]
EOF
```

Expected: `networkpolicy.networking.k8s.io/tig-admission-probe created (server dry run)`.
A Kyverno denial appears here as an explicit `admission webhook ... denied the
request` error naming the policy.

- [ ] **Step 4: Record the verdict**

Write the finding into the task report: the policy names, what they match, and
the dry-run result. If any policy **does** restrict the namespace, STOP and
report — Tasks 2-7 need amending before they are dispatched, and guessing at a
workaround is how a governance control gets quietly circumvented.

- [ ] **Step 5: Commit**

No files change. Nothing to commit; the verdict is the deliverable.

---

### Task 2: ArgoCD project and namespace network policies

**Files:**
- Create: `argocd/prod/projects/helivanta.yaml` (in `tesserix-k8s`)
- Create: `charts/apps/helivanta-network-policies/Chart.yaml`
- Create: `charts/apps/helivanta-network-policies/values.yaml`
- Create: `charts/apps/helivanta-network-policies/templates/default-deny.yaml`
- Create: `charts/apps/helivanta-network-policies/templates/allow-intra-namespace.yaml`
- Create: `charts/apps/helivanta-network-policies/templates/allow-dns.yaml`
- Create: `charts/apps/helivanta-network-policies/templates/allow-istio-ingress.yaml`
- Create: `charts/apps/helivanta-network-policies/templates/allow-egress-external.yaml`
- Reference (read, do not modify): `argocd/prod/projects/dwellm8.yaml`,
  `charts/apps/dwellm8-network-policies/`

**Interfaces:**
- Consumes: Task 1's verdict.
- Produces: AppProject `helivanta` (referenced by every Application in Tasks
  3-7 as `spec.project: helivanta`), and a namespace that denies by default.

- [ ] **Step 1: Read the dwellm8 equivalents in full**

```bash
cd ~/personal/tesserix-new/tesserix-k8s
cat argocd/prod/projects/dwellm8.yaml
find charts/apps/dwellm8-network-policies -type f -exec sh -c 'echo "=== $1"; cat "$1"' _ {} \;
```

Copy their structure. Do not invent a different policy shape — a reviewer
compares these two directories side by side.

- [ ] **Step 2: Write `argocd/prod/projects/helivanta.yaml`**

Mirror `dwellm8.yaml`, substituting `dwellm8` → `helivanta` throughout.
`destinations` must permit namespace `helivanta` **and** `argocd` (the
app-of-apps in Task 7 targets `argocd`). Add the same `clusterResourceWhitelist`
entries dwellm8 has.

- [ ] **Step 3: Write the network policies, default-deny first**

The ordering matters and is the same reason dwellm8's kustomization lists
network policies first: "The namespace lock first, so nothing runs unprotected
even briefly."

`default-deny.yaml` denies all ingress and egress for `podSelector: {}`. The
`allow-*` templates then open: intra-namespace traffic, DNS to `kube-system`,
ingress from the `istio-ingress` namespace, egress for external calls, ingress
from `cnpg-system` on 8000, and HBONE on 15008.

**Corrected 2026-08-17 after Task 2's review.** This step originally said the
templates open "exactly" four things, omitting the last two. Both omissions
were real and both are in `charts/apps/dwellm8-network-policies/templates/ingress.yaml`:

- **`cnpg-system` on 8000** is load-bearing. The CNPG operator probes cluster
  instances on that port, so without it the `Cluster` Task 4 creates never
  becomes healthy — and the failure presents as a broken database, sending the
  debugger to Postgres rather than to a network policy.
- **HBONE 15008** costs nothing today because the namespace is not
  ambient-meshed. It ships anyway, for the reason dwellm8 records in its
  `values.yaml:29`: its absence is invisible until the day the namespace goes
  ambient, which is the definition of a control that fails silently.

**OpenBao egress is deliberately absent**, against this step's original
parenthetical. OpenBao's own NetworkPolicy admits only `external-secrets` and
`secret-service` as callers, so a direct egress rule from `helivanta` would be
dead configuration — and ESO mediating the read is precisely what the secrets
spec's D4 requires ("no secret-store code enters Helivanta"). The chart says so
in a comment, so the next reader does not "fix" it by adding a rule.

- [ ] **Step 4: Render both charts and validate the YAML**

```bash
cd ~/personal/tesserix-new/tesserix-k8s
helm template helivanta-network-policies charts/apps/helivanta-network-policies \
  --namespace helivanta | tee /tmp/netpol.yaml
kubectl --context gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke \
  apply --dry-run=server -n helivanta -f /tmp/netpol.yaml
```

Expected: every object reports `(server dry run)`, no errors.

- [ ] **Step 5: Commit**

```bash
git add argocd/prod/projects/helivanta.yaml charts/apps/helivanta-network-policies
git commit -m "feat: add helivanta ArgoCD project and namespace network policies"
```

---

### Task 3: OpenBao secret paths and the ExternalSecrets that read them

The three CNPG role passwords must exist in OpenBao **before** the cluster is
created, because CNPG reconciles role passwords from the projected Secrets and
a missing Secret leaves a role with a password nobody holds.

**Files:**
- Create: `charts/apps/helivanta-postgres/templates/externalsecret.yaml`
  (the chart skeleton is created here; the `Cluster` lands in Task 4)
- Create: `charts/apps/helivanta-postgres/Chart.yaml`
- Create: `charts/apps/helivanta-postgres/values.yaml`
- Reference: `charts/apps/dwellm8-postgres/templates/externalsecret.yaml`

**Interfaces:**
- Consumes: `SecretStore/openbao-helivanta-api` (pre-existing, `Valid`/`Ready`).
- Produces: three Kubernetes Secrets, each with `username` and `password` keys —
  `helivanta-postgres-app-credentials` (owner `helivanta`),
  `helivanta-postgres-api-credentials` (`hms_app`),
  `helivanta-postgres-openfga-credentials` (`openfga`). Task 4 consumes all
  three by name.

- [ ] **Step 1: Write the three secrets into OpenBao — by the human operator**

**This step is not performed by an agent.** No secret value may pass through an
agent session (Global Constraints).

**Use the `secret-service.tesserix.app` console.** `docs/runbooks/secrets.md`
(lines 85 and 112) makes this the documented write path for every Helivanta
secret, and the console's policy is create/update with deliberately **no**
`read` on `kv/data` — which is assertion 3 of #45's D7. Both
`secret-service-api` and `secret-service-web` are running in the cluster and
the hostname resolves through the existing `*.tesserix.app` Cloudflare route.

Write three paths, each with a single key `password`:

```
kv/data/helivanta/helivanta-api/postgres-app      → password (owner role `helivanta`)
kv/data/helivanta/helivanta-api/postgres-api      → password (application role `hms_app`)
kv/data/helivanta/helivanta-api/postgres-openfga  → password (OpenFGA datastore role)
```

In the console form these are: Namespace `helivanta`, Apps `helivanta-api`
(the existing app — selecting it needs no whitelisting pull request), Secret
name `postgres-app` / `postgres-api` / `postgres-openfga`. The role must live in
the secret name because the path is `<namespace>/<app>/<secret name>` and the
app segment is what the policy is scoped to.

**Corrected 2026-08-17.** This step originally gave a `bao kv put` loop. That
was written without checking the runbook, and the CLI path is worse here for a
reason worth recording: OpenBao's services are all `ClusterIP` with no external
ingress, so a CLI write needs a port-forward *plus* a human-usable token — and
the chart configures Kubernetes auth, which binds ServiceAccounts rather than
people. Obtaining a human token means reaching for a privileged credential to
do a job the console already has exactly the right, narrower grant for. The
console is not merely more convenient; it is the least-privilege path.

Step 4 asserts each projected password is **non-empty**, not that it is any
particular length, because the operator chooses the value in the console. Use at
least 32 characters.

- [ ] **Step 2: Write the ExternalSecret template**

**Use `apiVersion: external-secrets.io/v1beta1`.** Task 1 established that `v1` is not a served version on this cluster; `v1alpha1` and `v1beta1` are, with `v1beta1` as storage. The live `SecretStore/openbao-helivanta-api` and dwellm8's chart both use `v1beta1`.

One `ExternalSecret` per role, each with
`secretStoreRef: {name: openbao-helivanta-api, kind: SecretStore}`, a
`refreshInterval` of `1h`, and a `target.template` producing both keys — CNPG's
bootstrap secret must be `kubernetes.io/basic-auth` with `username` and
`password`, so the username is templated as a literal and only the password
comes from OpenBao.

Read `charts/apps/dwellm8-postgres/templates/externalsecret.yaml` first and
follow its shape, **replacing** its GCP `ClusterSecretStore` reference with the
OpenBao `SecretStore` above (spec D9).

- [ ] **Step 3: Render and dry-run**

```bash
cd ~/personal/tesserix-new/tesserix-k8s
helm template helivanta-postgres charts/apps/helivanta-postgres --namespace helivanta \
  --show-only templates/externalsecret.yaml | tee /tmp/es.yaml
kubectl --context gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke \
  apply --dry-run=server -n helivanta -f /tmp/es.yaml
```

- [ ] **Step 4: Apply and prove the values actually arrive**

Apply for real (this is additive and reversible), then assert the Secrets are
populated **and non-empty** — an ExternalSecret that resolves nothing still
reports `SecretSynced` in some versions:

```bash
K=gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke
kubectl --context $K -n helivanta get externalsecret
for s in app api openfga; do
  n=$(kubectl --context $K -n helivanta get secret helivanta-postgres-$s-credentials \
        -o jsonpath='{.data.password}' | base64 -d | wc -c)
  echo "helivanta-postgres-$s-credentials password length: $n"
done
```

Expected: three `ExternalSecret`s `SecretSynced`, and three **non-zero**
lengths. A length of `0` is the failure this step exists to catch — the path
resolved but the key did not, which some ESO versions still report as
`SecretSynced`. Do not assert a specific length: the operator chose the values
in the console, so a hardcoded expected length would fail for a correct secret.

- [ ] **Step 5: Prove the grant boundary — the mutation**

The positive read succeeding proves nothing about scope; an over-broad policy
behaves identically on every positive test. This mirrors #45's D7(2), which
names this as the assertion that can silently be wrong.

The probe must target a path **outside** `kv/data/helivanta/*` — pointing it at
our own path only repeats Step 4. But it must not target another product's real
secret. If the policy is over-broad, this test does not report a failure; it
*succeeds*, and a real cross-product credential is now in a session log. A test
whose failure mode is a secret disclosure is the wrong test.

An unused path does not work either, and this is the trap #45's D7 names
explicitly: a read of a path that does not exist returns **not found**, not
**permission denied**. The probe would then pass while proving nothing about
the policy.

So the operator writes a worthless canary at a path outside the grant, first —
the `secret-service` console policy holds `create`/`update` on `kv/data/*` and
deliberately no `read`, so this is within its rights:

```bash
bao kv put kv/scope-probe/helivanta-denial canary="not-a-secret"
```

Then point one ExternalSecret at `kv/data/scope-probe/helivanta-denial`, apply,
and read the condition:

```bash
kubectl --context $K -n helivanta get externalsecret helivanta-postgres-api-credentials \
  -o jsonpath='{.status.conditions[*].message}{"\n"}'
```

Expected: a **permission-denied** message from OpenBao — not a 404, which would
mean the canary write did not land and the test is inert. Then revert the path,
re-apply, and confirm it returns to `SecretSynced`.

**All three halves are required.** The canary must exist (or a denial proves
nothing), the read must be refused (the actual assertion), and the revert must
restore `SecretSynced` (or the "denial" may just be a broken manifest). If the
read instead *succeeds* and returns `not-a-secret`, the `read-helivanta` policy
is over-broad — stop, and fix the policy before any real secret is written.

Delete the canary afterwards: `bao kv metadata delete kv/scope-probe/helivanta-denial`.

- [ ] **Step 6: Commit**

```bash
git add charts/apps/helivanta-postgres
git commit -m "feat: project helivanta postgres role passwords from OpenBao"
```

---

### Task 4: The CNPG Postgres cluster

**Files:**
- Modify: `charts/apps/helivanta-postgres/values.yaml`
- Create: `charts/apps/helivanta-postgres/templates/cluster.yaml`
- Create: `charts/apps/helivanta-postgres/templates/scheduled-backup.yaml`
- Create: `argocd/prod/apps/helivanta/helivanta-postgres.yaml`
- Reference: `charts/apps/dwellm8-postgres/` (all of it),
  `argocd/prod/apps/dwellm8/dwellm8-postgres.yaml`

**Interfaces:**
- Consumes: the three Secrets from Task 3.
- Produces: Service `helivanta-postgres-rw.helivanta.svc.cluster.local:5432`,
  database `helivanta`, owner role `helivanta`, application roles `hms_app` and
  `openfga`. Tasks 5, 6 and slice 1b all dial the `-rw` Service.

- [ ] **Step 1: Copy the dwellm8 chart and retarget it**

```bash
cd ~/personal/tesserix-new/tesserix-k8s
cp charts/apps/dwellm8-postgres/templates/cluster.yaml \
   charts/apps/dwellm8-postgres/templates/scheduled-backup.yaml \
   charts/apps/helivanta-postgres/templates/
```

Then rewrite `values.yaml`. Use `perl -pi -e`, **never** `sed` with `\b` — BSD
sed silently ignores it and produces a no-op with no error (handoff).

- [ ] **Step 2: Set the values, with `hms_app` deliberately not renamed**

```yaml
namespace: helivanta
clusterName: helivanta-postgres
instances: 1
imageName: ghcr.io/cloudnative-pg/postgresql:16.4
storageSize: 10Gi
storageClass: standard-rwo-retain
walStorageSize: 5Gi

bootstrap:
  database: helivanta
  owner: helivanta
  secret: helivanta-postgres-app-credentials
  postInitSQL:
    # RLS is worthless if the owner bypasses it; the application role below
    # is NOBYPASSRLS and is what a request runs as.
    - "ALTER ROLE helivanta SET row_security = on"

applicationRoles:
  # hms_app is NOT renamed: every migration and RLS policy names it, and
  # migrations are append-only (spec D9, rebrand spec D5).
  - name: hms_app
    secret: helivanta-postgres-api-credentials
  - name: openfga
    secret: helivanta-postgres-openfga-credentials
```

Delete every `gcpSecretName` key inherited from dwellm8 — Helivanta reads from
OpenBao and a leftover key is a silent no-op that misleads the next reader.

- [ ] **Step 3: Write the Application manifest**

Copy `argocd/prod/apps/dwellm8/dwellm8-postgres.yaml`, substitute names, keep
`sync-wave: "-5"`, and **keep its comment about `RespectIgnoreDifferences`
being deliberately absent** — that comment records a real failure where sync
reported `Succeeded` while the Cluster's generation never moved and a role
could not land.

- [ ] **Step 4: Render, dry-run, then sync**

```bash
cd ~/personal/tesserix-new/tesserix-k8s
helm template helivanta-postgres charts/apps/helivanta-postgres --namespace helivanta \
  | kubectl --context gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke \
    apply --dry-run=server -n helivanta -f -
```

Commit and push, then confirm ArgoCD synced **the right revision**:

```bash
K=gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke
kubectl --context $K -n argocd get application helivanta-postgres \
  -o jsonpath='{.status.sync.status} {.status.sync.revision}{"\n"}'
git ls-remote origin main
```

Expected: `Synced` and a revision equal to `origin/main`. If they differ, hard
refresh — a stale revision reporting `Synced` bit this project twice.

- [ ] **Step 5: Assert the cluster is healthy and the roles exist**

```bash
K=gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke
kubectl --context $K -n helivanta get cluster helivanta-postgres
kubectl --context $K -n helivanta exec -it helivanta-postgres-1 -- \
  psql -U postgres -d helivanta -c '\du'
```

Expected: `Cluster in healthy state`, and `\du` lists `helivanta`, `hms_app`
and `openfga`.

- [ ] **Step 6: Prove `hms_app` cannot bypass RLS — the mutation**

This is the control that matters, and it is invisible when correct. Assert the
attribute directly rather than trusting that the role was created as intended:

```bash
kubectl --context $K -n helivanta exec -it helivanta-postgres-1 -- \
  psql -U postgres -d helivanta -tAc \
  "SELECT rolname, rolbypassrls, rolsuper FROM pg_roles WHERE rolname IN ('hms_app','helivanta');"
```

Expected: `hms_app|f|f`. If `rolbypassrls` is `t` for `hms_app`, every RLS
policy in the schema is decorative and this task is not done.

- [ ] **Step 7: Commit**

```bash
git add charts/apps/helivanta-postgres argocd/prod/apps/helivanta/helivanta-postgres.yaml
git commit -m "feat: add helivanta CNPG postgres cluster"
```

---

### Task 5: Schema bootstrap — grants and the OpenFGA database

Helivanta's own schema is applied by `backend/cmd/migrate` in slice 1b, not
here. This task creates only what `migrate` cannot: the `openfga` database, and
the grants that `dev/init-db.sql` performs locally.

**Files:**
- Create: `argocd/prod/apps/helivanta/helivanta-db-schema-bootstrap.yaml`
- Reference: `argocd/prod/apps/dwellm8/dwellm8-db-schema-bootstrap.yaml`,
  `charts/apps/db-schema-bootstrap/` (shared chart — read `values.yaml` for the
  `targets` contract before writing values)

**Interfaces:**
- Consumes: the `-rw` Service and owner credentials from Task 4.
- Produces: database `openfga` owned by role `openfga`; `hms_app` holding the
  same grants `dev/init-db.sql` gives it. Task 6 and slice 1b depend on both.

- [ ] **Step 1: Read the shared chart's contract**

```bash
cd ~/personal/tesserix-new/tesserix-k8s
cat charts/apps/db-schema-bootstrap/values.yaml
ls -R charts/apps/db-schema-bootstrap
```

Determine exactly how `targets[]`, `combineInto` and `applySchemaToExistingDatabases`
behave, and where schema files are read from. Do not assume — dwellm8's values
reference a `schemas/dwellm8/...` path whose root is not obvious.

- [ ] **Step 2: Write the grants to mirror `dev/init-db.sql`**

The local file is the authority for what `hms_app` may do:

```sql
GRANT USAGE ON SCHEMA public TO hms_app;
ALTER DEFAULT PRIVILEGES FOR ROLE helivanta IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO hms_app;
ALTER DEFAULT PRIVILEGES FOR ROLE helivanta IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO hms_app;
```

Note `FOR ROLE helivanta`, not `hms` — the owner renamed (D9), the application
role did not. Getting this wrong produces tables `hms_app` cannot read, which
surfaces at the first request as a permission error, not at bootstrap.

- [ ] **Step 3: Write the Application manifest**

Copy the dwellm8 one, `sync-wave: "-3"`, two targets: `helivanta` (grants only —
set `applySchemaToExistingDatabases` per what Step 1 established) and `openfga`
(database creation and role grants only; OpenFGA's own schema is applied by its
migrate Job in Task 6).

- [ ] **Step 4: Sync and verify both databases exist**

```bash
K=gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke
kubectl --context $K -n helivanta exec -it helivanta-postgres-1 -- psql -U postgres -tAc '\l'
```

Expected: `helivanta` and `openfga` both listed.

- [ ] **Step 5: Prove the grants took — the mutation**

Connect **as `hms_app`** and confirm it can write to a table owned by
`helivanta`, then confirm it cannot do what it must not:

```bash
kubectl --context $K -n helivanta exec -it helivanta-postgres-1 -- bash -c '
psql -U postgres -d helivanta -c "CREATE TABLE grant_probe(id int);"
psql -U hms_app -d helivanta -c "INSERT INTO grant_probe VALUES (1);"
psql -U hms_app -d helivanta -c "DROP TABLE grant_probe;"   # must FAIL
psql -U postgres -d helivanta -c "DROP TABLE grant_probe;"
'
```

Expected: the `INSERT` succeeds, the `hms_app` `DROP` fails with
`must be owner of table`. A run where both succeed means `hms_app` is
over-granted.

- [ ] **Step 6: Commit**

```bash
git add argocd/prod/apps/helivanta/helivanta-db-schema-bootstrap.yaml
git commit -m "feat: bootstrap helivanta schema grants and the openfga database"
```

---

### Task 6: NATS with JetStream, and OpenFGA on its Postgres datastore

**Files:**
- Create: `charts/apps/nats/values-helivanta.yaml` (shared chart, new values file)
- Create: `argocd/prod/apps/helivanta/helivanta-nats.yaml`
- Create: `charts/apps/helivanta-openfga/` (Chart.yaml, values.yaml, and
  templates copied from `charts/apps/dwellm8-openfga/templates/`)
- Create: `argocd/prod/apps/helivanta/helivanta-openfga.yaml`

**Interfaces:**
- Consumes: Task 4's `-rw` Service, Task 3's `helivanta-postgres-openfga-credentials`,
  Task 5's `openfga` database.
- Produces: `nats://helivanta-nats.helivanta.svc.cluster.local:4222` and
  `http://helivanta-openfga.helivanta.svc.cluster.local:8080`. These are the
  literal values slice 1b sets as `NATS_URL` and `OPENFGA_URL`.

- [ ] **Step 1: Write the NATS values with JetStream enabled**

Read `charts/apps/nats/values-dwellm8.yaml` and copy its shape.

**JetStream must be on.** The local stack runs `nats:2.10-alpine` with
`command: ["-js", "-sd", "/data"]` (`docker-compose.dev.yml`), and the rebrand
moved the event stream to `HELIVANTA` — a stream is a JetStream object. A NATS
without JetStream accepts connections and passes a `Ping`, so the API would
become **ready** and then fail on first publish. That is why this is called out
rather than left to the chart default.

- [ ] **Step 2: Write the OpenFGA chart**

Copy all five templates from `charts/apps/dwellm8-openfga/templates/`.
`values.yaml` sets the datastore engine to `postgres` and the URI from the
`openfga` role's Secret.

**Do not copy the dev datastore setting.** `docker-compose.dev.yml` runs OpenFGA
with `OPENFGA_DATASTORE_ENGINE: memory`; in production that would silently
discard every authorization tuple on restart.

- [ ] **Step 3: Sync both, in wave order**

NATS at `sync-wave: "-6"`, OpenFGA at `"-2"` (after the schema bootstrap that
creates its database). Verify the synced revision matches `origin/main`, as in
Task 4 Step 4.

- [ ] **Step 4: Verify NATS JetStream is actually enabled — the mutation**

Connections succeeding is not evidence. Ask the server for its JetStream state:

```bash
K=gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke
kubectl --context $K -n helivanta exec -it deploy/helivanta-nats -- \
  nats --server localhost:4222 account info
```

Expected: a `JetStream Account Information` section with limits, **not**
`JetStream not enabled`.

- [ ] **Step 5: Verify OpenFGA persists across a restart — the mutation**

The memory-vs-postgres distinction is invisible until a restart, which is
exactly when it costs the most. Create a store, restart the pod, and read it
back:

```bash
K=gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke
kubectl --context $K -n helivanta port-forward svc/helivanta-openfga 8080:8080 &
curl -s -X POST localhost:8080/stores -d '{"name":"persistence-probe"}' | tee /tmp/store.json
kubectl --context $K -n helivanta rollout restart deploy/helivanta-openfga
kubectl --context $K -n helivanta rollout status deploy/helivanta-openfga
curl -s localhost:8080/stores | grep persistence-probe
```

Expected: the store is still listed after the restart. If it is gone, the
datastore is `memory` regardless of what the values file says. Delete the probe
store afterwards.

- [ ] **Step 6: Establish whether an image pull secret is needed**

dwellm8's kustomization lists a `dwellm8-secrets` Application ahead of
everything else — "Pull credentials before anything that pulls an image."
Whether Helivanta needs the equivalent depends on where these three images are
pulled from, so check rather than assume:

```bash
cd ~/personal/tesserix-new/tesserix-k8s
cat argocd/prod/apps/dwellm8/dwellm8-secrets.yaml
K=gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke
kubectl --context $K -n helivanta get pods -o jsonpath='{range .items[*]}{.spec.containers[*].image}{"\n"}{end}'
```

If any image resolves through the private GAR mirror
(`asia-south1-docker.pkg.dev/.../ghcr-remote/...`) rather than a public
registry, add a `helivanta-secrets` Application mirroring dwellm8's and place it
first in Task 7's kustomization. If all three pull from public registries, record
that and skip it — slice 1b will need it regardless, because Helivanta's own
images are published to GHCR and pulled through that mirror.

A pod stuck in `ImagePullBackOff` is the loud version of this failure; the quiet
version is a pull that succeeds now from a cached layer and fails on a node
that has never seen the image.

- [ ] **Step 7: Commit**

```bash
git add charts/apps/nats/values-helivanta.yaml charts/apps/helivanta-openfga \
        argocd/prod/apps/helivanta/helivanta-nats.yaml \
        argocd/prod/apps/helivanta/helivanta-openfga.yaml
git commit -m "feat: add helivanta NATS with JetStream and OpenFGA on postgres"
```

---

### Task 7: App-of-apps, and the network policy proven to deny

**Files:**
- Create: `argocd/prod/apps/helivanta/kustomization.yaml`
- Create: `argocd/prod/apps/helivanta-app-of-apps.yaml`
- Modify: `argocd/prod/apps/kustomization.yaml` (add the app-of-apps)
- Create: `argocd/prod/apps/helivanta/helivanta-network-policies.yaml`

**Interfaces:**
- Consumes: every Application from Tasks 2-6.
- Produces: a single ArgoCD entry point for the namespace. Slice 1b adds
  `helivanta-api.yaml` and `helivanta-shell.yaml` to this kustomization.

- [ ] **Step 1: Write the kustomization in dependency order**

Mirror `argocd/prod/apps/dwellm8/kustomization.yaml`, including its comments,
which explain the ordering rather than merely asserting it:

```yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  # The namespace lock first, so nothing runs unprotected even briefly.
  - helivanta-network-policies.yaml
  # CNPG first, then the schema (grants + the openfga database).
  - helivanta-postgres.yaml
  - helivanta-db-schema-bootstrap.yaml
  # The broker, then the authorization engine. Both before slice 1b's API,
  # which fails closed without either.
  - helivanta-nats.yaml
  - helivanta-openfga.yaml
```

- [ ] **Step 2: Write the app-of-apps**

Copy `argocd/prod/apps/dwellm8-app-of-apps.yaml`. **Add the
`ignoreDifferences` on `/spec/source/helm/parameters` that dwellm8 omits** —
its own comment says the exception was left out only because dwellm8 has no
Kargo Warehouse, and that omitting it made a stale `image.tag` unremovable by
git. Slice 1b introduces Kargo, so the exception belongs here from the start.

- [ ] **Step 3: Sync and confirm every Application is Healthy**

```bash
K=gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke
kubectl --context $K -n argocd get applications -l app.kubernetes.io/part-of=helivanta \
  -o custom-columns=NAME:.metadata.name,SYNC:.status.sync.status,HEALTH:.status.health.status,REV:.status.sync.revision
git ls-remote origin main
```

Expected: all `Synced`/`Healthy`, every revision equal to `origin/main`.

- [ ] **Step 4: Prove the network policy denies — the mutation**

A default-deny policy that matches nothing looks exactly like one that works.
Run a throwaway pod in another namespace and confirm it **cannot** reach
Postgres, then confirm a pod inside `helivanta` **can** — both halves, because
a failure caused by a wrong Service name would produce the same first result:

```bash
K=gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke
# Must FAIL (timeout), from outside the namespace:
kubectl --context $K -n default run netpol-probe --rm -it --restart=Never \
  --image=busybox:1.36 -- timeout 8 nc -zv \
  helivanta-postgres-rw.helivanta.svc.cluster.local 5432
# Must SUCCEED, from inside it:
kubectl --context $K -n helivanta run netpol-probe --rm -it --restart=Never \
  --image=busybox:1.36 -- timeout 8 nc -zv \
  helivanta-postgres-rw.helivanta.svc.cluster.local 5432
```

Expected: the first times out, the second reports `open`. If the first
succeeds, the default-deny selector is not matching and the namespace is
unprotected.

- [ ] **Step 5: Update the dev stack to the new database names (spec D9)**

In the **`helivanta`** repository:
- `dev/init-db.sql`: `ALTER DEFAULT PRIVILEGES FOR ROLE hms` → `FOR ROLE helivanta`.
- `docker-compose.dev.yml`: `POSTGRES_USER: hms` → `helivanta`,
  `POSTGRES_PASSWORD: hms` → `helivanta`, `POSTGRES_DB: hms` → `helivanta`.
- `backend/internal/config/config.go`: the `APP_DATABASE_URL` and
  `ADMIN_DATABASE_URL` defaults (`config.go:325` region) — `hms_app` keeps its
  role name; the **database** and **owner** change.
- `.env.example` and any `docs/` connection strings.

- [ ] **Step 6: Prove the dev stack still works from scratch**

A rename that breaks a fresh clone is the failure mode D3 of the rebrand spec
warns about. Run the real thing, not a proxy for it:

```bash
cd ~/personal/tesserix-new/helivanta
make reset && make up
make test-go
```

Expected: the stack comes up and the Go suite passes against it. Watch for
leftover `next dev` processes holding ports — the handoff names this the top
local-dev hazard, and it presents as "the app is broken".

- [ ] **Step 7: Commit both repositories**

```bash
cd ~/personal/tesserix-new/tesserix-k8s
git add argocd/prod/apps/helivanta argocd/prod/apps/helivanta-app-of-apps.yaml \
        argocd/prod/apps/kustomization.yaml
git commit -m "feat: add helivanta app-of-apps for platform dependencies"

cd ~/personal/tesserix-new/helivanta
git add dev/init-db.sql docker-compose.dev.yml backend/internal/config/config.go .env.example docs
git commit -m "refactor: rename dev database and owner role to helivanta"
```

---

## Definition of done for slice 1a

- All five Applications `Synced`/`Healthy` at a revision equal to `origin/main`.
- `hms_app` proven `NOBYPASSRLS` and `NOSUPERUSER` (Task 4 Step 6).
- `hms_app` proven able to write and **not** able to drop (Task 5 Step 5).
- JetStream proven enabled (Task 6 Step 4).
- OpenFGA proven to survive a restart (Task 6 Step 5).
- The namespace proven to deny cross-namespace traffic (Task 7 Step 4).
- The OpenBao grant proven to deny an existing path outside `kv/data/helivanta/*`,
  and proven to recover (Task 3 Step 5) — probed with a disposable canary, never
  another product's real secret.
- A fresh `make reset && make up` works on the renamed dev database.

Slice 1b is planned only after this is met, because it depends on values this
slice produces: the CNPG Secret names, the exact Service DNS, and the confirmed
health of all three dependencies.

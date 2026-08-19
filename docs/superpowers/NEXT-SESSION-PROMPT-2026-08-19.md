# Next session — take over Helivanta

Working directory: `/Users/Mahesh.Sangawar/personal/tesserix-new/helivanta`.
Sibling repos you will need: `../tesserix-k8s` (all charts and ArgoCD), and
`../secret-service` (the OpenBao console, cloned for reference).

## Where things stand

The API and shell are **live in production**. `https://helivanta.app` serves
Helivanta's own branded login form and the whole chain is proven end to end:
Cloudflare tunnel → Istio → shell → our login page → Go API → Zitadel's
login-client API. `/login` returns 200; `GET /api/v1/auth/login/request/<id>`
returns the real login policy.

Slice 1b Tasks 1–8 are done. Task 9 ("a real sign-in, in a browser") is the
only one left, and it is blocked — see the top blocker below.

## Start here — read before doing anything

1. `gh issue view 894` — **authorization reconciliation has never worked in
   production.** This is the top blocker and it has a destructive edge.
2. `docs/superpowers/plans/2026-08-18-deployment-slice-1b.md` — the plan. Task 5
   carries a written account of the five defects the first live deployment
   exposed; the pattern in it is more useful than the individual bugs.
3. `gh issue view 893` — the bootstrap path. Committed and pushed; needs
   review and a PR.

## Blockers, in order

### 1. #894 — the reconciler cannot read `iam_members` (do this first)

`iam_members` is FORCE RLS with policy
`tenant_id = current_setting('app.tenant_id', true)::uuid`. FORCE binds the
table owner too. `platform.Reconcile` reads across all tenants via `WithAdmin`,
assuming the admin role bypasses RLS. **In production it does not** — live
`helivanta` has `rolsuper=false, rolbypassrls=false`. Proven:

```
SET ROLE helivanta; SELECT count(*) FROM iam_members;   -- 0
SET app.tenant_id = '<uuid>'; SELECT count(*);          -- 1
```

So a real membership row exists and reconciles to nothing.

**What this actually breaks — corrected 2026-08-19.** An earlier draft of this
handoff said the reconciler would prune every tuple on the next restart. It will
not: `reconcile.go:243-251` already guards exactly this case. If zero *usable*
memberships are read while OpenFGA still holds tuples, `Reconcile` refuses to
prune and returns an error naming this very cause — "ADMIN_DATABASE_URL pointed
at a role that does not bypass RLS". The guard has been in place since #759
(`5926c0f`), is on `main`, and is covered by
`TestReconcileRefusesToPruneOnGlobalEmptyMembershipRead` and
`TestReconcileRefusesToPruneOnAllUnknownRoleKeys`
(`reconcile_prune_test.go:100`, `:127`). No tuple is at risk. Do not go looking
for silent deletion; it will not come.

The real failure mode is the opposite signature, and it is worse to be surprised
by. `cmd/api/main.go:239-241` returns that error straight out of boot. So today,
with no memberships granted through the API, OpenFGA holds no tuples, the guard
does not trip, and reconcile is merely a no-op — authorization silently does
nothing. **The moment one membership is granted through `POST /v1/iam/members`,
the `iam-fga-sync` consumer writes tuples, and from then on the API fails to
start.** Fail-closed and the correct direction, but it converts a silent no-op
into a total outage at the next restart, with nothing lost to point at as the
cause. That is why this is the top blocker, and it is a reason to fix it before
granting anything, not after.

Why nothing caught it: `dev/init-db.sql` constrains `hms_app` as
`NOSUPERUSER NOBYPASSRLS` but says nothing about the owner, which in dev is the
Postgres image superuser and in production is a plain CNPG owner. The test
harness reproduces dev.

### 2. #893 — first `tenant_admin` bootstrap (committed and pushed, no PR yet)

Branch `feat/893-bootstrap-first-admin` is pushed to origin. The work is commit
`c98952e`: `backend/cmd/bootstrap/` (new), plus edits to `pkg/authz/authz.go`
(`SystemRoles()`), `authz_test.go`, and `internal/archtest/arch_test.go`.

It **works against production** — already used to write the first row:

```
tenant  ba97b57a-f5fb-4c3b-b357-eba3a85463aa   <- the production tenant UUID, permanent
subject 386888878927118733                     <- Zitadel user mahesh.sangawar
role    tenant_admin
```

It uses `WithTenant`, not `WithAdmin`, so the RLS policy is **satisfied rather
than bypassed** and the write is provably scoped to the named tenant. An earlier
revision used `WithAdmin` and was refused by production. That fix also removed
the need for an archtest allowlist exception.

Still open on it, from the implementing agent's own report:
- it writes a row but publishes **no event**, so tuples only appear at the next
  API boot reconcile — an operator running it against a live API sees nothing
  change
- `tenantdb.Open` opens both pools, so `APP_DATABASE_URL` must be valid even
  though only one is used. Same is true of `cmd/migrate`. Matters when this is
  wired into a Job.
- the UUID check catches malformed, not wrong. There is no `tenants` table, so
  nothing ever catches a well-formed wrong tenant.

Tests are written and mutation-proven. `golangci-lint` clean. It works against
production and carries the three gaps above, so it wants a fresh review rather
than a tired merge — open the PR against `main` and review it cold.

### 3. Everything else

- **#891** — `helivanta-postgres` has no CPU limit; namespace quota is at 7/8.
  A drain, eviction or CNPG failover will not be re-admitted. **The database
  cannot currently be restarted.** Root cause is `nats` holding an unbounded 4
  CPU. Arguably more urgent than anything except #894.
- **#892** — branded login. Resolved in practice by disabling instance-wide
  Login V2, but see the standing warning below.
- **#890** — per-zone subdomains. Decision recorded, deliberately deferred.
- **#882** — `new-zone.mjs`; sibling of #830.
- **#883** — test DBs vanish between CREATE and GRANT. Much ruled out, root
  cause unproven; `pgForensics` will explain the next occurrence.
- **PR #886** in `helivanta` is still OPEN (runbook note about the console
  storing key names verbatim). Merge it.
- `tesserix-k8s` PRs **#414** and **#422** are the **empty** whitelist PRs the
  secret-service console generates — the defect filed as
  `tesserix/secret-service#16`. Harmless, but see the warning about #392 below.

## Standing rules — do not trip over these

- **`tesserix-k8s#392` must NEVER be merged.** It grants
  `helivanta/scope-probe`, and merging it creates the policy whose absence
  produces the 403 proving the OpenBao grant is bounded. That 403 is the only
  evidence there is, because an over-broad policy passes every positive test
  identically. Note #414 and #422 have near-identical generated titles.
- **`instances: 1` with backups off** must be raised before a patient record
  exists.
- **Instance-wide Login V2 is now Disabled.** `console-web` explicitly wants V1
  and gets it. `Management Console` needed an explicit V2 setting to keep
  working — removing it locks the operator out of the Zitadel console, which
  happened once. `tesserix-blog-web` and `stockpilot-web` are on V1;
  stockpilot's 503 is pre-existing (no pods in its namespace).
- **Zitadel `PUT .../oidc_config` is a full replace, not a patch.** Any omitted
  field resets to default and `authMethodType`'s default requires a client
  secret. Re-check it after every save; it was observed flipping to `Basic`
  twice.
- **No secret value passes through an agent session.** Read secrets inside a
  pipeline or a shell variable; never print them. `iam-admin-pat` in the
  `zitadel` namespace gives Zitadel admin API access this way.

## How defects were actually found today — this is the useful part

Every significant bug this session was a **confident written claim that its own
cited evidence did not support**, and each was caught only by reading the live
object:

- A NetworkPolicy allowing :8080 proved Zitadel was *reachable*, not that it
  would *serve* that Host. Zitadel resolves the instance from the Host header.
- A LimitRange's derived `default.cpu` "forbids nothing `max` already allowed" —
  true of the LimitRange alone, false once the namespace ResourceQuota is
  included. That wedged an ArgoCD sync for 40 minutes.
- A chart comment said a value was "NOT a boot-time guard". `cmd/api` exits on it.
- The dev-stack comment blames `LoginV2 required=true` for the per-app baseUri
  being ignored. The production instance disproves it.

And three times a **success signal was accepted without confirming it moved the
thing** — ArgoCD reporting `Synced` while the live policy was untouched;
Zitadel's features API answering `No changes` to a field it had ignored; a `404`
on a PAT probe read as "auth works" when it was the permission denial. Memory
entry `success-signals-are-not-proof` records this.

**So: after any change, read the live object back and assert on the specific
field.** Never close a loop on the mutating call's own return value.

## Verification habits this repo expects

- Prove a test can fail — mutate the implementation, watch it fail, revert.
- Server-side dry run (`kubectl apply --dry-run=server`) before touching the
  cluster; it caught real schema problems today.
- CI billing is restored, so red checks are real signal again.

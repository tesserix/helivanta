# Helivanta Rebrand (#863) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Rename `hms` to `helivanta` across code, packages, environment, infrastructure and documentation, in one coordinated change.

**Architecture:** Six sequential tasks, each independently testable: Go module, frontend packages, environment and dev stack, documentation, infrastructure, then the repo and directory rename last. The mechanical majority is caught by compilers; the dangerous minority lives in string literals, env vars and YAML that no compiler checks.

**Tech Stack:** Go 1.26, pnpm workspaces, Next.js 16, Zitadel v4, OpenBao, Kubernetes, GitHub.

## Global Constraints

- **Spec:** `docs/superpowers/specs/2026-08-17-helivanta-rebrand-design.md`. D1–D8 refer to it.
- **The identifier is `helivanta`** — the bare word, never `helivanta.app`. Case forms: `hms`→`helivanta`, `HMS`→`HELIVANTA` (env prefixes), `HMS`/`Hms` as the product name in prose→`Helivanta`.
- **D1's acronym rule:** `HMS` expands to "Hospital Management System"; `Helivanta` expands to nothing. **The name renames, the category description stays.** Never produce "a helivanta platform" where the text meant the category. This cannot be done by `sed` alone.
- **D5 — DATABASE IDENTIFIERS DO NOT RENAME.** `hms_app` (Postgres role) and `hms_tenant_visible()` (the RLS predicate function) and every role name inside migrations stay exactly as they are. Migrations are append-only. Touching them fails this plan.
- **`hms_session` DOES rename** — it is the HTTP session cookie (`backend/pkg/authn/authn.go:15`), NOT a database object. An earlier draft of D5 wrongly excluded it. Task 3 owns it. Free now (no deployment, no live sessions); a fleet-wide forced sign-out later.
- **D2's evidence carve-out:** in `docs/superpowers/spikes/2026-08-16-zitadel-login-client.md`, the literal transcribed request bodies containing `pwchange-test@hms.dev` and `HmsDev123!` keep their original values — they record what was sent on the wire. Everything else in that file renames.
- **UI copy `Email`, `Password`, `Sign in` MUST NOT CHANGE** (login-client spec D6). `e2e/tests/support/login.ts` drives sign-in by accessible name.
- **Commit messages:** single line, conventional commits, no signature, no `Co-Authored-By`.
- **Before done:** `make lint-go`, `make test-go`, `make coverage-go` green; `pnpm turbo lint type-check test build` green.

**Prerequisite:** PR #864 (#45 boot secrets) must be merged to `main` and this branch rebased onto it before Task 1 starts. This branch is already based on `feat/45-secrets-management` for that reason.

---

## File Structure

| Area | Files | Task |
|---|---|---|
| `backend/go.mod`, 110 Go files (364 import lines), `archtest` string constants | Go module path | 1 |
| `packages/{api,config,ui}`, `apps/{shell,medicore,pharmacy,lab}` package.json + imports + tsconfig paths | Frontend packages | 2 |
| `.env.example`, `docker-compose.dev.yml`, `Makefile`, `scripts/lib/zitadel.mjs`, CI env | Environment + Zitadel | 3 |
| 66 markdown files | Documentation | 4 |
| `.gitleaks.toml`, `docs/runbooks/secrets.md`, `tesserix-k8s` PR, live k8s objects | Infrastructure | 5 |
| GitHub repo, local directory | Repo rename | 6 |

---

### Task 1: Go module path and backend

**Files:**
- Modify: `backend/go.mod` (line 1)
- Modify: 110 Go files across `backend/` — 364 import lines
- Modify: `backend/internal/archtest/arch_test.go:38` (string constant)
- Modify: any Go **string literal** containing the module path

**Interfaces:**
- Consumes: nothing.
- Produces: module path `github.com/tesserix/helivanta`, which every later task's imports assume.

**The one thing that fails silently here.** `arch_test.go:38` is:
```go
const modulesPrefix = "github.com/tesserix/hms/internal/modules/"
```
`TestModulesDoNotImportEachOther` calls `packages.Load(..., modulesPrefix+"...")` and then loops over the result. If this **string constant** is not rewritten with the imports, `packages.Load` returns zero packages, the loop body never executes, and **the test passes while enforcing nothing** — the exact failure mode this codebase's principles single out. A tool that rewrites only import blocks would miss it.

- [ ] **Step 1: Rewrite the module path everywhere it appears as text**

```bash
cd /Users/Mahesh.Sangawar/personal/tesserix-new/hms
# -l lists matching files; rewrite them in place. This deliberately catches
# STRING LITERALS as well as import lines — see the arch_test.go note above.
grep -rl "github.com/tesserix/hms" --include="*.go" --include="go.mod" backend \
  | xargs sed -i.bak "s|github.com/tesserix/hms|github.com/tesserix/helivanta|g"
find backend -name "*.bak" -delete
```

- [ ] **Step 2: Confirm no occurrence survives**

```bash
grep -rn "github.com/tesserix/hms" backend | wc -l    # expect: 0
grep -n "modulesPrefix" backend/internal/archtest/arch_test.go
```
Expected: `0`, and `modulesPrefix` now reads `github.com/tesserix/helivanta/internal/modules/`.

- [ ] **Step 3: Build and test**

```bash
make lint-go && make test-go
```
Expected: both green.

- [ ] **Step 4: PROVE the arch test still enforces something**

A green arch suite proves nothing here on its own — a vacuous pass looks identical to a real one. Plant a violation it must catch: make one module import another.

Add to `backend/internal/modules/lab/service.go` (or any lab file) an import of `github.com/tesserix/helivanta/internal/modules/pharmacy` plus a reference that keeps it used, then:

```bash
cd backend && go test ./internal/archtest/ -run TestModulesDoNotImportEachOther -v
```
Expected: **FAIL**, with a message naming `module "lab" imports module "pharmacy"`.

If it PASSES, `modulesPrefix` did not get rewritten (or `packages.Load` is finding nothing) and the arch tests are inert — stop and fix that before continuing.

Then revert the planted import and confirm the test passes again.

- [ ] **Step 5: Commit**

```bash
git add -A backend
git commit -m "refactor: move go module path to github.com/tesserix/helivanta (#863)"
```

---

### Task 2: Frontend packages

**Files:**
- Modify: `packages/{api,config,ui}/package.json` — `@hms/*` → `@helivanta/*`
- Modify: `apps/{shell,medicore,pharmacy,lab}/package.json` — same
- Modify: every `.ts`/`.tsx` importing `@hms/*` (64 files in `apps`, 26 in `packages`)
- Modify: `tsconfig.json` path aliases, `turbo.json` if it names packages
- Modify: `pnpm-workspace.yaml` if it names packages
- Regenerate: `pnpm-lock.yaml`

**Interfaces:**
- Consumes: nothing from Task 1 (independent trees).
- Produces: `@helivanta/{api,config,ui,shell,medicore,pharmacy,lab}`.

All seven packages are `private: true` at version `0.0.0` (verified 2026-08-17), so nothing external consumes them and no registry publish is involved.

- [ ] **Step 1: Rewrite the package scope**

```bash
cd /Users/Mahesh.Sangawar/personal/tesserix-new/hms
git ls-files apps packages e2e '*.json' '*.ts' '*.tsx' \
  | xargs grep -l "@hms/" 2>/dev/null \
  | xargs sed -i.bak 's|@hms/|@helivanta/|g'
find . -name "*.bak" -not -path "*/node_modules/*" -delete
```

- [ ] **Step 2: Confirm and reinstall**

```bash
grep -rn "@hms/" --include="*.ts" --include="*.tsx" --include="*.json" apps packages e2e | grep -v node_modules | wc -l   # expect: 0
pnpm install
```
Expected: `0`, and a clean install regenerating `pnpm-lock.yaml`.

- [ ] **Step 3: Verify the accessible-name contract is untouched**

Before running anything, confirm the rename did not touch UI copy:

```bash
grep -rn "Sign in\|\"Email\"\|\"Password\"" apps/shell packages/ui --include="*.tsx" | head
```
Expected: the strings `Email`, `Password`, `Sign in` still present and unchanged. If any became "Helivanta …", revert that edit — all eleven e2e specs drive login by these names.

- [ ] **Step 4: Build and test**

```bash
pnpm turbo lint type-check test build
```
Expected: green.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "refactor: rename @hms/* workspace packages to @helivanta/* (#863)"
```

---

### Task 3: Environment variables, dev stack and Zitadel

**Files:**
- Modify: `.env.example`, `docker-compose.dev.yml`, `Makefile`
- Modify: `scripts/lib/zitadel.mjs` and any script referencing `hms-web`, `hms-client`, `hms-seed`, `hms-login`, org `HMS`
- Modify: `.github/workflows/ci.yml` (build env vars)
- Modify: Go/TS reading `HELIVANTA_*` (Task 1 and 2 left these as string literals)

**Interfaces:**
- Consumes: Tasks 1 and 2.
- Produces: `HELIVANTA_*` env vars and `helivanta-*` Zitadel identifiers.

**The 16 variables:** `HELIVANTA_API_PORT`, `HELIVANTA_DEV_REDIRECT_URI`, `HELIVANTA_DEV_SESSION_SIGNING_KEY`, `HELIVANTA_DEV_ZITADEL_MASTERKEY`, `HELIVANTA_ENV`, `HELIVANTA_IDLE_API_PORT`, `HELIVANTA_IDLE_WEB_PORT`, `HELIVANTA_NATS_MONITOR_PORT`, `HELIVANTA_NATS_PORT`, `HELIVANTA_OPENFGA_PORT`, `HELIVANTA_PG_PORT`, `HELIVANTA_REDIS_PORT`, `HELIVANTA_TEST_CONTAINER_TIMEOUT`, `HELIVANTA_WEB_ORIGIN`, `HELIVANTA_ZITADEL_PG_PORT`, `HELIVANTA_ZITADEL_PORT`.

**Renaming the Zitadel app mints a NEW client ID.** `NEXT_PUBLIC_ZITADEL_CLIENT_ID` changes with it. This is the single most likely thing to be missed, because a wrong client ID does not fail the build — CI's own comment in `ci.yml` records that `next build` succeeds with these unset and surfaces as a **runtime 500 on `/login`**. Only Step 5 catches it.

- [ ] **Step 1: Rename the environment variables**

```bash
cd /Users/Mahesh.Sangawar/personal/tesserix-new/hms
# perl, NOT sed. Verified 2026-08-17 on this machine: BSD sed (what macOS
# ships) SILENTLY IGNORES \b — it makes no change and reports no error, so
# `sed 's|\bHMS_|...|'` is a no-op you would only notice via the grep below.
# Do NOT "fix" that by dropping the \b: an unbounded pattern is how D5's
# database identifiers get mangled. perl honours \b on macOS and Linux both.
git ls-files | xargs grep -l "HELIVANTA_" 2>/dev/null \
  | xargs perl -pi -e 's|\bHMS_|HELIVANTA_|g'
grep -rn "\bHMS_" $(git ls-files) 2>/dev/null | wc -l    # expect: 0
```

- [ ] **Step 2: Rename the Zitadel identifiers**

In `scripts/lib/zitadel.mjs`, `docker-compose.dev.yml` and any sibling script, rename `hms-web`→`helivanta-web`, `hms-client`→`helivanta-client`, `hms-seed`→`helivanta-seed`, `hms-login`→`helivanta-login`, and the org `HMS`→`Helivanta`.

Also rename the PAT paths under `dev/zitadel/secrets/` if the scripts name them (`hms-seed.pat` → `helivanta-seed.pat`). These files are gitignored; the *references* are what changes.

- [ ] **Step 3: Verify D5 was not violated**

The `sed` in Step 1 used `\bHMS_` (uppercase) so it cannot have touched `hms_app`. Confirm anyway — this is the constraint whose breach is least recoverable:

```bash
grep -rn "hms_app\|hms_session\|hms_tenant" backend/internal --include="*.go" --include="*.sql" | wc -l
```
Expected: a NON-ZERO count, unchanged from before this task. These must still be there. If it is 0, database identifiers were renamed — revert immediately.

- [ ] **Step 4: Reset the dev stack from scratch**

```bash
make down && make reset && make up
```
Expected: the stack comes up and Zitadel provisions the renamed app at first boot.

Note: leftover `next dev` processes hold ports and make turbo fail the whole group, which presents as "the app is broken" rather than "the port is taken". Check ports before debugging a dead stack.

- [ ] **Step 5: PROVE a real sign-in works**

A green build says nothing here. Complete an actual login against the reset stack:

```bash
make e2e
```
Expected: all eleven specs pass, including the login step.

If login fails with a 500 on `/login`, `NEXT_PUBLIC_ZITADEL_CLIENT_ID` is stale — the Zitadel app was renamed and minted a new client ID that the env still does not carry.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "refactor: rename HELIVANTA_* env vars and zitadel identifiers to helivanta (#863)"
```

---

### Task 4: Documentation

**Files:**
- Modify: 66 markdown files across `docs/`, `README.md`, `CLAUDE.md`, `.claude/`

**Interfaces:**
- Consumes: the identifiers settled in Tasks 1–3, so docs match reality.
- Produces: nothing code depends on.

**This task needs judgement and must NOT be a bare `sed`.** Two rules govern it.

**D1 — the name renames, the category description stays.** `HMS` expands to "Hospital Management System"; `Helivanta` expands to nothing. So:
- `README.md`'s "Hospital Management System platform — multi-zone monorepo" becomes **"Helivanta — a hospital management system platform, multi-zone monorepo"**, not "Helivanta Management System platform".
- Lowercase generic prose describing the category ("a hospital management system") stays.
- Every use of `HMS` meaning *this product* becomes `Helivanta`.

**D2 — one evidence carve-out.** In `docs/superpowers/spikes/2026-08-16-zitadel-login-client.md`, two lines are literal transcripts of API request bodies actually sent during a live spike:
- line ~152: `{"userName":"pwchange-test@hms.dev", ...}`
- line ~177: `{"auth_request_id":"V2_...","login_name":"pwchange-test@hms.dev","password":"HmsDev123!"}`

**Leave those two values exactly as they are** and add a one-line note above them: that they record what was sent on the wire before the rename and are preserved as evidence. Rewriting them would make the document assert it observed something it did not — and the handoff instructs readers to trust this spike over Zitadel's own documentation, so its evidentiary value is load-bearing. Everything else in that file renames.

- [ ] **Step 1: Rewrite identifiers and paths in docs**

Mechanical part first — code identifiers, module paths, package names, env vars and OpenBao paths quoted inside docs:

```bash
cd /Users/Mahesh.Sangawar/personal/tesserix-new/hms
# perl, not sed — see Task 3 Step 1 on why \b is a silent no-op in BSD sed.
git ls-files '*.md' | xargs perl -pi \
  -e 's|github.com/tesserix/hms|github.com/tesserix/helivanta|g;' \
  -e 's|\@hms/|\@helivanta/|g;' \
  -e 's|\bHMS_|HELIVANTA_|g;' \
  -e 's|kv/data/hms/|kv/data/helivanta/|g;' \
  -e 's|openbao-hms-api|openbao-helivanta-api|g;'
```

- [ ] **Step 2: Restore the two evidence lines**

```bash
git diff docs/superpowers/spikes/2026-08-16-zitadel-login-client.md
```
Confirm neither `pwchange-test@hms.dev` nor `HmsDev123!` was altered by Step 1 (they contain no pattern Step 1 matches, so they should be untouched — verify rather than assume). Add the one-line evidence note described above.

- [ ] **Step 3: Rewrite prose by hand, applying D1's rule**

Go through the remaining `HMS`/`hms` occurrences in prose. This is the judgement part. For each, decide: is this the product's name (→ `Helivanta`), or the category description (→ leave, lowercase)?

```bash
git ls-files '*.md' | xargs grep -n "\bHMS\b\|\bhms\b" | grep -v "hms_app\|hms_session\|hms_tenant"
```
Work the list. Do not batch-replace.

- [ ] **Step 4: Review the docs diff as a group**

```bash
git diff --stat -- '*.md'
git diff -- README.md CLAUDE.md docs/standards/
```
Read for mangled prose — the failure mode is "a helivanta platform" where the text meant the category. This is why the markdown files are reviewed together rather than skimmed inside a 324-file diff.

- [ ] **Step 5: Commit**

```bash
git add -A '*.md'
git commit -m "docs: rename hms to helivanta across documentation (#863)"
```

---

### Task 5: Infrastructure

**Files:**
- Modify: `.gitleaks.toml`, `docs/runbooks/secrets.md` (OpenBao paths)
- Modify (other repo): `tesserix-k8s` — `charts/thirdparty/openbao/values.yaml`, `argocd/prod/projects/security.yaml`
- Live cluster: recreate the namespace and ServiceAccount under the new name

**Interfaces:**
- Consumes: the path convention from Task 4's docs.
- Produces: `kv/data/helivanta/api/*`, `namespace/helivanta`, `serviceaccount/helivanta-api`.

The live objects hold **no secrets** — #45 Task 4 was deliberately held for this. Renaming is therefore create-new plus delete-old, not a migration.

- [ ] **Step 1: Open the tesserix-k8s PR**

In `/Users/Mahesh.Sangawar/personal/tesserix-new/tesserix-k8s`, on a new branch, change the `namespaceWhitelist` entry from `hms`/`hms-api` to `helivanta`/`helivanta-api`, and the `destinations` entry in `argocd/prod/projects/security.yaml` from `hms` to `helivanta`.

Note this is an **edit** of the entry added by `tesserix-k8s#365`, not a new addition.

- [ ] **Step 2: Verify the chart renders before opening the PR**

```bash
cd /Users/Mahesh.Sangawar/personal/tesserix-new/tesserix-k8s
helm template openbao charts/thirdparty/openbao -n openbao \
  --show-only templates/app-secret-stores.yaml | grep -A16 "openbao-helivanta-api"
```
Expected: `SecretStore` named `openbao-helivanta-api`, namespace `helivanta`, `role: app-helivanta_helivanta-api`, `serviceAccountRef.name: helivanta-api`.

If `helm dependency build` complains about a missing repo: `helm repo add openbao https://openbao.github.io/openbao-helm` first.

- [ ] **Step 3: Create the renamed cluster objects (CONFIRM WITH USER FIRST)**

```bash
K=gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke
kubectl --context $K create namespace helivanta
kubectl --context $K create serviceaccount helivanta-api -n helivanta
```

- [ ] **Step 4: Delete the old ones (CONFIRM WITH USER FIRST)**

Only after confirming the old namespace holds nothing:
```bash
kubectl --context $K get all,secret,externalsecret -n hms
kubectl --context $K delete namespace hms
```
Expected: the listing shows only the `SecretStore` and default objects — no secrets, no workloads. If anything unexpected is present, stop and report rather than deleting.

- [ ] **Step 5: Update the in-repo references**

`.gitleaks.toml`'s comments and `docs/runbooks/secrets.md`'s paths were rewritten by Task 4's `sed`. Verify:
```bash
grep -rn "kv/data/hms\|openbao-hms" .gitleaks.toml docs/ | wc -l   # expect: 0
```

- [ ] **Step 6: Prove the gate still works after the config edit**

```bash
PIN=$(grep -oE 'zricethezav/gitleaks:[^ ]+' .github/workflows/ci.yml | head -1)
docker run --rm -v "$PWD:/repo" "$PIN" detect --source=/repo --config=/repo/.gitleaks.toml --no-banner --redact
```
Expected: `no leaks found`.

Then prove it is not inert — in a scratch directory, copy `.gitleaks.toml`, write a file containing `aws_secret_access_key = "<40 random alphanumerics>"`, scan it, and confirm **`leaks found: 1`**. Clean alone is also what a broken gate reports.

- [ ] **Step 7: Commit**

```bash
git add -A
git commit -m "refactor: move openbao paths and k8s identifiers to helivanta (#863)"
```

---

### Task 6: Repo and directory rename

**Files:** none in the tree. GitHub settings and the local filesystem.

**Interfaces:**
- Consumes: everything above — the module path already says `helivanta`.
- Produces: `tesserix/helivanta`, and a local path matching.

Done last because it changes the path every tool in the session is using.

- [ ] **Step 1: Confirm the module path already moved**

```bash
head -1 backend/go.mod
```
Expected: `module github.com/tesserix/helivanta`. If it still says `hms`, Task 1 is incomplete — stop.

- [ ] **Step 2: Rename the GitHub repository (CONFIRM WITH USER FIRST)**

```bash
gh repo rename helivanta --repo tesserix/hms
```
GitHub redirects existing clones and issue links.

- [ ] **Step 3: Update the local remote**

```bash
git remote set-url origin git@github.com:tesserix/helivanta.git
git remote -v
```

- [ ] **Step 4: Verify push/fetch works against the new name**

```bash
git fetch origin
```
Expected: succeeds with no redirect warning.

- [ ] **Step 5: Rename the local directory (CONFIRM WITH USER FIRST)**

```bash
cd /Users/Mahesh.Sangawar/personal/tesserix-new
mv hms helivanta
```
Every subsequent command runs from `/Users/Mahesh.Sangawar/personal/tesserix-new/helivanta`.

- [ ] **Step 6: Full verification from the new path**

```bash
cd /Users/Mahesh.Sangawar/personal/tesserix-new/helivanta
make lint-go && make test-go && make coverage-go
pnpm install && pnpm turbo lint type-check test build
```
Expected: all green.

- [ ] **Step 7: Commit any path-dependent fixes**

```bash
git add -A
git commit -m "chore: rename repository and local path to helivanta (#863)"
```

---

## Definition of done

- `grep -rn "\bhms\b" $(git ls-files)` returns **only** the D5 database identifiers (`hms_app`, `hms_session`, `hms_tenant` and migration contents) and the D2 evidence lines in the Zitadel spike. Anything else is a miss.
- `make lint-go`, `make test-go`, `make coverage-go` green.
- `pnpm turbo lint type-check test build` green.
- The arch-test violation probe (Task 1 Step 4) was **observed failing**, proving the arch tests still enforce something.
- The gitleaks probe (Task 5 Step 6) was **observed firing**.
- `make e2e` green — all eleven specs, including a real sign-in against a freshly reset stack.
- `tesserix-k8s` PR merged and the renamed `SecretStore` present.

## Out of scope (do not build these)

Database identifier renames (D5); the `helivanta.app` deployment, DNS and TLS (#824); a CI guard preventing `hms` reappearing (#713); any product, UI or marketing copy beyond mechanical renaming; provisioning the secrets themselves (#45 Task 4, which resumes after this).

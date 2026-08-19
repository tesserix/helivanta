# Next session — take over Helivanta

Working directory: `/Users/Mahesh.Sangawar/personal/tesserix-new/helivanta`.
Sibling repos you will need: `../tesserix-k8s` (charts and ArgoCD),
`../kargo-manifests` (Kargo projects — **not** in tesserix-k8s), and
`../secret-service` (the OpenBao console, cloned for reference).

## Where things stand

**Slice 1b is complete and #824 is closed.** A user can sign in at
`https://helivanta.app`, and authorization actually resolves. That sentence was
not true yesterday morning, and every clause of it was separately broken.

Working and verified against production on 2026-08-19:

- **Sign-in.** Real browser round trip, Helivanta's own branded form, Zitadel
  behind it. Two accounts have done it.
- **Authorization.** `Reconcile` reads memberships and writes tuples
  (`tenant_count=1, existing_tuple_count=25`). It read **zero** for the entire
  prior life of the system.
- **The event path.** A grant through `POST /v1/iam/members` produced its
  OpenFGA tuple in **330ms** — outbox → drainer → `iam-fga-sync`. The drainer
  had never once worked in production.
- **Promotion.** Kargo `kargo-helivanta:prod` moves the running pod digest.
  Proven: `sha256:6a95303c…` → `sha256:7574408e…` with no hand-editing.
- **The rate limiter.** Not bypassable — ten forged `X-Forwarded-For` values
  earned zero new buckets, while a control burst without a header did trigger
  429s. `TRUSTED_PROXY_CIDRS=10.20.0.0/16` confirmed by observing the real
  client IP, not by inference.

Permanent facts worth not rediscovering:

```
tenant     ba97b57a-f5fb-4c3b-b357-eba3a85463aa
zitadel org TESSERIX  386377229942128837   <- the only org with Helivanta users
subjects   386888878927118733  mahesh.sangawar@gmail.com  tenant_admin
           386961292646154540  samyak.rout                tenant_admin
openfga    store 01M09YRSJDW0KAGXS56W8VQ7JN  (name "helivanta")
```

## Start here — the state of the world before you touch anything

1. **All three repos are PUBLIC right now.** They are flipped public so GitHub
   Actions run, because billing is failing. Flipping them private stops CI
   dead. If you flip them, flip them back before expecting a merge to build.
2. `gh issue view 913` — the top pick. Both of its blockers were resolved
   empirically on 2026-08-19; it is ready to implement, not to investigate.
3. `docs/superpowers/plans/2026-08-18-deployment-slice-1b.md` — Tasks 8 and 9
   carry the full record of what was proven and how.

## Blockers, in order

### 1. GitHub Actions billing

Not a code problem, and it gates everything. While billing fails, a private
repo's jobs do not start — they report `failure` in ~2s with
"recent account payments have failed or your spending limit needs to be
increased", including jobs that never ran. **Those red checks are not signal.**
Read the annotation before diagnosing anything.

The workaround in use is flipping repos public. It works and it is not free:
the repos are public *right now*.

### 2. #913 — MFA policy is read against the wrong organization (fail OPEN)

`CompleteIfSufficient` reads the login policy unscoped, so it resolves against
the login client's org rather than the authenticating user's. `sufficiency.go`
called this out itself and said "Adding a second org to this instance REQUIRES
fixing this first." **The instance has three orgs.** Nothing is exposed today
because only TESSERIX has Helivanta users and it does not force MFA — the
exposure arrives on a *configuration* change, with no error anywhere.

Zitadel does not itself refuse to finalize a password-only session against a
`forceMfa` policy. This read **is** the enforcement.

Both unknowns the code cited are now answered, tested live:

- Zitadel honours `x-zitadel-orgid` on `GET /management/v1/policies/login`
  (two orgs return two different `resourceOwner`s).
- The session carries `factors.user.organizationId`. The spike captured
  `factors: {user, password}` and did not record it, which is why it was
  believed absent.

Do not let an absent org id fall back to an unscoped read. That is the present
bug, and the fallback would reproduce it for exactly the users hardest to
notice. `do()` takes no per-request headers today; this is the first caller
that must scope a request and will not be the last.

### 3. #912 — nothing refuses an unsigned image at admission

Images are cosign-signed (keyless) and Trivy-scanned, and no admission
controller checks any of it. Kyverno **is** installed (two working
ClusterPolicies), so the mechanism exists.

The blocker is registry asymmetry: images are signed at `ghcr.io` and pulled
from the GAR `ghcr-remote` pull-through mirror. Cosign stores a signature as a
separate `sha256-<digest>.sig` tag *in the same repository*, and pull-through
mirrors are lazy. **Whether that tag exists at the GAR path is unverified** —
I could not check it, because neither the default SA nor `helivanta-api` has
GAR read via workload identity. Resolve that first. A `verifyImages` policy in
`Enforce` mode built on the wrong assumption rejects every pod in the namespace.

Audit mode first, then Enforce, with the audit result stated in the PR.

### 4. Everything else

- **#904** — PHI masking marshals unbounded values. A struct with references
  shared ~25 deep emits a multi-gigabyte log line from one `slog` call.
  Measured: depth 20 is 88 MB of JSON.
- **#901** — a failed sign-in logs a two-valued outcome, and `bad_credentials`
  means *any* HTTP 400. Diagnosing a real login failure required Zitadel's logs
  because ours could not answer. Sibling of #856.
- **#855 / #861** — no account lockout, nothing throttles pre-auth. Both sit on
  the surface #824 Task 9 just measured.
- **PR #898** — dependabot, `moby/go-archive`. Fine to merge now CI runs.

## Standing rules — do not trip over these

- **`tesserix-k8s#392` must NEVER be merged.** It grants
  `helivanta/scope-probe`, and merging it destroys the 403 that is the only
  evidence the OpenBao grant is bounded. An over-broad policy passes every
  positive test identically. Note #412/#413/#414/#422/#463 have near-identical
  generated titles — **read the app name, never the title**. Those five are
  empty no-ops from the secret-service console (`tesserix/secret-service#16`)
  and can be closed.
- **`instances: 1` with backups off** must be raised before a patient record
  exists.
- **Instance-wide Login V2 is Disabled.** `Management Console` needs an
  explicit V2 setting to keep working; removing it locks the operator out of
  the Zitadel console, which happened once.
- **Zitadel `PUT .../oidc_config` is a full replace, not a patch.**
- **No secret value passes through an agent session.** Read secrets inside a
  pipeline or a shell variable; never print them. `iam-admin-pat` in the
  `zitadel` namespace gives Zitadel admin API access this way.
- **The frontend gate is FIVE tasks**: `pnpm turbo lint type-check test build
  format:check`. Running the four it used to document is how a prettier
  violation reached `main`.
- **`helivanta_system` is the only role that may cross tenants.** BYPASSRLS,
  no DDL, allowlisted to three files by
  `TestWithAllTenantsIsOnlyCalledFromTheAllowlist`. `WithAdmin` is the schema
  OWNER and does **not** bypass RLS — its comment claimed it did for months.
- **Grants for `helivanta_system` live in migrations, per table.** BYPASSRLS
  confers no table privileges. The harness deliberately grants it nothing, so a
  fixture that forgets the grant fails exactly as production would.

## How defects were actually found — this is the useful part

Yesterday's handoff said every significant bug was a confident written claim
its own cited evidence did not support. That held again, and a second pattern
joined it.

**A success signal that moved nothing.** Four times:

- ArgoCD reported `Synced` at a revision *predating* the change — three
  separate times, and it cost real minutes each time. `Synced` is true about a
  revision, not about your commit. **Check `.status.sync.revision`.**
- A green `Images` workflow meant an artifact existed, not that anything
  deployed it. Three merges sat undeployed behind a hand-pinned tag while every
  pod reported `Ready`.
- #906 was **fully green** — lint, tests, images, Trivy — and shipped a tag
  (`sha-<40>` instead of `<40>`) that would have wedged the first promotion.
  Green CI said the workflow ran, not that it produced the name something else
  depended on.
- A `202` from `POST /v1/iam/members` said the request was accepted. The tuple
  is what said it worked.

**A failure signal that meant nothing.** The inverse bit once: an OpenFGA read
filtered by user returned **0 tuples**, which reads exactly like "the fix
failed". It was a bad filter shape; the unfiltered read had the tuple. Verify a
negative result as carefully as a positive one.

**A harness more permissive than production manufactures confidence.** #894
existed because `testinfra` made the owner a superuser, so `FORCE ROW LEVEL
SECURITY` never bound it under test. The *first* fix repeated the mistake by
granting the system role privileges production did not have — a fix for "the
harness is too permissive" that was itself too permissive. Four fixtures then
failed with the real production error, which is the harness finally being
honest.

**Prove the mutation happened.** A `sed` whose pattern missed a trailing
backtick left a file unchanged, the test "passed", and that pass was worthless.
Check the edit landed before trusting what the test then says.

## Verification habits this repo expects

- Prove a test can fail — mutate the implementation, watch it fail, revert.
  Then confirm the mutation actually applied.
- Server-side dry run (`kubectl apply --dry-run=server`) before touching the
  cluster. Note a Job reports `field is immutable` under `apply` — that is an
  artifact of `apply`, not a blocker; ArgoCD deletes and recreates PreSync hooks.
- After any change, read the live object back and assert on the specific field.
  Never close a loop on the mutating call's own return value.
- CI is real signal again **only while the repos are public**.

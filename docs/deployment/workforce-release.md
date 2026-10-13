# Workforce foundation release

Status: local implementation and validation passed. The user approved publication, migration and API rollout after backup readiness. Backup infrastructure has been provisioned; GitOps activation is awaiting the required pull-request path. The workforce migration and API rollout have not run.

## Backup readiness progress

On 2026-10-13, an isolated GitOps worktree produced commit `c780ef85` on `feat/helivanta-backup-readiness` in `tesserix-k8s`. A reviewed, targeted Terraform plan added seven resources with zero changes or deletions: a private regional Helivanta backup bucket, separate backup/restore service accounts, exact Kubernetes workload-identity bindings and bucket-scoped permissions. Restore access is read-only. The bucket has versioning, seven-day soft deletion and destruction protection.

The GitOps change enables six-hourly CNPG base backups, an immediate first backup, 30-day retention and narrowly selected GKE metadata egress. Terraform mock tests and rendered Helm tests failed before implementation and passed afterward; `terraform validate`, formatting and `helm lint` passed. The targeted plan was used to avoid unrelated estate changes; no whole-estate reconciliation is claimed.

GitHub rejected the direct `main` push because it requires a pull request and `atlantis/apply`. The branch is published; approval to open/merge PRs and post required Atlantis commands is pending under the user's AGENTS instructions. Argo CD still runs the previous configuration. A successful base backup and isolated restore remain required before publishing the automatically promoted API release.

## Observed target

Read-only discovery on 2026-10-13:

- Active GCP account: `unidevidp@gmail.com`.
- Project: `tesseracthub-480811`.
- Context: `gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`.
- Namespace: `helivanta`.
- CNPG `helivanta-postgres`: two ready instances, healthy (one primary and one standby), PostgreSQL 16.4.
- Argo CD `helivanta-api`: Healthy/Synced, image parameter `7a3e31266ce85ae1f8873ee9b6454fcb5bc90367`.
- `0001_workforce` is not in the live migration ledger.
- Live `hms_app` is neither superuser nor BYPASSRLS; `helivanta` is a non-bypass schema owner and `helivanta_system` has the expected dedicated bypass privilege.
- The existing deployed API returned 200 from `/healthz` and `/readyz`. The new workforce path returned route-not-found, confirming this code has not been deployed.
- No CNPG Backup or ScheduledBackup resources were returned in this namespace; previous cluster status also did not report a last successful backup. This does not prove that every possible external backup mechanism is absent, but recoverability has not been verified.
- The live Cluster has no backup or backup-plugin specification. A read-only listing of the configured GCS destination prefix returned no matching objects. Its `ContinuousArchiving` condition alone is not evidence of a recoverable base backup.

The owning chart is `tesserix-k8s/charts/apps/helivanta-api`; Kargo owns its promoted image parameter. Do not patch the live Deployment or edit a chart default expecting it to override Kargo's parameter. The GitOps repository has unrelated working-tree changes that must remain untouched.

## Local validation completed

From `backend/`, with the existing Colima Docker socket selected explicitly:

```sh
DOCKER_HOST=unix:///Users/samyakrout/.colima/default/docker.sock \
TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock \
GOTOOLCHAIN=go1.26.6 go test -race -p 2 ./...
GOTOOLCHAIN=go1.26.6 go test -race ./internal/config
GOTOOLCHAIN=go1.26.6 golangci-lint run ./...
GOTOOLCHAIN=go1.26.6 go vet ./...
GOTOOLCHAIN=go1.26.6 go build ./...
```

The full race suite passed. The configuration regression suite was rerun after removing raw invalid environment values from three existing warning messages; fallback behavior is unchanged. Full lint reports zero issues. The workforce integration tests include real Postgres with non-owner/non-bypass serving credentials, signed-session network HTTP requests and real OpenFGA membership revocation. Initial red tests established the missing module, missing registration and unsafe warning output before implementation.

The workstation defaults to Go 1.27, which caused unrelated logging tests to fail due to changed JSON tag handling and was incompatible with the installed linter's build toolchain. Validation above uses the repository's declared Go 1.26.6. The production CI/image must use the declared version too; Go 1.27 compatibility is not claimed.

From the repository root, the existing Dockerfile successfully built the final source:

```sh
docker build -f Dockerfile.api -t helivanta-api:workforce-dev \
  --build-arg VERSION=workforce-dev --build-arg COMMIT=working-tree .
git diff --check
```

Local image ID: `sha256:2bf826cc4bcd8e50277be61b1a7ab277cfe5fc0a56a7b3b44fdbcd23aec81dc4`, arm64, UID 65532. This is a local validation artifact, not the signed CI image for GKE. Both the API and migration binary compiled in that image. Changed Markdown passed Prettier. No legacy records, private keys, bearer tokens or production data were added to the change.

## Release content

One additive migration, `0001_workforce`, and six workforce routes documented in [the API contract](../api/workforce.md). No old table changes, source data import or identity provisioning. Both API and migration binaries are included in the existing API image. The API also invokes the idempotent migration runner during startup, so rolling out this binary is itself a schema-changing action even when the chart's migration hook is omitted.

## Before production mutation

1. Complete all local tests, build and security checks; record unrelated baseline failures separately.
2. Verify backup/restore readiness or choose a separately approved non-production deployment target. Do not equate a standby with a backup.
3. Obtain the specific approval required by the workspace AGENTS instructions for applying `0001_workforce` to this database and rolling out `helivanta-api` in this production namespace. Git commits/pushes also require authorization under those instructions.
4. Commit only reviewed Helivanta changes with the repository-matched identity; publish through its normal CI build/scan/sign workflow. Record immutable source revision and image digest. Do not promote an uncommitted local image under an existing tag.
5. Promote through the existing Kargo/Argo CD path. Observe the migration job, API rollout, `/healthz` and `/readyz`, and verify all four new tables carry forced RLS.
6. Provision the test organization through the existing onboarding service, checking product registration, owner invitation and grant warnings. Verify its tenant/membership projection before granting hospital access. YES HOSPITAL's owner email remains required.
7. Run the six endpoints using synthetic records and real signed sessions. Prove 401 for missing/invalid sessions, 403 for nonmembership, 404 for another tenant/sibling hospital's object, reader write denial, explicit revocation, cursor bounds and unchanged PUT retry behavior. Confirm readiness and error rates remain healthy.

The new tables can remain during an approved application rollback to the previously observed image; do not drop them or undo audit records. A rollback is a separate live action requiring its own approval. No restore, rollout, production smoke test or load test is claimed by local validation.

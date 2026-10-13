# Workforce foundation — deployment evidence

Status: deployed on 2026-10-13. This is the hospital staff-directory foundation, not a completed HMS or automated onboarding integration.

## Deployed release

- Source commit: `76cf9ff3a83a54e8b4ba601c3e4c1c81f634de45`, [release PR #1083](https://github.com/tesserix/helivanta/pull/1083).
- Running API digest: `sha256:0df726068f13bdd47d2eb66237780583b69304482d80e291259f00d01caf361e`.
- Two API replicas available; Kargo promotion `prod.01m4st5h0sgswb4h99zrafb773.26d3014` succeeded through the existing deploy-branch/Argo CD pipeline.
- Migration `0001_workforce` is recorded in the production migration ledger. All four workforce tables have both `ENABLE ROW LEVEL SECURITY` and `FORCE ROW LEVEL SECURITY` enabled.
- Public API prefix: `https://helivanta.app/api/v1`. Istio rewrites this to the service's `/v1` prefix.

The authorized target was project `tesseracthub-480811`, context `gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`, namespace `helivanta`, cluster `helivanta-postgres`. CNPG remains two healthy instances: one primary and one standby.

## Backup and recovery evidence

[GitOps PR #1376](https://github.com/tesserix/tesserix-k8s/pull/1376) enabled six-hourly base backups and continuous WAL archiving to `gs://tesseracthub-480811-helivanta-backups-prod/cnpg`, with 30-day retention. The private regional bucket has uniform access, public-access prevention, versioning, seven-day soft deletion and Terraform destruction protection. Dedicated backup and restore identities have exact namespace/service-account workload-identity bindings; restore object access is read-only.

The initial Terraform plan added seven resources without changing or destroying existing resources. Atlantis subsequently verified and applied a no-change targeted plan. [PR #1377](https://github.com/tesserix/tesserix-k8s/pull/1377) added two bucket-scoped metadata-reader bindings after Barman demonstrated that object permissions alone do not include `storage.buckets.get`.

A completed backup status alone was insufficient: the first standby backup referenced a WAL segment from before GCS archiving was enabled. The isolated restore rejected it. A checkpoint and WAL switch established a new archived boundary, followed by a fresh primary backup:

| Evidence             | Result                                                           |
| -------------------- | ---------------------------------------------------------------- |
| CNPG Backup resource | `helivanta-workforce-primary-20261013`                           |
| Barman backup ID     | `20261013T045540`                                                |
| Restore Job          | `helivanta-restore-20261013t045540-retry2`                       |
| PostgreSQL recovery  | Completed; `pg_is_in_recovery()` was false                       |
| Verification         | Migration ledger present; serving role did not bypass RLS        |
| Final output         | `restore_check_passed`                                           |
| Cleanup              | PostgreSQL stopped; restored data removed from temporary storage |

The restore Job uses the pinned PostgreSQL image, temporary storage, no production volumes or credentials, and no TCP listener. [PR #1378](https://github.com/tesserix/tesserix-k8s/pull/1378) fixes copied read-only configuration handling, reports startup diagnostics and waits for completed recovery with a bounded poll. Historical failed diagnostic Jobs remain as evidence; they are not running workloads.

The successful restore preceded the workforce migration. This demonstrates recovery of that backup at its earliest consistent point; it does not establish a measured full-scale RTO, an arbitrary-time PITR drill, regional disaster recovery, or compliance certification.

## Verification performed

- Full backend race suite: 28 packages, Go 1.26.6, real PostgreSQL and OpenFGA.
- Full backend lint: zero findings; `go vet ./...` and `go build ./...` passed.
- Config log-redaction regression test passed after failing first.
- Workforce tests cover cross-tenant and sibling-hospital isolation, grant revocation, reader write denial, bounded input, idempotent retries, audit immutability and real HTTP requests using signed sessions.
- PR CI, including browser end-to-end tests, passed before merge. Release images were built, scanned and signed by CI.
- Terraform tests and rendered Helm tests passed after failing first; `terraform validate`, `terraform fmt`, `helm lint`, shell syntax and `shellcheck` passed. Release changes passed secret scanning.
- Deployed `/healthz` and `/readyz` returned 200.
- All six public workforce routes returned 401 without credentials. An invalid bearer token also returned 401 against the deployed service.
- A live transaction under `hms_app` verified hospital isolation, tenant isolation, missing-grant denial, reader write denial and append-only audit. All synthetic fixture writes were rolled back. See [the SQL check](../../scripts/sql/workforce-smoke.sql).

The local backend commands were:

```sh
cd backend
DOCKER_HOST=unix:///Users/samyakrout/.colima/default/docker.sock \
TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=/var/run/docker.sock \
GOTOOLCHAIN=go1.26.6 go test -race -p 2 ./...
GOTOOLCHAIN=go1.26.6 go test -race ./internal/config
GOTOOLCHAIN=go1.26.6 golangci-lint run ./...
GOTOOLCHAIN=go1.26.6 go vet ./...
GOTOOLCHAIN=go1.26.6 go build ./...
```

## Synthetic tenant and remaining integration

[YES HOSPITAL](https://onboard.tesserix.app/orgs/8f8ed375-596b-4633-b90b-27fd3e6f299c) was created through the existing onboarding service, with no direct tenant-table insert:

- Tenant UUID: `8f8ed375-596b-4633-b90b-27fd3e6f299c`.
- Zitadel organization: `394886004164526645`.
- Initial owner: `yes-hospital-owner@example.invalid`, replaced after the first workforce release; retained without organization membership.
- Active owner: `yes-hospital-test-owner@example.invalid`, subject `394892763469972021`; organization-owner membership and active state verified.
- Plan: `synthetic-test`; external reference: `helivanta-synthetic-yes-hospital`.

The replacement owner is active and has a one-time HMS tenant_admin bootstrap grant, as detailed below. OpenFGA convergence and signed-in production CRUD remain pending the next API rollout. The operator has no Helivanta product registration or automatic operator-to-IAM membership projection. Real signed-session HTTP isolation tests ran against the test stack; live gateway rejection and database isolation checks are separate evidence.

The next implementation slice is dedicated product-machine authentication, verified product entitlement and organization/owner projection into HMS, followed by a signed-in synthetic-user live CRUD/isolation test. The existing login-client machine identity must not be reused as the product provisioning identity. Employment records must not imply access grants.

No legacy customer records were imported. The generated fixture format remains an interchange artifact, not a production seed script. Detailed legacy employee creation and credential fields still need read-only verification before expanding the staff model.

## Owner activation and workspace follow-up — 2026-10-13

The replacement synthetic owner `yes-hospital-test-owner@example.invalid`, subject `394892763469972021`, is `USER_STATE_ACTIVE` with verified email and `ORG_OWNER` in the existing YES HOSPITAL organization. A real Zitadel password check succeeded and its verification session was revoked. Credentials live in OpenBao at `helivanta/helivanta-api/helivanta-synthetic-owner`; no credential values are in this repository. The original subject is retained without organization membership; pre-change membership metadata was captured locally in `/tmp/helivanta-owner-membership-before.json`.

The existing `cmd/bootstrap` assigned the active subject `tenant_admin` for tenant `8f8ed375-596b-4633-b90b-27fd3e6f299c`, using the application database role and tenant RLS context. OpenFGA convergence occurs at the next API startup. This is an explicit one-time test setup, not automatic operator integration.

The new workforce/branding UI and additive `0001_workspace` migration are under development on `feat/workforce-branded-workspace`. Local browser verification used an isolated development stack with synthetic records: sign-in, hospital creation with explicit self-grant, staff creation/edit/reload, branding publish/reload and the branded login link. These checks do not claim a production UI rollout or signed-in production CRUD. See [workspace API and release contract](../api/workspace.md).

## Workspace release verified — 2026-10-13

PR [#1085](https://github.com/tesserix/helivanta/pull/1085) merged as `0519e273245cd85c796c97bcf3c327eb4adef884`. Image workflow `38360408399` built, scanned and published both images and advanced the deploy branch. Kargo promotion `prod.01m4t1fjfe0b0qh9kevry94dka.3d1cf25` succeeded. Argo CD reports both API and shell Synced/Healthy, each with two ready replicas on that source tag; CNPG retains two healthy instances. The workspace PreSync migration Job succeeded.

A real public OAuth authorization-code/PKCE flow authenticated `yes-hospital-test-owner@example.invalid`, verified the signed ID token, exchanged it for a Helivanta session and confirmed the YES HOSPITAL tenant membership. Through `https://helivanta.app/api/v1`, the check created or reused `YES Main Hospital (Synthetic)`, granted the owner explicit hospital-manager access, and created/read/updated `Maya Chen (Synthetic)` (`YES-SYN-001`, ending inactive). A switch to an inaccessible tenant and a staff lookup under an unrelated hospital both returned 404. Tenant branding was published as YES HOSPITAL / blue / split and matched both authenticated and public branding reads. The Helivanta verification session was signed out through the same-origin logout endpoint. All records were synthetic; no legacy customer records or secrets were captured in the repository. The read-only OpenBao token was revoked and its local port-forward closed.

This proves the activated test owner's production workspace access and startup membership convergence. It does not implement automated signup or product registration. The public landing page is separate work and is not included in this production release. See [ADR 0011](../adr/0011-public-site-and-tenant-addresses.md).

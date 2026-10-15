# Private staff files: local verification, 15 October 2026

Owning story: [#1105](https://github.com/tesserix/helivanta/issues/1105).
Parent: [#979](https://github.com/tesserix/helivanta/issues/979).
This is local processing/delivery evidence, not completed-story or production acceptance.

## Latest application and applied-IaC evidence

Approved infrastructure [#1415](https://github.com/tesserix/tesserix-k8s/pull/1415)
was applied through Atlantis and merged at `f01b19c49c4638ecddc949d3d39b2880582da2f9`.
Its 16 removals eliminated seeded legacy IAM bindings from the four private-file
buckets. The authorized Cloud Asset IAM view confirmed that intended get/create
workload grants remain and public/legacy bucket grants are absent. Direct storage
policy reads correctly became unavailable to the active account; no access was
regranted. Before/after recovery captures remain private under
`/tmp/helivanta-bucket-iam-{before,after}-hardening-20261015.json`.

Argo reconciled the preparatory PostgreSQL, API, quotas, network policies,
Temporal and OpenBao resources as Synced/Healthy at `f01b19c`. CNPG has two ready
instances. Worker LOGIN, credential projection, uploads and workloads remain
disabled. No production migration or application promotion was performed.

The staff UI and API share generated document contracts. Record choices require
document permission and an explicit hospital-manager grant, omit license numbers
and the full enrollment, and are bounded to one enrollment plus twenty credentials
and twenty qualifications. Expected-tenant constraints reject all seven document
operations when the verified session no longer matches the workspace. Upload and
scan retries retain their original intent/attempt after lost acknowledgment.
Unmount, hospital/staff selection and tenant switching clear/cancel private work.
Filename errors appear inline before hashing or network I/O.

Local web checks used Node 24.21.0, Next.js 16 and Tailwind 4:

- `pnpm turbo lint test format:check`: API **79**, UI **57**, shell **176** tests pass.
- `pnpm turbo build`, followed by `pnpm turbo type-check`: pass. Sequential
  build/typecheck avoids concurrent Next-generated type-directory churn.
- `/tmp/helivanta-go-tools/golangci-lint run` (v2.14.0 built with Go1.27.2),
  `go vet ./...` and `go build ./...`: pass.
- Full Go `./scripts/coverage-gate.sh` with **Go1.27.2**, `-race -count=1 -cover
./...`, `GOFLAGS=-p=2`, correct repository layout and real disposable fixtures
  inside Colima: passes. Workforce **78.8%**, attachments **81.5%**, events
  **77.1%**, tenantdb **83.8%**. Earlier attempts encountered a portable test-helper
  compile error and kernel-confirmed architecture-test OOM; neither is a pass.
  The stdout-capture helper now uses `x/sys/unix` on Linux ARM64 as well as Darwin.

Existing isolated CNPG verification roles expired on 14 October. No password
rotation or expiry extension was made; specific renewal approval is pending.
A separate disposable local database, isolated OpenFGA store and dedicated
JetStream server were prepared for browser verification. All eight migrations
were applied only to that disposable database. No customer records were copied.

Actual API acceptance at `localhost:8086` used real Zitadel sign-in and synthetic
enrollment/credential/qualification fixtures. Record choices and file-list reads
returned no-store; expected-tenant mismatch returned 404; protected personal fields
and license numbers were absent. Actual synthetic rows were visible at
`http://helivanta.localhost:4306/workforce` through the new API. Sign-in used the
existing registered local callback at port 4301; the development hostname shares
the session with the isolated UI port. No identity-provider registration changed.

Browser acceptance verified Documents focus, expanded/collapsed navigation, the
actual reusable hospital dropdown, private-workspace clearing on hospital change
and disabled uploads while storage is inactive. At 412×915 the document panel fit
without page-level horizontal overflow; the viewport override was reset. Browser
console had no errors/warnings. Screenshots:
`/tmp/helivanta-staff-documents-local-20261015.jpg` and
`/tmp/helivanta-staff-documents-mobile-20261015.jpg`.

The legacy EMR shell was visible, but its HR switch did not load HR. Main-portal
navigation reached login again. No customer export was performed; fresh legacy
HR verification requires renewed sign-in.

The [design](superpowers/specs/2026-10-14-private-staff-files-design.md) and
[editable draw.io diagram](diagrams/private-staff-documents.drawio) describe the
processing boundaries. These local checks do **not** prove actual GCS IAM/CMEK,
production scanner receipt/recovery, CNPG migration, monitored live runtime or
deployed browser acceptance. Story #1105 remains open.

The sections below preserve earlier processing checkpoints; this section is the
current continuation status.

## Implemented boundary

The workforce module owns intent, list and bounded binary upload routes.
Each requires `workforce.documents.manage` and a current explicit hospital
manager grant. Enrollment permission alone grants no document access.
The independently pinned system-role matrix currently maps this sensitive
permission to tenant administrators only; administration never substitutes
for hospital access.

The binary request must match the immutable intent's media type, size and
SHA-256. Content encoding is rejected. Admission fully validates PNG/JPEG;
PDF, Office and archives remain unsupported. A database clock fixes the UTC
ownership path before storage. Create-only quarantine writes run outside
database/grant locks; a lost receipt reconciles only identical bytes at the
exact immutable generation. Finalization rechecks the grant, record binding,
version and expiration, then commits the generation and opaque scan intent in
one transaction. Conflicts/uncertainty never make a file downloadable.

## Regression and integration evidence

Observed failures before implementation/correction:

- The missing binary route returned 404.
- Quarantine receipt reconciliation did not exist.
- An enrollment-only manager could access document routes.
- The architecture checks rejected unclassified scan references and the
  undocumented new permission's role mapping.

Real PostgreSQL and NATS JetStream tests now verify:

- Cross-tenant, hospital-reader and enrollment-only permission denials.
- Declared type, encoding, short/extra bytes and checksum mismatches never
  reach cloud storage.
- Exact retries preserve a receipt and publish no duplicate scan intent.
- Six simultaneous uploads produce one committed scan handoff; losers
  reconcile the pinned object or receive its existing receipt.
- Revoking the hospital grant during the storage request completes without
  waiting for database/grant locks and prevents finalization/publication.
- The real dispatcher drains committed rows into the durable JetStream
  consumer; only the two committed file IDs arrive with the correct tenant,
  hospital and staff scope. No filename, checksum or storage path is sent.

GCS operations here use the vendor SDK against an HTTP protocol simulator.
They are not proof of actual GCS IAM, encryption, audit or workload identity.
Real isolated ClamAV acceptance additionally passes benign, standard and
compressed antivirus fixtures and bounded oversize handling.

The existing isolated CNPG database `helivanta_verify_20261013_04` was reached
through the established localhost `15435` port-forward. `go run ./cmd/migrate`
applied and linted the committed intake migrations through `0007`, including forced document RLS
and directed-event provenance. The schema owner and application connection
roles are neither superusers nor `BYPASSRLS`; the separate system connection
is the intentionally checked platform dispatcher role. No production tenant
rows or customer fixtures were altered. Credentials and runtime JSON stay
outside Git.

## Commands and gates

Go commands use `GOTOOLCHAIN=go1.27.2` and the isolated Docker daemon with
Ryuk enabled:

```sh
go test -race ./internal/modules/workforce ./pkg/attachments -count=1
go test -race ./internal/modules/workforce -run TestStaffDocumentUpload -count=1
./scripts/coverage-gate.sh
go vet ./...
go build ./...
golangci-lint run
govulncheck ./...
go test -tags clamav_acceptance ./pkg/attachments -run TestRealClamAV -count=1
```

The complete race/coverage gate passed: workforce 81.4%, attachments 82.3%.
Supplementary concurrency and actual JetStream delivery tests also pass under
the race detector. Vet/build/lint pass. Vulnerability scanning finds no
reachable or imported-package findings and one uncalled required-module
finding; this is not an all-level zero-findings assertion.

The active local checkout's development preflight passes. Running it in the
isolated feature checkout correctly reports the other checkout's occupied
8081/4301 ports; those working user sessions were preserved. Web checks use
Node 24 with frozen-lockfile dependencies: `pnpm lint`, `pnpm type-check`,
`pnpm test` and `pnpm build` pass. Build-time public Zitadel values
must match the repository CI fixture; the credentialless fixture does not
prove runtime sign-in. The existing local/live roster screens are signed in
as YES HOSPITAL and show the synthetic weekly template. No private-file UI
has been implemented or verified yet.

## Processing and delivery continuation

The new `0008_workforce_document_worker` migration requires the distinct,
non-elevated `helivanta_document_worker` PostgreSQL identity and all four
transaction-local ownership UUIDs. A reproduced API-session-flag forgery can no
longer manufacture an available verdict. The runtime-only pool has no owner
pool and refuses elevated, inherited-owner and schema/database-creation rights.
Worker column grants exclude personnel tables and original filenames, and its
audit/outbox grants permit only the current scoped document's service actions.
This migration has run against disposable PostgreSQL fixtures, **not** the
existing CNPG verification database or production.

The owning processor now persists terminal malware/integrity rejection and
observable scanner/storage errors. It commits each state and its outbox event
atomically, suppresses repeated identical error events, and reconciles a
create-only copy when a real worker database pool is deliberately closed after
the external copy commits. Revoked, rejected and available receipts do no new
remote work. An explicit retry increments the attempt once; old attempts return
`superseded` and duplicate requests return the original receipt.

Temporal SDK v1.49.0 tests use the real processor/database with vendor protocol
fixtures. They verify successful release and exactly five attempts on persistent
scanner failure. The actual local Temporal server at localhost `18233`, real
NATS JetStream dispatcher/consumer and actual worker registration additionally
verify a single deterministic execution and rejection of a conflicting retained
execution. Duplicate starts verify the initial history's exact scope, workflow
type and queue before acknowledgment. Durable history receives only opaque IDs,
bounded status and generic errors. Activities heartbeat with no document details.

The private generation-pinned binary download, idempotent revocation and explicit
retry endpoints require the same independent document permission and current
hospital-manager grant. Downloads read remotely outside locks and reauthorize
before bytes/audit are served. Deliberately removing that second authorization
returned 200 after grant revocation; restoring it returns 404 with no successful
download audit. Deliberately disabling immutable intent matching failed the
changed-bytes-before-promotion test. Both restored tests pass. Download headers
are private/no-store, nosniff, attachment disposition and a restrictive CSP.

API boot refuses partially configured storage/Temporal. The separate
`/app/document-worker` command uses only the runtime credential, private GCS
identity, scanner and Temporal; it has bounded concurrency, heartbeats, shutdown
and dependency readiness checks. It is built in the existing company-base API
image, so worker, API and migrations share one promoted artifact. No new CI/image
pipeline or replacement base image was introduced.

Latest continuation checks actually run:

```sh
go test -race ./...
./scripts/coverage-gate.sh
go vet ./...
go build ./...
golangci-lint run
govulncheck ./...
go test -race -tags acceptance ./internal/modules/workforce -run 'TestDocumentJetStream|TestDocumentConsumerRefuses' -count=1
go test -tags clamav_acceptance ./pkg/attachments -run TestRealClamAV -count=1
```

The initial full race run found the single-binary GET missing from the explicit
non-collection allowlist; that classification was corrected. The subsequent full
race/coverage gate passes: workforce **78.1%**, attachments **81.2%**, events
**78.0%**, tenantdb **83.8%**. Vet/build/lint pass, including correction of an
unchecked GCS client close. The latest vulnerability result remains zero
reachable/imported-package findings and one uncalled required-module finding.
Actual isolated ClamAV and local Temporal/JetStream acceptance pass. GCS remains
a vendor protocol fixture in these tests; they do not prove production IAM.

A further regression observed three separate scanner/probe connections. The
client now uses one numbered IDSESSION for both VERSION checks and INSTREAM, so
a service cannot silently substitute another replica's provenance. The new
connection-bound test fails with the old implementation and passes with the
session client. Full attachment race tests and actual isolated ClamAV benign,
EICAR/compressed-EICAR acceptance pass with this protocol.

Read-only production discovery confirms account `unidevidp@gmail.com`, project
`tesseracthub-480811`, context
`gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`, namespace `helivanta`:
CNPG has **two instances, both ready**, and is healthy. No live mutation was
issued. Infrastructure code registers the future worker role without LOGIN,
membership, elevated privileges or a password; its Helm identity test failed
before the addition and passes afterward. Private credential/login activation
requires a reviewed separate rollout before this worker can run.

The legacy Holiday Master remained visible, but opening Employees redirected to
the login page; a new sign-in has been requested. No customer values were copied.

At head `f042874`, CI web/e2e, script/security checks and reusable image builds
passed, with deployment advancement skipped. CI Go reported a core-NATS timeout
in the existing tenant-less outbox test. Its subscriber used a different socket
from the publisher and did not wait for subscription registration. The one-line
flush barrier correction passes all three affected dispatcher/isolation tests
under `-race -count=10`; current-head CI is being rerun. This is not a weakened
assertion or an increased receive timeout.

At that checkpoint, runtime IaC and frontend work remained outstanding. Their
current status is recorded above. #1105 and the parent epic remain open.

## Infrastructure and remaining acceptance

[Infrastructure #1414](https://github.com/tesserix/tesserix-k8s/pull/1414)
owns the isolated `17-helivanta-files` state and worker-role pre-registration.
The old shared-stack
plan contained 11 unrelated deletions; all its saved plans were discarded.
The refreshed [saved isolated plan](https://github.com/tesserix/tesserix-k8s/pull/1414#issuecomment-6173657955)
had **17 additions, no changes and no deletions** and was subsequently approved,
applied and merged. Approved follow-up #1415 hardened seeded bucket IAM and
reconciled the disabled runtime preparation.
Native Terraform assertions, 45 Atlantis/graph tests, formatting/lint and
the prior infrastructure CI pass. The worker-role addition has its own Helm
render/identity/backup tests. The preparatory provisioning approval and apply are
complete; activation remains separate.

Still required before release or issue closure: private worker credential and
login activation, scanner receipt/recovery acceptance, monitored runtime and
effective audit evidence, real GCS acceptance, full local
CNPG/API/browser flow and exact-source post-deployment synthetic tests/logs.
The backend PR remains draft and production upload intents remain unavailable
when the runtime store is unconfigured. No owning story is closed by this
evidence.

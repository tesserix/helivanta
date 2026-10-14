# Private staff files: local verification, 15 October 2026

Owning story: [#1105](https://github.com/tesserix/helivanta/issues/1105).
Parent: [#979](https://github.com/tesserix/helivanta/issues/979).
This is foundation evidence, not completed-story or production acceptance.

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
applied and linted the registered migrations, including forced document RLS
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

## Infrastructure and remaining acceptance

[Infrastructure draft #1414](https://github.com/tesserix/tesserix-k8s/pull/1414)
now owns only the new `17-helivanta-files` project/state. The old shared-stack
plan contained 11 unrelated deletions; all its saved plans were discarded.
The new [saved isolated plan](https://github.com/tesserix/tesserix-k8s/pull/1414#issuecomment-6169769836)
has **17 additions, no changes and no deletions**. It has not been applied.
Native Terraform assertions, 45 Atlantis/graph tests, formatting/lint and
current infrastructure CI pass. Production provisioning approval is pending.

Still required before release or issue closure: runtime store configuration,
owning Temporal worker/workflows/activities, tightly scoped worker database
access, scan/verdict/release transitions, download authorization, retry and
revocation/expiration flows, generated UI contracts and staff UI, GitOps
worker/scanner/IAM/audit configuration, real GCS acceptance, full local
CNPG/API/browser flow and exact-source post-deployment synthetic tests/logs.
The backend PR remains draft and production upload intents remain unavailable
when the runtime store is unconfigured. No owning story is closed by this
evidence.

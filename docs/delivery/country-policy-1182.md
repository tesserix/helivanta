# Reviewed country policy configuration — #1182

> **2026-10-23, #1287:** Pakistan was removed; India is the only supported country.

Parent [#1134](https://github.com/tesserix/helivanta/issues/1134), epic [#1133](https://github.com/tesserix/helivanta/issues/1133). Owner: Sam123ben. Verified locally on 2026-10-17; production migration and rollout pending specific approval.

## Delivered scope

Settings → Country policies supports immutable source-backed submissions, explicit scope access, independent approval/rejection, suspension, paginated versions/history and authoritative current-policy preview. Exact tenant/country/jurisdiction/category/operation/currency matching prevents an approval from activating another scope. All eleven requirement domains must be cited and reviewed. Configuration approval does not represent legal or licence verification.

A generated JSON contract feeds Go and TypeScript. Shared query hooks forward tenant headers and cancellation. Policy queries are keyed by verified tenant and subject; commands abort on tenant change, retain stable UUIDv7 identities on unchanged retries and reject changed-payload replay or stale revisions. Native date fields retain user input and convert from explicitly labelled UTC only at submission; display dates name the browser timezone. Focused headings have scroll spacing below the sticky header; the browser reproduced a hidden title at y=0 before the fix and verified y=96 afterwards.

```mermaid
sequenceDiagram
    actor Owner as Tenant scope owner
    actor Reviewer as Independent reviewer
    participant UI as Settings UI
    participant API as Workspace API
    participant DB as PostgreSQL + forced RLS
    participant NATS as NATS JetStream
    Owner->>UI: Complete exact scope, sources and 11 requirements
    UI->>API: Submit UUIDv7 command + expected revision
    API->>DB: Atomic version + receipt + audit + outbox
    DB-->>API: Submitted revision
    Owner->>API: Explicit reviewer scope delegation
    Reviewer->>API: Review with reason and expected revision
    API->>DB: Reject self-review; lock scope; record decision
    DB-->>NATS: Existing outbox relay publishes stable event ID
    Reviewer->>API: Suspend with reason
    API->>DB: Lock scope and append suspension
    UI->>API: Refresh current-policy preview
    API-->>UI: Denied; prior approval and sources retained
```

Editable architecture: [reviewed-country-policy.drawio](../architecture/reviewed-country-policy.drawio). XML structure validated; no draw.io-rendered export is claimed. Solid paths describe this slice; dotted trading paths remain future integration. Decisions: [ADR 0030](../adr/0030-reviewed-country-trading-policy.md).

## Local acceptance evidence

An isolated scratch database, separate OpenFGA store, dedicated JetStream and Valkey containers were used with existing local synthetic owner/reviewer identities. Real legacy/customer data was not imported. The fictional citation uses `.invalid` and all rationale text explicitly says TEST ONLY.

| Check actually run                                                                                                    | Result                                                                                                                                         |
| --------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| Full `bash scripts/coverage-gate.sh` under Go 1.27.2 with real PostgreSQL/NATS containers and VM port-forward adapter | Passed, including race and architecture checks; workspace 81.8%, authority 83.3%, existing package gates passed                                |
| Final `go test -race ./internal/modules/workspace ./pkg/tradingpolicy ./internal/archtest -count=1`                   | Passed after adding migration session timeouts                                                                                                 |
| `gofmt -l`, `go vet ./...`, `go build ./...`, `golangci-lint run --timeout=10m`                                       | Passed; new data-only workspace event contract registered in existing isolation allowlist                                                      |
| `go run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...`                                                               | Zero reachable vulnerabilities; one non-reachable required-module advisory remains                                                             |
| `pnpm turbo run lint type-check test format:check`                                                                    | All 19 tasks passed; API 198, UI 92 and shell 309 tests = 599 web tests, plus scripts tests                                                    |
| Configured `pnpm turbo run build`                                                                                     | Passed on Next.js 16.3.8; first unconfigured invocation correctly rejected missing required public OIDC build settings                         |
| `pnpm audit --prod`                                                                                                   | Exit 0 under existing documented braces GHSA-vfj7-8cjw-p6xm exception; this change adds no dependency or exception                             |
| Real Chrome owner → reviewer journey                                                                                  | Submit → author self-review blocked → delegated reviewer approves → revision 3 current → suspension revision 4 denied; prior evidence retained |
| Responsive inspection                                                                                                 | Desktop and 390×844 English layout inspected; no horizontal overflow; existing navigation expand/collapse usable; viewport reset               |
| Migration repeat and previous-release migration compatibility                                                         | Both exit 0 against the scratch database; one named migration record retained; policy and audit/history preserved                              |
| Scratch database evidence                                                                                             | Seven tables all enable and force RLS; one scope, one immutable version, one source, eleven requirements and eleven citations                  |
| Action/audit/outbox proof                                                                                             | Four actions, four append-only audit events, four unique outbox events, all published                                                          |
| Real JetStream readback                                                                                               | Four policy messages with four unique event IDs: submitted, access_granted, approved and suspended                                             |
| Local Valkey health                                                                                                   | PONG; Valkey is not consulted for policy authority                                                                                             |

The original native-date defect and missing timezone label were reproduced before their fixes. Full checks also caught a test-only unsupported Testing Library option and the new event-contract allowlist entry; both were corrected without weakening checks. Existing React `act` warnings and Next middleware deprecation warnings remain non-failing.

After rebasing onto the independent HR release `813bc14`, API (206), shared UI (92), scripts, lint/type/format checks passed. Two tests hit the unchanged five-second limit during the concurrent shell run (country-policy submission and an upstream payroll test); the **entire shell suite then passed all 314 tests with `--maxWorkers=1`**, for 612 web tests on the rebased code. No assertions, tests or timeout limits were removed or relaxed.

A supplementary post-rebase full Go repeat was stopped when the local Docker VM stalled container inventory both through its socket and directly inside the VM, and the shared local identity services became slow. Only that owned runner and its descendants were stopped; no shared service or unrelated test process was restarted. This repeat is **not claimed as a pass**. The earlier full Go gate and subsequent targeted workspace/policy/architecture race run passed; post-rebase Go vet/build/lint also passed. The full hosted Go/E2E run remains required before merge. Explicit local JetStream/Valkey port-forwards were restored and both were verified again; the real browser policy loaded with the corrected title spacing.

The repository was made **public** at the user's explicit request after a redacted history scan of 366 locally available commits found no leaks. Initial hosted jobs had not started because of GitHub's private-repository account restriction; after the visibility change, reruns started successfully. Hosted CI run 38912582052 passed Go, web, scripts, secret scan and browser E2E; image runs 38912570373 and 38912581636 both passed. Use [PR #1183 checks](https://github.com/tesserix/helivanta/pull/1183/checks) for the final hosted result. Main/release remains unmerged pending the specific production approval below.

Local logs and screenshots are under `/tmp/helivanta-country-*`; database proof `/tmp/helivanta-country-local-proof.json`, broker proof `/tmp/helivanta-country-broker-proof.json`, desktop `/tmp/helivanta-country-policy-suspended-local.png`, mobile `/tmp/helivanta-country-policy-mobile-local.png`. Protected runtime/login JSON stays outside the repository and is not part of delivery.

## API authorisation

All routes require a verified tenant principal. Collection reads use current read permission and return only owned or explicitly granted scopes. Detail/version/action reads require the same read permission and explicit scope access; another tenant or an inaccessible scope returns 404. Submission requires manage permission and owner/editor access for an existing scope. Access changes require manage permission and the actual scope owner. Approval/rejection/suspension require review permission and owner/reviewer scope access; approval/rejection additionally require a different subject from the immutable author. Forced RLS and composite references protect all seven tables independently of the UI. Expected-tenant headers guard against a tenant switch between rendering and sending a command.

Policy preview is a read, not a commitment. The platform transaction guard is tested to hold a shared scope lock against concurrent suspension; no trading handler is wired to it in this slice. Database failure denies access; broker failure leaves committed outbox intent for retry; duplicate events carry stable IDs. No new Temporal workflow is needed for these single-database commands. Reminders, provider integrations and longer workflows remain separate stories.

## Migration and recovery review

Named migration: **`0002_workspace_country_policies`**, [SQL](../../backend/internal/modules/workspace/country_policy.sql). It adds seven new empty tables, associated indexes, tenant/id references, RLS and immutable-history/review/audit triggers. No existing table data is rewritten or dropped. `lock_timeout=5s` and `statement_timeout=60s` bound its DDL transaction. The existing ledger makes application repeatable and transactionally rolls back an unsuccessful migration.

The down path is deliberately **non-destructive code rollback with schema preservation**. The previous release's migration command was exercised against the migrated local database and still passed all schema checks; current records and immutable history remained intact. This proves migration compatibility, not a production rollback drill. Do not drop policy/audit tables or truncate history during recovery. Failed or partial migration is investigated through the normal GitOps migration job; do not edit the production ledger by hand. The expected production tables are empty at first application; no backfill or new infrastructure capacity is required.

Merging main automatically starts image/release delivery and migration. The user-provided AGENTS.md requires specific approval for a named production migration and rollout. Keep the PR unmerged until approval for this migration and country-policy API/UI rollout is recorded. Then use the owning GitOps/Kargo path, verify CI/images/migration and exact rollout revision, inspect API/shell/worker readiness and privacy-safe errors, and perform synthetic deployed acceptance. Do not seed fictional policy approvals into a real operating jurisdiction.

## Remaining parent work

#1134 and epic #1133 remain open. Restricted listing/order/dispatch/dispense integration, real jurisdiction reviews, country-specific adapters, Urdu localisation/RTL acceptance and full country activation are **not** delivered here. Other B2B onboarding/licensing, bilateral relationships, pricing, warehouse, fulfilment, finance and automation stories retain their existing acceptance criteria. No SOC 2 certification, HIPAA attestation, government licence verification, regulated sale or payment-provider processing is claimed.

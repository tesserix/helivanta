# Batch safety and customer time acceptance

Story: [#1213](https://github.com/tesserix/helivanta/issues/1213), child of batch management #166.

This delivery adds a permanent concern journal, not stock release or verified recall authority. All stock remains quarantined and dispensable quantities remain zero. Broader batch, recall, expiry, licensing and B2B stories remain open. The [ADR](../adr/0032-pharmacy-batch-safety-journal.md) and [editable diagram](../architecture/diagrams/13-pharmacy-batch-safety.drawio) explain the boundary.

## Customer time

Pharmacy onboarding already persists an IANA time zone per outlet. The selected outlet now supplies that saved setting to inventory snapshots/movements, batch safety forms/history, catalogue revisions, payables and settlements. Reloading or using a browser in another zone does not change the customer's zone. Different outlets can have different zones. API instants remain UTC; expiry and invoice dates remain date-only. DST gaps and ambiguous wall times are rejected explicitly.

Browser acceptance used the saved Asia/Kolkata outlet while the host used Australia/Melbourne. The UTC observation `2026-10-17T04:30:00.123456Z` displayed as `2026-10-17 10:00:00.123456 GMT+05:30`. The form identified Asia/Kolkata and defaulted to the customer's local time. Desktop and 390 × 748 responsive layouts were inspected at standard browser zoom; timestamps wrap without losing their offsets. Scope/zone-switch regression tests ensure drafts and prior history do not cross outlets. Other hospital and cross-product screens are outside this pharmacy change.

## Local acceptance

A private synthetic scratch database, real non-superuser/non-BYPASSRLS application role, local API, production standalone Next build and genuine Zitadel/OIDC test session were used. No customer records or credentials are included in the repository.

- Migrator ran twice successfully against the scratch database.
- Twenty API requests verified create/replay, changed-payload conflict, nullable fields, cursor uniqueness, anonymous denial, inaccessible references, invalid/spoofed/future/high-precision timestamps, and unchanged quarantined stock.
- Browser recording created one additional synthetic quality concern. Refresh retained it. Three incidents, three audit/outbox publications and unchanged stock were checked against PostgreSQL and the relay.
- Real PostgreSQL/NATS integration tests cover tenant/outlet/grant isolation, immutable database triggers, actor/reference/time forgery, concurrent retries, atomic rollback and identifier-only JetStream delivery.
- Browser finance acceptance recorded a synthetic INR 10.00 expense and INR 4.00 external payment record, with INR 6.00 outstanding after refresh. Both recording timestamps used Asia/Kolkata; document, due and settlement dates remained date-only. No money was transferred.
- The small fixture history query took 0.927 ms. This is functional query evidence, not a representative load benchmark.
- Read-only legacy review confirmed store issue/return/transfer/indent, consumption/ledger, posting exceptions, batch, expiry, price and rack/shelf concepts. Those broader workflows are tracked separately.

## Commands and gates

Commands were run from the isolated worktree using Node 24.11.1 and Go 1.27.2. Test logs and temporary credentials stay outside Git.

```sh
pnpm exec turbo run type-check lint format:check
pnpm --filter @helivanta/shell build
pnpm --filter @helivanta/shell exec vitest run components/pharmacy/batch-safety.test.tsx --maxWorkers=1
pnpm --filter @helivanta/shell exec vitest run components/pharmacy/stock-register.test.tsx --maxWorkers=1
pnpm --filter @helivanta/api exec vitest run src/customer-time.test.ts

golangci-lint run ./...
go vet ./...
go build ./cmd/api ./cmd/migrate
govulncheck ./...
./scripts/coverage-gate.sh
```

The batch-safety component also passed with `TZ=Pacific/Honolulu`, independent of the saved Asia/Kolkata outlet. Typecheck/lint/format tasks and the Next build passed. Vulnerability scanning found no reachable or imported-package vulnerabilities; one required-module advisory was outside called code. The full backend run initially identified missing role-matrix and event privacy classifications; these were added without relaxing either guardrail. The full frontend run initially timed out in three unchanged payroll tests under concurrent host load; no timeout was raised and no test was skipped. The frontend rerun passed all 820 tests: shell 466, API package 261, shared UI 93. The architecture package rerun passed with the race detector. The first full backend run passed all application packages (pharmacy coverage 83.4%); only the two architecture declarations failed. The full coverage gate is rerunning after their correction and must pass before merge.

## Release gate

Production migration `0007_pharmacy` and the API/shell rollout require specific approval after a reviewable PR and passing CI. This branch has not been migrated or deployed to production. The additive migration creates a new empty journal and indexes/triggers; it rewrites no existing records. On application rollback, retain the journal, audit and outbox data; older code ignores the new table. Verify exact deployed SHA, readiness, synthetic API/UI, event delivery and logs before posting the completion comment and closing only #1213.

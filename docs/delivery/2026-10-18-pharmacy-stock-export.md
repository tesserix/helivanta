# Scoped stock CSV exports

> **2026-10-23, #1287:** Pakistan was removed; India is the only supported country.

Story [#1232](https://github.com/tesserix/helivanta/issues/1232), reporting parent #1060, inventory parent #162. Assigned to Sam123ben. Local implementation and verification are recorded below; reviewed release and production verification remain pending. This does not complete either parent.

## Contract and meaning

`GET /pharmacy/pharmacies/{pharmacy}/outlets/{outlet}/stock/positions.csv` requires the same verified tenant membership, `pharmacy.stock.read`, explicit live reader/editor outlet grant and forced RLS as the JSON stock register. It accepts only one nonempty occurrence of `as_of` (required), `location_id`, `product_id`, `batch_id` (added with #1303, so the register can reopen one position), `expiry` and `balance`. The same cutoff, scoped-reference and expiry/balance rules apply. Pagination parameters and unsupported filters are refused rather than silently ignored.

Each CSV contains all matching positions, including unloaded pages, bounded to **1,000 rows and 8 MiB**. Overflow returns `413 stock_export_too_large` before attachment bytes; database/access/timeout failures also return explicit errors without partial CSV. Empty results contain the column headers. Standard CSV escaping protects delimiters and quotes; untrusted formula-like text is prefixed with an apostrophe. Numeric totals remain decimal integer strings in the file. Spreadsheet consumers should import identifiers and long numbers as text when necessary to avoid automatic formatting or precision loss.

Columns cover the explicit UTC cutoff; exact location, product and lot identifiers; zone/aisle/rack/shelf; product/base unit; expiry date, outlet timezone/local date/band; product/regulatory/quarantine statuses; receipts, signed corrections, on-hand and zero dispensable quantity. Unknown coordinates stay blank. There are no fabricated sales, available/reserved quantities, valuation or mixed-unit/currency totals.

The generated-response audit stream records `audit_event=pharmacy.stock.exported` with verified actor/tenant, pharmacy/outlet, cutoff, row count, SHA-256 and request correlation. It contains no cell values or names. The audit proves a successful generation, not that a user retained or opened a file. Responses carry attachment disposition, `no-store`, `nosniff`, row count and checksum. The UI uses the same-origin client/expected-tenant header, disables repeated clicks, reports success/failure accessibly and aborts old requests on scope/filter/cutoff changes.

One statement provides each file's recorded-time view. Late commits and current product/location metadata can change a later export at the same cutoff. This is not an immutable stocktake or accounting close. See [ADR-0034](../adr/0034-pharmacy-stock-export.md) for threat model, operating assumptions, failure modes and query-plan limitations.

## Evidence and boundaries

- Reproduced the missing API route and download action before implementation. Reproduced formula injection before adding the text guard; reproduced oversized encoding before adding the bounded writer. Error feedback also reproduced a transport-detail leak before replacing it with generic guidance.
- Real PostgreSQL tests cover authorized readers, anonymous/wrong-action/other-tenant/unassigned/revoked/wrong-pharmacy access, malformed/repeated/unsupported filters, missing references, authorization failure, empty CSV, normalized addresses, signed quantities, successful audit attribution and no cell values in logs. A 1,000-position fixture proves that all unloaded rows are exported, JSON remains paginated at 50 rows, and row 1,001 yields an error without partial output. Encoder tests cover 8 MiB overflow and spreadsheet text hazards; client/UI tests cover unexpected attachments, errors/retry, cancellation during body reads and late scope/filter responses.
- A fresh-table nested-loop failure was fixed in the shared register query using bounded lateral unique-key reference lookups, retaining every tenant/outlet predicate and RLS check. Diagnostic `EXPLAIN ANALYZE` measured 129.259 ms for 1,000 synthetic positions under the non-bypass app role. That diagnostic code was removed. A local one-position lookup measured approximately 1.3–1.6 ms; neither result is a representative-volume or product-count benchmark.
- A genuine local OIDC/API walkthrough passed **42 requests** across synthetic India outlets. All 22 exported positions match limit-one pagination, exact values, saved timezones and addresses; identical replay/checksums and reader access passed. Foreign outlet and wrong pharmacy returned 404, anonymous requests 401, invalid filters 400. No stock write was made.
- Native Chrome saved the India one-row location-filtered export through a Save dialog. File checksums match attributed API audit entries. India uses Asia/Kolkata with the full address. Changing country cleared the previous download status and disabled export during reload. Automated UI cancellation and responsive wrapping are covered; a fresh narrow-device walkthrough remains pending.
- Read-only local SQL confirms the non-superuser/non-BYPASSRLS app role, four forced-RLS tables and unchanged synthetic totals: 12 movements / 50 units / 12 published stock events per country. Export adds no stock events, commands, schema, durable file or migration. Existing NATS/outbox behavior is unchanged; the Valkey rate limiter remains active. This synchronous read needs no new Temporal workflow or cache.
- The complete frontend run passed **892 tests** (API 291, shared UI 93, shell 508) and the script tasks. Web lint/typecheck/format passed all 15 tasks, the Next.js 16 production build passed, and Go vet/build/lint passed (zero lint findings). An initial broad frontend run hit an unchanged payroll test's five-second timeout under concurrent load; the final full rerun passed without changing that test, assertions or timeout.
- The complete Go race/coverage gate passed (exit 0), including pharmacy at 84.1% with the unchanged 70% package floor. Production acceptance is pending. Do not close #1232 until the reviewed release and deployed verification are recorded.

## Commands run

From `backend/`, with task-owned `GOCACHE`, `GOFLAGS=-p=1`, the local Colima `DOCKER_HOST` and its normal testcontainers socket override:

```sh
go test ./internal/modules/pharmacy -run 'TestStockExport|TestStockCSV' -count=1
./scripts/coverage-gate.sh
go vet ./...
go build -o /tmp/helivanta-1232-api ./cmd/api
golangci-lint run ./...
```

From the repository, using Node 24.11.1:

```sh
pnpm install --frozen-lockfile
pnpm turbo type-check lint format:check
pnpm turbo test -- --pool=threads --maxWorkers=1
pnpm --filter @helivanta/shell build
git diff --check
```

Private local acceptance/proof helpers and logs remain outside Git, with credentials/session values suppressed. Legacy StoreHome/ItemLedger were refreshed read-only on 18 October; no customer values or legacy transactions/downloads were copied. There is no production mutation in this delivery so far. Application rollback removes the additive route/button through the normal GitOps image change and preserves existing stock.

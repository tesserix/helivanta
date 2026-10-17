# Warehouse/depot bin configuration: local acceptance

Parent #1137 and related legacy store-location story #1011 remain open. This is a
configuration slice, not completion of distributor structure, scoped staff roles,
warehouse movements, store/category/unit mappings or import. Release is pending.

## Delivered scope

- Reuse owned organisations and existing warehouse/depot premises. Counterparties,
  non-storage addresses and inaccessible parents cannot expose or own bin records.
- Unique bin code per premise; name, aisle/rack/shelf, storage declaration and status.
  Country and ownership come from the parent rather than a request body.
- Cursor-paged lists/history, immutable revisions, optimistic versions and exact retries.
- Current explicit organisation grants and forced RLS; finance/warehouse/sales/driver
  roles do not acquire bin or private organisation access through this slice.
- Atomic head/history/audit/NATS outbox; IDs/version only in the event payload.
- Existing shared form/select/query components; focus and scroll on opening the bin
  panel/form; visible failures, retained draft IDs and hidden stale results after denial.

Migration `0005_supply` adds new empty bin/revision tables, indexes and constraints,
and prevents reclassifying a premise with retained bins as a non-storage address.
No existing rows are backfilled or deleted. An application image rollback can leave
these additive tables and audit history intact. No new infrastructure is introduced.

## Local evidence

Go 1.27.2 was used with real PostgreSQL and NATS Testcontainers. Ryuk and all assertions
remain enabled. No authentication bypass was introduced.

Commands run:

```sh
pnpm install --frozen-lockfile --prefer-offline
pnpm turbo type-check lint format:check
pnpm turbo test build --concurrency=2
pnpm --filter @helivanta/shell exec vitest run --maxWorkers=1 --no-file-parallelism
pnpm --filter @helivanta/shell build
go -C backend test -race -count=1 -cover ./internal/archtest ./internal/modules/iam
go -C backend test -race -count=1 -cover ./internal/modules/supply
(cd backend && bash scripts/coverage-gate.sh)
(cd backend && golangci-lint run ./...)
git diff --check
```

- API package: 217 tests passed; shared UI: 92 passed; shell: 339 passed across 52 files.
- Type checks, generated-contract drift checks, lint and formatting passed.
- Production Next.js build passed with the actual local public Zitadel settings.
  The first build lacked these required settings; no validation was weakened.
- The first parallel shell run hit an existing duty-exchange timeout under host
  contention. Its full file passed on retry, then the entire serial shell run passed.
- The full Go race/coverage run found the new route/event/permission registry entries
  were missing. The singleton route, four non-PHI event fields and exact permission
  matrix are now explicitly reviewed and pinned; the entire architecture suite passed.
- That same full run hit the existing IAM cache-concurrency test's context deadline.
  The entire IAM suite then passed with race/coverage (80.4%) without changing the
  test, its deadline or application code. All other packages passed in the full run;
  SupplyConnect coverage was 79.4%. Hosted full CI remains a separate merge gate.

The real running API was also exercised through Next.js at the registered local origin
using the repository's genuine Zitadel password + authorization-code/PKCE helper:
real session/permissions, bin create/update, identical replay, stale change 409,
duplicate code 409, retained history, ungranted reviewer 404 and unauthenticated 401.

Native browser acceptance at `http://helivanta.localhost:4398` verified real sign-in,
Supply Connect navigation, owned organisation/premise selection, panel/form focus,
modern storage options, a synthetic quarantine bin create, shelf update S03 to S04,
versions 1 and 2 with actor/time, and persistence after a full reload. The app remains
running there against the new API on port 8094. No legacy/customer data was used.
Screenshots, session material and one-off acceptance artifacts remain outside Git.

The shared local Zitadel instance was initially blocked by its existing
`repeatable_sync_role_permissions` migration. After explicit user approval and a
private database backup, the vendor-supported `zitadel setup cleanup` recorded the
stuck step as failed so it could retry. Discovery recovered to 200 and API readiness
to 200. No users, credentials, production identity resources or migration rows were reset.

## Release boundary

No production change was made for this feature during local acceptance. Read-only
production inspection found API/shell/document-worker/supply-evidence-worker 2/2 ready
on `7ca6d17601975110d2083fa51c1fb68fa6eed235`; CNPG 2/2 ready. The release target is
project `tesseracthub-480811`, context
`gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`, namespace `helivanta`.

Before declaring release complete: full hosted CI, reviewed merge, signed image and
Kargo/Argo advancement, exact image/readiness verification, production UI/API synthetic
acceptance and relevant log checks. Keep #1137/#1011/#1133 open for their remaining
acceptance criteria; add release evidence without an automatic parent closing keyword.

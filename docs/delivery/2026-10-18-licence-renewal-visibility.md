# Licence renewal visibility

Parent: #1135, epic #1133. This bounded story adds premise-local renewal notices to the private SupplyConnect licence list and detail reads. Organisation approval, canonical pharmacy/premise binding, notifications and trading activation remain separate work. No licence declaration is evidence of legal eligibility.

Implementation tracker: #1217 (Sam123ben).

## Contract and boundaries

`GET /v1/supply/parties/:party/licenses` and `GET /v1/supply/parties/:party/licenses/:license` add a `renewal` read projection. It contains `date_status`, signed calendar `days_remaining`, `local_date`, the saved premise `time_zone`, and UTC `observed_at`. States are not yet valid, expired, expires today, renewal due within 30/90/180 days, and current. Expiry-day treatment preserves the existing review guard's inclusive date convention; this advisory does not decide country-specific legal validity.

Use one database observation time and one batched premise lookup for each bounded keyset page. Missing/inaccessible premises or invalid source dates/time zones fail the read safely. The existing tenant, organisation role and licence-read route permission checks remain mandatory. No new permissions, schema migration, stock effects, approval flags, contact/bank disclosure or external requests are introduced. Write responses and immutable revision snapshots retain their existing shape; advisories are recalculated on reads and never written into retained history.

Threat boundary: private licence identities and premise metadata must remain visible only to currently authorised organisation readers. Cross-tenant users, ungranted administrators, revoked readers and stale workspaces must receive the existing denial with no renewal metadata. Browser notices are informational and cannot authorise trading. Saved zones, rather than the developer's/browser's zone, select the calendar date.

## Design constraints

Planning assumption: peak 20 list reads/s, 200 rows/page maximum (the existing platform limit, plus one keyset sentinel), under 200 KiB/page, read/write ratio 20:1, 100k licences at 12 months and 500k at 36 months. Target p99 below 300 ms within the existing API availability target; these are planning budgets, not load-test claims. The existing tenant/party keyset and premise composite keys bound database work. No additional hosting cost beyond two indexed page reads and one clock query. PostgreSQL unavailable means the read fails; no cached eligibility is served. NATS, Valkey and Temporal are unnecessary for a synchronous read projection with no new side effects.

The UI shows the date warning, saved zone, observation date and the existing edit/evidence/review actions. A refresh retrieves a new server observation; missing advisory data from an older API shows unavailable, never an inferred current state. During refresh or failure, stale notices must not be presented as current. The list must not claim page-local values are organisation totals.

## Acceptance and release

- Frozen-clock tests cover IN/PK midnight differences, exact 0/30/90/180 boundaries, future issue, invalid input and DST calendar-day arithmetic.
- Real PostgreSQL API tests cover list/detail, private roles, tenant denial, immediate revocation, bounded reads, inaccessible premises and no changes to immutable histories/outbox. Pure tests cover both saved country zones.
- UI tests cover readable status text, server-zone display, refresh/error/stale suppression and edit-form focus.
- Run affected full Go/UI/API suites, race, vet, lint, typecheck, generation checks and production build; exercise genuine local API and UI with synthetic records.
- Keep the child open until the approved release is deployed and verified; post the exact SHA, acceptance and completion comment before closing. Parent #1135 remains open.

Recovery: additive read fields only. The previous binary can run against the same schema; old clients ignore the new field and the new UI handles absent data. There is no down migration. A production rollout still requires approval for the concrete PR.

## Current acceptance status

The new calculation tests, PostgreSQL API tests and UI interaction tests pass. Full API client suite: 262 tests. Full shell suite: 468 tests before adding the final notice-only cases. Race-enabled Supply, Pharmacy and architecture packages pass. API/shell ESLint, Go vet, Supply golangci-lint (zero issues), API/shell typecheck and the standalone production Next build pass.

Final targeted UI run: 19 tests across registration and renewal notices. Final targeted race run includes the inaccessible-premise denial test. Commands run from the checkout (Go commands from `backend/`, with the local Colima Docker socket configured):

```sh
go test -race ./internal/modules/supply ./internal/modules/pharmacy ./internal/archtest -count=1
go test -race ./internal/modules/supply -run '^TestLicence(Renewal|Reads)' -count=1
go vet ./internal/modules/supply/...
golangci-lint run ./internal/modules/supply/...
pnpm --filter @helivanta/api test
pnpm --filter @helivanta/shell test
pnpm --filter @helivanta/shell test components/supply/licence-renewal-notice.test.tsx components/supply/party-registration.test.tsx
pnpm --filter @helivanta/api type-check
pnpm --filter @helivanta/shell type-check
pnpm --filter @helivanta/api lint
pnpm --filter @helivanta/shell lint
node scripts/generate-supply-contract.mjs --check
```

The production build used the private local environment wrapper to run `pnpm --filter @helivanta/shell build`; no environment values are committed.

Genuine localhost API/browser acceptance is blocked: `auth.tesserix.localhost:20080` returns HTTP 502, its core container is waiting on the pre-existing `repeatable_scim_provisioned_user_count_trigger` migration, and the new API correctly refuses startup when OIDC discovery is unavailable. The saved NATS/Valkey endpoints were also stale; an attempt using the running development NATS and Valkey endpoints reached the same OIDC dependency failure. Shared identity containers, migration journals and the old local app were not mutated. No production resources were changed. This story remains open; UI DOM tests are not a claim of genuine OIDC/browser acceptance.

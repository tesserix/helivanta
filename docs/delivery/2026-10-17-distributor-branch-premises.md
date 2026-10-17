# Branch premise assignment: local acceptance

Story #1137 and epic #1133 remain open. This slice links an owned organisation’s
warehouse/depot to one of its branches. It does not complete corporate parent entities,
territories, sales representatives, location-specific grants, delivery access or B2B trading.

## Behavior and safety

- Existing legal organisations, premises and revision history remain the only masters.
- Optional UUIDv7 branch reference; explicit null unlinks, older-client omission preserves.
- Same tenant, organisation and country; owned organisations only; active branch required
  for new/reassigned links. Existing links survive branch retirement as historical structure.
- Only storage premises can be linked; a branch with linked children cannot change kind.
  Composite self-FK, forced RLS and SQL guards repeat the API’s boundaries.
- Organisation structure locking serializes assignment and parent reclassification.
  Existing current-role checks remain authoritative; no access inheritance is introduced.
- Exact retries and optimistic versions retain one result. Head, immutable snapshot, audit
  and existing minimal NATS outbox commit atomically. No PHI or branch labels enter events.
- Shared generated Go/TS contract and shared selector/form/query controls. Nullable selectors
  submit null on explicit clearing. Off-page selections resolve privately; failed reads
  hide stale options and block writes, while failed saves retain the draft and stable ID.

Migration `0006_supply` adds one nullable UUID, composite reference/check constraints and
trigger guards. It has bounded DDL timeouts, no data backfill/deletion and no populated
index build. Existing tenant/party index prefixes cover the bounded per-organisation
premise lookups. Application rollback preserves the additive column and immutable evidence.
Production migration/release is pending specific approval under the user’s working agreements.

## Verification performed

Go 1.27.2 and Node 24 were used for verification. PostgreSQL/NATS tests use the existing
Testcontainers harness with Ryuk enabled. No authentication or authorization bypass.

Observed red before implementation:

- New API linked-warehouse assertion returned 400 instead of 200.
- Shared contract rejected `branch_premise_id` as an unknown field.
- The real form test could not find Operating branch.
- Clearing the shared nullable selector submitted an empty string instead of null.

Then the tests passed. Commands actually run:

```sh
pnpm install --frozen-lockfile --prefer-offline
pnpm turbo type-check lint format:check --concurrency=2
pnpm turbo test --concurrency=1
pnpm turbo build --concurrency=2
go -C backend test -race -count=1 -cover ./internal/modules/supply ./internal/archtest
go -C backend vet ./...
go -C backend build ./...
make lint-go
(cd backend && bash scripts/coverage-gate.sh)
git diff --cached --no-ext-diff --binary | gitleaks stdin --redact --no-banner
git diff --check
```

- Web: API 222, UI 93, shell 349 tests passed (664 total). Type checks, generated contract
  drift checks, lint, formatting and the actual production Next build passed.
- SupplyConnect race/coverage passed at 79.9%; the entire architecture suite passed.
- Go vet/build and golangci-lint passed (0 issues). The full repository race/coverage
  gate passed, including IAM 80.4%, workforce 83.7%, events 77.1% and tenantdb 83.8%.
  No deadlines or assertions were weakened; all gated packages met the 70% floor.
- Secret scan passed. Standard `make lint-shell` failed because this Docker VM cannot
  bind-mount `/private/tmp`; the identical digest-pinned image, `.shellcheckrc` and all
  11 discovered scripts passed when transferred with Docker’s copy API into the isolated
  scanner container instead. The disposable scanner was removed after verification.

The actual API on 8095 was tested through Next at the registered origin
`http://helivanta.localhost:4398`: genuine Zitadel password and authorization-code/PKCE,
real session, branch creation/assignment, older-client omission, exact replay, explicit
unlink/reassignment, stale 409, paged saved snapshots, ungranted reviewer 404 and
unauthenticated 401. The initial attempt on 4397 was rejected by OIDC callback
registration; the preview was moved to the existing registered origin without IdP changes.

Native browser acceptance additionally verified real sign-in, form focus, reusable
branch selection, saved unlink/reassignment, updated list context, full reload persistence
and revisions 1 through 6 with actor/time. The browser was then reloaded against the actual
production Next build and the persisted branch structure verified again. Synthetic records
only; no legacy/customer data.
Session material, runtime scripts/results and screenshots remain private outside Git.

## Release target

User-approved rollout is required for migration `0006_supply` before merging, because
main automatically publishes and advances through Kargo/Argo. Intended target: project
`tesseracthub-480811`, context
`gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`, namespace `helivanta`.
Keep the repository public and keep #1137/#1011/#1133 open. Verify hosted full CI, exact
signed image promotion, 2/2 application readiness, healthy CNPG, production UI/API synthetic
acceptance and logs before adding final release evidence. No imperative rollout or live SQL.

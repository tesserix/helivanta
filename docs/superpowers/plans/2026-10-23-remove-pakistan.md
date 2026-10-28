# Remove Pakistan Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** India is the only supported country. No list offers Pakistan, no API or table
accepts `PK` / `PKR` / `ntn` / `stn`, and a database already holding Pakistan rows refuses
the migration instead of changing them.

**Architecture:**

- **Contract:** one edit to `contracts/country-profiles.json`. The pharmacy, supply and
  trading-policy contracts derive their country, currency and identifier enums from it,
  so regenerating covers them.
- **Migrations:** each owning module gets one append-only migration that refuses existing
  Pakistan rows and then tightens its checks. It counts across tenants by lifting
  `FORCE ROW LEVEL SECURITY` inside its own transaction.
- **Tests:** tests that used Pakistan move to an unsupported code or a legacy row seeded
  directly in the database.

**Tech Stack:** Go, Postgres, Node generators (`scripts/generate-*.mjs`), TypeScript/zod,
Vitest, Playwright.

**Spec:** `docs/superpowers/specs/2026-10-23-remove-pakistan-design.md`

## Global Constraints

- Generated files are regenerated, never hand-edited. Each generator's `--check` passes.
- Migrations are append-only. Each new ID follows `NNNN_<module>`, the next free number in
  that module.
- **Each migration:**
  - lifts `FORCE ROW LEVEL SECURITY` on a table only inside its own transaction, and
    restores it before the transaction ends;
  - raises on any Pakistan row, naming the table and its count;
  - changes nothing when it raises.
- No hand-written code path may still accept `PK`, `PKR`, `ntn` or `stn`.
- The "no tax scheme" server paths stay. They are tested with an outlet whose country has
  no profile, seeded directly in the database.
- Final `git grep -n -E '"PK"|\bPKR\b|Asia/Karachi|Pakistan|\bntn\b|\bstn\b'` finds nothing,
  except ADR supersession notes and the #1287 spec and plan.
- No AI attribution in commits or the PR.
- **Gates**, one at a time:
  - `pnpm turbo lint type-check test build format:check`, `make lint-shell`, `make lint-go`;
  - `GOTOOLCHAIN=go1.27.3 ./scripts/coverage-gate.sh`, `go test -race ./...`;
  - `go run ./cmd/contractdocs`, then `-check`;
  - the full e2e suite.

## Review Focus

1. **A migration that cannot see the rows.** As the forced owner, a plain count is always 0.
   Pinned per module: a test seeds one tenant's PK row and expects the migration to fail.
   It is proven able to fail by removing the `NO FORCE` step.
2. **FORCE left off.** After every migration, success or failure, `relforcerowsecurity` is
   true on each table touched. Pinned by a test, and also by the existing `LintRLS`.
3. **A Pakistan path in hand-written code that no generator reaches**, such as
   `supply-operations.ts` or the `organisation-structure.tsx` options. Pinned by the final
   grep and by Vitest on the country selects.
4. **Losing the "no tax scheme" coverage.** The overview with no Setup tile,
   `tax=unclassified` → 400, and tax registration refused must still be exercised. Pinned
   in Task 3.
5. **Slice 3 (#1302) still unmerged.** Its tests use PK, so this branch rebases onto main
   after #1302 merges, and Task 3 converts those tests too.

---

### Task 1: The profile contract and its generated outputs

**Files:**

- Modify: `contracts/country-profiles.json`, removing the PK entry.
- Regenerate:
  - `backend/pkg/countryprofile/profiles_gen.go`
  - `packages/api/src/country-profiles.ts`
  - every output of `generate-trading-policy.mjs`, `generate-pharmacy-contract.mjs` and
    `generate-supply-contract.mjs` that changes
- Modify tests:
  - `backend/pkg/countryprofile/profiles_test.go`
  - `backend/pkg/tradingpolicy/{policy,authority}_test.go`
  - `packages/api/src/{customer-time,trading-policy,supply,supply-registration,tax-registration}.test.ts`

- [ ] **Step 1:** Change the tests to the new truth:
  - `countryprofile.Lookup("PK")` is not ok;
  - `ValidTimezone("PK","Asia/Karachi")` is false;
  - `ValidTaxIdentifierKind("PK","ntn")` is false;
  - the TS `countryConfiguration.countries` is `["IN"]`;
  - trading-policy and supply schemas reject `PK`, `PKR` and `ntn`.
- [ ] **Step 2: Run** `cd backend && go test ./pkg/... -count=1` and
      `pnpm --filter @helivanta/api test`. Expected: FAIL, because PK is still supported.
- [ ] **Step 3:** Remove PK from the contract, then run each generator, followed by its
      `--check`.
- [ ] **Step 4: Run** the step 2 commands again, plus `go build ./...` and
      `pnpm --filter @helivanta/api type-check`. Expected: PASS. Compile errors in module code
      are expected, and are fixed in Tasks 2–4.
- [ ] **Step 5: Commit** `feat(country): India is the only supported country (#1287)`

### Task 2: Workspace country policy migration

**Files:**

- Create: the next `NNNN_workspace` migration (SQL alongside `country_policy.sql`, registered
  where the module lists its migrations).
- Test: `backend/internal/modules/workspace/country_policy_test.go`

**Interfaces:**

- Produces the SQL pattern Tasks 3–4 copy:

  ```sql
  DO $$
  DECLARE n bigint;
  BEGIN
    ALTER TABLE <t> NO FORCE ROW LEVEL SECURITY;
    SELECT count(*) INTO n FROM <t> WHERE <pakistan>;
    ALTER TABLE <t> FORCE ROW LEVEL SECURITY;
    IF n > 0 THEN
      RAISE EXCEPTION '#1287: % Pakistan rows in <t>; delete or correct them, then re-run', n;
    END IF;
  END $$;
  ```

  Then the tightened checks.

- [ ] **Step 1: Write the failing tests:**
  - `TestCountryPolicyMigrationRefusesPakistanRows`: apply the migrations up to the one
    before, insert one tenant's PK policy with `WithTenant`, apply the new migration, and
    expect an error naming the table. `relforcerowsecurity` must still be true.
  - `TestCountryPolicyRefusesPakistan`: after a clean migrate, a PK insert fails on the
    check constraint.
- [ ] **Step 2: Run** them. Expected: FAIL, because the migration does not exist yet.
- [ ] **Step 3:** Write the migration. Then prove focus 1: drop the `NO FORCE` line, see the
      refusal test fail, and restore it.
- [ ] **Step 4: Run** `go test ./internal/modules/workspace -count=1`. Expected: PASS. Rewrite
      any other PK use in that package.
- [ ] **Step 5: Commit** `feat(workspace): refuse Pakistan country policies (#1287)`

### Task 3: Pharmacy

**Files:**

- Create: the next `NNNN_pharmacy` migration. It refuses PK outlets and PKR on every
  pharmacy table with a `currency` column (`grep -n "currency text" internal/modules/pharmacy/*.sql`).
- Modify tests:
  - `country_test.go`, `tax_registrations_test.go`, `stock_expiry_test.go`, `overview_test.go`;
  - after rebasing on #1302, `counted_lists_test.go`.
- Create: a test helper `legacyOutlet(t, db, country)`. It seeds, through `db.WithTenant`
  (not the API), an outlet with an unsupported code such as `XX`, plus the editor's grant.

- [ ] **Step 1: Write the failing tests:**
  - `TestPharmacyMigrationRefusesPakistanRows`, covering both an outlet and a PKR-only
    product row;
  - `TestOutletRefusesPakistan`: `PUT` an outlet with `PK` returns 400;
  - convert the no-scheme tests to `legacyOutlet`:
    - the overview omits Setup;
    - counted `tax=unclassified` → 400;
    - tax registration is refused.
- [ ] **Step 2: Run** `go test ./internal/modules/pharmacy -count=1`. Expected: the refusal
      test fails, and the converted tests pass. If any converted test fails, its path depended on
      PK specifically: rule and ledger.
- [ ] **Step 3:** Write the migration and prove focus 1.
- [ ] **Step 4: Run** the package. Expected: PASS.
- [ ] **Step 5: Commit** `feat(pharmacy): refuse Pakistan outlets and amounts (#1287)`

### Task 4: Supply

**Files:**

- Create: the next `NNNN_supply` migration. It refuses PK on parties, party addresses,
  identifiers (`country_code='PK'` or `kind IN ('ntn','stn')`), premises, licences,
  territories and delivery tasks. It then replaces each `IN ('IN','PK')` check with
  `= 'IN'`, and the identifier check with India's kinds.
  - Constraint names come from `\d+` on a migrated test database.
  - The `(tenant_id,…,country_code)` foreign keys are kept.
- Modify:
  - `bins_test.go`, `registration_test.go`, `review_test.go`, `license_renewal_test.go`,
    `branches_test.go`, `access_limits_test.go`, `territories_test.go`,
    `invariants_test.go` and `delivery_tasks_test.go`;
  - any hand-written Go validation listing PK.

- [ ] **Step 1: Write the failing tests:**
  - `TestSupplyMigrationRefusesPakistanRows`: one PK party, and separately one `ntn`
    identifier on an IN party, each refused.
  - `TestSupplyRefusesPakistan`: creating a PK party gives 400, and so does an `ntn`
    identifier.
- [ ] **Step 2: Run** them. Expected: FAIL.
- [ ] **Step 3:**
  - Write the migration and prove focus 1.
  - Rewrite each test's PK use:
    - "the other country" isolation → expect 400 for PK;
    - PK-only identifier → expect 400.
- [ ] **Step 4: Run** `go test ./internal/modules/supply ./internal/archtest -count=1` and
      `go test ./... -count=1`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(supply): refuse Pakistan parties and identifiers (#1287)`

### Task 5: Frontend

**Files:**

- Modify:
  - `packages/api/src/supply-operations.ts:92`, so the type is `"IN"`;
  - `apps/shell/components/supply/organisation-structure.tsx`, removing the PK options;
  - the shell tests listed by `git grep -l -E '"PK"|PKR|Asia/Karachi|ntn' apps packages`.

- [ ] **Step 1: Write the failing tests:**
  - `country-setup.test.tsx`: the country select offers India only.
  - `party-registration.test.tsx`: the identifier kinds are gstin, pan and other.
  - Rewrite the tests that defaulted Pakistan to PKR as India / INR. The no-scheme catalogue
    and accounts tests use an unsupported code such as `XX`.
- [ ] **Step 2: Run** `pnpm --filter shell test`. Expected: the new assertions fail where
      hand-written options still list PK.
- [ ] **Step 3:** Remove the hand-written PK paths.
- [ ] **Step 4: Run** `pnpm --filter shell test`, then `type-check` and `lint`. Expected: PASS.
- [ ] **Step 5: Commit** `feat(shell): country lists offer India only (#1287)`

### Task 6: Docs, grep and gates

- [ ] **Step 1: Docs.**
  - Remove Pakistan from the current-state docs listed by
    `git grep -l -E 'Pakistan|"PK"|PKR' docs` (excluding ADRs and this spec and plan).
  - Rename `docs/requirements/pharmaconnect-india-pakistan.md` to
    `pharmaconnect-india.md` and fix the links to it.
  - Add a dated supersession note to ADRs 0026, 0027 and 0030.
  - Mark the two network diagrams superseded in their docs, if they cannot be regenerated.
- [ ] **Step 2:** Run `go run ./cmd/contractdocs`, then `-check`, and run the final grep.
      Expected: clean.
- [ ] **Step 3:** Run every gate, then the full e2e on a production build. Expected: all
      green.
- [ ] **Step 4: Commit** `docs(country): Pakistan removed (#1287)`

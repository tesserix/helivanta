# Counter hospital names Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The counter's two hospital pickers show each verified hospital's
name, from a pharmacy copy kept current by a workforce event and disclosed only
through a verified affiliation (#1637).

**Architecture:** Workforce versions each hospital's name in the database and a
background announcer publishes `hospital_named.v1` for every unsent version.
Pharmacy stores every name in its tenant through an idempotent, version-ordered
consumer and adds `hospital_name` to verified affiliations only. One shared
labelling function renders the pickers.

**Tech Stack:** Go (gin, gorm, Postgres RLS, outbox), Next.js shell, Vitest,
Playwright.

**Spec:** `docs/superpowers/specs/2026-11-11-pharmacy-counter-hospital-names-design.md`

## Global Constraints

- Event subject `helivanta.in.workforce.hospital_named.v1`; data `{hospital_id, name, name_version}` (`name_version` a JSON number). Nothing else about the hospital.
- Only `workforce_hospitals` rows with `establishment_kind = 'hospital'` are announced.
- `name_version`: 1 on insert, +1 only when `name` changes, set by a database trigger.
- Announcement marker lives in `workforce_hospital_name_announcements`, never on `workforce_hospitals`; an announcement writes no `workforce_audit` row.
- Announcer actor `system:workforce-hospital-name-announcer`; its cross-tenant scan reads ids and versions only, never names.
- Pharmacy table `pharmacy_hospital_names`, consumer name `pharmacy-hospital-names`; a stored row changes only for a strictly greater `name_version`.
- `hospital_name` is the stored name only when the affiliation's current status is `verified`; otherwise `null`. No other route returns stored names.
- Picker labels: `<name>`; `<name> · <hospital id>` when another verified hospital in the list has the same name; `Name unavailable · <hospital id>` when the name is null or absent. The separator is `·` (U+00B7 with spaces).
- Migration IDs: `0097_workforce_hospital_names`, `0045_pharmacy_hospital_names` (main took `0096_workforce_business_profile` and `0044_pharmacy_business_profile` while this branch was open); re-check both against `main` and `codex/*` before merge.

## Review Focus

1. A rename racing the announcer: the event must carry the name and version read under the same lock, so a pass never publishes an old name with a new version — Task 1's "rename between passes" test asserts the published pair matches the row.
2. Events arriving out of order or twice: version 2 then version 1 leaves version 2; version 2 twice is one row — Task 2's consumer test.
3. Revocation after verification: an affiliation verified, then revoked, must go back to `null` — Task 2's disclosure test walks accepted → revoked.
4. Two verified affiliations to the same hospital (two affiliation rows, one hospital ID): the picker lists it once and does not mark it as a duplicate name — Task 3's picker test.
5. An older server that omits `hospital_name`: the pickers read "Name unavailable · <id>", never `undefined` or blank — Task 3's picker test.

---

### Task 1: Workforce versions and announces hospital names

**Files:**

- Create: `backend/internal/modules/workforce/hospital_names.sql`, `backend/internal/modules/workforce/hospital_names.go`
- Modify: `backend/internal/modules/workforce/module.go:355` (append the migration; embed the SQL like its neighbours), `backend/internal/modules/workforce/contract/events.go` (subject and data type), `backend/cmd/api/main.go:661` (start the announcer beside `pharmacy.RunAffiliationRedelivery`)
- Test: `backend/internal/modules/workforce/hospital_names_test.go`

**Interfaces:**

- Produces: `workforcecontract.SubjectHospitalNamed = "helivanta.in.workforce.hospital_named.v1"`; `type HospitalNamedData struct { HospitalID string \`json:"hospital_id"\`; Name string \`json:"name"\`; NameVersion int64 \`json:"name_version"\` }`; `func RunHospitalNameAnnouncer(ctx context.Context, deps platform.Deps)`(30 s ticker);`func AnnounceHospitalNamesOnce(ctx context.Context, db *tenantdb.DB) error`.

- [ ] **Step 1: Write the failing tests** in `hospital_names_test.go`, using `newEstablishmentRouter` / `newEstablishmentHarness` from `establishment_kind_test.go`:
  - `TestHospitalNameVersionFollowsTheName`: PUT a hospital → `name_version` 1; PUT the same name → still 1; PUT a new name → 2.
  - `TestAnnouncerPublishesEachHospitalNameOnce`: after one PUT, `AnnounceHospitalNamesOnce` → exactly one outbox row with subject `SubjectHospitalNamed` for this hospital, data `{hospital_id, name, name_version: 1}`; a second pass adds none; the `workforce_audit` count for the hospital is unchanged by the passes.
  - `TestAnnouncerSendsOnlyTheNewestName`: PUT name A, PUT name B, one pass → one event, name B, version 2; then PUT C, pass → one more event, C, 3.
  - `TestAnnouncerSkipsPharmaciesAndSuppliers`: insert a `pharmacy` and a `supplier` row the way `establishments.go:151` / `supplier_staff.go:108` do; a pass publishes nothing for them.
  - `TestAnnouncerCoversSignupHospitals`: a hospital created through the customer workspace path (`customer_workspace.go:58`, as `customeronboarding/workspaces_test.go` drives it) is announced.
- [ ] **Step 2: Run** `cd backend && GOTOOLCHAIN=go1.27.3 go test ./internal/modules/workforce -run 'TestHospitalName|TestAnnouncer' -count=1`. Expected: FAIL to compile — `undefined: workforcecontract.SubjectHospitalNamed`.
- [ ] **Step 3: Implement.** SQL: `ALTER TABLE workforce_hospitals ADD COLUMN name_version bigint NOT NULL DEFAULT 1 CHECK (name_version >= 1)`; a `BEFORE UPDATE` trigger setting `NEW.name_version := OLD.name_version + 1` when `NEW.name IS DISTINCT FROM OLD.name`, else `OLD.name_version` (so no writer can set it); `workforce_hospital_name_announcements (tenant_id, hospital_id, announced_version bigint NOT NULL, updated_at)`, primary key `(tenant_id, hospital_id)`, FK to `workforce_hospitals`, forced-RLS boilerplate as `schema.sql:9-13`; `GRANT SELECT (tenant_id, id, establishment_kind, name_version) ON workforce_hospitals TO helivanta_system` and `GRANT SELECT (tenant_id, hospital_id, announced_version)` on the announcement table. Go: the pass mirrors `RedeliverAffiliationsOnce` (`pharmacy/affiliation_delivery.go:131`) — one `WithAllTenants` scan of due ids (`LEFT JOIN` announcements, `announced_version IS NULL OR < name_version`, bounded batch), then per hospital a `WithTenant` tx with `tenantdb.WithActor(ctx, "system:workforce-hospital-name-announcer")` that re-reads `name, name_version` `FOR UPDATE` and re-checks due, publishes through `events.Enqueue`, and upserts the announcement row. Log a per-hospital failure and continue, as the redelivery pass does.
- [ ] **Step 4: Run** the Step 2 command. Expected: PASS. Then `go test ./internal/modules/workforce -count=1` to confirm the existing workforce suite (audit triggers included) is unchanged.
- [ ] **Step 5: Mutation check** — make the trigger bump on every UPDATE; `TestHospitalNameVersionFollowsTheName` fails. Restore.
- [ ] **Step 6: Commit** `feat(workforce): version hospital names and announce each version once (#1637)`

### Task 2: Pharmacy keeps the names and discloses them through verified affiliations

**Files:**

- Create: `backend/internal/modules/pharmacy/hospital_names.sql`, `backend/internal/modules/pharmacy/hospital_names.go`
- Modify: `backend/internal/modules/pharmacy/module.go:290` (append `0045_pharmacy_hospital_names`), `backend/internal/modules/pharmacy/consumers.go:28` (register the consumer), `backend/internal/modules/pharmacy/registry.go:46-55` (field), `registry.go:292,327` and `affiliation_verification.go:152` (call the fill after `fillVerification`)
- Test: `backend/internal/modules/pharmacy/hospital_names_test.go`

**Interfaces:**

- Consumes: `workforcecontract.SubjectHospitalNamed`, `HospitalNamedData` (Task 1).
- Produces: `hospitalAffiliation.HospitalName *string \`json:"hospital_name" gorm:"-"\``; `func hospitalNameConsumer() events.Consumer`named`pharmacy-hospital-names`; `func fillHospitalNames(tx *gorm.DB, tenant string, rows []hospitalAffiliation) error`, called after `fillVerification` wherever that runs.

- [ ] **Step 1: Write the failing tests** with `catalogueHarness` and the `TestAffiliationVerificationLifecycle` helpers (`declare`, `decide`, `deliverDecision`):
  - `TestHospitalNamesKeepTheNewestVersion`: deliver version 2 "B", then version 1 "A", then version 2 "B" again → one row, "B", version 2; an invalid payload (missing id, empty name, version 0) returns an error.
  - `TestHospitalNameShownOnlyForAVerifiedAffiliation`: name stored for hospital H; a declared affiliation to H lists `"hospital_name": null`; after a request and an accepted decision it lists the name; after a revoke it is `null` again. A declined affiliation and a withdrawn one are `null`. A hospital with a stored name and no affiliation never appears in the list.
  - `TestHospitalNamesStayInTheirTenant`: a name delivered for tenant B under the same hospital ID is not shown on tenant A's verified affiliation.
- [ ] **Step 2: Run** `cd backend && GOTOOLCHAIN=go1.27.3 go test ./internal/modules/pharmacy -run 'TestHospitalName' -count=1`. Expected: FAIL — no consumer named `pharmacy-hospital-names`.
- [ ] **Step 3: Implement.** SQL: `pharmacy_hospital_names (tenant_id, hospital_id, name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 150), name_version bigint NOT NULL CHECK (name_version >= 1), updated_at)`, primary key `(tenant_id, hospital_id)`, forced-RLS boilerplate. Consumer: `INSERT … ON CONFLICT (tenant_id, hospital_id) DO UPDATE … WHERE pharmacy_hospital_names.name_version < EXCLUDED.name_version`, tenant from the envelope. Fill: one query for the page's verified hospital IDs; set `HospitalName` only on rows whose `VerificationStatus == "verified"`. Comment at that check that it fails closed: any other status, including a future one, discloses nothing.
- [ ] **Step 4: Run** the Step 2 command, then `go test ./internal/modules/pharmacy -run 'TestAffiliation' -count=1`. Expected: PASS, existing affiliation tests unchanged.
- [ ] **Step 5: Mutation check** — drop the `verified` condition in the fill; the disclosure test fails. Drop the version guard in the upsert; the newest-version test fails. Restore both.
- [ ] **Step 6: Commit** `feat(pharmacy): keep hospital names and show them on verified affiliations (#1637)`

### Task 3: Pickers show the hospital name

**Files:**

- Create: `apps/shell/components/pharmacy/hospital-options.ts`
- Modify: `packages/api/src/pharmacy.ts:378` (`hospital_name: string | null` on `PharmacyHospitalAffiliation`), `apps/shell/components/pharmacy/counter-patient.tsx:49,86-99,151`, `apps/shell/components/pharmacy/counter-admission.tsx:51-55,89,165`
- Test: `apps/shell/components/pharmacy/counter-patient.test.tsx`, `counter-admission.test.tsx`

**Interfaces:**

- Consumes: `hospital_name` on the affiliation list (Task 2).
- Produces: `export function hospitalOptions(affiliations: PharmacyHospitalAffiliation[]): SelectOption[]` — verified only, one option per hospital ID, labels per Global Constraints. Both pickers pass the result straight to `FormSelectField` and preselect when there is exactly one option, as today.

- [ ] **Step 1: Write the failing tests**, in each picker's test file (fixtures gain `hospital_name`):
  - "lists a verified hospital by its name": option "TEST City Hospital", no UUID in its label.
  - "adds the ID when two verified hospitals share a name": both options read `TEST City Hospital · <id>`.
  - "says the name is unavailable": one fixture with `hospital_name: null`, one with the field deleted; both read `Name unavailable · <id>`.
  - "lists a hospital once when two affiliations point at it": two verified rows, same `hospital_id` and name → one option, label is the bare name.
- [ ] **Step 2: Run** `pnpm --filter @helivanta/shell exec vitest run components/pharmacy/counter-patient.test.tsx components/pharmacy/counter-admission.test.tsx`. Expected: FAIL — options are labelled with the ID.
- [ ] **Step 3: Implement** `hospitalOptions` and use it in both pickers in place of the local `hospitals` arrays; the forms keep `hospital_id` as the value.
- [ ] **Step 4: Run** the Step 2 command. Expected: PASS, existing picker tests unchanged. Mutation: return the bare name for duplicates; the shared-name test fails. Restore.
- [ ] **Step 5: Commit** `feat(pharmacy): the counter pickers show hospital names (#1637)`

### Task 4: Contracts, docs and the browser check

**Files:**

- Modify: `docs/api/domains/workforce.md` (the event: subject, data, when sent, delivery delay, no backfill needed), `docs/api/domains/pharmacy.md` (`hospital_name` and its disclosure rule; hand-edit, do not run prettier on this file), `docs/api/integration-contracts.md` (one row: workforce hospital name → pharmacy), `docs/qa/2026-11-09-pharmacy-counter-acceptance-plan.md` (F-3 "Done (#1637); verify with PAT-01"; PAT-01 and the admission cases expect hospital names), generated contract docs
- Modify: `e2e/tests/support/pharmacy-reports.ts:22,107` (return the hospital name it sets)
- Create: `e2e/tests/pharmacy-counter-hospital-names.spec.ts`

- [ ] **Step 1: Write the e2e test** — `readyOutlet`, then `affiliatedPatient`; open the counter's patient lookup and expect the "Hospital" picker to show the returned name (poll up to 60 s for the announcer), with no UUID in the option label.
- [ ] **Step 2: Run it** against the local stack: `pnpm --filter e2e exec playwright test pharmacy-counter-hospital-names`. Expected: PASS (it would fail on main: the label is the ID).
- [ ] **Step 3: Docs and contracts** — the edits above; then from `backend/`: `GOTOOLCHAIN=go1.27.3 go run ./cmd/contractdocs` and `-check`. Expected: the new subject, consumer and migrations recorded, `-check` clean.
- [ ] **Step 4: Gate** — `pnpm turbo lint type-check test build format:check --continue --concurrency=4`, `make lint-shell`, `make lint-go`, `cd backend && GOTOOLCHAIN=go1.27.3 ./scripts/coverage-gate.sh`, `go test -race ./internal/modules/workforce ./internal/modules/pharmacy`. Expected: all green.
- [ ] **Step 5: Commit** `docs(pharmacy): hospital names in the counter pickers and PAT-01 (#1637)`

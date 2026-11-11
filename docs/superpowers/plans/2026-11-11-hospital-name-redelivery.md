# Hospital name redelivery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A hospital's current name reaches pharmacy without an operator after a `hospital_named.v1` delivery dead-letters (#1653).

**Architecture:** Pharmacy confirms each name version it handles, in the same transaction as the upsert. Workforce marks every announced version unconfirmed. Its existing 30-second announcer re-sends due, unconfirmed versions with backoff from 2 to 60 minutes, and a new receipt consumer clears them. Mirrors `pharmacy/affiliation_delivery.go`.

**Tech Stack:** Go 1.27.4, GORM, Postgres (forced RLS), `pkg/events` outbox and JetStream.

**Spec:** `docs/superpowers/specs/2026-11-11-hospital-name-redelivery-design.md`

## Global Constraints

- Subject `helivanta.in.pharmacy.hospital_name_received.v1`; constant `SubjectHospitalNameReceived`; payload `HospitalNameReceivedData{HospitalID string \`json:"hospital_id"\`; NameVersion int64 \`json:"name_version"\`}`, with no name.
- Consumer name `workforce-hospital-name-receipt`.
- Migration ID `0098_workforce_hospital_name_receipts`. Recheck it against `origin/main` and the `codex/*` branches before merge.
- `firstRedelivery = 2 * time.Minute`, `maxRedelivery = time.Hour`. The backoff never gives up.
- `hospitalNameBatch` stays 200; the ticker stays 30 s; the actor stays `system:workforce-hospital-name-announcer`.
- The cross-tenant scan reads ids only, never names.
- Modules never import each other except through `contract` packages. Tests may import a module under test with `//nolint:depguard`, as the existing tests do.
- Never run prettier on `docs/api/domains/pharmacy.md`; edit it by hand.
- Run Go tests with `GOTOOLCHAIN=go1.27.4` and `-timeout=20m`.
- No AI attribution in commits.

## Review Focus

1. **A late receipt for an old version after a rename.** It must not stop re-sending of the new version. Pinned in Task 2: `TestReceiptForAnOlderVersionKeepsResending`.
2. **A rename while the old version is still unconfirmed.** Only the new version is ever sent afterwards. Pinned in Task 3: `TestRenameSupersedesAnUnconfirmedVersion`.
3. **A backlog of re-sends.** It must not starve first announcements. Pinned in Task 3: `TestFirstAnnouncementsComeBeforeResends`.
4. **The confirmation itself dead-letters.** The name is re-sent, pharmacy re-confirms and the loop converges. Covered by the recovery test in Task 3, which drops the confirmation once.
5. **Pre-existing hospitals at deploy.** Every existing marker is due immediately. Pinned in Task 2: `TestMigrationMakesExistingMarkersDue`.

---

### Task 1: Pharmacy confirms every hospital name it handles

**Files:**
- Modify: `backend/internal/modules/pharmacy/contract/events.go` (add the subject and type after `AffiliationDecisionReceivedData`)
- Modify: `backend/internal/modules/pharmacy/hospital_names.go` (`hospitalNameConsumer`)
- Modify: `backend/internal/modules/pharmacy/module.go` (`Publishes`)
- Test: `backend/internal/modules/pharmacy/hospital_names_test.go`

**Interfaces:**
- Produces: `pharmacycontract.SubjectHospitalNameReceived`, `pharmacycontract.HospitalNameReceivedData` (Global Constraints).

- [ ] **Step 1: Write the failing tests** in `hospital_names_test.go`. Reuse `consumerNamed` and `deliverName`, and add a helper `receipts(t, db, tenant, hospital) []pharmacycontract.HospitalNameReceivedData`. It reads `outbox_events` where `subject = SubjectHospitalNameReceived`, decodes `payload->'data'`, and asserts that the data has exactly 2 keys.
  - `TestHospitalNameIsConfirmed`: deliver v1, then assert one receipt `{hospital, 1}` and that the stored name is the delivered name.
  - `TestStaleHospitalNameIsStillConfirmed`: deliver v2 and then v1. Assert receipts `[{h,2},{h,1}]`, oldest first, and that the stored name is still v2's.
  - `TestRejectedHospitalNameIsNotConfirmed`: deliver an empty name, a nil UUID and `name_version` 0. Each returns an error. Assert no receipt and no name row.
  - `TestHospitalNameAndConfirmationCommitTogether`: run the handler inside `WithTenant` and return an error after it. Assert that neither the name row nor the receipt exists.

- [ ] **Step 2: Run** `cd backend && GOTOOLCHAIN=go1.27.4 go test -timeout=20m ./internal/modules/pharmacy -run 'HospitalName'`. Expected: compile failure on `SubjectHospitalNameReceived`.

- [ ] **Step 3: Implement.** Add the subject and type, each with a doc comment. The comment says the payload confirms receipt so workforce stops re-sending, and carries no name. In `hospitalNameConsumer`, after the upsert succeeds, `events.Enqueue(tx, SubjectHospitalNameReceived, events.Event{Type: …, Version: 1, TenantID: evt.TenantID, Data: …})` with the *received* `NameVersion`. Add a one-line comment saying a stale version is confirmed too, so its re-sending stops. Add the subject to pharmacy's `Publishes`.

- [ ] **Step 4: Run** the Step 2 command. Expected: PASS. Then run the mutation check: drop the `Enqueue` and confirm that the first three tests fail; restore it.

- [ ] **Step 5: Commit** `feat(pharmacy): confirm each hospital name it handles (#1653)`.

### Task 2: Workforce tracks unconfirmed versions and consumes receipts

**Files:**
- Create: `backend/internal/modules/workforce/hospital_name_receipts.sql`
- Create: `backend/internal/modules/workforce/hospital_name_receipts.go` (embed, receipt consumer, `redeliveryDelay`)
- Modify: `backend/internal/modules/workforce/hospital_names.go` (the claim statement only)
- Modify: `backend/internal/modules/workforce/module.go` (append the migration, add the consumer to both `Consumers` branches)
- Modify: `backend/internal/globalapi/workforce_person_review_test.go`: add `0098_workforce_hospital_name_receipts` to the skip list (about line 388) and the apply list (about line 2221)
- Test: `backend/internal/modules/workforce/hospital_name_receipts_test.go`

**Interfaces:**
- Consumes: Task 1's `pharmacycontract.SubjectHospitalNameReceived`, `HospitalNameReceivedData`.
- Produces: the columns `attempts` and `next_attempt_at` on `workforce_hospital_name_announcements`; `redeliveryDelay(attempts int) time.Duration` (unexported); the consumer `workforce-hospital-name-receipt`; the constants `firstRedelivery` and `maxRedelivery`.

- [ ] **Step 1: Write the failing tests.** Use the existing `establishmentHarness`. Add a helper `marker(t, id) (announced int64, attempts int, next *time.Time)` and a helper `receipt(t, id string, version int64) error`, which calls the consumer's `Handle` the way pharmacy's `deliverName` does.
  - `TestClaimStartsUnconfirmed`: create a hospital and run `AnnounceHospitalNamesOnce`. Assert `attempts == 0` and that `next` is between 1 m 50 s and 2 m 10 s from now.
  - `TestReceiptClearsTheAnnouncedVersion`: receipt v1, then assert `next == nil`.
  - `TestReceiptForANewerVersionClears`: receipt v5, then assert `next == nil`.
  - `TestReceiptForAnOlderVersionKeepsResending`: rename and announce v2, then receipt v1. Assert `next != nil`.
  - `TestReceiptForAnUnknownHospitalIsANoOp`: a random UUID returns nil and leaves no row.
  - `TestInvalidReceiptIsRejected`: table cases, each returning an error: wrong `Type`, `Version` 2, non-empty `DestinationTenantID`, non-UUID id, nil UUID, `name_version` 0.
  - `TestMigrationMakesExistingMarkersDue`: mirror `workforceBeforeDocumentProvenance` (`internal/globalapi/workforce_person_review_test.go:43-60`). Use a module wrapper whose `Migrations()` stops before `0098_workforce_hospital_name_receipts`, on a fresh test database. Create a hospital and announce it, then `db.Migrate(ctx, []tenantdb.Migration{<the 0098 entry from workforce.New().Migrations()>})`. Assert `next_attempt_at <= now()` and `attempts == 0`. This is the real migration on real pre-existing rows, not a copy of its SQL.
  - `TestRedeliveryDelay`: table 1→2m, 2→4m, 3→8m, 4→16m, 5→32m, 6→60m, 20→60m.

- [ ] **Step 2: Run** `cd backend && GOTOOLCHAIN=go1.27.4 go test -timeout=20m ./internal/modules/workforce -run 'Receipt|ClaimStarts|RedeliveryDelay|MigrationMakes'`. Expected: compile failure.

- [ ] **Step 3: Write the migration.** Start with `SET LOCAL lock_timeout='5s'; SET LOCAL statement_timeout='60s';`. Add `attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0)` and `next_attempt_at timestamptz`, with a comment saying `NULL` means the announced version is confirmed. `UPDATE … SET next_attempt_at = clock_timestamp()` with a comment saying this recovers names lost before #1653. Create the index `workforce_hospital_name_announcements_due ON (next_attempt_at) WHERE next_attempt_at IS NOT NULL`, and `GRANT SELECT (next_attempt_at) … TO helivanta_system`.

- [ ] **Step 4: Change the claim.** Its `INSERT` and `DO UPDATE SET` also write `attempts = 0, next_attempt_at = clock_timestamp() + make_interval(secs => ?)`, with `firstRedelivery.Seconds()`. Keep the `WHERE announced_version < EXCLUDED.announced_version` guard.

- [ ] **Step 5: Write the consumer.** Model it on `affiliationReceiptConsumer` and use the spec's D3 `UPDATE` verbatim. Put a header comment on the file mirroring `affiliation_delivery.go:21-24`: why re-sending exists, and that the bus dead-letters with no replay. Copy `redeliveryDelay` with the same doc comment. Register the migration and the consumer.

- [ ] **Step 6: Run** the Step 2 command, plus `go test -timeout=20m ./internal/modules/workforce -run HospitalName`. Expected: PASS. Mutation checks: change `<=` to `=` in the receipt `UPDATE` and confirm `TestReceiptForANewerVersionClears` fails; drop the `next_attempt_at` write from the claim and confirm `TestClaimStartsUnconfirmed` fails. Restore both.

- [ ] **Step 7: Run** `GOTOOLCHAIN=go1.27.4 go test -timeout=20m ./internal/globalapi -run WorkforcePersonReview`. Expected: PASS. It takes about 14 minutes; run it in the background.

- [ ] **Step 8: Commit** `feat(workforce): track unconfirmed hospital names and consume receipts (#1653)`.

### Task 3: The announcer re-sends unconfirmed versions

**Files:**
- Modify: `backend/internal/modules/workforce/hospital_names.go` (`AnnounceHospitalNamesOnce`)
- Test: `backend/internal/modules/workforce/hospital_name_receipts_test.go`, plus `hospital_name_recovery_test.go` for the cross-module test

**Interfaces:**
- Consumes: Task 2's columns, `redeliveryDelay`, `firstRedelivery`; Task 1's pharmacy consumer (in the recovery test only, via `pharmacy.New().Consumers(platform.Deps{})`, with `//nolint:depguard // recovery crosses both modules`).
- Produces: nothing new for later tasks.

- [ ] **Step 1: Write the failing tests.** Add a helper `makeDue(t, id)` that sets `next_attempt_at = now() - interval '1 second'`.
  - `TestDueUnconfirmedNameIsResent`: announce, `makeDue`, pass. Assert `announced` has 2 payloads, both with the same `name_version` and the current name; `attempts == 1`; `next` about 2 minutes ahead (`redeliveryDelay(1)`). `makeDue` and pass again: `attempts == 2`, `next` about 4 minutes ahead.
  - `TestConfirmedNameIsNotResent`: announce, receipt (so `next_attempt_at` is `NULL`), set the marker's `updated_at` back an hour, and run a pass. Assert one payload. This catches a scan that treats `NULL` as due.
  - `TestNotYetDueNameIsNotResent`: announce, pass again immediately. Assert one payload.
  - `TestRenameSupersedesAnUnconfirmedVersion`: announce v1, rename, `makeDue`, pass. Assert payloads `[v1, v2]` and `attempts == 0`. `makeDue`, pass: payloads `[v1, v2, v2]`. v1 never reappears.
  - `TestResendRacingPassesSendOnce`: use the pattern of `TestAnnouncerPassesRacingEachOtherPublishOnce` on a due marker. Assert exactly one new payload.
  - `TestFirstAnnouncementsComeBeforeResends`: create a due marker, then a new hospital. Call a pass with an effective batch of 1. Add an unexported `announceHospitalNames(ctx, db, batch int)` that `AnnounceHospitalNamesOnce` calls with `hospitalNameBatch`, exported to the test via `export_test.go`. Assert the new hospital was announced and the due one was not re-sent.
  - In `hospital_name_recovery_test.go`, `TestDeadLetteredHospitalNameRecovers` covers both a first announcement and a rename. Announce, and do not deliver the event (the dead-letter). Pharmacy has no name or the old name. `makeDue` and pass. Take the newest `hospital_named` outbox payload, hand it to pharmacy's `pharmacy-hospital-names` handler, read the `hospital_name_received` outbox payload it wrote, and hand that to `workforce-hospital-name-receipt`. Assert that pharmacy's `pharmacy_hospital_names` row has the current name and that `next == nil`. A further pass adds no payload.
  - Add a dropped-confirmation variant of the same test: discard the first receipt and `makeDue` again; the re-send and second receipt converge.

- [ ] **Step 2: Run** `cd backend && GOTOOLCHAIN=go1.27.4 go test -timeout=20m ./internal/modules/workforce -run 'Resen|Rename|FirstAnnouncements|DeadLettered|NotYetDue|Confirmed'`. Expected: FAIL; no re-send happens.

- [ ] **Step 3: Implement**, per spec D4.
  - The scan's `WHERE` adds `OR a.next_attempt_at <= now()`. `ORDER BY (a.announced_version IS NULL OR a.announced_version < h.name_version) DESC, a.next_attempt_at NULLS FIRST, h.tenant_id, h.id`. It still selects ids only.
  - Per hospital, after the `FOR UPDATE`: if the claim affects a row, keep the existing path. If it affects zero rows, run a locked re-check: `SELECT attempts FROM workforce_hospital_name_announcements WHERE tenant_id=? AND hospital_id=? AND announced_version=? AND next_attempt_at <= now() FOR UPDATE`. No row means return nil.
  - Otherwise enqueue the same payload, update `attempts = attempts + 1, next_attempt_at = clock_timestamp() + make_interval(secs => redeliveryDelay(attempts+1).Seconds())`, and log the D4 warning.
  - Keep the payload construction in one place, shared by both branches.

- [ ] **Step 4: Run** the Step 2 command and `-run HospitalName`. Expected: PASS. Mutation checks: drop `next_attempt_at <= now()` from the re-check and confirm `TestNotYetDueNameIsNotResent` fails; drop the `ORDER BY` priority term and confirm `TestFirstAnnouncementsComeBeforeResends` fails. Restore both.

- [ ] **Step 5: Commit** `feat(workforce): re-send hospital names pharmacy has not confirmed (#1653)`.

### Task 4: Single-consumer guard, contracts and docs

**Files:**
- Modify: `backend/internal/archtest/arch_test.go` (new test)
- Modify: `docs/api/domains/workforce.md` (line 120 paragraph), `docs/api/domains/pharmacy.md` (line 697 paragraph, by hand), `docs/api/integration-contracts.md` (line 107 row)
- Regenerate: `docs/api/reference` via `go run ./cmd/contractdocs`
- Possibly: `docs/qa/staff-operational-access-reviews.json`, inventory

**Interfaces:**
- Consumes: everything above.

- [ ] **Step 1: Write the failing check.** `TestHospitalNamedHasOneConfirmingConsumer` iterates `allModules()` and `Consumers(platform.Deps{})`, and collects consumers whose `Subject == workforcecontract.SubjectHospitalNamed`. Assert they are exactly `["pharmacy-hospital-names"]`. The message says: "a second consumer needs per-consumer confirmation (#1653): workforce's next_attempt_at records one". Prove it can fail by temporarily registering a duplicate consumer in a test-local slice passed through the same helper; extract `confirmingConsumers(mods) []string` so the proof is a unit call, not a production edit.

- [ ] **Step 2: Run** `GOTOOLCHAIN=go1.27.4 go test -timeout=20m ./internal/archtest -run HospitalNamed`. Expected: PASS on real modules, FAIL on the fabricated list.

- [ ] **Step 3: Update the docs.**
  - In `workforce.md`, replace the sentence "If delivery to a consumer fails 5 times … next rename." with: the announcement stays unconfirmed until pharmacy's `hospital_name_received.v1` arrives; unconfirmed versions are re-sent from 2 minutes, doubling to hourly, never given up; a dead-lettered name arrives within about an hour after the failure clears; `pharmacy-hospital-names` is the only consumer allowed (archtest).
  - In `pharmacy.md`, replace "If delivery of a name event fails 5 times … next rename." with: the consumer confirms each version it handles, including stale ones, in the same transaction, and workforce re-sends unconfirmed names, so a dead-lettered name arrives within about an hour after the failure clears.
  - In `integration-contracts.md`, the row gains "confirmed by `hospital_name_received.v1` `{hospital_id, name_version}`; unconfirmed versions re-sent with 2–60 min backoff".

- [ ] **Step 4: Regenerate and check.** `cd backend && go run ./cmd/contractdocs && go run ./cmd/contractdocs -check`. Expected: the second prints no diff. Then run `node scripts/staff-operational-access-inventory.mjs --check` from the repo root. If pinned hashes changed, **stop and ask the user** before adding recheck notes. Regenerate the inventory if `--check` reports it stale.

- [ ] **Step 5: Run the full gate** from the repo root: `make lint-go`, then `cd backend && GOTOOLCHAIN=go1.27.4 ./scripts/coverage-gate.sh`. Expected: green, with coverage at or above 70%.

- [ ] **Step 6: Commit** `docs(workforce,pharmacy): hospital names are confirmed and re-sent (#1653)`.

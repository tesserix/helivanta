# Shift events Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish `shift_opened`, `shift_variance_recorded`, `shift_closed`, `cash_pickup_recorded` and `shift_handed_over` from billing.

**Architecture:** Migration `0016_billing` adds `AFTER` triggers on the shift, pickup and handover tables that write outbox rows, as `privacy/deadlines.sql` does. Subjects come from `billing/contract` constants substituted at migration time. Payload types live in `billing/contract/events.go` for the generated reference.

**Tech Stack:** Go, GORM, PostgreSQL 16.

**Spec:** `docs/superpowers/specs/2026-11-03-shift-events-design.md`

## Global Constraints

- Migration ID `0016_billing`, embedded as `eventsSchema` from `events.sql`; placeholders `${SHIFT_OPENED}` etc. replaced in `Migrations()`.
- Envelope: `event_id`, `type` (`ShiftOpened`, `ShiftVarianceRecorded`, `ShiftClosed`, `CashPickupRecorded`, `ShiftHandedOver`), `version` 1, `tenant_id`, `occurred_at`, `data`.
- No reasons, counts, patients or admissions in any payload.

## Review Focus

- A retried close, authorisation, pickup or acceptance publishes nothing new.
- Offering, withdrawing or disputing a handover publishes nothing.
- No payload carries `expected_minor` before the count.
- A rolled-back change (a refused close) publishes nothing.
- Payload keys equal the Go type's fields, both ways.

---

### Task 1: Contract types and triggers

**Files:**

- Modify: `backend/internal/modules/billing/contract/events.go` (five `Subject*` constants, five `*Data` types)
- Create: `backend/internal/modules/billing/events.sql`
- Modify: `backend/internal/modules/billing/module.go` (`eventsSchema`, `0016_billing`, `Publishes()`)
- Test: `backend/internal/modules/billing/events_test.go`

- [ ] **Step 1: Write the failing tests**
  - `TestAShiftPublishesItsLife`: open (float 1000) → one `shift_opened`; replay → still one. Pay 3000, admin picks up `note:2000` → one `cash_pickup_recorded` (`kind` pickup, 2000, `recorded_by` admin); a correction of 500 → a second with `corrects_id`. No variance or close event while open. Close declaring 1000 against 1500 expected → one `shift_variance_recorded` (-500), no `shift_closed`; replayed close publishes nothing. Authorise → one `shift_closed` with `authorised_by`. A second shift closing exactly → `shift_closed` only, `authorised_by` null. A close refused (another cashier) publishes nothing. Envelope checked once: `event_id` equals the row id, `type`, `version`, `tenant_id`.
  - `TestAHandoverPublishesTheChangeOfCashier`: offer and withdraw → nothing; offer, accept → one `shift_handed_over`, `shift_closed` (`handed_over` true) for the old shift, `shift_opened` with `handover_id` for the new. A disputed then settled handover → `shift_handed_over` with `settled_by`, `shift_variance_recorded` for the short count.
  - Helper `published[T]` decodes each payload strictly into `T` and requires the payload's keys to equal `T`'s json tags.
- [ ] **Step 2: Run, expect FAIL** — `go test ./internal/modules/billing -run 'Publishes' -count=1`: undefined contract types.
- [ ] **Step 3: Implement** the constants, types, `events.sql` (one `billing_publish(tenant, subject, type, at, data)` helper and three triggers) and the module wiring.
- [ ] **Step 4: Run, expect PASS**, then mutation-check: drop each trigger branch and one payload key; each run fails.
- [ ] **Step 5: Commit**

### Task 2: Docs and generated reference

- [ ] `docs/api/domains/billing.md`: a "Shift events (#198 slice 7)" section; correct "events" in earlier slices' not-covered lists. Same in the slice specs and `portfolio.md`.
- [ ] From `backend`: `go run ./cmd/contractdocs` then `-check`; then `node scripts/staff-operational-access-inventory.mjs` and `--check`.
- [ ] Gates: billing `-race`, `make lint-go`, archtest. Commit.

# Card, netbanking and wallet receipts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Record card, netbanking and wallet payments and deposits at a counter that accepts them, outside the drawer, reported by mode.

**Architecture:** Migration `0015_billing` adds an append-only `billing_counter_modes`, widens receipt `mode`, adds reference columns with checks and a partial unique index, and replaces `billing_receipt_guard`. Handlers extend `receive`, `voidReceipt`, counters and the daily collection. The shell extends `CashPayments`, `Counters` and `DailyCollectionReport`.

**Tech Stack:** Go, Gin, GORM, PostgreSQL 16, Next.js, zod, Vitest.

**Spec:** `docs/superpowers/specs/2026-11-02-card-netbanking-wallet-design.md`

## Global Constraints

- Migration ID `0015_billing`, embedded as `modesSchema` from `modes.sql`.
- Modes: `cash`, `upi`, `card`, `netbanking`, `wallet`; only the last three are switched per counter.
- Error codes: `mode_not_accepted` (409), `duplicate_reference` (409).
- No card digits anywhere.

## Review Focus

- A counter switched off between the form loading and submit: the receipt is refused, not taken.
- The same UTR on two admissions: the second is refused while the first stands, accepted once it is voided.
- Voiding a card payment after cash refunds emptied the drawer: allowed.
- A card deposit voided after it was applied: refused.
- An open shift's card takings never reach the daily collection.

---

### Task 1: Schema

**Files:** Create `backend/internal/modules/billing/modes.sql`; modify `module.go`; test `modes_test.go`.

- [ ] Write `TestModesHoldInTheDatabase`: direct SQL as a cashier cannot insert a card receipt on a counter without card; cannot insert one with card digits shaped reference missing approval code; a duplicate standing reference violates the unique index; a voided one frees it; voiding a card receipt with an empty drawer succeeds.
- [ ] Run it: FAIL (no migration).
- [ ] Write `modes.sql` per the spec's Enforcement; register `0015_billing`.
- [ ] Run it: PASS.

### Task 2: API

**Files:** modify `cash.go`, `collections.go`, `module.go`; test `modes_test.go`, `collections_test.go`.

- [ ] Write `TestACounterTakesTheModesItAccepts` (switch on/off, replay `200`, list shows `modes`, receipt refused with `mode_not_accepted`), `TestCardReceiptsStayOutOfTheDrawer` (card payment and deposit; shift closes against cash only; void with no drawer check; replay and conflict; `duplicate_reference`; refund with mode card `400`), and extend the collection test with per-mode columns and blinding.
- [ ] Run: FAIL.
- [ ] Implement `putCounterMode`, `counter.Modes`, `receive` mode fields, void without drawer check for non-cash, collection columns and CSV.
- [ ] Run: PASS; regenerate contract docs and QA inventory.

### Task 3: Shell

**Files:** modify `packages/api/src/billing.ts`, `apps/shell/components/medicore/cash-payments.tsx`, `cash-desk.tsx`, `daily-collection.tsx` and their tests.

- [ ] Write tests: mode choice limited to the counter's modes; card asks for approval code and reference and sends them; receipt shows mode and reference and offers void; counters switch modes; collection shows the new columns.
- [ ] Run: FAIL.
- [ ] Implement.
- [ ] Run: PASS; `pnpm turbo lint type-check test build format:check`.

### Task 4: Docs

- [ ] `docs/api/domains/billing.md` section, `portfolio.md`, superseded "card/netbanking/wallet out of scope" lines in earlier specs.

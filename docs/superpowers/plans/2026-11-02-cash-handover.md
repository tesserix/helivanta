# Handover between cashiers Implementation Plan

**Goal:** Hand a counter's drawer from one cashier to the next with two blind
counts, a supervisor settling any disagreement, in one transaction.

**Spec:** `docs/superpowers/specs/2026-11-02-cash-handover-design.md`

## Global Constraints

- Migration `0014_billing` (`handovers.sql`), append-only; forced RLS, one
  permissive policy, audit and kept triggers, as every billing table.
- Triggers decide states and actors; handlers only translate errors.
- A count is the slice-4 `cashCount` / `billing_cash_count_total`.
- Neither cashier ever receives the other's count or the expected cash
  before the handover is resolved.

## Review Focus

1. Receipts, refunds, voids, pickups and UPI requests on a `handing_over` shift: refused.
2. Probing: a second acceptance with a different count after a dispute: refused.
3. Supervisor who is one of the two cashiers: refused.
4. Incoming cashier who already holds an open shift: the whole acceptance rolls back.
5. Concurrent withdrawal and acceptance: exactly one wins.

## Task 1: Schema and guards (`handovers.sql`, `module.go`)

- `billing_cash_handovers`: tenant, hospital, id (v7), `from_shift_id`,
  `counter_id`, `from_cashier`, `to_cashier`, currency, `outgoing_counts`,
  `incoming_counts`, `agreed_counts`, `agreed_minor`, `to_shift_id`, status
  `offered|withdrawn|disputed|accepted|settled`, `settled_by`,
  `settlement_reason`, timestamps. One live (`offered|disputed`) and one
  resolved (`accepted|settled`) handover per shift.
- Guard trigger on handovers: insert by the open shift's cashier to someone
  else with a valid count; offered → withdrawn by `from_cashier`; offered →
  accepted (counts equal) / disputed (counts differ) by `to_cashier`;
  disputed → settled by neither cashier, with a reason.
- Shifts: status `handing_over`; `handover_id` (opened from) and
  `handed_over` (closed by); counter index ignores handed-over shifts.
  Guard transitions as in the spec.
- Tests (`handovers_test.go`): `TestAHandoverMovesTheDrawer`,
  `TestAHandoverThatDisagreesWaitsForASupervisor`,
  `TestAHandingOverShiftTakesNothing`, `TestHandoversHoldInTheDatabase`,
  `TestAHandoverIsWithdrawnOnlyWhileOffered`, `TestAcceptanceRacesWithdrawal`.

## Task 2: Handlers and routes (`handovers.go`, `cash.go`, `collections.go`)

- Routes: `PUT cash-shifts/:shift/handovers/:handover`,
  `PUT cash-handovers/:handover/withdrawal`,
  `PUT cash-handovers/:handover/acceptance` (`billing.cash.collect`);
  `PUT cash-handovers/:handover/settlement` (`billing.shift.authorise`);
  lists `cash-handovers/incoming`, `cash-handovers/disputed`.
- `shift` gains `handover_id`, `handed_over` and, for `handing_over`, the
  live handover id; `shifts` accepts `?status=handing_over`; the daily
  collection blinds every shift not yet counted.

## Task 3: Shell and docs

- `packages/api/src/billing.ts`: `CashHandover` type and schemas.
- `cash-desk.tsx`: hand over, incoming offers with a blind count, disputed
  handovers for the supervisor; Vitest tests.
- `docs/api/domains/billing.md`, superseded lines, contract docs, QA inventory.

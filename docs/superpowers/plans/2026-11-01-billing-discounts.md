# Discounts on room charges Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Reasoned, limited, approved discounts on an admission's exempt room charges, posted to the ledger.

**Architecture:** Migration `0012_billing` (`discounts.sql`) adds reasons, discounts and the entry
link; triggers decide status, allocate and bound. `discounts.go` serves the routes;
`bed_days.go` withdraws a voided day's discounts. The shell adds a reasons panel and a desk form.

**Tech Stack:** Go (gin, gorm), Postgres plpgsql, Next.js shell, `@helivanta/api`.

**Spec:** `docs/superpowers/specs/2026-11-01-billing-discounts-design.md`

## Global Constraints

- Currency INR only; amounts 1–1000000000 paise; basis points 0–10000.
- Ids UUIDv7 from the client; replay `200`, conflict `409`.
- Permissions: `billing.discount.manage`, `billing.discount.approve` (administrators only);
  asking needs `billing.bill.issue`; reading `billing.account.read`; all under the hospital grant.
- Error codes: `discount_over_limit`, `nothing_to_discount`, `discount_over_balance`,
  `reason_retired`, `own_request`, `already_decided`.
- Every money rule lives in the database; Go pre-checks only to say why.

## Review Focus

1. Allocation sums exactly to the amount with uneven remainders (three charges, 1 paisa leftovers).
2. A discounted day voided by the census: withdrawal then full reversal, net zero, no bound error.
3. Concurrent approvals or requests on one admission cannot exceed the room or the limits.
4. A taxable or invoiced charge is never discounted; an invoice after a discount shows net value.
5. Approver equal to requester refused in the database, not only in Go.

---

### Task 1: Schema and rules (`discounts.sql`, `0012_billing`)

**Files:** Create `backend/internal/modules/billing/discounts.sql`; modify `module.go`
(embed, migration, permissions); test `discounts_test.go`.

- Tables `billing_discount_reasons`, `billing_discounts` per spec; RLS, audit, kept triggers.
- `billing_account_entries.discount_id` + check + FK; insert policy `discount_desk`.
- Functions: `billing_room_taxable(tenant, day)`, `billing_discount_base`,
  `billing_discount_given(tenant, admission, reason)`, `billing_discountable(tenant, admission)`;
  `billing_discount_guard` (insert: status; update: decision rules), `billing_discount_apply`
  (allocation, AFTER trigger), deferred `billing_discount_complete`, entry guard
  `billing_discount_entry_guard`; replaced `billing_reversal_bounded`,
  `billing_tax_invoice_line_fill` (+`discount_minor`), value checks relaxed to `>= 0`.
- Tests (DB level): desk-limit applies, over-desk pends, over-approval refused; cumulative
  limit; allocation exact over uneven charges; taxable and invoiced charges excluded; own
  approval refused; decided is final; entries outside the applying transaction refused.

### Task 2: Routes (`discounts.go`)

Handlers `putDiscountReason`, `retireDiscountReason`, `discountReasons`, `putDiscount`,
`discounts`, `pendingDiscounts`, `decideDiscount`; error mapping; account totals gain
`discountable_minor`. Tests through HTTP: each route's success, replay, conflict, 404 without
grant, and every error code; archtest and contract docs regenerated.

### Task 3: Voided days (`bed_days.go`)

`voidDay` withdraws the day's standing discount lines before the full reversal. Test: discount,
then end the stay so the day is voided; the account balance for the day is zero and the census
does not fail.

### Task 4: Shell

`packages/api/src/billing.ts` types and zod schemas; `medicore/discounts.tsx`
(`DiscountReasons`, `AdmissionDiscounts`, `PendingDiscounts`); wired into the billing page and
`cash-payments.tsx`; bill line labels; account summary. Vitest for each panel.

### Task 5: Docs

`docs/api/domains/billing.md` Discounts section; "Not covered" lines that named #200 corrected;
`portfolio.md`; contract docs.

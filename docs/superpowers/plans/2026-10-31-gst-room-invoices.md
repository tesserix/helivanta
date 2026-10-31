# GST on hospital rooms and the admission tax invoice — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** tax non-ICU/NICU room days above ₹5,000 at 5% for GST-registered hospitals,
and issue an invoice-cum-bill of supply for an admission's room supplies.

**Architecture:** PR A adds the `nicu` bed class. PR B adds billing migration
`0009_billing`:

- a GSTIN registration per hospital;
- the dated policy as a versioned SQL function;
- GST lines computed by triggers and posted as `gst` account entries by the census;
- tax invoices with database-set numbering and snapshot lines.

The shell gets a GST setup panel and an invoice section on the admission's bills.

**Tech Stack:** Go (gin, gorm, Postgres with forced RLS), Next.js, zod, Vitest.

**Spec:** `docs/superpowers/specs/2026-10-31-gst-room-invoices-design.md`

## Global Constraints

- **Policy `IN-ROOM-2022-07-18`:** effective 2022-07-18; threshold 500000 paise
  (exceeding); exempt classes `icu`, `nicu`; CGST 250 bp and SGST 250 bp; SAC `9993`.
- **Tax rounding:** `(value × bp + 5000) / 10000` in integer paise, per day, per tax.
- **Invoice number:** `IP/<yy><yy+1>/<n>`, n ≤ 9,999,999, consecutive per (tenant,
  GSTIN, financial year). The financial year runs April to March by the hospital's
  date.
- **Errors:**
  - `409 gst_unregistered`, `409 no_final_bill`, `409 nothing_to_invoice`,
    `409 credit_note_required`;
  - `400` for an invalid GSTIN;
  - `409 registration_backdated`.
- **Permissions:** `billing.tax.manage`, no roles (matrix: tenant administrators only).
  Issuing needs `billing.bill.issue`, reading `billing.account.read`.

## Review Focus

- A day of exactly ₹5,000 stays exempt; ₹5,000.01 is taxed on the whole amount.
- An HDU or `neonatal` day above the threshold is taxed; an ICU or NICU day is not.
- A voided taxable day reverses its GST entry; an invoiced day reversed blocks the next
  invoice.
- A registration recorded after days were priced taxes the eligible days from its
  date, and a final bill waits until it is applied.
- Two invoices issued at once for two admissions under one GSTIN get consecutive
  numbers.

---

### Task A: NICU bed class (PR A)

- [x] Migration `0009_medicore`; contract `BedClasses`; handler message; shell type,
      label and enum; docs.
- [x] Tests: a bed of class `nicu` is created and the database check lists it; a
      billing rate for `nicu` is accepted. Mutation-checked.

### Task B1: Registration and policy

**Files:** `backend/internal/modules/billing/gst.sql`, `gst.go`, `gst_test.go`,
`module.go`, archtest matrix.

- [ ] Test: GSTIN check character (valid, wrong check, wrong pattern); set once;
      backdated refused; needs bed charging; clerk 403.
- [ ] Implement `billing_gstin_valid(text)`, `billing_gst_registrations`, and the
      routes `PUT /gst-registration` and `GET /gst-registration`.

### Task B2: GST lines and entries

- [ ] Test: the threshold boundary, the classes, rounding, a void reversing the GST
      entry, and a registration applied to already-priced days. The final bill waits for
      the registration to be applied.
- [ ] Implement `billing_gst_room_policy(date)`, `billing_gst_lines` (BEFORE trigger
      computes), `taxDays` in `reconcileBedDays`, the GST reversal in `voidDay`, the
      `applySetup` union, and the `billing_bed_days_settled` term.

### Task B3: Tax invoices

- [ ] Test:
  - issue after a final bill, with consecutive numbering and replay;
  - lines are snapshots with totals;
  - pharmacy lines are excluded;
  - `credit_note_required`, `nothing_to_invoice`, `gst_unregistered`;
  - concurrent issue under one GSTIN.
- [ ] Implement `billing_tax_invoices` and `billing_tax_invoice_lines` with their
      triggers, and the routes: `PUT` issue, a list, `GET` one, and a paged list of lines.

### Task B4: Shell

- [ ] Test: the GST setup panel (admin only), and issuing an invoice with recipient
      validation, showing CGST and SGST.
- [ ] Implement it in `packages/api/src/billing.ts`, `gst-setup.tsx` and
      `tax-invoices.tsx`, and mount it in `admission-bills.tsx` and on the billing desk.

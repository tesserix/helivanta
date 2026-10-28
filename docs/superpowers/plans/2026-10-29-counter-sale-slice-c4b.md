# Verified UPI at the counter (#1386 C-4b) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** A counter sale can be paid by a UPI payment the system saw settle.

**Spec:** `docs/superpowers/specs/2026-10-28-payments-upi-counter-design.md`
(section "Counter (C-4b)").

## Global Constraints

- A sale never posts on a payment that is not `paid`, of another outlet,
  for another amount, or already on a sale. The database enforces each of
  these, not only the handler.
- The reference-only `upi` tender stays, labelled "UPI (reference only)".
- The UI never shows paid on anything but `paid`.

## Tasks

1. **Migration `0022_pharmacy`.** Add a `upi_verified` tender kind and a
   `payment_request_id` column.
   - Enforce structurally:
     - a foreign key to the outlet's payment request, so the outlet must
       match;
     - a unique index, so one payment pays one sale;
     - a `BEFORE INSERT` trigger requiring status `paid`, the same amount
       and the sale's currency.
   - The sale handler accepts and digests the field and maps each violation
     to `409 not_sellable`.
   - **Test:** `TestAVerifiedUPISaleNeedsItsOwnPaidPayment`.
2. **Unused paid payments.** `GET …/counter-payments`, a `ListRoute`
   listing paid requests on no sale.
   - The daily report gains `upi_verified_minor`.
   - Both are covered by the same test.
3. **Counter UI.** `VerifiedUpi` shows the QR and follows the payment,
   including earlier paid money for the same amount.
   - The tender form posts `upi_verified` only with a paid payment.
   - The invoice and the daily report label it.
   - **Tests:** `verified-upi.test.tsx`, plus a counter-sale test that
     posts only after paid.

## Rulings

- **Poll interval:** the shared 3 s, not the spec's 2 s, because the hook
  has one interval. The spec is corrected.
- **CSP:** no change; `img-src` already allows `https:`.
- **The QR is a plain `<img>`, not `next/image`:** `next/image` would proxy
  the payment QR through our server.

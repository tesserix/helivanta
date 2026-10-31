# Credit notes for room invoices Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** The billing desk issues a credit note for an invoice's reversed room days, after which the admission can be invoiced again.

**Architecture:** A new migration `0010_billing` (`credit_notes.sql`) holds the tables, triggers and the replaced invoice block; `credit_notes.go` holds the handlers; the shell's `gst.tsx` gains the button and the list.

**Tech Stack:** Go (gin, gorm), Postgres (RLS, triggers), Next.js, zod, Vitest.

**Spec:** `docs/superpowers/specs/2026-10-31-gst-credit-notes-design.md`

## Global Constraints

- Number `CN/<fy>/<serial>`, at most 16 characters; one series per GSTIN and financial year of issue.
- Deadline: 30 November after the end of the invoice's financial year (`2627` → `2027-11-30`), against the hospital's today.
- Constraint names: `billing_credit_note_serial`, `billing_credit_note_deadline`, `billing_credit_note_line`, `billing_credit_note_complete`; the invoice block stays `billing_tax_invoice_credit_note`.
- Errors: `409 nothing_to_credit`, `409 credit_note_deadline_passed`, `404` for the tax invoice, `409` conflict for an id reused on another invoice.
- Use `WithTenant` in tests; never `WithAdmin`.

## Review Focus

- An id replayed on another invoice of the same admission → 409, never a second note.
- Two concurrent notes under one GSTIN → consecutive serials.
- A reversed charge credited twice → refused by the primary key, not only by the handler.
- An invoice of another hospital or tenant → 404.
- A credited reversal no longer blocks invoicing; an uncredited one still does.

---

### Task 1: Schema and handlers

**Files:** Create `backend/internal/modules/billing/credit_notes.sql`, `credit_notes.go`, `credit_notes_test.go`; modify `module.go` (migration, routes), `gst.go` (`creditable_lines`, error copy), `internal/archtest` matrix if routes are listed there.

- [ ] Write `TestACreditNoteCreditsTheReversedDaysAndUnblocksInvoicing`: invoice a private day; a late earlier stay reverses it; the invoice shows `creditable_lines: 1`; issue → 201, `CN/<fy>/1`, value 600000, CGST 15000, SGST 15000, `invoice_number` equal to the invoice's; replay → 200 and equal; same id on another invoice → 409; second issue → `409 nothing_to_credit`; the new invoice → 201 with the general day; nurse → 403; outsider GET → 404.
- [ ] Write `TestCreditNoteRulesHoldInTheDatabase`: `billing_credit_note_open('2627', d)` is true for `2027-11-30` and false for `2027-12-01`; a line outside its issue is refused; a line for a charge not reversed → `billing_credit_note_line`.
- [ ] Write `TestConcurrentCreditNotesAreNumberedInOrder` (4 admissions).
- [ ] Run them: FAIL (404 route).
- [ ] Implement the migration and handlers; run: PASS. Mutation-check each new assertion.
- [ ] `go run ./cmd/contractdocs` and `-check`; `make lint-go`; commit.

### Task 2: Shell

**Files:** `packages/api/src/billing.ts` (`CreditNote`, `CreditNoteLine`, `creditable_lines`), `apps/shell/components/medicore/gst.tsx`, `gst.test.tsx`.

- [ ] Tests: the button appears only with `creditable_lines > 0` and the permission; clicking sends `PUT …/credit-notes/<v7>` and toasts "Credit note issued"; the credit note list renders number, invoice reference and total.
- [ ] Implement; run lint, type-check, format and tests; commit.

### Task 3: Docs

- [ ] `docs/api/domains/billing.md` section; `docs/api/portfolio.md` row (credit notes no longer excluded); the slice 1 spec's mentions of credit notes as not built. Commit.

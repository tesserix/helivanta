# Pharmacy audited reads — implementation plan

**Goal:** #1555: the sale, credit-note, prescription, worklist and summary
reads write their audit row in the read's transaction.

**Spec:** `docs/superpowers/specs/2026-11-04-pharmacy-audited-reads-design.md`

## Global constraints

- No migration, no new permission; `auditRegisterRead` is the one mechanism.
- The audit insert is inside the read's transaction, after the rows are read.
- `changed_fields` holds filter names only, never values.

## Task 1: backend

- `sale.go` (`getSale`, `listSales`), `credit_notes.go` (`getCreditNote`,
  `listCreditNotes`), `prescriptions.go` (`getPrescription`,
  `listPrescriptions`, `getOpenPrescriptionLines`), `sales_worklist.go`
  (`listSalesWorklist` when a row is an admission, `getSaleSummary` when the
  sale is): call `auditRegisterRead` per the spec table.
- Tests, written first (`audited_reads_test.go`): for each route, one
  successful read writes exactly one row with the spec's resource type,
  resource id, action and filter names; a refused read (bad id, another
  tenant, missing permission) writes none; an audit insert that fails (a
  test seam forcing the error) returns an error and no body. Worklist and
  summary: no row for a walk-in-only page or a walk-in sale, one when an
  admission sale is shown.
- Mutation-check each call.

## Task 2: docs

Domain doc: an "Audited reads" section with the table, and the existing
sections that call these reads unaudited corrected.

## Review focus

- A read whose handler writes the response before the transaction commits.
- A list page of zero rows (still audited for sales, credit notes and
  prescriptions: the read happened).
- The summary's snapshot transaction is read-only: the audit insert must not
  run in it.

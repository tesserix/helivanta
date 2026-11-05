# Sales by item by lot and by prescriber Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Filter sales by item by lot number and by prescriber, with a bounded prescriber list.

**Architecture:** `salesReportFilter` gains a lot and a prescriber. The two sale-line subqueries of `itemSalesRows` gain predicates for them. The prescriber predicate joins the cited prescription, and for credit lines it goes through the sale line. The prescriber filter adds a second permission check and an audit row inside the read's transaction. A new bounded GET lists the period's prescribers. The shell adds a lot field and a prescriber picker to `ItemSales`.

**Tech Stack:** Go, Gin, GORM, PostgreSQL 16; Next.js, TanStack Query, Vitest.

**Spec:** `docs/superpowers/specs/2026-11-04-pharmacy-lot-prescriber-report-filters-design.md`

## Global Constraints

- No migration.
- `lot_number`: trimmed, 1–100 characters, matched case-insensitively.
- `prescriber_registration` and `prescriber_council` together, or neither (400).
- The prescriber filter and the list need `pharmacy.prescription.read` (403), and are audited in the read's transaction.
- The list returns at most 200 rows, ordered by name. It is allowlisted as a bounded GET in `arch_test.go`.

## Review Focus

- A return of a prescription sale: it counts under the prescriber (Task 1).
- An OTC line of the same product: it never matches a prescriber (Task 1).
- A failed audit write returns no data (Task 1, via `SetReadRecordFault`).
- A lot in another case: it matches (Task 1).
- A sale reader without prescription read: no picker and no list request (Task 2).

---

### Task 1: API

**Files:**
- Modify: `backend/internal/modules/pharmacy/sales_reports.go`, `backend/internal/archtest/arch_test.go`.
- Test: `backend/internal/modules/pharmacy/lot_prescriber_reports_test.go`.

- [ ] Tests:
  - `TestSalesByItemFiltersByLot`;
  - `TestSalesByItemFiltersByPrescriber` (match, return, OTC, 403, 400s, audit row and fields, fault returns 500 and no data);
  - `TestThePrescribersOfAPeriod` (distinct by registration and council, latest name, bounded, 403, audited).
- [ ] Run them and see them fail.
- [ ] Implement; run the targeted tests and then the package. Mutation-check the credit-line prescriber join and the permission check.
- [ ] Commit.

### Task 2: Screens

**Files:**
- Modify: `apps/shell/components/pharmacy/sales-reports.tsx`, `packages/api/src/pharmacy-sales-reports.ts`.
- Test: `apps/shell/components/pharmacy/sales-reports.test.tsx`.

- [ ] Tests:
  - an applied lot is sent trimmed;
  - choosing a prescriber sends both fields;
  - without prescription read there is no picker and no list request;
  - "more than 200" is said.
- [ ] Implement, then run the shell and api gates.
- [ ] Commit.

### Task 3: Docs and generated contracts

- [ ] `docs/api/domains/pharmacy.md`: the parameters, the new route, permissions and audit. Remove "clinician filters" from the not-covered list.
- [ ] `contractdocs` and the staff-access inventory: regenerate and `-check`.
- [ ] Commit.

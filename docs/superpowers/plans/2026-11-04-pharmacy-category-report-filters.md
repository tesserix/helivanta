# Pharmacy reports by item category Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Filter sales by item and sales and stock by item category and subcategory.

**Architecture:** One SQL predicate, built in Go per (product, day) column pair, calls slice 1's `pharmacy_product_category_on`; `salesReportArgs` parses the two ids beside the existing filter. The shell adds a category picker to each of the two report sections, fed by slice 1's category list.

**Tech Stack:** Go, Gin, GORM, PostgreSQL 16; Next.js, TanStack Query, Vitest.

**Spec:** `docs/superpowers/specs/2026-11-04-pharmacy-category-report-filters-design.md`

## Global Constraints

- No migration: slice 1's function is enough.
- `category_id`, `subcategory_id` query parameters; `subcategory_id` alone is 400; malformed is 400.
- Sale lines by sale date; credit lines by their sale line's sale date; stock-sales rows by today's category.
- Dropdowns are `SelectControl`; tests use `renderWithProviders` and `chooseOption`.

## Review Focus

- A product re-categorised mid-period: split between categories by sale date (test in Task 1).
- A return dated after the product moved: counts under the sale's category (Task 1).
- A subcategory of another category with `category_id`: matches nothing (Task 1).
- A category of another outlet: empty page, not 404 or 403 (Task 1).
- Changing the category clears the subcategory in the picker (Task 2).

---

### Task 1: API filter

**Files:** Modify `backend/internal/modules/pharmacy/sales_reports.go`; Test `backend/internal/modules/pharmacy/category_reports_test.go`.

- [ ] Write `TestSalesByItemFiltersByTheCategoryOnTheSaleDate`: on 31 October two products are approved into Antibiotics›Oral and Analgesics from 1 November; one is moved to Analgesics from 3 November; sales on 1 and 3 November and a return on 4 November of the 1 November sale. Assert per-category rows and totals, subcategory narrowing, the return counted under Antibiotics, 400 for `subcategory_id` alone and malformed ids, and an empty page for another outlet's category.
- [ ] Write `TestTheSalesStockReportFiltersByTodaysCategory`: rows listed by today's category, units counted line by line.
- [ ] Run: `go test -race ./internal/modules/pharmacy -run 'Category.*Report|FiltersBy'`. Expected: FAIL (filters ignored).
- [ ] Add `category, subcategory *uuid.UUID` to `salesReportFilter`, parse in `salesReportArgs` for the two reports, and splice `categoryOn(product, day string) string` into `itemSalesRows` and `stockSalesRows`.
- [ ] Run the targeted tests, then the package. Expected: PASS. Mutation-check: drop the credit-line sale-date join and see the return test fail.
- [ ] Commit.

### Task 2: Screens

**Files:** Modify `apps/shell/components/pharmacy/sales-reports.tsx`, `apps/shell/components/pharmacy/item-categories.tsx` (export `useItemCategories`), `packages/api/src/pharmacy-sales-reports.ts`; Test `apps/shell/components/pharmacy/sales-reports.test.tsx`.

- [ ] Test: choosing a category and then a subcategory requests `category_id` and `subcategory_id`; choosing another category drops the subcategory; retired categories are offered; without catalogue read there is no picker.
- [ ] Path builders take an optional `{ categoryId, subcategoryId }`; a `CategoryFilter` component renders the two pickers.
- [ ] Run `pnpm turbo lint type-check test format:check --filter=@helivanta/shell --filter=@helivanta/api`. Expected: PASS.
- [ ] Commit.

### Task 3: Contract docs

- [ ] `docs/api/domains/pharmacy.md`: the two parameters, their 400s, and decisions 2–4.
- [ ] `go run ./cmd/contractdocs` and `-check`; the staff-access inventory and `--check`.
- [ ] Commit.

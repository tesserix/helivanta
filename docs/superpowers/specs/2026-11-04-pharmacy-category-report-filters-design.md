# Pharmacy reports by item category (#988, slice 2)

Issue: #988 (slice 2; the issue stays open for patient categories, margin
discount rules, templates and indent groupings). Builds on slice 1,
[item categories](2026-11-04-pharmacy-item-categories-design.md), and the
#1060 reports in `sales_reports.go`.

## Why

Slice 1 gave each product a reviewed, dated category, but no report can be
cut by one. The legacy sales-by-item and sales-and-stock reports filter by
item category and subcategory; ours list every product.

## Decisions (agreed 2026-11-04)

1. **Two reports take the filter:** sales by item (`GET …/reports/items`)
   and sales and stock (`GET …/reports/stock-sales`). Optional query
   parameters `category_id` and `subcategory_id`; absent means all, as
   today. The receipts register has no items and patient-wise sales is one
   patient's lines, so neither takes it.
2. **A sale line counts under the category in force on its sale date**
   (`pharmacy_product_category_on(tenant, outlet, product, sale_date)`),
   which slice 1 makes final: no approval takes effect on or before the day
   it is made. A product moved from one category to another in the period
   is split between them, each sale where it was made.
3. **A credit-note line counts under the category of the sale line it
   reverses** (the original sale's date), not the note's date, so a return
   always cancels the sale it returns and a category's net is never
   negative because the product moved in between. The note is still
   *counted* in the period by its own date, as today.
4. **Sales and stock's stock is now, so its category is today's**: a row is
   listed when the product's category in force at the outlet today matches;
   its sold and returned units are counted line by line as in 2 and 3.
   Sold and returned units therefore equal sales by item's for the same
   filter whenever no listed product changed category in the period.
5. **A subcategory narrows its category.** `subcategory_id` without
   `category_id` is refused (400): fail closed rather than guess the
   parent. A malformed id is 400. An id that is not a category of this
   outlet matches nothing and returns an empty page, as an unknown product
   does today: the list reveals nothing about other outlets.
6. **Retired categories stay selectable** in the filter: past sales were
   made under them.
7. **No audit.** These reports hold no patient, prescriber or buyer and are
   not audited today (`sales_reports.go`); a category filter adds nothing
   personal.
8. **Screens:** each of the two reports gets its own category picker
   (all, or one category) and, once a category is chosen, a subcategory
   picker (all, or one of its subcategories). Shown only to a holder of
   `pharmacy.catalogue.read`, which every reader of these reports holds
   today; the API enforces nothing new.

## Not covered

- Products with no category in force ("uncategorised") as a filter value.
- Category totals or grouping rows: the reports stay one row per product.
- Category filters on other reports (daily report, stock overview,
  dispensing registers).
- Margin-discount rules: they need finance's precedence decision (#988).

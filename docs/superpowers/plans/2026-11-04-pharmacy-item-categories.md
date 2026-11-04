# Pharmacy item categories — implementation plan

**Goal:** #988 slice 1, first PR: an outlet's item categories and
subcategories, and each product's reviewed, dated category.

**Spec:** `docs/superpowers/specs/2026-11-04-pharmacy-item-categories-design.md`

## Global constraints

- New migration `0033_pharmacy` (after #1560's `0032`); forced RLS; the
  writing outlet grant is `editor`.
- Declarations and reviews are insert-only; four eyes database-enforced.
- New permissions `pharmacy.category.declare` and `pharmacy.category.review`,
  pharmacist and pharmacy owner.

## Task 1: categories (`item_categories.go`, `.sql`)

- `pharmacy_item_categories (tenant, outlet, id, parent_id, code, name,
  retired_at, created_by, created_at)`: two levels (a parent has no parent);
  code unique among siblings; code and parent fixed; retired once.
- `PUT …/catalogue/categories/{id}` (create, or rename with the same code
  and parent), `POST …/{id}/retire`, `GET …/catalogue/categories` (paged).
- Tests first: create, subcategory, rename, replay, a code clash, a
  third level refused, code/parent change refused, retire, retired parent
  refuses a new subcategory, permission and outlet scope.

## Task 2: assignment (`item_categories.go`, `.sql`)

- `pharmacy_product_category_declarations` and `_reviews`, as schedule
  classes; a guard requires a top-level live category, a subcategory of it,
  and `effective_from` not before the outlet's today.
- `pharmacy_product_category_on(tenant, outlet, product, day)`: the
  approved declaration in force, effective from the later of
  `effective_from` and its approval's outlet date.
- `PUT …/catalogue/products/{id}/category/{declaration}`,
  `POST …/review`, `GET …/catalogue/products/{id}/category` (history and
  the category in force today).
- Tests first: declare and approve, four eyes, reject, a retired category
  refused, a foreign subcategory refused, a past date refused, approval
  later than the date takes effect on approval, a newer approval supersedes.

## Task 3: shell

- Setup: categories and subcategories list with create, rename and retire.
- Product detail: category in force, history, declare and review.
- Tests with `renderWithProviders`.

## Task 4: docs

Domain doc section; permissions and generated references.

## Review focus

- Two declarations effective the same day: the later approved wins.
- A category retired after a product was assigned to it: the product keeps
  it; it cannot be newly assigned.

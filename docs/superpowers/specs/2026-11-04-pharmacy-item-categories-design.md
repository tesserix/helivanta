# Pharmacy item categories (#988, slice 1)

Issue: #988 (slice 1; the issue stays open for patient categories, margin
discount rules, templates and indent groupings). Feeds #1060's category
filter and, later, #988's margin rules keyed on item category/subcategory.

## Why

The legacy setup groups items into a category and a subcategory, and both
its reports and its margin-discount rules key on them. Helivanta's catalogue
has no category at all, so no report can be cut by one and no pricing rule
could ever target one. Margin rules come later and need finance's precedence
decision; categories are their foundation and are useful on their own.

## Decisions (agreed 2026-11-04)

1. **Two levels, outlet-owned.** An outlet's categories (code, name) and
   subcategories under one category, matching the legacy item
   category/subcategory. Outlet-owned because the catalogue is: a product
   belongs to one outlet.
2. **A product's category is a reviewed, dated declaration**, like its
   schedule class: a catalogue editor declares `{category, subcategory?,
effective_from, reason}`; a pharmacist (a second person) approves or
   rejects. Insert-only, with actor, reason and effective date (#988:
   "approved changes retain actor, reason and effective-date history").
   Review, because margin rules will price by category: re-categorising an
   item will change what it may be discounted.
3. **Historically stable.** A category takes effect from tomorrow at the
   earliest (`effective_from` after the outlet's today), and an approval from
   the later of `effective_from` and the outlet's day after approval. Today's
   sales are already made, so no approval ever re-files a sale: what a sale
   counted as is the category in force on its sale date, and that never
   changes afterwards. The outlet's today is read in Go, where the module
   clock lives; the database enforces that an approval is never in force
   before its declaration's date.
4. **Categories themselves** are created and renamed by the catalogue editor
   without review (a name is a label, not a price); a category in use can be
   retired, not deleted, and a retired one cannot be newly assigned. Retirement
   is enforced at declaration and again at approval, with the category row
   locked, so a product never lands in a category retired under it.
5. **Permissions.** `pharmacy.category.declare` and `pharmacy.category.review`,
   held by the pharmacist and the pharmacy owner, exactly as schedule classes
   are; the reviewer can never be the declarer (database-enforced). Editing an
   outlet's data also needs its `editor` grant.
   Only a product's newest declaration can be approved; an older one left
   pending can only be rejected, so it never comes back over a newer decision.
6. **Reports come next.** Sales by item and sales-and-stock gaining category
   filters, read by the category in force on each sale's date, is the next
   PR of this slice; this one makes categories and assignments exist.

## Not covered

- Renaming a category on screen: the API renames (the same id, a new name);
  the screen adds and retires.

- Category filters on reports (the next PR, decision 6).

- Margin discount rules and their precedence (needs the pharmacy/finance
  decision the issue records), patient categories, pharmacy templates,
  indent groupings: later #988 slices.
- Categories shared across a pharmacy's outlets.
- Bulk import of categories or assignments.

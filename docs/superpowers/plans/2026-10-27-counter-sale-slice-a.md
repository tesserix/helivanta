# Counter sale slice A implementation plan: sellable stock

**Goal:** stock can become sellable only through checked steps:

- a reviewed, valid outlet drug licence;
- a reviewed schedule class per product;
- an MRP recorded per batch;
- a pharmacist's release from quarantine.

**Spec:** `docs/superpowers/specs/2026-10-27-counter-sale-design.md` (#1386), decisions 1 and 2.

**Architecture:** four new migrations, `0012_pharmacy` to `0015_pharmacy`. All tables are
outlet-scoped through `pharmacy_outlet_allowed`, insert-only and audited. Reviews are
separate rows, and the trigger refuses a reviewer who is the author. Each part is its own PR:

| PR  | Part                    | Issue       |
| --- | ----------------------- | ----------- |
| A1  | Licence                 | #1386, #171 |
| A2  | Declared class          | #171        |
| A3  | Batch price and release | #162, #167  |

## Global constraints

- **Routes:** under `/pharmacy/pharmacies/:pharmacy/outlets/:outlet/`. Writes need the
  outlet `editor` grant; reads need any grant. Otherwise 404.
- **Dates:**
  - "Today" is the outlet's own date, in its `timezone`.
  - A licence is in force when it is approved and `valid_from <= today <= valid_until`.
- **Four-eyes:** the reviewer's subject must differ from the author's. A CHECK enforces it
  in the database, so it holds even for direct app-role writes.
- **Writes:** idempotent, using a client UUIDv7 and the same-intent replay used across the
  catalogue.
- **Permissions:**

  | Permission                  | Holders           |
  | --------------------------- | ----------------- |
  | `pharmacy.licence.manage`   | owner             |
  | `pharmacy.licence.review`   | owner             |
  | `pharmacy.schedule.declare` | pharmacist, owner |
  | `pharmacy.schedule.review`  | pharmacist, owner |
  | `pharmacy.stock.release`    | pharmacist        |

## Review focus

1. A licence entered and reviewed by the same subject never comes into force.
   - Test: same-subject review refused (403/409); licence not in force.
2. A licence's validity is judged on the outlet's date, not UTC.
   - Test: an outlet in `Asia/Kolkata`, with `valid_until` today in IST while UTC is
     already tomorrow (or the reverse).
3. A newer pending class declaration blocks release, even when an older approved one
   exists.
   - Test: approve H, declare H1 pending, release refused.
4. Release refuses each unsafe batch, and each refusal names its reason:
   - expired, without MRP, untaxed product, inactive product, or a batch under an open
     safety incident.
   - Test: one case per reason.
5. Release cannot overdraw quarantine or create stock from nothing.
   - Test: release more than on hand → 409. The quarantine and sellable balances sum to the
     same total.

## A1 Outlet drug licence

- **Tables:**
  - `pharmacy_outlet_licences`: `id`, outlet, `licence_kind`, `form_label`, number,
    issuing authority, `valid_from`, `valid_until`, pharmacist name, pharmacist
    registration number, `entered_by`, `intent_sha256`.
  - `pharmacy_outlet_licence_reviews`: `licence_id`, `decision approved|rejected`, reason,
    `reviewed_by <> entered_by`.

  `licence_kind` is `retail | wholesale`. `form_label` is free text: the form the licence
  was issued on, as printed. No list of forms is encoded.

- **Routes:**
  - `PUT /licences/:id`
  - `POST /licences/:id/review`
  - `GET /licences` (paged)
  - `GET /licence-status`: `{in_force, licence?, reason}`.
- **SQL:** `pharmacy_outlet_licence_in_force(tenant, outlet, date) boolean`, for later
  slices to call inside their transactions.
- **Not covered:** the evidence file (follow-up issue; the reviewer checks the physical
  licence), and renewal notices.

## A2 Declared schedule class

- **Tables:**
  - `pharmacy_product_schedule_declarations`: product, `class none|G|H|H1|X`, `ndps`,
    `source`, `declared_by`.
  - `..._reviews`: approve or reject, with `reviewed_by <> declared_by`.
- **In force:** the latest declaration. It counts only if approved; if pending or rejected,
  the product has no class and cannot be released or sold.
- **Routes:** `PUT /catalogue/products/:product/schedule/:id`, `POST …/schedule/:id/review`,
  `GET …/schedule` (current state and history).

## A3 Batch price and release

- **Batch price:** add `mrp_minor` and `purchase_cost_minor` to batches (nullable for
  existing rows, required for new batches). A batch without MRP can never be released.
- **Locations:** `stock_status` gains `sellable`. A location is created with its status,
  and the status never changes.
- **Release:** new movement kind `release`. `PUT /stock/releases/:id`, with
  `{batch_id, from_location, to_location, quantity_units, reason}`, writes two movements in
  one transaction: minus from quarantine, plus to sellable, sharing `release_id`. It checks
  review focus 4 and the outlet's date. The existing bucket lock prevents overdraw.
- **Batch safety:** an open safety incident on a batch blocks its release. The sale slice
  will also stop the sale of already-released units.

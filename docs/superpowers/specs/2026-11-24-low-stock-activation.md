# Low-stock release activation — #1685

Status: development design; this document activates no feature.

## Verified implementation and remaining work

PR #1761 already owns append-only per-outlet/product minimum revisions, exact
actor/input-bound recovery, released/unexpired availability, zero-stock products,
and the existing stock-list/overview parity tests. Its deployment does not expose
those controls: general Stock, Catalogue and Reports remain release locked.

Deliver the issue through a separate `pharmacy_low_stock` capability depending
on `pharmacy_setup`. Do not activate unrelated product, stock-movement, dispensing,
financial, supplier or ordering functionality. Keep eligibility false until the
focused owner, gateway, built-browser and capacity gates pass.

## Owner and authorization

PharmaConnect remains the single owner of product identity, minimum revisions and
available stock. Browser traffic uses Global API. Reuse the accepted #1685
inventory-administration contract: current tenant membership and stock-read
permission plus an explicit exact-pharmacy/outlet reader grant. Configuration also
requires catalogue-write permission and the current outlet editor grant. Ownership,
affiliation and user-supplied scope do not grant access. These metadata commands
create no physical movement, dispensing authority or clinical role; they do not
reuse stocktake duties as minimum-level administration.

Both hospital-owned and independent outlets use their actual owner's scope. The
new read projections expose only product identity/version/unit needed for this
workflow, not another catalogue master. Missing permission is 403, invisible
scope/object 404, changed session 409 and unavailable authority/audit 503.
Reauthorize before disclosure, writes and recovery. Audit successful disclosures
inside the same guarded transaction; audit failure releases no results.

## Contract and UI

Keep existing reorder-level commands and low-product list wire formats. Bind
those exact routes to the narrow capability. Add bounded active-product choices,
an exact current product projection for reload, and a low-stock overview count
under the same outlet scope. Canonical Go/Zod models come from pharmacy.json.
The overview count reuses lowStockSource and its observation time, local date,
unit and reservation rules. It is not a total inferred from a partial page.

Expose a named Low stock workspace using the existing pharmacy/outlet selection
and reader/editor access guard. The existing current-user `/access` read is
foundational Setup metadata, not a Catalogue feature; bind it to `pharmacy_setup`
while retaining catalogue-read permission and the exact current outlet grant.
Workspace discovery therefore needs catalogue-read in addition to stock-read;
the minimum APIs retain their narrower existing permissions. Show its count in a bounded outlet overview and
link to the matching warning list. Keep Setup dedicated to outlet/access
management under its existing page contract; the Low stock workspace contains
the released outlet overview. The general Reports overview retains its existing
gates and low-stock parity. Broad Stock additionally depends on Low stock so
its legacy count/list cannot bypass withdrawal of the narrow capability.
No raw identifiers appear as product, account or outlet labels. Scope changes and
failed reads hide stale private data; incomplete pages retain continuation.

Retain explicit zero versus disabled/unconfigured, active-product and unit/version
checks, immutable receipts, original-command retry, stale-edit conflict and
uncertain-save recovery. No automatic ordering or stock fixture fabrication.

## Bounds and failures

Reuse the original #1685 envelope: 1,000/3,000 peak application RPS at months
12/36, 80:20 reads/writes, typical 2–20 KiB responses, 10,000 configured products
per acceptance outlet, read p99 at most 500 ms. Pages default to 50 and cap at 200;
summary responses contain one count and observation metadata. Preserve the
existing eight-second SQL / ten-second request ceilings. Measure list and summary
on hosted Postgres before activation; optimize only after an observed failure,
without raising the acceptance budget.

Reads use current owner state with explicit observation time; separate requests
can legitimately differ after changes. Authorization, database and audit outages
fail closed. Do not show unavailable as zero or an empty successful warning list.
No new service, cache, datastore or worker is introduced. Existing storage/history
and guarded transactions remain the durability boundary.

## Acceptance and delivery

Prove equality/zero/above level, missing batches, quarantine, expiry/local date,
safety holds, reservations, disable, unit/version changes, bounded complete pages,
count parity, denied scope/reader writes, audit rollback and exact replay. Exercise
the built UI through Global API with only Setup and Low stock available; general
Catalogue/Stock/Reports and their commands must remain denied. Include uncertain
save and refreshed-read denial. Retain the original broad-page regression.

Run focused local pure tests and affected builds without starting local apps,
databases, containers or Colima; remove generated build outputs. Run actual owner,
DB, gateway/browser and capacity checks in hosted CI. Keep work committed/pushed,
merge only after protected checks, release exact signed images through the normal
production workflow and Kargo, then align the owning chart catalogue and enable
only this capability through audited Console controls. Verify a later scheduled
sync preserves its desired value.

Live acceptance uses explicitly authorized synthetic existing products/stock and
actual outlet grants. Do not turn on broad capabilities to prepare fixtures or
bypass identity/Staff review. Record cases not actually run as Pending. Disable
new low-stock work through Console if acceptance fails; preserve configuration
history and original receipts. No destructive migration or ownership change is
planned. Existing public contracts remain compatible; only additive narrow reads
and release classification change.

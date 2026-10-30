# Released prescription evidence before a new sale

The accepted counter-sale design B5 and #1386 require a scanned prescription
image for G/H/H1 sales. The current handler and database guard only check recorded
prescription lines. Restore that existing product requirement; this is not a new
interpretation of medicine law or activation of a prescribing policy.

Pharmacy owns prescriptions, retained image lifecycle records, sales and stock.
Use the existing verified tenant and outlet scope and existing sale permission.
The image must belong to the exact `(tenant, outlet, prescription)` and have
terminal `available` status. Pending, failed, rejected, absent and other-scope
images grant nothing. The approved lifecycle already prevents deleting images
or transitioning available images away, so the sale's prescription citation
retains its clean evidence without a new file master or copied file metadata.

The handler returns 409 `not_sellable` with an actionable reason for quotes and
posting. A new append-only migration adds an insert guard on sale lines, so
direct writes cannot bypass the requirement. Existing invoices are retained and
can replay unchanged; no historical sale is rewritten or invalidated. OTC remains
independent of prescription evidence; X/NDPS remain refused.

Tests exercise absent/pending/rejected/error evidence, clean release through the
actual owner processor identity, exact scope, direct SQL guard and replay. Existing
prescription-sale fixtures must upload and scan their synthetic images through
the owning flow instead of fabricating available rows. Relevant acceptance:
IN-PH-03/04/06, owner schema and integration boundaries.

Release dependency: #1406 worker rollout and synthetic acceptance must precede
enforcement deployment, otherwise new prescription sales correctly fail closed
but would be operationally unavailable. Neither this feature nor clean file
scanning approves a drug, prescriber or pharmacy licence.

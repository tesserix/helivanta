# Supplier grant consent and effective access

Unreleased candidate. Shared IAM owns supplier grants and their hospital consent
projections. Source products keep stock, shipments, invoices and field-release
policy. Workforce keeps Staff, employment, accounts and postings. Hospital
publication or membership alone never grants supplier access. The existing
[planning drafts](partner-grants.md) remain non-authorizing.

Canonical records are in `contracts/supplier-access.json`; run
`node scripts/generate-supplier-access.mjs` and `--check`. Staff capability
catalogs remain canonical in `contracts/staff-access.json` and its immutable
migration history. This implementation adds IAM migrations 0039–0040.

## Ownership and consent

The source prepares a grant against an exact current bilateral relationship and
its terms digest. Scope comes from that relationship, including exact source
product, organization and location, hospital and distinct recipient tenant.
A grant targets either one named account or one hospital-owned group and one
closed template. Validity cannot exceed the commercial terms or 366 days.

An independent current source administrator offers the grant. A current hospital
Staff reviewer accepts the same immutable input after checking the recipient.
Named recipients require exact reviewed Staff/posting references and current
account/generation eligibility; no Staff directory is copied to the supplier.
Groups require the exact published version, membership policy, purpose and
validity. A reviewer cannot approve access for their own account or Staff identity,
or approve a group they are already nominated to receive through.

The directed transactional protocol records source offer, hospital acceptance and
source acknowledgement. Acknowledgement retains the source decision epoch.
`approved` records consent; it is not proof that a particular user currently has
access. Either party may suspend. Late or repeated exchanges cannot reactivate a
terminal grant. Source and hospital retain their own projections and append-only
history with original source provenance. Private recipient basis remains solely
in the hospital's IAM data.

## Browser/API contracts

All paths below are relative to `/v1/iam` and travel through Global API.

| Method    | Path                                                                     | Authority                                                                                                                                            |
| --------- | ------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| PUT / GET | `/supplier-grants/{id}`                                                  | Current source IAM capability and exact private source administration; prepare additionally rechecks current bilateral relationship and source facts |
| PUT / GET | `/supplier-grants/{id}/commands/{command}`                               | Independent source review or current source read; actor-bound immutable decision recovery                                                            |
| GET       | `/supplier-sources/{product}/{organization}/locations/{location}/grants` | Exact private source read; signed bounded keyset page                                                                                                |
| GET       | `/hospitals/{hospital}/supplier-grant-offers`                            | Current hospital-scoped reviewer and Staff identity; bounded keyset page                                                                             |
| GET       | `/hospitals/{hospital}/supplier-grant-offers/{id}`                       | Same exact hospital scope and current reviewer                                                                                                       |
| PUT / GET | `/hospitals/{hospital}/supplier-grant-offers/{id}/commands/{command}`    | Current hospital reviewer; acceptance checks current recipient source and relationship, reductions do not require trading eligibility                |

Writes require client UUIDv7 identities, exact expected version, a bounded reason,
and matching expected tenant/subject headers. Bodies reject unknown or duplicate
JSON keys and exceed neither 16 KiB nor the closed canonical fields. Changed
command content or another actor cannot reuse a command identity. Recovery
reauthorizes the current caller and returns the retained decision, including its
reason and resulting version. Sensitive reads are audited; unavailable authority
returns an explicit error and never a fabricated empty directory or approval.

## Effective disclosure boundary

The closed templates are catalogue view, shipment read and invoice view. Each
supplies one record-scoped permission. Hospital viewer capabilities only allow
requesting a current check; they do not grant access to any supplier by themselves.

The consuming authorizer must check current hospital Staff/account/posting,
current group membership where applicable, exact bilateral grant and commercial
relationship, current source eligibility, and an acknowledged content-bound
OpenFGA projection. Source tenant membership and tenant-wide permissions must not
be created for a hospital user. Projections include recipient eligibility and
membership epochs so an old tuple is insufficient after source changes.

Stock mutations, quality release, prescribing, patient records, supplier Staff
administration and financial decisions are outside these disclosure templates.
After a grant or relationship ends, retained hospital obligations remain governed
by their own financial/receipt owner permissions; historical consent is not a
replacement user grant.

The current consent/store/API implementation still needs its consuming authorizer,
product projections, SDK/UI and complete shared-environment acceptance before any
claim of the full supplier workflow. The owner protocol tests are component
acceptance, not substitutes for those gates.

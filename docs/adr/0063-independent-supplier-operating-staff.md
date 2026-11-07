# ADR-0063: Independent supplier operating staff

Date: 2026-11-07. Status: implementation design for the requested supplier workflow; unreleased. Extends [ADR-0054](0054-typed-employing-establishments.md) and the [collaboration delivery record](../design/staff-and-rbac/independent-collaboration-delivery.md).

## Decision

Workforce remains the single Staff owner. A distributor/wholesaler represented by an owned SupplyConnect organization receives a `supplier` establishment and a supplier route family. Its establishment ID is the SupplyConnect organization ID. Hospital, pharmacy and supplier kinds remain immutable and distinct at route, transaction and database boundaries. Existing hospital and pharmacy wire contracts remain unchanged; supplier wire models carry `supplier_id`.

SupplyConnect publishes the structural owned-employer fact through an owner guard. Workforce does not query supply tables. A counterparty, foreign organization or missing source cannot register an employer. Structural registration configures HR ownership; regulated operations retain their separate current identity, licence, qualification, jurisdiction, category and activity checks.

Basic directory status grants no operating rights. Nonclinical commercial duties require independently reviewed Shared IAM assignments, bound to the current reviewed account/Staff link, exact product owner, source location and validity. Source Staff identity checks provide attribution and cutoff; they do not assert a hospital employment generation or clinical posting. The commercial duty assignment is explicit authorization and may represent a business owner or an authorized contractor as well as an employee. Ending the Staff/account link or duty assignment cuts off operations. Current HR/clinical employment and professional authority remain separately required wherever the consuming action requires them.

New source-operating templates must contain only their declared nonclinical permissions. A supplier or standalone pharmacy root never satisfies a hospital clinical template. Source warehouse/outlet ownership is checked by its product owner. A role name, Staff job category, catalogue item, hospital affiliation or basic directory record cannot confer professional authority.

Hospital recipients continue through hospital-owned current Staff/posting and partner-group checks. Partner grants authorize only the relationship/action/object/field scope explicitly accepted by both parties; they never turn a hospital recipient into a supplier stock administrator.

## Consistency and failure

Use existing Postgres, Shared IAM, FGA and owner callback boundaries; introduce no new service or store. Preserve hospital graph content hashes and all existing hospital payloads. Kind/scope mismatches answer 404; missing or unavailable authority denies access. Reviewed Staff identity and assignment revisions are held through consequential owner commits, with attributable audit and durable command recovery. Known aliases of one Staff record cannot satisfy independent approval. No client-provided Staff/source ID establishes authority.

Each implementation increment must include canonical schemas, owner/Global API/SDK/UI paths and negative tests before release. Actual Maharashtra/Odisha participants, qualified licence/policy reviewers and source registration are required before live activation. This decision makes no legal, tax or clinical certification claim.

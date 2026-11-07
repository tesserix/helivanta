# ADR 0064: Independent operating assignments in Shared IAM

Status: unreleased implementation candidate. This does not authorize trading or describe deployed APIs. Related: [independent Staff](0063-independent-supplier-operating-staff.md), [reviewed country policy](0030-reviewed-country-trading-policy.md), [identity boundary](../api/workforce-independent-identity.md).

A supplier/pharmacy directory manager and a reviewed login link establish an accountable nonclinical identity. Neither is operating authority. Existing hospital employment/posting assignments cannot represent that authority by putting a supplier ID in `hospital_id`.

Shared IAM owns a separate typed operating-assignment family in its existing service and database ownership boundary. Workforce continues to own Staff and reviewed links. PharmaConnect and SupplyConnect continue to own the two stock models. No new deployed service or product-private role vocabulary is introduced. The new family does not change hospital v1 payloads, hospital graph hashes, clinical posting permissions or source data ownership.

An operating assignment binds one verified tenant, independent employer kind/ID, exact outlet/premise, named account, independently reviewed Staff/link revisions, canonical duty template, effective interval and source revisions. Group-based hospital collaboration remains a separate recipient grant with current hospital-owned membership; a source operating assignment cannot confer hospital access.

Initial templates cover quarantined receiving and stock-history reads using the existing owner permissions. They confer no quality release, dispensing, dispatch, prescription, patient, financial approval or professional authority. Further templates are added with their actual owner operations and acceptance tests. Any professional activity must additionally verify current professional/operating qualifications and applicable reviewed policy at the commitment boundary.

## Preparation, review and projection

The canonical candidate is `contracts/operating-assignments.json`. Common identity field definitions derive from the existing Workforce canonical inputs. Generated DTOs are not evidence of a registered or implemented endpoint.

Preparation requires explicit current Shared IAM preparation permission and a current employer manager grant. Supplier owner/admin and pharmacy owner are the initial preparer/revoker roles. Compliance can review; auditors can read with employer reader access. TenantAdmin remains implicit, and also needs the employer access check. Structural employer registration remains a TenantAdmin bootstrap action. These administrative roles do not confer operating or professional authority. The source owner verifies the exact independent stock location in a guarded tenant command. Workforce binds the target account to its current reviewed Staff/link revision. IAM retains current target membership and credential-revocation provenance. A supplied source ID, directory manager row, tenant affiliation or legacy source role cannot stand in for these checks.

A different authorized reviewer must approve the exact immutable input and current source basis. Workforce checks known historically approved aliases of both the target Staff and the preparer; changing an account does not establish independence. This protects known associations within the employer, rather than claiming automatic global person deduplication. Identity and qualified review remain accountable human processes.

Approval creates an immutable projection request in the owner's audit/outbox transaction. It is not effective before acknowledgement of its exact current graph revision. Graph publication occurs outside database locks. Partial tuple publication is retried against the same immutable content key; it does not make an unacknowledged assignment effective. A delayed acknowledgement cannot reactivate a revoked, superseded or otherwise ineligible revision.

## Current authorization and immediate cutoff

The existing OpenFGA `assignment`/`scoped_permission` relations can represent the immutable projection. A separately prefixed, content-addressed operating key binds employer kind, location, Staff/link basis, source revisions, uninterrupted membership epoch, per-assignment authority epoch, validity and permissions. It does not add scoped permissions to tenant-wide resolution. Hospital keys remain byte-identical.

The business grant survives an ordinary sign-out; signing in again does not create a new Staff or assignment revision. Credential provenance belongs to the current command proof, rather than the published business grant.

A successful graph check alone never authorizes a product action. The IAM operating authorizer must recheck the current acknowledged assignment, validity and membership/credential state, then hold the relevant owner state through the bounded product command. Workforce rechecks active Staff and the exact reviewed basis. The stock owner rechecks source ownership and location revision/status. Changes discovered after the graph check drop that proof; they do not refresh it into a different grant.

The existing credential revocation owner serializes writes on `iam.credential:{subject}`; a commitment takes its shared counterpart and compares the command's verified credential time and captured watermark. Zero means the owner confirmed no revocation, rather than an invented epoch or unavailable lookup. Unknown/unavailable identity, graph, membership, source or policy denies the action.

Staff edits, inactivation, relinking, membership discontinuity, source changes, expiry and explicit revocation invalidate the old basis. Reactivating Staff or relinking an account cannot reconstruct authority bound to the old Staff/link revision. A fresh session can use a still-valid business grant only after current identity, membership and credential checks. Revocation and command-recovery paths remain available under current administrative authority even when the original target/source eligibility is gone.

## Owner location evidence and legal boundary

SupplyConnect publishes a minimal current owned-organization/warehouse-or-depot reference with actual organization and premise versions. An inactive premise is not an eligible operating source. PharmaConnect publishes an exact independent/chain organization and owned outlet reference; its immutable structural rows use structural revision 1. Hospital-owned pharmacies require hospital workforce authority and are refused by this independent source family.

These references contain no stock quantities, contacts, licence numbers or professional claims. Their country is India. They establish structural ownership, not permission to trade in a state. Maharashtra and Odisha commitments still require exact premises, independently accepted relationships/grants, current licence/qualification evidence, category/activity policy and all stock/financial checks. An affiliation or a declared address/registration is not activation.

The physical stock owners retain their ledgers. The collaboration layer consumes authorized projections and owner-issued reservation/dispatch/receipt references; it creates neither a third stock master nor cross-module table joins.

## Acceptance and release

Tests must cover source-kind/location/tenant isolation, unchanged hospital keys, no global permission leakage, unknown dependencies, stale source/Staff/link versions, known reviewer aliases, membership/credential cutoff, duplicate/changed command intent, replay after revocation, projection races and atomic audit/outbox rollback. Browser acceptance must use the actual registered owners and distinguish fixture authentication from real sign-in acceptance.

Before release, implement and verify the owner operations, administrative and recipient UI, bilateral relationship/grants, quality/balance/reservation/dispatch, receiving/discrepancies and financial composition. Update delivery evidence and generated route/schema references. Prepare a concrete public-repository release and exact Kargo migration/image promotion; new production mutations require their named approval. This ADR does not close those remaining gates.

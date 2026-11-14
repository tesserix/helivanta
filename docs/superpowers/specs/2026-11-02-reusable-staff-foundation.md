# Reusable Staff foundation and departmental specialization

Status: active implementation; no new production rollout or migration.
Requested outcome: departments and product areas reuse Staff management, including
Pharma Connect, and add their own departmental responsibilities without copying
people, employment, identity evidence or access rules.

## Ownership and invariants

Workforce owns canonical Staff records, employment, supervision, reviewed login
links, credentials and postings. IAM owns identities, membership and independently
reviewed permissions. The platform organization-structure owner holds departments
and sub-units (accepted ADR-0044); FacilityConnect owns physical locations.
Pharma Connect owns outlet operations and pharmacist-specific verification and
designations. Product records reference canonical Staff IDs and the effective
source version/generation; no product may create a second employee master.

A job category, department, posting, shared component or displayed Staff record
never grants access. Product admission and sensitive actions require current
verified tenant, narrower hospital/outlet scope, independently approved assignment
and the relevant current credential/designation. Ending source employment or a
posting must invalidate dependent authority without erasing historical evidence.
Cross-tenant borrowing is prohibited. Standalone pharmacy organizations exist;
they must never be represented by fabricated hospitals to fit current Staff APIs.
The employer-scope decision is accepted in
[ADR-0054](../../adr/0054-typed-employing-establishments.md) but not yet
implemented. Existing Staff remains hospital-scoped until that contract is
explicitly evolved. Current enrollment
already retains hospital-local `department_revision_id` references to Workforce
HR master revisions. These are HR profile vocabulary, not the platform-owned
organization hierarchy, supervised postings or product-unit authorization. The
department owner work must reconcile and migrate those references explicitly;
creating a second unconnected department list is not an acceptable outcome.

## Numbers, boundaries and failure behavior

Use existing unmeasured Staff planning budgets: 50 peak requests/s, 80:20 reads to
writes, 2–20 KiB bounded responses, 500,000 Staff at 12 months and 1.5 million at
36 months, p99 under 500 ms and 99.9% availability. A rendered directory page
contains at most 200 canonical records; filtering describes the current page,
never all staff or authority. Reuse the modular monolith and existing Global API;
no new service, cache, queue or deployment cost is required by the first slice.

Private Staff identity/employment data is the asset. Other tenants, users in
sibling hospitals/outlets and stale logged-in accounts are the relevant attackers.
The server's verified tenant and current object grants remain the trust boundary.
Client slots and labels are presentation only. When session/scope changes, the
workspace clears private rows and panels; unavailable authorization denies.
Loading/error states never display retained rows as verified current results.

## Delivery and completion evidence

1. **Shared basic directory:** a typed shared UI component renders canonical
   identity summary, employee code, job category and employment state. It supplies
   page-local filtering, accessible states and bounded pagination, with explicit
   caller-owned actions. The existing Staff workspace consumes it; its server
   permissions, account/scope reset, forms, private panels and navigation guards
   remain intact. No private fields are copied into a product-specific master.
   Tests must prove read-only consumers, custom actions, page-only filtering,
   loading/error precedence and paging. Existing workspace journeys must still pass.
2. **Canonical department owner:** implement the accepted platform-owned
   department/sub-unit contract and UI, tenant/hospital RLS, versions, retirement,
   supervised hierarchy and history. Reconcile #22/#287/#616 rather than adding
   lists inside Workforce or Pharma Connect. Test sibling/foreign scope, cycles,
   concurrent changes and historical references.
3. **Shared postings:** Workforce owns effective-dated Staff-to-department/product
   unit postings and supervision. Postings propose separate access; transfer/end
   invalidates dependent authority. Cover contractors, independent review, replay,
   current-state races and lifecycle cutoffs. Existing hospital records need an
   explicit migration path if employer scope expands.
4. **Pharma specialization:** consume the same Staff reference and shared UI;
   bind approved pharmacy units through owner-verified references. Add council
   registration, current evidence, in-charge designation and assistant-versus-
   pharmacist action policy under #418 without copying Staff identity or assuming
   that the category `pharmacist` permits dispensing. Regulatory activation needs
   applicable primary sources and qualified state/service policy review.
5. **Future consumers and acceptance:** document an integration example with
   canonical reference + product-owned fields, current-source/assignment checks,
   and source-ending enforcement. Verify actual normal-role UI journeys for
   Staff preparation and departmental/product use, independent approval, denied
   sibling/foreign scope, expired/disabled source and preserved history.

All five items remain part of the objective. Passing the shared component tests
alone does not establish departments, standalone-employer support, pharmacist
verification or whole-Staff completion. Keep the shared delivery tracker current.

## Alternatives and migration

Rejected: product-owned Staff tables (duplicate identity/lifecycle), arbitrary
JSON specialization fields on Staff (unbounded ownership and disclosure), UI-only
department filters (no authorization), class inheritance across domain owners
(couples unrelated authority), or a new staff microservice before a scaling need.
Composition of canonical references, shared components and owner-specific records
preserves independent policies and is the chosen approach.

The first UI extraction changes no API/schema/event/permission and requires no
migration or production flag. Reverting its source restores the previous renderer
without altering records. Later additive schema/API changes require canonical
contract generation, owner/consumer tests, GitOps and named migration/rollout
approval. Previously approved activation does not approve those future changes.

# ADR-0044: Organization structure owner and access defaults

Date: 2026-10-26. Status: accepted by the product owner; no implementation in this change.

Settles ORG-D1, ORG-D3, ORG-D4 and ORG-D7 from the
[organization knowledge index](../domain/organization/README.md#open-decisions).
ORG-D2, ORG-D5 and ORG-D6 stay open. Each decision takes the recommendation
already recorded there, which follows common practice for least privilege,
joiner-mover-leaver control and administrator recovery: NIST SP 800-53 AC-2
(account management), AC-5 (separation of duties) and AC-6 (least privilege),
ISO/IEC 27001:2022 Annex A 5.15–5.18 and 8.2, and Microsoft Entra guidance to keep
more than one privileged administrator and a verified recovery path. Helivanta is
built for hospitals in any country and launches in India first, so none of these
decisions depends on one country's titles or law.

## Decisions

**ORG-D1. A platform organization-structure owner holds the department and
sub-unit master.** It owns one effective-dated tree per hospital. FacilityConnect
owns physical locations, workforce owns postings, and every product references
department and sub-unit IDs from this owner. This resolves the overlap between
#22, #287 and #616 and unblocks #429.

**ORG-D3. The tenant owner holds no clinical access by default.** Clinical access
comes only from a separately assigned clinical template whose credential and
privilege preconditions are met. The reconciler must stop appending `tenant_admin`
to clinical permissions (ORG-AC-03), which also removes the current conflict with
[ADR-0008](0008-multi-hospital-tenancy-and-sharing.md).

**ORG-D4. A posting proposes access; it never grants it.** An authorized approver
activates the proposed assignment. Ending a posting, exit, suspension and
credential expiry revoke dependent access automatically on the effective date,
without waiting for a person.

**ORG-D7. Privileged grants always need a second person.** Multi-hospital tenants
must keep at least two owners. In a single-owner tenant, a privileged grant takes
effect but notifies every owner and enters a review queue the auditor template can
read; it cannot be self-approved. Lost owner access is recovered only through
verified platform support on the just-in-time operator path, with tenant-visible
audit.

## Consequences

The department owner is a new module with its own migration, RLS and contract;
products must not create private department lists in the meantime (ORG-ST-02).
Removing clinical permissions from `tenant_admin` changes existing tenants, so it
ships as its own slice with a test proving no patient-identifiable permission is
reachable through the template. Posting proposals depend on linking workforce staff
to login subjects. Rejected: letting postings grant access directly (an HR edit
would become a clinical grant) and allowing a single owner to self-approve
privileged grants (no second person ever sees them).

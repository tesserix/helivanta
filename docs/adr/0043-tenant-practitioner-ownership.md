# ADR-0043: Tenant-isolated practitioner identity ownership

Status: implementation proposal; no production clinical authority activated.

## Context

MediConnect discovery (#77), scheduling (#102) and released clinical records need
one accountable professional source. There is no implemented practitioner owner.
Workforce HR staff/credentials do not establish clinical privileges. #39 proposes
a residency-wide registry; #474 instead requires separate tenant records and no
cross-tenant reads. Root enterprise isolation applies to this implementation.

## Decision

Use a `practitioner` module within the existing API. It owns tenant-isolated
professional identity, with separate registration, verification and clinical-role
histories developed against the full #474 acceptance. Follow verified session
and membership authority, explicit administrative permissions, forced tenant RLS,
immutable command identities and attributable read/write audit. Client claims and
administrative intake cannot create HPR verification or clinical authority.
No HR table joins, copied practitioner master in scheduling, or cross-tenant
matching are permitted. Public discovery is a distinct authorized projection;
base registry reads do not publish a listing.

The initial implementation remains unregistered in the runtime registry while
verification/role contracts and dependency tests are developed. Do not promote
this branch as a complete practitioner service or close parent issues. Integration,
contract inventory, matrix/architecture tests and deployment are explicit gates.

## Alternatives and consequences

A shared cross-tenant identity would require explicit privacy/ownership decisions
and a reviewed migration; it is not inferred from the older issue. Using HR as the
clinical registry conflates employment with accountable professional authority.
A separate deployable service adds networking/availability costs without a different
scaling requirement. Tenant-local duplicates across unrelated hospitals remain
separate; do not silently merge them or rewrite signed historical attribution.

Additive forward-only migrations retain identity/audit evidence during rollback.
No destructive down migration is supplied. Canonical base-record intent is immutable;
future correction/verification commands must preserve attribution and prior facts.
See the [implementation specification](../superpowers/specs/2026-10-26-practitioner-master.md)
for capacity assumptions, dependency failure behavior and remaining release gates.

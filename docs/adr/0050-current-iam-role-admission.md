# ADR-0050: Current IAM roles bound graph admission

Date: 2026-10-30. Status: implementation candidate; review with the Access/RBAC
change. Production activation is separately gated.

## Context

IAM's membership rows are authoritative. OpenFGA role tuples are asynchronous
projections. A real PostgreSQL/OpenFGA signed-session regression reproduced a
removed administrator role still authorizing a request while another role kept
the subject a tenant member. Retry, reordering or delayed cleanup must not retain
authority absent from IAM.

## Decision

The authenticated V1 permission resolver and route membership checker share an
IAM-owned adapter. Permissions are the intersection of graph-resolved authority
and canonical registry grants for currently held IAM roles. Membership requires
both graph membership and current known IAM membership. Read IAM only through
normal tenant-scoped forced RLS, after the graph response. Source/graph failures,
unknown roles and an exceeded source bound deny. No network call runs inside the
source-read transaction.

Use existing `platform.GrantsFor` and the current role catalogue; no copied
permission mappings, new clinical policy, private grant vocabulary, cache,
service or datastore. Retain independent hospital/object and scoped-assignment
guards. Preserve authenticated self-service route markers and other tenants.

## Consequences and alternatives

This closes stale projection authority at new-request admission. It does not
replace transaction-held current authority, reviewed employment, legacy-grant
classification, identity provisioning, safe removal commands or effective
cross-owner lifecycle cutoffs. Those remain in the
[full A01–A12 plan](../superpowers/plans/2026-10-30-staff-rbac-completion.md).
Tenant discovery/switching retains its existing role-list contract.

Reject event ordering/boot reconciliation as the sole current authority check;
reject SQL-only authorization because it bypasses graph denial/unavailability.
The existing bounded indexed lookup adds database work without new infrastructure
or schema. The [design](../superpowers/specs/2026-10-30-current-tenant-role-authority.md)
records planning budgets, failure semantics, compatibility and acceptance.

Graph-only legacy authority denies until reconciled through the IAM owner. Do not
backfill membership from tuples. Deploy only a tested exact candidate after the
runtime cohort and recovery checks. Rollback removes this additional protection
and needs specific live approval; it does not erase membership or audit history.

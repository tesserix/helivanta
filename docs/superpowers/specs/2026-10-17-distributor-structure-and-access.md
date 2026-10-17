# Distributor structure and time-bounded access

Issue: #1137; parent epic #1133. This extends released bins (#1192) and branch grouping (#1194). Development approval includes coordinated agents; the additive production migration requires separate approval after local acceptance.

## Workload and boundary

Planning assumptions, not measured production traffic: 20 administrative writes/s peak and 200 structure/access reads/s across tenants, payloads capped at 32 KiB, roughly 10:1 reads:writes. At 12 months plan for 1,000 organisations and 200 territories each (200,000 territory rows); at 36 months, tenfold growth. Per-organisation lists use bounded keyset pages. Target existing platform availability 99.9% and p99 read latency below 300 ms, subject to load-test evidence; no new service or infrastructure is required. PostgreSQL remains authoritative and read-after-write is required. No new Valkey cache or Temporal worker is justified for single-transaction administration; existing NATS outbox carries minimized versioned events.

## Corporate identity

Keep country-specific legal registrations and tax declarations on the existing owned organisation master. Corporate group text remains descriptive. Add a separately versioned corporate structure resource with an optional parent owned organisation. Group roots and subsidiaries form two explicit levels; branches remain operating premises underneath a legal entity. Cross-country corporate ownership does not merge registrations, taxes, licences, data partitions or permissions. Assignment requires current administrator authority on both organisations; clearing only requires authority on the child. Unknown/inaccessible parents cannot be resolved through the relationship.

A generated root boolean and composite self-FK enforce two levels and acyclicity even below the handlers, without recursive reads through partially visible RLS rows or SECURITY DEFINER. The simplest alternative (a nullable text label alone) is rejected because it cannot enforce canonical ownership; arbitrary-depth hierarchy is unnecessary for the stated group/legal-entity/branch structure and would require additional safe traversal design. Root configuration is explicit, auditable and versioned.

## Territories and representatives

A territory belongs to one owned organisation and country; it records unique code, name, region, optional city/description, active/inactive state and optional branch. Assignments are normalized and unique, capped at 50 per territory. An assignment references a subject with an existing effective explicit sales grant and never grants access itself. The API validates current branch ownership/status/country and current representative authority. Sales readers get only their currently assigned active territories and their own assignment; private legal/finance/licence data and other users' assignment history remain unavailable. Only administrators edit; explicit auditors/compliance read without mutation.

## Explicit access

Fix the legacy grant-administration path first: tenant-wide permission is necessary but insufficient; require a current administrator grant on the target organisation and protect its last administrator. Existing permanent legacy grants remain visible and explicitly revocable rather than silently converted.

New grants are versioned UUIDv7 resources with subject, role, optional premise, start, mandatory expiry, reason and retained revoked state. A common SQL effective-role predicate governs RLS, current access, directory projections, licence review and mutation helpers. Global checks exclude location-only grants. Expired, scheduled and revoked rows confer no authority; revocation and edits are checked on every server operation, including stale sessions. Finite administrators cannot mint permanent legacy authority or extend rights beyond their own authority. The verified RoleLister dependency validates target tenant membership without cross-module SQL and without remote calls inside locked business transactions.

Tenant-level endpoint permissions and object grants remain separate. These additions do not create platform-wide support privileges or clinical privileges. Assigned-delivery and approved support-escalation resources are implemented in the slices below; their local and production acceptance must be verified before #1137 can close.

Administrator, purchasing, finance, compliance and sales grants are organisation-wide.
Warehouse, inventory, picker, packer, dispatcher, driver and auditor grants may be
organisation-wide or restricted to an active owned warehouse/depot. Regulator
grants require an exact active warehouse/depot. The shared contract rejects
unusable combinations; unsupported historical grants remain revocable. Directory
and current-access DTOs keep global `roles` and separate `operating_roles`, so
location authority can open an operational workspace without becoming private
profile authority. Tenant-level support capability cannot reroute a user who also
has valid scoped operational access into a support-only workspace.

## Operational access limits

At least one permanent explicit organisation administrator must remain. An active
finite administrator is not a replacement for that recovery anchor: otherwise
removing the permanent administrator would make the organisation irrecoverable
when the finite grant expires. A deliberate transfer to another verified
permanent administrator is supported. This does not create a tenant-wide recovery
bypass or complete the separately owned identity recovery workflow.

An administrator of the exact owned organisation can configure versioned maximum
durations for new and extended non-administrator operational grants and for support
requests/approvals. The existing platform ceilings remain 366 days (8784 hours)
and 24 hours respectively; organisation limits may be tighter, with a minimum of
one hour. Independent support approval is mandatory and cannot be disabled. This
resource is not the purchase-approval matrix owned by #532 or the general tenant
access-approval engine owned by #434/#431. Country regulatory policy remains with
the reviewed Workspace/trading-policy master.

`GET/PUT /parties/:party/access-limits` uses the existing access-management
capability plus fresh exact-party administrator authority and owned-organisation
proof. A missing configuration returns version zero and the documented platform
limits without writing rows. A database, identity or authorization failure is
not a missing-policy fallback. Writes require expected version and a reason,
retain immutable snapshots, and commit row audit and a minimal versioned outbox
event together. The revision list uses the existing bounded keyset contract.

The same organisation access-administration lock serializes settings changes,
grant mutations and support decisions. Database triggers re-evaluate current
limits on new active non-administrator grants, expiry extensions, new support
requests and approvals, so stale sessions, forms and direct requests cannot
issue longer access. Duration is the complete requested window from valid-from
to expiry, not a sliding extension from the current time. Lowering a limit does
not rewrite or revoke existing approved access: its explicitly recorded expiry
remains authoritative. Existing exact replay, reason corrections, shortening
and explicit revocation remain possible. A pending support request outside the
new limit must be rejected/revoked and recreated with a shorter window before
approval. The interface must explain this prospective behavior rather than
promise retroactive revocation.

After an organisation explicitly configures limits, the permanent legacy grant
endpoint rejects new non-administrator grants and directs administrators to the
timed-grant flow. Pre-existing permanent grants can still be read, replayed and
revoked; administrator ownership/bootstrap remains separate. The compatibility
endpoint therefore cannot bypass an organisation's configured issuance limit.

## Consistency and recovery

Optimistic expected versions and stable resource IDs make exact replay return the saved state while changed stale writes conflict. Mutations serialize structural/authority changes with bounded transactions; business row, immutable revision, audit and minimized outbox event commit together. Database constraints protect scoped references and duplicate identities. NATS downtime leaves a committed outbox backlog for normal relay retries, not a partial external write. PostgreSQL or policy failures deny the operation through existing privacy-safe non-success responses; FGA identity failures fail closed. No private record is retained in UI after an authoritative denial.

## Migration, cost and acceptance

Additive 0007_supply creates new structure, territory and grant resources and replaces helper predicates; additive 0008_supply adds operating projections, delivery, support and operational access limits. Existing grants remain valid and no backfill/deletion is required. DDL has bounded timeouts; new tables receive their indexes before population. Application rollback can retain added rows, but must not silently remove time-bound authorization enforcement; use a reviewed forward fix for authorization defects. Existing two API/two UI replicas and PostgreSQL capacity are retained. New cost is row/index/audit/outbox storage and bounded queries; no new cloud resources.

Plan a two-release rollout: first release the 0006-compatible legacy-grant
security fix and verify every API replica uses that fixed image. Only then apply
0007/0008 and release their dependent API/UI after specific migration/release
approval. This prevents an old permissive binary remaining active during the
authorization migration. This is a rollout requirement, not evidence that either
release or migration has happened. Capture concrete image, migration and replica
evidence in the release record.

Run actual red/green integration tests with PostgreSQL/NATS, database-level isolation/constraint tests, concurrent/replay/stale/expiry/revoke cases, fail-closed identity tests, shared-contract tests, full local gates, genuine authenticated UI/API workflows and production acceptance after migration approval. Use synthetic records only. Do not close #1137 while role/delivery/support acceptance remains unverified.

## Operational access and logistics implementation

Separate warehouse/inventory, picker, packer, dispatcher, driver, regulator and platform-support capability roles; no role aliases to purchasing, finance, clinical or tenant administrator. Actual operating-location projections reuse Supply premises/bins with exact current location grants. Picker/packer projection access does not invent pick/pack completion; the fulfilment workflow stays with #1141. Finance cannot obtain stock actions from this work. Pharmacy owner/purchasing/inventory action roles must remain in the Pharmacy owner module and intersect its existing outlet grants; Supply grants never confer Pharmacy access.

Introduce one canonical Supply delivery-task aggregate for manual logistics scheduling and assignment. This resource is a logistics task, not a shipment, accepted medicine order, inventory movement, invoice or payment, and produces none of those side effects. It owns unique source reference, origin premise/country, business delivery destination/contact, planned window, package count/handling notes, assignment subject/generation/expiry, status, optimistic version and immutable revisions. Future fulfillment consumes/links this same source-keyed task master rather than inventing another delivery identity. Authorized administrator/dispatcher creates, assigns/reassigns or cancels; driver reads/actions require exact assignment plus current scoped driver grant and unexpired assignment. Driver views contain only necessary task/handoff fields; no patient/HR/credit/tax/licence data. Server transition/replay checks revalidate access before returning any saved response. Opaque denial, immediate reassignment/revocation, concurrent commands, immutable histories, minimal outbox and rollback must be tested with real PostgreSQL. No regulated dispatch or physical delivery is initiated in testing.

Support access uses a separate request and independent approval, never a generic support/admin grant. Record named requester/beneficiary, exact organisation/premise, support ticket, reason, start/mandatory expiry (at most 24 hours), pending/approved/rejected/revoked state and version. Approver must currently administer the organisation and differ from both requester and beneficiary. Beneficiary must be a verified current member; tenant capability roles never grant blanket tenant data. Approved reads are read-only, narrow nonclinical operational diagnostics and must recheck membership, expiry, scope and current approval, append a read audit and expose no secrets, contacts, financial/clinical payloads or document downloads. Approver and beneficiary authority can be revoked immediately; no timer is required for correctness. Test self-approval, wrong scope/tenant, expired/revoked approvals, replay/stale conflicts and audit atomicity. UI has request/review and current approved-session views with server state/expiry and no privilege impersonation.

These are further correct slices within #1137. They add local schema/contract changes subject to the same full validation and concrete migration approval before release. Broad warehouse/fulfilment stories remain separately tracked; this issue closes only on its own complete access acceptance.

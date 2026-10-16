# Private premises and licence declarations

Issues: #1135, #1137, #1005; epic #1133. Partial delivery: these stories remain open.

## Boundary and expected load

The Supply module owns private premises and declared licences under its existing party identity. Independent pharmacies and distributors need no hospital identifier. Platform tenant identity comes exclusively from the verified principal. A party is a tenant-owned business record, not a second platform tenant or proof of a counterparty's identity.

Planning assumptions, not measured service guarantees: 1,000 tenants at 12 months, 5,000 at 36 months; 20 parties × 5 premises × 4 licences per tenant; ten revisions per object gives roughly 5 million rows at 12 months and 25 million at 36 months. Peak 50 requests/s, 10:1 reads:writes, request limit 32 KiB, page size at most 200. Target 99.9% availability, p99 reads <300 ms and writes <500 ms, excluding network. Composite indexes and keyset pagination suit one PostgreSQL authority; no sharding or cache is warranted without load evidence. Estimate 5–25 GB for registry/index/history working data, with shared PostgreSQL/NATS infrastructure and no additional paid service for this slice.

## Threat model and invariants

Addresses, licence identities and revision snapshots are private business evidence. Other tenants, users with directory-only access, and overly privileged operational users must not read them. Route permission AND a current party-specific role are required. Forced RLS additionally checks exact tenant, party and subject, even if hospital/group visibility is broadened. Compliance may maintain declarations; it cannot edit premises or ordinary profiles. Auditors read only. Every mutating transaction locks the authorizing grant so revocation serializes with writes.

Premises capture code, name, type, address, country-bound IANA time zone and active/inactive status. Country must match the immutable party country. Licences reference a premise in the same tenant/party/country and capture declared type, authority, jurisdiction, number, issue/expiry dates and handled category set. All licence status is **declared**, never approved. These categories are metadata, not permission to handle restricted products.

Stable client UUIDv7 identities, optimistic expected versions, and transaction locks make exact concurrent retries return one result. Current/current-minus-one equal requests replay; stale changed or future requests conflict. Licence business identity is unique by tenant, country, type, authority, jurisdiction and number. A duplicate rolls back head, history, audit and outbox. Every successful change has a retained immutable snapshot and actor. Deferred head-to-revision foreign keys prevent committing a head without its revision.

## API and screen

The `/v1/supply/parties/{party}` prefix exposes `PUT/GET /premises/{id}`, cursor `GET /premises`, `GET /premises/{id}/revisions`, and equivalent `/licenses` routes. Singleton and history lookups return 404 for unknown/inaccessible objects; route denials 403; invalid input 400; version/uniqueness conflicts 409. Requests reject unknown fields, forged tenant/status, excessive input, invalid calendars, duplicate categories and incompatible country/time zone.

The private organisation view contains separate premise and declaration cards, accessible labelled forms, shared dropdowns, explicit loading/empty/error/retry states and revision paging. Add/edit scrolls to and focuses the form. IDs survive failed saves. A licence's selected premise remains resolvable when outside the currently loaded premise page. No UI control suggests that declaration verifies a licence.

## Failure and publication

PostgreSQL or authorization failure denies the operation. NATS outage leaves durable pending outbox rows; write correctness does not depend on broker availability. Atomic head/revision/audit/outbox writes prevent partial success. Opaque event IDs and versions carry no address/licence number. Duplicate delivery does not represent a second registry write. Temporal, GCS and ClamAV are not involved in a declaration write; evidence scanning/review is a separate complete slice described in ADR 0028. Valkey is not authoritative and is not added to private registry reads.

## Migration and recovery

Additive `0002_supply` creates four new empty tables, functions, constraints, indexes and policies under bounded DDL timeouts; no existing data rewrite. Apply to a disposable database and reapply idempotently via the migration runner. Rollback is application-code rollback preserving immutable history. Destructive SQL down migration is intentionally unsupported and needs separate approval. Production migration/rollout requires the concrete named release approval.

## Exclusions and alternatives

This slice does not complete evidence upload/scanning, independent verification decisions, onboarding lifecycle, banking references, bilateral trading, commercial offers, orders or country policy activation. Those remain open under their owning stories. In particular no scan status or approval can be self-declared.

Rejected alternatives: forcing hospital IDs excludes standalone organisations; reusing HR business records crosses module ownership; storing unversioned JSON loses immutable identity/history; per-tenant databases multiply migrations/pools without a requirement; caching authorization risks delayed revocation.

## Acceptance

Real disposable PostgreSQL and NATS tests cover private/country-bound create/read/edit, history paging, unknown records, auditor/compliance boundaries, revoked grants, wrong-tenant references, broadened-group RLS, invalid fields, duplicate identity, six concurrent identical retries, immutable histories, audit and atomic outbox rollback. UI tests cover permission denial, read-only auditors, focus, stable failed-save retries, country defaults and an off-page selected premise. Browser acceptance uses only synthetic TEST records against the local API, including reload, dates, selection, history and mobile layout. Run repository lint/typechecks/build/race suites and report any existing gate failures explicitly.

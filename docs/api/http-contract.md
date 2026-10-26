# HTTP contract conventions and current exceptions

[Contract index](README.md) · descriptive baseline, with explicit proposals

## Current transport

Backend routes use `/v1`, not the proposed `/api/v1/<product>` edge routes in older architecture diagrams. The web application has its own `/api/*` handlers. Do not prepend `/api` to a backend path without checking the actual proxy. [Runtime inventory](reference/README.md) enumerates backend routes; health and explicitly unauthenticated routes are listed separately.

Authentication accepts a Helivanta session bearer token or `helivanta_session` cookie. A Zitadel ID token is exchanged at the login boundary; it is not a substitute for the Helivanta session on product routes. This is not evidence that OAuth client-credentials delegation is implemented for external services.

Authorization resolves tenant permissions and then checks current tenant membership and declared route permission. `authz.Public` means no specific permission, **not** unauthenticated access. `authz.NoTenantMembership` still requires a valid session. Domain handlers additionally enforce hospital/outlet/premise/base/facility grants and object state. A tenant administrator is not automatically a patient registrar, pharmacy outlet operator or clinical reviewer.

`X-Request-ID` is returned for correlation. Accepted incoming values match `[A-Za-z0-9_-]{1,128}`; otherwise a new ID is generated. The error body does not currently include this ID. Global API retains one validated response ID instead of appending an upstream copy. An edge proxy can replace the supplied ID; use the returned ID for log correlation. Sensitive responses default to `Cache-Control: no-store`.

Pharmacy, supply, MediConnect, patientmaster and privacy support the optional
`X-Helivanta-Expected-Tenant` precondition. If supplied, it must be a single
nonzero UUID matching the verified session tenant; otherwise the request returns
`409 session_changed`. MediConnect, patientmaster and privacy also reject empty, repeated
and comma-joined values before domain reads/writes. Omitting the header preserves
existing behavior. It detects an unintended workspace switch; it never selects
or authorizes a tenant, replaces membership checks or grants hospital access.
Workload-only current-claim checks retain their separate contract. Check the
owning handler for adoption elsewhere.

## Success, errors and pagination

Success responses are passed through unchanged; there is no universal `{data: ...}` wrapper for individual resources. Current errors use:

```json
{ "error": "conflict", "message": "result already recorded" }
```

Common errors are `invalid_request` (400), `unauthenticated` (401), `forbidden` (403), `not_found` (404), `conflict` (409), `rate_limited` (429), `internal` (500) and `authz_unavailable` (503). Domains add codes such as `session_changed` and conflict detail fields. Missing route permission/non-membership returns 403; an invisible domain object generally returns 404. Preserve domain-specific errors and avoid promising that all errors have only these two properties.

`ListRoute` and read-only `SearchRoute` responses have:

```json
{ "data": [], "page": { "next_cursor": null, "has_more": false } }
```

A **counted** list (`CountedListRoute`, #1295) adds the size of its filtered set: `"page": { "next_cursor": null, "has_more": false, "total": 41 }`. The total and the rows are read in one read-only snapshot, so they agree; the total is exact at that snapshot and moves as the data does. The [runtime reference](reference/README.md) marks counted routes in its "Page total" column.

Query `limit` defaults to 50, supports 1–200 and rejects invalid/out-of-range values with 400. Pass `next_cursor` back as opaque `cursor`; do not construct or reuse it across tenants/scopes. Collection ordering is generally descending `(created_at,id)`; history/journal sequences and custom report endpoints can differ. A counted route may order oldest first or by another timestamp; its ordering is fixed by the route, never chosen by the client, and a cursor issued for the other direction is refused with 400. A cursor is not a snapshot or an authorization credential. Preserve filters and scope while paging; deduplicate by resource ID if a workflow tolerates changing data.

429 includes integer-second `Retry-After` and `RateLimit-Limit`, `RateLimit-Remaining`, `RateLimit-Reset`. Obey the retry delay and apply bounded jittered backoff. A timeout on a mutation means the result may be unknown, not that nothing happened.

## IDs, time, money and validation

- Use the exact identifier version accepted by each domain. New pharmacy/supply resource paths commonly require UUIDv7; legacy visit/lab identifiers use existing UUIDs. Do not rewrite historic IDs.
- Timestamps are RFC 3339 values with an explicit offset; persisted instants use PostgreSQL `timestamptz`. Calendar dates such as DOB, expiry and attendance dates stay `YYYY-MM-DD`; operational deadlines use the owning facility/outlet timezone.
- Money uses integer minor units with currency or an exact decimal string as specified. Stock quantities, packs and unit conversion are distinct; never silently reinterpret units or use binary floating point for money.
- Missing, null, empty string, false and zero differ. Generated Zod models may normalize trim/case before sending; the owning backend validation remains authoritative. Unknown-field rejection and body limits vary by handler; there is no proven universal strict decoder.
- CSV/binary evidence APIs have their own content types and response headers. Do not run them through an assumed JSON envelope.

## Retry and concurrency contracts

There is **no universal implemented `Idempotency-Key` or `If-Match` guarantee**. Registration uses body `request_id`; several domains use client-chosen resource/command IDs plus immutable content checks; others use explicit `version` or `expected_version`. Lab result and legacy dispense completion use guarded updates and return 409 after completion. Visit creation currently has no documented deduplication key. Never automatically retry it after an ambiguous timeout.

Every new reviewed operation must specify key scope, fingerprint fields, retention, replay status/body, conflicting-key status, concurrency token and recovery lookup. For physical dispensing, payment, clinical sign-off and stock movement, a retry is safe only when the owning service enforces it durably. Do not assume a 409 means the original request succeeded; refetch and reconcile the exact operation.

## Conflicts requiring a decision

Issue [#4](https://github.com/tesserix/helivanta/issues/4) requests OpenAPI-first generation, RFC 9457 errors, `Idempotency-Key`, ETags, external M2M and a minimum 12-month external sunset window. Existing architecture prose proposes `{code,message,request_id}`. Current code returns `{error,message}` and header correlation. These conflict; this document records the implementation and proposes retaining v1 behavior until a reviewed migration is agreed. Do not silently change all handlers while writing docs.

Likewise, older issues name GIP/Keycloak and Redis while later ADRs select Zitadel and the target uses Valkey. Follow current source and accepted ADRs; update backlog wording during implementation review. Proposed OpenAPI authority must reconcile the existing JSON→Go/Zod generators rather than create parallel handwritten model definitions.

For an additive v1 change, assess requiredness, enum additions, nullability, validation tightening, permissions, ordering, event semantics and status codes—not just field removal. A stricter permission or a new required input can break consumers. A breaking contract requires a reviewed new version or explicit coordinated migration with consumer evidence and a sunset plan.

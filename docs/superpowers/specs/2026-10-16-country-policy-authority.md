# Reviewed country policy authority

Child #1182; parent #1134; epic #1133. Owner Sam123ben. Status: configuration/API/UI verified locally; production and restricted-trade integration pending.

## Scope and boundaries

Implement independently reviewed IN/PK policy configuration through workspace,
with a platform-owned transaction guard available for subsequent trading-module integration. This release connects the guard to the configuration preview only.
Country metadata remains generated from the existing shared country profiles.
Tenant, jurisdiction, category, operation and currency are an exact scope; there
is no default country, wildcard jurisdiction or inherited approval. A policy
approval records software configuration review, not government verification.
No real source, jurisdiction, licence or legal approval is invented.

Assume 100 tenants, 50 policy scopes per tenant, 12 immutable revisions per scope
per year: 60,000 versions at 12 months, 180,000 at 36 months. A configuration
document is bounded to 128 KiB, 30 citations and all 11 requirement domains.
Peak configuration traffic 5 reads/s and 1 write/s; trade decisions inherit the
existing 100 reads/s and 20 writes/s planning envelope. Target configuration
p99 <500 ms and 99.9% monthly availability; these are targets, not measurements.
One indexed PostgreSQL database meets this envelope without another service.

Assets are trading eligibility, policy provenance and tenant configuration.
Threats include another tenant, a sibling organisation user, and an author
self-approving changes. The trust boundary is verified tenant membership,
route permission, explicit scope grant, forced tenant RLS and independent
approval. Supply compliance permission alone grants no policy-scope access.

## Lifecycle and access

The creator owns a scope and may delegate explicit reader/editor/reviewer
access. Route permissions remain required. A version is submitted as an
immutable snapshot; a changed policy is another version. An independent
reviewer can approve or reject a submitted version with a recorded reason.
The author/submitter cannot approve it. Activation and suspension increment
the scope revision and serialize on its row. Immutable receipts bind caller,
operation, expected revision and canonical payload to a UUIDv7 key. Replay
returns the original receipt; conflicting reuse and stale revisions return 409.
Wrong-tenant or inaccessible objects return 404, authz failure 503.

List APIs are keyset paginated with a bounded page size. Detailed reads expose
only explicitly granted scopes; writes use transaction-local verified actor
identity. Citations are stored HTTPS links without credentials, query strings
or fragments; the server never fetches them. Every requirement has reviewed
applicability, a reason, and at least one citation reviewed before approval.
The eleven domains are licensing, product registration, prescription,
controlled medicines, cold chain, tax/invoice, credit notes, retention,
privacy, residency and reporting.

## Consistency and failures

The platform authority owns policy tables. A future trading integration must call its guard
inside the same tenant transaction as the business commitment. The guard
holds the scope row FOR SHARE through commit; suspension uses FOR UPDATE.
Current version, independent approval, citations and validity are evaluated
from PostgreSQL, never Valkey or an asynchronous projection. Unavailable DB,
missing policy or invalid policy denies the commitment with a stable reason.
No network request occurs while holding policy locks. Atomic audit and outbox
records accompany changes; JetStream retries do not repeat business writes.
Temporal reminder workflows and provider integrations belong to later stories.

The configuration UI uses the shared query/form primitives and accessible controls, showing
loading, empty, denied, conflict, dependency error and success states. Approval
shows complete citations and the independent-review requirement. Expired and
suspended status is explicit. A stale cached screen never authorizes a write.

## Migration, release and evidence

Additive migration `0002_workspace_country_policies` creates forced-RLS scope,
immutable version/source/requirement and review/access records with tenant/id
composite references. No existing tables are dropped or rewritten. Test empty
DB migration and repeat application; recover by deploying the previous binary
while preserving new tables and immutable history. Destructive down migration
is intentionally unsupported. A production application of this named
migration needs separate specific approval after the artifact is reviewable.
No live migration is performed during local development.

Prove real PostgreSQL tenant isolation, explicit scope access, self-approval
denial, replay/conflict/concurrency, IN/PK independence, expiry boundaries,
revocation-versus-commit locking, audit/outbox exact counts, PKR precision,
English/Urdu layout and actual UI/API journeys with synthetic data. Test
restricted listing/order/dispatch/dispense transitions before closing #1134.
Attach local and hosted checks plus deployed synthetic acceptance; pure rules
tests alone do not complete the story or certify compliance.

The alternative of a separate policy service is rejected: it introduces a
distributed commitment/revocation protocol without a different scale or data
owner. Approximate storage is <1 GiB at 36 months excluding DB overhead;
no new paid runtime dependency is introduced.

## Verified delivery slice

See [#1182 local acceptance and release evidence](../../delivery/country-policy-1182.md). The real owner/reviewer browser journey, native UTC fields, suspension denial and immutable history passed. Desktop and 390px English layouts were inspected. Urdu translation/RTL, country-specific adapters, real regulatory review and restricted listing/order/dispatch/dispense integrations remain unverified and outside this configuration slice.

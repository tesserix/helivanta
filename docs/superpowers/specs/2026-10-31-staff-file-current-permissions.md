# Current permission checks across Staff file I/O

Status: source implementation locally validated, not production activation. Full Staff
Access/RBAC A01–A12 remains required. Read with ADR-0049, the compound command
design and the Staff intake transaction-phase inventory.

## Boundary and actual gap

Workforce owns Staff files, employment evidence, benefits evidence and tax
attachments. IAM owns current tenant permission/membership source; OpenFGA is
its checked projection. Storage owns bytes and immutable generations. Private
Staff/financial evidence is the protected asset; a caller retaining an old signed
session and stale graph tuple must not release it after its IAM permission ends.

The shared `sendDocument` path currently authorizes the object, reads storage
outside transactions, and reauthorizes the object/release before recording a read.
Its owner grant remains current even if the account loses its tenant-level IAM
role during storage I/O. `receiveDocument` has the analogous gap between input,
quarantine storage and scan-command commit. Rechecking only the hospital row is
insufficient. No graph or provider network call belongs inside the owner SQL
transaction.

## Change

Declare the existing required permissions on each private document gate, using
server constants rather than request input. Refresh the existing authenticated
resolver before each owner transaction phase; check all required permissions from
that single current result. Preserve hospital/Staff/purpose/release checks inside
the owner transaction. Do not cache the result across storage I/O.

| Family                    | Read bytes                                                     | Upload bytes                              |
| ------------------------- | -------------------------------------------------------------- | ----------------------------------------- |
| Staff documents           | `workforce.documents.manage`                                   | Same                                      |
| Employment evidence       | `workforce.documents.manage` and `workforce.enrollment.manage` | Same if a future upload consumer is added |
| Benefit proposal evidence | `workforce.benefits.view`                                      | `workforce.benefits.manage`               |
| Tax declaration evidence  | `workforce.payroll.view`                                       | `workforce.payroll.configure`             |

No new role, permission, route, file scope or migration is introduced. Missing
current permission returns 403 without private bytes or a successful read/scan
receipt. Resolver failure returns the existing no-store 503. Invisible objects
retain 404. Storage failure retains its existing error/recovery contract. If a
quarantine write already occurred, denial must leave it uncommitted to scanning;
existing generation/retry/cleanup ownership remains authoritative.

Reusing `authz.Refresh` keeps the current source/graph failure behavior and avoids
a second role vocabulary or cross-module SQL in product code. Required compound
permissions are checked together after one resolver observation. This refresh is
not a transaction-held IAM source lock: withdrawal between that refresh and owner
commit, scoped HR activation and employment-generation cutoff still require the
planned guarded owner/consumer integration. Do not present this as full A06 or
whole-module lifecycle acceptance.

## Verification and cost

Reproduce a private download with real IAM, PostgreSQL, OpenFGA, released bytes
and a signed session. Remove its IAM administrator row during the storage read
while retaining ordinary membership, the hospital-manager row and old graph
permission. The final read must deny and preserve the earlier successful-read
count. Use public owner file/scan APIs; direct IAM state setup/fault injection is
confined to disposable test fixtures.

Cover each file family, each compound requirement, dependency failure, normal
reads/uploads, object revocation and upload loss before/after quarantine. Assert
actual response bytes, headers, source status, audit and outbox. Run affected
Workforce/journey race suites, vet/build/lint and contract freshness. Preserve
normal Admin/recipient/reviewer UI acceptance separately.

Reuse current Staff planning assumptions (50 peak metadata requests/second,
99.9% availability and metadata p99 below 500 ms); these are not measured file
SLOs. Each refresh adds the existing bounded five-second IAM lookup budget,
without storage calls under SQL locks or a new service/cache/table. There is no
new infrastructure cost class. Measure before release/SLO claims. Application
rollback preserves the schema and all file/history/receipt data, but reintroduces
this denial gap; production rollout or rollback needs its exact named approval.

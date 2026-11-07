# ADR 0065: Current bilateral supplier trade authority

Status: implementation in progress; no production trade activation.

Independent supply requires two tenants to consent to the same immutable terms.
The existing grant draft and receiving duty authorize neither consent nor trade.
This extends ADRs 0031, 0049 and 0062; it does not replace their owners.

SupplyConnect owns relationship terms, counterpart consent, exchange history and
order correlation. Each tenant retains a local, provenance-bound projection;
directed contracts carry only the disclosed terms and immutable correlation.
A supplied tenant UUID is an invitation destination, never authority. Source
organization identity is verified by the canonical onboarding owner. Each side
checks its actual product endpoint, current reviewed licence and exact approved
country/category/operation policy. Missing evidence denies eligibility.

Shared IAM owns effective named-person/group grants. The hospital authorizes its
own current published group or named Staff/account/posting basis; a supplier
independently authorizes the exact relationship, template, fields and interval.
Neither proposal nor historical acknowledgement grants current access.

Source owners expose narrow machine-authenticated current-claim contracts.
Consumers never query another tenant's or product's private tables. Current
checks bind exact relationship/terms/epoch, recipient, purpose, object and field
policy; no positive eligibility cache is permitted. Network checks run outside
owner database locks. The final local command repeats its authorization and
version checks. These are bounded observations, not distributed serializability;
revocation is repeated at every later disclosure/commitment boundary.

PharmaConnect keeps its existing stock and payable ledgers. SupplyConnect keeps
its distributor stock and seller financial records. Reservation reduces the
source owner's shared availability; dispatch and hospital receipt are separate
idempotent owner commands. Receipt, discrepancy, quality acceptance, invoice,
credit and settlement retain distinct evidence and status. No shared stock or
financial master is introduced. Outbox/inbox and business command identities
prevent duplicate effects; Temporal coordinates durable waits and compensation.

Existing unmeasured planning assumptions remain 50/150 tenants, 1,000/3,000 peak
RPS at months 12/36, 80:20 reads/writes, 2–20 KiB typical JSON and a 30% hot tenant.
New command inputs are bounded, collections use signed keysets, and current
identity/authorization/owner dependencies fail closed. The availability target
is 99.9%; the initial synchronous budget is p99 under 500 ms excluding an explicit
pending external decision. Actual trade volumes and latency require measurement.

Acceptance must exercise real owner modules through Global API and real
Postgres/OpenFGA/message paths, including cross-tenant denial, current-source
cutoff, self-review/alias refusal, conflicting and ambiguous retries, partial and
unsafe receipts, reservation races, duplicate/reordered messages and exact
financial reconciliation. External registry/issuer/payment sandboxes remain
separate from synthetic owner acceptance. A local test cannot activate a legal
policy, certify a licence or prove a production deployment.

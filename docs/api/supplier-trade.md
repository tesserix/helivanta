# Supplier trade owner contracts

Status: unreleased implementation in progress. This contract does not establish
full supplier fulfilment or regulatory readiness.

SupplyConnect owns bilateral relationship terms, consent history and directed
exchange. PharmaConnect owns its outlet regulatory-scope extensions and licence
revocations. Each product retains stock and financial ownership. Shared IAM owns
operating duties and effective partner grants; directory membership and a
relationship offer are not a grant.

Canonical DTO source is `contracts/supplier-trade.json`; generator
`node scripts/generate-supplier-trade.mjs` emits Go/Zod, the matching pharmacy
scope-input constraint and the directed event contract. Trading categories are
resolved from `contracts/trading-policy.json`, not independently redefined.

## Current local source facts

The owner callback requires an existing guarded tenant command. Supply checks the
exact owned organization/warehouse/depot, source versions, state, current latest
independent licence review, available evidence, expiry and category. Pharmacy
checks its exact organization/outlet/licence, current independently approved
state/category registration, evidence, validity and current country policy. A
hospital-owned outlet additionally requires its exact approved current department
binding and the canonical department owner. Missing/withdrawn facts deny the
callback; unavailable owners do not become empty data or successful verification.

`POST /v1/supply/internal/trading-source/check` is an internal machine-only source
fact interface. It is not exposed through the browser Global API. It requires a
separate Kubernetes TokenReview-verified caller with audience
`helivanta-supplier-trade-current`, never a session cookie or normal bearer token.
The exact caller account, peer URL and projected token file must be configured
together. Missing configuration does not activate the endpoint or trade.

Requests bind an unpredictable request identifier, exact endpoint, category and
operation. Replies bind that nonce and context, a current observation time and
versioned local owner facts. The client does not follow redirects, send cookies,
cache eligibility, accept ambiguous JSON, or reinterpret malformed/stale peer
responses as ineligibility. The verification kind is explicitly
`local_source_eligibility`. Canonical platform identity, bilateral consent, current
recipient eligibility, object authorization and field-release policy remain
separate mandatory checks. A positive source-facts response is not trading,
clinical, professional or licence authority.

## Pharmacy state/category scope and revocation

Paths under `/v1/pharmacy/pharmacies/{pharmacy}/outlets/{outlet}`:

- `PUT/GET /trading-scopes/{id}` prepares or reads a state/category fact proposal.
- `GET /trading-scopes/{id}/proposal-receipt` recovers the original actor's exact
  proposal even after later transitions.
- `PUT/GET /trading-scopes/{id}/commands/{command}` reviews or recovers the exact
  actor-bound decision; `GET /trading-scopes` uses signed keyset pagination.
- `PUT/GET /licences/{id}/revocations/{revocation}` records or recovers an
  attributable licence withdrawal, preserving the historical licence and review.

Commands check the verified tenant and reject a stale expected-tenant header with
`409 session_changed` before parsing the request body. No stock, consent or
licence action runs under a switched workspace.

Scope lifecycle is pending review → approved/rejected, then approved → revoked.
Approval requires a distinct account, a current independently reviewed licence
and available evidence. Input, proposal receipt and command history are retained.
One active category and a consistent current jurisdiction apply per outlet.
These are regulatory-scope facts on the existing outlet, not a second premise
master, operating duty or an automatically certified legal assessment.

Administrative requests require current IAM membership/session authority plus
current private outlet rights. Existing member/credential proofs are checked
through commit; private IAM/product lenses are restored before callbacks.
Shared/exclusive licence barriers serialize action checks against withdrawal;
current checks open a fresh statement after acquiring the barrier. Legacy
licence status preserves historical dates and reflects current withdrawal. Its
bounded 128-candidate lookup fails with unavailability on overflow.

## Bilateral consent

`PUT/GET /v1/supply/relationships/{relationship}` prepares or reads a relationship.
Proposal/command receipt routes preserve original results and actor/intent binding.
`GET /v1/supply/trading-sources/{product}/{organization}/locations/{location}/relationships`
provides a private, scoped, signed-keyset directory. Source IDs filter owner data;
they do not establish access.

Terms select distinct tenant parties, exact source product/organization/location
versions, licence references, scope/binding references, category, INR and bounded
validity. Requests do not switch the caller's tenant. Supplier preparation and
review require current tenant-level administrative capability and actual private
source ownership. Approvals require fresh canonical platform identities and
remote/local source checks. Network checks execute outside database locks and
local authority/state is reacquired before commit. Concurrent retries recover the
original decision; conflicting commands return 409.

Seller review emits an offered exchange; the buyer explicitly accepts under its
own authority; the seller records acceptance and emits activation acknowledgement.
The buyer waits for that acknowledgement. `active` records bilateral consent,
not a permanent source-eligibility or recipient grant. Current authority is
rechecked at every later business/disclosure boundary. Suspension is terminal for
new business; historical records remain available only through their independently
authorized retained-transaction purposes.

Directed subject `helivanta.in.supply.relationship_exchange.v1` carries only
immutable terms, their digest, source epoch, phase and consent signatory. Each
owner stores its own provenance-bound projection/history. The consumer validates
origin/destination, exact terms and monotonic remote epoch. Duplicates, reordering
and late acknowledgements cannot revive a suspended/rejected relationship. A
seller's rejected unpublished draft is not disclosed to the counterparty.

`POST /v1/supply/internal/relationship/check` uses the same workload caller and
fresh nonce protocol as source checks. It returns only the exact relationship's
consent eligibility and local/remote authority epochs, never its terms or staff
directory. `new_business` requires current bilateral consent, matching epochs
and term validity. `retained_transaction` proves previously established consent;
it grants no user access and does not establish any outstanding stock or financial
obligation. Those records require their own owner authorization.

The in-process relationship guard checks canonical organization identities,
current peer consent and both source endpoints before issuing a private,
short-lived proof. Consumers must reacquire local relationship/source authority
through `WithVerifiedRelationship` before committing. The proof is tied to the
issuing owner instance and expires after five seconds; snapshots are copied so a
consumer cannot alter the verified terms. Each proof can be consumed by one
guarded command attempt; retries require fresh verification. Proof expiry constrains the
owner transaction through commit, including time spent waiting for local locks.
Network verification runs before local
transaction locks. Missing peer configuration fails closed. The projected token,
TokenReview RBAC and consumer integrations remain release prerequisites.

## Acceptance still required

This source/consent implementation does not close effective named/group grants,
minimized partner projections, owner stock release/reservation/dispatch, hospital
receiving/discrepancies, financial composition, their UI or the full web journey.
Those consumers must use current authority and retain their own idempotent
command/evidence histories. Source-fact, consent protocol and component tests must
not be reported as full journey acceptance.

After the user's local-runtime restriction, only static build/vet, formatting and
contract generation run locally. Runtime/unit/E2E execution belongs to CI and the
shared pre-GA web environment using dedicated synthetic users and records.

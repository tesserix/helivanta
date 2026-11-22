# Reviewed Staff authority for pharmacy stocktakes — #1686

Status: implementation proposal, not an activated API or release approval.
Depends on the tested capacity change in PR #1817. This document does not close
#1686, grant operating access, or enable Stock/Catalogue.

Development checkpoint: #1817 merged normally at `0189efde`; main CI 45055181362
and signed-image run 45055181387 passed. Migration0064 and its protected
Release/Kargo rollout still await named approval. Draft PR #1818 contains the
unreleased reviewed independent backend. No candidate migration, identity, stock
or release flag has changed live.

The independent/chain candidate implements start, paged line/header/list reads,
counting, submission, review/rejection, cancellation, original command receipts
and the CSV register. Commands retain immutable Staff/link/duty provenance.
Hosted 45072870012 passed core Workforce history, actual Global API regression,
native corruption/stocktake and IAM release checks, plus the legacy capacity
case, but failed actual reviewed start at the five-second gateway deadline.
Core command tests include retired-counter evidence, same-Staff alias refusal,
rejection/cancellation, graph failure and one-time correction conservation.

Focused 45072841622 passed read scope/legacy separation, posted CSV values,
provenance/audit failure nondisclosure and rollback, unchanged physical stock,
retry and cleanup. The later rotated-account header test failed as intended in
45074170257: the same Staff was labeled independent. `597fb19b` derives known
participation from retained Staff/account evidence. Focused 45074844870 passed
that regression together with a distinct independent reviewer and the other
read/failure tests. The label remains a presentation hint; approval still uses
the full Workforce-owned historical independence check.

Performance diagnostics exposed repeated older-purpose checks and a join doing
10,000 batch lookups, each filtering approximately 5,000 rows. Direct reviewed
RLS selection reduced that work, but total start still timed out. A trial
concurrent index was not chosen and was removed; no new stock-batch index or
migration 0066 remains. The final query aggregates physical movements per
selected batch/location before metadata projection, retaining selection,
zero-balance buckets, cutoff, ordering, units, row cap and regulatory source.
The submission guard also defers compilation of completeness SQL until submit,
when observations exist. Historical migration 0064 remains unchanged.

Diagnostic 45074658896 then passed both capacity cases. The actual reviewed
Global API workflow measured start 4.348 s, page p99 110.18 ms and posting 500
corrections in 1.433 s, with replay and 99,500 retained units. Legacy measurements were
start 2.752 s, page p99 5.71 ms and post 1.343 s. These instrumented measurements are
**not clean acceptance evidence**. All temporary diagnostic hooks/options were
removed in `55e10301`. Clean full Stocktake run 45075494219 passed build/vet,
contracts, Workforce history, Global API/native regression, corruption/rollback,
IAM admission and both capacity gates. Actual reviewed measurements were
start 4.331 s, page p99 108.30 ms and posting 500 corrections in 1.432 s; legacy
measurements were 2.650 s, 7.47 ms and 1.303 s. Both preserved exact replay and
stock conservation at the original 5 s start/post and 300 ms page-p99 limits.
Those measurements apply to `55e10301`; current-head protection/capacity checks
remain required. A subsequent PR run again hit the start deadline. Snapshot
inserts now use at most 2,000 rows, additionally bounded by PostgreSQL's 65,535
bind-parameter limit, to reduce repeated statement-level validation. Every row
and the complete snapshot remain validated in the original atomic transaction.
The unchanged Staff UI source passed tests/lint/types/Shell build in
45069240276. PR #1818 additionally enforces the required protected checks.

Eligibility remains false. Hospital authority, withdrawal continuations,
named choices, operational SDK/UI, full modern reservation/concurrency evidence
and both-model browser/live acceptance remain open in #1686. All new read
routes are still Stocktake-gated and do not yet satisfy withdrawal recovery.
Source implementation and backend tests do not establish full release
acceptance. Candidate IAM 0055/pharmacy 0065 are not live; no new Workforce DDL is
introduced. New registry entries remain unclassified until their release gates
are completed.

## Problem verified against source

The existing stocktake handlers consume `pharmacy.stock.read` /
`pharmacy.stock.correct` through `catalogueHandlers.transaction`, legacy outlet
grants and participant account subjects. Their frozen snapshots, independent
subject checks, linked adjustments and recovery remain authoritative. Hosted
tests of those handlers do not establish reviewed Staff authority.

At the proposal baseline, neither canonical IAM template catalogue declared stocktake duties:
`contracts/staff-access.json` owns hospital assignments and
`contracts/operating-assignments.json` owns independent duties. The branch now declares the independent templates; hospital stocktake templates
remain unfinished. Reviewed hospital employment cannot be repaired by adding legacy inventory roles. An independent
pharmacy establishment must not be fabricated for hospital employees:
`pharmacy/employer_guard.go` rejects hospital-owned pharmacies as independent
employers.

There are reusable integration boundaries, with different purposes:

- `operatingStock` already holds current independent duty and Staff/link checks
  around the existing physical ledger transaction.
- `fulfilment_stock.go` demonstrates an approved hospital outlet/department
  binding, current department revision and current hospital source actor. Its
  fulfilment permission is not stocktake authority.
- `CurrentStaffActorGuard` has restricted governance/posting consumers. A new
  stocktake consumer must not expand that purpose implicitly. Workforce's
  purchase-order actor guard demonstrates a separately declared operational
  eligibility contract; it is not permission to reuse procurement authority.
- `pharmacy_stock` depends on unreleased Catalogue and covers other stock
  operations. Turning it on to test a count would expose unrelated operations.

## Ownership, actors and scope

PharmaConnect remains the only owner of count state, snapshot lines, observation
commands, adjustment links and physical movements. Workforce owns Staff,
employment generations, account-link history and identity eligibility. IAM owns
reviewed assignments and duties. Organization owns department revisions. No new
service, person master, stock balance, private role vocabulary or financial
posting is introduced.

| Actor                      | Authority                               | Exact resource boundary                                                          | Assignment preconditions                                                                  |
| -------------------------- | --------------------------------------- | -------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| Independent/chain counter  | IAM operating duty                      | Verified tenant, independent pharmacy and outlet                                 | Current acknowledged duty, membership, active Staff and reviewed current account link     |
| Independent/chain reviewer | Separate reviewed IAM duty              | Same pharmacy/outlet                                                             | Same current source checks; different reviewed Staff from every participant               |
| Hospital pharmacy counter  | IAM hospital/department assignment      | Verified tenant, hospital, current department and approved bound pharmacy outlet | Current reviewed employment/account generation and purpose-specific Workforce eligibility |
| Hospital pharmacy reviewer | Separate hospital/department assignment | Same bound outlet                                                                | Same current source checks; different reviewed Staff from every participant               |
| Reader                     | Explicit read assignment/duty           | Same exact owner and outlet                                                      | Current read authority; no command authority                                              |

Each assignment uses the existing independent approval and projection process.
Tenant ownership, an HR job category, a department row, posting, licence or
entitlement alone confers none of these duties. Counting is nonclinical physical
stock work; it grants no dispensing, prescribing, controlled-drug designation,
release, financial or statutory filing power. Existing classification and stock
guards remain in force for Maharashtra/Odisha; no new legal rules are invented.

## Contract and consumer design

Introduce code-declared `pharmacy.stocktake.read`, `.count` and `.review`
permissions. Counter templates include read/count; reviewer templates include
read/review. Hospital templates also include a separately declared Workforce
stocktake actor eligibility permission. An independent receiving duty does not
gain count/review rights through an edit to its historical template.

Use new reviewed route families, rather than making legacy routes public or
silently redefining their v1 authorization:

- Independent: `/v1/pharmacy/pharmacies/{pharmacy}/operating-locations/{outlet}/stocktakes`.
- Hospital: `/v1/pharmacy/hospitals/{hospital}/departments/{department}/pharmacies/{pharmacy}/outlets/{outlet}/stocktakes`.

Reuse the existing canonical count, observation, decision and receipt schemas,
UUIDv7 command identities, bounded pagination and ten-second command deadlines.
Route registration through Global API must declare the product destination and
current authorization strategy. No direct browser-to-product route is added.

Read permits list/header/lines/original receipts and audited register export.
Count permits start, record, submit and cancellation. Review permits approve and
reject. Decision-specific authority is resolved before any write; a counter's
general decision payload must never select review authority. Snapshot product
and quarantine choices use bounded owner projections; they do not require or
disclose the broad editor catalogue. Both expected-tenant and expected-subject
preconditions bind SDK requests, query keys and retained recovery to the verified
session. Supplied IDs or headers never select authority.

Use existing count behavior inside one owner transaction, held within current
IAM/Workforce/source guards. Preserve scope and actor evidence through commit;
restore transaction-local purpose settings on exit. Do not fall back to the
legacy transaction if a reviewed guard denies or is unavailable.

## Attributable participants and independent approval

Retain immutable owner evidence for every reviewed start and command: employing
establishment, Staff ID, account subject, exact link and source version/generation,
and the consumed IAM assignment/duty revision and epochs. These are observations
of owner-issued facts, not reusable grants. Reads and file release retain current
authorization and audit requirements.

Bind participant evidence to the existing count/command identities in the same
transaction. Approval compares the reviewer's stable owner-issued Staff identity
with the starter and **all** accepted counting revisions, including superseded
observations. Relinking the same Staff record to another account cannot create an
independent reviewer. Keep the existing account-subject prohibition as an
additional check. Reject missing or inconsistent participant evidence; never
guess a Staff identity for historical legacy commands or backfill from a display
name/email. Historical source-link comparisons belong to Workforce's published
guard, not a Pharmacy join against Workforce tables.

The guarantee is independence of reviewed source identities and retained linkage
history. Two separately entered Staff records are not proof of two real people;
fixture approval and source identity review must establish distinct individuals.
No new automatic person-matching system is proposed.

The history comparison uses the separate callback-only
`authz.IndependentStaffHistoryGuard`, not the current-target duty approval
operation. A completed observation remains attributable after its author account
is retired. Workforce verifies the exact historically active link/version and
unchanged link-to-Staff basis, checks the current reviewer and all historical
account aliases, and audits source use in the same guarded transaction. Retired
accounts gain no access. The consumer keeps current reviewer admission and pages
its own distinct participant references at 200 per query while holding the count
lock; every starter, accepted counter and submitter must pass before review.
Missing provenance refuses with 409 and unavailable source/audit with 503.

The initial positive reviewer red test reached the intended missing decision
(expected 200, actual 404) in run 45063570066 after correcting fixture contracts.
The same-Staff alias scenario uses canonical selection/quarantine input.
Review/rejection, counter cancellation, original decision recovery and exact
correction-only purpose checks passed core native/source and Global API
acceptance in run 45072870012. The current clean regression and remaining release
gates are tracked in the development checkpoint above.

The reviewed correction purpose also reads exact-outlet supplier holds for the
existing ledger quantity guard. It cannot insert, update, delete or release those
holds. This preserves commitments without broad supplier allocation access.

## Database and release boundary

Add forward migrations after the actually merged migration registry. Keep prior
migration bytes intact. Extend forced-RLS policies only for a dedicated,
IAM-admitted stocktake purpose, exact tenant/pharmacy/outlet and verified actor.
Hospital admission additionally holds the approved binding and current department
revision. Receipt/fulfilment purpose settings must not admit stocktake writes.

Permit physical `correction` writes only when linked to the same independently
approved count/command/line through the existing movement evidence guard. Do not
grant general movement insertion, receipts, release, catalogue/location editing,
or removal of supplier reservations. Preserve migration 0064's set validation,
per-line audit cardinality, immutable history, deferred completeness and bounds.

Introduce an isolated `pharmacy_stocktake` release capability for the new routes,
named working-outlet discovery/projections and page. Keep it compiled ineligible
until its own acceptance is complete. Do not change broad Stock/Catalogue
eligibility. Keep retained status, exact original-result recovery and
cancellation available through explicitly classified continuations, subject to
current authority, without exposing start/count/review when withdrawn. Recovery
markers retain references only, survive reload and cannot cross account/scope.

The UI reuses named account presentation, stocktake components and strict SDK
receipts. It offers only reviewed named outlet choices. No raw identifier entry,
duplicate stock UI or fallback to an editor endpoint. A withdrawn capability,
changed account or failed current read removes private cached evidence/actions.

## Bounds, failures and delivery

Retain ADR-0073's planning numbers: 10,000 buckets/count, 100 observations/command,
500 differences/approval, 64 KiB commands, pages at most 200, five-second start/
post and page p99 below 300 ms. Plan for 10 count commands/s, read:write 10:1,
approximately 4 KiB commands/50 KiB pages and 14.4 million snapshot rows/year at
200 outlets. These are planning assumptions, not production measurements. Index
participant evidence by tenant/outlet/count and Staff identity; independence must
not scan every historical command in every outlet. Re-measure the full declared
capacity with the reviewed guards; legacy performance evidence cannot establish
the added guard's budget. No additional infrastructure is proposed; measure
extra source locks, participant/audit rows and query cost in hosted acceptance.

Unknown or foreign owner/outlet/binding returns 404. In-scope missing permission
returns 403 where the registered hospital contract establishes that scope;
independent duties retain the existing nondisclosing 404 convention. Revoked
credentials require reauthentication. Changed expected session returns 409.
Unavailable identity/IAM/graph/source returns 503, with no fallback. Stale versions,
changed command reuse or source revisions conflict before a partial write.
Audit/outbox failure rolls back physical/state changes; unaudited private reads
and export bytes are withheld. Unknown transport outcomes recover the original
actor-bound command before any new command is issued.

No live migration or rollout follows from this proposal. Capture affected
before-state definitions, obtain named migration/rollout approval, and release
signed main images through the protected release/Kargo flow. Older legacy code
must tolerate the additive schema. Restoration uses a reviewed forward migration;
reverting application code never deletes count or participant history.

## Required executable acceptance

1. Red-first tests through Global API with actual IAM duties/assignments,
   Workforce identities, graph acknowledgement and the real product database;
   fake authorizers alone are insufficient. Both ownership models use the same
   physical ledger, including actual quarantined receipt stock.
2. Named outlet selection, start/all and selected snapshots, every explicit
   count, submit, independent approve, exact replay and verified register export
   through the real SDK and browser UI.
3. Counter cannot review; reviewer cannot count. Starter, every historical
   counter and a relinked alias of the same Staff cannot approve. An unresolved
   legacy participant cannot be accepted as independent Staff evidence.
4. Foreign tenant, sibling outlet/department, hospital-owned-as-independent,
   unapproved/replaced binding, stale department/source revisions, current actor Staff/link or
   employment changes, duty revocation/expiry, membership interruption and graph
   or audit failures all refuse without partial rows/movements/disclosure.
5. PostgreSQL statements prove exact purpose/scope isolation and rollback,
   including attempted general corrections, receipt/release and inherited grants.
   Architecture tests restrict who can establish the stocktake purpose settings.
6. Capability off hides the operational UI and denies commands through Global
   API while exact recovery/cancellation remains authorized and usable. Test
   withdrawal after a lost committed response and after page reload.
7. Preserve existing reservation, controlled classification, unknown/zero,
   concurrent approval, limits and corruption regressions. Re-run uninstrumented
   maximum-capacity acceptance under the reviewed path.
8. Focused hosted Linux build/vet/race tests and canonical generation/check,
   required PR checks, normal merge, approved Kargo rollout and actual named
   hospital/independent live synthetic acceptance precede issue closure. No local
   app, database, runtime or Colima is required.

## Alternatives rejected

- Legacy inventory roles: bypass reviewed employment and scoped duty admission.
- Duplicate independent employment for hospital staff: contradicts ownership and
  employer guard semantics.
- Reuse fulfilment/procurement/governance permission: confuses separately bounded
  purposes and would expand a restricted source contract.
- Enable broad Stock/Catalogue: exposes unaccepted operations beyond this issue.
- Parallel stock ledger or copied Workforce tables: duplicates a canonical owner.
- Subject-only review: account rotation can change a subject without changing the
  reviewed Staff identity.

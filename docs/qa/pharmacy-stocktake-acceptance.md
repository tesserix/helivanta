# Pharmacy stocktake acceptance — #1686

This is an execution guide, not a completed sign-off. Record the deployed build,
actor roles, outlet and observed result for every applicable case. PharmaConnect
owns the stocktake and append-only stock ledger. These checks do not certify a
statutory register or authorise dispensing, disposal, tax or financial postings.

## Prerequisites

- Use an explicitly authorised synthetic outlet/stock fixture through normal
  product APIs. Do not edit database balances, invent licence evidence or use real
  medicines, patient data or payments for these checks.
- Verify the active tenant, hospital/pharmacy and outlet by their displayed names.
  Use separate actual staff members: a starter/counter and an independent reviewer
  who neither started the count nor entered any count revision. Two aliases for
  the same person do not establish independent review.
- Confirm current tenant membership, `pharmacy.stock.read`, and an outlet grant.
  Starting, counting and reviewing also require `pharmacy.stock.correct` and
  current editor authority. A tenant-wide role alone is not an outlet grant;
  reviewed Staff/employment or professional authority must not be inferred from a
  legacy grant or a role title.
- Check the current release snapshot before testing. The operational Stock page,
  snapshot lines, counts, posting and register download are Stock-gated; catalogue
  selection also depends on current catalogue read authority. Retained status,
  original-result recovery and cancellation have their narrower continuation
  routes. Do not enable broader retail capabilities solely to bypass acceptance
  prerequisites. Any live activation needs the existing owner/reviewer and
  release process in [the feature-flag runbook](../runbooks/feature-flags.md).
- Repeat applicable positive and denial cases for hospital-owned and independent
  pharmacies; include chain outlets in hosted acceptance. India scope is
  Maharashtra/Odisha as applicable to the actual fixture. Country/state selection
  does not create a controlled-drug policy or licence.

## Main workflow

1. Open Pharmacy → Stock → Stocktakes in the intended named outlet. Start an
   **all-products** count with a reason and a named quarantine destination. Record
   its human stocktake number, scope and snapshot time. Verify another active
   count cannot be started for that outlet.
2. Record each product/lot/location bucket's frozen book units and unit of issue.
   Do not treat an uncounted bucket as zero. Enter an explicit zero for one
   synthetic bucket, a shortage for another and an unchanged count where the
   fixture permits. Invalid, fractional or negative physical counts must fail.
3. A later authorised stock movement must not rewrite the frozen snapshot. If
   movement occurs before a count entry is accepted, stale entry evidence must
   require refresh/re-entry, including when movements net to zero. A movement
   after an accepted count must remain in the live ledger after approval.
4. Submit only after every captured bucket has an explicit count. The starter and
   anyone who recorded a count revision must be unable to approve. Rejecting or
   cancelling preserves history; neither posts differences.
5. Have the independent staff reviewer inspect and approve the submitted revision
   with a reason. Positive differences go to quarantine. Where extra units were
   observed on a sellable shelf, the required physical-move attestation must be
   truthful for the approved fixture. Approval must never make found units
   sellable or create a payment/credit/GL posting.
6. Verify all nonzero adjustments post atomically, with the stocktake/command/line
   evidence and original reason. A shortage that would consume a supplier
   reservation must fail without partial movements or partial posted state.
   Replaying a confirmed command must return its original result without posting
   another adjustment. Concurrent or stale approval must not post twice.
7. Download the verified stocktake register. Check the complete snapshot, including
   explicit zero and uncounted distinctions, actor references, count revisions,
   stocktake number/state and linked correction movements. Compare the differences
   with the ordinary stock register's correction totals; preserve all later
   movements when reconciling closing stock. The UI uses names/human references;
   internal correlation references in audited exports are not an invitation to
   enter raw identifiers in forms.
8. In a separate count after termination, select specific named products and prove
   other products are outside that frozen snapshot. Repeat the count/review/export
   checks for that scope.

## Controlled classification and failures

| Case                                                                 | Required result                                                                                                                                                                                                                                                                    |
| -------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| H1, X, NDPS and unknown classifications                              | Separate discrepancy groups and source declarations in the count/register; unknown remains explicit. No automatic regulatory filing, release or certificate. Use approved synthetic classification fixtures only.                                                                  |
| Changed tenant/actor, foreign outlet or lost read authority          | Server denies the request; previously loaded header, buckets, classified differences and export/actions disappear. No raw subject IDs replace names.                                                                                                                               |
| Header or bucket read unavailable                                    | Hide cached private details, including retained status and failed quarantine choices, pause actions and offer the appropriate retry. Refusal takes precedence over a concurrent outage; 401 directs the user to sign in rather than treating it as a retryable permission failure. |
| Correction authority removed while read remains valid                | Preserve the open physical entry for the authorised reader, but pause saving/review; permission restoration alone must not submit it.                                                                                                                                              |
| Failed read followed by restored authority                           | Recheck both current header and buckets before details/actions return. No success is fabricated from cached rows.                                                                                                                                                                  |
| Response lost after count/decision commit                            | Recover the original actor-bound command reference through retained work, including after navigation/reload or Stock withdrawal. Confirm the original result before retry; no fresh command identity or duplicate movement.                                                        |
| Invalid/duplicate/changed command, stale version or oversize request | Explicit refusal; no partial count/post/export.                                                                                                                                                                                                                                    |
| Audit, outbox or database failure                                    | No partial posted correction. Sensitive reads/file release fail closed without releasing unaudited rows/bytes.                                                                                                                                                                     |

Dependency failure, response-loss and concurrency injection belong in the approved
hosted test harness. Do not disrupt the shared live service to manufacture them.

## Evidence and closure

Record build SHA, ownership model, outlet/count human reference, actor roles,
case, expected/observed outcome and a redacted screenshot or permitted audit
reference. Never include passwords, codes, cookies, tokens or private CSV bodies
in an issue. Mark unexecuted cases **Pending**, not Pass.

Relevant executable evidence:

- `.github/workflows/stocktake-capacity.yml`: isolated hosted Linux measurement
  of the ADR-0073 limits. `TestStocktakeCapacityAt10000Buckets` creates 10,000
  distinct batches and receipts through the product APIs, walks every snapshot
  page without duplicate buckets, records 100 bounded count commands and posts
  500 differences with a separate reviewer account. It asserts page p99 below
  300 ms and start/post below five seconds, then verifies exact approval replay.
  It also requires exactly 10,000 attributable snapshot-line audit events.
  Snapshot guard regressions cover malformed mixed statements, missing audit
  actors, two granted outlets, unfiltered scoped reads, cross-outlet writes,
  readers and grant downgrades. Native fixture transactions supply the same
  tenant/pharmacy/outlet context as the real handler; missing pharmacy scope is
  separately required to fail closed.
  Run it with `HELIVANTA_STOCKTAKE_CAPACITY=1`; ordinary coverage deliberately
  does not run timing assertions alongside unrelated packages. The fixture uses
  one product and one quarantine location with legacy outlet grants. This is a
  maximum-bucket measurement, not proof of reviewed Staff authority, a diverse
  product catalogue, ten simultaneous outlets or production network latency.
- `backend/internal/modules/pharmacy/stocktake_test.go`: frozen scope, versioned
  counts, both-owner posting, independent review, later movements and rollback.
- `backend/internal/globalapi/stocktake_test.go`: Global API isolation, register
  evidence and supplier reservation conservation.
- `backend/internal/modules/pharmacy/stocktake_csv_test.go`: controlled grouping,
  zeros, corrections and bounded/private export behavior.
- `apps/shell/components/pharmacy/stocktake-detail.test.tsx`: denied/unavailable
  refreshed reads hide cached evidence; recovery and independent read/edit access.
- `e2e/tests/pharmacy-stocktake.spec.ts`: real hosted count → lost-response recovery
  → independent review → verified export, with injected read denial/recheck.

Use [ADR-0073](../adr/0073-pharmacy-stocktake.md) and the
[API contract](../api/domains/pharmacy.md) against current source.
Passing hosted tests or a healthy deployment alone does not close #1686: record the
applicable authenticated live acceptance and release review explicitly.

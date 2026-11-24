# Delivery plan — low-stock activation #1685

## Implemented and verified

1. Dedicated `pharmacy_low_stock` release boundary, dependent on Setup, with
   canonical route/page/panel classifications. Broad Stock also depends on Low
   stock so its old overview cannot bypass withdrawal.
2. Scoped owner projections for bounded product choices, exact product reload,
   minimum revision/receipt recovery, warnings and exact overview count. Successful
   disclosures require committed audit evidence; unavailable authority fails closed.
3. Generated Go/Zod contracts and validated SDK, named Low stock workspace,
   reader/editor controls, stale-data hiding and uncertain-save recovery.
4. Forward migration `0071_pharmacy_low_stock_scope_read`: statement-local existing
   grant evaluation, centralized canonical lot/bucket projections and guarded
   hospital source-seal update. Existing writes, purpose policies, forced RLS,
   signatures, stock arithmetic and runtime restrictions remain enforced.
5. Measured query correction: materialize canonical outlet totals once and read
   the numeric JSONB lookup through scalar InitPlans, avoiding repeated large
   tuple scans under generic plans. No persistent count/stock cache or planner
   override is introduced.

Hosted run [45405102721](https://github.com/tesserix/helivanta/actions/runs/45405102721)
passed owner, real Global API, built-browser and capacity on `7c871c33`:

| Ownership      | Complete pages |   Page p99 | Exact overview reads | Overview p99 |
| -------------- | -------------: | ---------: | -------------------: | -----------: |
| Independent    |            200 | 365.755 ms |                  100 |   339.204 ms |
| Hospital-owned |            200 | 352.775 ms |                  100 |   337.058 ms |

Each outlet contained 10,000 configured products, 1,000 released buckets and
9,000 zero-batch products created through owner APIs. Product identity/quantity,
complete pagination and exact counts passed. Actual-role custom/generic plans
also remained below the unchanged 500 ms budget. The complete affected pharmacy
package passed with `-race` in 308.851 seconds, including original hospital
stocktake integrity/tamper and bulk/legacy rule parity.

Earlier measured failures drove these optimizations: original page/count p99
2.997/3.223 seconds, then 933/942 ms pages, then a 534 ms generic-plan page.
No acceptance target was raised. Migration tests cover predecessor/new visibility,
unchanged writes/purposes, unrelated metadata drift refusal and rollback atomicity.
Built-browser tests cover original receipt recovery after a lost committed
response, reader write denial, revoked-grant stale-data removal, disable/count
refresh, exactly two attributable revisions and no physical stock movements.

Local evidence before eligibility: full API 2,012, shell 2,378 and shared UI 333
tests passed, plus focused checks, Go vet/lint, generators, contractdocs/source
review and affected production builds. Generated build artifacts were removed.
No local app, database, container or Colima ran. The new provider-controlled
eligibility regression was observed red before regeneration and green after it;
the matching chart test likewise failed before catalogue sync and passed after.

## Completed delivery

1. Final eligibility/generated outputs, canonical catalogue/schema/reconciler
   checks and affected local API/shell builds passed; generated outputs from the
   builds were removed. No local applications, databases or Colima ran.
2. All protected checks passed on `03f13955`, including three Go race/coverage
   shards and their combined gate, full web checks/builds and full E2E. The final
   [low-stock acceptance run](https://github.com/tesserix/helivanta/actions/runs/45409622996)
   passed for both owners. Stocktake capacity and compound supplier/hospital Staff
   admission regressions also passed. Application PR #1842 merged normally as
   `95285c5b`; no admin bypass was used.
3. Main image publication built, scanned and signed both images. Policy/role/RLS
   and source-integrity metadata was captured before release. Explicit approval
   named migration 0071, the signed revision, catalogue sync and flag activation.
   The normal reviewer-gated
   [Release/Kargo flow](https://github.com/tesserix/helivanta/actions/runs/45497399513)
   completed, with migration 0071 applied at 22:11:08 UTC on 2026-11-24.
4. All seven application deployments reached 2/2 readiness on the approved
   digests, and the new Kargo Freight became Ready/Healthy/Verified. The live
   before/after comparison retained 28 other policies and every unaffected field
   in the five changed policies; forced RLS and purpose/source integrity passed.
5. Catalogue PR tesserix/tesserix-k8s#1723 merged normally as `969f3055` and the
   chart ConfigMap synchronized. The 22:20 scheduled job created one disabled
   definition and preserved 24. Console confirmed activation of only Low stock
   at 22:21; the 22:30 scheduled job preserved all 25, with no new definitions.
   Console afterward confirmed nine enabled flags and 16 release-locked flags.
6. The signed-in hospital owner exercised the live UI/Global API against one
   labelled non-clinical QA fixture in an already-authorized outlet: minimum
   `0` gave available `0`, one warning and count `1`; editing to `1` persisted
   through reload; disabling removed the warning and returned the count to `0`,
   also after navigation/reload. Backend evidence confirmed three append-only,
   owner-attributed revisions/audit rows and no stock batches/movements. An
   ungranted outlet and unreleased Catalogue remained inaccessible. Account and
   scope labels showed names.

See the [acceptance record](../specs/2026-11-24-low-stock-activation.md#production-acceptance--2026-11-24-utc)
for evidence boundaries and fixture provenance. The QA fixture was created by
an audited, scoped maintenance transaction because both QA outlets were empty;
this does not prove live Catalogue product creation. Its warning is left disabled.
Both-owner denial/recovery and stock-safety cases are proved by hosted acceptance,
not by a second live tenant login. Staff onboarding, clinical authority and
ordering (#1144) remain outside this issue. Issue #1685 closes only after this
deployed evidence is recorded.

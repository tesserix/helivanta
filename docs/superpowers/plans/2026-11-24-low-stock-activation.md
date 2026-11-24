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

| Ownership | Complete pages | Page p99 | Exact overview reads | Overview p99 |
| --- | ---: | ---: | ---: | ---: |
| Independent | 200 | 365.755 ms | 100 | 339.204 ms |
| Hospital-owned | 200 | 352.775 ms | 100 | 337.058 ms |

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

## Remaining delivery gates

1. Validate final eligibility/generated outputs and matching chart catalogue,
   affected local builds/checks, and remove generated build artifacts.
2. Push the stable final head and pass all required protected checks; merge
   application PR #1842 normally. Keep the GitOps catalogue PR #1723 aligned.
3. Publish signed main images. Capture the five live policies, roles, forced-RLS
   and purpose/source integrity before release. Obtain explicit approval naming
   migration 0071 (including function delegation/source-seal update) and the
   signed release revision; run the normal reviewer-gated Release/Kargo flow.
4. Verify migration completion, exact signed workload digests and readiness.
   Merge/sync the matching chart catalogue in the release sequence. Enable only
   Low stock through audited Console controls and verify a later scheduled sync
   preserves the acknowledged desired value.
5. Exercise the actual signed-in owner UI and Global API using authorized
   synthetic existing products/outlets, including denial and recovery checks.
   Record actual running revision and unexecuted cases explicitly.
6. Update and close #1685 only after deployed acceptance. Automatic ordering stays
   in #1144. Hosted fixtures do not prove live onboarding or Staff sign-in.

# Staff employment draft and evidence release — 2026-10-30

Source `22b6241ed0ece7180d713b77145104a63d95d343`,
[PR #1458](https://github.com/tesserix/helivanta/pull/1458), includes draft
preparation [PR #1457](https://github.com/tesserix/helivanta/pull/1457).
[Published release](https://github.com/tesserix/helivanta/releases/tag/2026-10-30-staff-employment-evidence).

The user approved this source and migrations
`0050_workforce_employment_period_drafts` and
`0051_workforce_employment_evidence`. Kargo freight
`dcd330ac6aaed2a3106cf600f2146aa61d777355`, promotion
`prod.01m63tbd92xn9gt1ntzzmjb6er.dcd330a`, succeeded through the existing GitOps
pipeline. The target was project `tesseracthub-480811`, context
`gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`, namespace `helivanta`;
Kargo stage `prod` is in `kargo-helivanta`.

Both migration IDs are recorded. The two employment draft/history tables and two
evidence binding/history tables have enabled and forced RLS. All six application
workloads have two updated, ready and available replicas on the source tag.
API, Global API and shell are Argo Synced/Healthy. PostgreSQL had two healthy
instances, primary `helivanta-postgres-2`, and latest successful backup
`2026-10-29T12:00:24Z`. Promotion completed at `2026-10-29T12:33:56Z`; subsequent
UI verification crossed midnight in Australia/Melbourne.

No resource, replica, secret configuration or infrastructure configuration changed
for this application rollout. It reuses the existing API, workers, PostgreSQL and
private file/scanning owner; additional cost is retained private rows and file
bytes, not another runtime service. Application rollback retains the additive
private tables/history; any future rollback or retention migration needs its own
specific approval.

[PR CI](https://github.com/tesserix/helivanta/actions/runs/40868299072),
[main CI](https://github.com/tesserix/helivanta/actions/runs/40872297255) and
[image publication](https://github.com/tesserix/helivanta/actions/runs/40872296822)
passed. Merge and tested PR head have identical Git trees. Local verification
included Workforce race/coverage (83.6%), architecture/contracts, vet/build/lint
and all 30 web tasks (1,195 shell, 408 API tests and shared packages).

Production owner UI verified ongoing draft save, real upload/scanning, exact
revision binding, withdrawal/history and full reload. The synthetic Staff stayed
inactive. Two downloads were authorized and audited; automation did not capture a
newly saved production browser file, so artifact capture is explicitly unconfirmed.
The earlier local downloaded-file SHA-256 verification passed. A user-approved
backend credential rotation restored fresh synthetic reviewer login; Staff and
payroll access remained denied without expanding its authority. Details are in
the [acceptance ledger](../qa/staff-management-acceptance.md#employment-draftevidence-production-rollout--2026-10-30).

These are private preparation/evidence foundations. They do not approve
employment, confer clinical/login authority, enforce operational cutoff, execute
payroll, or issue cards/letters. The [Staff tracker](../qa/staff-management-progress.md)
and [issue map](../qa/staff-management-open-issues.md) retain all remaining work.

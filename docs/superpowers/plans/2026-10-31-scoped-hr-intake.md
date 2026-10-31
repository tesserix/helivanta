# Scoped HR intake implementation plan

Follow the [design](../specs/2026-10-31-scoped-hr-intake.md), the 33-route inventory
and the full A01–A12 plan. This is not an Access/RBAC completion checklist on its
own. No new production role/migration is approved by local test execution.

- Append the exact two-permission intake template in `0020_iam_hr_intake_templates`.
  Preserve historical definitions/0014 SQL, pin generated SQL/digests and register
  it. Negative bundles, duplicate/unknown catalog entries and old snapshot growth
  must fail before any output. Default public discovery stays at three templates;
  explicit `hr-intake-v1` selects four and preserves old consumers.
- Add purpose-bound RLS in `0053_workforce_hr_intake`, current original-credential
  checks and one/two-grant command guards. Require the original account/tenant for
  `hr_intake=v1`; keep legacy admission without that projection. Maintain lock
  order and current actor checks without creating hospital-manager rows.
- Prove real IAM/SQL/FGA/signed Global API inactive new-hire preparation without a
  target login, global business permissions or a manager grant. Test active-state
  bypass, workspace omission/change, foreign scope, duplicates and actor cutoff.
  Mutation removing the status guard must fail. Do not change `enrollmentScope`
  globally or grant credential/leave/HR-master maintenance authority.
- Wire employment source/draft/read/history/withdrawal and all six employment
  evidence routes. Hold both permissions for evidence; retain exact command
  retries, immutable revisions, private history and lifecycle projection rules.
- Wire all ten Staff-file routes and every shared storage phase. Exercise actual
  GCS and ClamAV protocol clients, the real separate-role worker processor and
  metadata owner; distinguish protocol fixtures from live scanner/UI acceptance.
  Verify original/scoped and benefit/tax non-regression, dependency failures,
  permission/link loss, source epoch changes and role replacement during I/O.
- Pin each request's original assignment/actor evidence across owner phases;
  never borrow a new assignment or link. Prove denial/no bytes/filename/success
  audit while a fresh request can use the independently approved replacement.
  Include quarantine/binding/scan receipts and source-lock observations.
- Implement bounded current-scope discovery, directory reads/editing and read-only
  master/record choices so the ordinary assigned user can reach the workflow.
  Keep master changes, credential decisions, reminders and leave consumers out of
  this template. Correct error handling so known cutoff clears private UI data.
- Generate/publish canonical capability and scope-projection contracts; integrate
  SDK selectors and Staff Access's intake family, directory, profile/terms/forms
  and files. Clear private state on account/access changes; preserve required
  fields, versioned references, uncertain commands and original context.
- Run complete affected suites and generators/checks, contract docs normal/check,
  vet/build/lint, all web gates and exact-head CI. Perform normal local Admin
  assignment/independent reviewer/intake-user UI and stale-cookie acceptance.
  Update the tracker/ledger/source inventory; commit/push the complete consumer
  work together and keep draft until the broader release gates are satisfied.
- Continue current-person/employment submission/review, legacy cohort review,
  governance recovery, renewal, suspension/resumption, resignation, termination,
  contract expiry, rehire and all operational consumers. Verify each A01–A12
  requirement before requesting exact production rollout/migration approval.

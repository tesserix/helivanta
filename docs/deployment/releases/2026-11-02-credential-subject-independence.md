# Credential subject independence — verified production rollout

Revision: `6e9c4816d18a16dd63d8fbf0d06caa1ab1e62646`, merge of
[PR #1522](https://github.com/tesserix/helivanta/pull/1522).
The user explicitly approved this revision and both named migrations after
preflight. This release does not complete Staff Management or Pharma duties.

## Source and validation

PR head `95de52b3` passed Go/coverage, web, E2E, scripts, secret/security and image
checks in CI run 41431224601. Its Git tree is identical to the merge revision.
Main image workflow 41435550875 passed build, scan/sign/publish and advancement
of the deploy branch. The repository remains public.

Local real Postgres/OpenFGA and normal browser evidence covers refusal of
approval/rejection by the linked Staff subject, retained associations, actor
binding, concurrent association, normal withdrawal, independent positive review,
protected evidence and retained history. See
[the exact acceptance and limitations](../../qa/2026-11-02-credential-subject-independence-local.md).
These local decisions are not represented as production professional review.

## Exact production action

Target: GCP project `tesseracthub-480811`, context
`gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`, workload namespace
`helivanta`; Kargo namespace/stage `kargo-helivanta`/`prod`.

Freight `e8a64043d33ddaaf09048b10bfff1d7a3ebe634b` references the exact merge.
The current promotion template matched the previous approved template and its
three Argo update steps. Existing authorized-stage annotations were verified.
Promotion `prod.01m6dzzcxs2g6ddex1gjfcxafw.e8a6404` succeeded through that
configured template. No imperative Deployment patch or dirty GitOps checkout
change was used.

Preflight compared every generated migration definition against the prior
production revision `058ea37a`: all 211 historical definitions were unchanged.
Read-only production receipt comparison found exactly these two pending:

- `0071_workforce_credential_subject_independence` — owner subject, actor and
  serialization enforcement.
- `0072_workforce_credential_subject_index` — concurrent covering index on
  retained login associations.

Migration job `helivanta-api-migrate` succeeded using the merge image. Read-only
verification found both receipts, the subject guard function, and a ready/valid
`workforce_login_links_credential_subject` index. All three installed function
bodies matched the canonical migration byte-for-byte; both enforcement triggers
were enabled. API, Global API, shell and
document worker were each 2 ready/2 updated at `6e9c4816`. Their Argo applications
were Synced/Healthy. No other migration or grant expansion was approved/applied
by this action.

## Production browser smoke acceptance

The existing owner signed in normally using the authorized private JSON file,
then selected the intended existing synthetic tenant through the normal Tenant
control. The Staff directory retained the edited inactive synthetic pharmacist.
Its credential panel loaded, reported no enrolled credentials and explicitly
required a configured hospital timezone for in-date status; it did not fabricate
professional eligibility.

After owner sign-out, the existing restricted reviewer signed in normally.
Direct Pharma Staff source and Organization structure visits were refused
without exposing their data. The reviewer was signed out after the checks.
No membership, hospital or product permission was expanded.

Pharma's read-only shared source showed the same single filtered inactive
canonical Staff record before and after a full browser reload. This release's
production browser smoke created no Staff, credential, evidence, login link,
approval, role, duty or operating grant. The distinct subject/proposer and
independent-review decision matrix remains the locally verified matrix linked
above; this smoke is not a full production four-eyes acceptance.

## Retained evidence and remaining work

Private mode-0600 preflight, approval, before-state and result receipts are in
`~/.helivanta/staff-management-20261027/credential-review-rollout-6e9c4816/`:
`migration-preflight.json`, `approval.json`, `stage-before.json`,
`approved-freight.json`, `approved-promotion.json`, `promotion-receipt.json`,
`migration-job-receipt.json`, `migration-receipts.json`, `health-receipt.json`,
`installed-guard-proof.json`, `production-credential-panel.png`,
`production-pharma-source.png`, `production-reviewer-pharma-denied.png` and
`production-reviewer-organization-denied.png`.
Credentials, tokens and business database dumps are not committed.

Canonical effective postings, standalone employers, Pharma professional
acceptance/PIC/assistant supervision, complete operating lifecycle enforcement
and Staff audit acceptance remain open. The source correction and its migration
receipts must not be counted as those workflows. Schema/history are retained;
any live rollback that removes this protection needs its own approval.

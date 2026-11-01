# Person-review browser acceptance — 2026-11-01

This checkpoint covers the existing person-finding workspace, not employment
approval, operational activation or complete Staff Management. Related issues
[#1035](https://github.com/tesserix/helivanta/issues/1035) and
[#417](https://github.com/tesserix/helivanta/issues/417) remain open.

Subsequently, the user-approved `b6ba32e5` validation-guidance revision was deployed
and verified. The [production release evidence](../deployment/releases/2026-11-01-person-review-validation.md)
separates actual production smoke/denial checks from the local positive journeys
below; it does not promote them to production acceptance or activate employment.

## Environment and authority

Normal Chrome UI at `http://helivanta.localhost:4399`, through the Global API,
with current release owner/gateway/document-worker binaries and local migrations
through 193. The database archive was verified with `pg_restore --list` before
the local upgrade; this was not a restore rehearsal. No production records,
grants, migrations or workloads changed during these browser checks.

Staff Admin created a fresh inactive synthetic applicant and uploaded a harmless
179-byte PNG through the directory. A different actor prepared the person finding
under `workforce.identity_manager.v1`; a third actor reviewed it under
`workforce.identity_reviewer.v1`. Each operational actor had an independently
reviewed active Staff/login link and a four-hour hospital-scoped assignment,
approved by a separate authorized approver through the existing owner commands
as an authorized local setup fixture. These assignments confer no general HR,
payroll or private-document authority. Staff Admin alone had no person-review
discovery scope. Business submissions and decisions below used the UI.

Private credentials and raw evidence remain outside Git in the existing private
QA directory, under `reviewer-ui-acceptance-20261101/`. The public repository
contains only this minimized account of the synthetic cases.

## Observed browser results

| Case                                  | Result and evidence                                                                                                                                                                                                                                           |
| ------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Upload with stale scanner definitions | Release correctly refused. Native Homebrew definition validation failed; the definition-age policy was preserved.                                                                                                                                             |
| Actual scanner recovery and UI retry  | An isolated official Linux ClamAV 1.5.4 scanner validated definition 28170; clean-stream and EICAR controls passed. UI Retry scan released the earlier specimen at attempt 2. A fresh applicant's upload subsequently reached Ready normally.                 |
| Required preparation fields           | Blank policy, finding, UTC observation date and expiry blocked submission. Raw schema messages were reproduced, replaced with field-specific instructions and covered by a failing-then-passing regression test.                                              |
| Preparation attestations              | Examination and known-association disclosure were separately required; neither was prechecked.                                                                                                                                                                |
| Preparer submission                   | Inactive applicant and released identity file were selectable. Complete submission produced a pending inbox entry. Opening the preparer's own finding offered withdrawal, without approve/reject controls.                                                    |
| Independent reviewer disclosure       | Reviewer saw the pending inbox and protected finding without preparation controls. Normal Download returned the bound specimen; all 179 bytes and SHA-256 matched the original. Owner authorization/download audits were also verified on the first specimen. |
| Decision validation                   | Missing reason, independence and examination separately blocked approval.                                                                                                                                                                                     |
| Historical-contributor exclusion      | The reviewer had originally created/edited an older applicant. Approval was refused, the pending version remained 1 and private detail cleared. No contributor history was removed to make approval pass.                                                     |
| Independent positive approval         | A fresh applicant created by Staff Admin was submitted by the different preparer and approved by the independent reviewer. Reopening showed approved version 2 and history with separate pending/approved receipts.                                           |
| No implicit activation                | A read-only owner check after approval confirmed the fresh applicant remained inactive, Staff version 1, employment epoch 1, with zero login links.                                                                                                           |
| Original-preparer withdrawal          | The preparer withdrew the earlier pending contributor-denial specimen through the UI, preserving its receipt.                                                                                                                                                 |
| Independent rejection                 | A separate finding was rejected with reason and both human attestations; owner read confirmed rejected version 2.                                                                                                                                             |
| Independent revocation                | Another specimen was approved then revoked with reason. Reopened history retained pending v1, approved v2 and revoked v3; Download was disabled. The original positive finding remained approved.                                                             |

The reviewer also opened the normal Staff directory: it explicitly showed
read-only access with no Add staff, Edit, Employment terms, Documents or Account
link controls. The inactive applicant was still visible as inactive. This checks
the browser boundary; it does not replace server authorization tests.

The first reviewer sign-in after creating the fresh specimen stalled on the local
callback. A new normal sign-in completed; no session was injected into the browser.
The browser download-event relay also timed out on the first Blob download even
though the file had downloaded. Actual file bytes and the owner's successful
disclosure audit established the result; the event timeout is not counted as a
successful relay test.

Private screenshot/receipt references include
`fresh-applicant-scan-ready.png`, `fresh-person-approved-history.png`,
`revoked-finding-preserved-history.png`, `historical-contributor-denial.json`,
`fresh-approval-no-activation.json` and the `ui-ending-*.json` owner reads.

## Automated verification

- `pnpm exec vitest run 'app/(workspace)/workforce/person-reviews/page.test.tsx'`:
  **24 passed**. Earlier red/green logs for the required-field regression are
  retained privately.
- `pnpm test` from `apps/shell`: **164 files, 1,328 tests passed** (292.78s).
- `pnpm run type-check` and `pnpm run lint` from `apps/shell`: passed.
- Affected-file Prettier check and `git diff --check`: passed.

No API, permission, schema, event or migration changed in the validation-message
fix. No new build is claimed here; the change touches client form validation
messages and its regression test, without routing/server/caching changes.

## Remaining acceptance and activation gates

The cases above do not establish every browser race, expiry, role-removal,
source-change, foreign-scope, outage or performance gate. Automated coverage for
some of those cases does not replace normal-browser acceptance. Reviewer UI
acceptance remains partial until the employment-review inbox/history, protected
bytes, target/evidence selection and normal Staff Admin/preparer/reviewer journey
are implemented and verified as well.

Operational activation still lacks the reviewed-terms-to-employment-generation
transition, explicit legacy/governance review and authoritative owner cutoff.
The checked ledger contains **719 registered surfaces, 9 source classifications,
710 unclassified and zero full cutoff acceptances**. The normal inventory check
passes; `node scripts/staff-operational-access-inventory.mjs --check
--require-classified` correctly fails with 710 unclassified surfaces. No enforcement
switch was enabled. Separately wired Temporal/background continuations also need
their own authority and cutoff review.

Suspension/resumption, notice/accepted resignation, termination, contract
expiry/renewal and rehire must preserve history, isolate other hospitals and
require fresh authority where specified by ADR-0049. A directory status or an
approved person finding cannot substitute for those workflows. Whole Staff remains
**10 partial, 10 awaiting audit, 1 research, 0 fully accepted areas**; these counts
are not a percentage of implementation effort.

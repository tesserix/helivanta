# Scoped posting reader and preparer release — 2026-11-04

PR #1532 adds immutable reader and preparer catalogs, the shared Workforce guard for
current reviewed digital Staff eligibility, and the scoped posting workspace. Drafts stay
non-operating: a saved draft grants no access.

## Release identity

- PR: https://github.com/tesserix/helivanta/pull/1532
- Checked head: `0b27e927d2d3a0f2f3d74324ada9f8da270313a2`; tree `d465d104` exactly matches the merge revision.
- CI: https://github.com/tesserix/helivanta/actions/runs/41637096023 — 11 passed, 2 skipped.
- Merge revision: `091f2287f77170c3ead3ecf972246b94822b9d14`.
- Images: https://github.com/tesserix/helivanta/actions/runs/41637095414 — build, scan, sign, publish and deploy-branch advancement passed. `deploy` is at `091f2287`.
- Kargo freight: `a03f45e8204fb727fd107b5f5698261ae05ca0ab` (`waxen-duck`).

### Forward migrations

| Migration                          | Added by |
| ---------------------------------- | -------- |
| `0016_billing`                     | #1527    |
| `0024_iam_staff_posting_templates` | #1532    |
| `0029_pharmacy`                    | #1529    |
| `0031_pharmacy`                    | #1531    |
| `0074_workforce_scoped_postings`   | #1532    |

### Other changes shipped by this freight

Production runs `305294ab`. Promoting this freight also ships #1527 (billing shift,
pickup and handover events), #1529 (NPPA ceiling prices), #1531 (reviewed service
charges), #1534 (counter sale hold), #1535 (prescription register), the authorization
source-cutoff fence and documentation commits.

## Approval and production status

The user approved freight `a03f45e8` and the five migrations above on 2026-11-04.

Read-only preflight on 2026-11-04: Stage `prod` Healthy with last promotion
`prod.01m6eanw1cfa4s587sv6rx21qc.0477a4c` Succeeded, all nine deployments ready at
`305294ab`, and every Helivanta Argo application Synced/Healthy.

**Promoted.** After the maintainer refreshed the Kargo CLI session, Promotion
`prod.01m6gxvcqvx37wcqnavf92aq27.a03f45e` was created at 2026-11-03T14:43:43Z and
Succeeded at 14:44:45Z.

Read-only post-promotion checks:

- Migration hook `helivanta-api-migrate-td545` on `091f2287` Succeeded.
  `schema_migrations` gained exactly `0024_iam_staff_posting_templates`,
  `0029_pharmacy`, `0031_pharmacy`, `0016_billing` and
  `0074_workforce_scoped_postings` (14:44:14–14:44:15Z), and nothing else.
- All nine deployments are ready at full replicas; every application pod runs
  `091f2287f77170c3ead3ecf972246b94822b9d14`.
- Every Helivanta Argo application, including `helivanta-api`, `helivanta-shell`
  and `helivanta-global-api`, is Synced/Healthy.

## Local acceptance

Run against the API, Global API and shell built from tree `d465d104`, with the existing
synthetic owner, approver, reviewer and posting-source accounts. No real person's data
was used.

### Scoped posting

- An approver proposed the reader/preparer assignment and the owner approved it; the assignment became active at version 5.
- The digital posting source sees the hospital as Preparer with 12 Staff targets, gets department choices in the drafts panel, and saved a draft at version 1 with authority unchanged.
- Another hospital's posting targets and department choices return 403; another Staff record's drafts return 404.
- A reviewer with no digital Staff record gets empty posting scopes and 403 on posting targets.

### Leave approval and separation of duties

- HR filed one day (2026-12-02) for SYN-QA-EMPLOYEE-20261027. The leave reviewer saw it in the queue, opened the detail (days counted, no roster clashes) and approved it. The ledger records leave taken −1 day, leaving 8 days.
- **The filer cannot decide.** The owner filed 2026-12-16 for the same employee and tried to approve it: 403, "Someone other than the filer and the linked staff member must decide or revoke this request." The reviewer then approved it (version 2).
- **Nobody decides their own leave.** HR filed 2026-12-09 on the reviewer's own record. The reviewer's queue stayed empty, the review detail returned 404, and both approve and reject returned 403. The request stayed pending at version 1 with only the filing in its history.
- Setup used the normal UI: probation end date 2026-06-30 in the Staff directory editor, then the leave structure assignment on Leave balances. The opening credit (+2 days) went through the correction API because the UI hides **Correct balance** until the first ledger entry ([#1537](https://github.com/tesserix/helivanta/issues/1537)).

## Not covered

- Staff filing their own leave: no staff login link exists yet ([#1132](https://github.com/tesserix/helivanta/issues/1132)).
- Routing approval through the reporting line with escalation ([#1536](https://github.com/tesserix/helivanta/issues/1536)).
- Whole Staff Management remains incomplete. Parent issues #118, #417 and #418 stay open, and no umbrella acceptance claim is made.

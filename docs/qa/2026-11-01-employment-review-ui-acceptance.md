# Employment-review workspace acceptance — 2026-11-01

This candidate connects existing immutable employment submissions and decisions
to normal scoped hospital discovery, a recorded-state inbox, protected detail,
selected-file download, history and HR preparation from the exact employment
terms/evidence screen. It is local acceptance work, not production activation or
completion of Staff Management. Tracks #1035 and #417; neither umbrella issue is
closed by this slice.

The workforce owner retains tenant/hospital/Staff/review scope. A preparer needs
both current scoped HR intake permissions and a reviewed active actor link. An
independent reviewer needs the separate employment-review assignment and the
same actor preconditions. Discovery is bounded to 20 candidates and preserves
empty filtered-page continuation. Every inbox page and finding-selector release
is audited. Finding selection revalidates the exact approved receipt and current
released evidence without returning identity files or granting identity-review
authority. Commands revalidate their own complete source and independence.

## Local browser evidence

Uses the existing synthetic hospital and applicant, separate HR preparer and
reviewer sign-ins, the real local GCS/scanner/Temporal path and the normal Global
API proxy. Staff business records and file bindings below are created through the
UI. The previously authorized local setup fixture separately provisions only
hospital-scoped reviewed test assignments through proposal and independent
approval, with one-hour expiry; it does not insert employment business records.
Credentials, raw receipts, screenshots and synthetic files remain outside Git.

| Workflow                                                                       | Local result                                                                                                        |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------- |
| Reviewer sign-in without HR intake, current hospital discovery and empty inbox | Passed; independent permission required                                                                             |
| Identity-manager-only preparer opens Staff                                     | Correctly denied HR intake until a separate reviewed HR assignment                                                  |
| Preparer opens existing inactive applicant and saves appointment draft         | Passed; persisted revision 1 through normal UI                                                                      |
| Upload employment PNG and await real scanner release                           | Passed; UI shows Ready                                                                                              |
| Attach released file to exact terms revision                                   | Passed; retained binding revision and scanner metadata                                                              |
| Choose approved person finding without copying IDs                             | Passed; current selector returns the exact reviewed basis                                                           |
| Missing policy, reason, evidence and attestations                              | Blocked; browser found raw policy/reason errors, corrected with failing-then-passing UI regression and live recheck |
| Submit the selected package                                                    | Passed; appears as pending in normal employment-review inbox                                                        |
| Preparer opens pending detail                                                  | Passed; withdrawal available, no approval button                                                                    |
| Preparer protected download                                                    | Passed; fresh 179-byte download exactly matches uploaded specimen                                                   |
| Independent reviewer examination, approval and history                         | Passed; UI approval v2 retains submission v1; Staff stays inactive at version/epoch 1 without login links           |
| Original-preparer withdrawal and independent rejection/revocation              | Passed on separate UI-created packages; history retains every submitted and ended receipt                           |
| Access withdrawal and private-data cutoff                                      | Passed; immediate inbox 403, denied history clears private details, and refresh removes hospital discovery          |
| Operational activation and lifecycle cutoff                                    | Open; this review grants neither                                                                                    |

## Validation and deployment boundary

Signed missing-route regressions first returned 404 for discovery, inbox and the
initial selector design. The selector uses a GET with non-sensitive version/epoch
filters so it can reuse the existing bounded filtered-list contract; it never
places identity hashes or private finding text in a URL. SDK missing-method and
navigation regressions failed before implementation. The first discovery test
incorrectly assumed its first candidate page must be visible; it now follows the
opaque cursor through empty filtered pages, preserving server controls.

Global API race passes (**95.779s**), including missing workspace assertions,
failed sensitive-read audit without disclosure, cross-tenant RLS, inbox read
context refusal to mutate a frozen review, and immediate inbox denial after
reviewer assignment withdrawal. API **541 tests / 70 files**, type-check/lint,
Go build/vet and affected Go lint pass. All **15 workspace UI tests** pass,
including owner capabilities, both human attestations, private-data cutoff and
byte-for-byte uncertain command retries. Workforce race (728.357s), architecture race (51.467s) and contractdocs race
(6.716s) pass. The full shell suite passes 1,343 tests across 166 files, followed
by its production build with explicit build-only local API/auth configuration.

Four forward-only migrations are persisted in the workforce registry: two narrow
read-context changes and two concurrent indexes. They preserve explicit write
checks and prior delete restrictions, add no permission or production grant and
were applied only to the backed-up local QA database. Rollback of these
append-only migrations is not automatic: restore/recovery needs a separately
reviewed plan; the application can stop exposing the new reads without removing
retained business history. Small-fixture query checks do not establish load
capacity or production restore readiness.

The registry contains **724 surfaces, 14 source-classified, 710 unclassified** and
**zero runtime cutoff acceptances**. Strict classification completion must still
refuse enforcement activation. Production remains on approved `b6ba32e5`.
Evidence-download PR #1506 is merged and undeployed. This workspace candidate
requires its own CI, exact migration manifest and named rollout approval.

The first live reviewer download received an upstream 503 and exposed no bytes.
An independent current owner request then returned 200. Subsequent browser saves
waited in native Chrome Save dialogs, including a download-event relay timeout;
one local hot reload also overlapped diagnosis. After completing those dialogs,
two fresh 179-byte files exactly matched the uploaded specimen. These failed or
interrupted attempts are retained separately from the verified download results;
no authorization/storage timeout or scanner policy was weakened to make QA pass.

Read-only owner checks after the UI endings verify history sequences
`revoked → approved → pending`, `withdrawn → pending` and `rejected → pending`.
All three ended file-content routes return 409, independently of disabled UI
buttons. The target remains at Staff version/employment epoch 1. The temporary
employment-review assignment is withdrawn; unrelated reviewer assignments and
credentials are unchanged. The HR preparation fixture retains its one-hour expiry.

The full shared UI suite initially caught the new navigation entry reusing an
existing Staff-page icon. Giving employment reviews its distinct terms-document
icon resolves the unchanged test; the complete rerun passes **269 tests / 26
files**, alongside shared UI lint/type-check. The shell production build was
rerun successfully after that icon correction. This is UI acceptance for the
local non-digital review journey; digital-recipient provisioning, production
rollout and operational/lifecycle acceptance remain distinct open gates.

## Merge and rollout boundary

[PR #1507](https://github.com/tesserix/helivanta/pull/1507) passed all required CI
and merged as `7e5b7800b88f60a2579f6bacfd2f8dc23ea571ca`; the merged tree exactly
matches tested head `176d16f4`. Merged image build/scan/sign/publication passed.
Production approval for this revision and all four new migrations is pending;
`b6ba32e5` remains deployed. This supersedes the pre-merge CI requirement above,
without changing the local-versus-production acceptance boundary. A subsequent
source-only session audit has 17 classifications and 707 unclassified surfaces;
zero full runtime cutoff acceptances remain established.

# Enabled MVP acceptance: hospital and independent pharmacy

Execution date: 2026-11-17 UTC. This is an incremental acceptance record, not a
claim that every Staff workflow, regulated pharmacy operation or release gate
has passed. Real contact addresses, native proofs, passwords, MFA secrets,
session material and private screenshots are excluded from this repository.

## Running application and release baseline

The live observations below used deployed Helivanta commit
`eb8d7015e91d3c375c93cfacad0cc111ab166bf6`. API, Global API and Shell each had two
updated, ready replicas on their pinned images before testing. The dedicated
provider published 22 definitions, with exactly these six effective flags:
`onboarding`, `team_accounts`, `staff_core`, `department_teams`, `pharmacy_setup`
and `pharmacy_staff`. All other release definitions remained locked/off.

The hospital owner was already signed in. A new explicitly labelled independent
QA pharmacy tenant was created through public onboarding, using an owned test
mailbox and clearly synthetic Odisha premises. No clinical licence, GST
verification, employment evidence, physical stock or financial document was
fabricated. No local application, database or container was started.

## Observed live results

| Case                                       | Result                                                                                                                                                                                                                                                                                                    |
| ------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Public onboarding and anonymous boundaries | Public capability read returned 200 with onboarding available. Protected account, Staff, governance and pharmacy reads returned 401 without authentication.                                                                                                                                               |
| Independent signup                         | Verification email delivered, native email proof accepted, password setup completed, receipt became `ready`. Normal first sign-in and MFA enrollment succeeded.                                                                                                                                           |
| Independent ownership                      | Authenticated reads returned one independent pharmacy employer and zero hospitals. Initial outlet had the supplied branch name, `IN`, `Asia/Kolkata` and pending regulatory review.                                                                                                                       |
| Native identity ownership                  | Read-only Zitadel checks confirmed the distinct tenant-owned Helivanta-QA-Independent-Pharmacy-20261117 organization. Its owner alone held ORG_OWNER; both verified QA Staff identities had no native organization-management roles.                                                                      |
| Hospital-owned setup                       | The existing hospital owner registered a new hospital-owned QA pharmacy and its first outlet in the existing tenant. The outlet began with pending regulatory review and no outlet access, including for its creator.                                                                                     |
| Hospital Staff directory                   | The signed-in owner saved a clearly labelled QA hospital Staff record with minimal required details. It appeared in the correct hospital directory, preserving the four existing records. Account linking, reviewed employment and narrower access remain separate.                                       |
| Ordinary Staff identity                    | A QA Staff human was created without native administrator roles. Verification and password emails delivered; recipient proofs completed successfully. First application login before tenant membership was refused with 404.                                                                              |
| Membership and sign-in                     | Explicit `staff_member` enrollment returned 202. Retrying the same command retained membership ID/epoch. Staff sign-in then succeeded, with zero business permissions. Employer discovery disclosed no roots, Staff read returned 404, and outlet/membership administration returned 403.                 |
| Inactive Staff source                      | Account-link preparation against an inactive directory record returned 409. Owner activation used the current directory version; no business authority followed automatically.                                                                                                                            |
| Separate account-link review               | A second verified ordinary member received a temporary, exact-pharmacy account-link reviewer grant. Its recorded source was `single_owner_provisional`, not a fabricated independent grant approval. The proposer could not approve its own Staff link (409); the separate reviewer read and approved it. |
| Approval recovery and stale intent         | Same-command replay returned the same link approval result. A new command with the obsolete expected version returned 409.                                                                                                                                                                                |
| Link versus access                         | The active reviewed account link alone still did not permit Staff directory access (404).                                                                                                                                                                                                                 |
| Explicit scoped reader                     | A separately assigned pharmacy HR reader could discover that employer and read its one QA Staff record (200). Updating the record returned 404; outlet operations and membership administration remained forbidden. This is HR read access, not a pharmacist operating duty.                              |
| Temporary reviewer cutoff                  | The owner revoked the temporary reviewer grant through its retained decision command (202). The reviewer's next account-link read returned 403. Staff-link history and approved link were preserved.                                                                                                      |
| Cross-tenant concealment                   | The independent owner could not read the hospital tenant's pharmacy outlets (404) or current hospital Staff intake (404). The compatibility Staff collection returned 200 with zero rows; this is recorded separately from current intake refusal.                                                        |

## Dashboard: Console → provider → app → API → restoration

The dashboard already consumed the effective page flags. The owner initially had
16 MVP shortcuts because several cards share a released capability group.

1. Console disabled `pharmacy_staff` with an attributable acceptance reason.
   Command `881fbcc2-962a-4f67-8389-46f5f894d257` completed and was confirmed.
2. The actual SDK payload had five enabled flags and `pharmacy_staff` off.
3. After a fresh snapshot, the hospital dashboard had 13 shortcuts. The
   PharmaConnect Staff, Pharmacy Staff and Pharmacy relationships cards were
   absent; pharmacy setup and hospital Staff remained available.
4. An already-authorized pharmacy HR reader's directory API returned 404 while
   that flag was off; retained IAM/root access did not bypass release admission.
5. Console restored the original enabled value. Command
   `a5a0dbff-bf90-4c1d-ab8c-c7c49d47a4a1` completed and was confirmed.
6. The provider again had the original six enabled flags. The same Staff reader
   request returned 200, and all 16 owner dashboard shortcuts returned.

No unreleased capability was enabled, and no original flag value was left changed.

## Findings and candidate validation

Live Organization structure still displayed hospital UUIDs on the deployed
revision. A regression reproduced that behavior. The candidate uses the current
account's separately permitted hospital directory for reference, selected context
and accessible table labels. Directory refusal removes cached names; pagination
is bounded and explicit. A safe missing-name label replaces UUID fallback.

Local candidate checks passed: 2,094 Shell tests in 233 files, Shell and E2E
typechecks, E2E lint, Shell lint (zero errors, one existing signup dependency
warning), and the production Next build. Generated build artifacts were removed.
Nine Organization component tests and eleven dashboard release tests cover the
changed display and existing flag behavior. The real owner browser fixture now
also checks names after the Organization/Workforce reference handoff; hosted
execution for that candidate is a separate required gate.

The first candidate image build exposed a test import of a repository-root JSON
file absent from the minimal container context. The test now consumes the same
generated canonical definitions through the API package's test-fixture export.
After this correction, all 1,457 API-package tests, all 2,094 Shell tests, package
typechecks and the local production build passed. The hosted image rebuild
remains required; the local build alone does not prove image packaging.

The earlier documentation-only CI run `44076177242` failed the existing
`TestPreEmploymentPersonReviewPreservesInactiveJoiner/activation_posting_chain_child`
coverage case on unchanged application code, returning `posting_source_unavailable`
at the five-second authority deadline. The candidate limits package-level coverage
parallelism to one because those database fixtures share a runner. It preserves
the full suite, race detector, coverage accounting/floor, production deadlines and
concurrent requests inside each suite. Existing coverage-gate tests and ShellCheck
passed locally. Hosted full coverage must pass before this is considered effective;
this configuration change is not evidence of a production latency repair.

### Hospital recipient setup follow-up

The hospital QA invitation was found in Gmail Trash through a read-only search;
a duplicate email delivery was avoided. Recipient email proof returned
`already_verified`. The retained recipient flow then requested the password phase
and successfully completed setup (`200 complete`). The real owner's recovered
account command observed the verified native account and offered ordinary
membership. Membership submission is pending action-time browser confirmation;
no hospital business permissions or outlet access have been granted by this step.

### Owner reviewer appointment form candidate

Live testing identified the missing native appointment form even though its IAM
API and independent-pharmacy consumer worked. The candidate consumes existing
canonical contracts and named member/source projections. Automated coverage
includes the original epoch/exact scope, uncertain retry identity, refusal,
unreleased choices, Pharmacy Staff disablement and invalid validity. Current
receipt status remains explicit. New UI deployment and real-browser appointment
acceptance are still required; this is not a claim of full enabled-MVP acceptance.

### Further real-browser observations

The independent pharmacy Staff account completed email-based account discovery,
password and its existing authenticator challenge in a separate private Chrome
window. The header used the account and tenant names. Pharmacy Staff displayed
only the one authorized employer record, with no edit/account-management actions.
No browser-session cookie was copied or injected.

Backgrounding both the owner and Staff tabs exposed capability expiry gaps. The
owner page recovered automatically after returning to the foreground, but its
form had been unmounted. A regression test reproduced a 25-second remaining
provider-aged snapshot against the previous 30-second poll. The candidate uses
15-second polling, focus/background refresh and a deduplicated expiry recheck;
healthy, missed-poll recovery and continuing-outage refusal cases pass. Lifetime
and server admission remain unchanged. Post-deploy background/focus testing is
still required.

The independent pharmacy HR reader also saw hospital-oriented dashboard shortcuts
because those pages share coarse Staff permissions. Hospital discovery returned
no roots and hospital data remained concealed in API testing, so this is not
evidence of cross-tenant access. Product/source-specific shortcut discovery still
needs a qualified source design and verification; global release flags alone do
not prove that a user has a hospital posting or employer scope. It is recorded as
an unresolved navigation finding, not silently treated as full MVP acceptance.

## Checks still required before claiming full live acceptance

- Merge and deploy the hospital-name candidate after every required check passes,
  then repeat the live named Organization/department context check.
- Complete the hospital-owned pharmacy's new Staff identity/link, reviewed
  employment, department head/supervisor/member and transfer journeys with
  independent actors and exact scope. The existing owner session and new setup
  records do not prove those operations.
- Complete live person/employment/credential review and source-cutoff cases for
  the enabled Staff workflows; distinguish these from hosted lifecycle fixtures.
- Verify the independent owner and Staff browser journeys and account chooser in
  addition to the successful normal backend sign-ins.
- Preserve the baseline six flags and verify the chart reconciler remains healthy
  after the candidate rollout.

The independent QA Staff account retains only its explicitly tested pharmacy HR
reader access for subsequent testing. Its temporary reviewer assignment is
revoked. Pending regulated outlet review and the 16 locked capability definitions
remain closed. Follow the [operator guide](../runbooks/feature-flags.md) for any
subsequent flag changes.

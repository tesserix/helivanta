# Scoped posting source-writer review

2026-11-03. Candidate review against `11cf4101` plus the different-target fresh-write
race assertions. This is an evidence map, not full Staff or production acceptance.
The [delivery plan](../superpowers/plans/2026-11-03-scoped-staff-posting-preparation.md),
[source authorization proposal](../adr/0052-staff-source-command-authorization.md)
and [Staff tracker](staff-management-progress.md) retain the release gates.

## Facts and owners

IAM owns membership, credentials, scoped assignments, projection epochs and the
consumed employment basis. Workforce owns the canonical actor/target Staff records,
reviewed login links, identity/employment findings and generations, private evidence
and posting drafts. Organization owns the selected structural department revision.
The two consumers are Workforce posting preparation and Organization's bounded
department choices. Neither consumer receives another owner's repository, private
HR findings or a duplicate employee record. Draft preparation grants no operating
assignment, clinical authority or supervisor relationship.

For source use, the actor is a current digitally activated Staff member with a
reviewed subject link and an independently approved hospital reader/preparer
assignment. Administration/proposal and independent review remain separate
authority. Reader and preparer are non-operating hospital scopes; they do not
approve effective postings or provide department clinical access. The selected
target may be a different, inactive Staff member awaiting onboarding.

## Current read and writer graph

| Boundary                             | Current source consumption                                                                                                                                                                                             | Writer or effective cutoff reviewed                                                                                                                                                                                                                   | Evidence and remaining check                                                                                                                                                                                                                                                                                                   |
| ------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| IAM operation/actor grants           | `assignment_command.go` sorts proposal IDs, takes the shared employment gate and credential fence, locks active proposals, membership epoch and scope epoch, and checks exact current receipts again before returning. | `assignment_decisions.go` locks the proposal before withdrawal; the current generated snapshot trigger advances the scope epoch. `assignment_activation.go` and its SQL lock current membership/scope before activation.                              | Real independent approval, preparer withdrawal/reapproval, stale receipt denial and membership cutoff are exercised in the Global API parent. Normal isolated-origin withdrawal now also passes locally.                                                                                                                       |
| Membership                           | `proposalMembershipEpoch` takes a shared lock on the current nonempty membership epoch. A missing or changed epoch denies.                                                                                             | Membership commands update the owner epoch; no asynchronous graph tuple can replace the current database check.                                                                                                                                       | The posting-membership case deliberately retains old graph membership while source admission denies. Enrollment/recovery do not restore historical grants.                                                                                                                                                                     |
| Credential watermark                 | The guarded IAM command takes a subject-specific shared advisory fence, including when no watermark row exists, and checks `revoked_at >= auth_time`.                                                                  | `revocation.go` takes the matching exclusive subject fence.                                                                                                                                                                                           | Real credential revocation denies the old signed session while a fresh session can recover independently authorized retained history. The prior watermark-bypass mutation is recorded in the tracker.                                                                                                                          |
| Employment basis activation          | The source-use command holds IAM's shared employment fence before entering Workforce.                                                                                                                                  | `WithEmploymentActivation` obtains the same fence exclusively before Workforce and uses a callback-bound basis writer. The writer cannot escape or select a different tenant/hospital/actor.                                                          | Activation, membership-bound basis and callback/database/subject misuse have real-fixture coverage. Activation alone grants no login link or assignment.                                                                                                                                                                       |
| Actor Staff and reviewed login link  | `staff_actor_guard.go` enters the Workforce supervision gate, then `lockStaffActorLink` locks active Staff and the exact current link in that order. Historical nil-generation links are refused by this new consumer. | Login-link commands lock Staff before changing the link. Directory Staff changes lock their selected Staff row; protected enrollment writers take the supervision gate before Staff/profile changes.                                                  | Login-link replacement/withdrawal, independent source approval and changed/restored identity are covered. A generic Active Staff snapshot is not sufficient.                                                                                                                                                                   |
| Actor identity and reviewed findings | `loadIdentitySource` holds Staff/profile share locks. `employmentGenerationState` checks the locked identity epoch, exact employment review revision/version and current person basis. The generation is immutable.    | Staff/profile capture triggers advance the identity epoch. Person/employment review commands use current scoped authority, the supervision gate and exact review-head locks. Evidence/classification checks retain their selected versions and locks. | Identity mutation followed by restoration must not resurrect the old generation. Both different-target fresh-write order cases pass without changing the target version/epoch. Bypassing the actor identity-epoch check produces 200 instead of 403 in both cases; exact source was restored before the complete suite passed. |
| Target Staff source                  | Scoped posting admission sets an exact target/purpose after current actor admission. The write retains its Staff version/employment epoch and appends an immutable draft revision/command.                             | Directory/profile writers change the owner version/epoch; scoped posting RLS grants no profile UPDATE or DELETE capability.                                                                                                                           | RLS and source/version conflicts are covered. Both new-write/source-ending orders pass on a different target: command-first retains one immutable revision/audit; source-first retains none. Target version/epoch remain unchanged.                                                                                            |
| Structural department                | `WithCurrentDepartment` holds the shared Organization restructuring gate and selected current active department row through the outer commit. Choices expose only bounded current canonical heads.                     | `putDepartment` and the SQL write guard use the matching exclusive restructuring gate. Immutable revisions preserve the old reference rather than rewrite receipts.                                                                                   | The owner test observes both gate and exact-row locks through the product commit, tests callback/database escape, and refuses retired revisions. Current hospital registration verification is a separate owner fact; this does not certify current facility licensing.                                                        |
| Commit and deadlines                 | The root `tenantdb` command commits successful source-use evidence with the draft, audit and outbox. It is constrained by IAM and Workforce's earliest effective cutoff.                                               | Membership/assignment expiry, person finding validity and fixed appointment end deny without waiting for asynchronous cleanup.                                                                                                                        | Real expiry during a blocked save withholds the receipt and rolls back the save audit. UI same-session expiry removes the dirty editor while preserving independent Reader history.                                                                                                                                            |

The current IAM snapshot function is the generated template implementation;
`assignment_activation.sql` contains an older definition that later migrations
replace. Reviewing that historical body alone would incorrectly describe the
current permission snapshot as leave-only. Original historical migrations remain
unchanged. Contract/source-inventory checks are not substitutes for runtime tests.

## Lock-order interpretation

The guarded consumer enters IAM authority before Workforce actor/target sources
and then Organization structural validation. Posting writes acquire the Workforce
gate before actor rows to avoid an upgrade after taking actor locks. Owner-private
purpose settings are restored before the callback receives opaque source IDs.
The callback remains bound to the same tenant database and guarded root command.

Directory Staff and login-link writers that do not enter the compound source guard
take their Staff row before later Staff/link work; they do not acquire an earlier
IAM owner after holding those rows. Identity/profile capture also updates the
identity-epoch row, so absent/changed profile state cannot become a new valid old
generation by restoring the old text. The different-target fresh-write ordering and mutation checks now have runtime
evidence, described below. This evidence does not complete the remaining browser
or parent departmental lifecycle gates.

## Verified fresh-write ordering and mutation

`activation_posting_new_command_first` and `activation_posting_new_source_first`
use a separate inactive Staff target and a current canonical department. Real HTTP
requests are ordered by observed PostgreSQL blocking. Command-first commits one
revision, receipt and save audit; source-first leaves none. Both retain unchanged
target Staff version/employment epoch. Restoring actor identity text revives
neither retained nor fresh command authority. Successful actor-use audit counts
match only writes committed before cutoff. The complete parent baseline passed
in 329.231s.

A temporary mutation bypassed exactly `basis.Epoch != g.IdentityEpoch`. Both new
cases then returned 200 instead of required 403; source-first created a new
unauthorized synthetic draft for the unchanged target. The negative suite failed
as intended. Original source bytes were restored and their hash verified; no
running application was rebuilt from mutated source. Private evidence is
`posting-actor-epoch-mutation-proof.json` and its retained log.

After restoration, the complete Global API race suite passed in **363.724s**:
22 top-level tests / 130 including subtests, zero fallback forwards. The runner
removed its three owned disposable fixtures. `go vet ./internal/globalapi`,
`golangci-lint run ./internal/globalapi/...` (0 issues), `go build ./...`,
`go run ./cmd/contractdocs -check`, the Staff operational inventory check
(784 registered / 761 unclassified) and whitespace checks passed.

## Acceptance still required

- Reconcile the previously intermittent evidence-admission failure separately;
  an unchanged successful rerun is not a fix.
- Finish the remaining account/foreign-scope normal-browser matrix and release
  gates, preserving the distinction from signed-session HTTP integration tests.
- Keep independent effective posting approval, transfers/supervision, contractor
  and exit/rehire lifecycles, standalone employers and Pharma operating consumers
  as parent obligations. This hospital-scoped draft slice cannot close #429's
  clinical reachability, roster, referral or sensitivity criteria.

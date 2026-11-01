# Employment activation availability and digital acceptance — 2026-11-02

Status: production source activation is enabled and verified; local digital
recipient and deterministic lock acceptance passed. Production enablement shipped in [GitOps PR #1619](https://github.com/tesserix/tesserix-k8s/pull/1619)
on the existing approved `02b1e2b7e5a0fb9fa1fdbc65e449ca322cfda56a` image.
Both running owner pods carry `WORKFORCE_EMPLOYMENT_ACTIVATION_ENABLED=true`,
are ready with zero restarts and match the previously published API digest. Argo
is Synced/Healthy at GitOps commit `58226e30296a5f75514d1bc72f94283da8cb1c7d`.
All six application deployments are 2/2 ready. The production Staff UI reload
preserved its six synthetic records. Migration ledger remains 201; production
employment generation count remains zero. See the
[configuration release record](../deployment/releases/2026-11-02-employment-activation-enablement.md).
This slice adds no migration or production identity, membership, login link or role.
Full Staff Management and cross-product operational lifecycle acceptance remain open.

## Availability decision

The product owner explicitly requested that production activation be switched on
and the remaining Staff work continue. This authorizes availability of the
reviewed-source eligibility/activation commands. It does not classify legacy
employment, migrate existing grants or certify every operational consumer.
Other installations retain the compiled default-off behavior. All existing
server-side source, current-account, independent-review, actor, tenant, hospital,
transactional IAM-basis and generation-bound authorization checks stay enforced.
Activation itself grants no membership, login link, business role or clinical privilege.

## Local digital browser acceptance

Used the normal application at `helivanta.localhost:4399`, Global API 8199 and owner
8399, with the production source binary and a real local ClamAV/Temporal worker.
The applicant is `Synthetic Digital Activation Applicant 20261102`, employee code
`SYN-DIGITAL-20261102`. All data is synthetic; no real employment decision occurred.

Identity-provider account creation and ordinary membership were local setup fixtures.
Temporary preparer/reviewer/activator assignments were independently approved through
IAM. Every business step below used normal UI forms and fresh sign-ins:

1. HR preparer created inactive Staff, fixed-term draft revision 1 and separate
   identity/employment uploads. Both files reached Ready through the scanner.
2. Preparer bound the exact employment file generation to terms revision 1,
   submitted a person finding declaring the new digital account, and selected the
   exact released identity evidence.
3. A separate reviewer downloaded the bound identity evidence, examined it,
   recorded reason/independence and approved the finding. Reopening showed approval.
4. Preparer separately submitted the exact employment terms, approved person finding
   and bound evidence. The separate reviewer downloaded/examined those bytes and
   independently approved the terms; approval persisted on reopen.
5. Preparer explicitly checked activation eligibility, supplied examination and
   reason, and activated. Reopening returned already recorded with no second form.
6. Read-only persistence confirmed Active Staff version 2/employment epoch 2,
   exactly one generation and one IAM employment basis, with zero login links and
   zero assignment bases at activation. A fresh recipient sign-in showed
   **No hospital access yet**.
7. Existing account-link proposer created a fresh link through the UI; a separate
   reviewer approved it to Linked version 2. Linking alone granted no role.
8. A separate access proposer submitted a hospital-only identity-manager assignment,
   expiring within the appointment. Independent hospital verification and approval
   reached Active version 5. The recipient then signed in normally and opened an
   authorized synthetic target in that hospital's person-review workspace.
9. Disabling only this synthetic account in the local IdP invalidated its retained
   browser session: private workspace cleared and access failed closed. Account
   reactivation was local fixture restoration for the remaining test.
10. The original independent reviewer revoked the employment approval through UI.
    While the separately recorded assignment was still Active, a fresh recipient
    sign-in had **no available person-review hospitals**. Payroll setup explicitly
    denied access. Read-only persistence confirmed one generation/basis/link/
    assignment binding; recorded assignment state was still Active at cutoff.
11. Proposer withdrew that temporary recipient assignment through UI (version 6).
    The reviewed account link was revoked through UI (version 3). All five new
    preparer/reviewer/activator fixture assignments were withdrawn through IAM.
    History and synthetic account credentials are retained; no active QA role remains.

Both downloaded files matched all 179 original bytes and SHA-256
`a8cf1db44d3b6cb252b53506ac3daadba919e0bb2c0721901851d335d83e3c64`.
The browser download-event relay timed out despite successful delivery. The saved
artifact names include Chrome collision suffixes; completed bytes were verified on
disk. This is not a demonstrated file-delivery defect. No further downloads were
needed after verification. An earlier temporary reviewer assignment also expired
during the journey; the UI cleared access and fresh bounded fixtures were used.

Private evidence and mode-600 credential JSON live under
`~/.helivanta/staff-management-20261027/activation-readiness-20261102`.
The generation/source history is retained. Source revocation invalidated tested
identity authority; it did not perform a suspension, termination or directory-status
transition. The Staff record remains Active in the directory until a separate
lifecycle command. Do not count this as complete offboarding or roster/payroll cutoff.

## Deterministic signed concurrency acceptance

Extended `TestPreEmploymentPersonReviewPreservesInactiveJoiner` with:

| Case                          | Observed boundary and result                                                                                                                                |
| ----------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Concurrent source consumption | First activation holds the exclusive hospital gate while blocked on supervision; second command waits behind it, then conflicts; one immutable generation   |
| Concurrent exact retries      | Same lock witness; both responses return the same immutable receipt                                                                                         |
| Recipient request first       | Shared recipient gate reaches supervision; activation waits behind it; admitted request completes before activation; later stale authority denies           |
| Activation first              | Activation reaches supervision; recipient waits on the hospital shared gate; post-activation reauthorization denies the stale grant                         |
| Reciprocal denied targets     | Two activators target each other's Staff; exclusive admission serializes before actor/target locks; both deny without deadlock; valid source remains usable |

Tests observe `pg_stat_activity`/`pg_blocking_pids`; no timed launch is treated as
proof that a request reached its lock boundary. Cleanup joins in-flight requests.
Temporarily replacing the exclusive gate with the shared gate makes these boundary
assertions fail. That negative mutation failed cleanly, with no panic/data race,
and was restored before the final full-package run. No weakened product lock is committed.

Recorded commands in the dedicated Linux Colima/Docker environment:

- `go test -race -count=1 -timeout=10m ./internal/globalapi` — pass, 113.627s.
- `go build ./internal/globalapi` and `go vet ./internal/globalapi` — pass.
- `golangci-lint run ./internal/globalapi/...` — pass, zero issues.
- GitOps production render regression failed before adding the switch, then passed.
  The switch reaches only the owner API; promoted image and mesh injection persist.
- 29 related Helm/Global-boundary/document pytest checks pass; Helm lint and changed
  Python Ruff format/lint pass. Production chart default-off test also passes.

## Remaining acceptance

Digital source preparation, independent approvals, explicit activation, fresh
login/assignment admission and the tested current-account/source cutoffs now have
normal browser evidence. Deterministic activation lock races are accepted locally.
Neither gate closes #417 or #1035.

Legacy/governance cohorts, independent two-hospital/governance retention, suspension,
resignation, termination, contract-expiry, renewal/rehire and every affected HTTP,
async, external, timer and broadcast consumer still require their own acceptance.
The inventory remains 734 surfaces, 17 classified, 717 unclassified and zero complete
consumer cutoff acceptances. Whole Staff remains 10 partial, 10 awaiting audit,
1 research and 0 fully accepted areas. Keep these gates open in the delivery tracker.

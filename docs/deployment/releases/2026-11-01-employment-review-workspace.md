# Employment-review workspace rollout — 2026-11-01

Status: **deployed and verified** at approved revision
`7e5b7800b88f60a2579f6bacfd2f8dc23ea571ca`, merged through
[PR #1507](https://github.com/tesserix/helivanta/pull/1507). Its tree exactly
matches tested head `176d16f4`. The user explicitly approved this revision and
all four new Workforce migrations. The release adds scoped employment-review
discovery/inbox, HR finding/evidence selection, protected history/downloads and
independent decisions. Approval remains separate from employment/access activation.

## Delivery and migration evidence

Every required PR Go, web, E2E, script and secret-scan check passed. The
[merged image publication](https://github.com/tesserix/helivanta/actions/runs/41274127978)
passed build, vulnerability scan, SBOM and signing. The repository remains public;
no branch protection or bypass actor was changed for this merge or rollout.

Kargo promotion `prod.01m6apybqctyqtr0mvj1k573n4.aadb359` selected exact freight
`aadb359528ff758ad1b43a2825024668ee2261f7`; all three steps succeeded. The existing
Argo PreSync migration hook completed before the application update. The production
ledger changed from **193 to 197** entries: exactly these IDs were added and none
removed:

- `0062_workforce_employment_review_browse` — narrow inbox read context.
- `0063_workforce_employment_review_inbox_index` — concurrent inbox index.
- `0064_workforce_employment_finding_selection` — narrow finding/evidence selector context.
- `0065_workforce_employment_finding_selection_index` — concurrent selector index.

Both indexes are valid and ready. The
[machine-readable manifest](2026-11-01-employment-review-workspace.json) preserves
the canonical SQL source/hash and structured index declarations. SQL remains
append-only in the owner registry; no extracted SQL was applied manually.

All six application workloads reached **2/2 updated, ready and available** replicas,
with current observed generations. All **12 application pods** use the exact
published digests and have zero restarts. Argo API, Global API and shell are
Synced/Healthy; Kargo is Healthy:

- API and workers: `sha256:0efea2483f833b1324ac3031070eb5f4c24fbb514ef916f10d1e9507f5c61dab`.
- Shell: `sha256:4acf246f94ecf7c6d6cc257f6371256f3019b9957350c38396730cde6b7addef`.

The preflight verified two ready database instances, completed scheduled backup
`2026-11-01T00:00:24Z` and healthy continuous archiving. This is backup-status
evidence, not a new restore rehearsal. No production resource, replica, secret,
assignment or business record was changed by QA. Additional storage/index work
reuses the current runtime; no new always-on service is introduced.

## Production browser checks and limits

Fresh normal sign-in succeeded separately for the existing synthetic owner and
reviewer. Both loaded the new Employment reviews workspace and correctly saw no
hospitals without current scoped review authority. The reviewer also saw explicit
payroll denial and **No hospital access yet** on the Staff directory. No grant was
created to manufacture a positive production result. Private screenshots and raw
receipts remain in the existing QA evidence store.

One reviewer sign-in browser wait encountered a relay timeout; a subsequent DOM
snapshot verified successful sign-in without a credential retry or policy change.
An initial pod audit also included the completed migration Job in a prefix scan;
the final application audit uses ReplicaSet ownership and independently verifies
the migration Job's success. These interrupted checks are distinct from the final
verified results.

Positive preparation, real scanner release, exact downloads, independent approval,
rejection, withdrawal/revocation, immutable history and private-data cutoff were
accepted **locally** in the [workspace matrix](../../qa/2026-11-01-employment-review-ui-acceptance.md).
This rollout does not establish positive production review acceptance, digital
recipient provisioning, operational generation or suspension/exit/contract-expiry/
rehire enforcement. Whole Staff remains 10 partial, 10 awaiting audit, 1 research
and 0 fully accepted areas; no umbrella issue is closed.

Retain expanded read contexts/indexes and immutable evidence/history during
recovery. Prefer forward correction; a live rollback or schema removal requires
separate named approval and an owner recovery assessment.

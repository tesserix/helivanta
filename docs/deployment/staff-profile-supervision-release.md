# Staff profile and supervision release — 2026-10-29

Protected Staff enrollment now binds requests to the account and workspace that
opened them. The editor requires the owner acknowledgment and matching Staff ID,
clears private forms after account/access loss, and retains ordinary conflict
drafts. Inactive supervisors are labeled. Supervisor relationships remain HR
metadata and confer no login permission or clinical authority.

## Released source and delivery

- Source: `cf8ae1cfb14157fa2cd3a45b7da01151a224177a`, [PR #1447](https://github.com/tesserix/helivanta/pull/1447).
- User approved the named production rollout and inherited migrations.
- Freight: `d866a0e290e93782f6b4e43c6db5f548c1e6cee5`.
- Successful Kargo promotion: `prod.01m62wkctnk5tmp7vaf2n8awfg.d866a0e`, stage `prod` in `kargo-helivanta`.
- [Image publication](https://github.com/tesserix/helivanta/actions/runs/40812672395) succeeded on attempt 2. Attempt 1 timed out fetching GitHub's OIDC token for API signing; the failed job was retried with signing still required.
- [PR CI](https://github.com/tesserix/helivanta/actions/runs/40809708956) and [main CI](https://github.com/tesserix/helivanta/actions/runs/40812672797) passed. The checked head and squash merge have the identical Git tree `6a34f0bdf1663ac19ee00e271e9e5ce88ba66a81`.

All six application deployments (API, Global API, shell, document worker,
emergency worker and supply evidence worker) have two updated, ready and available
replicas on the source tag. API/Global API/shell are Argo Synced/Healthy. CNPG has
two ready instances. Production auto-promotion remains disabled. No resource,
replica, secret or infrastructure configuration was changed by this release.

## Schema evidence

The migration job ran the same approved API image and succeeded. Read-only
production ledger queries confirmed `0014_patientmaster` and `0025_pharmacy`
were applied at 03:52:58 UTC. These are inherited Pharmacy lookup changes; the
Staff slice adds no migration, permission, endpoint or table. Catalog queries
confirmed enabled/forced RLS on `patient_pharmacy_links`,
`patient_pharmacy_lookup_access` and `pharmacy_registered_patients`.

The prior code revision is `7671a23460fe25cd1f767bed3dcbbc4883008e79`.
Any rollback is a separate approved promotion; it must retain applied schema
and history rather than issue ad hoc down migrations or delete the new tables.

## Verified user journeys

Production tests used existing accounts and the designated synthetic hospital.
All business-data changes were made through the UI and Global API; SQL was used
only for read-only schema and persistence checks.

- Fresh restricted reviewer: no hospital Staff access or write controls.
- Staff Admin: a tenant without hospital grants showed no Staff access; explicit
  tenant selection opened the intended synthetic hospital.
- Blank save: full name and employee code failed validation. Two clearly named
  synthetic records were created and reopened after reload.
- Supervisor assignment persisted. A reciprocal link was refused as a cycle;
  reopening confirmed the refused link was absent.
- Concurrent tabs: a stale save was refused with its draft preserved. Discarding
  it required confirmation before loading the newer saved revision. Temporary
  test text and the synthetic name were restored.
- Tenant change: private fields and save controls disappeared; returning and
  reopening showed saved data, without the unsaved draft.
- Cleanup: supervision was cleared and both new records were inactivated,
  retained and reopened. Inactive supervisor choices were labeled. DB reads
  confirmed exact names/versions, SQL NULL supervisor IDs, empty temporary
  fields and retained write audits. No pre-existing staff or login grant was changed.

Local verification additionally covered header/marker malformed values, exact
unchanged-state rejection, another Staff response, old API refusal, and a
GET-new/PUT-old deployment. The strict old decoder rejected the projected PUT
without changing the synthetic profile/version/write-audit count. The temporary
compatibility processes were stopped. Full Workforce/Authn race tests, final
enrollment/master/supervision regressions, architecture checks, vet/build/lint,
canonical generation, contractdocs checks, scripts and all 30 frontend gates
passed; see PR #1447 for commands and detailed evidence.

Private credentials, screenshots and exact fixture identifiers remain in the
existing private evidence bundle, outside this public repository. The PR carries
local UI, CI E2E and deployed-release labels; broad issues are not closed.

## Remaining scope

The [Staff tracker](../qa/staff-management-progress.md) and
[acceptance ledger](../qa/staff-management-acceptance.md) remain authoritative.
Full employee/consultant onboarding, editing by other authorized roles, dated
transfers/postings, employment lifecycle and safe offboarding/removal remain
open, as do broader identity/photo/card and payroll journeys. The next transfer
foundation is the department owner accepted in ADR-0044, followed by Workforce
postings and IAM-owned reviewed assignments with effective access cutoffs.

This release does not certify full Staff Management or regulatory readiness.

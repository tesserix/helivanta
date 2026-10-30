# Staff preparation and scheduling release — 2026-10-30

Approved and deployed source: `c20a220267cc4e71cc169c47b3ef7220612da1ce`.
Release: [Staff preparation and scheduling safeguards](https://github.com/tesserix/helivanta/releases/tag/2026-10-30-staff-preparation-and-scheduling).
Previous production source: `22b6241ed0ece7180d713b77145104a63d95d343`.
The user approved this exact revision and all four migrations below before promotion.

## Durable migration sources

The [machine-readable manifest](2026-10-30-staff-preparation-and-scheduling.json)
records each migration's owner, registered ID, canonical SQL path, embedded Go
variable, SHA-256 of its committed SQL bytes, and observed production application
time. The SQL is versioned in Git and embedded into the migration binary; it is
not an uncommitted production patch or a separate schema copy.

| Migration                              | Canonical SQL                                                                                              |
| -------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `0002_billing`                         | [Bed-day charging](../../../backend/internal/modules/billing/bed_days.sql)                                 |
| `0003_billing`                         | [Interim billing](../../../backend/internal/modules/billing/interim_bills.sql)                             |
| `0008_medicore`                        | [Doctor discharge orders](../../../backend/internal/modules/medicore/discharge.sql)                        |
| `0052_workforce_employment_withdrawal` | [Employment preparation withdrawal](../../../backend/internal/modules/workforce/employment_withdrawal.sql) |

Registration remains in each owner's `module.go`. All four `//go:embed` and
migration ID/SQL-variable pairs were verified against the approved Git revision.
The release registry contains 166 migrations. Before promotion, 162 were applied,
four were pending, and no applied ID was absent from that registry. After the
normal migration job succeeded, all four IDs and application timestamps were
read from production `schema_migrations`.

For a future environment, use the established database-role bootstrap and the
normal [migration runner](../../../backend/cmd/migrate/main.go), with the secured
environment configuration. From `backend/`, its development command is
`go run ./cmd/migrate`; production uses the GitOps PreSync migration job. The
release requires the Go version declared in `backend/go.mod` (1.27.3 at this
revision). Apply the complete registered history to a new database, rather than
running these four delta files alone. The runner's ID ledger skips previously
applied migrations. The database ledger stores IDs/timestamps; the release
manifest supplies source hashes for provenance. Future corrections add a new
migration ID and retain these historical files.

## Deployment evidence

- Image workflow [40988659441](https://github.com/tesserix/helivanta/actions/runs/40988659441) built, scanned, signed and published both images and advanced the deploy branch.
- Kargo Freight `06dda82bbbcc843e705309c96f090f54d7fa7322`, promotion `prod.01m659npxcq1ejdjcv41942wbe.06dda82`, stage `prod` in `kargo-helivanta`: Succeeded.
- All six application workloads have two updated, ready and available replicas on the exact source. API, Global API and shell are Synced/Healthy in Argo CD.
- The migration job succeeded. Enabled and forced RLS were verified for all seven new Billing/MediCore tables and the four existing employment draft/evidence tables.
- Required PR CI [40985367730](https://github.com/tesserix/helivanta/actions/runs/40985367730) passed Go coverage, web, browser E2E, scripts and secrets. The merged runtime files match tested head `5363c59213e0d7aa425baa5add9b63380bdf5d12`; the only additional diff is the separately reviewed payroll contract correction. Final-source contractdocs check passed.
- Full local web checks passed all 30 tasks, including 1,222 shell tests and 409 API tests. The earlier complete local backend race/coverage gate passed, including Workforce at 83.7%; the subsequent Billing/architecture/contractdocs race checks passed after main reconciliation.

Merged-source CI [40988659542](https://github.com/tesserix/helivanta/actions/runs/40988659542) subsequently passed all required Go, web, browser E2E, script and secret gates on the exact deployed revision.

## Production Staff UI evidence

A fresh normal restricted-reviewer login retained hospital-scope denial and
payroll denial. The denied payroll page now correctly displays Payroll setup.
A fresh normal owner login first showed the ungranted tenant; selecting the
fixture's verified tenant exposed the existing explicit hospital-manager grant.
No QA account role, membership or hospital grant was expanded during these checks.

In the designated synthetic hospital, the Admin created a separate ongoing
employee preparation, verified the mandatory withdrawal reason, withdrew it,
and reloaded/reopened terminal revision 2 and both history revisions. The
original draft remained at revision 1 and the Staff record stayed inactive.
This closes preparation and does not approve or terminate employment.

The Admin created an unallocated synthetic shift, verified navigation confirmation
and Keep editing preservation, and saved its rules. Roster configuration-kind
changes similarly preserved the unsaved area until explicit confirmation. A
synthetic area pinned the shift revision. In an unpublished weekly-template form,
long area/shift labels stayed within their weekday cards (`min-width: 0`), no
controls overflowed or overlapped, and Remove second shift worked. The temporary
weekly pattern was explicitly discarded. The shift and area were retired at
revision 2; reload/history retained revisions 1–2 and their pinned timings.
No Staff assignment, dated roster, attendance or payroll action was performed.

Private screenshots and fixture evidence remain outside the public repository.
The release inherits bed-day/interim billing and doctor discharge ordering from
merged main; this Staff UI walkthrough does not claim additional clinical or
billing UI acceptance.

## Remaining scope and recovery

The [Staff tracker](../../qa/staff-management-progress.md) remains **10 partial,
10 audit pending, 1 research, 0 fully accepted**. Independent employment/identity
review, effective lifecycle cutoff, full scheduling, payroll runs, branded PDFs
and controlled offer/exit letters remain open. No umbrella issue is closed by
this release.

Retain expanded schema, command receipts and history on application recovery;
prefer a forward correction. A production rollback is a separately approved
operation and needs the relevant owner state assessment. No destructive down
migration or history deletion is part of this rollout.

# Staff activation foundation and scanner release — 2026-11-01

[Published release](https://github.com/tesserix/helivanta/releases/tag/2026-11-01-staff-activation-scanner).

Status: **deployed and verified** at approved revision `02b1e2b7e5a0fb9fa1fdbc65e449ca322cfda56a`. Employment activation remains **off**. This release does not establish full Staff Management or operational activation acceptance.

## Source and delivery

Activation foundation merged through [#1510](https://github.com/tesserix/helivanta/pull/1510), scanner provenance alignment through [#1511](https://github.com/tesserix/helivanta/pull/1511), and final local scanner recovery evidence through [#1513](https://github.com/tesserix/helivanta/pull/1513). Required PR checks passed, including Go race/coverage, lint, vulnerability checks, web, E2E, scripts and secret scanning. The deployed tree matches CI-tested code head `66eb9b4c`; subsequent QA-only changes contain no runtime changes. [Main image publication](https://github.com/tesserix/helivanta/actions/runs/41304202468) passed build, scan, SBOM and signing. The repository remains public.

The user explicitly approved this exact revision and all four migrations in the [persisted manifest](2026-11-01-employment-activation-candidate.json). Kargo freight `f250c430aa344725859fa7270a03681e81aec3e4`, promotion `prod.01m6bhqda9v5pd5xb3amqeh2hw.f250c43`, completed all three existing GitOps steps. The target was project `tesseracthub-480811`, context `gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`, namespace `helivanta`.

The existing Argo migration hook completed before application convergence. The production ledger changed **197 → 201**, adding exactly these IDs and removing none:

- `0022_iam_employment_activation_templates`
- `0023_iam_employment_bases`
- `0066_workforce_employment_generations`
- `0067_workforce_document_scanner_freshness`

All six new tables have enabled and forced RLS. PostgreSQL confirms the 48-hour scanner provenance limit and retained 45-minute retry guard. Employment generations remain zero; no operational grant or activation was performed.

All six application deployments have two updated, ready and available replicas. All twelve application pods match the published digests and have zero application restarts. API, Global API and shell are Argo Synced/Healthy; Kargo promotion succeeded.

- API and workers: `sha256:aea6335fcdb758f6fc9bacae0df7934504fd6b195642ad5a8ef8d4dbe0f2fdf2`.
- Shell: `sha256:e6fbbde3661c44c0d4c13c5267bf254de7157bfc484803e7ef1fa77f3d496c14`.

Preflight verified two ready database instances and a completed backup at `2026-11-01T12:00:22Z`. This is backup-status evidence, not a restore rehearsal. No new always-on service, resource request, replica setting or secret configuration was introduced. Schema/history and private file storage use the existing runtime. Recovery retains additive schema, receipts and audit history; a live rollback or removal requires separate named approval.

## Production browser acceptance

Fresh normal sign-in passed separately for the existing synthetic reviewer and owner. The owner explicitly selected the intended YES HOSPITAL tenant. The reviewer saw no unassigned employment-review hospitals, No hospital access yet on the Staff directory, and explicit payroll denial at `/workforce/payroll-setup`. The owner also had no implicit independent employment-review scope, while existing hospital authority correctly exposed the synthetic Staff directory.

Using that existing owner authority, QA uploaded `synthetic-production-scan-probe-20261101.png` to an existing synthetic Staff record. The real production scanner and worker produced Ready. The normal UI download saved all 179 original bytes exactly (SHA-256 `a8cf1db44d3b6cb252b53506ac3daadba919e0bb2c0721901851d335d83e3c64`). The probe was then revoked through the UI; its audit history remains and its download control is removed. No Staff employment facts, accounts or role grants were changed by this probe.

One initial server dry-run rejected a name prefix before creation; the corrected request used the previous release's supported prefix. A browser relay delay and an initial obsolete payroll URL were resolved against current UI/source before final assertions. The first download-file check ran before delivery settled; the completed saved artifact was subsequently verified. Those interrupted checks are not counted as successful assertions.

Private receipts and screenshots are retained under the existing `reviewer-ui-acceptance-20261101/rollout-02b1e2b7` evidence directory. Local scanner regression, both supported UI retries and the independent-source workflow remain tracked in [scanner acceptance](../../qa/2026-11-01-workforce-scanner-freshness.md) and the [Staff tracker](../../qa/staff-management-progress.md).

## Remaining gates

Employment activation remains default-off. Digital recipient acceptance, deterministic activation races, legacy/governance review and all consumer/lifecycle cutoffs remain open. Whole Staff remains 10 partial, 10 awaiting audit, 1 research and 0 fully accepted areas. No umbrella issue is closed or tagged as fully complete.

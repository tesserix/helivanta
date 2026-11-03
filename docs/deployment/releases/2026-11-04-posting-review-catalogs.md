# Posting submission and review catalogs — 2026-11-04

PR [#1539](https://github.com/tesserix/helivanta/pull/1539) adds the closed
`staff-posting-reviews-v1` catalog with hospital-scoped
`workforce.posting_submitter.v1` and `workforce.posting_reviewer.v1` templates.
Historical permission snapshots remain unchanged. This prerequisite does not
activate a posting, grant departmental operating authority or expose proposal UI.

## Release and migration evidence

- Merge source: `e9ea72816b688918aa5602d32e669a4dafa4655b`.
- Freight: `181116378923cd272cb0c8e5b298a47ad6cd610e`.
- Promotion: `prod.01m6gzg126gejj9mtq6bz45aba.1811163`, Succeeded through the unchanged Kargo promotion template.
- Exactly `0025_iam_staff_posting_review_templates` was added: 219 migration receipts before, 220 after. The preceding reader/preparer rollout had already applied its five migrations.
- Installed `iam_assignment_template_permissions(text)` function body matches canonical migration SQL byte for byte; SHA256 `4655ae9dbb038c973b59a0e778e4fadbb3d1fe97ddb3fcfd1d9182e94286bc80`.
- Migration job `helivanta-api-migrate-q6lfv` used the merged image and exited 0.
- Seven application deployments were 2/2 updated/ready/available. All 14 ReplicaSet-owned application containers were ready, with zero restarts. Completed migration Job pods were checked separately.
- API, Global API and shell Argo applications were Synced/Healthy.
- Published Artifact Registry digests exactly match running container image IDs: API `sha256:fb5f2d96fa282adbf517fefc4016e5e59c90cc69045bfdecc7b14a6504668f31`; shell `sha256:8decc82acb7350d32983c675afa55a950f76d0dd12ab6073399cbf4e34b55b2e`.

The approved boundary was account `unidevidp@gmail.com`, project
`tesseracthub-480811`, context
`gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`, application namespace
`helivanta`, and Kargo stage `kargo-helivanta/prod`. No imperative Deployment image
patch or unrelated GitOps worktree change was used.

## Validation and merge controls

Full serial real IAM race tests passed in 462.333s. SDK tests passed (609 tests in
76 files), as did 66 catalog/generator/inventory checks, API typecheck/lint, Go
build, scoped IAM vet/lint, contractdocs generation/check and whitespace checks.
The first IAM run exposed a stale migration-count assertion; the corrected test
also requires migration 0025 and nonempty SQL.

All [PR checks](https://github.com/tesserix/helivanta/actions/runs/41656940327)
subsequently passed, including Go, web, scripts, secrets and E2E. The approved
admin merge occurred while checks were pending. Normal admin merge was blocked
by ruleset 21115396; the user specifically approved a pull-request-only repository
administrator exception for this one merge. The exact original writable ruleset
was restored immediately and verified with no bypass actors. That exception is
not authorization for another PR. The repository remained PUBLIC.

The [merged image workflow](https://github.com/tesserix/helivanta/actions/runs/41659304484)
passed builds, Trivy scans, SBOMs, Cosign signing and deploy-branch advancement.
At evidence recording, the [main CI run](https://github.com/tesserix/helivanta/actions/runs/41659305740)
had passed web, scripts, secrets and E2E; Go was still running.

## Remaining acceptance

Public landing and sign-in rendered normally. Authenticated production catalog
UI acceptance remains pending owner sign-in. Computer Use denied access to
Terminal during credential-entry setup; no alternative route around that denial
was attempted. Credentials remain in the private QA JSON and are not copied here.

Proposal submission and rejection/withdrawal API/SDK changes remain unmerged in a
separate worktree. Positive approval, supervision, effective departmental
postings, lifecycle consumers and Pharma duties remain open. Full Staff Management
is incomplete: 10 partial areas, 10 awaiting audit and 1 in research, with zero
fully accepted areas. No umbrella completion label is warranted.

The [machine-readable record](2026-11-04-posting-review-catalogs.json) contains
bounded runtime evidence. Private captures are retained under
`~/.helivanta/staff-management-20261027/posting-review-catalogs-20261104/`.
See the [Staff tracker](../../qa/staff-management-progress.md).

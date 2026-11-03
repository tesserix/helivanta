# Staff posting preparation production release — 2026-11-03

PR #1524 adds non-operating preparation drafts against canonical Workforce Staff and the
current Organization department revision. Every public receipt remains `draft` with
`grants_access: false`. No duplicate employee or department master is introduced.

## Release identity

- PR: https://github.com/tesserix/helivanta/pull/1524
- Checked head: `f852ce7ce47d0f5ab4001318c9fadbce826fbfc1`; application code is identical to the separately browser-tested head `e061b955188f7264141382f88db96a45d904f409`.
- CI: https://github.com/tesserix/helivanta/actions/runs/41456474994 — all jobs passed.
- Merge revision: `305294ab3bf5e77dcb9db88d11af258f55be22f2`; its tree exactly matches the checked head.
- Main image workflow: https://github.com/tesserix/helivanta/actions/runs/41460365395 — API/shell build, scan/sign/publish and deploy-branch advancement passed.
- Kargo freight: `0477a4c69fe5b531512e28282673dac9b8762653` from the merged revision; promoted successfully.
- Forward migration: `0073_workforce_posting_drafts`.
- Specific user approval received for revision `305294ab` and migration 0073.
- Production promotion `prod.01m6eanw1cfa4s587sv6rx21qc.0477a4c` succeeded using the unchanged configured template. Migration job succeeded at the approved image; all seven deployments are 2/2 updated/ready, and API/Global API/shell are Argo Synced/Healthy.

## Delivered boundary

The new permission is tenant-admin preparation plus the existing explicit hospital
manager/reader grant. The Global API preserves original tenant/subject preconditions and
product-owned authorization. New writes pin the Staff version/epoch and an exact active
department revision. The narrow Organization owner guard holds both the restructuring
gate and source row until the outer product transaction commits. Heads, immutable
revisions, save audit and identifier-only outbox are atomic. Current authorization is
required for retained historical command receipts; an old receipt does not establish
current eligibility.

The Staff directory supports required fields, a canonical department picker, explicit
UTC instants, editing, bounded collection/history and retained exact command
retry/recovery. An uncertain command keeps its identifier and intent. Unsaved navigation
is guarded; tenant transitions cancel outstanding requests. Reasons, input hashes and
actor subjects are not part of public receipts.

## Observed validation

- Real PostgreSQL/OpenFGA tests cover source availability, invalid/missing fields, unauthorized tenant/hospital/Staff scope, role/grant removal, actor/intent replay conflicts, stale sources, competing commands, exact retained history, forced RLS and immutable records.
- Audit/outbox fault injection leaves zero heads, revisions, saved audits and events. A deterministic lock-removal mutation fails the owner-lock assertion; restored code passes.
- Timestamp regressions were observed failing, then fixed: truncation to the zero instant is refused; all receipt paths preserve UTC microseconds.
- Full affected Go race suites, architecture, Global API, contractdocs generation/check, vet/build/lint, generator checks and source-review inventory tests passed locally. Full API suite/typecheck: 594 tests. Full shell suite: 1,396 tests. Focused Staff directory/panel: 11 tests. Configured production-style build and relevant linters passed.
- Normal local Staff Admin browser blocked empty/invalid intervals, created version 1, edited version 2, displayed both complete historical intervals and persisted version 2 after reload. Read-only SQL confirmed one head, two immutable revisions, two save audits, unchanged Staff version/epoch, inactive employment and zero login links.
- Cancel and switching to Credentials prompt before losing an unsaved edit. Keep editing preserves text; Discard affects only the unsaved edit and leaves saved version 2 intact.
- Existing restricted reviewer normal sign-in shows the canonical inactive Staff but no posting preparation/edit controls. No membership or role grants changed. This does not prove positive scoped preparer/reader catalogs or independent posting approval.
- The inherited Pharma payment E2E text predicate returned before current payment confirmation. Its repair requires the current paid status region; timeout and sale/invoice/stock/return assertions are unchanged. Corrected-head E2E passed.

## Production preflight and recovery

Read-only preflight observed all seven application deployments ready at `6e9c4816`,
Kargo healthy with no automatic promotion policy, and 213 production migration receipts
identical to the local pre-migration baseline. Both new tables are absent and required
owner helpers/Organization tables exist. The approved local fixture moved from 213 to
214 receipts with exactly 0073 added.

The named approval was executed through the owning Kargo/Argo workflow without
deployment image patches or edits to the unrelated GitOps worktree. An initial invalid
generated-name prefix was rejected before any Promotion existed; the corrected request
passed server-side validation and then succeeded.

Production now has 214 receipts with exactly 0073 added. All four installed function
bodies exactly match canonical SQL. Both tables force RLS, all six triggers are enabled,
all eight indexes are ready/valid and constraints are validated.

Normal production owner sign-in and selection of the intended tenant passed. Against the
existing clearly synthetic inactive Pharma Staff record and canonical synthetic
department, UI QA created version 1, edited version 2, displayed both full intervals and
persisted after reload. Read-only SQL confirmed one canonical Staff master, one head,
two immutable revisions, two save audits, unchanged Staff version 2/employment epoch 1,
inactive employment and zero login links. The QA-only reasons identify the records; no
operating posting or account authority was granted.

The existing restricted production reviewer signed in normally and saw No hospital
access yet, with zero posting controls. No reviewer membership or role was changed; the
production session was signed out afterward. This is negative authorization acceptance,
not positive scoped preparer/reader or independent-review acceptance.

Preserve the additive schema and immutable drafts if an approved application rollback is
needed; do not drop or rewrite history. No rollback was performed.

Private captures, failure context, runtime metadata and screenshots are under
`~/.helivanta/staff-management-20261027/posting-preparation-20261103/`. Existing private
QA JSON credentials were reused and left unchanged. Cached dependencies were reused; no
container images or dependencies were downloaded locally. The bounded CI failure
artifact was retained privately for diagnosis.

## Remaining acceptance

Whole Staff Management is incomplete: 10 partial, 10 awaiting audit, 1 research and zero
fully accepted areas. Scoped preparer/reader catalogs, reviewed operating postings,
independent decisions, transfers/supervision/contractors, standalone employers and
Pharma duty/lifecycle enforcement remain open. Parent issues #118, #417 and #418 remain
open; no full Staff E2E-completion tag is earned by this preparation slice. ADR 0052
remains proposed.

See [posting contract](../../api/workforce-postings.md), [progress
tracker](../../qa/staff-management-progress.md) and [effective posting
plan](../../superpowers/plans/2026-11-02-effective-staff-postings.md).

## Scoped posting-role follow-up candidate — not deployed

Merged as #1532 at `091f2287`. Its release, approval and acceptance evidence is in the
[2026-11-04 release record](2026-11-04-scoped-posting-actors.md); the text below
describes the candidate as it stood before merge.

The `feat/scoped-posting-actors-20261103` follow-up adds immutable reader/preparer
catalogs through append-only IAM migration `0024_iam_staff_posting_templates`, a
bounded assignment-family list, exact SDK/admin catalog support and a shared
Workforce guard for current reviewed digital Staff eligibility. Earlier catalog
snapshots and the deployed legacy draft admission above remain unchanged.

The real reviewer/employment workflow exercises successful actor consumption,
non-identity-edit compatibility, identity-change invalidation and source-use
audit rollback. Full IAM/Workforce race suites, 596 SDK tests, 1,409 shell tests,
62 generator/inventory checks, typechecks and affected build/vet/lint passed.
The [current tracker](../../qa/staff-management-progress.md) records exact commands
and bootstrap/architecture results. The configured webpack build passed
with inherited React Hook Form export and external-cache standalone tracing
warnings; it does not establish a portable deployment artifact.

This candidate is not a release or approval request. Narrow department choices,
scoped draft consumers and their purpose RLS, bounded discovery, source-ending /
ordering coverage and normal positive-role browser acceptance remain open.
No production migration, rollout, new operating permission, acceptance label or
issue closure was performed for this follow-up. The approved production revision
and migration above apply only to PR #1524.

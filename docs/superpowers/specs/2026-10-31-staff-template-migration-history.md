# Preserve Staff template migration history

Status: local Access/RBAC implementation design. Required by the scoped HR intake
route/phase inventory; does not activate an intake role or complete A01–A12.

IAM owns templates, assignments and immutable migration history. The current
canonical generator reads the live template list when rebuilding SQL embedded
by migration 0014. Changing an existing template permission can therefore rewrite
historical SQL instead of requiring a new version/additive migration. Assets are
reviewed authority and reproducible schema history; accidental edits and a
compromised contributor must not silently redefine an existing template through
ordinary regeneration. Code review remains required for canonical authority.

Move the existing canonical definitions into an ordered template-migration
catalog in the same contract JSON. Each entry names its IAM migration, generated
SQL file, reviewed SQL SHA-256, normalized public-definition SHA-256 and the
template versions it introduces. Definitions
have one canonical location; effective runtime/Go/Zod/UI definitions flatten the
catalog. Never reuse a template key. The initial entry reproduces the current
0014 artifact byte-for-byte; later entries append keys through a generated
CREATE OR REPLACE template-permissions function preserving every earlier mapping.

Validate the complete catalog, ordered IDs, bounded/unique keys and permissions,
safe artifact filenames and each exact definition/SQL digest before writing ANY generated
model/artifact. A changed historical definition or activation source fails before
partial output. The digest is an explicit review anchor, not cryptographic proof
that a human approved an edit; changing it remains a reviewable authority change.
Keep migration IDs/metadata out of public template responses and UI definitions.

This tooling change does not add a role, permission, route or runtime migration.
The existing three templates and their permission sets must generate identical
artifacts. Keep current template-family validation until the full intake/owner/UI
slice adds its reviewed family. Do not enable a template-only partial workflow.

The command runs at development/CI time, with a catalog of tens of templates and
kilobytes of SQL. It adds no request latency, process, datastore, cloud resource
or recurring runtime cost. Missing/invalid history fails generation/check closed.
Rollback retains the canonical history and pinned old SQL; do not regenerate an
old migration from new authority. Scratch tests must exercise the actual CLI and
both normal/check modes, no partial writes on rejection, unchanged baseline and
an additive new version with exact preserved old migration. Run generator, script,
canonical model freshness, lint/typecheck/build and applicable backend checks.
No live database migration or production approval is claimed.

The catalog does not by itself prove older application versions reject every
newly introduced snapshot or enforce new template preconditions. The database
fixture proves unchanged discovery and refusal of an undeclared proposal only.
Supported snapshot authorization and old/new consumer compatibility remain
explicit activation gates for every real template extension; generation must
not be mistaken for runtime workflow readiness.

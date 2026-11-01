# Hospital-scoped HR intake

Status: implementation design, not activated authority. Complete the owner,
storage, discovery and UI paths together before release. Full A01–A12 remains
the objective; this is the intake prerequisite to independent employment review.
Follow ADR-0044/0047/0048/0049 and the existing 33-route intake inventory.

## Ownership, actors and bounds

IAM owns independently reviewed assignments, membership incarnations, scope
epochs, credential watermarks and immutable template definitions. Workforce owns
hospital-local Staff/profile/terms/evidence and current reviewed actor links.
Zitadel owns the global identity; Global API remains the browser entry point.
Private identity/employment files must be protected against another tenant or
hospital, revoked/stale actors, uncertain commands and unavailable dependencies.

Reuse the existing unmeasured planning assumptions: 500,000/1.5 million Staff at
12/36 months, 50 peak metadata requests/second, 80:20 read/write, metadata p99
below 500 ms and 99.9% availability. These are planning targets, not measured
capacity. Preserve current bounded payloads (8 KiB commands, existing enrollment
32 KiB exception, 10 MB files), default-50/max-200 keyset pages and bounded
20-candidate filtered scope discovery. The scope-command budget is five seconds,
bounded by the original idle deadline; identity checks keep their existing
ten-second bound and request cancellation. No new service, cache or datastore is
needed; cost is additional bounded indexed reads and existing audit/outbox writes.

The new `workforce.hr_intake.v1` template has administration authority at one
hospital and exactly `workforce.enrollment.manage` and
`workforce.documents.manage`. Existing authorized access makers propose it and
independent access reviewers decide it through IAM's current workflow. A current
reviewed actor link and active Staff source remain required at owner execution.
The assignment creates no hospital-manager row, clinical privilege, membership,
payroll, benefits, HR-master maintenance, credential decision or employment review
authority. Governance/recovery classification remains a separate migration gate.

## Template and compatibility contract

Append `0020_iam_hr_intake_templates` and its digest-pinned generated SQL to the
canonical catalog. Preserve every byte of migration 0014 and every historical
definition/hash. The new family is `intake`; new keys, family and both permission
snapshots must agree across canonical input, Go/Zod, discovery, SQL constraints
and projection/command guards. Do not accept arbitrary permission bundles or
silently expand an existing template.

Older applications do not support the new key or its permissions as scoped
authority. Keep old leave/identity family lists unchanged and introduce explicit
intake discovery/list selection for new consumers. Test old/new source admission,
list parsing, snapshots and projection before activation. A generated mapping
alone is not an enabled HR workflow. Application rollback retains migration,
epochs, assignments and history; it must deny unsupported authority rather than
restore old grants. Production migration/rollout needs its exact revision and
named action approved after validation.

## Owner and UI integration

The explicit resource projection is `hr_intake=v1`; require the original expected
tenant and expected subject headers. Reject unsupported/repeated projections with
400 and missing original workspace context with 409 before owner data is read.
Absent projection retains legacy admission, including its 403 rather than
changing it to 503 when a legacy fixture has no scope provider.

Template discovery defaults to the frozen three-template `legacy-v1` catalog.
`catalog=hr-intake-v1` explicitly selects four definitions. Canonical catalog
membership and its normalized definition digest are generated for Go/TypeScript;
adding a template must not expand a previously published closed catalog. Reject
unknown/repeated catalog selectors instead of exposing an unsupported definition.

Deliver a bounded hospital-intake scope discovery projection based on the
caller's own current reviewed links. Reauthorize each candidate under IAM's
guard; expose hospital identity and explicit current capabilities, not internal
assignment receipts. An absent or unavailable dependency must not become an
empty successful capability response. Preserve existing legacy hospital discovery.

At each affected owner phase, revalidate original credential/current identity,
refresh original IAM permissions and choose an independently valid legacy
manager path or the exact scoped intake requirements. Scoped phases must resolve
one/two required grants and hold them through the same owner transaction with
`WithScopeGrants`; check the current actor link/Staff and hospital there. Never
fall back after revoked credentials or borrow permission from another hospital,
account, epoch or callback. Scoped authority uses purpose-bound RLS context;
it does not insert a legacy manager grant. Out-of-scope objects stay 404, missing
authority 403, changed workspace 409 and unavailable authority 503.

Apply this only to the inventoried enrollment GET/PUT, employment preparation/
history/withdrawal, employment evidence and ten Staff-file routes. Employment
evidence needs both permissions in every phase. Staff files need document
authority. Directory discovery/read/edit and read-only HR-master choices must
support the same current intake context so the assigned user can reach and use
the workflow. Keep enrollment's credential/reminder and leave-standing/report
consumers separate; granting intake must not approve credentials or leave.

An intake user may prepare an inactive new Staff record and edit permitted
profile/terms/evidence. Intake cannot change an existing employment status or
activate operational employment through a directory/enrollment field. Return an
explicit review-required state and preserve input rather than silently changing
the requested status. Existing independent employment/current-person work still
has to provide the activation path. Preparing a draft grants no operational use.

The UI must use server-returned hospital capabilities in addition to global
navigation permissions. Show only scoped hospitals/actions; clear private data
on account/access change; retain exact uncertain commands in their original
context; preserve required fields, master revision pins and unsaved navigation
guards. Staff Access lists the intake family explicitly without exposing it to
old leave/identity consumers. No implementation details belong in the user flow.

## Scoped workspace client integration

Represent a selected hospital as a local discriminated view: either the actual
legacy hospital/access response or an independently returned intake capability
response. Do not synthesize a manager or reader grant to satisfy existing UI
checks. Shared enrollment components need only hospital ID/name; legacy-only
credential/link/identity actions retain their original checks. Directory edits,
terms and protected files use explicit intake capabilities, and their owners
continue to authorize every request independently.

The original account/tenant context also carries the selected intake request
mode locally. Append the generated `hr_intake=v1` selector only at final request
boundaries, after path suffixes and other projections have been composed. Preserve
cursors and lifecycle/profile selectors; never put a query on a base path before
adding child resources. Cache keys and retained command scopes distinguish intake
from legacy views. Switching hospital or mode confirms unsaved work and remounts
private state; a revoked or missing selected scope cannot silently select another
hospital. A new hire starts inactive in intake, and its employment status cannot
be changed through this preparation form.

Discovery validates the canonical capability response and bounded pagination.
Keep legacy discovery usable when an older owner lacks the new projection, while
reporting unavailable additional onboarding access explicitly. An unavailable
scope source must not become a successful empty list. Empty filtered pages with
continuation remain navigable. Initial selection waits for discovery to settle
and then remains stable as subsequent sources refresh.

The Staff access page also rechecks the server's explicit template catalog when
assignments refresh. A failed, pending or changed catalog blocks retained intake
commands, including an already-open decision. Preserve its selected family and
entered/uncertain request state while blocked; keep catalog refresh available.
Cached successful catalog data cannot imply current compatibility after failure.
The server still independently authorizes every command.

## Storage, failure and acceptance

Pin the first owner phase's authority kind and all required IAM assignment IDs,
versions, scope/membership epochs to this request. Bind the reviewed actor's Staff,
link ID/version and current employment epoch while their rows are held. Subsequent
phases must retain that exact requirement set and evidence; a separately approved
replacement is for a fresh request. Do not switch from scoped to legacy authority
or the reverse after the first owner phase. A real signed-session/GCS protocol
test reproduced 200 with PNG bytes after withdrawal and replacement during a read;
simply resolving the latest grant is insufficient.

Lock order is IAM proposals/membership/scope, hospital supervision, current actor
Staff/link, then target Staff/sponsor/child rows. Take the supervision lock before
actor rows to match existing profile writers. Keep network calls outside that
sequence. Request pins are private and never become a caller-supplied receipt.

These request pins are not a substitute for A04's accepted current-person and
employment-generation basis. Actor eligibility currently uses the existing
reviewed-link/active-Staff policy; generation-bound fresh-request eligibility,
formal employment review and legacy/governance classification remain required
before full A01–A12 acceptance or production activation.

Storage calls stay outside IAM/owner transactions. Repeat the full requirement
before each metadata/binding/release phase and after quarantine/released-byte
I/O. Scope/credential withdrawal must block a later owner commit despite stale
FGA; missing either permission denies employment evidence. Preserve purpose,
generation, scan/release, cleanup, exact retry and audit semantics. Denial releases
no private bytes/filename, successful-read audit or new scan command.

Required tests use real IAM, SQL, graph, owner and signed-session Global API:
independent assignment; an actor without a manager row; another hospital/tenant;
missing either permission; reviewed actor link/source loss; membership/credential/
scope withdrawal and expiry; source-lock waits and rollback; dependencies down;
unknown/new template compatibility; exact duplicate/conflicting commands; inactive
new-hire preparation without operational access; every storage phase and relevant
unchanged credential/leave consumers. Run generator normal/check and historical
artifact checks, contracts, complete affected race suites, vet/build/lint, all
web gates and exact-head CI. Record normal Admin assignment, intake user's
profile/terms/file workflow and restricted account UI evidence separately from
deployed acceptance. Do not merge a template-only result or mark A01–A12 complete.

Rejected: broad tenant-admin HR access, manufactured hospital-manager rows,
blanket changes to `enrollmentScope`, network calls under source locks, treating
directory-active as reviewed employment, or making non-digital hires create a
login just to prepare HR evidence. Person basis, independent employment decisions,
legacy cohort reconciliation and effective lifecycle/consumer cutoff remain
mandatory next parts of the same objective.

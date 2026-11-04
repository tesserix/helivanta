# Workforce postings — preparation and independent review

This preparation slice merged as `305294ab` in PR #1524 and is deployed after
specific approval of the revision and migration 0073. Every receipt is a non-operating draft. Workforce owns the draft and immutable commands/revisions; Organization
owns the referenced department/sub-unit. No Staff, employment, credential or
department master is copied. The complete
[posting specification](../superpowers/specs/2026-11-02-effective-staff-postings.md)
and [delivery plan](../superpowers/plans/2026-11-02-effective-staff-postings.md)
remain required, including independent review and operating lifecycle enforcement.

## Transport and current authority

All UI/app traffic enters Global API and routes to Workforce. Each legacy preparation route below requires
verified tenant membership, `workforce.posting.prepare`, and current explicit
Workforce hospital reader/manager access. Writes and PUT retries require manager.
The hospital grant and Staff source are held through the write commit. The new
permission confers preparation only; no clinical, professional, payroll, login or
operating authority follows. Scoped reader/preparer consumers and independent review are separate deployed
slices documented below. None grants an effective placement or operating access.

Every request requires single `X-Helivanta-Expected-Tenant` and
`X-Helivanta-Expected-Subject` values matching the verified principal. These detect
an unintended workspace/account change and confer no authority. Missing/mismatched
preconditions return 409 `session_changed`. Responses are `no-store`.

| Method and path below `/v1/workforce/hospitals/:hospital/staff/:id` | Result                                                                                                                                                                                           |
| ------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| GET `/posting-draft-source`                                         | The existing canonical `EmploymentPeriodSource` four-field concurrency basis: hospital/Staff ID, Staff version and employment epoch. No identity hash, name, evidence or private profile fields. |
| GET `/posting-drafts`                                               | Bounded newest-first draft heads.                                                                                                                                                                |
| GET `/posting-drafts/:posting`                                      | Current immutable draft revision.                                                                                                                                                                |
| PUT `/posting-drafts/:posting/commands/:command`                    | Create or edit a draft, or recover the exact retained command.                                                                                                                                   |
| GET same command path                                               | Recover its original receipt under current read authority.                                                                                                                                       |
| GET `/posting-drafts/:posting/history`                              | Bounded newest-first immutable revision history.                                                                                                                                                 |

Hospital/Staff retain their existing nonzero UUID formats. Posting, command,
structural department and structural revision IDs require UUIDv7. History orders
by `(recorded_at, revision_id)`; draft collections order by `(created_at, id)`.
Standard opaque cursor/limit rules apply. Private reads have owner audit
consequences; audit failure withholds the response.

## Canonical input and retained state

`contracts/workforce-postings.json` owns draft input/output and identifier-only
change-event payloads. `scripts/generate-workforce-postings.mjs` emits Go/Zod
models and supports `--check`. The source response reuses the existing
`EmploymentPeriodSource` model rather than copying its four wire fields.

A complete command supplies `expected_version` (zero only for creation), current
`staff_version` and `employment_epoch`, `department_id`,
`department_revision_id`, an explicit `starts_at` instant, optional `ends_at`,
and a required protected reason (1–500 characters). Null/absent expected version
is refused. Unknown fields and additional JSON values are refused. Omitted/null
end means no proposed end; this full command replaces the prior interval rather
than patching it. End must follow start. Instants normalize to UTC microseconds;
invalid, zero and out-of-range instants are refused.

Every public receipt is `status:draft`, `grants_access:false`, with canonical IDs,
versions and recorded instants. Reasons, input hashes and actor subjects remain
private. Drafts can prepare a placement for an inactive joiner before employment
activation. They do not establish current employer eligibility, change the
canonical Staff profile's department, assign a supervisor or outlet, create a
login link, or activate an IAM assignment. Submission/approval must later bind
the current reviewed employment generation and all required posting conditions.

Command IDs are unique within the tenant. The retained fingerprint binds the
exact Staff/draft, actor, expected version and normalized complete intent.
Same-key/same-intent PUT retry reauthorizes and returns its original revision,
including after later edits. Changed actor/intent or stale Staff/draft source
conflicts. A response timeout is an unknown outcome: recover the exact command;
do not substitute a new ID/version after refreshing history.

## Current structural check and persistence

For a new write, Workforce starts its bounded guarded command, holds its hospital
grant and Staff source, then invokes the registered Organization reference guard.
That owner checks the exact verified hospital and current active department
revision, holds its restructuring gate/row through the same database commit, and
returns no copied repository or employee data. Only the registered Organization
owner supplies that guard. Missing/invalid owner configuration fails closed;
it never fabricates an empty current source. This is the narrow local structural
part of proposed ADR-0052, not a general cross-module RPC or a cross-service
cutoff protocol. A history read does not certify its old reference as current.

Forward migration `0073_workforce_posting_drafts` adds owner-only draft heads,
immutable revisions/commands, exact Staff-purpose RLS, source/version/actor
constraints and deferred head/latest-revision integrity. Department data remains
with Organization; no cross-owner foreign key/query is introduced. A save,
revision, audit and outbox insertion commit together. No destructive down
migration is supplied; rollback retains additive schema and history.

The event `helivanta.in.workforce.posting_draft_changed.v1` carries hospital,
Staff, draft/revision IDs and version only, within the verified tenant envelope.
It contains no reason, name, registration or actor subject. No downstream
operating-authority consumer is registered by this candidate.

## Failure and acceptance boundaries

Missing route permission is 403. Hidden/foreign Staff, hospital, draft or
structural object is 404. A changed/retired structural revision, stale version
or conflicting command is 409 `posting_draft_conflict`. Missing source provider
or unavailable authorization/source is 503 `posting_source_unavailable`.
Unexpected persistence/audit/outbox failure is a generic 500 with rollback.

Handler, SQL and real OpenFGA integration tests now cover required fields, stale
sources, revoked and foreign scope, exact command/history retention, competing
commands, atomic audit/outbox failure and Staff-purpose RLS. Deterministic owner
tests hold both source locks through the outer commit, refuse escaped/mismatched
contexts and detect removal of the shared restructuring lock. All public receipt
paths serialize UTC instants with microsecond precision. SDK tests verify scope,
intent, non-operating receipts and bounded page integrity. See the
[progress evidence](../qa/staff-management-progress.md) for actual suite results.
The Staff directory now exposes preparation/editing and immutable history to the
declared tenant-admin permission plus the current explicit hospital grant. UI
tests cover mandatory fields, exact retained retry, reader controls and source
outages. Normal local owner create/edit/reload and restricted-reviewer denial
passed through the Global API. Positive scoped role catalogs and independent
review/effective activation remain open. Migration 0073 and this slice were specifically approved and deployed. The [release journal](../deployment/releases/2026-11-03-staff-posting-preparation.md) records CI, images, installed schema and local/production acceptance evidence.
Preparing these drafts does not complete effective transfers, supervisor changes, contractors, independent
posting approvals, Pharma professional duties, standalone employers or operating
lifecycle enforcement.

## Scoped-role foundation

Merged as #1532 at `091f2287` and deployed to production by an approved promotion
on 2026-11-04; see the [release record](../deployment/releases/2026-11-04-scoped-posting-actors.md).

The candidate `staff-postings-v1` access catalog is owned by
`contracts/staff-access.json` and installed by the additive IAM migration
`0024_iam_staff_posting_templates`. Earlier catalog snapshots are unchanged.
`GET /v1/iam/posting-assignment-proposals` is the separate bounded posting-family
list, under the same current proposal-management/review authority as other IAM
assignment families. The SDK and assignment administration use that endpoint.

The reader snapshot contains `workforce.posting.read` and
`workforce.staff_actor.use`. The preparer adds `workforce.posting.prepare` and
`organization.posting_choices.read`. Catalog publication and graph projection
do not activate either runtime consumer. The currently deployed six draft
routes above retain their existing preparation permission and explicit hospital
grant. The candidate source guard additionally requires IAM's exact active
callback-bound source-use grant, a current reviewed link and effective digital
employment generation for the same subject and hospital. It constrains the
shared root command to the earliest owner source end and audits successful use;
denial rolls back the source-use audit. Historical links without generations
remain compatible with existing consumers and are refused by this new guard.

The provider is registered in bootstrap and the test harness. The Organization-owned choices consumer is implemented in the same candidate;
see [its contract](organization.md#scoped-posting-choices). Scoped draft
consumer composition, posting-purpose RLS and discovery are implemented in the
candidate contract below. Normal positive-role browser acceptance remains open.
These routes run in production from `091f2287`; drafts stay non-operating and no effective posting exists. See the
[delivery tracker](../qa/staff-management-progress.md) for actual verification
and remaining release gates.

### Scoped workspace candidate contract (implemented; normal UI acceptance open)

Additive scoped draft paths use `/v1/workforce/hospitals/:hospital/posting-staff/:id`
with the same six suffixes and receipts above. Reads require
`workforce.posting.read`; PUT requires `workforce.posting.prepare`. Both require
the independent `workforce.staff_actor.use` assignment, fresh IAM evidence and a
current reviewed digital actor at the same hospital. Legacy paths are unchanged.
Business audit retains operation assignment/version/epochs and actor/link/generation
references. Migration `0074_workforce_scoped_postings` adds exact-target read/lock
visibility without Staff profile writes; rollback retains additive schema/history.

`GET /v1/workforce/posting-scopes` uses the existing bounded
20-candidate reviewed-link discovery, filters each hospital through the current
compound posting/source authority and returns canonical `StaffPostingHospitalScope`
(hospital ID, name, can_prepare). Empty visible pages retain their continuation.
`GET /v1/workforce/hospitals/:hospital/posting-targets` is a separate bounded
50-target projection returning only canonical `StaffPostingTarget` ID, employee
code and display name. It requires the same current reader/source command;
private profile data is excluded. Target discovery cannot broaden draft-purpose
RLS beyond its selected exact Staff. Both carry expected account headers, no-store
responses and atomic owner/source-use audit. Authorization unavailability withholds
results; absent current eligibility denies rather than reusing a legacy grant.
Real signed-session/PostgreSQL/OpenFGA tests cover reader/preparer discovery,
retained commands, grant/link/membership withdrawal and source expiry. Normal
positive-role core UI journeys now also pass with a freshly reviewed digital
actor and no legacy hospital grant: reader history/reload without write controls,
and preparer required-field create/edit/history/reload and unsaved-navigation
protection on the same canonical Pharma Staff target. See the
[local UI proof](../qa/2026-11-03-scoped-posting-positive-ui.md). Exact response-loss
recovery, role withdrawal and the remaining negative browser matrix still need
acceptance; these results grant no effective posting or production authority.

## Deployed independent review catalog

The isolated independent-review feature appends the closed
`staff-posting-reviews-v1` catalog and additive IAM
`0025_iam_staff_posting_review_templates`. It preserves all eight
`staff-postings-v1` definitions byte-for-byte at the contract level, then adds:

| Template                         | Additional command authority                                                                                                |
| -------------------------------- | --------------------------------------------------------------------------------------------------------------------------- |
| `workforce.posting_submitter.v1` | Draft read/preparation, proposal read/submission, current canonical department choices and reviewed Staff actor source use. |
| `workforce.posting_reviewer.v1`  | Draft/proposal read, independent proposal review, current canonical department choices and reviewed Staff actor source use. |

Neither role grants general profile editing, structure governance, operating IAM,
clinical or Pharma authority. Reviewer excludes draft preparation and submission;
submitter excludes independent review. The catalog deployed in #1539. Proposal API/SDK, discovery and the read-only
reviewer inbox deployed in #1544 (`1d6becdf`); rejection/withdrawal UI deployed
in #1547 (`e7588e65`). These releases do not prove authenticated browser acceptance
or positive approval. The [independent review specification](../superpowers/specs/2026-11-03-independent-staff-posting-review.md)
retains the complete supervision and lifecycle obligations. Positive approval
still awaits supervision policy; no operating role is activated by a proposal.

## Separate hospital management review catalog — candidate

The closed `staff-posting-management-v1` catalog appends
`workforce.posting_management_reviewer.v1`, containing exactly
`workforce.posting.management.review` and `workforce.staff_actor.use`.
IAM owns its additive `0026_iam_staff_posting_management_templates` migration;
Workforce registers the management permission. Default and all previous catalog
snapshots, including the ten-entry independent-review catalog, remain unchanged.
Canonical inputs are `contracts/staff-access.json`; generated Go, Zod and SQL must
remain aligned. The ordinary IAM assignment/review and OpenFGA projection journeys
apply, with verified tenant/hospital scope, current reviewed Staff source,
independent assignment review and durable withdrawal/recovery.

This capability is separate from ordinary posting review. Its template cannot
submit or approve a proposal on its own, edit profiles or structure, or grant
operating, clinical or Pharma authority. Nothing is automatically assigned.
The root-approval consumer, supervision/lifecycle enforcement and authenticated
reviewer UI acceptance remain separate increments. This catalog-only change adds
no route, event, Workforce migration or activation behavior.

The normal Staff access page requests these closed catalogs through Global API,
verifies exact definitions and permission sets, and offers only templates from
the newest supported catalog. An unavailable or malformed management catalog
falls back to a verified independent-review/preparation catalog; unverified new
roles are absent. The area is labelled “Staff posting access”. A retained renewal
whose template loses catalog confirmation cannot submit or retry until its exact
catalog is restored; restoration preserves the same command ID and payload.
Catalog metadata and UI gating never replace server authorization or independent
assignment approval. Real-account acceptance remains pending; component checks
are not end-to-end approval evidence.

## Deployed proposal submissions and readiness

The #1544 release implements the following suffixes below
`/v1/workforce/hospitals/:hospital/posting-staff/:id`. These production interfaces
create non-operating proposals, not effective departmental assignments.

| Method and suffix                                    | Required current scoped capability |
| ---------------------------------------------------- | ---------------------------------- |
| GET `/posting-review-source`                         | `workforce.posting.proposal.read`  |
| PUT `/posting-proposals/:proposal/commands/:command` | `workforce.posting.submit`         |
| GET same command path                                | `workforce.posting.proposal.read`  |
| GET `/posting-proposals`                             | `workforce.posting.proposal.read`  |
| GET `/posting-proposals/:proposal`                   | `workforce.posting.proposal.read`  |
| GET `/posting-proposals/:proposal/history`           | `workforce.posting.proposal.read`  |

Every path additionally composes the exact current `workforce.staff_actor.use`
assignment and reviewed digital actor through the existing owner-held root
transaction. Tenant/hospital/Staff, original account headers and current
credential/membership/scope epochs remain independent checks. No legacy grant
fallback is available for these paths. Target employment need not have a digital
login; the real different-target/non-digital journey verifies source admission,
immutable submission and changed/restored identity refusal.

Submission requires explicit expected version zero, the current draft ID/revision/
version, current Staff version/employment epoch, exact reviewed generation ID,
required reason and true `sources_examined`. Missing/null/false and unknown fields
are refused. The server derives the department and proposed interval from the
selected current immutable draft and checks its Organization-owned reference.
It consumes the Workforce-owned current generation, person/employment findings,
identity epoch and evidence; no private HR projection is copied into another owner.

Fixed-term placements require a planned end no later than the reviewed appointment
end. Ongoing employment may propose a null placement end. A placement's nominal
end is distinct from `eligibility_until`, the captured reviewed-source cutoff:
source validity may expire before a long-running/open-ended placement ends.
The start must fall within the reviewed appointment and before that cutoff.
Current actor/target source deadlines still constrain the entire submission
transaction. Later operational consumers must intersect all current source,
posting, duty and IAM intervals; a stored open-ended proposal grants no authority
beyond a source cutoff. No interval is silently shortened or fabricated.

The public receipt includes proposal/revision/command IDs, exact Staff/draft/
generation/department pins, interval, source cutoff, status and recorded instants.
Initial status is `pending`; terminal decisions produce `rejected` or `withdrawn`.
`grants_access` is always false. Protected reason,
input hash and actor/contributor identities are excluded. Same-key/same-actor/
same-intent retry reauthorizes and returns its original receipt; changed intent
conflicts. An additional live proposal for the same draft revision conflicts.
Lists/history use bounded keyset pages. Sensitive source/receipt/history reads
are audited; all responses are no-store.

Readiness exposes current Staff concurrency and a minimal reviewed generation
projection with an owner `checked_at` instant. `no_current_generation`,
`current`, `not_started`, `source_changed` and `expired` are explicit states.
Missing generations use nulls, never fabricated zero IDs or success. Missing
source/authorization dependencies return 503. The SDK validates original scope,
selected draft/command/generation and microsecond intervals, rejects operating
receipts and never routes review through the legacy Staff paths.

Deployed Workforce migration `0075_workforce_posting_proposals` adds forced-RLS
heads, immutable command/revision rows, exact composite source references and
current-head/actor/interval guards. Submission, audit and the identifier-only
`helivanta.in.workforce.posting_proposal_changed.v1` event commit together.
Removal and history rewriting are refused. The SQL guard permits only initial
pending submission followed by one independently rejected or proposer-withdrawn
revision. Approval is refused until the supervision policy is implemented.
Earlier SQL remains unchanged; rollback retains the additive tables and evidence.

## Deployed rejection and withdrawal

### Protected review context

`GET /posting-proposals/:proposal/context` requires exactly one `purpose`
(`review` or `submission`), positive safe-integer `expected_version`, and UUIDv7
`expected_revision_id` query parameter. Unknown/duplicate/malformed parameters
and query strings over 1 KiB are 400. A changed current head is 409; a hidden
Staff/proposal is 404. No body field or original-account header grants authority.

`review` requires current hospital-scoped `workforce.posting.review`, actor-use
authority and the same target/contributor/historical-association independence
checks as a decision. `submission` requires current `workforce.posting.submit`,
actor-use authority and the original submitter subject. These checks precede
private rationale loading, including for retained terminal heads. A static legacy
permission is not used to choose between these two owner-authorized purposes.
The registry uses `authz.Public` for dispatch; authentication, membership and
purpose-specific current owner checks remain required.

The canonical response contains `purpose`, exact public `proposal` and selected
immutable `draft`, `draft_reason`, `submission_reason` and nullable
`decision_reason`. Pending heads have no terminal rationale. Each reason is at
most 500 characters; no actor subject, hash, identity dossier, attachment or
private HR projection is returned. Later draft edits cannot replace the submitted
revision or rationale. This retained view does not certify current source or
department eligibility and does not authorize a later command.

The existing guarded decision-purpose transaction acquires its supervision gate
before actor rows, preserving source writers and current authority through outer
commit. Exact-version context-read and Staff-source-use audit commit together;
failure withholds all protected data and rolls back source-use evidence. No
business revision, head or outbox row changes. Responses are no-store and the
single-object response fits the 20-KiB planning budget. Dependency failure returns
503 without a legacy fallback. This adds no migration or deployed UI acceptance.

### Terminal command behavior

`PUT /posting-proposals/:proposal/decisions/:command` selects current
`workforce.posting.review` for rejection or `workforce.posting.submit` for
withdrawal, always composing the exact actor-use grant and reviewed digital source.
The route's registry entry is public only with respect to a static legacy
permission; it still requires authentication/membership and the action-specific
owner authorization above. Missing current owners fail 503 before parsing; there
is no broad administrator fallback. `GET` of the same command suffix requires
current `workforce.posting.proposal.read` and returns one immutable receipt.

Input requires the exact pending `expected_revision_id` (UUIDv7), positive
`expected_version`, `decision`, protected control-free reason and true
`sources_examined`. Unknown fields/false confirmation are 400. Stale pins,
changed key intent or a second terminal transition are 409. An unrelated hidden
proposal/receipt is 404. Original proposer identity is required for withdrawal,
even if the caller separately holds both submit/review capabilities. Independent
review excludes the target, original submitter and every retained draft
contributor, including historical target and reviewer login associations.
Only exact target/reviewer associations are visible under the protected decision
purpose; existing write checks and delete restrictions remain unchanged. No
association identity is returned to a caller.

Rejection and withdrawal copy the exact original source/interval pins, append
version 2 and atomically update the head, audit and identifier-only outbox. They
do not require currently eligible target sources, so expired/changed evidence
does not strand a pending proposal. Actors and IAM scopes must still be current.
Original submission and terminal command receipts remain immutable. Exact-key
retry reauthorizes and returns the original result after later changes; changed
actor/intent conflicts. Rejected/withdrawn drafts may be proposed again under a
new proposal ID. No result grants operating access.

An authorized independent `approve` returns 409 `posting_supervision_required`
and appends nothing while the mandatory supervision policy is unavailable.
The SDK exposes rejection/withdrawal and exact-command recovery, retaining the
original pending receipt to verify every immutable source/interval pin and the
expected terminal version. It refuses approval before transport. This is not
positive posting approval or reviewer UI acceptance.

Independent approval, supervision/transfer/lifecycle enforcement, Staff review UI
acceptance and operating consumers remain open. Earlier migrations and catalog
snapshots remain unchanged. See the [delivery evidence](../superpowers/plans/2026-11-03-independent-staff-posting-review.md).

### Review hospital discovery

`GET /v1/workforce/posting-review-scopes` uses the same bounded 20-candidate
reviewed-link discovery and opaque continuation rules. It independently requires
current scoped proposal-read and Staff-actor-use authority and a current reviewed
digital actor. The canonical `StaffPostingReviewHospitalScope` projection contains
only `hospital_id`, `name`, `can_submit` and `can_review`. Permission/source refusal
filters a candidate; unavailable authorization or audit persistence withholds the
page. Missing current owner providers return 503 even before an empty scan.
Submit/review flags are observations; commands reauthorize the exact current
assignment and Staff source through commit. No draft-edit, clinical or Pharma
right follows from this projection. The existing reviewer catalog includes draft
reading and review, without preparation/editing. Earlier catalogs remain intact.

The SDK uses same-origin Global API, mandatory original-account headers, no-store
and cancellation. It rejects extra/private fields, missing/nonboolean flags,
duplicate hospitals, oversized pages and inconsistent/repeating continuation.
Discovery and the hospital inbox deployed in #1544. Reviewer rejection and
submitter withdrawal controls deployed in #1547; normal real-account browser
acceptance, supervision and positive approval remain unfinished.

## Hospital posting proposal inbox

`GET /v1/workforce/hospitals/:hospital/posting-proposal-inbox` requires current
hospital-scoped `workforce.posting.proposal.read` and a reviewed digital Staff
actor with `workforce.staff_actor.use`. The caller's active login link supplies
a candidate actor only; current assignment, membership/credential epochs and
actor source are rechecked under the owner transaction through commit. Legacy
administrator authority alone does not authorize this route.

The only filters are `status`, `limit` and `cursor`, each supplied once. Status
defaults to `pending`; `approved`, `rejected` and `withdrawn` are also accepted.
Pages default to at most eight items, plus one internal continuation probe, in
descending `(created_at,id)` order. Cursors bind to the current subject, exact
hospital, route and normalized status as well as the verified tenant. Replaying
across those boundaries returns 400. Responses use `Cache-Control: no-store`.

Each item contains the public proposal, canonical Staff ID/code/display name,
and `is_submitter`. It includes no rationale, subject, hashes or identity dossier.
`is_submitter` is a display hint; exact-head protected context and decisions
independently reauthorize the original submitter or independent reviewer.

The owner records hospital, status, returned-row count and current actor source
proof atomically. Audit persistence failure returns 500 with no data and rolls
actor-use evidence back. Unavailable authorization returns 503; missing current
permission returns 403. Empty queues are not a fallback for unavailable owners.

Append-only deployed migration `0076_workforce_posting_proposal_browse` extends
the single existing permissive read predicate for heads/current-and-original
revisions, with restrictive UPDATE/DELETE guards retaining the prior read
restriction and unchanged write checks. History-preservation guards remain intact. Migration `0077`
registers a concurrent `(tenant_id,hospital_id,status,created_at DESC,id DESC)`
index. These follow 0075; all three deployed in #1544, advancing the production
ledger 220→223. #1547 added no migrations. Rollback preserves additive schema and
history; a compensating change needs separate review. No positive operating
activation is established by these releases.

## Normal submission UI — candidate, browser acceptance open

The next UI slice adds submission from the normal posting review workspace only
for a server-observed hospital `can_submit`. It uses canonical scoped Staff
choices and existing draft/source endpoints. It displays the exact draft version,
interval with recorded offset and matching active Organization department code/name before
rationale and source-examination attestation. Current department choices are
bounded and paginated; an empty page with continuation is not treated as absence.
Unavailable or changed department/source observations disable new submission.
The owner remains responsible for complete interval and source admission.

One parsed intent, proposal ID and command ID are retained through uncertain
outcomes. Receipt404 preserves them and independently rechecks current hospital submission
admission, draft, employment and department observations. A lost submit capability
clears private controls even when read access remains. Admission503 preserves the
command and permits a deliberate admission recheck without granting new authority. Exact retry never rebases the command onto
new sources. Dirty/retained forms pause background refresh and prevent silent
navigation loss. Account/tenant changes abort the client and clear private state;
the original request may already have committed. All requests retain original
account headers through Global API, and owner commands reauthorize through commit.
No API, schema, permission, OpenFGA model or production activation changes here.

The released reviewer slice is recorded in the
[deployment evidence](../deployment/releases/2026-11-04-posting-decisions.md).
Authenticated browser acceptance of normal submission/review, effective approval,
supervision, transfers, contractors and all departmental/Pharma consumers remain
open. UI component/SDK tests do not close those acceptance gates.

## Reviewed supervision evidence — unpublished candidate

The `codex/posting-supervision-approval-20261104` candidate accepts optional
`supervision` in proposal submission. Its kind is `hospital_management`,
`department_supervisor` or `department_member`. Hospital management has no
supervisor references; other kinds require nonzero `supervisor_staff_id` and
UUIDv7 `supervisor_proposal_id` / `supervisor_revision_id`. Omission preserves
legacy non-operating meaning; explicit null, partial references and unknown
fields are rejected. These are proposed references, not verified supervisory
appointments. Positive approval must independently resolve their exact scope,
current approved revision, managerial chain and source eligibility. Retaining a
claim neither verifies it nor grants access.

Additive forward migration `0079_workforce_posting_supervision_intents` retains
typed immutable intent alongside the exact initial proposal revision. Forced RLS
uses the existing selected-Staff posting purpose. The initial revision marks
whether intent was supplied. Both records must be created by the same subject in
the same transaction; deferred integrity refuses commit if requested evidence is
lost. Historical rows are not backfilled or reinterpreted. New creation-transaction
defaults apply only to future revisions; the migration does not rewrite historical
rows with a volatile default. Revisions and intent cannot be updated or removed.
There is no destructive down migration; rollback preserves the additive schema
and audit history. Migration0079 alone retains the old positive-approval denial;
the candidate root approval extension below is separate.

An authorized submitter or independent reviewer can explicitly request:

`GET /posting-proposals/:proposal/context?purpose=submission&expected_version=1&expected_revision_id=:revision&supervision_context=v1`

The opt-in response is `{context: <existing protected context>, supervision:
<intent or null>}`. Default responses retain their exact existing shape. Repeated,
unknown or invalid flags return 400. Current scoped operation/source grants,
reviewer independence, exact immutable source pins and atomic disclosure audit
are unchanged. Missing marked evidence returns 503 rather than an invented root
or null intent. The SDK's separate `getStaffPostingSupervisionContext` verifies
the envelope and all existing nested source checks. Its legacy context getter
rejects the new transport selector rather than silently discarding it.

This branch is unfinished and is not a production release or normal UI acceptance.
Root-management capability, positive approval and normal selection/review UI are
implemented in the candidate described below. Real-account browser acceptance,
operating lifecycle consumers and full Staff acceptance remain open.

### Separate management-review capability — unpublished candidate

The root consumer uses the separate management catalog and verified normal
assignment choices described above. An ordinary independent reviewer assignment
and current Staff actor source remain additional requirements. Catalog membership
alone cannot approve a posting or grant operating/clinical authority. The new root
consumer and its migration remain separate from catalog PR #1563.

### Hospital-management root approval — unpublished candidate

Candidate migration `0080_workforce_posting_root_approvals` admits only an
independent approval of retained `hospital_management` intent. It records immutable,
forced-RLS management evidence against the initial and approved revisions, current
reviewer Staff/login/generation, and separate current ordinary-review and
management-review assignments. Both assignments remain callback-bound through
commit; the shared IAM/authz guard accepts at most three distinct exact checks.
The explicit decision field `hospital_management_reviewed=true` selects the extra
management check before Workforce locks. Missing authority denies403; omitted or
false attestation retains409 `posting_supervision_required` and consumes no command.
The SDK permits an explicit attested approval and validates an `approved` receipt.
The candidate review UI described below requires separate browser acceptance.

The target's locked Staff version, employment epoch, reviewed generation, nominal
interval and effective cutoff must still match. Organization holds the current
exact active department/ancestry gate until commit. The transition preserves the
proposal's source pins, adds the management evidence, head, revision, audit and
identifier-only outbox in one transaction. Missing internal evidence fails commit;
no operating, clinical, IAM or profile-supervisor authority is granted.
Non-root and legacy omitted-intent positive decisions remain denied pending chain
implementation. There is no historical backfill or destructive rollback.

Real Global API/PostgreSQL/OpenFGA tests pass for independent root approval,
missing management authority, missing attestation, pending-state preservation,
exact retry, changed-intent conflict and retained history (54.953s, race enabled).
These focused tests do not establish complete negative/race coverage, normal UI
acceptance, lifecycle enforcement or production deployment. The full IAM recovery
failure and the remaining obligations are tracked in
[Staff progress](../qa/staff-management-progress.md).

### Normal management review UI — unpublished candidate

Submission explicitly offers hospital management responsibility and retains that
typed intent with its immutable command through uncertain outcomes and exact
retry. Omitted legacy intent remains compatible but cannot be approved. Review
shows the retained responsibility and, for non-root requests, the requested
supervisor Staff/posting/revision references.

Only pending hospital-management intent offers the management approval action.
Before showing its form, the client requests the bounded supervision envelope
with `purpose=review`, `supervision_context=v1` and
`management_review=current`, retaining the exact proposal version/revision.
The last selector is invalid without the first two. Duplicate, unknown, empty or
extra selectors return 400. The owner reserves ordinary review, current Staff
actor and separate management-review authority before taking Workforce locks;
the callback must contain the exact management grant. Self/submitter review is
denied. Terminal or non-management intent cannot supply this context.

The response shape is unchanged. Its private disclosure audit records
`context_read_management_review` and the current management assignment ID/version;
these are not added to the public envelope. The SDK exposes a separate
`getStaffPostingManagementReviewContext` and rejects management selectors in its
ordinary getters. Query state is pinned to the current account, hospital and
proposal revision, with no retained cache lifetime.

Approval requires a matching current employment source, source-examination
attestation, explicit independent management-review attestation and rationale.
An uncertain command retains its exact payload for receipt recovery/retry.
Denied or unavailable context offers no approval form. Component/SDK checks do
not establish real-account browser, deployment or lifecycle acceptance.

## Supervisor-chain approval candidate (not released)

The separate supervisor-chain worktree extends the root-review candidate #1565.
It is not deployed, exposed by a normal supervisor-selection UI, or accepted as
full Staff/Pharma operating activation. The first real Global API root-to-child
case passed with the race detector in 76.240s after an observed 409
`posting_supervision_required` before implementation.

Workforce resolves the initial intent's exact same-tenant/hospital canonical
Staff/proposal/approved-revision key. It restores selected-child RLS purpose before
writing. Each ancestor must be its current approved head with retained review
evidence, unchanged Staff version/employment epoch/generation and current reviewed
source cutoff. Only hospital management or a department supervisor may supervise.
A member, self-link, repeated Staff/posting, missing root or chain exceeding 32
postings including the child cannot authorize approval. Nominal parent intervals
contain child intervals; the earliest source cutoff is separately retained without
rewriting ongoing nominal terms.

Organization's existing current-reference/ancestry callback remains authoritative
through commit. A root covers its own hospital; department supervisors cover only
their exact department and current descendants. Workforce never reads Organization
or IAM tables or creates a product-specific Staff master. Ordinary scoped posting
review and current Staff actor use are required before owner locks. Root approval
still requires its separate management capability/attestation; a root-only
attestation does not reclassify a non-root request.

Candidate `0081_workforce_posting_chain_approvals` adds immutable chain-review and
per-hop source evidence with forced tenant/selected-child RLS, composite source
references, bounded hops, indexes and deferred completeness/interval/link checks.
Approval revision, head, all required source evidence, audit and existing canonical
event commit atomically. No old migration is modified. Default/v1 contexts,
public receipt fields, events and IAM/OpenFGA templates remain unchanged;
`grants_access` remains false. The immutable command retry reauthorizes and returns
its original receipt without rebasing supervision or duplicating approval effects.

Canonical supervisor choices, protected reviewed-chain context, UI acceptance,
remaining negative/concurrency/lifecycle tests and independent rollout approval
are still required. See the [increment plan](../superpowers/plans/2026-11-04-posting-supervisor-chains.md).

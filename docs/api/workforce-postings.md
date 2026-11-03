# Workforce posting preparation — implementation candidate

This preparation slice merged as `305294ab` in PR #1524 and is deployed after
specific approval of the revision and migration 0073. Every receipt is a non-operating draft. Workforce owns the draft and immutable commands/revisions; Organization
owns the referenced department/sub-unit. No Staff, employment, credential or
department master is copied. The complete
[posting specification](../superpowers/specs/2026-11-02-effective-staff-postings.md)
and [delivery plan](../superpowers/plans/2026-11-02-effective-staff-postings.md)
remain required, including independent review and operating lifecycle enforcement.

## Transport and current authority

All UI/app traffic enters Global API and routes to Workforce. Each route requires
verified tenant membership, `workforce.posting.prepare`, and current explicit
Workforce hospital reader/manager access. Writes and PUT retries require manager.
The hospital grant and Staff source are held through the write commit. The new
permission confers preparation only; no clinical, professional, payroll, login or
operating authority follows. Scoped posting-preparer catalogs and assignments
remain a separate parent-plan obligation; this draft candidate does not add them.

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

## Unreleased scoped-role foundation

Merged as #1532 at `091f2287`. Its production promotion was approved on 2026-11-04
and has not run yet; see the [release record](../deployment/releases/2026-11-04-scoped-posting-actors.md).

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
see [its contract](organization.md#unreleased-scoped-posting-choices). Scoped draft
consumer composition, posting-purpose RLS and discovery are implemented in the
candidate contract below. Normal positive-role browser acceptance remains open.
This section announces no enabled production route, effective posting or rollout. See the
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

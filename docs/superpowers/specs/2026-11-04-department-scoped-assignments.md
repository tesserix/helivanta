# Department-scoped Staff assignments

Status: in progress. Tracks #429 without closing it. Implements the accepted
[ADR-0055](../../adr/0055-department-scoped-assignments.md). The Organization
guard shipped first in #1562; the IAM, Workforce and UI parts of slice 1 are
implemented as a candidate on `feat/429-department-assignments`, not released.
Extends the [scoped staff access foundation](2026-10-27-scoped-staff-access.md)
and the [scoped posting preparation](2026-11-03-scoped-staff-posting-preparation.md).
Uses ORG-AC-01/02/05/06/07/08 and ORG-ST-02. Settles no `ORG-D*` decision.

## Problem

Every scoped grant is hospital-wide. A Staff posting reviewer for Surgery can
read and reject placements into Psychiatry, and a hospital has no way to give
a department head review authority over only their own department. #429's
clinical reach (encounters, referrals, sensitive departments) needs the same
department scope, so the scope itself is built first, with Staff posting review
as its first consumer.

## Authority

| Actor                       | Authority kind | Template                                       | Scope                                                  | Preconditions                                                     | Approver                 |
| --------------------------- | -------------- | ---------------------------------------------- | ------------------------------------------------------ | ----------------------------------------------------------------- | ------------------------ |
| Hospital IAM administrator  | Administration | Existing `iam.assignment.manage` holder        | Hospital                                               | Current Workforce manager grant at the hospital                   | ORG-D7 rules, unchanged  |
| Independent IAM reviewer    | Administration | Existing `iam.assignment.review` holder        | Hospital                                               | Not the proposer or the target                                    | —                        |
| Department posting reviewer | Supervision    | New `workforce.department_posting_reviewer.v1` | One department or sub-unit and its current descendants | Reviewed Staff actor link; not the preparer, submitter or subject | Independent IAM reviewer |
| Hospital posting reviewer   | Supervision    | Existing `workforce.posting_reviewer.v1`       | Hospital, unchanged                                    | Unchanged                                                         | Unchanged                |

The new template carries the same five permissions as
`workforce.posting_reviewer.v1`: `workforce.posting.read`,
`workforce.posting.proposal.read`, `workforce.posting.review`,
`organization.posting_choices.read` and `workforce.staff_actor.use`. Its
`scope_kinds` is `["department"]`. Every existing template declares
`["hospital"]`.

## Objects

| Object                        | Owner     | Containing scope         | 404                                                                        | 403                                 | 503                                                     |
| ----------------------------- | --------- | ------------------------ | -------------------------------------------------------------------------- | ----------------------------------- | ------------------------------------------------------- |
| Department-scoped proposal    | IAM       | Hospital                 | Foreign tenant or hospital; an unknown department ends in `scope_rejected` | Missing `iam.assignment.manage`     | IAM unavailable; owner outages leave it `pending_scope` |
| Posting proposal under review | Workforce | Hospital, department     | Department outside every held department subtree                           | Held department but no review grant | IAM, FGA or Organization down                           |
| Posting review inbox          | Workforce | Held department subtrees | No held department and no hospital grant                                   | —                                   | IAM, FGA or Organization down                           |

## Numbers

Inherit the Staff planning budgets: 50 peak requests/s, 80:20 reads to writes,
p99 under 500 ms, 99.9% availability. These are unmeasured. New bounds:

- An Organization path is at most 8 nodes (the persisted depth limit).
- A subtree read returns at most 1,000 nodes. A larger subtree fails closed
  with 503 and a logged `department_subtree_limit` reason. A 600-bed hospital
  has roughly 40 departments and 150 sub-units, so the cap leaves headroom.
- A subject holds at most 50 current department grants per hospital and
  permission. Discovery checks each one in OpenFGA, so 50 is also the per-call
  graph-check bound.
- Command checks run at most 9 graph checks: the hospital candidate plus one
  per node on the target's path.

## Design

### Contract and data

`contracts/staff-access.json` changes, with Go and Zod regenerated:

- `TemplateDefinition.scope_kinds`: required, non-empty, subset of
  `hospital` and `department`.
- `ProposalInput.department_id`: optional UUIDv7. When it is present, the
  template must allow `department`. When it is absent, the template must allow
  `hospital`.
- `ProposalOutput`, `ProposalHistory` and `ProposalDecisionRecord` gain
  `scope_kind` and nullable `department_id`.
- New event models `DepartmentScopeRequest` and `DepartmentScopeResult` carry
  only the tenant, proposal, request, hospital and department IDs, plus
  eligibility. They carry no subject, reason or Staff details.

IAM migration (next free ID at implementation, expand only):

- `iam_assignment_proposals` and `iam_assignment_snapshots` gain `scope_kind
text NOT NULL DEFAULT 'hospital' CHECK (scope_kind IN ('hospital','department'))`
  and `department_id uuid`, with `CHECK ((scope_kind='hospital') = (department_id IS NULL))`.
- The proposal guard function is replaced with one that also refuses any change
  to `scope_kind` or `department_id` after insert. The replacement extends the
  current function body; it does not drop earlier checks.
- A new `AS RESTRICTIVE` policy on each table hides `department` rows unless
  `current_setting('app.assignment_department_scope',true)='enabled'`.
  Restrictive policies are ANDed with the existing tenant policies, so they only
  narrow them. Only the new IAM code paths set the setting. Code that does not
  understand department scope, including rolled-back code, never sees those
  rows.
- `iam_assignment_department_verifications` records the Organization result
  per proposal and request, with the same append-only and RLS rules as the
  Workforce verification.

Organization migration: none. Organization reads its existing tables.

### Proposal and decision

1. The proposer submits `department_id` with a department-capable template.
   IAM commits `pending_scope` and publishes both the existing Workforce scope
   request and a new
   `helivanta.in.iam.assignment_department_scope_requested.v1` event, in one
   transaction.
2. Organization consumes the request and answers eligible only for an active
   node of that hospital. It publishes
   `helivanta.in.organization.assignment_department_verified.v1`, following the
   department-link pattern from ADR-0051.
3. IAM moves to `pending_review` only when both owners have answered eligible
   for the current request. Either refusal moves it to `scope_rejected`.
   Unknown, stale, duplicate or out-of-order answers change nothing.
4. A decision enters `pending_decision_scope` and asks both owners again.
   Approval needs both fresh answers. A department retired in between rejects
   the approval and returns the proposal to `pending_review`.
5. Projection writes `ScopedAssignment{ScopeKind: "department", ScopeID:
department_id}`. `objectKey` accepts `department` only with a UUIDv7
   `ScopeID` that is not the hospital ID.

### Organization guard

`pkg/authz` gains `DepartmentScopeGuard`, implemented by Organization and wired
through `cmd/api/main.go`, alongside `DepartmentReferenceGuard`:

- `CurrentPath(ctx, tenant, hospital, department) ([]uuid.UUID, error)`
  returns the active path from the node to its root. It is a pre-command hint
  and never authority.
- `CurrentSubtree(ctx, tenant, hospital, roots) ([]uuid.UUID, error)` returns
  the active nodes under the roots, up to the 1,000-node limit.
- `WithinCurrentDepartment(ctx, tenant, hospital, target, scope, action)` runs
  only inside a guarded command. It takes the shared advisory lock, locks the
  target's path rows `FOR SHARE` in ID order, requires every node active and
  `scope` on the path (or equal to `target`), then runs `action`. Unknown nodes
  return `ErrDepartmentReferenceHidden`. A path change returns
  `ErrDepartmentReferenceChanged`.

### Authority selection

`authz.ScopeRequest` gains `DepartmentPath []string`. `ScopeGrant` gains
`ScopeKind` and `DepartmentID`.

- With no path, IAM selects only `hospital` assignments, exactly as today.
- With a path, IAM selects `hospital` assignments and `department` assignments
  whose `department_id` is on the path. Hospital grants are tried first.
- `WithScopeGrant` locks the chosen assignment as today. For a department
  grant, the consumer must then call `WithinCurrentDepartment(target,
grant.DepartmentID)` before reading or writing. The Workforce command helper
  does this in one place, so no route can skip it.
- A new `DepartmentScopes(ctx, ScopeRequest)` returns the subject's current
  department grants for a permission in a hospital, each checked against the
  graph and the epoch. Inbox discovery uses it.

Department and hospital assignments share the `(tenant, hospital, subject)`
scope epoch, so any status change invalidates both.

### Posting review consumer

- **Inbox.** For each hospital, Workforce resolves the actor's grants. A
  hospital grant lists everything, as today. Otherwise Workforce reads the
  subtree of the held departments and lists proposals whose `department_id` is
  in it, with the existing keyset pagination. The list is a snapshot, labelled
  with the department name.
- **Read, reject and later approve.** The command reads the target path,
  requests authority with it, locks Workforce rows, and then runs
  `WithinCurrentDepartment`. A proposal outside every held subtree returns 404.
- **Posting choices.** A department-scoped reviewer sees only nodes in their
  held subtrees.
- **Independence.** The existing rules still apply: no self-review, and the
  preparer or submitter cannot decide.

### UI

- **Access proposals.** The form adds "Applies to" with two choices: "Whole
  hospital" or "One department and its sub-units". The department list is a
  `FormSelectField` filled from the Organization department list
  (`organization.structure.read`), offering only nodes that are active and
  already effective. The proposer holds `iam.assignment.manage`, which, like
  structure read, defaults only to tenant administrators; posting choices are
  not used because they also need a Staff actor link. A 403 explains the
  missing structure access without a retry. IAM and Organization verify the
  department again server-side. Only templates that allow the chosen scope are
  offered. Review and the access list show the scope by department name; the
  scope is immutable, so history rows inherit the card's scope line.
- **Posting review inbox.** Each row shows its department. A reviewer with
  only department grants sees the departments they cover in the page header.
- **Error states.** A retired department reads "Department retired. This access
  no longer applies". Organization being unavailable shows the standard
  unavailable state and never retained rows.

## Failure behaviour

- If Organization is down, department proposals stay in `pending_scope`.
  Department commands and inboxes return 503. Hospital-scoped work is
  unaffected.
- If the path changes between the hint and the lock, the command returns 409
  `department_scope_changed`, and the client re-reads. It never allows.
- Retiring the assignment's department, or any ancestor of the target, ends
  the grant's effect immediately. History is kept, and an administrator revokes
  the assignment explicitly.
- Moving a sub-unit under a department the actor holds brings it into scope.
  Moving it out removes it. Both take effect at the next locked check.

## Delivery slices

1. **Department scope and posting review (this specification).** The
   Organization guard shipped first in #1562. Contract, IAM migration,
   verification, projection, authority selection, posting review consumer and
   UI follow in one PR.
2. **Department leave review.** It needs effective postings from the
   supervision slice to know a requester's department. It adds
   `workforce.department_leave_reviewer.v1` and composes with reporting-line
   routing.
3. **Clinical department reach (#429 body).** Encounter reach, referrals,
   sensitivity clearance (#426), treating relationships (#430) and
   roster-derived membership get their own specification.
4. **Outlet scope (#418).** Pharma outlets as a scope kind follow the same
   pattern with Pharma as the verifying owner.

## Required tests

- **Real Postgres and OpenFGA.**
  - A department grant allows a proposal in that department and in a sub-unit,
    and returns 404 for a sibling department and for a parent department.
  - A hospital-only request never matches a department grant.
  - A foreign tenant or hospital returns 404.
- **Rollback guard.** A session without `app.assignment_department_scope`
  cannot select or project department rows. Prove the assertion fails when the
  policy is removed.
- **Races, with deterministic barriers.**
  - Retirement before admission denies.
  - A reparent between the hint and the lock returns 409.
  - An admitted command commits before a later retirement.
  - Revocation advances the epoch and denies.
- **Verification.** Both owners must answer. Stale, duplicate and out-of-order
  answers are ignored. An Organization refusal ends in `scope_rejected`. A
  department that retires before approval sends the proposal back to
  `pending_review`.
- **Limits.** 1,001 subtree nodes return 503. 51 department grants are
  bounded.
- **Contract and inventory.** Generator and contractdocs parity pass, and the
  inventory of registered surfaces classifies every new route.
- **Browser, through the Global API.** An administrator proposes department
  access, and an independent reviewer approves it. The department reviewer
  sees only their department's proposals and rejects one. Then the department
  retires, and the reviewer loses access on reload.

## Not covered

Clinical encounter reach, referrals, sensitivity labels, roster-derived
membership and emergency override stay in #429's clinical scope, or in #426,
#430 and #432. Department heads cannot propose access for their own members
yet; proposing stays a hospital administrator task. Outlet scope belongs to
#418. Standalone pharmacy establishments ([ADR-0054](../../adr/0054-typed-employing-establishments.md))
use outlet, not department, scope.

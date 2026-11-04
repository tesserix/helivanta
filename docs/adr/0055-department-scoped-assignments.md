# ADR-0055: Department-scoped IAM assignments

Date: 2026-11-04. Status: accepted on 2026-11-04, when the product owner asked
for the Staff work to be completed. It settles no open
`ORG-D*` decision. Related: #429, #417, #418,
[ADR-0044](0044-organization-structure-and-access-defaults.md),
[ADR-0051](0051-shared-department-owner-boundary.md),
[ADR-0052](0052-staff-source-command-authorization.md) and
[ADR-0053](0053-reviewed-managerial-posting-supervision.md). Delivery detail is
in the [department-scoped assignment specification](../superpowers/specs/2026-11-04-department-scoped-assignments.md).

## Context

A hospital runs most staff work through departments. A head of Surgery reviews
placements into Surgery and its wards, not into Psychiatry. Helivanta cannot
express that today. Source on 2026-11-04:

- `authz.ScopedAssignment` admits only the `hospital` and `staff` scope kinds
  (`backend/pkg/authz/scoped_assignment.go`). IAM writes only `hospital`: the
  authority provider (`iam/assignment_authority.go`) and the projection worker
  (`iam/assignment_projection.go`) both hard-code it.
- `iam_assignment_proposals` and `iam_assignment_snapshots` carry
  `hospital_id` and no other scope column. Every scoped grant is hospital-wide.
- Organization owns departments and sub-units (ADR-0044). Rows are `department`
  (no parent) or `sub_unit` (one parent), `active` or `retired`, at most eight
  deep. A parent can change, and a department cannot retire while it has
  active children. `DepartmentReferenceGuard` already checks current ancestry
  under shared locks inside a guarded command.
- Posting proposals carry `department_id` and `department_revision_id`
  references to an Organization node.

The [access model](../domain/organization/access-model.md) names department
and sub-unit as assignment scopes (ORG-AC-02) and suggested OpenFGA `department`
types with parent relations. That approach needs a second hierarchy copy in the
graph, with its own epoch, kept in step with Organization's revisions.

## Decision

1. **Scope kind `department`.** An assignment may name one Organization node,
   department or sub-unit, inside its hospital. It covers that node and every
   current descendant. The hospital scope keeps covering every department.
   A request that names no department never matches a department assignment,
   so a department grant can never widen to the hospital.
2. **Organization answers ancestry; the graph does not.** IAM keeps projecting
   content-bound assignment keys with the department ID inside the hashed
   snapshot. OpenFGA gets no department type and no parent tuples. Whether a
   target department currently lies under the assignment's department is
   decided by Organization, under shared row locks, inside the consumer's
   guarded command. This keeps one hierarchy owner and no second epoch.
3. **Lock order stays ADR-0052's.** IAM locks the assignment rows first, then
   the consumer locks its own rows, then Organization locks the department
   path. Before the command, the consumer reads Organization's current path for
   the target as a hint. IAM uses the hint only to pick candidate assignments,
   and the locked Organization check is the authority. A path that changed in
   between fails the command; it never allows.
4. **Templates declare scope kinds.** A template definition gains an explicit
   `scope_kinds` list. Existing templates are `["hospital"]`, and their
   definitions are unchanged. Department use needs a new versioned template
   key, so no approved v1 assignment changes meaning.
5. **Owner verification follows ADR-0051.** A department proposal adds an
   Organization verification to the existing Workforce one. IAM records
   `pending_review` only when both owners confirm, and approval needs both to
   confirm again. Organization confirms only an active node in that hospital.
6. **Old code cannot see department rows.** A restrictive row-level security
   policy hides department-scoped proposals and snapshots unless the session sets
   `app.assignment_department_scope`. Only code that handles department scope
   sets it. Rolled-back or unaware code then fails closed: it never projects
   or selects a department grant as a hospital grant.

## Status codes and failure

An object outside every department the actor holds returns 404. A permission
missing inside a held department returns 403. Organization, IAM or OpenFGA
being unavailable returns 503. A retired assignment department authorizes
nothing from the moment it retires, because the locked check requires every
node on the path to be active. While Organization is down, department commands
return 503, and hospital-scoped commands are unaffected.

## Migration and rollback

Expand only. New nullable `department_id` and `scope_kind` columns default to
`hospital`, so every existing row keeps its meaning. Rollback leaves the
columns, and point 6 keeps rolled-back code from seeing department rows.
Department assignments stop authorizing; hospital assignments are unchanged.
Re-enabling needs no data repair.

## Rejected

- **OpenFGA department hierarchy.** It needs tuples for every node and parent
  change, a projection epoch, and a recheck against Organization anyway. Two
  hierarchy copies would drift.
- **Copy departments into IAM.** IAM would become a second structure owner,
  which ADR-0044 forbids.
- **Exact-node assignments only.** A head of Surgery would need one assignment
  per ward, and a new ward would silently fall outside every head's scope.
- **Widening v1 templates to department scope.** That changes approved grants'
  meaning without review.
- **One synchronous IAM call into Organization.** IAM would then lock
  Organization rows before the consumer's rows, inverting ADR-0052's order.
- **Clinical encounter reach in the same change.** Care-team reach, referrals,
  sensitivity clearance and roster-derived membership are #429's clinical
  scope. They depend on #426 and #430 and on effective postings, so they get
  their own specification.

# Department-scoped leave review

Status: design, implemented in the same change. Resolves #1596. Slice 2 of the
accepted [ADR-0055](../../adr/0055-department-scoped-assignments.md) and of the
[department-scoped assignments specification](2026-11-04-department-scoped-assignments.md).
Composes with [reporting-line routing](2026-11-04-leave-reporting-line-routing.md)
and the [scoped leave consumer](2026-10-27-scoped-leave-consumer.md). Uses
ORG-AU-05, ORG-AC-01/02/05/08 and ORG-ST-02. Settles no `ORG-D*` decision.

## Problem

`workforce.leave_reviewer.v1` is hospital-wide. Routing narrows it to the
reporting line, but a hospital cannot give a head of department or a nursing
in-charge leave review over their own department alone. Slice 1 built the
department scope and the Organization guard. This slice uses them for leave.

## Authority

| Actor                     | Authority kind | Template                                          | Scope                                                  | Preconditions                                                     | Approver                 |
| ------------------------- | -------------- | ------------------------------------------------- | ------------------------------------------------------ | ----------------------------------------------------------------- | ------------------------ |
| Department leave reviewer | Supervision    | New `workforce.department_leave_reviewer.v1`      | One department or sub-unit and its current descendants | Active Staff with a reviewed login link; not the filer or subject | Independent IAM reviewer |
| Hospital leave reviewer   | Supervision    | Existing `workforce.leave_reviewer.v1`            | Hospital, unchanged                                    | Unchanged                                                         | Unchanged                |
| Hospital leave desk       | Management     | Hospital `manager` plus `workforce.leave.approve` | Hospital, unchanged                                    | Unchanged                                                         | Unchanged                |

The new template has family `leave`, `scope_kinds: ["department"]` and three
permissions:

- `workforce.leave.approve` decides the leave.
- `organization.posting_choices.read` and `workforce.staff_actor.use` let the
  page name the held departments through the existing Organization
  `posting-department-choices` read, which needs both at department scope.
  Neither grants any leave or posting decision.

It adds no permission to any existing template. A legacy role holding
`workforce.leave.approve` gains nothing on the department routes.

## Objects

| Object                 | Owner     | Containing scope         | 404                                                                                                    | 403                                                | 503                                                     |
| ---------------------- | --------- | ------------------------ | ------------------------------------------------------------------------------------------------------ | -------------------------------------------------- | ------------------------------------------------------- |
| Department leave queue | Workforce | One held subtree         | Unknown, retired or foreign department; a department outside every held grant                          | No department leave grant anywhere in the hospital | IAM, OpenFGA or Organization down; a bound exceeded     |
| Request detail         | Workforce | Requester's live posting | Requester outside the subtree, posted elsewhere too, or with no live posting; routed with no open step | As above; self or filer decision                   | As above, or the requester's posting sources unreadable |
| Department decision    | Workforce | Requester's live posting | As detail                                                                                              | As detail                                          | As detail                                               |

## Requester's department

A request is in a department reviewer's scope when **every** live approved
posting of the requester sits in the held subtree, and there is at least one.

- **Live** is the owner's per-posting current-use check from ADR-0058 and
  ADR-0060: the proposal is approved at its current revision, its employment
  generation is effective and matches the posting's Staff version and epoch,
  it has started, and the check time is before the earliest of its nominal
  end, eligibility end, generation end and retained cutoff. The code reuses
  `currentPostingUseDeadline` and the lifecycle cutoff observation. It does not
  copy them.
- **The ancestor chain is not checked.** ADR-0058 ends a descendant's posting
  _authority_ when an ancestor is cut off. Leave review uses the posting only to
  locate where the requester works, never as authority. Ending a ward
  in-charge's posting must not push the whole ward's leave to the desk.
- **Staff posted in two departments** are in scope only for a reviewer whose
  subtree holds both. Otherwise the hospital reviewer or the desk decides.
  Pulling them into either department would let one head decide leave that
  another head also owns.
- **Staff with no live posting** are outside every department scope.

Each call names one held department with a required `department` query
parameter, as the posting inbox does. The scope is that department's current
subtree. A reviewer holding two departments uses them one at a time.

## Routes

These are new, alongside the hospital routes. The hospital routes and their
callers stay unchanged.

| Method | Path under `/workforce/hospitals/:hospital`                          | Helper                        |
| ------ | -------------------------------------------------------------------- | ----------------------------- |
| GET    | `/department-leave-review-requests?department=`                      | `HandlerScopedQueryListRoute` |
| GET    | `/staff/:id/department-leave-review-requests/:request?department=`   | `GETHandlerScoped`            |
| POST   | `/staff/:id/leave-requests/:request/department-decision?department=` | new `POSTHandlerScoped`       |

Each route declares `workforce.leave.approve` with department scope. The
handler must prove the matched assignment through `AdmitHandlerScope`, or the
buffered success becomes a 500.

- The queue and detail return the existing `LeaveReviewRequest` and
  `LeaveRequestDetail` shapes.
- The decision takes the existing `LeaveDecisionInput` and returns the
  existing detail.
- `GET /workforce/leave-review-scopes` adds to each hospital `scope_kind`
  (`hospital` or `department`) and `department_ids`, the held department roots.
  These are additive fields.

## Command order

1. **Hint and authority.** Read the hint's current path from Organization
   (`CurrentPath`, at most 8 nodes). `AuthorizeScope` runs with that
   `DepartmentPath`, and the chosen grant must be a department grant on the
   path. `WithScopeGrant` locks it. No grant: 404 when the caller holds a
   department leave grant elsewhere in the hospital, otherwise 403.
2. **Subtree.** Read `CurrentSubtree([hint])`, at most 1,000 nodes, which must
   contain the hint. Hold the hint with `WithinCurrentDepartment(hint,
grant.DepartmentID)`. A path change returns 409 `department_scope_changed`.
3. **Ledger and supervision.** The decision takes the leave-ledger advisory
   lock, then `workforce_lock_supervision`, so a concurrent posting approval or
   cutoff commits wholly before or after it. Reads take neither lock.
4. **Actor.** `lockReviewedLeaveActor` locks the reviewer's Staff row and login
   link. Self-decision is refused as today.
5. **Requester postings.** Read at most 8 approved postings whose current
   revision covers the check time. More than 8 fails closed with 503. Run the
   live check on each, and require at least one live posting and every live
   department in the subtree. The decision then holds each live department with
   `WithinCurrentDepartment(department, grant.DepartmentID)` and constrains the
   guarded command to the earliest live deadline.
6. **Request.** Set the leave review flags and the department flags, lock the
   request, then run the existing decision body unchanged. That covers version,
   replay, year closure, balance and the routing guard.

The audit evidence adds `scope_kind: "department"`, `department_id` (the grant)
and `requester_departments`, as identifiers only.

## Database enforcement (Workforce 0089)

The leave review flags are already bound to one staff member and one request.
This migration adds a restrictive guard for department sessions:

- `workforce_leave_department_session()` is true when
  `app.workforce_leave_department='authorized'` inside a leave review or read
  session.
- `workforce_leave_department_candidate(t,h,staff)` is true when the staff
  member has an approved posting whose current revision covers `now()` in a
  department listed in `app.workforce_leave_department_subtree`.
- Restrictive policy `leave_department_review_guard` on
  `workforce_leave_requests`:
  `NOT workforce_leave_department_session() OR workforce_leave_department_candidate(...)`.
  Every table reached through the leave review functions joins the request, so
  it inherits the guard, as routing does.
- A permissive SELECT branch on `workforce_posting_proposals` and
  `workforce_posting_proposal_revisions` lets a department session read only
  approved posting rows. It never writes, and the rows never leave the server.
- `workforce_posting_proposal_context` gains a read-only `leave_department`
  mode, bound to `workforce.leave.approve`, so the owner live check can read the
  requester's posting sources inside the leave transaction.

The database guard is a necessary condition, enforced for every query. The
exact live check in Go narrows it further. Leaving the department flag unset
selects the existing policies unchanged, so rolled-back code is unaffected.

## IAM (0032)

- Template `workforce.department_leave_reviewer.v1`, as above.
- Catalog `staff-department-leave-reviews-v1`.

The generator rule changes from "only the department posting reviewer may
declare department scope" to an allow-list of the two reviewed department
templates, each declaring `["department"]` alone.

## Numbers

The Staff budgets apply: 50 peak requests/s, 80:20 reads to writes, p99 under
500 ms and 99.9% availability. These are unmeasured.

- Path ≤ 8 nodes, subtree ≤ 1,000 nodes, ≤ 50 department grants per subject and
  permission, all inherited.
- At most 8 covering postings per requester. A requester normally has one.
- The queue page is at most 10 rows. The SQL guard filters candidates by an
  indexed posting probe, and each row then costs one bounded live check.
  That is at most 11 × 8 posting checks per page, with five to seven queries
  each, and about 11 × 7 queries typically.

## UI

The leave review page reads `leave-review-scopes`.

- A hospital held only by department grants shows a department selector, named
  through `useStaffPostingReviewDepartmentNames`.
- The queue, detail and decision use the department routes for the selected
  department. Rows keep the existing direct and escalated badges.
- A 404 after a transfer or retirement reads "This request is no longer in your
  department. Reload the queue."
- Organization being unavailable shows the standard unavailable state and never
  retained rows.

The access page offers the new template only with "One department and its
sub-units".

## Failure behaviour

- If IAM, OpenFGA or Organization is down, the department routes return 503.
  The hospital routes are unaffected.
- A requester transferred out of the subtree, or given a second posting
  elsewhere, leaves scope at the next locked check.
- A routed request with no open step for the reviewer returns 404. The
  existing route guard enforces this.
- A department retired between the hint and the lock returns 409 or 404, and
  never allows.

## Required tests

- **Real Postgres and OpenFGA.**
  - A department grant decides a requester posted in that department or a
    sub-unit.
  - It returns 404 for a sibling, a parent, a foreign tenant and a foreign
    hospital.
  - It returns 404 for a requester posted in two departments, one outside, and
    for a requester with no live posting.
  - It returns 404 for a requester whose only posting has a retained cutoff in
    the past.
  - A hospital grant never matches the department routes, and a department
    grant never matches the hospital routes.
  - A routed request without an open step returns 404. With an open step it
    succeeds.
  - Self-decision is refused.
- **Rollback guard.** Removing `leave_department_review_guard` makes the
  database-level queue test fail.
- **Races, with barriers.**
  - A cutoff that commits first denies the decision.
  - A reparent between the hint and the lock returns 409.
  - Revocation denies.
- **Limits.** Nine covering postings return 503. 1,001 subtree nodes return 503.
- **Contract.** The generator, contractdocs and inventory parity pass.
- **Global API journey and browser.** A department reviewer sees only their
  department's requests and approves one. A sibling department's request is
  absent and returns 404 by direct URL.

## Not covered

- Department heads proposing access for their own members.
- Delegation and acting reviewers.
- Coverage staffing checks.
- Self-service filing (#1132).
- Clinical department reach (slice 3).
- Outlet scope (#418).
- Leave revocation stays a desk action.

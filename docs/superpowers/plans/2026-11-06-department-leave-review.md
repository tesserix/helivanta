# Department leave review — implementation plan

**Goal:** #1596, ADR-0055 slice 2. Give the department leave reviewer the
department queue, detail and decision, with database enforcement, audit, UI,
SDK, contract and inventory.

**Spec:** `docs/superpowers/specs/2026-11-06-department-leave-review.md`
**ADR:** `docs/adr/0055-department-scoped-assignments.md`

## Global constraints

- Starts from `main` at `5a35bc93`. The next free IDs are IAM `0032` and
  Workforce `0089`. Both migrations are expand only.
- `contracts/staff-access.json` is canonical. Regenerate Go and Zod, then run
  `go run ./cmd/contractdocs` and `-check`.
- The hospital leave routes, their SQL and their tests are untouched. Each
  department route admits only a department grant.
- The posting live check is reused from `currentPostingUseDeadline` and the
  lifecycle cutoff observation. It is not reimplemented in SQL.
- Status codes:
  - 404 for a request outside the subtree, a hidden hint or an absent grant;
  - 403 when the caller holds no department leave grant in the hospital;
  - 503 when an owner is unavailable or a bound is exceeded;
  - 409 `department_scope_changed`.

## Task 1: IAM template and catalog

- Add `workforce.department_leave_reviewer.v1` and catalog
  `staff-department-leave-reviews-v1` to `contracts/staff-access.json`, with IAM
  migration `0032`.
- Generator rule: the department scope allow-list holds both reviewed
  templates, each declaring `["department"]` alone.
- Red first: the generator test rejects any other template declaring
  department scope, and accepts the new one. The append test sees the new
  template.

## Task 2: Workforce 0089 database guard

- Add the `workforce_leave_department_session` and
  `workforce_leave_department_candidate` functions, and the restrictive
  `leave_department_review_guard` on `workforce_leave_requests`.
- Add the approved-only permissive SELECT branch on posting proposals and
  revisions for department sessions.
- Replace `workforce_posting_proposal_context` with the read-only
  `leave_department` mode added.
- Red first:
  - a department session sees a requester posted in the subtree and not a
    sibling's requester;
  - a session without the flag is unchanged;
  - the assertion fails when the restrictive policy is dropped.

## Task 3: router and authority

- `POSTHandlerScoped` mirrors `GETHandlerScoped`.
- `leaveDepartmentTransaction` in Workforce implements the spec's command order
  in one place. All three routes call it, so none can skip a step.
- Red first: a router test shows that a POST handler that does not admit its
  scope turns its success into a 500.

## Task 4: requester live postings

- `leaveRequesterDepartments(tx, …)` reads at most 9 covering approved
  postings, fails with 503 above 8, and runs the owner live check on each.
- Red first, with real Postgres:
  - one live posting in the subtree;
  - a sibling posting;
  - two postings with one outside;
  - none live;
  - a cutoff in the past;
  - nine postings.

## Task 5: queue, detail and decision handlers

- Queue: `HandlerScopedQueryListRoute` with the `department` filter, limit 10,
  keyset over the pending requests the database guard admits, then the live
  check per requester. Rows are audited as `queue_read`.
- Detail and decision reuse `loadLeaveRequestDetail` and the existing
  decision body. The audit adds `scope_kind`, `department_id` and
  `requester_departments`.
- Red first, through the real module harness with OpenFGA:
  - every 404, 403 and 503 case in the spec;
  - routed with and without an open step;
  - self-decision;
  - a cutoff race and a reparent race with barriers.

## Task 6: discovery, SDK and UI

- `leave-review-scopes` gains `scope_kind` and `department_ids`.
- SDK: `listDepartmentLeaveReviewRequests`, `getDepartmentLeaveReviewRequest`
  and `decideDepartmentLeaveRequest`, plus the schemas.
- Leave review page: department selector, department routes and the
  out-of-scope message. The access page offers the template at department
  scope.
- Red first: Vitest for the selector, the routing of calls and the 404 copy.

## Task 7: contract, inventory and journey

- Run `go run ./cmd/contractdocs` and `-check`, and update the registered
  inventory.
- Add a Global API journey test for a department reviewer approving through
  `/v1/workforce`.
- Gates:
  - `make lint-go`
  - `./scripts/coverage-gate.sh`
  - `go test -race ./...`
  - `pnpm turbo lint type-check test build format:check`

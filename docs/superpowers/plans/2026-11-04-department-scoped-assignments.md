# Department-scoped assignments and posting review — implementation plan

**Goal:** #429 slice 1, after the Organization guard (#1562): department
assignments in IAM, department authority selection, and the department
posting reviewer in Workforce and the shell.

**Spec:** `docs/superpowers/specs/2026-11-04-department-scoped-assignments.md`
**ADR:** `docs/adr/0055-department-scoped-assignments.md`

## Global constraints

- Starts from `main` after the posting supervision branch merges. It edits
  `contracts/staff-access.json`, the template generator, IAM proposals,
  `posting_authority.go` and `scoped_authority.go`; rebase rather than
  editing in parallel.
- One IAM migration, next free ID at implementation (`0026` is reserved by
  supervision, so expect `0027`; Workforce `0079`–`0080` are also taken). Expand only; Organization has no
  migration.
- `contracts/staff-access.json` is canonical. Regenerate Go and Zod, then
  `go run ./cmd/contractdocs` and `-check`.
- A request without `DepartmentPath` behaves exactly as today. Every existing
  template declares `scope_kinds: ["hospital"]`.
- Department rows are invisible unless `app.assignment_department_scope` is
  `enabled` (restrictive policy), so rolled-back code fails closed.
- 404 outside every held subtree, 403 held department without the permission,
  503 for IAM, OpenFGA or Organization failure, 409 `department_scope_changed`.

## Task 1: contract and IAM storage

- `TemplateDefinition.scope_kinds`, `ProposalInput.department_id`, and
  `scope_kind`/`department_id` on proposal output, history and decision
  records. New `DepartmentScopeRequest`/`DepartmentScopeResult` events.
- New template `workforce.department_posting_reviewer.v1` with the five
  permissions of `workforce.posting_reviewer.v1` and `["department"]`.
- Migration: columns and paired CHECK on proposals and snapshots; guard
  function extended to refuse scope changes after insert; restrictive
  policies; append-only `iam_assignment_department_verifications` with RLS.
- Red first: insert with mismatched kind/ID fails; update of `department_id`
  fails; a session without the setting sees no department rows; the
  assertion fails when the policy is dropped.

## Task 2: two-owner verification

- `put` validates the template against the chosen scope and publishes both the
  Workforce scope request and
  `helivanta.in.iam.assignment_department_scope_requested.v1` in one
  transaction.
- Organization consumer `department_assignment_verifications.go` follows
  `department_observations.go` for the strict envelope and audit, but answers
  from current structure without its own table (Organization has no
  migration); IAM's append-only verifications keep the first answer per owner
  and request, so a redelivered request cannot change a settled answer.
- IAM consumer records each answer and moves to `pending_review` only when
  both are eligible for the current request; either refusal is
  `scope_rejected`. The decision path re-asks both; a retired department
  returns the proposal to `pending_review`.
- Red first: one owner answering alone changes nothing; stale, duplicate and
  out-of-order answers are ignored; Organization down leaves `pending_scope`.

## Task 3: projection and authority selection

- Projection writes `ScopeKind: "department", ScopeID: department_id`.
  `objectKey` accepts `department` only with a UUIDv7 ID unequal to the
  hospital.
- `ScopeRequest.DepartmentPath`, `ScopeGrant.ScopeKind`/`DepartmentID`.
  Candidates: hospital first, then department grants on the path; at most
  nine graph checks per command.
- `DepartmentScopes(ctx, ScopeRequest)` lists current department grants,
  bounded at 50, each checked against the graph and epoch.
- Red first: a hospital-only request never matches a department grant; a
  sibling or parent department is denied; revocation advances the shared
  epoch and denies; 51 grants fail closed.

## Task 4: Workforce posting review

- `scopedPostingTransaction` reads the target path from the guard, requests
  authority with it, and for a department grant runs
  `WithinCurrentDepartment` inside the held grant. No route calls IAM
  directly, so none can skip the check.
- Workforce SQL context records the grant's scope kind and department so
  audit and guard functions bind the department used.
- Inbox: hospital grant lists all; otherwise read `CurrentSubtree` of the held
  departments and filter by `department_id` with the existing keyset.
- Organization posting choices are filtered to the held subtrees.
- Red first, with barriers: retirement before admission denies; reparent
  between hint and lock is 409; an admitted command commits before a later
  retirement; 1,001 subtree nodes is 503; self-review still refused.

Decisions settled before code:

- Admission: reviewer routes move from hospital-only `ScopedHospital*`
  admission to `markScopedPosting`, so every check runs in
  `scopedPostingTransaction`; the archtest inventory records each route.
- Target: a hospital grant needs no department. Otherwise the client echoes
  the row's `department` from the inbox as a hint; authority is the grant, the
  locked Organization path and RLS binding the row's department to that hint,
  so a wrong hint is 404 and never discloses. No pre-authorization row read.
- Workforce migration `0081` (`0079`–`0080` stay reserved for supervision).

## Task 5: shell

- Access proposal form: "Applies to" with "Whole hospital" or "One department
  and its sub-units", department `FormSelectField` from the Organization
  department list (active, already effective nodes; readable 403), templates
  filtered by scope. Review and the access list show the department name;
  history inherits the immutable card scope.
- Posting review inbox shows each row's department and the covered
  departments in the header; retired and unavailable states per the spec.
- Vitest with `renderWithProviders` for each changed panel.

## Task 6: docs and evidence

- `docs/api/organization.md`, the Staff access and posting docs, the tracker
  and the operational access inventory for every new route and consumer.
- Correct the spec's delivery slice 1: the Organization guard shipped first
  in #1562.
- Browser acceptance through the Global API, as listed in the spec.

## Review focus

- A department grant reaching a proposal in a sub-unit moved in after
  approval, and losing it when moved out.
- Rolled-back code with department rows present.
- An Organization answer for a request superseded by a newer one.

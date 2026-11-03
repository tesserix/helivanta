# Reporting-line leave approval with escalation

Resolves [#1536](https://github.com/tesserix/helivanta/issues/1536). Extends the
[leave requests design](2026-10-15-leave-requests-design.md), the
[scoped leave consumer](2026-10-27-scoped-leave-consumer.md) and the
[acyclic supervision guard](2026-10-28-staff-supervision-cycles.md). Uses
ORG-AU-05, ORG-AC-01/02/05/08 and the configurable-workflows fallback rule. It
does not settle ORG-D2/D5/D6.

## Problem

A `workforce.leave_reviewer.v1` holder can read and decide every pending request
in the hospital. Hospitals run leave through the reporting line: the reporting
officer decides first, a slow or absent officer's requests move up the chain,
and the hospital leave desk has final authority. Today nothing routes a request,
and nothing stops an unrelated ward lead from deciding a stranger's leave.

## Authority

| Actor                          | Authority kind | Grant                                                               | Scope    | Route effect                                                      |
| ------------------------------ | -------------- | ------------------------------------------------------------------- | -------- | ----------------------------------------------------------------- |
| Reporting officer at level _k_ | Supervision    | IAM `workforce.leave_reviewer.v1`, approved by an independent owner | Hospital | Reads and decides a routed request only once level _k_ has opened |
| Hospital leave desk            | Management     | Hospital `manager` access plus `workforce.leave.approve`            | Hospital | Unchanged; decides any pending request                            |
| HR configuring the policy      | Administration | Hospital `manager` access plus `workforce.leave.configure`          | Hospital | Edits the policy; gains no decision right                         |

The supervisor reference stays a structural fact and never grants authority: a
route step only narrows an IAM grant the reviewer already holds. Out-of-route
reads and decisions return 404, a missing permission 403, and an unavailable
scope check 503, as today.

## Definitions

- **Routing policy**: an append-only, versioned hospital record. Mode `hospital`
  keeps today's behaviour. Mode `reporting_line` also needs a customer-entered
  escalation interval in whole hours (1–720) and a source reference, such as the
  HR circular that set it. Helivanta seeds no interval. A hospital with no
  policy behaves as `hospital` at version 0.
- **Pinned route**: written once, in the transaction that files a request. The
  request records the policy version in force and whether it was routed. A
  routed request gets one step per eligible level. A routed request with no
  steps has the fallback `no_eligible_reporting_line`, and only the leave desk
  can decide it.
- **Eligible step**: an ancestor in the requesting staff member's supervision
  chain who, at filing, is active staff with an active, reviewed login link in
  the same hospital. Ineligible ancestors are skipped and the walk continues
  above them. At most eight steps are pinned and the walk is bounded.
- **Open step**: level _k_ opens at `filed_at + (k − 1) × interval` on the
  database clock. Opened steps stay open, so a reporting officer can still act
  after escalation.

## Required behaviour

1. HR reads and edits the policy with `expected_version`. A stale version
   returns 409, and an identical replay returns the current revision. A change
   applies only to requests filed afterwards.
2. A database trigger pins the route when the request is inserted, so the
   client never supplies route data and no code path can file around it. The
   request's `routing_version` and `routed` columns, and its steps, are
   immutable.
3. For a routed request, a scoped reviewer can list, read and decide only when
   their linked staff member holds an open step. A restrictive row-security
   policy on `workforce_leave_requests` enforces this. Every table readable
   through the scoped review functions inherits it. Unrouted requests,
   including every request filed before this change, keep today's behaviour.
4. A desk manager who also holds a scoped grant falls back to the desk path when
   they hold no open step, exactly as today when the scoped path is refused.
5. Self-decision, filer independence, balance, overlap, year closure, expected
   version, replay and audit rules are unchanged. A filer on the route still
   cannot decide their own filing, so that request waits for the next level or
   the desk. Decision evidence adds `route_level` (a level, or `desk`) and
   `routing_version` for routed requests.
6. HR's request detail shows the routing version, each step with its opening
   time and whether it is open, or the fallback. The reviewer queue gives each
   routed request the reviewer's own level, so the UI marks it direct (level 1)
   or escalated (level > 1).

## Data

- `workforce_leave_routing_revisions(tenant_id, hospital_id, version, mode,
escalation_hours, source_reference, reviewed_by_subject, created_at)`, PK
  `(tenant_id, hospital_id, version)`. A trigger allows only
  `version = max + 1`. Rows are immutable.
- `workforce_leave_requests` gains `routing_version integer` (FK to the
  revision) and `routed boolean NOT NULL DEFAULT false`, with
  `CHECK (NOT routed OR routing_version IS NOT NULL)`.
- `workforce_leave_route_steps(tenant_id, hospital_id, request_id, level,
staff_id, opens_at)`, PK `(tenant_id, hospital_id, request_id, level)`, unique
  per request and staff member. Steps can only be inserted by the pinning
  trigger in the request's own filing transaction, and they are immutable.
- RLS: each new table has one permissive policy. Managers read it, and writes
  need hospital `manager` access plus the transaction-local flag. Scoped
  reviewers read only their own steps.

## API

- `GET /hr/leave-routing` and `PUT /hr/leave-routing` (`workforce.leave.configure`).
- `GET /staff/:id/leave-requests/:request` adds `routing` with `version`,
  `routed`, `steps[]` (`level`, `staff_id`, `display_name`, `opens_at`, `open`)
  and `fallback`.
- `GET /leave-review-requests` items add `route_level` (null when unrouted).

## Not covered

Multi-level sanction thresholds, explicit delegation and acting approvers,
coverage staffing checks, department-scoped roles (#429), PharmaConnect
pharmacist-in-charge cover (#418), standalone pharmacy employers, notifications
(#416), and routing for staff self-service filing (#1132). #1132 must pin under
the same trigger.

## Verification

Real Postgres:

- cross-tenant, cross-hospital and out-of-route 404s;
- level 2 opens only after one interval, with time simulated by backdating
  `opens_at` through the admin pool;
- a chain or policy change after filing leaves the pinned route intact;
- the no-eligible-line fallback;
- unrouted and pre-existing requests unchanged;
- desk decisions on a routed request;
- direct writes to the route columns and steps are rejected.

Vitest covers the policy form, the route view and the queue badges. A local
two-person browser acceptance runs on the dev stack.

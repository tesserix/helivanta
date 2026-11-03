# Plan: reporting-line leave routing (#1536)

Spec: [reporting-line leave approval with escalation](../specs/2026-11-04-leave-reporting-line-routing.md).

1. **Red, policy.** `leave_routing_test.go`: GET returns `hospital` at version
   0; PUT with a stale version returns 409; replay is idempotent; validation
   covers the mode and interval pairing, the 1–720 bounds and a required source
   reference; a reader gets 403; another tenant gets 404.
2. **Migration `0078_workforce_leave_routing`** (`leave_routing.sql`): revisions
   table with a version-sequence trigger; request columns with an insert guard
   that pins `routing_version`/`routed` from the current revision and an update
   guard; steps table with an AFTER INSERT pinning trigger that walks the
   supervision chain with bounded `LATERAL` hops; forced RLS; a restrictive
   policy on `workforce_leave_requests` for scoped review sessions.
3. **Green, policy.** `leave_routing.go` handlers under `lockHospitalManager`
   with the `leave-ledger` lock, plus an audit row. Routes in `module.go`.
4. **Red, routing.** Three supervision levels, each a scoped reviewer. Level 1
   sees and decides; level 2 sees nothing until `opens_at` is backdated through
   the admin pool; an unrelated reviewer gets 404; the desk decides a routed
   request; a reviewer with no open step and desk rights falls back to desk; the
   no-eligible-line fallback; a chain edit after filing changes nothing;
   unrouted requests are unchanged; direct updates of the route columns and
   steps fail.
5. **Green, routing.** `prepareScopedLeaveReview` checks visibility after setting
   the flags and clears them on refusal; evidence adds `route_level` and
   `routing_version`; the queue adds `route_level`; HR detail adds `routing`.
6. **Contracts.** `contracts/workforce-leave.json` gets the policy, its input,
   the route and the queue field. Run `node scripts/generate-workforce-leave.mjs`
   and `go run ./cmd/contractdocs`.
7. **Frontend.** A routing policy panel beside leave settings, a route section in
   request review and a direct/escalated badge in the reviewer queue, each with
   Vitest.
8. **Docs.** `docs/api/workforce.md` leave routing section; the organization
   gaps list.
9. **Gates.** `make lint-go`, `go vet`, archtest, the leave suite with `-race`,
   contract checks, and
   `pnpm turbo lint type-check test build format:check`.
10. **Local acceptance.** On the dev stack, HR sets `reporting_line`; a clerk
    files for a nurse; the reporting officer (a second person) approves from
    the review queue; an unrelated reviewer sees nothing.

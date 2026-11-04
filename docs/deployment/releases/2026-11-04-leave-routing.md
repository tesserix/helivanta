# Reporting-line leave routing — 2026-11-04

PR [#1546](https://github.com/tesserix/helivanta/pull/1546) is merged and deployed
as `f4799d63acfd58f05dc8901346ffba636b751f4c`, closing
[#1536](https://github.com/tesserix/helivanta/issues/1536). HR can version a
reviewed routing policy (`hospital`, or `reporting_line` with an escalation
interval). The policy in force is pinned to each leave request at filing, and a
scoped reviewer decides a routed request only through an open step they hold;
the hospital leave desk keeps final authority. The release also carries the
documentation-only [#1549](https://github.com/tesserix/helivanta/pull/1549).

## Verified rollout

- All required exact-head checks passed on `295fffc7` before the normal admin
  merge; the merge tree is identical to that head. Post-merge
  [main CI](https://github.com/tesserix/helivanta/actions/runs/41745373823) passed.
- The [merged image workflow](https://github.com/tesserix/helivanta/actions/runs/41745373463)
  passed API/shell builds, scans, SBOMs, signing and deploy-branch advancement.
- Kargo Freight `df0c93cd9a7c4e238356b67b4f205f2fc1a767a1`; Promotion
  `prod.01m6j1pxzcrgtx292yncjqn86d.df0c93c`, created by the owner, Succeeded
  through the unchanged Stage template. No imperative Deployment image patch was
  used.
- The merged-image migration hook `helivanta-api-migrate-lzrzt` exited 0. The
  ledger grew 223→224, adding exactly `0078_workforce_leave_routing`.
- Read-only verification on the primary: both new tables have forced RLS; all
  five routing triggers are enabled; `leave_route_review_guard` is restrictive;
  both new leave-request constraints are validated; the reviewer index is valid
  and ready. The installed `workforce_pin_leave_route`,
  `workforce_leave_route_reviewer` and `workforce_leave_request_routing_pinned`
  bodies hash-match committed SQL.
- Production holds no routing revisions, route steps or leave requests, so no
  filed request changed; routing begins only once HR saves a policy.
- Seven application deployments are updated/ready/available at 2/2. All 14
  application containers match signed digests (API `sha256:a584f987…`, shell
  `sha256:35a6a098…`) with zero restarts. All 12 Helivanta Argo applications
  are Synced/Healthy.
- Public landing/login GETs return 200. Unauthenticated
  `GET /hr/leave-routing` returns 401 through Global API. This is an
  authentication-negative smoke check, not authenticated acceptance.

The verified boundary was account `unidevidp@gmail.com`, project
`tesseracthub-480811`, context
`gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`, application namespace
`helivanta`, Kargo stage `kargo-helivanta/prod`. The repository remained PUBLIC.
Replica counts and infrastructure services are unchanged.

## Scope and remaining acceptance

Two-person local browser acceptance of routing is recorded in the
[Staff tracker](../../qa/staff-management-progress.md). Production authenticated
acceptance still needs owner sign-in. Self-service leave filing
([#1132](https://github.com/tesserix/helivanta/issues/1132)) must pin under the
same trigger and is the next slice. Multi-level sanction thresholds, delegation,
coverage checks, escalation notifications and department-scoped roles remain
open. Staff Management remains **10 partial, 10 awaiting audit, 1 research, 0
fully accepted**.

See the [machine record](2026-11-04-leave-routing.json). Private captures are under
`~/.helivanta/staff-management-20261027/leave-routing-release-20261104/`.

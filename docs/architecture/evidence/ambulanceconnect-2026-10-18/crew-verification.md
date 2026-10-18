# Dispatch and crew delivery evidence, 2026-10-18

This evidence covers the production web application in `apps/shell`, built by
`Dockerfile.shell`, and the real emergency API. It does not certify the entire
EmergencyConnect programme or activate citizen booking or clinical transport.

## Production worker recovery

The worker repair is merged in infrastructure PR #1458. A later read-only check
confirmed API 2/2, document worker 2/2, supply-evidence worker 2/2 and ClamAV 1/1 in
namespace `helivanta`. Argo reported Synced/Healthy. CPU limits used 10.75 of the
12-core namespace quota. The running scanner reported ClamAV 1.5.4, definition
version 28157, published 2026-10-18T06:24:14Z.

The earlier live synthetic scanner probe accepted clean input and rejected the
standard EICAR sample. It created no patient or staff objects. Worker repair and
this unreleased dispatch branch are separate deliveries.

## Crew and authorization behavior

- Discovery returns only the verified subject's offered, active assignments,
  including assignments across bases in the active provider tenant.
- Protected incident reads require the selected assignment, use the existing
  audited decrypt path, and return `Cache-Control: no-store`.
- Unassigned users, other tenants and tenant administrators without assignment
  cannot read through the crew endpoints. Reconciled stand-down removes private
  access. Failed current reads hide previously displayed details.
- The driver and clinician acknowledge independently. Both are required before
  departure. An actor's acknowledgement cannot be repeated as the other actor.
- Cancellation and reconciliation require their own application permissions as
  well as the base grant. Crew permissions cannot substitute for dispatch
  permissions, or vice versa. Reconciliation requires supervisor authority.
- The worker rejects role memberships as well as INHERIT, superuser, BYPASSRLS,
  role creation and database creation. Permission-denied checks cover private
  payloads, credential references and SET ROLE into application/owner roles.
- Purpose flags cannot make an unassigned application identity eligible or grant
  it access to private case payloads.
- Duplicate Temporal starts verify workflow type, queue and original canonical
  tenant/base/dispatch input. Failed, cancelled or terminated executions cannot
  be accepted as completed receipts. A missing run receipt requires reconciliation.

## Connectivity and current-state behavior

The web does not queue clinical commands for reconnect. Offline attempts fail
immediately with a contact-dispatch message. An uncertain response retains the
same command ID, version and body for an explicit retry. A failed refresh hides
protected content without discarding that command. A newer polled version cannot
silently change it. After a successful receipt, another action waits until the
current read reaches that receipt's version.

Closing the assignment removes protected queries immediately. No contact or
incident payload is written to localStorage by this feature. The future native
queue remains separate work and must not be inferred from this web behavior.

## Browser journey

The Playwright suite uses a production Next build at a local origin, backed by
real disposable Postgres, OpenBao and NATS. All records are synthetic. It covers:

1. Staff intake and inaccessible case links.
2. Transport assessment and current fleet readiness.
3. Driver acknowledgement without premature departure.
4. A separate clinician session and acknowledgement.
5. Departure and arrival on scene by the assigned crew.
6. Explicit dispatcher cancellation and supervisor reconciliation.
7. Loss of private incident access after stand-down.
8. A 390px viewport with no horizontal document overflow.

The captured mobile screen is [crew-mobile.png](crew-mobile.png). Browser API
routing is confined to the loopback-only test harness; test authentication is not
part of the production application.

## Scale check

The `emergencyplans` test seeds 100,000 historical dispatch records in disposable
Postgres. Crew discovery used the existing active-dispatch index and took about
0.23 ms in that fixture. This is a query-plan check, not a production latency SLO.
No speculative index was added.

## Release boundary

Application PR #1246 and infrastructure PR #1460 are separate from the already-live
worker repair. Migrations 0004/0005, the new dedicated worker identity and the
emergency orchestration flag have not been activated by this verification.

Public provider discovery, citizen booking/status/cancellation, verified workforce
qualification linkage, receiving-facility acceptance/rejection/revocation/expiry,
transport/arrival/dual handover/clear, and native/device journeys remain programme
work. The YES Hospital clinical/dispatch owner and approved receiving and crew
qualification policies must be supplied before clinical/public activation.

## Executed validation

- Shell: 557 tests passed; the full suite was run with `--maxWorkers=1` after an
  unchanged payroll test timed out under competing test load. Its isolated rerun
  also passed. No timeout was increased and no shell tests were skipped.
- API package: 309 tests passed; shared UI: 94 tests passed.
- `pnpm --filter @helivanta/shell type-check` and `lint` passed.
- Production Next build passed using the CI synthetic public OIDC settings; all six
  browser journeys passed against that build and the real disposable API.
- Current emergency Go race suite passed; testinfra, config and authz race suites
  also passed during this delivery.
- `go -C backend build ./...`, `go -C backend vet ./...`, affected
  `golangci-lint run` and real worker identity/scope checks passed.
- Infrastructure PR #1460 current-head CI: 923 repository tests passed and
  53 subtests passed; four existing cases were deselected by the existing
  workflow quarantine. Helm template/lint and Argo validation passed.

The affected web tests include permission denial, stale/current-read guards,
retained uncertain commands after failed refresh, offline non-queueing and
protected content removal after revocation.

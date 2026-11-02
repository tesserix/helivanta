# Shared departments and Pharma foundation — local acceptance

Branch: `feat/shared-staff-foundation-20261102`, initially based on `40787523`
and rebased onto `d22bbde1` before the draft review. The upstream base also adds
its own `0015_billing` mode-policy migration; it is not part of this PR diff. The generated source inventory
was regenerated after the upstream billing route; metadata behavior is unchanged.
This extends the [initial reference protocol](2026-11-02-organization-reference-local.md)
and [Pharma shared Staff source](2026-11-02-pharma-shared-staff-local.md).
It does not complete Staff postings or product professional authorization.

## Normal owner browser journey

The existing signed-in synthetic owner used the registered local app origin
`http://helivanta.localhost:4399/settings/organization` through Global API 8199
and owner API 8399. All business mutations below used the normal UI:

1. Empty hospital registration and department commands showed required fields.
2. Registered the existing synthetic hospital reference. Real outbox/NATS/
   Workforce observation advanced it to verified. This records existence only.
3. Created department `SYN-PHARMA-20261102` and sub-unit
   `SYN-COUNTER-20261102` with that exact canonical parent.
4. Attempted parent retirement while its child was active. The owner refused it;
   both stayed active and no successful revision was appended for that refusal.
5. Renamed the department and saw the child's parent label update without copying
   or replacing its parent ID. History retained both original and updated snapshots.
6. After audit/integrity corrections, edited both records again through the UI.
   At that checkpoint the root was version 3 and child version 2.
7. Reloaded the built standalone production bundle and saved another attributed
   revision through it. Final root is version 4, child version 2: six snapshots.
8. A supplementary read-only tenant-scoped database check verified those heads,
   the parent link, correct current audit resource targets, and the installed
   deferred revision-integrity trigger. It did not create or modify business data.

History labels its instantaneous effective times explicitly in UTC. The UI
production bundle built successfully and is served locally on the same
port for final persistence checks. The browser initially hit a Chrome block after
an alternate `localhost` origin prompted re-authentication. Returning to the
registered app origin reused the valid owner session. No browser security control
was disabled and no sign-in handoff or credential change was required.

## Problems found and corrected

- RED create returned 404 before implementation; RED edit returned 409 before
  restructuring/history support.
- The real hospital list includes a retained head version. The first client used
  the request-receipt schema and rejected it. A failing SDK test captured the real
  shape; canonical `OrganizationHospitalHead` and strict client parsing now agree.
- Direct SQL could append a future snapshot without advancing its head. A real
  Postgres failing test demonstrated it. Forward migration 0003 now rejects that
  commit; immutable history and head transition must agree.
- Department audit rows initially reused the hospital-request resource label. A
  failing resource-target assertion caught it. Explicit resources now distinguish
  hospital requests/heads, department heads and retained revisions. Early synthetic
  QA rows keep their original labels as failure evidence; they were not rewritten.
  The final current heads have correct newly recorded audit targets.
- Required reason validation now uses a field-specific message. The UI tests prove
  that incomplete forms make no command request and failed unchanged retries keep
  the same command ID.

## OpenFGA and negative coverage

A cached real OpenFGA server, real Postgres and the shared server permission/
membership middleware prove manager allow, pharmacist deny, exact department-route
denial, same-identity manager-role revocation with independent read permission
retained, and 503 after an authorization outage even following a successful read.
Authentication in that focused test is the explicit harness verifier; it does not
replace IAM reconciliation tests. The actual local API uses IAM's
`NewCurrentAuthorization` and the persisted OpenFGA model.

Protocol tests cover exact/changed retry, old and contradictory observations,
explicit boolean presence/type, same-tenant isolation, stale edits, immutable code/
kind, cycles, eight-level depth, concurrent edits, terminal retirement, unavailable
parent, bounded history cursor, retained evidence, audit/outbox rollback and the
head/revision commit invariant. Invalid input never becomes a fabricated empty or
verified result. Organization metadata grants no Staff or outlet authority.

## Commands and evidence

With cached dependencies, host Go concurrency bounded to 2, `GOPROXY=off`, the
existing Docker test socket and Node 24.11.1:

```sh
# backend/
go test -race -count=1 -timeout=10m ./internal/archtest ./internal/bootstrap ./internal/modules/organization
go test -race -count=1 -timeout=5m ./internal/globalapi -run TestEveryRegisteredRouteReachesItsProductOwner
go build -o /tmp/helivanta-shared-organization-api ./cmd/api
go build -o /tmp/helivanta-shared-organization-global-api ./cmd/global-api
go vet ./internal/modules/organization/... ./internal/archtest/...
golangci-lint run ./internal/modules/organization/... ./internal/archtest/...
go run ./cmd/contractdocs
go run ./cmd/contractdocs -check

# repository root
pnpm --filter @helivanta/api type-check
pnpm --filter @helivanta/shell type-check
pnpm --filter @helivanta/api test
pnpm --filter @helivanta/ui test
pnpm --filter @helivanta/shell exec vitest run components/pharmacy components/organization 'app/(workspace)/workforce'
pnpm --filter @helivanta/shell exec vitest run components/organization/structure.test.tsx
pnpm --filter @helivanta/api exec eslint src/organization-directory.ts src/organization-directory.test.ts src/organization-queries.ts
pnpm --filter @helivanta/shell exec eslint components/organization 'app/(workspace)/settings/organization/page.tsx'
node scripts/staff-operational-access-inventory.mjs --check
node scripts/generate-organization-structure.mjs --check
git diff --check

# apps/shell/, with local fixture public config
pnpm exec next build --webpack
node .next/standalone/apps/shell/server.js # PORT=4399, HOSTNAME=127.0.0.1
```

Final affected suites: 577 API, 278 shared UI, 659 Pharmacy/Workforce/organization
shell tests. Focused UI and SDK reruns cover later contract/error refinements.
Final architecture/bootstrap/organization race suite, owner routing, typechecks,
vet, scoped lint (zero Go issues) and production web build pass. Pre-existing React
Hook Form server-export and middleware deprecation warnings remain. Dev style/CSP
reports were observed; the final local preview uses the built production bundle,
without weakening the policy. `next start` reported the standalone-output
warning; the accepted preview runs the generated standalone server with the
built static/public assets and loopback binding. Workforce's unchanged package passed its full race
suite in the preceding turn (576.560 seconds); it was not rerun after organization-
only changes. No new dependencies/images were downloaded.

Private mode-600 artifacts are under
`~/.helivanta/staff-management-20261027/shared-staff-foundation-20261102/`:
`organization-ui-persistence-proof.json`, API/UI runtime records, screenshots and
check/build logs. The existing secure QA JSON now references canonical unit IDs
and versions; all account credentials and unrelated entries were preserved.

Migrations 0001/0002/0003 organization and Workforce 0069 are persisted and applied
to local QA only. Original local binaries/config references were captured before
replacement. No production rollout, new production migration, operational grant,
issue closure or completion tag is claimed. The source ledger is 754 surfaces,
17 classified and 737 unclassified. Whole-Staff acceptance remains incomplete.

Next: explicit legacy HR vocabulary handoff, canonical Workforce postings,
professional/evidence review, outlet/PIC/assistant duties, scoped permission
projection and revocation, Staff audit workspace and full role-specific browser
journeys. See the [Pharma role/audit scope](../architecture/pharma-staff-specialization.md).

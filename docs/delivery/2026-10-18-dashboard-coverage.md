# Clinical dashboard coverage

Story [#1234](https://github.com/tesserix/helivanta/issues/1234), parent #573. Assigned to Sam123ben. This is the reporting-coverage slice; the full issue remains open.

The overview and patient activity view now expose emergency, maternity/births, admissions/discharges, beds/occupancy, procedures/theatre and deaths as six distinct reporting groups. They show an explicit unavailable state without fabricated totals, fake timestamps or unsupported workspace links. Existing patient/lab/pharmacy values remain sourced from their APIs; clinical text now explains the tenant-wide scope and independent staff hospital selection. The [specification](../superpowers/specs/2026-10-18-1234-dashboard-coverage.md) inventories the source, semantics, access and outstanding ownership for clinical, diagnostic, pharmacy, financial, staffing and facility statistics.

## Verification

- The new rendered-interface assertion failed because the clinical coverage region was absent; after implementation, the dashboard suite passed 10 tests. With extended view-navigation and permission assertions, the complete shell suite passed all 505 tests across 69 files.
- All 15 repository frontend lint/typecheck/format tasks passed. The Next.js 16 production build passed, including a second build with the real local issuer/client configuration for browser acceptance. A first default-fork targeted test command was terminated after stalling; the explicit thread-pool red/green run completed without changing assertions or test timeouts.
- Native Chrome opened a dedicated local tab at `http://helivanta1217.localhost:14401/dashboard` using the existing synthetic local session and real API. All six groups appeared without counts; selecting Patient activity retained them, and selecting Diagnostics & pharmacy removed them while retaining real service panels. The current local tenant's empty cohorts were visibly distinguished from unsupported reporting.
- A private read-only helper compared nine real authenticated API aggregates against SQL under the existing local application role: MediCore, lab and pharmacy in UTC, Asia/Kolkata and Asia/Karachi. All matched; anonymous clinical access returned 401. No clinical record or grant was created, and no unsupported event count was inferred.
- Fresh narrow-device manual acceptance remains pending. No production mutation or release has occurred.

## Commands

With Node 24.11.1:

```sh
pnpm install --frozen-lockfile
pnpm --filter @helivanta/shell exec vitest run components/dashboard/operational-dashboard.test.tsx --pool=threads --maxWorkers=1
pnpm --filter @helivanta/shell exec vitest run --pool=threads --maxWorkers=1
pnpm turbo type-check lint format:check
pnpm --filter @helivanta/shell build
git diff --check
```

Local launcher and API/SQL helpers read private configuration without printing credentials and remain outside Git. There are no backend changes, new queries in the application, dependencies, migrations, NATS events, Temporal workflows, Valkey keys or GCS objects in this slice. Existing backend race/coverage passed in the separately tested stock-export branch; no new backend claim is made here.

## Work still required

Main has no ED encounter reporting source or maternity/birth-event source. Draft #1233 handles separate emergency transport foundations and is not an ED attendance feed. #817/#818 retain maternity and statutory birth/death ownership. Verified event dates, twins/multiple births, stillbirths, corrections, hospital scope, per-module access, timezone boundaries and real database/API/UI acceptance must precede actual clinical counts. The existing tenant-wide visit source also needs explicit hospital ownership before hospital filtering can be offered. No patient date of birth, ambulance request, logistics delivery or visit registration is substituted for these events.

Keep #1234/#573 and the dependent clinical stories open. A reviewed, specifically approved application release and deployed verification are required before claiming this coverage slice is deployed; full clinical metric completion requires the missing sources as well.

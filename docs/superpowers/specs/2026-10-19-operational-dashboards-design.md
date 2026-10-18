# Operational dashboards

Issues: #573, #574, #576, #577 (delivers the source-backed reporting slice;
does not close the broader dashboard framework or role-dashboard issues).

## Scope and definitions

Replace the navigation-only landing surface with permission-gated, independent
clinical, pharmacy and laboratory analytics, plus hospital-local workforce
composition. Keep the existing department links. Date presets, custom dates,
daily/weekly/monthly/yearly buckets and clinical OPD/IPD filtering apply to
created-in-period cohorts. Weeks begin Monday. Dates are inclusive in the
selected IANA timezone; SQL uses a half-open timestamp range. Incomplete first
and last buckets are intentionally clipped to that range. Missing buckets are
zero-filled. Every panel states the query time and its metric definition.

IPD visit registrations are not unique patients, occupied beds, discharges or
length of stay. Pharmacy and laboratory statuses are the current status of
records created in the selected period, not completion events in that period.
Workforce is a current snapshot, not historical headcount or on-duty staffing.

The repository has no appointment, visit-doctor assignment, discharge or bed
inventory model. Appointment and doctor-workload metrics cannot be derived
correctly. Explain this on the clinical surface and link to existing workflows;
never display invented zeros or imply scheduling exists. This change does not
implement clinical scheduling, warehouse analytics, layout persistence, SSE or
cross-hospital clinical rollups. Those broader issues remain open.

## Design

Each owning module exposes its own aggregate endpoint, protected by its existing
read permission. No module queries another module's tables. A shared platform
helper implements only the three identical window/count contracts. PostgreSQL
groups rows before transmission; the browser never downloads patient lists for
analytics. No migrations or new dependencies are required.

Planning envelope (assumptions, not measured production demand): 100 dashboard
viewers refreshing once/minute = 5 aggregate requests/s, 10x peak = 50/s;
read-only requests, at most 366 buckets and five calendar years, roughly 30 KB
per response. At 2,000 registrations/day, a hospital has 730k rows at 12 months
and 2.2m at 36 months, before growth. Existing tenant/time indexes bound the
scan. Target p99 under two seconds per panel, with a two-second SQL timeout;
no separate availability SLO is introduced. Measure query plans on synthetic
100k+ rows. Do not claim these targets as production benchmark results.

Queries use WithTenant plus explicit current-tenant predicates. Clinical scope
is the current tenant; workforce also enforces existing hospital access/RLS,
including revocation and the absence of inherited administrator access. Unknown,
duplicate and malformed query parameters fail closed. Dates, zones, intervals
and bucket count are bounded. Responses contain counts only and use no-store.
Queries use the existing authenticated request logging; no patient identifiers
are returned. Permission failure prevents resolution; no unavailable module is
rendered as zero. Each panel owns loading/error/retry/empty/freshness states.

The browser uses useApiQuery/useApiPagedQuery. Cache keys include all filters
and hospital scope; the existing tenant switch clears the cache. Hospital
selection applies only to workforce because visits do not have hospital IDs.
Use semantic-token SVG charts with accessible data tables, responsive layout,
and the existing shared controls. No new chart package or remote assets.

## Legacy dashboard review and adoption

The user requested a browser review of `http://59.94.33.26/mednet/` for design
inspiration. The authenticated Analytics area was inspected through its visible
navigation. No legacy records, names, financial figures or exports are copied
into the repository or fixtures.

Observed patterns: Operations → Admission uses a KPI band followed by monthly
occupancy, length-of-stay, admission-purpose and ward breakdowns. Its displayed
period is anchored to a financial year. Doctor Performance uses primary-doctor
search, monthly OP/IP patient and billing columns, a new-patient option and
spreadsheet/PDF reports. This is a patient/billing performance report, not an
appointment schedule. Finance separates collection, payables and payment-mode
reports; HR is its own area.

Adopt focused Overview, Patient activity, Diagnostics & pharmacy and Care teams
views, prominent metric cards, and a distribution beside each activity trend.
Clinical visit mix means shares of the currently displayed OPD/IPD registrations,
not conversion between those workflows. Laboratory/pharmacy distributions show
current statuses within the created-in-period cohort. An inclusive-calendar-day
registration average includes empty days and uses dates rather than elapsed
hours, so DST does not change its denominator. Each view retains the existing
permissions, dates and independent query/error boundaries. A view whose required
permission disappears falls back to Overview. Care teams is a current snapshot,
so historical filter controls are hidden on that focused view.

Do not copy legacy zero tiles for unavailable metrics. Ward occupancy needs bed
inventory and time-bounded bed allocations; LOS needs admission/discharge
timestamps; conversion needs linked patient journeys and a defined window;
unique/new patients need a stable identity/cohort definition; doctor billing
needs doctor assignments and billing/credit ledgers. Those source workflows
remain outside this reporting slice. Financial-year defaults must come from a
tenant accounting policy rather than assuming April for every country. No new
API, data store, dependency, permission or infrastructure is needed for the
adopted presentation changes; all derived values use the existing aggregates.

## Alternatives, delivery and recovery

Client aggregation of collection pages was rejected: incomplete totals, PHI
overfetch and unbounded downloads. A cross-module dashboard database query was
rejected: it bypasses ownership and local authorization. Event projections and
warehouse pipelines are appropriate for the full issues but introduce data
stores and replay/correctness requirements not needed for these bounded source
queries. Infrastructure cost is existing API/PostgreSQL compute only; there is
no new service or secret. Database outages yield explicit panel errors; healthy
panels and navigation remain usable. Shipping is an additive API/UI release;
reverting the code restores the old page without database changes. No live
deployment is part of this branch-development request.

### Calendar boundary correction

Regression coverage exposed a midnight-DST error in local-midnight iteration.
Calendar labels and bucket iteration now use civil UTC dates; SQL bounds choose
the earliest valid local midnight from the adjacent timezone offsets. A skipped
midnight starts at the first valid instant; a wholly skipped date has a zero-length
SQL window and remains a zero-filled calendar bucket. Requested response dates
are preserved. Tests cover Havana's spring/fall boundaries and Apia's skipped day.

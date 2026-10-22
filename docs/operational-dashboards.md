# Operational dashboards

The `/dashboard` landing page adds source-backed reporting above the existing
department shortcuts. Related issues: #573, #574, #576 and #577. This implements
bounded operational reports, not the full configurable dashboard framework in
those issues.

## Using the dashboard

1. Sign in and select your tenant using the existing tenant switcher. Clinical,
   pharmacy and laboratory panels appear only when you hold their read
   permission. Server checks enforce those permissions independently of the UI.
2. Choose **Overview**, **Patient activity**, **Diagnostics & pharmacy** or
   **Care teams** to focus on a service. Views respect your permissions; period
   selections survive view changes. Care teams shows a current snapshot, so
   historical date controls are hidden in that view.
3. Choose **Today**, **This week**, **This month**, **This year** or **Last 5
   years**. Presets apply immediately. The current year defaults to monthly
   buckets; the five-year preset uses yearly buckets.
4. For a custom report, enter **From**, **To**, **Group by** and **Timezone**,
   then choose **Apply filters**. Dates are inclusive. Use an IANA timezone such
   as `Asia/Kolkata`, `Australia/Melbourne` or `UTC`. The initial zone is explicitly
   UTC; it does not infer a hospital zone from the viewer's device.
5. Switch between daily, weekly, monthly and yearly grouping. Weeks start on
   Monday. The first and last buckets may contain only part of a week, month or
   year. Reports cover at most five calendar years and 366 buckets; daily
   grouping therefore accepts at most 366 days.
6. Use **Visit type** to compare OPD and IPD together or inspect either series.
   This changes the displayed clinical totals, daily average, chart and visit
   mix. **Registrations / day** divides the selected registrations by inclusive
   calendar days, including empty days, regardless of chart grouping or daylight
   saving. Mix percentages use only displayed visit types; IPD-only is 100% and
   is not an OP-to-IP conversion rate. It does not filter lab
   or pharmacy records, which do not carry that dimension.
7. Choose **Lines** or **Bars** for all time-series charts. Open **View data
   table** for exact, keyboard-accessible values. Empty periods remain visible
   as zero buckets. Laboratory and pharmacy status distributions show the share
   of records created in the period in each current status. Zero results and
   failed queries have different states; empty distributions have no percentages.
8. In **Doctors & care teams**, select a hospital to which you have explicit
   staff access. Use **Load more** to discover additional hospital choices.
   The staff snapshot is independent of the historical date filters. Tenant
   administrators still need an explicit hospital grant.
9. Each panel shows when it was queried. **Refresh** updates that panel only;
   **Try again** retries a failed panel without hiding healthy panels.

## What the numbers mean

| Panel                | Definition                                                                                 | Do not interpret as                                                      |
| -------------------- | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| Patient activity     | OPD/IPD visit rows created in the inclusive reporting period                               | Unique patients, bed occupancy or discharges                             |
| IPD registrations    | IPD visit registrations in that period                                                     | Current inpatient census, length of stay or bed utilisation              |
| Laboratory           | Current pending/completed status of orders created in the period                           | Results completed during the period or all current outstanding work      |
| Pharmacy             | Current pending/dispensed status of tasks created in the period                            | Dispensing events during the period, revenue or stock availability       |
| Doctors & care teams | Current staff by profession and active/inactive employment status in the selected hospital | Historical headcount, on-duty coverage, availability or appointment load |

No patient names, patient IDs, staff names, diagnoses or laboratory result
values are returned by these aggregate endpoints. The clinical panels are
scoped to the current tenant. Changing **Staff hospital** affects workforce
only: the clinical visit model has no hospital/branch foreign key, so this page
does not offer a misleading cross-hospital clinical filter.

## API and authorization

| Endpoint (under `/api/v1`)                    | Enforcement                                                                                                                      |
| --------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| `GET /medicore/insights`                      | Membership + `medicore.visit.read` + tenant transaction/RLS + explicit tenant equality                                           |
| `GET /lab/insights`                           | Membership + `lab.order.read` + tenant transaction/RLS + explicit tenant equality                                                |
| `GET /pharmacy/insights`                      | Membership + `pharmacy.dispense.read` + tenant transaction/RLS + explicit tenant equality                                        |
| `GET /workforce/hospitals/:hospital/insights` | Authentication/membership + explicit hospital staff grant inside the hospital transaction/RLS; inaccessible hospital returns 404 |

The first three accept exactly `from`, `to`, `interval` and `timezone`.
Unknown or repeated parameters are rejected with 400. Tenant IDs cannot be
supplied to change scope. Workforce accepts no query parameters and returns a
current snapshot. All responses use `Cache-Control: no-store`. Queries have a
two-second SQL statement timeout. The existing API error envelope is preserved;
database failures do not become empty successful datasets.

Example request (using the existing authenticated same-origin session):

```http
GET /api/v1/medicore/insights?from=2026-10-01&to=2026-10-02&interval=day&timezone=Asia%2FKolkata
```

A successful response is the aggregate object below, not a paginated list. An
invalid or repeated filter returns 400; missing authentication returns 401;
missing owner-module read permission returns 403. Existing authorization
infrastructure failures fail closed, and database failures return the existing
safe error envelope. Consumers must show an error/retry state, never substitute
zero counts for a failed request.

Example clinical response:

```json
{
  "from": "2026-10-01",
  "to": "2026-10-02",
  "interval": "day",
  "timezone": "Asia/Kolkata",
  "as_at": "2026-10-02T12:00:00Z",
  "totals": { "OPD": 12, "IPD": 3 },
  "series": [
    { "date": "2026-10-01", "counts": { "OPD": 12, "IPD": 3 } },
    { "date": "2026-10-02", "counts": { "OPD": 0, "IPD": 0 } }
  ]
}
```

Workforce response example (no historical filters):

```json
{
  "hospital_id": "11111111-1111-4111-8111-111111111111",
  "as_at": "2026-10-02T12:00:00Z",
  "counts": [
    { "category": "doctor", "status": "active", "count": 3 },
    { "category": "nurse", "status": "inactive", "count": 1 }
  ]
}
```

The workforce categories are doctor, nurse, pharmacist, lab technician,
receptionist, administration and other; employment status is active or inactive.
Absent combinations mean zero, and a hospital without staff returns an empty
`counts` array. An inaccessible or revoked hospital returns 404, including for
tenant administrators. The frontend validates the returned hospital ID against
the selection before rendering a snapshot.

`as_at` is the query-start time, not a projection watermark or a guarantee that
the source records will not change after the response. Counts reflect one
PostgreSQL statement snapshot. These endpoints read only their owning module's
tables, with no new cache, service, secret or schema migration.

The security boundary protects hospital operational counts from unauthenticated
visitors, other tenants and hospital staff without a grant. Route permissions
are checked before aggregation. Explicit tenant predicates prevent a future
group-read RLS policy from silently broadening clinical reports. Existing
authenticated request logging supplies request/tenant/actor correlation; this
feature does not claim certification or replace clinical access auditing.

## Legacy dashboard mapping and required workflows

The Mednet review informed focused views, KPI cards, time trends and category
breakdowns. No legacy records or figures were copied. These are the reporting
boundaries, including what must exist before additional APIs can be accurate:

| Legacy measure                               | Helivanta reporting and source requirements                                                                                                                                          |
| -------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Admission trend                              | IPD registration trends are available by day/week/month/year; these count visit rows.                                                                                                |
| Occupancy / ward occupancy / ARPOB           | Requires hospital/ward/bed inventory, available bed-day history, time-bounded bed allocations and attributable recognised revenue. No substitute zero is displayed.                  |
| Discharges / average length of stay          | Requires audited admission, discharge and transfer timestamps with encounter identity, hospital scope and correction rules.                                                          |
| Unique patients / new OP or IP patients      | Requires stable patient-to-encounter identity and an agreed first-visit/lookback definition; registration counts cannot establish uniqueness.                                        |
| OP-to-IP conversion                          | Requires explicit linkage between outpatient and inpatient encounters and an agreed conversion window. The visit mix is not conversion.                                              |
| Admission purpose                            | Requires a validated admission-purpose dimension; currently no such breakdown is inferred.                                                                                           |
| Doctor performance / OP and IP bills         | Requires effective doctor-to-encounter assignment, invoice/refund records, currency and attribution rules. Legacy doctor performance was patient/billing activity, not appointments. |
| Appointments / doctor utilisation / no-shows | Requires scheduled start/end, timezone, doctor reference, hospital scope and audited appointment outcomes. Staff headcount is not availability.                                      |

The relevant clinical/billing owners must define and authorize those source
workflows before extending their aggregate APIs. The current clinical visit
schema has no hospital/branch foreign key, so the workforce hospital selector
cannot safely filter clinical charts. Financial-year comparisons also require
an explicit fiscal calendar; April is not assumed for every country. These
workflows remain outside this registration/status reporting slice.

## Validation and release

The implementation includes real-PostgreSQL checks for tenant isolation,
permission denial, whole-cohort totals beyond list-page sizes, empty periods,
inclusive boundaries, 23/25-hour DST days, midnight transitions (Havana),
whole skipped dates (Apia), calendar grouping and immediate
hospital-access revocation. Frontend tests cover query filters, panel failure,
permission hiding, hospital pagination/switching, malformed snapshot scope,
chart modes, inline validation and mobile chart sizing. A local browser preview
uses synthetic data only; it is not evidence of a production deployment.

The 100,000-visit synthetic query-plan check uses the existing
`medicore_visits_tenant_id_created_at_idx`. A one-day cohort scanned 288 rows,
returned OPD 216/IPD 72, and the recorded PostgreSQL execution time was 0.822 ms
on the local test machine. This is a query-plan observation, not an API p99 or
production capacity claim.

Reproduce the affected checks from the repository root:

```sh
pnpm --filter @helivanta/api test --maxWorkers=2
pnpm --filter @helivanta/shell test --maxWorkers=1 --testTimeout=20000
pnpm --filter @helivanta/ui test --maxWorkers=2
pnpm turbo lint type-check build format:check
make lint-go
make lint-shell
cd backend
./scripts/coverage-gate.sh
```

Use Node 24 and Go 1.27.3, an accessible Docker daemon for integration tests,
and the existing documented development Zitadel build variables. The longer
frontend test timeout accommodates the full suite on a shared development
machine; assertions are unchanged. No production credentials or environment
files belong in this branch.

Release through the existing application pipeline after review, deploying the
additive backend endpoints before the frontend. For a rollback, restore the
frontend before removing the endpoints. No migration, data backfill, credential
rotation or production configuration change is needed.

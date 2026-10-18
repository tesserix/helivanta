# Hospital dashboard coverage

Issue: [#1234](https://github.com/tesserix/helivanta/issues/1234), parent #573. This slice makes missing clinical reporting visible and documents each source contract. It does not complete #1234: emergency, maternity and birth-record sources still need implementation and acceptance before numbers can be reported.

## Scope and semantics

The released dashboard reports tenant-level OPD/IPD registrations, laboratory-order and dispensing-task cohorts, plus an independently selected hospital's current staff snapshot. These are different scopes and time bases. The staff hospital selection must not imply that it filters clinical visits. Existing clinical totals cannot truthfully become bed occupancy, ED attendance, maternity deliveries or births.

Add a compact, responsive clinical reporting coverage panel to the overview and clinical view for users with clinical read permission. Include emergency attendance, maternity and births, admissions and discharges, beds and occupancy, procedures and theatre, and deaths. Each group names its intended measurements and explicitly says that reporting is not available. Do not render zero, trend, percentage, fake last-updated time or a link to an unimplemented workspace. This is static capability information, never a substitute for fetching clinical records. Diagnostics, pharmacy and team views retain their existing focus.

The UI uses the existing permission hook; it makes no new network requests, expands no authorization, returns no patient data and stores nothing. Unavailable groups are omitted for users without clinical read permission. Existing API enforcement remains authoritative. No subscription or hospital grant is inferred from another module; future feeds must validate their own entitlement, permission, tenant and hospital grants before resolving data.

## Metric/source contract

| Measurement | Current source and scope | Permission / missing dependency |
| --- | --- | --- |
| OPD/IPD registrations | `GET /medicore/insights`, current tenant; visits created within inclusive dates in the selected IANA timezone | `medicore.visit.read`; no hospital selector on this source |
| ED attendance, triage, waiting, disposition | No ED encounter registry on main | Requires explicit hospital clinical grant and clinical event source; #1017/#1234 |
| Emergency transport cases, dispatch, readiness | Separate EmergencyConnect ownership; draft #1233 under #1220 | Must use EmergencyConnect's own access; transport intake is not ED attendance |
| Delivery episodes | No maternity event source | #817; actual delivery timestamp and hospital; multiple babies can belong to one delivery |
| Babies born, live births, stillbirths | No birth-event source; demographics are excluded | #818/#1059; baby's outcome and event timestamp, including twins and corrections |
| Admissions, discharges, inpatient census | Existing IPD registration cohort is not an admission/discharge ledger | #1017/#1234; admissions/discharges need separate events and current census a snapshot |
| Occupied/staffed beds, occupancy | No authoritative bed denominator or occupancy source | #106/#386; do not divide registrations by beds or substitute licensed capacity |
| Procedures, OT, cancellations | No completed procedure/event reporting feed | Clinical analytics #1017; planned and completed procedures are separate |
| Deaths | No death-event reporting source | #818/#1059; date of death/event and confirmed scope, not patient demographics |
| Laboratory task cohort | `GET /lab/insights`, tenant-level current status of orders created in the period | `lab.order.read`; critical results and turnaround need additional result/event sources |
| Pharmacy dispense-task cohort | `GET /pharmacy/insights`, tenant-level current status of tasks created in the period | `pharmacy.dispense.read`; not sales, inventory or dispense-event throughput |
| Pharmacy stock and expiry | Outlet-scoped register in `/pharmacy/stock`, explicit cutoff, units, saved outlet timezone | `pharmacy.stock.read` plus live outlet grant; no hospital roll-up feed; #1060/#162 |
| Finance, collections, outstanding | Outlet-scoped pharmacy payables workspace, not hospital revenue | `pharmacy.accounts.read` plus live outlet grant; #580 owns hospital finance; never add currencies |
| Staff by profession/status | `/workforce/hospitals/{id}/insights`, current snapshot | Explicit workforce hospital access; independent of historical clinical filters; not attendance or roster coverage |
| Facilities, safety and compliance | No hospital-wide dashboard aggregate | #294/#573; require module-owned scopes, status and event/snapshot definitions |

## Acceptance and failure handling

Test that the new clinical groups exist for clinical readers, show no fabricated counts, make no unsupported endpoint requests and remain hidden for pharmacy-only users. Verify overview/clinical/service/team navigation retains its behavior. Existing failed-source/retry tests and period semantics remain intact. Perform a native local browser walkthrough against the real API; no legacy patient names, rows or exports are copied.

This adds no endpoint, query, write, migration, queue, workflow, cache or infrastructure cost. Existing platform workload/latency/SLO assumptions are unchanged. The panel works when a clinical insight source fails because it describes coverage rather than claiming successful measurement. Rollback is the normal application image change. Source implementation is a separate reviewable vertical slice with schema, RLS, audit, timezone/boundary tests and real API/UI acceptance; it cannot be replaced by a guessed count here.

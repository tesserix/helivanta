# Epic 979 delivery ledger

Source: [Exploration epic](https://github.com/tesserix/helivanta/issues/979).
Reviewed 2026-10-14. All 103 linked stories and the epic are assigned to Samyak Rout (`sam123ben`).

An assignment or legacy screen inventory is not implementation completion. No story is closed by this ledger.
Each story needs its own legacy delta, failing acceptance tests, local verification, security review and release evidence.
PR #1095 delivered hospital-scoped registration foundations; the parent identity and reception stories remain open.

## Working sequence

1. Patient registration completeness, identity and configured sections: #980, #1058, #1055, #1050.
2. Reception, encounters and appointments: #981, #991, #984, #983.
3. Product-specific lab/pharmacy handoffs and worklists, then billing/inventory dependencies.
4. Remaining clinical, workforce, financial, reporting and configuration stories in dependency order.
5. Navigation/accessibility (#989, #990) apply to every delivery; do not defer them until the end.

## Story acceptance inventory

The acceptance criteria below are copied from the owning issues for review. Status remains unverified until evidence is recorded.

### [980](https://github.com/tesserix/helivanta/issues/980) — [Platform][Exploration] Unify patient search and identity across clinical, lab and pharmacy workflows

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Given synthetic existing and similarly named patients, when staff search by supported identifier, then results show sufficient permitted identity context and the selected patientmaster ID is preserved across product handoffs.
- [ ] Given a shared household phone or an approximate name match, when staff search/register, then no automatic identity merge occurs and the existing duplicate-match/override workflow is used.
- [ ] Given a pharmacy-only or lab-only entitlement, when identity search is used, then it does not require a MediCore subscription; unauthorized tenant records never appear.
- [ ] Loading, empty, failure and permission-denied states are explicit; patient and encounter context cannot silently drift.

### [981](https://github.com/tesserix/helivanta/issues/981) — [MediCore][Exploration] Preserve outpatient reception queues, filters and encounter context

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Given synthetic outpatient visits, when filters are combined, then results and totals consistently reflect date, department, referrer, panel and authorized counter scope.
- [ ] Given multiple encounters for one patient, when a row is opened, then patient identity, encounter identifier, clinician and token remain unambiguous.
- [ ] Given empty results, failed loads or later pages, when filters change, then explicit empty/error/loading states appear and pagination resets without stale records.
- [ ] Loading, empty, failure and permission-denied states are explicit; patient and encounter context cannot silently drift.

### [982](https://github.com/tesserix/helivanta/issues/982) — [MediCore][Exploration] Model hospital bill states, balances and cashier reporting explicitly

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Given synthetic bills in each supported state, when a cashier views the worklist, then textual state, patient liability and payer liability are clearly distinguished.
- [ ] Given a permitted bill transition, when it is confirmed in a test environment, then permissions, reason, audit trail and balance effects are verified; locked/sealed behavior follows the approved business rules.
- [ ] Given partial payment, cancellation, refund or repeated submission, when applied to synthetic bills, then reconciliation remains correct and duplicate financial effects are prevented.
- [ ] Loading, empty, failure and permission-denied states are explicit; patient and encounter context cannot silently drift.

### [983](https://github.com/tesserix/helivanta/issues/983) — [MediCore][Exploration] Preserve inpatient admission, ward and discharge worklist distinctions

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Given synthetic pre-registered, admitted, discharge-ready and discharged encounters, when filtering, then each state and its applicable dates are unambiguous.
- [ ] Given a ward/bed transfer, when tested through the owning clinical workflow, then current location and occupancy update once and historical location remains traceable.
- [ ] Given incomplete required discharge steps, when discharge is attempted in non-production, then the approved clinical and financial gates explain the blocker without discarding progress.
- [ ] Loading, empty, failure and permission-denied states are explicit; patient and encounter context cannot silently drift.

### [984](https://github.com/tesserix/helivanta/issues/984) — [MediCore][Exploration] Implement appointment worklist, calendar views and explicit status transitions

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Given synthetic appointments across clinicians and dates, when changing calendar view or search filters, then the same appointment identities and status counts are retained.
- [ ] Given a booking or rescheduling conflict, when submitted in non-production, then the conflict is explicit and duplicate or overlapping capacity allocation is prevented.
- [ ] Given a supported status change, when an authorized user confirms it, then allowed transitions, actor/time history and downstream queue effects follow documented rules.
- [ ] Loading, empty, failure and permission-denied states are explicit; patient and encounter context cannot silently drift.

### [985](https://github.com/tesserix/helivanta/issues/985) — [LabConnect][Exploration] Implement the laboratory order-to-dispatch worklist with specimen traceability

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Given synthetic OPD/IPD/walk-in orders, when navigating stage views, then order, investigation, specimen, patient and encounter identities remain linked.
- [ ] Given partial completion of a multi-investigation order, when one result progresses, then remaining investigations retain their correct stage and aggregate status is accurate.
- [ ] Given duplicate requests or an unavailable downstream consumer, when retried in non-production, then no duplicate order/specimen/report is created and pending handoffs remain visible.
- [ ] Loading, empty, failure and permission-denied states are explicit; patient and encounter context cannot silently drift.

### [986](https://github.com/tesserix/helivanta/issues/986) — [LabConnect][Exploration] Make critical, delayed and outsourced laboratory work actionable

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Given synthetic critical/delayed investigations, when thresholds are crossed, then visible text, responsible role, timestamps and follow-up status distinguish the exceptions.
- [ ] Given an outsourced investigation, when its sample/result handoff is recorded in non-production, then destination, dispatch, receipt and reconciliation status are traceable without duplicating the order.
- [ ] Given a rejected or corrected report, when rework is authorized, then previous versions, reason and approval history remain available and the patient sees only the appropriate released version.
- [ ] Loading, empty, failure and permission-denied states are explicit; patient and encounter context cannot silently drift.

### [987](https://github.com/tesserix/helivanta/issues/987) — [PharmaConnect][Exploration] Implement pharmacy sales worklists with encounter linkage and liability breakdown

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Given synthetic counter, OPD, IPD and casualty pharmacy sales, when filtering by store and linkage, then only matching authorized records appear with clear patient and encounter context.
- [ ] Given a sale with patient and payer liability, when viewing its summary, then monetary totals reconcile with its lines, discounts, payments and outstanding balance.
- [ ] Given a retry or partial failure during a synthetic sale, when resumed, then one sale and one inventory effect occur; hospital billing consumes the published pharmacy contract without reading pharmacy tables.
- [ ] Loading, empty, failure and permission-denied states are explicit; patient and encounter context cannot silently drift.

### [988](https://github.com/tesserix/helivanta/issues/988) — [PharmaConnect][Exploration] Provide pharmacy-owned category, discount, template and indent configuration

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Given authorized pharmacy configuration, when a category, discount rule, template or indent grouping is viewed, then its scope and active version are explicit.
- [ ] Given overlapping or invalid pricing rules, when configuration is validated in non-production, then conflicts are rejected with actionable errors and existing issued sales remain historically stable.
- [ ] Given a restricted role, when configuration changes are attempted, then they are denied; approved changes retain actor, reason and effective-date history.
- [ ] Loading, empty, failure and permission-denied states are explicit; patient and encounter context cannot silently drift.

### [989](https://github.com/tesserix/helivanta/issues/989) — [Platform][Exploration] Keep module labels and navigation consistent across multiple tabs

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Given Pharmacy open in one tab, when EMR home is opened in a second authenticated tab, then the second tab identifies itself as EMR/MediCore with matching navigation.
- [ ] Given two product tabs, when switching modules in either tab, then the other tab retains a correct independent page label and context.
- [ ] Given a stale or unsupported product route, when it is loaded directly, then an explicit consistent fallback is shown instead of another product label.
- [ ] Loading, empty, failure and permission-denied states are explicit; patient and encounter context cannot silently drift.

### [990](https://github.com/tesserix/helivanta/issues/990) — [Platform][Exploration] Replace ambiguous icon controls with accessible, explicit actions

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Given worklists and module navigation, when using keyboard alone, then every action has visible focus, logical order and an accessible name/role.
- [ ] Given state or priority information, when color/icons are unavailable, then text still communicates the same meaning.
- [ ] Given a destructive or clinical/financial state-changing action, when opened with synthetic data, then its specific effect, target identity and confirmation are explicit and distinct from read-only navigation.
- [ ] Loading, empty, failure and permission-denied states are explicit; patient and encounter context cannot silently drift.

### [991](https://github.com/tesserix/helivanta/issues/991) — [MediCore][Exploration] Provide clinician OPD/IPD worklists with visit timing and ward context

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Given synthetic outpatient cases, when displaying the queue, then registration/appointment timestamps, visit type and TAT have documented definitions and a consistent facility timezone.
- [ ] Given synthetic admissions, when combining ward, floor, department, purpose and discharge-ready filters, then patient identity and encounter/location context remain accurate.
- [ ] Given clinical and reception users, when viewing the same encounter, then each sees authorized role-specific work while the shared encounter identity stays consistent.
- [ ] Loading, empty, failure and permission-denied states are explicit and keyboard-accessible.

### [992](https://github.com/tesserix/helivanta/issues/992) — [MediCore][Exploration] Support tumor board scheduling and multidisciplinary review tracking

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Given synthetic cases in different specialty groups, when filtering date/open status, then scheduled cases, presenter and attendees are represented consistently.
- [ ] Given a board decision recorded in non-production, when signed off by the authorized clinician, then attributable decision history and follow-up ownership link to the clinical encounter.
- [ ] Given a rescheduled or cancelled case, when changed in non-production, then history remains traceable and no duplicate active board review is created.
- [ ] Loading, empty, failure and permission-denied states are explicit and keyboard-accessible.

### [993](https://github.com/tesserix/helivanta/issues/993) — [MediCore][Exploration] Manage chemotherapy scheduling and review states with pharmacy handoffs

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Given synthetic chemotherapy schedules, when filtering status/ward/date, then requested, pending, continuing and re-evaluation work is clearly distinguishable.
- [ ] Given treatment awaiting an approved regimen or required review, when progressed in non-production, then clinician-defined safety prerequisites block unauthorized advancement; no autonomous dose or regimen decision is introduced.
- [ ] Given an approved medication request, when handed to PharmaConnect, then patient/encounter/regimen version and fulfilment status are traceable through published contracts, with one medication/inventory effect per action.
- [ ] Loading, empty, failure and permission-denied states are explicit and keyboard-accessible.

### [994](https://github.com/tesserix/helivanta/issues/994) — [MediCore][Exploration] Track radiation counselling, planning and execution as linked clinical stages

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Given a synthetic radiation episode, when moving between counselling, planning and execution views, then patient, episode and RT identifiers remain linked without duplicating records.
- [ ] Given an approved plan and selected equipment, when scheduling a synthetic session, then resource conflicts and required clinical approvals are validated before execution can be recorded.
- [ ] Given a reschedule, missed session or plan revision, when recorded in non-production, then scheduled versus actual dates and attributable revision history remain distinguishable; clinical staff define safety rules.
- [ ] Loading, empty, failure and permission-denied states are explicit and keyboard-accessible.

### [995](https://github.com/tesserix/helivanta/issues/995) — [MediCore][Exploration] Preserve diagnostic report dispatch, scheduling and radiology review workflow

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Given synthetic imaging requests, when filtering the diagnostic queue, then request identity, patient/encounter and report state remain linked.
- [ ] Given amended or cancelled reports, when authorized in non-production, then versions and reasons remain traceable and dispatch uses the correct released version.
- [ ] Given a scan or external-message integration failure, when staff inspect the worklist, then the failure is visible and reconciliation does not create duplicate studies or reports.
- [ ] Loading, empty, failure and permission-denied states are explicit and keyboard-accessible.

### [996](https://github.com/tesserix/helivanta/issues/996) — [MediCore][Exploration] Schedule theatre rooms and resources with conflict-aware capacity

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Calendar and resource views use one case identity and show room/clinician availability and status.
- [ ] Concurrent bookings and changes cannot allocate the same exclusive resource twice; cancellations release capacity with history.
- [ ] Room/resource changes are permissioned and versioned; historical cases retain the configuration used when booked.
- [ ] Given empty results, a failed load, an invalid filter or restricted access, the screen shows a clear accessible state and never stale results from another patient, tenant or scope.
- [ ] Given the observed fields and filters above, a synthetic workflow walkthrough demonstrates each retained capability; any intentional departure is documented with clinical/operational rationale.

### [997](https://github.com/tesserix/helivanta/issues/997) — [MediCore][Exploration] Connect pre-operative and operative theatre worklists to the surgical case

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Pre-op and operative lists retain the same patient/encounter/case identity, planned surgeon and room.
- [ ] Case readiness and allowed stage changes follow clinician-approved rules; incomplete prerequisites produce explicit blockers.
- [ ] Stale and repeated stage changes do not create duplicate cases or lose attributable state history.
- [ ] Given empty results, a failed load, an invalid filter or restricted access, the screen shows a clear accessible state and never stale results from another patient, tenant or scope.
- [ ] Given the observed fields and filters above, a synthetic workflow walkthrough demonstrates each retained capability; any intentional departure is documented with clinical/operational rationale.

### [998](https://github.com/tesserix/helivanta/issues/998) — [MediCore][Exploration] Version surgical consent templates and peri-operative checklists

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Authorized users manage versioned consent/checklist/anatomy reference data with validation and audit.
- [ ] A synthetic case records the exact template version, required checklist completions and responsible clinician; missing required consent blocks the appropriate stage.
- [ ] Updating or retiring a template preserves historic evidence and does not silently rewrite completed cases.
- [ ] Given empty results, a failed load, an invalid filter or restricted access, the screen shows a clear accessible state and never stale results from another patient, tenant or scope.
- [ ] Given the observed fields and filters above, a synthetic workflow walkthrough demonstrates each retained capability; any intentional departure is documented with clinical/operational rationale.

### [999](https://github.com/tesserix/helivanta/issues/999) — [MediCore][Exploration] Map surgery catalogue and theatre team services into auditable billing

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] A surgical case produces itemized charges from the appropriate effective surgery/service mappings, retaining the source case and team roles.
- [ ] Invalid or duplicate coding/service mappings are rejected and historical billed cases retain their original values.
- [ ] Correction/cancellation/retry paths preserve audit and financial reconciliation without duplicate charge effects.
- [ ] Given empty results, a failed load, an invalid filter or restricted access, the screen shows a clear accessible state and never stale results from another patient, tenant or scope.
- [ ] Given the observed fields and filters above, a synthetic workflow walkthrough demonstrates each retained capability; any intentional departure is documented with clinical/operational rationale.

### [1000](https://github.com/tesserix/helivanta/issues/1000) — [SupplyConnect][Exploration] Compare supplier quotations and manage effective rate contracts

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Compare equivalent item/unit/quantity/tax/discount terms from supplier quotes and identify the selected source and reason.
- [ ] Contract validity and price effective dates are validated against the intended purchase date; expired terms cannot silently price a new order.
- [ ] Retain quote revisions, approvals and selection history with role restrictions and links to requisitions/orders.
- [ ] Given empty results, a failed load, an invalid filter or restricted access, the screen shows a clear accessible state and never stale results from another patient, tenant or scope.
- [ ] Given the observed fields and filters above, a synthetic workflow walkthrough demonstrates each retained capability; any intentional departure is documented with clinical/operational rationale.

### [1001](https://github.com/tesserix/helivanta/issues/1001) — [SupplyConnect][Exploration] Track purchase requisitions through sanctioned purchase orders

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Preserve item, quantity, unit, requesting department and source requisition across order creation and partial realization.
- [ ] Enforce approval roles and thresholds; expose ordered/received/outstanding quantities and sanctioned status explicitly.
- [ ] Prevent duplicate POs on retry and retain changes/cancellation reasons without altering already received quantities.
- [ ] Given empty results, a failed load, an invalid filter or restricted access, the screen shows a clear accessible state and never stale results from another patient, tenant or scope.
- [ ] Given the observed fields and filters above, a synthetic workflow walkthrough demonstrates each retained capability; any intentional departure is documented with clinical/operational rationale.

### [1002](https://github.com/tesserix/helivanta/issues/1002) — [SupplyConnect][Exploration] Reconcile challans, goods receipts and supplier invoices

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Match synthetic delivery/challan, PO, receipt and invoice lines with partial delivery, unit conversion and discrepancy handling.
- [ ] Record accepted, rejected and pending quantities with batch/expiry where applicable; post each accepted receipt exactly once.
- [ ] Expose unmatched invoices, duplicate external references and failed B2B handoffs in an actionable reconciliation queue.
- [ ] Given empty results, a failed load, an invalid filter or restricted access, the screen shows a clear accessible state and never stale results from another patient, tenant or scope.
- [ ] Given the observed fields and filters above, a synthetic workflow walkthrough demonstrates each retained capability; any intentional departure is documented with clinical/operational rationale.

### [1003](https://github.com/tesserix/helivanta/issues/1003) — [SupplyConnect][Exploration] Process traceable supplier returns and related stock/credit reconciliation

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] A synthetic return identifies original receipt, item/batch, quantity, reason and disposition.
- [ ] Reject returns above eligible stock/received quantity and prevent expired/quarantined stock from re-entering usable inventory through a return correction.
- [ ] Reconcile return, physical stock effect and supplier credit once, with approval and immutable history.
- [ ] Given empty results, a failed load, an invalid filter or restricted access, the screen shows a clear accessible state and never stale results from another patient, tenant or scope.
- [ ] Given the observed fields and filters above, a synthetic workflow walkthrough demonstrates each retained capability; any intentional departure is documented with clinical/operational rationale.

### [1004](https://github.com/tesserix/helivanta/issues/1004) — [SupplyConnect][Exploration] Manage store inventory and demand-based replenishment thresholds

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Show stock by store/item/batch with consistent available, committed, quarantine and expiry quantities.
- [ ] Apply scoped reorder thresholds with clear units and preferred-vendor constraints; handle zero stock and missing supplier terms explicitly.
- [ ] Create a replenishment proposal in synthetic testing without duplicating requests or automatically purchasing; changes retain actor and effective dates.
- [ ] Given empty results, a failed load, an invalid filter or restricted access, the screen shows a clear accessible state and never stale results from another patient, tenant or scope.
- [ ] Given the observed fields and filters above, a synthetic workflow walkthrough demonstrates each retained capability; any intentional departure is documented with clinical/operational rationale.

### [1005](https://github.com/tesserix/helivanta/issues/1005) — [SupplyConnect][Exploration] Maintain item, supplier and retailer masters with validated business identities

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Validate scoped unique item codes, units/categories, tax/licence formats and vendor/retailer identity; support inactive records without deleting history.
- [ ] Restrict and audit financial/contact field access and changes; mask sensitive values in unnecessary views and exclude them from logs.
- [ ] Validate synthetic imports with row-level errors and duplicate handling before commit; retain source/version and keep pharmacy dispensing ownership separate.
- [ ] Given empty results, a failed load, an invalid filter or restricted access, the screen shows a clear accessible state and never stale results from another patient, tenant or scope.
- [ ] Given the observed fields and filters above, a synthetic workflow walkthrough demonstrates each retained capability; any intentional departure is documented with clinical/operational rationale.

### [1006](https://github.com/tesserix/helivanta/issues/1006) — [SupplyConnect][Exploration] Configure procurement departments, price factors and document layouts

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Scope settings to the intended facility/department/category and show effective version and precedence.
- [ ] Validate price factor bounds and rounding; preserve the calculation used for historical purchase/sales documents.
- [ ] Preview documents using synthetic fixtures; settings changes require appropriate permission and audit and cannot silently affect another department.
- [ ] Given empty results, a failed load, an invalid filter or restricted access, the screen shows a clear accessible state and never stale results from another patient, tenant or scope.
- [ ] Given the observed fields and filters above, a synthetic workflow walkthrough demonstrates each retained capability; any intentional departure is documented with clinical/operational rationale.

### [1007](https://github.com/tesserix/helivanta/issues/1007) — [SupplyConnect][Exploration] Fulfil store indents with attributable goods and patient issues

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Link requested/sanctioned quantities to partial issues, receiving location and responsible staff; patient issues retain encounter and patientmaster identity.
- [ ] Enforce available stock, unit conversion and batch/expiry eligibility and show unfulfilled quantities.
- [ ] Retries cannot double-issue stock; authorized reversals retain original issue and reason, with published billing/pharmacy handoffs where applicable.
- [ ] Given empty results, a failed load, an invalid filter or restricted access, the screen shows a clear accessible state and never stale results from another patient, tenant or scope.
- [ ] Given the observed fields and filters above, a synthetic workflow walkthrough demonstrates each retained capability; any intentional departure is documented with clinical/operational rationale.

### [1008](https://github.com/tesserix/helivanta/issues/1008) — [SupplyConnect][Exploration] Track store transfers and returns as balanced stock movements

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Model source/destination, batch, quantity and custody through requested, dispatched and received states with documented allowed transitions.
- [ ] Reconcile stock at both ends atomically or through the established idempotent event contract; detect incomplete handoffs.
- [ ] Restrict allowed routes and returns and preserve movement history instead of overwriting original records.
- [ ] Inter-unit routes require explicit company/destination scope, permitted transfer modes and active status; cross-company authorization must not be inferred from a selected dropdown.
- [ ] Given empty results, a failed load, an invalid filter or restricted access, the screen shows a clear accessible state and never stale results from another patient, tenant or scope.
- [ ] Given the observed fields and filters above, a synthetic workflow walkthrough demonstrates each retained capability; any intentional departure is documented with clinical/operational rationale.

### [1009](https://github.com/tesserix/helivanta/issues/1009) — [SupplyConnect][Exploration] Reconcile batch ledgers, consumption and stock-posting exceptions

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Opening balance plus receipts/transfers/adjustments minus issues/returns reconciles to closing stock for each store/item/batch.
- [ ] Expired, unmanaged and usable quantities remain distinguishable; ledger filters cannot mix incompatible units silently.
- [ ] Posting failures show source transaction, reason, owner and safe retry/reconciliation status; repeated recovery cannot post twice.
- [ ] Given empty results, a failed load, an invalid filter or restricted access, the screen shows a clear accessible state and never stale results from another patient, tenant or scope.
- [ ] Given the observed fields and filters above, a synthetic workflow walkthrough demonstrates each retained capability; any intentional departure is documented with clinical/operational rationale.

### [1010](https://github.com/tesserix/helivanta/issues/1010) — [SupplyConnect][Exploration] Control stock imports, recounts and reasoned inventory adjustments

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Validate synthetic import rows before commit with duplicate detection, unit/batch/expiry checks and a reconciled summary.
- [ ] Stocktakes retain counted versus book quantities at a defined point in time and account for movements during the count.
- [ ] Adjustments require reason and permission/approval appropriate to their impact, write immutable history and cannot drive prohibited negative stock or duplicate valuation effects.
- [ ] Given empty results, a failed load, an invalid filter or restricted access, the screen shows a clear accessible state and never stale results from another patient, tenant or scope.
- [ ] Given the observed fields and filters above, a synthetic workflow walkthrough demonstrates each retained capability; any intentional departure is documented with clinical/operational rationale.

### [1011](https://github.com/tesserix/helivanta/issues/1011) — [SupplyConnect][Exploration] Configure store roles, locations and rack/shelf assignments

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Represent facility/department/store/location hierarchy and supported store roles without creating duplicate product patient/inventory masters.
- [ ] Validate unique location/rack identifiers and unit mappings; prevent inactive or unauthorized destinations on new movements.
- [ ] Configuration/import changes are permissioned and auditable; pharmacy fulfilment remains PharmaConnect-owned and existing movement history remains valid.
- [ ] Given empty results, a failed load, an invalid filter or restricted access, the screen shows a clear accessible state and never stale results from another patient, tenant or scope.
- [ ] Given the observed fields and filters above, a synthetic workflow walkthrough demonstrates each retained capability; any intentional departure is documented with clinical/operational rationale.

### [1012](https://github.com/tesserix/helivanta/issues/1012) — [FacilityConnect][Exploration] Track fixed-asset allocation and time-bound gate-pass custody

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Track each asset's allocation, location and custodian with a consistent asset identifier and lifecycle history.
- [ ] Gate passes specify return expectations and show overdue versus returned assets with attributable checkout/return history.
- [ ] Prevent duplicate active custody and unauthorized movement; reconcile returns without deleting the original pass.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1013](https://github.com/tesserix/helivanta/issues/1013) — [MediCore][Exploration] Resolve dietary workflow coverage unavailable to the current legacy session

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Obtain a read-only authorized role walkthrough or synthetic demonstration covering diet orders, restrictions/allergens, kitchen production, delivery and cancellation without creating source production records.
- [ ] Map each demonstrated capability to #822 with field/state evidence, role ownership and acceptance tests; explicitly mark unsupported capabilities.
- [ ] Do not equate an empty menu with absence of a feature or ship inferred clinical/dietary rules without domain confirmation.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1014](https://github.com/tesserix/helivanta/issues/1014) — [MediCore][Exploration] Reconcile collections, payables and payment-mode analytics with source transactions

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Define revenue, collection, payable, discount and payment-mode measures with accounting date basis, exclusions and currency/rounding rules.
- [ ] Reconcile totals and authorized drill-downs to synthetic transactions including partial payments, refunds/cancellations and period boundaries.
- [ ] Period changes, empty periods and failed loads are clear; report/export access is role-scoped and personal data is minimized.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1015](https://github.com/tesserix/helivanta/issues/1015) — [MediCore][Exploration] Provide admission, geographic and clinician operational analytics

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Document numerator/denominator, time zone, encounter inclusion and admission/discharge boundary rules for each clinical operations metric.
- [ ] Reconcile synthetic ward/doctor/geographic cohorts and drill-downs to source encounters without exposing unauthorized records.
- [ ] Handle zero denominators and incomplete source data distinctly from genuine zero activity; preserve filter context across drill-downs.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1016](https://github.com/tesserix/helivanta/issues/1016) — [SupplyConnect][Exploration] Deliver procurement and inventory classification reports with explainable calculations

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Define classification measures, thresholds, cost/profit basis and fast/slow/non-moving/dead-stock windows with versioned configuration.
- [ ] A synthetic dataset with known movements/value/criticality yields expected classifications and reconciles to the stock ledger.
- [ ] Show explanatory drill-downs and distinct no-data/error states; scope reports to permitted departments/stores and prevent classifications from automatically placing orders.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1017](https://github.com/tesserix/helivanta/issues/1017) — [MediCore][Exploration] Preserve clinical and oncology analytics with defined cohorts and drill-downs

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Catalogue each report's measures and source-domain ownership; resolve SDPI/GCC meanings with the hospital before implementation.
- [ ] Synthetic OPD/IPD/oncology cohorts reconcile across diagnosis, investigation and medicine measures with documented inclusion/version rules.
- [ ] Drill-down/export access follows patient-data authorization and minimized identifiers; incomplete datasets are distinguished from zero activity.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1018](https://github.com/tesserix/helivanta/issues/1018) — [MediCore][Exploration] Scope workforce analytics independently from clinical and financial access

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Define employee-report measures and purpose with HR owners and reconcile synthetic effective-dated employee records.
- [ ] Restrict detailed workforce records to authorized HR roles even when a user can access general analytics.
- [ ] Support clear period/empty/error states and audit permitted sensitive exports without logging personnel contents.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1019](https://github.com/tesserix/helivanta/issues/1019) — [MediCore][Exploration] Manage accounting transaction worklists and traceable voucher lifecycles

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Preserve distinct journal/payment/receipt/contra/purchase/debit/credit/invoice types and link each entry to its source and accounting center.
- [ ] Validate balanced entries, permissions, effective accounting date and supported draft/approved/posted/reversed transitions; retain original entries on correction.
- [ ] Repeated submission and source-event delivery cannot duplicate accounting effects; list totals and transaction details reconcile using synthetic fixtures.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1020](https://github.com/tesserix/helivanta/issues/1020) — [MediCore][Exploration] Reconcile bank and forex transactions with explicit matching and exceptions

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Match synthetic statement and internal entries by documented references/amount/date/currency rules with manual exception handling and attributable decisions.
- [ ] Handle partial, duplicate, fee, refund and forex-difference cases without changing original transaction amounts silently.
- [ ] Reconciled/unreconciled totals agree across individual, summarized and report views; unmatching is controlled and audited.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1021](https://github.com/tesserix/helivanta/issues/1021) — [MediCore][Exploration] Control ERP and ledger posting with reviewed mappings and recoverable exceptions

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Use reviewed effective mappings from source transaction types to ledger/ERP destinations, with balanced postings and source traceability.
- [ ] Expose invalid mappings, closed periods and failed external handoffs as actionable exceptions rather than silently dropping transactions.
- [ ] Idempotent recovery posts once and preserves batch/result history; consumers use domain contracts/outbox, not direct pharmacy/lab/clinical table reads.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1022](https://github.com/tesserix/helivanta/issues/1022) — [MediCore][Exploration] Review insurance adjustments with attributable financial approval

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Present the payer/empanelment, source bill/claim and proposed adjustment with before/after financial impact.
- [ ] Enforce authorized approval and segregation rules, reason/history and closed-period constraints.
- [ ] Duplicate or stale approvals cannot apply an adjustment twice; approved changes reconcile to payer/patient liability and ledger.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1023](https://github.com/tesserix/helivanta/issues/1023) — [MediCore][Exploration] Produce reconciled books, ledger statements and financial statements

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Define period, posting status, opening/closing balance and accounting-center semantics; reconcile synthetic books, trial balance, P&L and balance sheet.
- [ ] Drill down to attributable source entries while preserving filters and authorization; zero activity, missing mapping and failed loads are distinct.
- [ ] Confirm the required current statutory presentation with finance owners; no blind duplication of obsolete formats or unauthorized exports.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1024](https://github.com/tesserix/helivanta/issues/1024) — [MediCore][Exploration] Reconcile tax registers and GST report outputs under approved current rules

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Finance owners approve supported current return schemas, supply classifications, tax components and rounding; legacy menu names are not legal specifications.
- [ ] Synthetic invoices, returns, credit/debit notes and inter/intra-state supplies reconcile between itemized registers and return totals.
- [ ] Show incomplete/mismatched tax data before export; generated outputs are validated and no government filing occurs without a separate authorized workflow.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1025](https://github.com/tesserix/helivanta/issues/1025) — [MediCore][Exploration] Analyse revenue, profitability and receivables across hospital dimensions

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Define each measure, cost/margin basis, aging bucket and as-of boundary, including cancellations, refunds and partial payments.
- [ ] Reconcile synthetic cohort totals to hospital, PharmaConnect and SupplyConnect source contracts without duplicating transaction ownership.
- [ ] Preserve dimensions during drill-down and apply role/data-minimization rules; resolve SAR/PAR definitions with finance owners before implementing ambiguous measures.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1026](https://github.com/tesserix/helivanta/issues/1026) — [MediCore][Exploration] Version chart-of-accounts, tax, currency and accounting-center configuration

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Validate scoped unique codes, mappings, rates, currencies and effective dates with controlled activation and history.
- [ ] Opening balances reconcile; closed-period rules block inappropriate backdated changes; historical documents retain their original configuration.
- [ ] Sensitive setup and cheque-template changes are permissioned/audited; preview with synthetic data and never print or initiate payment automatically.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1027](https://github.com/tesserix/helivanta/issues/1027) — [MediCore][Exploration] Reconcile cashier handovers, counter verification and deposit mismatches

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Keep counter sessions, handovers, deposits and verification separately attributable with explicit permitted transitions.
- [ ] Reconcile cash and non-cash amounts/counts; show mismatch reasons and prohibit silent balancing or self-verification when separation of duties applies.
- [ ] Preserve locked entries and corrections as auditable records; synthetic tests cover duplicate submissions and competing verification.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1028](https://github.com/tesserix/helivanta/issues/1028) — [MediCore][Exploration] Preserve refund processing and receipt-cancellation audit trails

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Track refund request, decision and settlement separately; validate available refundable balance and duplicate requests.
- [ ] Restrict and mask bank details; audit permitted changes, cancellation reasons and original receipt linkage.
- [ ] Support date semantics and OPD/IPD/walk-in/advance/sales/journal receipt filters without conflating cancellation with deletion.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1029](https://github.com/tesserix/helivanta/issues/1029) — [PharmaConnect][Exploration] Track pharmacy refund requests through hospital accounting settlement

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] PharmaConnect owns pharmacy refund eligibility and sale/return references; coordinate accounting settlement through published module contracts.
- [ ] Separate stock-return acceptance from money-refund settlement; prohibit duplicate settlement on retry.
- [ ] Preserve company scope, state history, role restrictions and reconciliation against original sale.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1030](https://github.com/tesserix/helivanta/issues/1030) — [MediCore][Exploration] Preserve discharge, discount, balance and service-group accounting reports

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Keep financial reporting date and status definitions explicit and reconcile totals to source documents.
- [ ] Preserve read/unread review independently of discount approval and capture review actor/time without rewriting approval history.
- [ ] Provide permitted filters and paging; define claim-detail export and protect it by role and audit.
- [ ] Synthetic reconciliation covers patient/insurer splits, discounts, partially paid bills and discharge balances.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1031](https://github.com/tesserix/helivanta/issues/1031) — [MediCore][Exploration] Track payment-provider requests and reconciliation states

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Retain provider-neutral payment request and reconciliation identifiers with explicit state mapping.
- [ ] Ensure idempotent callback/retry handling and independently reconcile payment success to receipt posting.
- [ ] Restrict provider setup and sensitive reference display; demonstrate synthetic pending, success, cancellation and late-callback scenarios.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1032](https://github.com/tesserix/helivanta/issues/1032) — [MediCore][Exploration] Resolve Grants module navigation failure and complete grants discovery

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Reproduce in an authorized synthetic environment and establish whether route, method, permissions or deployment causes the failure.
- [ ] Authorized grants navigation opens the intended surface; denied access shows an accessible permission message rather than a server exception.
- [ ] Inventory grants pages, fields, states, roles and accounting handoffs after access is restored and link resulting acceptance stories.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1033](https://github.com/tesserix/helivanta/issues/1033) — [MediCore][Exploration] Manage insurer claim batches, cover letters and bill-level sanctions

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Preserve batch membership, attachments and versioned cover-letter templates with explicit draft/submission states and audit history.
- [ ] Prevent duplicate claim inclusion and define permitted lock/seal transitions; preserve original bill/service references.
- [ ] Apply validated sanctions at approved granularity, reconcile line and claim totals and retain reasons and decision attribution.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1034](https://github.com/tesserix/helivanta/issues/1034) — [MediCore][Exploration] Reconcile insurance receipts, deductions, unmapped money and ageing

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Distinguish received funds, allocation to claims and unallocated balances; retries cannot double-allocate.
- [ ] Define configurable ageing/as-of semantics and approved meanings for deductions; reconcile opening plus activity to closing balance.
- [ ] Synthetic cases cover partial payment, disallowance, withholding, overpayment, unmapped receipt and reversed allocation with auditable history.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1035](https://github.com/tesserix/helivanta/issues/1035) — [MediCore][Exploration] Maintain employee and consultant registries with controlled HR master data

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Reuse workforce identity references and keep employee/consultant lifecycle and confidential HR attributes permissioned.
- [ ] Validate imports using preview, duplicate detection and row-level errors; no partial silent overwrite.
- [ ] Version master data, preserve historical references and prevent disclosure through search/export.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1036](https://github.com/tesserix/helivanta/issues/1036) — [MediCore][Exploration] Plan company-scoped shifts, rosters and locum coverage

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Support all observed planning modes with overlap, overnight-shift and leave conflict checks.
- [ ] Version draft/frozen rosters and audit authorised corrections; prevent conflicting concurrent allocations.
- [ ] Define roster areas, shift templates/groups and grace/break/OT/locum rules with owner-reviewed effective dates.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1037](https://github.com/tesserix/helivanta/issues/1037) — [MediCore][Exploration] Manage leave balances, allocation structures and approval requests

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Preserve allocation, accrual, consumption and correction ledgers with deterministic balances.
- [ ] Validate overlapping requests, effective employment dates, leave type and approval permissions; retain request/decision/cancellation histories.
- [ ] Reconcile calendar/financial-year, proration, carry/expiry, holidays and short/half-day rules with HR; synthetic retries cannot double-credit.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1038](https://github.com/tesserix/helivanta/issues/1038) — [MediCore][Exploration] Audit duty exchanges, overtime and missing-punch corrections

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Keep source punches immutable and link approved corrections with before/after values, reasons and actors.
- [ ] Validate exchange consent/approval, roster conflict and overtime limits under documented HR policy.
- [ ] Separate requested from approved hours and ensure rejected/cancelled changes never feed payable hours.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1039](https://github.com/tesserix/helivanta/issues/1039) — [MediCore][Exploration] Reconcile attendance, exceptions and timesheet finalisation

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Separate raw observations, derived attendance, corrections and finalised timesheets.
- [ ] Handle overnight shifts, missing/duplicate punches and timezone boundaries; explain each exception and payable-hours derivation.
- [ ] Version recalculations and lock finalised periods; reconcile aggregate reports to individual synthetic records.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1040](https://github.com/tesserix/helivanta/issues/1040) — [MediCore][Exploration] Preserve payroll preparation, locking, processing and correction stages

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Require resolved or explicitly accepted exceptions before locking/preprocessing; separate authorisation for finalisation.
- [ ] Compute payroll reproducibly from versioned attendance, salary and tax inputs; prevent duplicate period processing.
- [ ] Audit corrections, reversals and arrears without rewriting finalised history; reconcile earnings/deductions/net and payroll reports.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1041](https://github.com/tesserix/helivanta/issues/1041) — [MediCore][Exploration] Version salary, tax, payslip and HR policy configuration

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Inventory and obtain HR/legal approval for all calculation and exemption rules before implementing; do not copy obsolete tax defaults.
- [ ] Version effective dates and dependency-safe formula/rounding validation; historical payroll retains original policy.
- [ ] Restrict sensitive payslip fields and export permissions; synthetic previews verify templates and totals.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1042](https://github.com/tesserix/helivanta/issues/1042) — [MediCore][Exploration] Manage performance objectives and appraisal review lifecycle

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Define objectives, measurement periods, reviewer access and state transitions with HR owners.
- [ ] Restrict confidential evaluations and separate appraisal approval from payroll changes.
- [ ] Audit revisions and normalization rules; use synthetic examples to validate without making employment decisions.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1043](https://github.com/tesserix/helivanta/issues/1043) — [MediCore][Exploration] Configure ward hierarchy, occupancy and departmental discharge clearances

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Model ward/room/bed and occupancy inclusion rules with historical admission references and gender/ICU constraints.
- [ ] Define and reconcile occupancy denominators, dates and accreditation categories; avoid treating legacy labels as current regulatory certification.
- [ ] Version departmental discharge gates and timestamps; each product owns its clearance and exchanges status through published contracts.
- [ ] Counter shifts handle overnight/day boundaries and reconcile with cashier sessions.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1044](https://github.com/tesserix/helivanta/issues/1044) — [MediCore][Exploration] Govern shared financial masters and staged period closure across products

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Define the source of truth and per-product consumers of financial masters; synchronisation is versioned, authorised and retry-safe through published contracts.
- [ ] Keep visibility restriction separate from transactional closure and financial-year finalisation; special historical access does not implicitly authorise editing.
- [ ] Validate closure prerequisites across billing, pharmacy and other affected products; record preview, authorised execution, effective boundaries and audit history.
- [ ] Preserve period configuration and exchange rates on historical documents; synthetic tests cover partial failure and late/backdated transactions.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1045](https://github.com/tesserix/helivanta/issues/1045) — [MediCore][Exploration] Version tariffs, payer categories and service pricing precedence

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Specify deterministic precedence across base tariff, category, ward, payer and effective time; reject inheritance cycles and overlaps.
- [ ] Preserve historical prices and payer agreements; explain applied rule on synthetic billing examples.
- [ ] Preview imports with validation and audit; pricing changes cannot silently reprice issued bills.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1046](https://github.com/tesserix/helivanta/issues/1046) — [MediCore][Exploration] Govern service catalogues, groups, taxes and accounting mappings

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Maintain stable codes and product-owned clinical catalogue references rather than duplicate lab/pharmacy catalogues.
- [ ] Validate service-group, ledger, tax and copay mappings and effective dates.
- [ ] Cover applicable service types and invalid/inactive references in synthetic charge generation.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1047](https://github.com/tesserix/helivanta/issues/1047) — [MediCore][Exploration] Configure consultation follow-up entitlements and room charges

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Calculate follow-up eligibility from approved visit and duration rules with clear reasons.
- [ ] Version practitioner/service/insurer coding and charges; test boundary dates and exhausted visit allowances.
- [ ] Reconcile bed moves and room-charge periods to admission history under documented proration rules.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1048](https://github.com/tesserix/helivanta/issues/1048) — [MediCore][Exploration] Model billing bundles, health packages, classifications and multi-visit entitlements

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Version package composition, inclusions/exclusions, validity, discount approval and accounting allocation.
- [ ] Track per-patient entitlements and consumption without double charging or exceeding visits; support explicit expiry/cancellation/refund policy.
- [ ] Synthetic coverage includes partially consumed, expired, overlapping and amended packages; preserve prior bills.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1049](https://github.com/tesserix/helivanta/issues/1049) — [MediCore][Exploration] Configure doctor groups and effective theatre charging templates

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Keep care-team identity distinct from billing group allocation and permissioned display.
- [ ] Define template precedence, effective windows and multi-practitioner charge allocation with theatre and finance owners.
- [ ] Audit changes and verify synthetic theatre bills against approved templates without duplicate charges.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1050](https://github.com/tesserix/helivanta/issues/1050) — [MediCore][Exploration] Preserve patient-organisation reference history

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Use stable scoped organisation identifiers linked to shared patientmaster references.
- [ ] Deactivation preserves historical encounters and billing relationships; audit all permitted changes.
- [ ] Search and access honor tenant/product scope; do not treat organisation membership as automatic payer eligibility.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1051](https://github.com/tesserix/helivanta/issues/1051) — [PharmaConnect][Exploration] Map pharmacy sales into hospital bills without duplicate revenue

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] PharmaConnect owns sale/return events and MediCore owns hospital bill projection through published contracts.
- [ ] Version default/category/subcategory/OT mapping precedence and validate missing or inactive ledgers.
- [ ] Retry-safe posting reconciles sale, return, hospital bill and accounting totals; prevent duplicate revenue.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1052](https://github.com/tesserix/helivanta/issues/1052) — [LabConnect][Exploration] Govern outsourced diagnostic partners and billing references

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Maintain lab partner identity, permitted departments and effective accreditation evidence; a legacy checkbox is not proof of accreditation.
- [ ] Coordinate partner service and billing references through module contracts with restricted contact access.
- [ ] Synthetic outsourced-order reconciliation covers inactive partners and missing mappings.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1053](https://github.com/tesserix/helivanta/issues/1053) — [AdminConnect][Exploration] Reconcile workforce onboarding and product access administration

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Separate shared identity, HR employment data, practitioner credentials and product access; coordinate via existing module contracts.
- [ ] Validate company scope, duplicate identity, contact verification and effective credential dates; protect uploads and confidential identifiers.
- [ ] Complete roles/mobile permission inventory using synthetic users; cover least privilege, revocation and audited role changes with no production test accounts.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1054](https://github.com/tesserix/helivanta/issues/1054) — [Platform][Exploration] Reconcile legacy profile recovery and integration credential workflows with platform identity

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Map profile and recovery to the established platform identity provider; do not replicate security-question recovery by default.
- [ ] Inventory integration-key scopes, ownership, expiry, revocation and audit in a synthetic environment; never expose stored secret values.
- [ ] Document verified migration requirements and explicitly retire unsupported legacy credential fields through an approved plan.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1055](https://github.com/tesserix/helivanta/issues/1055) — [AdminConnect][Exploration] Govern identifier prefixes and scoped demographic reference masters

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Confirm whether prefixes are display titles or identifier sequences before defining rules; inventory fields using synthetic configuration.
- [ ] Keep geographic and optional demographic reference values scoped/versioned with historical references.
- [ ] Minimise collection of sensitive demographic data and restrict access; configuration does not make optional attributes mandatory.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1056](https://github.com/tesserix/helivanta/issues/1056) — [MediCore][Exploration] Reconcile terminal payment-request status with receipts

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Map terminal/provider references to the existing payment reconciliation contract and maintain explicit status history.
- [ ] Test duplicate and late callbacks, failed payments and receipt-posting mismatch using synthetic provider responses.
- [ ] Restrict payment identifiers and setup; no live payment action is needed for acceptance.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1057](https://github.com/tesserix/helivanta/issues/1057) — [Platform][Exploration] Reconcile multi-channel event notifications and protected attachments

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Inventory each observed event with owning product, recipient resolution, consent/mandatory-notice classification and channel-specific template.
- [ ] Use published product events and established outbox/delivery contracts; retries are idempotent and delivery failure does not roll back completed clinical care.
- [ ] Protect clinical/financial/HR attachments with recipient authorisation and expiring access; audit dispatch without logging sensitive payloads.
- [ ] Separate security OTP/recovery traffic from optional messaging; synthetic tests cover unsubscribe, stale contact, duplicate events and failed provider delivery.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1058](https://github.com/tesserix/helivanta/issues/1058) — [Platform][Exploration] Configure shared patient registration sections and classification rules

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Extend shared patientmaster configuration rather than create product-specific patient identity stores.
- [ ] Define jurisdiction/tenant-specific optional sections with minimum necessary collection, field validation and restricted access; sensitive community/income/government identifiers are not required by default.
- [ ] Version classification and category restrictions without rewriting historical registrations; separate clinical doctor requirement from financial category assignment.
- [ ] Synthetic registration covers hidden/required fields, guardian/kin relationships, optional demographics and ABHA linkage.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1059](https://github.com/tesserix/helivanta/issues/1059) — [MediCore][Exploration] Medical-record reporting for coding gaps, deaths and medico-legal cases

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Provide role-scoped coding-completeness reports with explicit inclusion criteria and date semantics for currently admitted, admissions and discharges.
- [ ] Provide death-register filtering and authorized date/time/cause-of-death fields; reuse existing death-registration functionality without inferring certificate submission from this report.
- [ ] Provide MLC reporting with accident/location, case and FIR references; restrict legal and clinical detail to authorized roles.
- [ ] Reuse Platform patientmaster and existing encounter, discharge, coding, death and MLC records; reports must not create a second patient or legal-case registry.
- [ ] Use synthetic fixtures to verify inclusion/exclusion, date boundaries, missing coding, missing legal references and tenant/role denials.
- [ ] Audit authorized sensitive report access and exports; define retention and masking with clinical/compliance owners.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1060](https://github.com/tesserix/helivanta/issues/1060) — [PharmaConnect][Exploration] Pharmacy sales, stock, indent and margin reporting

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Reconcile sales, returns, deleted amounts, discounts, receipts and refunds with source transactions using documented sign conventions.
- [ ] Provide patient, item, batch, store, category, clinician and period filters where observed; expose indent statuses with ward/room/bed context only to authorized staff.
- [ ] Separate wholesale-party and patient sales; distinguish patient, insurer, paid, pending and hospital-linked balances without double counting.
- [ ] Define purchase cost, tax, gross/net margin and monthly aggregation formulas with approved examples; test returns, cancellations, rounding and zero denominators.
- [ ] Coordinate stock and indent views with SupplyConnect through published contracts, keeping PharmaConnect dispensing and sales ownership.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1061](https://github.com/tesserix/helivanta/issues/1061) — [PharmaConnect][Exploration] Pharmacy GST and HSN reconciliation reports

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Provide detail, HSN summary and consolidated GST reporting with explicit inclusion or exclusion of exempt sales.
- [ ] Define period, HSN, place-of-supply, GST-rate, taxable value, CGST, SGST, IGST and cess semantics using effective-dated tax policy.
- [ ] Reconcile detail to summaries with synthetic sales/returns, exemptions, interstate supplies, cancellations and rounding fixtures.
- [ ] Use one governed report model for equivalent legacy variants; retain distinct filters and outputs only where requirements justify them.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1062](https://github.com/tesserix/helivanta/issues/1062) — [PharmaConnect][Exploration] Scheduled-drug sales and return registers

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Filter scheduled-drug sales by period, classification, drug and patient; display batch, expiry, quantity, return quantity, type and prescriber with role restrictions.
- [ ] Link register entries to authorized dispensing/return records and Platform patientmaster rather than allowing independent edits to reported facts.
- [ ] Confirm applicable drug schedules, retention and statutory fields with pharmacy/compliance owners; report labels alone do not establish legal compliance.
- [ ] Test sales, partial/full returns, batch/expiry identity, date boundaries and denied roles with synthetic fixtures; audit sensitive access and authorized exports.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1063](https://github.com/tesserix/helivanta/issues/1063) — [MediCore][Exploration] Consultant revenue-share, invoice, TDS and suspense reporting

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Represent consultant share structures by patient type/category and service/group, distinguishing primary and rendering clinician, deductions and discounts.
- [ ] Provide expense, consultant invoice and TDS views with period, clinician/code, voucher, account-ledger and attribution filters as observed.
- [ ] Reconcile invoice amount less TDS to payable amounts with approved policy, effective dates and synthetic rounding/cancellation fixtures.
- [ ] Provide suspense/unallocated-share reporting with source bill/service references; prevent silent loss or double allocation.
- [ ] Record referral and marketing expense reporting as legacy capabilities requiring finance/compliance policy review before implementation; do not infer that every legacy payment rule is approved.
- [ ] Keep patient billing and consultant finance in module-owned MediCore records with published contracts, tenant-scoped permissions, auditable access and masked sensitive identifiers.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1064](https://github.com/tesserix/helivanta/issues/1064) — [MediCore][Exploration] Resolve catalogue-only links and undefined account report screens

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Ensure every entitled report catalogue entry resolves to the intended report view with a stable identifier and defined schema.
- [ ] Provide explicit loading, unavailable-configuration, access-denied, failed-load and empty-result states; do not display a blank grid shell as a successful report.
- [ ] Confirm report ownership, required fields and calculations with finance owners before implementing Diagnostics Share or Billwise Expense; do not invent schemas from titles.
- [ ] Add synthetic route-to-report coverage for both catalogue and breadcrumb paths, including absent definitions and entitlement denial.
- [ ] Disable export actions until a valid authorized report definition and result state exist.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1065](https://github.com/tesserix/helivanta/issues/1065) — [LabConnect][Exploration] Expose explicit availability and definitions for laboratory MIS reports

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Define and authorize each report before exposing it in the catalogue; unavailable definitions must show a clear actionable state.
- [ ] Distinguish no matching records from missing configuration, denied permission and load failure; suppress invalid export actions.
- [ ] Confirm intended investigation master/TAT, investigation-event detail, actor/update audit and integration-log schemas with laboratory owners before development.
- [ ] Integration-log views must redact credentials and minimize patient details; allow role-scoped correlation and failure status without revealing payloads by default.
- [ ] Verify catalogue-to-report routes, empty results, missing definition and role/tenant denial using synthetic fixtures; reuse LabConnect orders/results/events rather than parallel records.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1066](https://github.com/tesserix/helivanta/issues/1066) — [AdminConnect][Exploration] Govern report catalogues, module mappings and reporting-job visibility

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Provide an authorized catalogue of product-owned reports with stable identifiers, grouping and entitlement checks; validate destination routes before publication.
- [ ] Version source/destination mappings and active status with actor/time audit and validation rather than accepting arbitrary ungoverned mappings.
- [ ] Provide read-only reporting-job status, schedule, last success/failure, dependencies and data freshness; distinguish execution completion from freshness of the reported data.
- [ ] Gate any future rerun/edit/delete operations separately with audit and replay safety; no production job operation is authorized by this story discovery.
- [ ] Retain business report definitions and underlying data within their owning product modules; AdminConnect supplies administration, Platform supplies shared operational controls. Do not introduce a new service or a cross-module database reader.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1067](https://github.com/tesserix/helivanta/issues/1067) — [LabConnect][Exploration] Configure hierarchical investigations, reference ranges and analyser mappings

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Reuse the existing shared test/investigation MDM contract (#477), with LabConnect-owned laboratory configuration rather than a duplicate catalogue.
- [ ] Support versioned parent/child parameters, numeric/descriptive result types, methods, units, sample requirements, notes and applicable reference ranges.
- [ ] Validate reference-range applicability, boundary overlaps and units with laboratory approval and synthetic age/sex/range fixtures; never adopt observed legacy numbers as clinical defaults.
- [ ] Map machine and external investigation codes explicitly; constrain and test conversion formulas, unit conversions, missing inputs and invalid expressions before activation.
- [ ] Track accreditation scope and effective dates per investigation/parameter with auditable changes; print flags do not establish accreditation.
- [ ] Separate report/template import permissions from catalogue edits; validate imported formats and avoid executing formulas or markup as arbitrary code.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1068](https://github.com/tesserix/helivanta/issues/1068) — [LabConnect][Exploration] Maintain microbiology organisms, antibiotics, panels and mappings

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Provide governed organism/type, antibiotic/classification/panel and ordered line masters with stable identifiers.
- [ ] Validate organism-antibiotic-panel mappings, duplicates, required fields and retired references; preserve historical report meaning after master changes.
- [ ] Treat order/color as presentation metadata and never infer susceptibility or treatment recommendations from color.
- [ ] Use reviewed terminology and synthetic culture/organism fixtures; test authorized editing, tenant isolation and dependency checks.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1069](https://github.com/tesserix/helivanta/issues/1069) — [LabConnect][Exploration] Configure specimen identification, collection counters and dispatch policy

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Define specimen-ID generation stages and uniqueness scope, preserving traceability across collection, acceptance and departmental mappings.
- [ ] Validate counter/device configuration and barcode layouts using synthetic labels; protect device addresses and restrict administrative changes.
- [ ] Specify order-merge windows and permitted states without merging unrelated patients/specimens or losing attribution.
- [ ] Provide explicit report-finalized, ready-to-dispatch and dispatched states with eligibility, delay and batch limits; reuse published notification/delivery contracts.
- [ ] Handle outsourced attachments with provenance, authorization and duplicate-delivery prevention; do not mark report delivery complete without confirmation.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1070](https://github.com/tesserix/helivanta/issues/1070) — [LabConnect][Exploration] Render governed numeric, free-flow, microbiology and histology reports

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Provide versioned report templates for numeric, free-flow, incubation and histology/cytology with approved required fields and predictable pagination.
- [ ] Preserve result units, applicable reference ranges, methods and analyser provenance; verify rendering using synthetic reports rather than real patient documents.
- [ ] Separate accredited/non-accredited output and apply only approved accreditation scope and labels.
- [ ] Secure QR/download references with patient/report authorization; prohibit public unprotected medical-report URLs.
- [ ] Retain amendment history, author/finalizer identity and report version; a formatting change must not rewrite finalized clinical content.
- [ ] Validate uploaded images/templates and render accessible readable print/PDF layouts without executing embedded content.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1071](https://github.com/tesserix/helivanta/issues/1071) — [LabConnect][Exploration] Define laboratory TAT stages and governed retention for aged requests

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Define event boundaries, units and missing/out-of-order timestamp handling for every TAT metric; test synthetic event sequences and overdue warning thresholds.
- [ ] Distinguish ready-to-dispatch from finalized TAT and use consistent report/worklist calculations.
- [ ] Require an approved clinical/legal retention policy before implementing automated cleanup; preserve audit and clinically required records.
- [ ] Provide preview/counts and explicit authorized administration for any retention action; never silently delete pending, processing or provisional clinical work due to age.
- [ ] Document recoverability and legal holds; validate policy behavior only in synthetic nonproduction fixtures.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1072](https://github.com/tesserix/helivanta/issues/1072) — [LabConnect][Exploration] Manage in-source laboratory partners, sample requests and receivables

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Represent partner facility identity, validated contacts, registration and effective tariff/rate-category mappings with controlled accounting-center linkage.
- [ ] Accept partner requests with stable external identifiers, request/collection times and sample provenance; validate duplicates and chronology with synthetic fixtures.
- [ ] Reuse Platform patientmaster and LabConnect specimen/order identity; do not create a separate partner-patient registry.
- [ ] Track partner invoices, TDS, receipts and balances with explicit partial-payment/cancellation reconciliation; retain product-owned records and published finance contracts.
- [ ] Track report delivery by request, order, sample and partner with explicit error states and auditable status transitions; failed delivery must not be shown as dispatched.
- [ ] Implement safe retries and duplicate-event handling when integrations are built; test tenant/facility access and synthetic end-to-end scenarios.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1073](https://github.com/tesserix/helivanta/issues/1073) — [MediCore][Exploration] Govern prescribing catalogue views and administration-route metadata

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Reuse shared drug MDM (#476) and published PharmaConnect contracts; prescribing configuration belongs to MediCore while stock/dispensing remain PharmaConnect-owned.
- [ ] Preserve stable medicine/ingredient identifiers and source/version provenance; reconcile old/new catalogue variants and duplicate brand entries without silent merging.
- [ ] Validate governed import schemas, duplicates, version upgrades and rollback in nonproduction before any migration; no production import is authorized by exploration.
- [ ] Represent route/code, infusion applicability, diluent and tapering/generic attributes explicitly; clinical owners must approve medication semantics.
- [ ] Test active/inactive selection, source changes, historical prescription identity and denied roles using synthetic medication fixtures.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1074](https://github.com/tesserix/helivanta/issues/1074) — [MediCore][Exploration] Maintain clinically reviewed multilingual prescription dictionaries

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Version translation keys and reviewed language variants for prescription instructions without changing medicine identity, dose, units or frequency.
- [ ] Require clinician/qualified-language review of safety-relevant translations; do not treat visible legacy translations as clinically approved.
- [ ] Define fallback and missing-translation behavior and render Unicode correctly in UI and exported prescriptions.
- [ ] Test representative multilingual instructions, numeral/unit preservation, line wrapping and historical rendering with synthetic prescriptions.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1075](https://github.com/tesserix/helivanta/issues/1075) — [MediCore][Exploration] Version clinical card definitions and encounter-specific template composition

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Version reusable card definitions and template composition by encounter type, case type, department and practitioner/nursing context.
- [ ] Enforce approved required vitals/allergies and applicability rules while preserving signed historical encounters when templates change.
- [ ] Map every inventoried card to an existing feature or a separately reviewed implementation scope; do not treat a card title as a complete clinical schema.
- [ ] Verify each renderer in synthetic encounters before marking coverage complete, including save/version/signature lifecycle, accessibility, permissions and patient isolation.
- [ ] Clinical scores require approved instrument/version, licensing where applicable, input units, missing-data behavior and independently checked scoring fixtures; never infer algorithms from names.
- [ ] Preserve specialty coverage across ophthalmology, obstetrics, paediatrics/neonatal, diabetes, mental health, nutrition, oncology, nursing, discharge and general examination without duplicate patient records.
- [ ] Consolidate legacy duplicate/old/new cards only with an explicit historical-data mapping and clinical approval.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1076](https://github.com/tesserix/helivanta/issues/1076) — [MediCore][Exploration] Govern disease terminology and assessment-to-service mappings

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Preserve code-system/version identity separately from display names, synonyms and favorites; validate duplicate code/name records without silently discarding history.
- [ ] Search diagnoses by approved aliases and code system, retaining original code provenance in recorded encounters.
- [ ] Define assessment-service applicability by case type, department, tariff, duration and nursing context with deterministic precedence.
- [ ] Use approved jurisdiction-specific coding/billing rules; presence of a CPT field does not mandate its use in every deployment.
- [ ] Test effective dates, duplicate mappings, duration boundaries and clinical-to-billing handoffs using synthetic cases.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1077](https://github.com/tesserix/helivanta/issues/1077) — [MediCore][Exploration] Configure socioeconomic assessment and nursing documentation policy

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Define role-scoped socioeconomic sections with purpose limitation, consent/retention policy and minimum necessary access; no automated eligibility decision is inferred.
- [ ] Map nursing ABG/glucose activities to approved billing services without generating duplicate charges; test retries and cancelled activities.
- [ ] Clearly distinguish provisional from finalized discharge documentation and preserve clinician attribution.
- [ ] Version the chosen fall-risk instrument and clinically validate scoring instead of silently substituting scales.
- [ ] Keep integration secrets outside ordinary configuration output, audit privileged changes and require separate review for enabling external AI data flows.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1078](https://github.com/tesserix/helivanta/issues/1078) — [MediCore][Exploration] Render consistent configurable patient, insurer and copay bill documents

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Define versioned approved bill layouts for patient, insurer, copay, package, tax and day-wise outputs, preserving finalized bill content and traceability.
- [ ] Use a single computed monetary source for numeric totals, amount-in-words, shares, discounts, rounding and balances; add synthetic regression fixtures for the observed preview mismatch.
- [ ] Restrict optional patient/contact/deceased/insurance fields by document purpose and permissions; QR/barcodes must not expose unauthorized records.
- [ ] Reconcile linked/non-linked pharmacy amounts and package details without duplicate totals; validate service/tax codes and jurisdiction-specific remarks.
- [ ] Render readable multi-page A4/A5/PDF documents with validated margins, repeated headers, signatures and attribution; test long names, multilingual text and page breaks.
- [ ] Consent/signature assets require authorized provenance; never imply a signature or certification merely because a template contains its placeholder.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1079](https://github.com/tesserix/helivanta/issues/1079) — [MediCore][Exploration] Generate governed encounter-linked certificates from versioned templates

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

- [ ] Version certificate templates and approved placeholders with explicit OPD/IPD applicability and mandatory fields.
- [ ] Resolve placeholders from authorized patient/encounter records, distinguishing missing facts from blank text and preserving source/version provenance.
- [ ] Require authorized clinician review and attribution for clinical certification; generation must not automatically sign, send or assert unverified facts.
- [ ] Escape template content and validate placeholder types; test dates, pronouns, multilingual text and missing-data behavior using synthetic encounters.
- [ ] Audit generation/amendment/access and apply minimum necessary personal/clinical details with retention policy.
- [ ] Given empty results, failed loads, invalid filters or restricted access, the UI shows explicit accessible states without stale records.
- [ ] A synthetic walkthrough covers every enumerated capability; intentional departures have documented clinical/operational rationale.

### [1080](https://github.com/tesserix/helivanta/issues/1080) — [SupplyConnect][Exploration] Handle company configuration load failure in reorder settings

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

Read the owning issue for its defect-specific acceptance requirements.

### [1081](https://github.com/tesserix/helivanta/issues/1081) — [MediCore][Exploration] Govern radiology PACS accession and study-viewer configuration

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

Read the owning issue for its defect-specific acceptance requirements.

### [1082](https://github.com/tesserix/helivanta/issues/1082) — [MediCore][Exploration] Version radiology report amendment, dispatch and turnaround policies

Owner: Samyak Rout. Delivery status: open; acceptance not fully verified.

Read the owning issue for its defect-specific acceptance requirements.

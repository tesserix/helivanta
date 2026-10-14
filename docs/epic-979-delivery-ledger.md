# Epic #979 delivery ledger

Parent: [Exploration #979](https://github.com/tesserix/helivanta/issues/979).
GitHub hierarchy reconciled on 15 October 2026: **109 descendants, 100 direct
and 9 nested; 107 open, 2 closed; all assigned**. User scope includes every
story, completed one at a time through implementation and real acceptance
verification. Exploration notes/menu coverage alone are not implementation
or transactional end-to-end proof.

## Closure evidence

Each story needs its acceptance-criterion audit and applicable sanitized legacy
mapping, owning schema/API/UI implementation, current tenant/multi-hospital
and permission-denial tests, duplicate/retry/concurrency tests, local real
PostgreSQL/CNPG and browser verification, CI, exact-source GitOps deployment,
and post-deployment synthetic API/UI/log verification. External integrations
need real vendor acceptance in addition to protocol tests. Dependencies and
unknown source paths remain explicit. No customer values, clinical/employee
outcomes, credentials or private screenshots belong in public evidence.

The closed workforce slices are #1109 (shift definitions, #1110, release
`86e83cba2b9b617262015ce9416547a7f1763b28`) and #1111 (roster areas/groups/
weekly templates, #1115, subsequent verified Go 1.27.2 release
`696896be78ce2802786f3b96a2145e6616a9b0ba`). Their parents #1036 and #979
remain open. Existing delivered core enrollment/registration work does not
close the broader owning stories automatically.

## Current sequence

1. Finish #1105 private staff files, with #483/#29/#688/#6 integration
   dependencies, actual GCS identities, scan/promotion, Temporal and UI proof.
2. Complete registry children #1106–#1108 and remaining #1035 acceptance.
3. Finish dated roster/allocations #1036, leave #1037, corrections #1038,
   attendance #1039, policy #1041, payroll #1040 and appraisal #1042.
4. Audit and complete the remaining product-owned descendants and their
   clinical, pharmacy, lab, stock, finance, identity and reporting handoffs.
   Preserve shared patient identity and module-owned data; do not create
   duplicate implementations to satisfy a checkbox.

Private-file foundation checks are local only. The backend draft and storage
proposal do not close #1105. Reviewed jurisdictional/clinical/financial policy
is required where a calculation or consequential outcome depends on it.

Latest evidence: [private staff-file local verification, 15 October](private-staff-files-verification-2026-10-15.md).
Binary upload and an atomic outbox handoff now have real PostgreSQL/NATS,
concurrency, grant-revocation and permission-isolation proof. The unsafe
shared-storage saved plans were discarded; the isolated replacement plans
only 17 additions, with zero updates/deletions, and awaits production approval.
Neither draft has been deployed, and no additional story has been closed.
The source application is inspected read-only; writes are synthetic in
explicitly isolated verification environments.

## Reconciled hierarchy

GitHub state is a snapshot, not a new completion claim. Open rows still need
an acceptance audit even when parts already exist in the product.

| Story | Parent | GitHub state | Assignee | Verification position |
|---|---|---|---|---|
| [#980 — [Platform][Exploration] Unify patient search and identity across clinical, lab and pharmacy workflows](https://github.com/tesserix/helivanta/issues/980) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#981 — [MediCore][Exploration] Preserve outpatient reception queues, filters and encounter context](https://github.com/tesserix/helivanta/issues/981) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#982 — [MediCore][Exploration] Model hospital bill states, balances and cashier reporting explicitly](https://github.com/tesserix/helivanta/issues/982) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#983 — [MediCore][Exploration] Preserve inpatient admission, ward and discharge worklist distinctions](https://github.com/tesserix/helivanta/issues/983) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#984 — [MediCore][Exploration] Implement appointment worklist, calendar views and explicit status transitions](https://github.com/tesserix/helivanta/issues/984) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#985 — [LabConnect][Exploration] Implement the laboratory order-to-dispatch worklist with specimen traceability](https://github.com/tesserix/helivanta/issues/985) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#986 — [LabConnect][Exploration] Make critical, delayed and outsourced laboratory work actionable](https://github.com/tesserix/helivanta/issues/986) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#987 — [PharmaConnect][Exploration] Implement pharmacy sales worklists with encounter linkage and liability breakdown](https://github.com/tesserix/helivanta/issues/987) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#988 — [PharmaConnect][Exploration] Provide pharmacy-owned category, discount, template and indent configuration](https://github.com/tesserix/helivanta/issues/988) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#989 — [Platform][Exploration] Keep module labels and navigation consistent across multiple tabs](https://github.com/tesserix/helivanta/issues/989) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#990 — [Platform][Exploration] Replace ambiguous icon controls with accessible, explicit actions](https://github.com/tesserix/helivanta/issues/990) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#991 — [MediCore][Exploration] Provide clinician OPD/IPD worklists with visit timing and ward context](https://github.com/tesserix/helivanta/issues/991) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#992 — [MediCore][Exploration] Support tumor board scheduling and multidisciplinary review tracking](https://github.com/tesserix/helivanta/issues/992) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#993 — [MediCore][Exploration] Manage chemotherapy scheduling and review states with pharmacy handoffs](https://github.com/tesserix/helivanta/issues/993) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#994 — [MediCore][Exploration] Track radiation counselling, planning and execution as linked clinical stages](https://github.com/tesserix/helivanta/issues/994) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#995 — [MediCore][Exploration] Preserve diagnostic report dispatch, scheduling and radiology review workflow](https://github.com/tesserix/helivanta/issues/995) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#996 — [MediCore][Exploration] Schedule theatre rooms and resources with conflict-aware capacity](https://github.com/tesserix/helivanta/issues/996) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#997 — [MediCore][Exploration] Connect pre-operative and operative theatre worklists to the surgical case](https://github.com/tesserix/helivanta/issues/997) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#998 — [MediCore][Exploration] Version surgical consent templates and peri-operative checklists](https://github.com/tesserix/helivanta/issues/998) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#999 — [MediCore][Exploration] Map surgery catalogue and theatre team services into auditable billing](https://github.com/tesserix/helivanta/issues/999) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1000 — [SupplyConnect][Exploration] Compare supplier quotations and manage effective rate contracts](https://github.com/tesserix/helivanta/issues/1000) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1001 — [SupplyConnect][Exploration] Track purchase requisitions through sanctioned purchase orders](https://github.com/tesserix/helivanta/issues/1001) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1002 — [SupplyConnect][Exploration] Reconcile challans, goods receipts and supplier invoices](https://github.com/tesserix/helivanta/issues/1002) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1003 — [SupplyConnect][Exploration] Process traceable supplier returns and related stock/credit reconciliation](https://github.com/tesserix/helivanta/issues/1003) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1004 — [SupplyConnect][Exploration] Manage store inventory and demand-based replenishment thresholds](https://github.com/tesserix/helivanta/issues/1004) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1005 — [SupplyConnect][Exploration] Maintain item, supplier and retailer masters with validated business identities](https://github.com/tesserix/helivanta/issues/1005) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1006 — [SupplyConnect][Exploration] Configure procurement departments, price factors and document layouts](https://github.com/tesserix/helivanta/issues/1006) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1007 — [SupplyConnect][Exploration] Fulfil store indents with attributable goods and patient issues](https://github.com/tesserix/helivanta/issues/1007) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1008 — [SupplyConnect][Exploration] Track store transfers and returns as balanced stock movements](https://github.com/tesserix/helivanta/issues/1008) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1009 — [SupplyConnect][Exploration] Reconcile batch ledgers, consumption and stock-posting exceptions](https://github.com/tesserix/helivanta/issues/1009) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1010 — [SupplyConnect][Exploration] Control stock imports, recounts and reasoned inventory adjustments](https://github.com/tesserix/helivanta/issues/1010) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1011 — [SupplyConnect][Exploration] Configure store roles, locations and rack/shelf assignments](https://github.com/tesserix/helivanta/issues/1011) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1012 — [FacilityConnect][Exploration] Track fixed-asset allocation and time-bound gate-pass custody](https://github.com/tesserix/helivanta/issues/1012) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1013 — [MediCore][Exploration] Resolve dietary workflow coverage unavailable to the current legacy session](https://github.com/tesserix/helivanta/issues/1013) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1014 — [MediCore][Exploration] Reconcile collections, payables and payment-mode analytics with source transactions](https://github.com/tesserix/helivanta/issues/1014) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1015 — [MediCore][Exploration] Provide admission, geographic and clinician operational analytics](https://github.com/tesserix/helivanta/issues/1015) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1016 — [SupplyConnect][Exploration] Deliver procurement and inventory classification reports with explainable calculations](https://github.com/tesserix/helivanta/issues/1016) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1017 — [MediCore][Exploration] Preserve clinical and oncology analytics with defined cohorts and drill-downs](https://github.com/tesserix/helivanta/issues/1017) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1018 — [MediCore][Exploration] Scope workforce analytics independently from clinical and financial access](https://github.com/tesserix/helivanta/issues/1018) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1019 — [MediCore][Exploration] Manage accounting transaction worklists and traceable voucher lifecycles](https://github.com/tesserix/helivanta/issues/1019) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1020 — [MediCore][Exploration] Reconcile bank and forex transactions with explicit matching and exceptions](https://github.com/tesserix/helivanta/issues/1020) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1021 — [MediCore][Exploration] Control ERP and ledger posting with reviewed mappings and recoverable exceptions](https://github.com/tesserix/helivanta/issues/1021) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1022 — [MediCore][Exploration] Review insurance adjustments with attributable financial approval](https://github.com/tesserix/helivanta/issues/1022) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1023 — [MediCore][Exploration] Produce reconciled books, ledger statements and financial statements](https://github.com/tesserix/helivanta/issues/1023) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1024 — [MediCore][Exploration] Reconcile tax registers and GST report outputs under approved current rules](https://github.com/tesserix/helivanta/issues/1024) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1025 — [MediCore][Exploration] Analyse revenue, profitability and receivables across hospital dimensions](https://github.com/tesserix/helivanta/issues/1025) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1026 — [MediCore][Exploration] Version chart-of-accounts, tax, currency and accounting-center configuration](https://github.com/tesserix/helivanta/issues/1026) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1027 — [MediCore][Exploration] Reconcile cashier handovers, counter verification and deposit mismatches](https://github.com/tesserix/helivanta/issues/1027) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1028 — [MediCore][Exploration] Preserve refund processing and receipt-cancellation audit trails](https://github.com/tesserix/helivanta/issues/1028) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1029 — [PharmaConnect][Exploration] Track pharmacy refund requests through hospital accounting settlement](https://github.com/tesserix/helivanta/issues/1029) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1030 — [MediCore][Exploration] Preserve discharge, discount, balance and service-group accounting reports](https://github.com/tesserix/helivanta/issues/1030) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1031 — [MediCore][Exploration] Track payment-provider requests and reconciliation states](https://github.com/tesserix/helivanta/issues/1031) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1032 — [MediCore][Exploration] Resolve Grants module navigation failure and complete grants discovery](https://github.com/tesserix/helivanta/issues/1032) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1033 — [MediCore][Exploration] Manage insurer claim batches, cover letters and bill-level sanctions](https://github.com/tesserix/helivanta/issues/1033) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1034 — [MediCore][Exploration] Reconcile insurance receipts, deductions, unmapped money and ageing](https://github.com/tesserix/helivanta/issues/1034) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1035 — [MediCore][Exploration] Maintain employee and consultant registries with controlled HR master data](https://github.com/tesserix/helivanta/issues/1035) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1036 — [MediCore][Exploration] Plan company-scoped shifts, rosters and locum coverage](https://github.com/tesserix/helivanta/issues/1036) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1037 — [MediCore][Exploration] Manage leave balances, allocation structures and approval requests](https://github.com/tesserix/helivanta/issues/1037) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1038 — [MediCore][Exploration] Audit duty exchanges, overtime and missing-punch corrections](https://github.com/tesserix/helivanta/issues/1038) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1039 — [MediCore][Exploration] Reconcile attendance, exceptions and timesheet finalisation](https://github.com/tesserix/helivanta/issues/1039) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1040 — [MediCore][Exploration] Preserve payroll preparation, locking, processing and correction stages](https://github.com/tesserix/helivanta/issues/1040) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1041 — [MediCore][Exploration] Version salary, tax, payslip and HR policy configuration](https://github.com/tesserix/helivanta/issues/1041) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1042 — [MediCore][Exploration] Manage performance objectives and appraisal review lifecycle](https://github.com/tesserix/helivanta/issues/1042) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1043 — [MediCore][Exploration] Configure ward hierarchy, occupancy and departmental discharge clearances](https://github.com/tesserix/helivanta/issues/1043) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1044 — [MediCore][Exploration] Govern shared financial masters and staged period closure across products](https://github.com/tesserix/helivanta/issues/1044) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1045 — [MediCore][Exploration] Version tariffs, payer categories and service pricing precedence](https://github.com/tesserix/helivanta/issues/1045) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1046 — [MediCore][Exploration] Govern service catalogues, groups, taxes and accounting mappings](https://github.com/tesserix/helivanta/issues/1046) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1047 — [MediCore][Exploration] Configure consultation follow-up entitlements and room charges](https://github.com/tesserix/helivanta/issues/1047) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1048 — [MediCore][Exploration] Model billing bundles, health packages, classifications and multi-visit entitlements](https://github.com/tesserix/helivanta/issues/1048) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1049 — [MediCore][Exploration] Configure doctor groups and effective theatre charging templates](https://github.com/tesserix/helivanta/issues/1049) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1050 — [MediCore][Exploration] Preserve patient-organisation reference history](https://github.com/tesserix/helivanta/issues/1050) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1051 — [PharmaConnect][Exploration] Map pharmacy sales into hospital bills without duplicate revenue](https://github.com/tesserix/helivanta/issues/1051) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1052 — [LabConnect][Exploration] Govern outsourced diagnostic partners and billing references](https://github.com/tesserix/helivanta/issues/1052) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1053 — [AdminConnect][Exploration] Reconcile workforce onboarding and product access administration](https://github.com/tesserix/helivanta/issues/1053) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1054 — [Platform][Exploration] Reconcile legacy profile recovery and integration credential workflows with platform identity](https://github.com/tesserix/helivanta/issues/1054) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1055 — [AdminConnect][Exploration] Govern identifier prefixes and scoped demographic reference masters](https://github.com/tesserix/helivanta/issues/1055) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1056 — [MediCore][Exploration] Reconcile terminal payment-request status with receipts](https://github.com/tesserix/helivanta/issues/1056) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1057 — [Platform][Exploration] Reconcile multi-channel event notifications and protected attachments](https://github.com/tesserix/helivanta/issues/1057) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1058 — [Platform][Exploration] Configure shared patient registration sections and classification rules](https://github.com/tesserix/helivanta/issues/1058) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1059 — [MediCore][Exploration] Medical-record reporting for coding gaps, deaths and medico-legal cases](https://github.com/tesserix/helivanta/issues/1059) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1060 — [PharmaConnect][Exploration] Pharmacy sales, stock, indent and margin reporting](https://github.com/tesserix/helivanta/issues/1060) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1061 — [PharmaConnect][Exploration] Pharmacy GST and HSN reconciliation reports](https://github.com/tesserix/helivanta/issues/1061) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1062 — [PharmaConnect][Exploration] Scheduled-drug sales and return registers](https://github.com/tesserix/helivanta/issues/1062) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1063 — [MediCore][Exploration] Consultant revenue-share, invoice, TDS and suspense reporting](https://github.com/tesserix/helivanta/issues/1063) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1064 — [MediCore][Exploration] Resolve catalogue-only links and undefined account report screens](https://github.com/tesserix/helivanta/issues/1064) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1065 — [LabConnect][Exploration] Expose explicit availability and definitions for laboratory MIS reports](https://github.com/tesserix/helivanta/issues/1065) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1066 — [AdminConnect][Exploration] Govern report catalogues, module mappings and reporting-job visibility](https://github.com/tesserix/helivanta/issues/1066) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1067 — [LabConnect][Exploration] Configure hierarchical investigations, reference ranges and analyser mappings](https://github.com/tesserix/helivanta/issues/1067) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1068 — [LabConnect][Exploration] Maintain microbiology organisms, antibiotics, panels and mappings](https://github.com/tesserix/helivanta/issues/1068) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1069 — [LabConnect][Exploration] Configure specimen identification, collection counters and dispatch policy](https://github.com/tesserix/helivanta/issues/1069) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1070 — [LabConnect][Exploration] Render governed numeric, free-flow, microbiology and histology reports](https://github.com/tesserix/helivanta/issues/1070) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1071 — [LabConnect][Exploration] Define laboratory TAT stages and governed retention for aged requests](https://github.com/tesserix/helivanta/issues/1071) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1072 — [LabConnect][Exploration] Manage in-source laboratory partners, sample requests and receivables](https://github.com/tesserix/helivanta/issues/1072) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1073 — [MediCore][Exploration] Govern prescribing catalogue views and administration-route metadata](https://github.com/tesserix/helivanta/issues/1073) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1074 — [MediCore][Exploration] Maintain clinically reviewed multilingual prescription dictionaries](https://github.com/tesserix/helivanta/issues/1074) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1075 — [MediCore][Exploration] Version clinical card definitions and encounter-specific template composition](https://github.com/tesserix/helivanta/issues/1075) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1076 — [MediCore][Exploration] Govern disease terminology and assessment-to-service mappings](https://github.com/tesserix/helivanta/issues/1076) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1077 — [MediCore][Exploration] Configure socioeconomic assessment and nursing documentation policy](https://github.com/tesserix/helivanta/issues/1077) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1078 — [MediCore][Exploration] Render consistent configurable patient, insurer and copay bill documents](https://github.com/tesserix/helivanta/issues/1078) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1079 — [MediCore][Exploration] Generate governed encounter-linked certificates from versioned templates](https://github.com/tesserix/helivanta/issues/1079) | [#979](https://github.com/tesserix/helivanta/issues/979) | open | Sam123ben | Open acceptance audit / delivery |
| [#1081 — [MediCore][Exploration] Govern radiology PACS accession and study-viewer configuration](https://github.com/tesserix/helivanta/issues/1081) | [#995](https://github.com/tesserix/helivanta/issues/995) | open | Sam123ben | Open acceptance audit / delivery |
| [#1082 — [MediCore][Exploration] Version radiology report amendment, dispatch and turnaround policies](https://github.com/tesserix/helivanta/issues/1082) | [#995](https://github.com/tesserix/helivanta/issues/995) | open | Sam123ben | Open acceptance audit / delivery |
| [#1080 — [SupplyConnect][Exploration] Handle company configuration load failure in reorder settings](https://github.com/tesserix/helivanta/issues/1080) | [#1004](https://github.com/tesserix/helivanta/issues/1004) | open | Sam123ben | Open acceptance audit / delivery |
| [#1105 — [MediCore][HR] Quarantine, scan and release staff documents](https://github.com/tesserix/helivanta/issues/1105) | [#1035](https://github.com/tesserix/helivanta/issues/1035) | open | Sam123ben | Open acceptance audit / delivery |
| [#1106 — [MediCore][HR] Manage employee dependents and benefit enrollment](https://github.com/tesserix/helivanta/issues/1106) | [#1035](https://github.com/tesserix/helivanta/issues/1035) | open | Sam123ben | Open acceptance audit / delivery |
| [#1107 — [MediCore][HR] Version employment letters and employee exit workflows](https://github.com/tesserix/helivanta/issues/1107) | [#1035](https://github.com/tesserix/helivanta/issues/1035) | open | Sam123ben | Open acceptance audit / delivery |
| [#1108 — [MediCore][HR] Track credential expiry and reviewed renewal reminders](https://github.com/tesserix/helivanta/issues/1108) | [#1035](https://github.com/tesserix/helivanta/issues/1035) | open | Sam123ben | Open acceptance audit / delivery |
| [#1109 — [MediCore][HR] Version hospital shift definitions](https://github.com/tesserix/helivanta/issues/1109) | [#1036](https://github.com/tesserix/helivanta/issues/1036) | closed | Sam123ben | Deployed slice; evidence above |
| [#1111 — [MediCore][HR] Version roster areas, groups and weekly templates](https://github.com/tesserix/helivanta/issues/1111) | [#1036](https://github.com/tesserix/helivanta/issues/1036) | closed | Sam123ben | Deployed slice; evidence above |

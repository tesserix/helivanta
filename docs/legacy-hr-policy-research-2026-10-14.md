# Legacy HR policy research — 14 October 2026

Read-only inspection of the authenticated legacy HR interface. No records were
saved, exported, printed or copied into fixtures. This note records field
semantics, not configured amounts, defaults, employee details or legal policy.
Stories #1037–#1042 and parent #979 remain open.

| Legacy view | Observed fields/controls | Helivanta requirement |
|---|---|---|
| `Appraisal.htm`, blank request | Employee lookup; department, designation, joining/confirmation dates, grade/status; request date; appraisal start/end period | Versioned period and employee/job snapshot, explicit reviewer authorization, auditable request/review transitions; no automatic employment decision |
| `SalaryTemplateList.htm` / blank `SalaryTemplate.htm?templateID=0` | Department/designation/grade filters and bindings, template name, Earnings/Deduction/CTC head classification and search | Effective, versioned policy and template-head references; evaluated historical runs retain their original configuration |
| `SalarySetup.htm`, Earnings | Applicable months; head name/code/system reference; percentage/fixed type; standard/custom value; taxable/exemption limit; ordering; actual-working-day and shift flags; rounding; present/payable-day thresholds; gross-pay, CTC, zero-line and arrears flags | Decimal money and percentages, explicit bases/day rules, effective policy versions, configured rounding and visible payslip classifications |
| `SalarySetup.htm`, Deduction | Applicable months; name/code/system reference; percentage/fixed value; gross/head bases; standard/custom values; employee/company contribution limits; company percentage; present/payable-day thresholds; order/rounding; mandatory, active, system, other-deduction, tax-exempt, CTC, zero-line and arrears flags | Separate employee deduction and employer contribution, explicit caps/bases and statutory jurisdiction; no copied legacy amounts or inferred default tax treatment |
| `SalarySetup.htm`, CTC | Name/code/system reference; calculation action; fixed amount; standard/custom value; order/rounding; CTC, mandatory, active and system flags | Separate employer cost from net-pay effects; reviewed formula dependencies and immutable calculated-run snapshots |
| `SalarySetup.htm`, OT | Name/head code/system reference; percentage/fixed amount and head bases; standard/custom value; rounding; HH:MM applicability; working-hours and monthly-day basis; shift/roster binding; taxable/active flags | Approved attendance/OT units, explicit rate basis and effective policy; timezone-safe durations and audited punch corrections feed calculations |
| `SalarySetup.htm`, Leave | Display name/type; percentage and selected earning/deduction bases; uninformed-leave count; standard/custom value; rounding; separate-head and exemption flags | Explicit approved leave/policy integration; no implicit pay penalty or employment decision based on a copied field |
| `LeaveStructure.htm?structureID=0` | Financial year/template name; leave, annual quota, minimum/maximum application, carry-forward and encashment days | Effective reviewed entitlements and balance ledger with idempotent allocation/approval/cancellation/encashment |
| `LeaveSetup.htm`, Leave Master | Type classification (casual, medical, privilege, compensatory off, LWP, other, short, optional); name/code; half-day, balance check, paid, clubbing, short-leave and active controls | Versioned leave type and eligibility rules; explicit policy semantics with no copied legacy flag defaults |
| `LeaveSetup.htm`, Holiday Master | Date/name and calendar-year interval; categories **Regular**, **Government**, **Optional**; list labels include **Govt Mandatory** | Effective scoped holiday calendar and explicit category semantics; reviewed jurisdictional requirements, no inferred mandatory dates |
| `EmployeeList.htm` | Joining-date interval; department/division/status/grade/category/user-type and name/code filters; DOB/DOJ columns | Accessible reusable filters and bounded scoped pages; private staff enrollment/file/payroll data stays outside directory responses |

Rendered payslip detail and inner appraisal review/normalization remain to be
inspected. Do not mark those
legacy coverage gaps complete based on a menu or blank request alone.

Payslip execution and statutory policy require reviewed tenant/jurisdiction
configuration. Legacy labels guide completeness; they do not authorize copying
customer payroll settings, deciding real employee outcomes or exporting data.

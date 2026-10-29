# Staff Management open-issue map — 2026-10-30

Snapshot: all 935 open repository issue headers were fetched (limit 1,000; the
result was below the limit). A title/label candidate scan identified 39 entries:
21 directly relevant Staff stories, nine related ownership/access dependencies
and nine keyword matches outside Staff scope. This is a navigation aid, not a
claim that every issue body or acceptance criterion has been audited, or that
open issues have no implementation. Read each body and verify source before its
implementation/acceptance slice. The [delivery tracker](staff-management-progress.md)
and [acceptance ledger](staff-management-acceptance.md) govern completion.

The 21 direct issue headers below are not a one-to-one mapping to the tracker’s
21 acceptance areas. No issue-count ratio is an engineering completion percentage.
No broad issue is closed by the employment draft/evidence release.

## Direct Staff story candidates — all open at snapshot

| Issue                                                      | Current title                                                                                      |
| ---------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| [#118](https://github.com/tesserix/helivanta/issues/118)   | HR, payroll & staff scheduling                                                                     |
| [#134](https://github.com/tesserix/helivanta/issues/134)   | Oncall roster                                                                                      |
| [#417](https://github.com/tesserix/helivanta/issues/417)   | Hospital staff accounts                                                                            |
| [#419](https://github.com/tesserix/helivanta/issues/419)   | Laboratory staff accounts                                                                          |
| [#558](https://github.com/tesserix/helivanta/issues/558)   | Role-based staff dashboards                                                                        |
| [#589](https://github.com/tesserix/helivanta/issues/589)   | Staff-productivity reports                                                                         |
| [#617](https://github.com/tesserix/helivanta/issues/617)   | Staff import                                                                                       |
| [#655](https://github.com/tesserix/helivanta/issues/655)   | Staff participation & attendance (QR check-in, competency)                                         |
| [#1018](https://github.com/tesserix/helivanta/issues/1018) | [MediCore][Exploration] Scope workforce analytics independently from clinical and financial access |
| [#1035](https://github.com/tesserix/helivanta/issues/1035) | [MediCore][Exploration] Maintain employee and consultant registries with controlled HR master data |
| [#1036](https://github.com/tesserix/helivanta/issues/1036) | [MediCore][Exploration] Plan company-scoped shifts, rosters and locum coverage                     |
| [#1037](https://github.com/tesserix/helivanta/issues/1037) | [MediCore][Exploration] Manage leave balances, allocation structures and approval requests         |
| [#1038](https://github.com/tesserix/helivanta/issues/1038) | [MediCore][Exploration] Audit duty exchanges, overtime and missing-punch corrections               |
| [#1039](https://github.com/tesserix/helivanta/issues/1039) | [MediCore][Exploration] Reconcile attendance, exceptions and timesheet finalisation                |
| [#1040](https://github.com/tesserix/helivanta/issues/1040) | [MediCore][Exploration] Preserve payroll preparation, locking, processing and correction stages    |
| [#1041](https://github.com/tesserix/helivanta/issues/1041) | [MediCore][Exploration] Version salary, tax, payslip and HR policy configuration                   |
| [#1053](https://github.com/tesserix/helivanta/issues/1053) | [AdminConnect][Exploration] Reconcile workforce onboarding and product access administration       |
| [#1107](https://github.com/tesserix/helivanta/issues/1107) | [MediCore][HR] Version employment letters and employee exit workflows                              |
| [#1132](https://github.com/tesserix/helivanta/issues/1132) | [MediCore] Staff self-service leave requests via a staff-to-login link                             |
| [#1261](https://github.com/tesserix/helivanta/issues/1261) | Workforce: tidy pages to the one-look design                                                       |
| [#1399](https://github.com/tesserix/helivanta/issues/1399) | [MediCore][HR] Review staff photos and issue revocable identity cards                              |

## Related dependencies — all open at snapshot

Department/branch ownership, operational department permissions, product access
and recovery must be reconciled with current tenant/hospital owners. Facility
vendor/contractor service management (#298/#299) is distinct from an individual
or agency worker’s HR employment period; do not close either story from the other.

| Issue                                                      | Current title                                                                                                         |
| ---------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| [#22](https://github.com/tesserix/helivanta/issues/22)     | Branch and department hierarchy                                                                                       |
| [#287](https://github.com/tesserix/helivanta/issues/287)   | Department master data                                                                                                |
| [#298](https://github.com/tesserix/helivanta/issues/298)   | Contractor management                                                                                                 |
| [#299](https://github.com/tesserix/helivanta/issues/299)   | Contractor SLA breaches                                                                                               |
| [#429](https://github.com/tesserix/helivanta/issues/429)   | Department-level permissions                                                                                          |
| [#562](https://github.com/tesserix/helivanta/issues/562)   | Department-specific screens                                                                                           |
| [#616](https://github.com/tesserix/helivanta/issues/616)   | Department setup                                                                                                      |
| [#1054](https://github.com/tesserix/helivanta/issues/1054) | [Platform][Exploration] Reconcile legacy profile recovery and integration credential workflows with platform identity |
| [#1289](https://github.com/tesserix/helivanta/issues/1289) | Sign in in one step: a deliberate 'Sign in' goes straight to the credential form                                      |

## Excluded keyword matches

#116/#119 concern emergency clinical workflows; #360 is FacilityConnect field
service supervisor verification; #520 concerns department stores; #599 generic
background jobs; #649 emergency response plans; #793 agent credential brokering;
#1006 SupplyConnect procurement departments; #1043 ward/occupancy and patient
discharge clearance. These owners remain important, but their completion cannot
be inferred from Staff HR metadata or this release.

## Execution order and acceptance

1. Scoped HR intake administration, current-account authority and a safe person
   basis for new hires/non-digital workers. Verify owner grants, current IAM
   assignment/link generations, required profile fields and edit persistence.
2. Protected identity/background/photo evidence and independent review; immutable
   employment submission/review. No approval from a missing target identity or
   presumed job-title authority; do not activate employment just to create its
   identity prerequisite.
3. Effective expiry, suspension/resumption, notice/resignation/termination,
   rehire and legacy-grant reconciliation under ADR-0049. Prove every actual
   consumer denies affected old sessions/tuples/queued work at the cutoff while
   preserving governance, other hospitals and clinical/financial history.
4. Dated transfers/postings, supervision, department owner linkage and handover.
5. Full HR setup, shifts/rosters, leave/balances/review, exchanges, overtime,
   attendance/finalization/corrections and downstream ledgers, with role/state
   and dependency failures plus real local and deployed UI acceptance.
6. Reviewed jurisdictional payroll execution/posting/corrections and finalized
   data for branded Go-generated payslips; then controlled offer/exit documents,
   photographs and revocable cards with disclosure/reissue/expiry/offboarding.

Existing foundations may support these journeys, but passing a configuration
preview, draft receipt, route registry, deployment or denied screen does not
complete them. Country/state statutory payroll and employment policies still
need applicable primary-source and qualified-owner review before activation.

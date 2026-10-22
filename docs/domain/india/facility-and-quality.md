# Facility operations, licensing and quality evidence

[India knowledge index](README.md) · proposed specification; legal/accreditation applicability requires qualified review

## Applicable authority

**IN-FQ-01:** Maintain a facility/service applicability register: tenant, hospital/legal entity, state/UT, facility and service type, authority, licence/certificate number, scope, evidence, issue/validity dates, renewal/review status, accountable owner and source policy version. Avoid a single `india_compliant` or `nabh_enabled` flag.

The Clinical Establishments portal distinguishes adopted and non-adopted jurisdictions; state hospital/nursing-home law and local requirements must be assessed rather than assuming one nationwide registration rule. NABH accreditation, NABL accreditation, payer empanelment, professional registration and statutory licences are separate authorities. Verify exact scope and validity. [Facility sources and review limits](sources.md#care-facilities-and-quality).

## Operational records and safety gates

| Requirement                                              | Proposed record/behavior                                                                                                                                                           | Acceptance evidence                                                                                                                                      |
| -------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| IN-FQ-02 Assets and maintenance                          | Stable asset/location/service IDs, manufacturer/serial identity, maintenance/calibration evidence, readiness, downtime and release authority                                       | An available appointment/bed cannot rely on unavailable equipment; maintenance completion does not automatically close a clinical restriction            |
| IN-FQ-03 Housekeeping and infection-control coordination | Task linked to bed/room/encounter context as permitted, required protocol/version, assigned actor, performed/reviewed time and failed/pending state                                | Bed release leaves cleaning pending; wrong-room or stale-task completion cannot make a different bed available                                           |
| IN-FQ-04 Biomedical waste                                | Facility/collection location, classified stream, measured unit/quantity, container/barcode lineage where required, authorized handoff/vendor, timestamps, discrepancy and evidence | Capture actual segregation/handoff; missing weight is not zero; wrong vendor/expired authority is actionable; generation alone is not treatment/disposal |
| IN-FQ-05 Quality incidents                               | Restricted incident reference, source facts, immediate containment, accountable review, corrective action, evidence and verified closure                                           | A complaint is not automatically a clinical negligence conclusion; reporter identity and clinical details are not unrestricted dashboards                |
| IN-FQ-06 Competence and privilege                        | Staff identity, reviewed credential, current service privilege, training evidence and expiry/reminder state                                                                        | HR employment and a completed reminder do not renew a licence or authorize a clinical action                                                             |
| IN-FQ-07 Emergency readiness                             | Current site plans, drills, equipment checks, acknowledged escalation and approved evacuation coordination                                                                         | Stale bed/patient-location projections are visibly stale; no automated evacuation clinical decisions from a model                                        |
| IN-FQ-08 Patient rights and grievances                   | Attributed request, verified requester/representative, permitted records, response/denial rationale, progress and escalation                                                       | Supply the authorized record version; preserve other patients' privacy and legal holds; a relationship claim alone does not grant access                 |

Biomedical waste rules and SPCB/PCC procedures have their own applicability and reporting requirements. Record the selected authority, accepted classification and authorized treatment/handoff chain; do not create a universal colour/retention/reporting schedule from memory. A laboratory disposal event or generated manifest cannot prove the physical process occurred.

Quality dashboards need reviewed metric definitions: numerator/denominator, reporting site and period, source version, exclusions, missingness and freshness. Do not equate incident count with care quality or a dashboard screenshot with accreditation evidence. Audit trails must distinguish user assertion, imported document, verified review and actual external acknowledgement.

## Downtime and continuity

**IN-FQ-09:** Plan bounded offline/manual operations and reconciliation with hospital operations: temporary patient/encounter identifiers, printed labels, paper orders/MAR, failed communication, duplicate prevention and reconciliation ownership. A fallback must preserve clinical attribution and review; it is not permission to bypass tenant access or silently merge records when connectivity returns.

**IN-FQ-10:** Backups, restore, retained evidence and offboarding must respect hospital scope, applicable record class, legal holds and approved retention policy. Do not use one pharmacy register period as the retention period for all clinical, diagnostic, payroll or incident records. Restore tests must verify scope and pending work, not merely database startup.

Review with hospital administration, clinical/nursing governance, quality/infection-control, biomedical engineering, privacy/security and state/service compliance owners. Test expired/partial-scope certificates, altered evidence, stale task completion, equipment outage during scheduling, waste handoff discrepancy, legal hold versus deletion request, unauthorized grievance access, clock correction, downtime duplication and recovery of pending clinical work.

Accreditation mapping, fire/local permits, blood-centre and specialty service controls, prenatal diagnostic requirements, professional-record retention and regulatory reporting formats need dedicated current-source review for the selected hospital. These specs do not establish statutory filing capability, accreditation or a universal release checklist.

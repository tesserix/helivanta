# Clinical operations, admissions and care transitions

[India knowledge index](README.md) · Helivanta design requirements, not medical protocols

Read the [MediCore baseline](../../api/domains/patients-and-clinical.md) and [bed/admission draft](../../api/domains/beds-and-admissions.md) first. Current visit creation, default CBC intake and pending dispense tasks are foundation behavior, not a full clinical ordering or inpatient system. This document adds business acceptance criteria without inventing deployed endpoints.

## Distinct facts and authorities

| Requirement               | Domain facts that must stay separate                                                                                    | Owner and decisive evidence                                                                                                                                          |
| ------------------------- | ----------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| IN-CL-01 OPD/appointments | Appointment, arrival/check-in, queue token, triage, consultation, order and follow-up                                   | MediCore; each status needs its own actor/time and cancellation/no-show semantics. A queue token is not patient identity or care completion                          |
| IN-CL-02 Admission        | Planned admission, accepted encounter, clinical admission, bed reservation and occupancy                                | MediCore; current patient registration, admitting authority and placement decision. Payment or bed-selection alone is not admission                                  |
| IN-CL-03 Transfers        | Requested move, reserved target, clinical handover and effective occupancy transition                                   | Bed owner serializes allocation; preserve source/target interval and clinical receiving acknowledgement. A cross-hospital referral needs separate sharing/acceptance |
| IN-CL-04 Nursing          | Assignment, assessment, observations, care task, handover and acknowledgement                                           | Attributed current ward/encounter assignment, due/recorded/occurred times and reviewed escalation; a generated task is not performed care                            |
| IN-CL-05 Medication chain | Prescribe/sign, pharmacist verify, dispense, administer/omit/return and charge                                          | Separate clinical/pharmacy/nursing/financial owners. Medication administration records actual action and reason; stock fulfillment does not prove administration     |
| IN-CL-06 Diagnostic chain | Request, sample/study, validation/release, communication and clinical review                                            | Lab/radiology owns released report version; clinician owns interpretation/action. Delivery is not acknowledgement                                                    |
| IN-CL-07 Discharge        | Clinical readiness, instructions/summary, departmental clearances, physical departure, bed cleaning and financial close | Authorized clinical sign-off and recorded disposition. Late financial posting must not falsify departure or availability                                             |

For every operation, carry patient registration/encounter references, owning hospital, source/version, authorized actor and evidence time. Record when something occurred separately from when it was entered. Late entries/corrections must not silently become contemporaneous notes or overwrite signed evidence.

## Clinical safety invariants

- **IN-CL-08:** An unknown allergy is different from reviewed no-known-allergy, and a patient-reported blood group is different from a verified transfusion result. Preserve source, review and status rather than converting missing information into a reassuring default.
- **IN-CL-09:** A bed's operational state and suitability are not a room label. Reserve/occupy atomically, reject stale versions, and keep cleaning, isolation and maintenance clearances explicit. Capacity views show freshness; allocation rechecks the owner. Reuse the detailed concurrency/recovery rules in the bed draft.
- **IN-CL-10:** Clinician assignment, professional registration, current facility privileges and ability to sign a particular document are distinct. Staff enrollment or an HPR reference is not unrestricted prescribing/sign-off authority.
- **IN-CL-11:** Escalation and human review are explicit states with accountable recipients. A generated warning, SMS or AI summary cannot count as completed clinical action. Do not implement diagnosis, dosage, triage scores or automatic escalation thresholds from model memory; use medical-director-approved protocols and versioned test cases.
- **IN-CL-12:** Cancellation before performance differs from amendment after performance. A completed procedure, administration or physical transfer requires attributable correction/reconciliation, not deletion or a fictional rollback.

OT/day-care workflows need reviewed procedure/side/site, team/resources, consent, readiness, cancellations and attributable sign-off. Maternal/newborn, ICU, blood-bank/transfusion, dialysis and oncology workflows require separate specialty requirements and clinical review; a generic free-text visit form is insufficient. This documentation supplies no treatment thresholds or clinical calculator.

## Indian hospital operating context

Support assisted and local-language registration, provisional identities, attendants, nursing shifts crossing midnight, printed downtime identifiers and back-entry after an outage. Date-only business facts use the hospital's configured timezone; retain UTC instants for events and exact policy effective times. Do not assume every tenant's sites share one timezone or numbering scheme merely because the deployment is India-first.

Record admission source, elective/emergency intent and locally approved disposition vocabulary. LAMA/DAMA, referral, transfer, absconded/departed and death need reviewed meanings and permissions, not automatic equivalence or closure on a billing action. MLC designation and police/statutory reporting require local applicability review and restricted evidence access, rather than general CRM labels.

The [patient-rights charter and clinical-establishment sources](sources.md#care-facilities-and-quality) inform access/information and service-applicability review. National law, state adoption and hospital policy differ. The [existing emergency research](../../research/2026-10-18-india-emergency-transport.md) owns transport safety and public call-first behavior. No national-ID, insurer API or CRM outage may masquerade as a completed clinical workflow.

## Acceptance and accountable review

Registration, medical director, nursing, admissions, housekeeping, finance and patient-safety owners review the appropriate slice. At minimum exercise concurrent bed assignment; bed release without cleaning; unknown/allergy-status display; incorrect patient/encounter reference; twins/provisional identity; stale privileges; duplicate/reordered orders; medication given versus not-given; late observation; failed result acknowledgement; transfer/discharge while a dependent service is down; and loss/reconciliation of downtime records.

Measure operational intervals with explicit start/end events and exclusions: arrival-to-triage, order-to-collection, ready-to-departure and release-to-ready-bed are different metrics. Missing observations or timestamps must remain missing, not become zero waiting time or normal clinical status.

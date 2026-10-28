---
name: hms-india-clinical
description: Design or review Helivanta patient identity, OPD/IPD, admissions, nursing, clinical orders, diagnostic reporting and discharge workflows for Indian hospitals. Use for care-data and patient-safety behavior, not to generate treatment, dosage or triage advice.
---

# Indian hospital clinical workflows

Read the affected references:

- [Identity, consent and ABDM](../../../docs/domain/india/identity-consent-and-abdm.md)
  for registration, linking, proxies, disclosure and exchange.
- [Clinical operations](../../../docs/domain/india/clinical-operations.md) and
  [bed/admission contracts](../../../docs/api/domains/beds-and-admissions.md)
  for care transitions, nursing, occupancy and discharge.
- [Diagnostics](../../../docs/domain/india/diagnostics.md) for specimens,
  results, critical communication, sign-off and amendment.

Start from the actual [MediCore/patient](../../../docs/api/domains/patients-and-clinical.md)
or [LabConnect](../../../docs/api/domains/laboratory.md) implementation. A visit,
default pending task or result string is not the complete clinical workflow.

For the operation, trace patient registration and encounter identity, owning
hospital, authorized current role/assignment/privilege, source record/version and
what evidence makes the transition true. Shared phones, names, national IDs,
emergency contacts and linked relatives are not authorization or merge rules.
Unknown/unverified facts must remain distinct from reviewed negative findings.

Preserve prescription → dispense → administration and order → specimen/study →
release → acknowledgement as distinct facts with accountable owners. Clinical
discharge, physical departure, financial close and cleaned-bed availability also
remain separate. Use existing contract sources and approved state transitions;
do not add a second patient master or let a consuming service alter signed facts.

Select meaningful cases from the applicable `IN-ID`, `IN-CL` or `IN-DX`
requirements: provisional identity, twins/shared contact, wrong hospital, revoked
proxy/privilege, concurrent allocation, duplicate/reordered command, unknown
allergy, failed critical acknowledgement, late entry, amendment and downtime
reconciliation. Verify actual output/state, not just a successful request status.

Clinical thresholds, drug doses, triage, specialty pathways, consent exceptions
and legal applicability require their named qualified reviewers and current source
evidence. Do not fill those gaps from model memory. FHIR structure/profile checks
do not certify a clinical decision, patient match or production exchange.

# LabConnect integration specification

[Contract index](../README.md) · current minimal API plus target diagnostic lifecycle

## Implemented boundary

LabConnect currently owns `lab_orders`: visit reference, patient-name snapshot, default test name `CBC`, pending/completed status, nullable result value/time and creation time. It consumes MediCore `visit_created.v1`, creating a pending order in the consumer transaction. This automatic default is a development integration slice, not an approved clinical CPOE order.

| Current operation                 | Behavior                                                                                                                                                                               |
| --------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /v1/lab/orders`              | Tenant-scoped standard cursor page under `lab.order.read`                                                                                                                              |
| `POST /v1/lab/orders/{id}/result` | `{"result_value":"..."}`; required nonempty string, maximum 500; pending → completed via guarded update; 200 `{id,status:"completed"}`; 409 after completion; 404 if invisible/missing |
| `GET /v1/lab/insights`            | Bounded calendar counts of pending/completed orders; not specimen TAT or clinical release metrics                                                                                      |

See [current OpenAPI](../../../contracts/api/clinical.openapi.json), `backend/internal/modules/lab/{orders,consumers,module}.go` and `contract/events.go`. Result completion writes `result_ready.v1` with `order_id` and `visit_id` in the same local transaction. No current registered consumer attaches that result to a clinical chart or patient portal. A result string is not a signed structured report or a FHIR `DiagnosticReport`.

## Target contracts

Retain LabConnect ownership of diagnostic order acceptance, specimens, accession, collection/rejection/recollection, worklist, analyser output, QC, authorized validation, release, amendment and dispatch. A released result is a versioned clinical fact; a correction does not overwrite the original report. MediCore receives an authorized released-result projection with source version, release time and acknowledgement needs.

Before freezing a full contract, resolve:

- Order source and identity: patient registration, encounter, requesting clinician, ordered test/catalogue version, urgency, performing/outsourced lab and billing reference.
- Specimen lineage: accession/barcode, specimen type/site, collection actor/time, receipt, rejection reason, recollection linkage, aliquots and chain of custody.
- Measurement semantics: numeric/string/coded value, unit, method, instrument, reference interval/version, abnormal/critical interpretation, uncertainty where applicable and population context.
- Authority: operator versus verifier/pathologist, sign-off credentials, current patient/hospital scope, release and amendment permissions.
- Delivery: report version/checksum, protected document reference, authorized recipients, failed delivery, withdrawal/amendment notice and critical-result acknowledgement with escalation.
- Deduplication: stable external message and source order identities, duplicate instrument submissions, optimistic versioning and quarantine for unmatched patient/test/device mappings.

Test failures explicitly: wrong specimen or patient linkage, duplicated analyser message, concurrent sign-off, out-of-range unit/reference mapping, stale result amendment, delayed critical acknowledgement, revoked portal access and outsourced lab unavailable. If an analyser is down, the order remains pending with an actionable exception; missing telemetry cannot become a normal result. Billing failure must not mutate the signed report.

## Backlog coverage

[#985](https://github.com/tesserix/helivanta/issues/985) covers order-to-dispatch/specimen traceability; [#986](https://github.com/tesserix/helivanta/issues/986) critical/delayed/outsourced work. Masters and reporting: [#1067](https://github.com/tesserix/helivanta/issues/1067), [#1068](https://github.com/tesserix/helivanta/issues/1068), [#1069](https://github.com/tesserix/helivanta/issues/1069), [#1070](https://github.com/tesserix/helivanta/issues/1070), [#1071](https://github.com/tesserix/helivanta/issues/1071), [#1072](https://github.com/tesserix/helivanta/issues/1072), [#1052](https://github.com/tesserix/helivanta/issues/1052) and [#1065](https://github.com/tesserix/helivanta/issues/1065). PACS/radiology is a separate diagnostic boundary, not a lab-result string extension. FHIR/HL7 mappings remain adapter/profile work under #472 and integration stories.

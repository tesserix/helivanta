# Laboratory and diagnostic imaging

[India knowledge index](README.md) · domain review draft; [current LabConnect API](../../api/domains/laboratory.md)

The current backend records a small pending/completed order with a result string. Do not describe that foundation as specimen traceability, authorized report release, critical-result communication, analyser interoperability or laboratory accreditation.

## Laboratory contract

| Requirement                     | Required facts and ownership                                                                                                                      | Failure/acceptance scenario                                                                                                      |
| ------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------- |
| IN-DX-01 Order identity         | Authorized order/version, patient registration/encounter, requesting clinician, performing site, test/catalogue version and source-line ID        | Reject a valid-looking ID belonging to another hospital; never create a clinical CBC order just because a visit event arrived    |
| IN-DX-02 Specimen lineage       | Accession, patient association, specimen/site, collection/receipt actors and times, labels, aliquot/parent relationships and transport conditions | A barcode identifies a specimen, not an arbitrary patient; multiple specimens and rejected/recollected specimens retain lineage  |
| IN-DX-03 Work and QC            | Requested tests, instrument/method, operator, QC/calibration evidence and actionable hold/reject/recollect state                                  | Instrument or QC failure cannot yield an apparently normal result; incorrect instrument-to-test mapping enters quarantine/review |
| IN-DX-04 Result semantics       | Numeric/text/coded result as appropriate, unit, method, reviewed reference interval/context and flags, effective version and provenance           | No guessed unit conversion or universal age/sex range; absent/unperformed result is not zero or within-range                     |
| IN-DX-05 Validation/release     | Separate operator output, authorized review/sign-off, release state, document version and recipient scope                                         | An uploaded PDF or auto-generated interpretation is not a released result; an unauthorized operator cannot finalize it           |
| IN-DX-06 Critical communication | Versioned clinical critical-value policy, intended accountable recipient, contact attempts, acknowledgement and escalation outcome                | Delivery/SMS success is not acknowledgement; expired recipient assignment and unacknowledged results remain actionable           |
| IN-DX-07 Amendment              | New signed/amended version linked to the superseded one, reason and prior authorized recipients requiring notice                                  | Old downloads/projections are visibly superseded; duplicates/reordered events cannot restore an obsolete report                  |
| IN-DX-08 Outsourcing            | Partner authority, source/partner order and specimen mapping, transport/receipt state, expected service and reconciled report/billing references  | Unmatched callback, unavailable partner and duplicate external accession require explicit reconciliation                         |

Reference intervals, microbiology susceptibility interpretation, critical thresholds, delta checks and specialist report content require qualified laboratory/clinical approval with source/method/version. Do not synthesize medical ranges, diagnostics or thresholds from model memory. Age/context at the observation time matters; a current demographic value must not rewrite historic interpretation.

NABL medical accreditation and NABH laboratory certification are distinct programmes; neither is a general product checkbox. Store current performing-site certificate scope, test/method applicability, validity and reviewer evidence. The [official ISO 15189:2022 portal announcement](sources.md#care-facilities-and-quality) establishes a programme context, not that Helivanta or a particular test is accredited.

## Radiology and PACS

**IN-DX-09:** Preserve requesting order, patient/encounter, accession, study/series identifiers, modality, performing facility, protocol/consent where applicable, authorized report/version and protected image-access reference. DICOM study identity is not patient authorization. A report can be amended without reassigning the original images to a different patient.

**IN-DX-10:** Facility/equipment licence and operator privileges are independent of a viewer URL. AERB covers relevant ionising-radiation facilities/equipment; do not generalize an X-ray requirement to every diagnostic modality. State/service-specific controls, including applicable prenatal diagnostic requirements, need a separately verified policy and clinical/legal review. [AERB source and access limit](sources.md#care-facilities-and-quality).

**IN-DX-11:** Analyser HL7/device, PACS/DICOM and ABDM FHIR exchange are different adapters. Pin source/device/site mappings and agreed profiles, authenticate/restrict ingestion, deduplicate source message IDs and quarantine ambiguous matches. Do not directly write the lab owner's tables from a device gateway. Published source report versions and authorized owner reads remain the clinical authority.

Review with laboratory director, pathologist/radiologist, nursing/specimen operations, clinical governance, privacy/security and actual consumers. Test specimen swaps, recollection, failed QC, changed units, wrong-facility callbacks, concurrent sign-off, lost critical acknowledgement, amended/withdrawn reports, partner outages and revoked access to viewer links or downloaded reports. See [identity/exchange](identity-consent-and-abdm.md) for national profile verification.

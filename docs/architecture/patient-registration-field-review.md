# Patient registration field review

Reviewed 2026-10-14 for #980, #1058 and #990. This is a field-level design and gap register, not a claim that all fields or all legacy transactions are implemented.

## Evidence and standards

The earlier read-only blank New Member form and Patient Registration Setup captures were re-inspected. The setup explicitly lists blood group under Patient Other Details. The live root application currently requires a fresh login; current form-level verification of those configurable sections is pending. No customer record, screenshot or identifier is included here.

Primary sources retrieved on 2026-10-14:

- [HL7 FHIR R4 Patient](https://hl7.org/fhir/R4/patient.html): demographics, identifiers, contacts, communication and administrative gender. FHIR is an interoperability reference, not a requirement to copy its wire model into every database table.
- [HL7 FHIR R4 datatypes](https://hl7.org/fhir/R4/datatypes.html): HumanName, Address, ContactPoint and partial dates. Do not restrict names to Western alphabetic patterns or assume every person has a family name.
- [HL7 FHIR R4 Observation](https://hl7.org/fhir/R4/observation.html): clinical measurements and laboratory results belong to an observation/report with provenance. A receptionist-entered blood group is a reported value, never a verified transfusion result.
- [HL7 FHIR R4 blood-group example](https://hl7.org/fhir/R4/observation-example-bloodgroup.json.html): ABO laboratory measurement is illustrated as an Observation (LOINC 883-9). This informative example does not make our registration input a laboratory result.
- [WCAG 2.2 Error Identification](https://www.w3.org/WAI/WCAG22/Understanding/error-identification.html): identify the invalid field and explain the error in text; preserve values when validation fails.
- [WCAG 2.2 Focus Order](https://www.w3.org/WAI/WCAG22/Understanding/focus-order.html): navigation order must preserve meaning and operation. Opening registration brings its first field into view; cancelling returns focus to the trigger.

## Field coverage and ownership

| Legacy evidence / requested field                                                | Helivanta handling                                                                                                        | Verification boundary                                                                                                                   |
| -------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------- |
| Prefix, name, suffix; requested richer names                                     | Given/family, optional prefix/middle/suffix/preferred name; preserve Unicode and single-name patients                     | Existing given/family; additional fields need implementation                                                                            |
| Gender                                                                           | Keep administrative/recorded sex distinct from gender identity and clinical sex-related observations                      | Existing sex field is not a complete clinical sex/gender model                                                                          |
| DOB or age in years/months                                                       | Exact or explicitly estimated DOB; add partial-date/age-at-registration semantics without fabricating precision           | Current exact-date requirement is a known gap; estimated checkbox alone is insufficient                                                 |
| Primary/secondary/work phone, country code, WhatsApp, email                      | International contacts with explicit purpose; never infer messaging permission from possession of a phone number          | Primary phone exists; remaining contacts need implementation                                                                            |
| Address lines 1/2, country/state/district/city/taluka/village/region/postal code | Structured international address; locality/subdistrict fields remain optional                                             | Current single address line is insufficient                                                                                             |
| Blood group                                                                      | Optional ABO/Rh reported value with patient/guardian/document source; unknown/not recorded remain explicit                | No lab-verified flag or clinical decision derived from registration input                                                               |
| Emergency contact, second emergency contact, next of kin                         | Repeatable purpose-labelled name/relationship/phone contacts                                                              | Contact role is not legal consent authority                                                                                             |
| Responsible person                                                               | Explicit contact purpose; financial liability or consent authority belongs to a separately governed workflow              | Never grant chart access from a contact relationship                                                                                    |
| Nationality, country of residence, language, marital status, diet                | Optional purpose-specific details; language preference supports communication                                             | Diet is a reported preference, not a clinical diet order                                                                                |
| ABHA, external IDs, registration proof                                           | Existing patient identifier ownership; typed value must not imply verified identity                                       | Verification flow, masking/encryption and duplicate linkage need their owning stories; do not add raw government-ID text boxes casually |
| Photo / camera / proof upload                                                    | Tenant/hospital-scoped GCS, quarantine and ClamAV before attachment access                                                | Upload delivery remains a separate protected workflow; no fake upload control                                                           |
| Ration card, caste/tribe, race/ethnicity, religion, income, qualification        | Disabled by default; configurable only with a documented purpose, jurisdiction profile and field-level access             | Not universal mandatory registration fields; no collection merely because legacy has them                                               |
| Urban/non-urban supplemental address                                             | Optional local address attributes, not a globally required India-only hierarchy                                           | Configuration and reference masters belong to #1055/#1058                                                                               |
| Passport, SSN/tax/election identifiers                                           | Purpose-limited protected identifiers with masking and restricted access                                                  | Registration profile must not become an unrestricted identity-document store                                                            |
| Patient type, classification, member flag, source/tag                            | Governed reference codes and effective policy                                                                             | Do not use free text to duplicate billing or membership rules                                                                           |
| Consent notice / guardian receipt                                                | Published human-readable notice selection with immutable version reference; guardian authority distinct from relationship | Current free-text notice version is an interim UX/policy gap                                                                            |

## Delivery design

Extend the existing patientmaster, not a new service. Keep the registry result minimal; additional contact/demographic details are fetched only after patient/hospital authorization and audited read. Registration writes patient, details, related contacts, consent, idempotency receipt and outbox in one transaction. Persisted request fingerprints cover the extended input. A retry must not add contacts or events.

Use typed PostgreSQL columns for fixed details and a child table for repeated related contacts. Composite tenant/patient foreign keys prevent cross-tenant links. Force RLS through the same visible parent and current clinical grant. No patient data or authorization grants enter Valkey; NATS continues to receive IDs only. Temporal is unnecessary for this atomic operation, but fits later scan/verification workflows with external waits.

Threat model: patient demographics and third-party contact details are protected assets. Threats include another tenant's user, an unauthorized same-tenant employee and malicious input from an authenticated registrar. Trust boundaries are session-to-tenant, action permission, selected hospital grant, and database RLS. Every detail read/write must satisfy all boundaries. Errors and events contain no new demographics.

Planning assumptions (not measured capacity claims): 20 requests/s per API replica, at most 8 KiB registration input, approximately 10 reads per write; 1 million patients at 12 months and 3 million at 36 months, about 2/6 GB of 2 KiB profile payloads before indexes/audits/backups. Target existing service availability of 99.9% and p99 API latency below 300 ms under the chosen load; validate before scaling claims.

Database or authorization dependency failure denies the operation. Valkey outage degrades rate-limit accuracy only. Broker outage leaves a committed outbox event for retry. New fields are additive and optional for older clients. Apply forward-only migrations first; preserve data when rolling application code back, and never roll back past the hospital-scope security boundary. Incremental storage grows with optional details; no additional runtime service is needed. An untyped JSON catch-all was rejected because fixed sensitive fields need explicit validation and constraints.

## Acceptance gates

- Red/green tests for persisted reported blood group and provenance, optional details and repeated contact uniqueness.
- Invalid enums, lengths, phones, emails, incomplete contacts and attempted verified-result injection are rejected.
- Same request/body replays; changed body conflicts; concurrent retries create one patient/contact set/event.
- Reader vs registrar, missing hospital, foreign tenant and sibling-hospital denial; direct child-table RLS checks.
- List responses do not expose extended contacts. Detail responses show the persisted profile only after authorization.
- Keyboard/focus, desktop/mobile, clear optional groups, pending/error/success states and retained form values.
- No synthetic fixture or documentation contains transformed legacy customer data.
- Do not close #980/#1058 until all their product handoff and configuration acceptance criteria are actually covered.

## First implementation slice: reported blood group

Migration `0004_patientmaster` adds `patient_reported_blood_groups` with a composite tenant/patient primary and foreign key. ABO (`A`, `B`, `AB`, `O`, `unknown`), Rh(D) (`positive`, `negative`, `unknown`) and source (`patient`, `guardian`, `document`) are constrained in the API and database. An omitted object means not recorded; an explicit unknown value means the supplied report did not establish that component. Neither is inferred as a negative result.

Registration records the authenticated subject and database timestamp, in the existing patient/consent/outbox/idempotency transaction. The server rejects client-supplied verification and timestamp attributes. A trigger blocks updates, deletes and truncation: corrections need a future audited supersession workflow. Forced RLS inherits parent visibility and requires the current hospital registrar grant for inserts; an ordinary tenant-admin role is insufficient. Search and list responses omit blood-group data. An authorized, audited detail read returns its source and recording time.

The UI starts with this optional section disabled. Enabling it requires an explicit source; ABO and Rh(D) may be unknown. Disabling it removes the report from submission. Patient details explicitly label it unverified. This slice adds no laboratory interpretation, uploads, contact sharing or FHIR conformance claim.

The migration is forward-only. Application rollback preserves this table and its data; no destructive down migration is provided. Existing requests without the new optional object retain the same fingerprint encoding. Do not deploy an application rollback below the existing hospital-isolation release. Production deployment and post-deployment verification of this slice remain pending.

Research limitation: the HHS minimum-necessary guidance URL returned HTTP 403 during retrieval; it was not treated as freshly verified evidence. These technical controls do not establish SOC 2 certification or HIPAA compliance. Compliance review, jurisdiction-specific consent policy and operational evidence remain separate requirements.

## Verification of the first slice

Validated with synthetic data on 2026-10-14:

- API red test rejected the previously unsupported object with HTTP 400; it passed after schema/handler changes.
- `go test -race ./...` passed; the affected patientmaster suite was rerun after final trigger changes. `go vet ./...`, `go build ./...` and `golangci-lint run` passed.
- With the repo-required Node 24 runtime, `pnpm turbo test lint type-check format:check --filter=@helivanta/shell --filter=@helivanta/api` and `pnpm --filter @helivanta/shell build` passed.
- Local browser: source validation retained inputs and focused the invalid field; save and detail read returned the same reported value; the 390-pixel layout had no page overflow.
- Local API connected through a port-forward to the isolated CNPG verification database: eight retries returned one patient; changed report input returned 409; a foreign hospital returned 404; searches omitted the report. Database inspection confirmed one report, one request receipt and one event without report contents.
- The first direct tunnel reset mid-test (six 201 responses, two 500 responses); the retry used the same request key over a temporary loopback relay. The successful recovery did not duplicate data. This transport failure is retained as evidence, not counted as a passing attempt.
- `EXPLAIN (ANALYZE, BUFFERS)` used the report's composite primary-key index, with parent RLS checks. Execution was 0.378 ms on the small synthetic dataset; this is not a production-scale benchmark.

The separately merged focus fix (#1096, commit `973dfd0`) was checked on the production site: opening registration focused Given name at 223 px in an 835 px viewport, and cancellation returned focus to Register patient. This does not imply the blood-group slice is deployed.

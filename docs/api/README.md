# API contracts and integration specifications

**Baseline:** `2295017b47ed0b5774aa4ecbb2bcdbfca9c731da`, reviewed 2026-10-22.
**Status:** implementation reference plus proposals for review. No new API is deployed by this documentation.

Helivanta is a healthcare SaaS platform with a shared staff workspace, hospital-scoped patient identity, clinical operations, independent pharmacies, supply organizations, workforce and emergency operations. MediConnect is the patient-facing product; MediCore is the hospital clinical product. Commercial CRM, patient engagement and emergency case coordination have different owners and data boundaries.

Start with [platform coverage and ownership](portfolio.md), then [HTTP conventions](http-contract.md), [events and integration journeys](integration-contracts.md) and [schema ownership](schemas.md). The [review and delivery plan](review-plan.md) records gaps, priorities and acceptance gates.

For Indian hospital business rules and source applicability, read the
[India HMS domain knowledge](../domain/india/README.md): identity/consent, clinical
operations, pharmacy/supply, diagnostics, revenue/insurance and facility quality.
Its reusable agent skills route to the relevant `IN-*` acceptance criteria and
primary-source register; they do not turn proposals into deployed APIs.

All UI/app business APIs must enter the [Global API](global-connector.md). Product
APIs retain data ownership and authorization. New services must satisfy its
onboarding and end-to-end acceptance rules before exposure.

## Reference material

| Artifact                                                                 | Purpose                                                                                           | Authority                                                                               |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------- |
| [Generated runtime inventory](reference/README.md)                       | Every registered backend route, permission, event subscription and module-owned table declaration | Generated from the actual module registry; excludes UI-only routes                      |
| [Machine-readable inventory](reference/inventory.json)                   | Tooling input with routes, grants, migrations, tables and directed-event metadata                 | Same registry; a route entry is not a complete request/response specification           |
| [Clinical OpenAPI](../../contracts/api/clinical.openapi.json)            | Typed current visit, lab and legacy dispense HTTP interfaces                                      | Reviewed description of existing handlers; limited coverage is intentional and explicit |
| [Current event envelope](../../contracts/api/event-envelope.schema.json) | JSON Schema for the existing event wire shape                                                     | Mirrors `backend/pkg/events/types.go`; not evidence of runtime schema enforcement       |
| [Generated event payload schemas](reference/event-payloads.json)         | Registered published subjects with exact Go wire fields and source locations                          | Structural serialization schemas; semantic validation remains with producers/consumers  |
| [Contract source index](../../contracts/README.md)                       | Existing Go/TypeScript contract generators and canonical model sources                            | Keep editing the existing source, not a copied model                                    |
| [Architecture](../architecture/README.md)                                | Proposed independent product APIs, deployment and sizing                                          | Target architecture; not current runtime evidence                                       |

MediConnect owns the scoped [account and link-request API](mediconnect.md).
Patientmaster owns implemented identity review, self/guardian authority and the
canonical clinical-sharing ledger; see [verified release evidence](../delivery/mediconnect.md).
Owner-authorized clinical record release, adult/family projections and booking/reminder
integrations remain unfinished. A consent gate does not authorize clinical disclosure.

The [privacy casework API](privacy.md) owns authenticated tenant-local rights/grievance review; a case response does not execute disclosure or erasure.

## Domain specifications

| Domain                                                 | Specification                                                                                                          |
| ------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------- |
| Patients and clinical work                             | [Patient identity and MediCore](domains/patients-and-clinical.md)                                                      |
| Beds, admissions, transfers and discharge              | [Bed and inpatient specification](domains/beds-and-admissions.md)                                                      |
| Pharmacy, stock and financial records                  | [PharmaConnect](domains/pharmacy.md)                                                                                   |
| Supply organizations and procurement                   | [SupplyConnect](domains/supply.md)                                                                                     |
| Diagnostics                                            | [LabConnect](domains/laboratory.md)                                                                                    |
| Staff and HR                                           | [Workforce](workforce.md) and [integration specification](domains/workforce.md)                                        |
| Citizen calling, dispatch, crew and hospital receiving | [EmergencyConnect](domains/emergency.md)                                                                               |
| Commercial CRM, patient engagement and referrals       | [CRM and MediConnect](domains/crm-and-mediconnect.md)                                                                  |
| Identity, tenancy, files and country policy            | [Platform](domains/platform.md), [workspace API](workspace.md)                                                         |
| Remaining portfolio                                    | [Coverage matrix](portfolio.md): DoctorConnect, CareConnect, AdminConnect, FacilityConnect, SafetyConnect and adapters |

## Maintaining the reference

From `backend/`, run:

```sh
go run ./cmd/contractdocs
go run ./cmd/contractdocs -check
go test ./cmd/contractdocs
```

Generation registers handlers without dispatching requests or connecting to infrastructure. It writes the generated inventory, event payload schemas and Markdown reference. Conditional workflow consumers are included with registration gates enabled; that does not assert production enablement. It does not execute migrations, change data, contact CRM systems or create GitHub issues.

For a contract change, update its owning source, concrete examples, failure/retry rules and consumer references together. Run the existing generator's `--check` where relevant. A green inventory check proves registry parity only; it does not prove payload conformance, compatibility or production behavior. Follow the [review gates](review-plan.md) before consumers treat a proposal as approved.

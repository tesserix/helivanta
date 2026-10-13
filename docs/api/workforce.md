# Workforce API: first deployable slice

Implementation: `backend/internal/modules/workforce`. Design: [ADR 0009](../adr/0009-workforce-foundation.md). This is the hospital-local staff directory foundation, not a complete HR, payroll, practitioner credentialing or rostering system.

## Authentication and scope

Use an existing Helivanta session obtained through the Zitadel login flow. Every route checks the session and current OpenFGA tenant membership. The session's tenant is authoritative; request bodies cannot set tenant, actor or role. Unknown JSON properties are rejected. No route creates a tenant or a Zitadel identity.

The public gateway prefix is `https://helivanta.app/api/v1`; Istio rewrites it to the service's `/v1` prefix used below.

Tenant creation remains with the existing onboarding service's `POST /v1/orgs` contract (name, owner email, requested registered products). Its returned tenant ID must be used by the Helivanta tenant/membership projection. The operator created YES HOSPITAL. Its active synthetic owner is now `yes-hospital-test-owner@example.invalid` (Zitadel v2 initialization verified); the original INITIAL identity is retained without organization membership. The existing bootstrap command granted the active owner HMS tenant_admin. Automatic product registration and membership projection remain pending. A product grant in Zitadel is not automatically a Helivanta hospital grant. See [deployment evidence](../deployment/workforce-release.md) for the verified identifiers. Never manually invent a live tenant ID or bypass the operator with database inserts.

| Method and path (under `/v1`)                                 | Authority                            | Result                                                          |
| ------------------------------------------------------------- | ------------------------------------ | --------------------------------------------------------------- |
| `PUT /workforce/hospitals/{hospital}`                         | `workforce.hospital.manage`          | Create/update hospital metadata; 200                            |
| `PUT /workforce/hospitals/{hospital}/access/{subject}`        | `workforce.hospital.manage`          | Set explicit staff access to `reader` or `manager`; 200         |
| `DELETE /workforce/hospitals/{hospital}/access/{subject}`     | `workforce.hospital.manage`          | Revoke staff access; 204, also if already absent                |
| `PUT /workforce/hospitals/{hospital}/staff/{id}`              | Hospital `manager` grant             | Create/replace hospital-local staff record; 200                 |
| `GET /workforce/hospitals/{hospital}/staff/{id}`              | Hospital `reader` or `manager` grant | Staff record; 200, inaccessible/missing record 404              |
| `GET /workforce/hospitals/{hospital}/staff?limit=50&cursor=…` | Hospital `reader` or `manager` grant | Cursor page; inaccessible/missing hospital yields an empty page |

Hospital and staff IDs are non-nil UUIDs. Subject is the immutable identity subject, not an email address. Access grants do not create identity accounts or grant tenant membership. Tenant administrators can explicitly administer grants, but have no implicit staff-data access. Job category and employment status never confer permissions. Inactivating an employment record does not revoke a separate login or grant; access revocation is explicit in this slice.

POST is intentionally absent: clients choose UUIDs once and retry the same PUT URI after a timeout. Identical retries keep the original timestamps and do not add a second mutation audit. Changed PUTs replace the listed mutable fields; concurrent different updates currently use last-commit-wins semantics. Consumers requiring optimistic concurrency must wait for a versioned update contract. Codes are unique within a hospital and can repeat in isolated sibling hospitals.

## Payloads

Hospital:

```json
{ "name": "Synthetic Hospital" }
```

Explicit grant:

```json
{ "level": "manager" }
```

Staff:

```json
{
  "employee_code": "SYN-EMP-001",
  "display_name": "Synthetic Staff",
  "job_category": "nurse",
  "employment_status": "active"
}
```

Staff responses add `id`, `hospital_id`, `created_at` and `updated_at`. Timestamps are RFC 3339. Responses omit tenant credentials, login bindings and unrelated HR details. List envelopes use `data` and `page: {next_cursor, has_more}`. Default limit 50, maximum 200. Invalid cursors/limits and payloads return 400; duplicate employee codes return 409. Missing authentication returns 401, absent current tenant membership or hospital-administration permission returns 403, and unavailable authorization returns 503. Internal failures return a generic 500. Existing platform error envelopes are `{error, message}` with request correlation in the response headers.

Request bodies are capped at 8 KiB. Hospital name and staff display name allow 1–150 nonblank characters. Employee codes allow 1–40 ASCII letters, digits, underscores or hyphens. Local job categories are `doctor`, `nurse`, `pharmacist`, `lab_technician`, `receptionist`, `administration`, `other`; employment status is `active` or `inactive`.

## Legacy reference and international mapping

Read-only inspection on 2026-10-13 confirmed distinct HR MANAGEMENT, ADMIN and ACCESS CONTROL areas. HR navigation separates Employees and Consultants, shift allocation, leave requests/allocation, duty/overtime adjustments, missing punches, attendance, performance and payroll. The employee directory exposes name/code search and department, employee status, grade, category and user-type filters. Detailed employee creation and credential fields remain unverified. No staff rows or exports were copied.

| Observed legacy concept                            | Helivanta treatment                                                                                               |
| -------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| Employee name and code                             | `display_name` and hospital-local `employee_code` in this slice                                                   |
| Employee status                                    | Explicit `active` / `inactive`; legacy status vocabulary still requires mapping before import                     |
| Employees versus consultants                       | Future employment/engagement type, separate from clinical profession and permissions                              |
| Department, grade and category                     | Future hospital-scoped assignment and HR taxonomy; no inferred equivalence to this slice's `job_category`         |
| User type                                          | Identity/access concept requiring a verified mapping; never automatically grants authority from a personnel field |
| Shifts, leave, attendance, performance and payroll | Separate later workflows and protected HR data; not represented as generic staff attributes                       |

We use the legacy as workflow evidence, not a schema to reproduce. This foundation leaves room for international names (a display label does not require a Western given-name/family-name split), separates opaque IDs from local employee codes, uses explicit tenant/hospital scope and stores UTC-aware timestamps. The following are conceptual interoperability mappings, not implemented FHIR endpoints or a conformance claim:

| Helivanta concept                    | Standards reference / next modeling step                                                                                                                                  |
| ------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Customer tenant                      | SaaS security boundary; not automatically a FHIR clinical Organization                                                                                                    |
| Hospital                             | [FHIR Organization](https://hl7.org/fhir/R4/organization.html); physical departments/wards require separate [Location](https://hl7.org/fhir/R4/location.html) records     |
| Staff personal/professional identity | [FHIR Practitioner](https://hl7.org/fhir/R4/practitioner.html) for clinical practitioners; not every employee is a practitioner                                           |
| Hospital-local job assignment        | [FHIR PractitionerRole](https://hl7.org/fhir/R4/practitionerrole.html) for applicable clinical assignments; terminology system/code must be explicit when exchanging data |
| Employee code                        | Local business identifier scoped to its issuer/hospital; not a medical registration licence or global identifier                                                          |
| Login identity and hospital grant    | Zitadel subject and authorization facts, separate from Practitioner/PractitionerRole and from job category                                                                |

Verified follow-up field mapping should cover structured names, preferred name, work contacts, department/location, employment periods, multiple assignments, professional identifiers with issuer/country, qualifications and credential validity. Personal addresses, government identifiers and payroll require separately authorized HR records rather than this shared directory. Country, timezone, address and language conventions must be explicit when those fields are introduced. Sharing and identity linking need their own authorization and review; matching employee codes across hospitals does not establish either.

## Verification and release

Tests exercise the HTTP handlers with real PostgreSQL and NATS, plus a network HTTP test with signed Helivanta sessions and real OpenFGA membership revocation. The broader permission matrix checks the new administration permission against OpenFGA. These do not substitute for a live Zitadel login and post-deployment smoke test.

Migration `0001_workforce` creates four forced-RLS tables plus grant predicates and audit triggers. It does not change old clinical tables. Mutation audit is atomic and append-only for application roles; personal field values are excluded. Staff removal and table truncation are rejected; use inactive status. No patient or legacy data is migrated.

Production rollout uses the existing image pipeline and `helivanta-api` Argo CD application with the matching migration binary. See [release checklist](../deployment/workforce-release.md) for current observations and remaining gates.

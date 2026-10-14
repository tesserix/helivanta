# Workforce API: directory and protected enrollment

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

Read-only inspection on 2026-10-13 confirmed distinct HR MANAGEMENT, ADMIN and ACCESS CONTROL areas. HR navigation separates Employees and Consultants, shift allocation, leave requests/allocation, duty/overtime adjustments, missing punches, attendance, performance and payroll. The employee directory exposes name/code search and department, employee status, grade, category and user-type filters. Existing employee personal/contact/employment/qualification/experience/dependent/document tabs were subsequently reviewed on 2026-10-14; blank enrollment validation and consultant detail differences remain unverified. See ADR 0015 for the complete field mapping. No staff rows or exports were copied.

| Observed legacy concept                            | Helivanta treatment                                                                                                   |
| -------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- |
| Employee name and code                             | `display_name` and hospital-local `employee_code` in this slice                                                       |
| Employee status                                    | Explicit `active` / `inactive`; legacy status vocabulary still requires mapping before import                         |
| Employees versus consultants                       | Protected enrollment engagement type, separate from clinical profession and permissions                               |
| Department, grade and category                     | Enrollment department, designation and grade; future versioned HR taxonomy. No inferred equivalence to `job_category` |
| User type                                          | Identity/access concept requiring a verified mapping; never automatically grants authority from a personnel field     |
| Shifts, leave, attendance, performance and payroll | Separate later workflows and protected HR data; not represented as generic staff attributes                           |

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

## Protected enrollment

`GET` and `PUT /v1/workforce/hospitals/{hospital}/staff/{id}/enrollment` require `workforce.enrollment.manage` (tenant administrator initially) **and** a hospital manager grant. Ordinary directory managers/readers cannot access private profiles. Foreign-tenant/hospital objects return 404; a missing route permission returns 403. Every protected GET is audited. No personal profile is included in directory lists, outbox events, error bodies or persisted browser caches.

PUT carries `expected_version`, the existing `staff` directory fields, `profile`, and bounded arrays `addresses` (2), `emergency_contacts` (5), `credentials`, `qualifications`, `experiences` (20 each). Use version 0 for a new UUID; an edit uses the response `version`. Identical retries succeed without changing version/timestamps or appending an audit/event, even after the original write committed. Different stale content returns 409; reload explicitly rather than overwriting another editor. Staff codes are unique case-insensitively within their tenant and hospital. Child UUIDs must be stable across retries; duplicate IDs/natural keys are rejected.

The profile supports structured/preferred names, optional DOB/gender, separate work/personal email, E.164 mobile/work phone, communication language, optional **reported** blood group, engagement type, department/designation/grade, same-hospital supervisor and employment/probation/confirmation/end dates. Addresses use country/region/postal fields. Emergency contacts have name, relationship, international phone, optional email and distinct priorities 1–5. Credentials carry type/number/issuer/country/specialty and validity. Qualifications and prior employment are repeatable. No government ID, salary, scanned document URL or clinical verification fields are accepted.

All private tables have forced tenant/hospital RLS and composite staff/supervisor foreign keys. The new `helivanta.in.workforce.enrollment_changed.v1` event contains only hospital ID, staff ID and revision, and is inserted in the same transaction through the existing outbox/JetStream pipeline. Private data is not cached in Valkey. Temporal is reserved for a later multi-step review/upload/onboarding workflow; simple enrollment is one PostgreSQL transaction.

Field definitions are shared through `contracts/workforce-enrollment.json` and `scripts/generate-workforce-enrollment.mjs`, which generate backend field types and frontend schemas. Run `node scripts/generate-workforce-enrollment.mjs --check` to detect drift. Applied SQL migrations remain immutable; future database changes get a new migration.

See [ADR 0015](../adr/0015-staff-enrollment.md), [editable diagram](../diagrams/staff-enrollment.drawio), and `scripts/verify-staff-enrollment.mjs` for repeatable designated-synthetic-tenant verification. This enrollment release does not complete payroll, rosters, leave, dependent benefits, jurisdiction-specific IDs, document quarantine/release integration, legacy import or all of #1035/#979.

## Versioned HR setup

Design: [ADR 0016](../adr/0016-versioned-hr-masters.md). Diagram: [editable draw.io](../diagrams/versioned-hr-masters.drawio).

| Endpoint                                                         | Authorization                                                 | Behavior                                                                                                     |
| ---------------------------------------------------------------- | ------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| `GET /workforce/hospitals/{hospital}/hr/masters?kind=department` | Tenant membership and explicit hospital reader/manager grant  | Keyset-paginated current records; optional allowlisted category                                              |
| `PUT /workforce/hospitals/{hospital}/hr/masters/{id}`            | `workforce.master.manage` and explicit hospital manager grant | Stable client UUID; normalized unique code/name; optimistic version; immutable revision on meaningful change |
| `GET /workforce/hospitals/{hospital}/hr/masters/{id}/history`    | Tenant membership and explicit hospital reader/manager grant  | Keyset-paginated immutable revision history                                                                  |

PUT input: `expected_version` (0 for creation), `kind`, `code` (1–40 ASCII letters/digits/underscore/hyphen), `name` (1–80 characters), `description` (0–500), `active` (boolean). Code/category cannot change. Name whitespace is collapsed; code is uppercase. Duplicate normalized name/code returns 409, even when a record is retired. An identical retry preserves revision/timestamps and emits no new event. A different stale edit returns 409. Invalid fields return 400; inaccessible objects return 404. Tenant/hospital identity and unknown JSON fields are rejected. All responses explicitly use `Cache-Control: no-store`.

Categories: department, designation, grade, qualification, document_category, exit_reason, relationship, language, contract_type. This does not implement salary, tax, leave policy or KRA/KPI models. Retire/reactivate with `active`; no delete endpoint exists. List/history support the shared cursor and limit contract.

Enrollment profiles can select `department_revision_id`, `designation_revision_id`, `grade_revision_id`. Foreign-key scope includes tenant, hospital and category. The server derives the assignment name from the immutable revision; supplied label text cannot forge a snapshot. New assignments must select the active current revision. An existing assignment may retain its older/retired revision. Explicit empty reference clears it; omitted reference fields from older clients preserve an existing assignment. Free-text legacy assignments remain supported without silently matching names. A database guard refuses older API writers attempting to remove a pinned profile during rolling deployment.

`helivanta.in.workforce.master_changed.v1` contains hospital/master/revision UUIDs, category and revision number only. Audit, master head, immutable revision and outbox intent commit together. Neither employee attributes nor master labels are broadcast. The existing JetStream relay publishes; no Valkey caching or Temporal workflow is required for this transaction.

Synthetic verification: run `scripts/verify-hr-masters.mjs` with authenticated verification scope supplied privately via the same variables as `verify-staff-enrollment.mjs`. Its stable reserved IDs/codes are separate from manual browser fixtures. It restores its own synthetic baseline through versioned API operations on repeat runs and does not delete records.

## Versioned shift definitions

| Route                                                        | Access                                                      | Behavior                                                                                                                  |
| ------------------------------------------------------------ | ----------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------- |
| `GET /workforce/hospitals/{hospital}/hr/shifts`              | Authenticated tenant membership + explicit hospital grant   | Keyset pagination; optional allowlisted `shift_type` filter; private `no-store`                                           |
| `PUT /workforce/hospitals/{hospital}/hr/shifts/{id}`         | `workforce.roster.manage` + explicit hospital manager grant | Stable client UUID/code, normalized scoped uniqueness, expected version; unchanged retries preserve revision/audit/outbox |
| `GET /workforce/hospitals/{hospital}/hr/shifts/{id}/history` | Authenticated tenant membership + explicit hospital grant   | Immutable numbered snapshots, bounded keyset history                                                                      |

Input: `expected_version` (0 to create), `code`, `name`, `description`, `shift_type` (`day`, `evening`, `night`, `rotating`, `on_call`), `active`, `start_time`, `end_time` (strict `HH:MM`), explicit `break_minutes`, `grace_minutes`, `full_day_ot_minutes`, `half_day_ot_minutes`, and `locum`. All fields are allowlisted; body-supplied scope/unknown fields are rejected. Code cannot change. Response adds `id`, `revision_id`, `version`, timestamps and generated `duration_minutes` (local wall-clock overnight duration). History uses `shift_id` and immutable revision `id`.

Equal start/end is rejected rather than interpreted as a 24-hour shift. Break must be shorter than the shift; grace is 0–240; OT fields are 0–1440 with half ≤ full. These are configuration values, not payroll calculations. Dated rosters must pin a revision and apply an explicitly configured hospital timezone/DST policy; dated allocation is not delivered by this endpoint.

Tenant-admin holds the default management permission; an independent hospital manager grant is still required. Other fixed system roles receive no roster-management permission. A foreign/missing hospital or row is hidden with 404, missing route permission yields 403, invalid input 400 and stale/duplicate/changed-code conflicts 409. State/revision/audit/identifier-only `helivanta.in.workforce.shift_changed.v1` outbox commit atomically. No Valkey HR cache or unnecessary Temporal transaction introduced. See [ADR-0017](../adr/0017-versioned-shift-definitions.md) and [HR coverage](../hr-capability-coverage.md).

# Schema sources, ownership and data dictionary

[Contract index](README.md) · keep one authoritative definition per concern

## Source hierarchy today

| Concern                                           | Authority                                                                                   | What it does not prove                                                          |
| ------------------------------------------------- | ------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| Registered HTTP method/path/permission/pagination | Actual `bootstrap.Modules` + `platform.Router` declarations                                 | Request/response field conformance or runtime enablement                        |
| HTTP payload and business validation              | Owning Go handler/input/response projection, plus canonical JSON generator input where used | A database row or TypeScript interface alone is not the HTTP contract           |
| Storage columns/constraints/RLS                   | Registered SQL migrations in declared order, including embedded SQL                         | A `CREATE TABLE` list is not the final schema after later alterations           |
| Event subject/payload                             | Module `/contract/events.go`, producer and consumer validation                              | Generated structural JSON Schema does not impose semantic validation at runtime |
| Selected shared Go/Zod models                     | Existing `contracts/*.json` plus the matching generator                                     | These files use custom dialects, not standard JSON Schema/OpenAPI               |
| Reviewed current clinical HTTP description        | `contracts/api/clinical.openapi.json`                                                       | This is a limited descriptive pilot; current handlers are not generated from it |
| Planned product/schema                            | Explicit proposal in a domain specification                                                 | Not deployable or approved until owner/consumer/security review                 |

The proposed OpenAPI-first direction in #4 needs a migration decision. Preserve existing JSON-driven generation until a reviewed replacement can generate equivalent Go/Zod validation. A second handwritten model for the same operational API would create drift. The new clinical pilot covers handlers without an existing canonical generated HTTP model.

## Existing canonical inputs

| Input under `contracts/`            | Scope / generator                                                                                                                                                                                             |
| ----------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `organization-identity.json`        | Organization identity bindings; `generate-organization-identity.mjs`                                                                                                                                          |
| `country-profiles.json`             | Country/tax vocabulary; `generate-country-profiles.mjs`                                                                                                                                                       |
| `trading-policy.json`               | Reviewed policy requirements/access/versions; `generate-trading-policy.mjs`                                                                                                                                   |
| `pharmacy.json`                     | Organization/outlet, catalogue, inventory, safety and accounts inputs; `generate-pharmacy-contract.mjs`                                                                                                       |
| `supply.json`                       | Parties, licences, premises, access and operations inputs; `generate-supply-contract.mjs`                                                                                                                     |
| `emergency-dispatch.json`           | Dispatch/receiving models, commands, states and capabilities; `generate-emergency-dispatch.mjs`                                                                                                               |
| `workforce-enrollment.json`         | Staff enrollment; `generate-workforce-enrollment.mjs`                                                                                                                                                         |
| `workforce-directory.json`          | Basic directory account/version projection metadata; `generate-workforce-directory.mjs`                                                                                                                       |
| `workforce-roster-config.json`      | Versioned roster definition; `generate-workforce-roster-config.mjs`                                                                                                                                           |
| `workforce-documents.json`          | Private staff file models; `generate-workforce-documents.mjs`                                                                                                                                                 |
| `workforce-leave.json`              | Leave policies/requests/ledger and reviewer scope discovery; `generate-workforce-leave.mjs`                                                                                                                   |
| `workforce-duty.json`               | Duty exchanges; `generate-workforce-duty.mjs`                                                                                                                                                                 |
| `workforce-overtime.json`           | Overtime configuration and decisions; `generate-workforce-overtime.mjs`                                                                                                                                       |
| `workforce-attendance.json`         | Punches/corrections/periods/timesheets; `generate-workforce-attendance.mjs`                                                                                                                                   |
| `workforce-payroll.json`            | Payroll configuration and review inputs; `generate-workforce-payroll.mjs`                                                                                                                                     |
| `workforce-credentials.json`        | Credential reviews/reminders; `generate-workforce-credentials.mjs`                                                                                                                                            |
| `workforce-benefits.json`           | Dependents/benefits proposals and decisions; `generate-workforce-benefits.mjs`                                                                                                                                |
| `workforce-login-links.json`        | Reviewed staff/account binding commands and history; `generate-workforce-login-links.mjs`                                                                                                                     |
| `workforce-identity-checks.json`    | Workforce identity source/projection, employment generation, draft commands/receipts and scoped permission identifiers; `generate-workforce-identity.mjs` generates shared Go/Zod models                      |
| `workforce-employment-periods.json` | Private appointment/contract draft source, commands and immutable revisions; `generate-workforce-employment-periods.mjs` generates Go/Zod models                                                              |
| `staff-access.json`                 | IAM assignment proposals, decisions/history, owner verification and projection-request events; `generate-staff-access.mjs` generates IAM-owned Go HTTP models, shared Zod and public owner event declarations |

Staff access definitions live in `staff-access.json.template_migrations`, an
ordered canonical history. Each entry records its IAM migration ID, safe generated
SQL filename, normalized public-definition digest, exact SQL digest and introduced
versioned templates. The generator validates the complete history before writing
outputs, preserves migration 0014 byte-for-byte and flattens definitions for the
existing Go/Zod/UI artifacts. New keys require an additive catalog entry and a
separately registered/reviewed forward migration; generation alone neither wires
the migration nor activates a product workflow. Never edit old definitions or
regenerate old SQL from the latest authority. See the
[history design](../superpowers/specs/2026-10-31-staff-template-migration-history.md).

Also inspect module-local catalogues such as `backend/internal/modules/emergency/contract/intake.json`; the top-level directory is not the complete schema universe. Existing generators often encode additional checks such as date ordering, normalization, safe integer bounds and scope constraints. Reusing field names while dropping those checks is a breaking validation change.

Enrollment `request_context_projection` metadata also owns the opt-in query,
version and response acknowledgment. Its generated optional body marker is
required by the owner for projected PUTs and rejected without that query; the
strict decoder in older owners rejects it before writes. SDK parsing preserves
legacy inputs that omit the marker.

`workforce-enrollment.json` field tuples are `[name, typeOrLimit, optional?]`.
The optional input flag preserves omitted properties through SDK parsing rather
than synthesizing empty-string clears. Its `profile_masters` tuples are
`[category, profileField, rejectNull, selectionOnly]`; the generator validates
complete unique reference coverage and produces Go bindings, null/selection
rules and TypeScript selector metadata. These canonical mapping records are not
HTTP request fields. Its `profile_masters_projection` metadata declares query,
version and response-marker names; Go/TypeScript constants and the default
legacy response projection are generated from the same canonical input. New
optional fields are included only in the opted-in response. Existing validation
exceptions stay explicit in the owner.

## Storage dictionary

The [generated reference](reference/README.md) groups every registered module's table declarations by migration, including HR's large versioned model. [Machine inventory](reference/inventory.json) includes migration SQL hashes, typed concurrent-index definitions (table, ordered keys and direction), grants and directed-write tables. The SQL hash covers SQL only; index metadata is a separate canonical inventory field. Read the module's `Migrations()` and its embedded `.sql` files to reconstruct the actual schema, including later constraints/policies/triggers/views. Shared tenantdb, event and audit migrations follow the actual `bootstrap.PlatformMigrations()` order; migrator bookkeeping is identified separately.

Storage invariants are local to the owner. Tenant UUIDs and hospital/outlet/premise/base/facility relationships must be checked under the relevant principal. References across product boundaries are logical owner references; a future database split must not depend on cross-database foreign keys or shared write credentials. Do not add a second patient/stock/financial master to make a consumer join easier.

Use exact money, quantity precision, unit conversions, calendar dates, UTC instants and effective versions defined by the owner. UUIDv7 is required by several newer domains but not all legacy IDs. Private file references need exact object generation/checksum and authorized release state; file name or URL is not authority. A reported clinical fact and a verified/signed result must remain different schema concepts.

## Consumer review checklist

For every resource, specify names/types, bounds, required versus missing/null/empty, enum evolution, units/currency, timestamp versus local date, ID scope/version, optimistic token, sensitive fields, response projection and source owner. For commands, add legal state transition, replay and conflicting-key behavior. For history, document correction/retention and stable ordering. For reports, define numerator/denominator, time window, exclusions, missing-data state and freshness.

Validate synthetic examples against the selected standard schema with formats enabled. Reconcile examples with real handler tests; spec validity alone cannot establish semantic or authorization correctness. A future schema registry/CI gate must test both producer and actual consumer compatibility under #710. No production data or secrets belong in schemas or examples.

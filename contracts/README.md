# Helivanta contract sources

Start with the [API/specification index](../docs/api/README.md) and [schema source dictionary](../docs/api/schemas.md).

Existing top-level JSON files are custom input dialects for the corresponding `scripts/generate-*.mjs` generators. They generate selected backend Go models/validation and frontend Zod contracts. They are not directly consumable OpenAPI or JSON Schema. Edit the canonical input and regenerate; do not hand-edit generated models or copy them into consumers.

`partner-grants.json` owns the unreleased, non-operating supplier grant preparation
models. Generate/check with `node scripts/generate-partner-grants.mjs` (optionally
`--check`); see [its authority and recovery boundary](../docs/api/partner-grants.md).

`api/clinical.openapi.json` is an OpenAPI 3.1 descriptive pilot for the implemented legacy visit/lab/dispense/medication/insight APIs. It does not cover all runtime operations, replace existing generators, or assert hospital-level isolation for those tenant-scoped legacy tables. `api/event-envelope.schema.json` describes the existing event envelope, including tenant-less revocation and optional directed destination.

[Generated event payload schemas](../docs/api/reference/event-payloads.json) map all registered published subjects to their current Go payload schemas. [Runtime inventory](../docs/api/reference/inventory.json) records every module/route/permission/event/migration declaration. Regenerate/check from `backend/` with `go run ./cmd/contractdocs` / `go run ./cmd/contractdocs -check`.

Bed/admission, commercial CRM and MediConnect drafts live in the domain specifications. They are not approved schema sources for clients. Use [review gates](../docs/api/review-plan.md) before promotion to a supported API. No current artifact establishes FHIR/ABDM conformance or regulatory certification.

`organization-structure.json` owns organization hospital request receipts and
versioned collection heads, department commands/snapshots, and the organization/
Workforce identifier-only reference and revision events. Generate/check with
`node scripts/generate-organization-structure.mjs` and `--check`; no operational
Staff authority follows from these metadata models.

`workforce-postings.json` owns the local posting-preparation draft and identifier-only
change event. Generate/check with `node scripts/generate-workforce-postings.mjs`
and `--check`. Drafts never grant operating authority; see the
[candidate contract](../docs/api/workforce-postings.md).

The unreleased Pharmacy department relationship contract is canonical in
[pharmacy-staff-bindings.json](pharmacy-staff-bindings.json).
Run `node scripts/generate-pharmacy-staff-bindings.mjs` and its
`--check` mode. It generates owner Go models, the identifier-only
relationship event and SDK Zod models. Paired replacement identity/version and
approval-only attestation are validated by the generator and owner. These models
do not authorize dispensing or establish professional acceptance.

The unreleased own Staff-actor hospital choice contract is canonical in
`contracts/workforce-staff-actors.json`. Generate and verify the Go/Zod metadata
with `node scripts/generate-workforce-staff-actors.mjs` and `--check`. Its only
fields are hospital ID and current hospital name; it grants no Staff directory
or operating access. Pharmacy outlet-choice metadata belongs to the existing
`pharmacy-staff-bindings.json` contract and generator.

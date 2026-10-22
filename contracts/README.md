# Helivanta contract sources

Start with the [API/specification index](../docs/api/README.md) and [schema source dictionary](../docs/api/schemas.md).

Existing top-level JSON files are custom input dialects for the corresponding `scripts/generate-*.mjs` generators. They generate selected backend Go models/validation and frontend Zod contracts. They are not directly consumable OpenAPI or JSON Schema. Edit the canonical input and regenerate; do not hand-edit generated models or copy them into consumers.

`api/clinical.openapi.json` is an OpenAPI 3.1 descriptive pilot for the implemented legacy visit/lab/dispense/medication/insight APIs. It does not cover all runtime operations, replace existing generators, or assert hospital-level isolation for those tenant-scoped legacy tables. `api/event-envelope.schema.json` describes the existing event envelope, including tenant-less revocation and optional directed destination.

[Generated event payload schemas](../docs/api/reference/event-payloads.json) map all registered published subjects to their current Go payload schemas. [Runtime inventory](../docs/api/reference/inventory.json) records every module/route/permission/event/migration declaration. Regenerate/check from `backend/` with `go run ./cmd/contractdocs` / `go run ./cmd/contractdocs -check`.

Bed/admission, commercial CRM and MediConnect drafts live in the domain specifications. They are not approved schema sources for clients. Use [review gates](../docs/api/review-plan.md) before promotion to a supported API. No current artifact establishes FHIR/ABDM conformance or regulatory certification.

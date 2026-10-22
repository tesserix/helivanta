# Contract review findings and delivery plan

[Contract index](README.md) · baseline `2295017` · 2026-10-22

This is a repository-wide architecture/contract inventory and targeted source review. It is not a line-by-line security audit, production verification, regulatory sign-off or a claim that every backlog acceptance criterion has shipped. GitHub issue state and requirements were consulted on 2026-10-22; issue text can change. No issue/PR was created, closed or reassigned.

## Decisions used for this documentation

1. Preserve the current modular-monolith implementation and the proposed product-API direction as distinct states. Do not introduce new services to organize documentation.
2. Generate the exhaustive route/event/migration index from the real registry, including conditional consumers; keep narrative specs focused on ownership, business meaning and integration failure behavior.
3. Preserve existing JSON→Go/Zod sources. Introduce a descriptive OpenAPI pilot for the small legacy clinical API and structural schemas for all existing published events, with coverage/validation limits explicit.
4. Treat bed/admission and CRM/MediConnect contracts as review drafts. Do not silently invent deployed endpoints or select a CRM vendor.

The simpler alternative—one large handwritten API document—would immediately drift across 376 operations and 69 event subjects. A complete OpenAPI-first rewrite in the same change would alter existing generation/validation architecture without consumer evidence. The scoped generator and reviewed descriptive pilot provide concrete artifacts while retaining current behavior.

## Findings requiring follow-up

| Priority                    | Evidence / gap                                                                                                                        | Impact and proposed owner action                                                                                         |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| Before clinical reliance    | `medicore/visits.go` accepts a UUID `patient_id` without hospital-scoped existence/access validation                                  | Clinical/platform owners define and test authorized patient/encounter reference validation before expanding clinical use |
| Before clinical reliance    | Lab/pharmacy `consumers.go` automatically create work for every visit; lab defaults to CBC                                            | Clinical, lab and pharmacy owners replace demonstration intake with explicit authorized order/prescription intent        |
| Before external integration | Visit creation lacks durable request deduplication; result/dispense completion returns conflict on replay                             | Platform/domain owners specify operation-level timeout recovery and durable replay; do not apply blind retries           |
| Privacy review              | `medicore/contract.VisitCreatedData` includes patient name, unlike newer ID-only payloads                                             | Clinical/security owners review exposure/retention and plan a compatible metadata-only version with both consumers       |
| Architecture migration      | Legacy visit/lab/dispense tables are tenant-scoped while patient/workforce/outlet/base slices have narrower grants                    | Define hospital/resource scope migration and negative tests; do not claim universal hospital isolation                   |
| Contract authority          | #4 requests Problem Details/OpenAPI-first; target prose uses `{code,message,request_id}`; current `respond` uses `{error,message}`    | Platform and consumers approve a v1-compatible transition/ADR; keep current behavior documented until implemented        |
| Event compatibility         | Envelope `type` conventions differ; hospital/aggregate/correlation fields are not universal; some consumers reject unknown fields     | Platform and producers/consumers agree a versioned envelope/payload evolution strategy and fixtures                      |
| Completion semantics        | Published lab/dispense events have no chart/billing consumers                                                                         | Domain owners specify actual completion/freshness/reconciliation; publication is not an end-to-end handoff               |
| Product coverage            | Beds, full diagnostic lifecycle, signed prescription/stock/charge, full procurement and CRM/MediConnect are not complete current APIs | Domain owners turn draft requirements into reviewed vertical-slice contracts and tests                                   |
| Commercial ownership        | #808 external sales CRM and architecture hospital engagement are different contexts; DPA evidence versus verified state needs clarity | Commercial/platform/privacy owners choose vendor and ratify field-level ownership/activation gates                       |
| Tooling                     | Source registry parity and structural schemas do not validate real HTTP/event traffic or detect all breaking changes                  | DevEx implements conformance/compatibility gates under #710; no claim those gates are completed here                     |

These are integration-readiness findings, not unrelated runtime fixes. Existing behavior is unchanged.

## Ordered delivery

| Wave                                      | Deliverable                                                                                                                         | Reviewers and exit evidence                                                                                                                    |
| ----------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| 0 — baseline, this change                 | Full current inventory, structural event schemas, clinical OpenAPI pilot, domain coverage/specs and prioritized gaps                | Maintainers review evidence and terminology; generated drift check and artifact validation pass                                                |
| 1 — shared policy                         | Ratified HTTP/errors/versioning, idempotency, envelope, schema authority and consumer compatibility ADRs; inventory gate in CI      | Platform, security and at least one consuming owner; breaking/additive fixture tests and explicit v1 migration path                            |
| 2 — clinical identity and beds            | Validated patient reference, explicit order intent, bed/admission/transfer/discharge contracts and clearance ownership              | Clinical, nursing, housekeeping, lab, pharmacy, finance and safety; concurrent allocation, cross-hospital denial, correction and failure tests |
| 3 — established large domains             | Operation-by-operation OpenAPI for pharmacy, supply, workforce and emergency, derived from existing inputs and response projections | Each domain owner plus actual consumer; every inventory route assigned complete/unsupported/proposed status with payload/error/replay evidence |
| 4 — completion journeys                   | Specimen-to-release/amendment, prescription-to-dispense/charge, dispatch/receipt reconciliation and governed reporting              | Producer and consumer owners; duplicate, timeout-after-commit, stale version, revoked authority and DLQ replay scenarios                       |
| 5 — engagement and commercial             | Chosen CRM adapter, prospect/discovery/handover/DPA gate, patient/proxy portal, referral and messaging contracts                    | Commercial/implementation, hospital operations, privacy/security and clinical; source ownership, outage, consent and suppression evidence      |
| 6 — other portfolio and external adapters | Care/Doctor/Admin/Facility/Safety APIs, subscriptions/rights/retention, FHIR/ABDM/HL7/PACS/TPA/payment/ERP adapters                 | Named owners and jurisdiction reviewers; contract fixtures, capability/security review and external certification where actually required      |

These are dependency waves, not delivery dates or team assignments. Numeric per-product latency, peak volume, payload/attachment size, retention and recovery budgets need owner/customer confirmation. Reuse existing [architecture sizing](../architecture/01-system-design.md) and [availability/recovery/cost](../architecture/05-deployment-and-transition.md) assumptions rather than fabricate measured SLOs.

## Reviewable contract acceptance

Every new/changed operation must identify owner and consumers; exact versioned request/response/error schemas; scope/entitlement/action/object checks; state transition and concurrency; idempotency/replay/recovery; data classification/retention; events; dependency timeout/degradation; synthetic examples and positive/negative/duplicate/stale/partial-failure tests. Capture open decisions with responsible review roles. Record approval and consumer migration evidence before marking the contract approved.

An extraction/release needs a single writer, compatible expansion/backfill, consumer rollout, source/projection reconciliation, and rollback/recovery after accepted writes. Removing docs/tooling is reversible without changing runtime. Reverting live ownership after new writes is a data-reconciliation problem and requires its own approved action; these docs authorize no production mutation.

## Evidence sources

### Validation of this documentation change

Executed on 2026-10-22:

- `go test -race ./cmd/contractdocs` — passed, including registry/auth boundaries, conditional consumers, shared audit migrations, event fields, clinical route/permission parity and non-mutating drift checks. New generator behavior was introduced after the corresponding tests failed.
- `go vet ./cmd/contractdocs`, `go build -o /tmp/helivanta-contractdocs ./cmd/contractdocs`, `golangci-lint run ./cmd/contractdocs/...` and `gofmt -l cmd/contractdocs` — passed with no lint/format findings.
- `go run ./cmd/contractdocs` followed by `go run ./cmd/contractdocs -check` — generated and verified the references without infrastructure connections.
- OpenAPI validation with `openapi-spec-validator`; JSON Schema 2020-12 validation with `jsonschema` — passed for the 11-operation pilot, envelope, all 69 payload schemas and synthetic visit-event example.
- Local Markdown link checks, Prettier 3.9.10 checks on authored documentation/contracts and `git diff --check` — passed. Generated reference formatting is owned by the Go exporter.

Full application, database/container and browser suites were not run: this change adds documentation and an offline exporter, with no handler, migration, UI or deployment changes. Existing JSON→Go/Zod generators were not changed or claimed revalidated. Runtime HTTP/event conformance, compatibility enforcement in CI and production behavior remain the explicit follow-ups above.

Current code: `backend/internal/bootstrap/{modules,chain,unauthenticated}.go`, `internal/platform/{router,listroute,respond}/`, all ten module registration/contract/migration surfaces, selected owning handlers and consumers, `pkg/{events,authn,authz,pagination,tenantdb}` and existing generator sources. Existing documentation: architecture/ADRs, `docs/api`, HR capability coverage, pharmacy/supply delivery and requirement docs, emergency operations/dispatch/receiving and exploration ledgers.

GitHub review included [#4](https://github.com/tesserix/helivanta/issues/4), [#12](https://github.com/tesserix/helivanta/issues/12), [#106](https://github.com/tesserix/helivanta/issues/106), [#983](https://github.com/tesserix/helivanta/issues/983), [#1043](https://github.com/tesserix/helivanta/issues/1043), [#472](https://github.com/tesserix/helivanta/issues/472), [#667](https://github.com/tesserix/helivanta/issues/667), [#710](https://github.com/tesserix/helivanta/issues/710), [#808](https://github.com/tesserix/helivanta/issues/808), [#807](https://github.com/tesserix/helivanta/issues/807), [#811](https://github.com/tesserix/helivanta/issues/811), [#813](https://github.com/tesserix/helivanta/issues/813), [#1224](https://github.com/tesserix/helivanta/issues/1224), and domain issue searches for beds, CRM, MediConnect, diagnostics, integration and billing. This is traceable sampling plus the complete runtime registry, not a claim to have read every GitHub issue.

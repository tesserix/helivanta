# ADR-0035: Generated contract reference with explicit review status

**Date:** 2026-10-22

**Status:** Proposed for maintainer and consumer review; documentation tooling implemented. Does not ratify new runtime APIs.

## Context

The current modular monolith registers 376 product operations across ten modules and publishes 69 event subjects. Selected inputs are generated from custom JSON into Go/Zod, other HTTP models live in handlers, and product-API/FHIR/CRM architecture remains partly proposed. A handwritten portfolio-wide reference would drift, while declaring a new OpenAPI source of truth without migrating existing generators would create competing models.

## Decision

Generate route/permission/pagination, grant, subject/consumer, migration-hash and table-declaration facts from the actual module registry without connecting to infrastructure. Include consumers gated by workflow configuration using a disconnected registration client, clearly distinguishing capability from deployment enablement. Generate structural event payload schemas from the existing Go contract types and fail on unsupported types or missing subject mappings.

Use [the API index](../api/README.md) as the navigation point. Describe current transport/error/retry exceptions honestly. Introduce an OpenAPI 3.1 descriptive pilot for the small legacy clinical API. Preserve existing JSON→Go/Zod generators, module ownership and v1 behavior. Mark bed/admission, CRM/MediConnect and other unimplemented contracts as review proposals. Promote broader OpenAPI authority only through a consumer-reviewed migration with semantic/conformance evidence.

## Alternatives

One manually maintained document is simpler initially but cannot keep 376 routes and 69 subjects in step with source. A full OpenAPI-first rewrite offers a useful target but is not a documentation-only change and risks dropping current validation. Deploying a registry service adds operational cost without solving source ownership; local artifacts suffice for the present requirement.

## Consequences and validation

No new live service, datastore, endpoint, migration or deployment cost is introduced. Existing runtime behavior remains unchanged. Source parity is checked with `go run ./cmd/contractdocs -check`; tests exercise module/auth boundaries, conditional consumers, payload fields and non-mutating drift detection. Structural schema validity and static route parity do not prove real HTTP/event conformance or backward compatibility. [The review plan](../api/review-plan.md) names remaining gates and owners.

Tooling/docs can be reverted without changing runtime state. A future service extraction or new wire convention needs its own compatibility, single-writer migration, reconciliation, recovery and rollout approval. This ADR does not override existing release/destructive-operation rules.

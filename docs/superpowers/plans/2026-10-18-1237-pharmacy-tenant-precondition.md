# Pharmacy precondition implementation

[Specification](../specs/2026-10-18-1237-pharmacy-tenant-precondition.md), story #1237.

1. Reproduce a stale expected-tenant CSV request returning success using the existing real PostgreSQL harness.
2. Add the pharmacy principal boundary check and route the traced scope/registry/medication/dispense callers through it. Protect insights and country configuration without broad platform changes.
3. Extend public route tests across all declared families, header forms, both tenants, existing permissions/grants and no-side-effect/no-success-audit assertions. Change one behavior at a time with red→green evidence.
4. Run backend race/coverage, vet, build, lint and formatting. Exercise the local API with real OIDC sessions and synthetic data; retain source state and normal failure behavior.
5. Publish a reviewable PR referencing the open story. Obtain named production approval, release through the existing pipeline and verify real mismatched requests fail without files or writes before a completion comment and closure.

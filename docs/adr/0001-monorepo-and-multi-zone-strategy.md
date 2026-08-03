# ADR-0001: Full monorepo with path-based Next.js multi-zones

- **Status:** Accepted (2026-08-04)
- **Context:** Issue #665 asked where product code lives. Issue #2 fixes the
  backend as one Go module. The frontend needs per-product team isolation
  without per-department domain sprawl.
- **Decision:** `tesserix/hms` is a full monorepo: `apps/` (Next.js zones),
  `backend/` (Go modular monolith `github.com/tesserix/hms`), `packages/`.
  Zones map 1:1 to products (MediCore, PharmaConnect, LabConnect …) and are
  path-mounted (`/medicore`) behind the shell on one tenant domain. JS uses
  pnpm workspaces + Turborepo (deviation from mark8ly's npm — matches the
  Web SDK direction, issue #696). Deployment manifests live in tesserix-k8s,
  not here. SDK packages (go-shared, @tesserix/web) stay in their own repos.
- **Consequences:** one PR spans frontend+backend; zone count grows with
  products, not departments; cross-zone navigation is a hard navigation —
  shared chrome must come from a shared component, not a persistent tree.

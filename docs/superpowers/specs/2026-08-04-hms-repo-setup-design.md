# Helivanta Repo Setup — Multi-Zone Monorepo Design

**Date:** 2026-08-04
**Status:** Approved (brainstorming session with Mahesh)
**Closes planning questions from:** issues #1, #2, #12, #664, #665

## Context

The `tesserix/helivanta` repo is a greenfield hospital management system platform. The
product portfolio (issue #12) defines 11 products; v1 targets three product
zones — MediCore (OPD + IPD), PharmaConnect (pharmacy), LabConnect (lab) —
plus a shell app. The GitHub issues are the requirements corpus (753 issues);
the foundational ones already fix the backend architecture:

- **Issue #2**: one Go module `github.com/tesserix/helivanta`, modular monolith
  (`cmd/api`, `internal/modules/<domain>`, `pkg/`), CNPG PostgreSQL 16 with
  forced RLS, NATS JetStream with transactional outbox, Redis, OTel with PHI
  redaction, `make dev` local stack in ≤ 10 minutes.
- **Issue #1**: "multizone" there means multi-availability-zone GKE resilience
  plus OpenFGA as the single authorization decision point. It is distinct from
  the Next.js Multi-Zones frontend pattern chosen here; both apply.
- **Issue #12**: modules never share tables; cross-product data crosses
  boundaries only via NATS domain events. A code-first product registry drives
  entitlements, ownership, and billing codes.

## Decisions

### D1. Full monorepo

`hms` hosts all Next.js zone apps, the Go modular monolith backend, and shared
packages. Deployment manifests do NOT live here (see D5). This resolves issue
#665 for product code; record as ADR-0001.

### D2. Frontend: path-based Next.js Multi-Zones, one zone per product

One domain per hospital tenant: `{tenant}.hms.app` (`hms.app` is a working
placeholder — the design is domain-agnostic and the real domain is a
deployment-time Cloudflare/DNS choice, not a code change). A thin shell app
owns `/`;
each product zone is a separate Next.js app mounted under a `basePath`:

```
{tenant}.hms.app
├─ /              → apps/shell      (GIP login, dashboard, zone nav)
├─ /medicore/*    → apps/medicore   (OPD + IPD as route groups)
├─ /pharmacy/*    → apps/pharmacy   (PharmaConnect)
└─ /lab/*         → apps/lab        (LabConnect)
```

- Zone = product = owning team = entitlement code (aligns with issue #12).
  OPD and IPD stay inside `apps/medicore` as route groups because they share
  the patient-chart/encounter UI and one owning team.
- Same-origin means one HttpOnly session cookie works across all zones; no
  CORS; cross-department navigation is a hard navigation (full page load).
- Local dev: shell's `next.config.ts` carries rewrites to the other zones'
  dev ports. Deployed: Cloudflare/Istio route by path prefix.

### D3. Persistent-feeling chrome without a persistent shell

Multi-zones cannot keep one React tree mounted across zones. Instead:

- Every zone renders the same `app-shell` / `dashboard-layout` from
  `@tesserix/web` (v1.8.1 — the design system already ships these plus
  data-table, command-palette, auth-layout, audit-log-viewer, etc.).
- Sidebar collapsed state, active tenant, and theme are stored in cookies and
  read server-side, so each zone paints the chrome already in the correct
  state — no flash, no layout jump.
- Within a zone navigation is soft (client-side); the hard hop happens only
  when crossing departments.
- Escape hatch: because routes are path-stable, any two zones can later be
  folded into one app without URL changes if a truly persistent shell becomes
  mandatory.

### D4. Auth: Google Identity Platform (GIP), no Keycloak

The issues say "Keycloak/GIP"; this is resolved as **GIP** (record as
ADR-0002). The org already standardizes on GIP per-product tenants on
`tesseracthub-480811` (`tesserix-k8s/docs/identity/gip-tenant-google-idp.md`,
`scripts/identity/enable-tenant-google-idp.py`).

- Shell owns login against the Helivanta GIP tenant (Firebase Web SDK multi-tenant
  flow; OTP-first per UX issue #551) and mints the HttpOnly session cookie
  for `{tenant}.hms.app`.
- Go `pkg/authn` verifies GIP JWTs (port from `go-shared` GIP middleware);
  populates `Principal{Subject, TenantID, Country}`.
- `pkg/authz` wraps OpenFGA per issue #1: every Gin route must declare a
  required relation or `authz.Public`; router construction fails at boot
  otherwise.

### D5. Deployment lives in tesserix-k8s

`hms` ships images; `tesserix-k8s` deploys them, following existing precedent
(mark8ly-_, fanzone-_, devai-*):

- Helm charts: `charts/apps/hms-api`, `hms-shell`, `hms-medicore`,
  `hms-pharmacy`, `hms-lab`, `hms-postgres`, `hms-openfga` (copy the
  `mark8ly-openfga` pattern), NATS via the existing `nats` chart.
- ArgoCD apps per environment under `argocd/<env>/`; Kargo promotion.
- Register services in `tesserix-k8s/services.yaml`.
- The hms repo keeps only `docker-compose.dev.yml` (Postgres 16, NATS, Redis,
  OpenFGA, Firebase Auth emulator) + `Makefile` for the local ≤ 10-minute
  cold-start story (issue #2).

### D6. Data sharing between zones

Zones never call each other and never share tables:

- All zones fetch the same Go API at same-origin `/api/*` (edge rewrite to
  hms-api). "Sharing data" between departments = both read the same API.
- Cross-product/module data flows via NATS domain events between backend
  modules (issue #12), delivered through the transactional outbox (issue #2).
- CI boundary linter forbids `internal/modules/a` importing
  `internal/modules/b`; a second check forbids publishing to NATS subjects
  absent from the event-contract registry.

### D7. Mobile apps (planned surface, stubs in v1)

Per issues #11, #566–#571: doctor, patient, nurse, and pharmacist apps with
offline-first sync, barcode scanning, and biometric login.

- `apps/mobile/` reserved for Expo (React Native) apps sharing
  `packages/api-client` and `@tesserix/native` + tokens from the design
  system (both packages already exist).
- Auth: Firebase native SDKs against the same GIP tenant; same Go API.
- v1 scaffolds directory stubs only; the mobile SDK is planned separately
  (issue #664 scopes it out of the web SDK RFC).

### D8. Tooling

- **pnpm workspaces + Turborepo** for JS (aligns with Web SDK issue #696 and
  the design-system repo; deviation from mark8ly's npm recorded in ADR-0001).
- **One Go module** for the backend; golangci-lint + the RLS migration linter
  - module-boundary linter in CI.
- CI (GitHub Actions): affected-only builds per workspace, Go test with
  `-race`, Playwright E2E, image build/push consumed by tesserix-k8s.

## Repo layout

```
hms/                             # pnpm workspace + one Go module
├── apps/
│   ├── shell/                   # "/" — GIP login, dashboard, zone nav
│   ├── medicore/                # "/medicore/*" — OPD + IPD route groups
│   ├── pharmacy/                # "/pharmacy/*"
│   ├── lab/                     # "/lab/*"
│   └── mobile/                  # Expo app stubs (doctor, patient, nurse, pharmacist)
├── backend/                     # github.com/tesserix/helivanta — Go modular monolith
│   ├── cmd/api/
│   ├── internal/modules/
│   │   ├── medicore/
│   │   ├── pharmaconnect/
│   │   ├── labconnect/
│   │   └── reference/           # trivial module proving the full wiring (issue #2)
│   └── pkg/
│       ├── tenantdb/            # RLS-scoped GORM; no raw *gorm.DB escape
│       ├── authn/               # GIP JWT verification → Principal
│       ├── authz/               # OpenFGA client + Gin Require middleware
│       └── events/              # outbox publisher + durable pull consumers
├── packages/
│   ├── ui/                      # Helivanta-only compositions over @tesserix/web
│   │                            # (patient banner, vitals card, bed map, …)
│   ├── api-client/              # OpenAPI-generated TS client (web + mobile)
│   ├── registry/                # product-registry manifest → Go + TS codegen (issue #12)
│   └── config/                  # shared eslint/tsconfig
├── docker-compose.dev.yml
├── Makefile                     # make dev → full local stack
├── docs/
│   ├── adr/                     # 0001 repo strategy, 0002 GIP over Keycloak
│   └── superpowers/specs/       # this document
└── .github/workflows/
```

## Error handling & testing

- API errors: consistent JSON envelope; auth failures 401/403; unknown
  product 404, recognised-but-unentitled 402 (issue #12); cross-tenant probes
  return 404 (existence not leaked, issue #2).
- Fail-closed on authz mutation paths; deny decisions never cached (issue #1).
- Go: testify + testcontainers per module; adversarial RLS tests (no
  `app.tenant_id` set ⇒ zero rows); FGA store-test assertion suite in CI.
- Frontend: Vitest for packages; Playwright E2E covering the critical
  journey login → OPD visit → pharmacy dispense → lab result across zone
  boundaries.

## Phase 1 scope (first implementation plan)

1. Repo skeleton: pnpm workspace, Turborepo, Go module, CI pipelines.
2. `backend` skeleton with `reference` module proving the full wiring —
   authn → tenantdb (RLS) → outbox → JetStream → consumer, one trace
   end-to-end (issue #2's acceptance test).
3. `apps/shell` with GIP login + session cookie + zone nav.
4. `apps/medicore` zone consuming the shared chrome and `/api/*`.
5. `docker-compose.dev.yml` + `make dev`.
6. ADR-0001 and ADR-0002 committed; hms entries proposed to
   tesserix-k8s `services.yaml`.

Pharmacy and lab zones replicate the medicore pattern in later phases.
OpenFGA model, product registry codegen, and mobile apps are separate
follow-on phases mapped to their issues.

## Out of scope for this design

- OpenFGA model content (issue #1 owns it) beyond the middleware contract.
- Multi-region / country-residency separation.
- Subscription/billing enforcement (consumes the registry, own epic).
- Mobile implementation (stubs only).

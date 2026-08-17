# SDK Architecture & Decisions

Status: **Proposed** (fulfils issue [#664 — SDK architecture blueprint & package layout (RFC)](https://github.com/tesserix/helivanta/issues/664))
Owners: Platform team. Reviewed decisions must be recorded as ADRs (#674).

---

## 1. Why an SDK at all

We are building 9 backend products (MediCore, MediConnect, DoctorConnect, PharmaConnect, LabConnect, EmergencyConnect, CareConnect, SupplyConnect, AdminConnect) plus FacilityConnect and SafetyConnect, and ~6 web portals, on one multi-tenant, India-first, PHI-handling platform. The cross-cutting concerns — tenancy, authN/authZ, audit, PHI-safe logging, telemetry, events, resilience — are identical in every service and catastrophic to get wrong in any one of them.

### Options considered

| Option                                            | Pros                                                                                                                                                           | Cons                                                                                                                                                                              | Verdict                                                                         |
| ------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| **A. Reusable SDK packages** (chosen)             | Solved once, tested once; compliance enforced structurally; fast product spin-up via templates; consistent APIs/UX; concentrated audit surface for pen-testing | Up-front investment; needs version discipline; risk of becoming a bottleneck or over-abstracted; a bad SDK release affects many services                                          | ✅ **Chosen** — the compliance argument alone decides it for healthcare         |
| B. Copy-paste / per-service implementation        | Zero coordination cost; teams fully independent; no version management                                                                                         | Guaranteed drift; every service re-implements tenancy/PHI handling (each a breach risk); N× maintenance; inconsistent APIs and logs; audits must cover every repo                 | ❌ Unacceptable risk profile for PHI                                            |
| C. Sidecar / service-mesh only (Istio handles it) | No app-code changes for mTLS, retries, some telemetry; language-agnostic                                                                                       | Cannot do tenant RLS binding, PHI redaction, audit semantics, authZ decisions, error envelopes — those live _inside_ the app; mesh already in place and complements, not replaces | ❌ Insufficient alone; **complementary** (we keep Istio for transport concerns) |
| D. Shared base container image / framework fork   | Central control                                                                                                                                                | Opaque, hard to version, painful upgrades, still needs in-process libraries                                                                                                       | ❌ Worst of both worlds                                                         |

**Caveats we commit to (they address Option A's cons):**

1. **No domain logic in the SDK.** Litmus test: "would a food-delivery platform also want this package?" If no, it's domain logic.
2. **Rule of three-ish:** packages are designed against the needs of the first two consuming services; speculative abstraction is rejected in review.
3. **The SDK is a product**, with owners, semver, changelogs, deprecation windows and a contribution model (product teams PR into it; platform team reviews) — so it never becomes a queue.

---

## 2. Goals and non-goals

**Goals**

- Make the dangerous things impossible-by-default: tenant-less DB queries, PHI in logs, fail-open authorization, unaudited sensitive actions, non-standard error responses.
- Make the right things free: tracing, metrics, health endpoints, graceful shutdown, correlation IDs, i18n/RTL readiness, accessible components.
- Make new services/apps cheap: production-shaped scaffold in under a day.

**Non-goals**

- Abstracting away Postgres/NATS/Redis behind "any database" interfaces (we standardise on the chosen stack; see §4.3).
- Mobile (Flutter/RN) SDK — planned separately after the web SDK stabilises.
- A runtime platform/PaaS. The SDK is libraries + templates; deployment stays Helm/ArgoCD via `tesserix-k8s`.

---

## 3. Design principles

1. **Fail closed.** AuthZ unavailable → deny. Tenant unknown → refuse the query. Scan pending → quarantine. The only documented fail-open: rate limiting for life-critical (EmergencyConnect) routes.
2. **Secure and compliant by default, opt-out is loud.** System (cross-tenant) DB context requires an explicit, audited API call with a reason string.
3. **Context is the carrier.** Request ID, tenant, principal, locale, trace context flow through `context.Context` (Go) / a typed app context (Web). Packages read from context; nothing is passed hand-to-hand.
4. **Thin over frameworks, thick over risks.** We wrap Gin/pgx/NATS thinly (don't hide them); we build real machinery where risk lives (tenancy guard, redaction, audit chain, outbox).
5. **Observable by construction.** Every package emits its own metrics/traces; a service using the SDK is diagnosable with zero extra work.
6. **Configuration over conditionals.** Country- and tenant-specific behaviour comes from the country profile and feature flags — a lint rule bans `if country == "IN"`.
7. **Contracts are enforced, not documented.** OpenAPI conformance, event-schema validation and error-envelope shape are CI gates (#710).

---

## 4. Key decisions (with pros/cons)

### 4.1 Repository strategy — one SDK monorepo per language

Fulfils #665. Decision: **two monorepos**: `helivanta-go-sdk` (Go modules) and `helivanta-web-sdk` (pnpm workspace publishing `@tesserix/helivanta-*`), seeded from `go-shared` and `design-system` respectively.

| Option                             | Pros                                                                                                                                      | Cons                                                                                                                                                 |
| ---------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Monorepo per language** (chosen) | Atomic cross-package changes; one CI/release train; one place to review; easy internal refactors; matches changesets/Go workspace tooling | Repo grows large; coarse permissions; unrelated packages share release cadence unless tooling splits them (changesets/Go submodule tags handle this) |
| Repo per package                   | Independent cadence and ownership per package                                                                                             | Cross-package changes need N coordinated PRs; version matrix explodes; discovery suffers; CI duplication                                             |
| Everything inside `helivanta` app repo | One repo total                                                                                                                            | Couples SDK releases to app history; consumers pull app code; breaks the "SDK is a product" model                                                    |

Migration note: `go-shared` history is kept (rename/import); `design-system` continues to own _visual_ primitives — the web SDK monorepo either absorbs it or depends on it (decide in #665's review; default: absorb, keep `@tesserix/web` as a compatibility alias for HomeChef until migrated).

### 4.2 Go module layout — single module, package-per-concern

| Option                                                                        | Pros                                                                             | Cons                                                                                                                                                              |
| ----------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Single module `github.com/tesserix/helivanta-go-sdk`, packages beneath** (chosen) | One version to pin; internal packages can share helpers; simplest consumer story | A service importing only `logging` still resolves the full dependency graph (mitigated: Go compiles/links only what's imported; heavy deps kept in leaf packages) |
| Multi-module (module per package)                                             | Consumers take only the dep trees they use; independent versioning               | Version matrix ("logging v1.3 with tenant v1.1?") is exactly the confusion an SDK should remove; submodule tagging friction                                       |

Revisit trigger: if a consumer demonstrably suffers from the unified dependency graph (e.g. a tiny cron job), split _that_ package out then — not before.

### 4.3 Abstraction depth — standardise the stack, don't abstract it

| Option                                                                                          | Pros                                                                                                                                                                                                                                 | Cons                                                                                                           |
| ----------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------- |
| **Thin wrappers on the chosen stack (Gin, pgx, NATS, Redis, OpenFGA, Keycloak, OTel)** (chosen) | Full power of each library remains available; less SDK code to maintain; no leaky lowest-common-denominator interfaces                                                                                                               | Swapping a backing technology later touches consumers (accepted: such swaps are rare and platform-wide anyway) |
| Full abstraction (`Storage`, `Queue`, `AuthProvider` interfaces)                                | Theoretical portability                                                                                                                                                                                                              | Interfaces leak, grow unions of all features, block advanced usage; we'd maintain an ORM-for-everything        |
| Exceptions — **where we DO abstract**                                                           | **Payments** (per-country providers is a hard requirement), **notifications** (channel/provider routing per country), **national integrations** (ABDM adapter vs no-op) — these have _known multiple implementations_ on the roadmap |                                                                                                                |

### 4.4 Tenancy enforcement — RLS with SDK-enforced session binding

The tenancy model (from the platform plan) is shared-schema Postgres + Row-Level Security, with schema/DB-per-tenant as a premium tier later.

| Option                                                          | Pros                                                                                                                                             | Cons                                                                                                                                                          |
| --------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **RLS + SDK guard that refuses tenant-less DB access** (chosen) | Defence in depth: DB enforces even if app code is buggy; SDK makes the binding automatic and the bypass explicit+audited; testable adversarially | RLS policies must exist on every table (checked in CI against the schema repo); `SET LOCAL` requires transaction discipline (the SDK's tx helper provides it) |
| App-level `WHERE tenant_id = ?` only                            | No RLS complexity                                                                                                                                | One forgotten predicate = cross-tenant breach; unreviewable at scale                                                                                          |
| DB-per-tenant for everyone                                      | Strongest isolation                                                                                                                              | Cost and operational explosion at hundreds of clinics; migrations × N; cross-tenant platform features (AdminConnect) become federation problems               |

### 4.5 Authorization — OpenFGA, fail closed, checks in the SDK

| Option                                                 | Pros                                                                                                                               | Cons                                                                                                                                             |
| ------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| **OpenFGA relationship model via SDK client** (chosen) | Care-relationship semantics ("doctor treating this patient") model naturally; central policy; batch checks; consistent fail-closed | Extra infra dependency on the request path (mitigated: short-TTL caching, batch APIs, availability SLO); revocation latency bounded by cache TTL |
| Roles-only (JWT claims)                                | No extra call                                                                                                                      | Cannot express patient-care relationships or facility scoping — the core healthcare requirement                                                  |
| Policy-in-DB per service                               | No new infra                                                                                                                       | Re-invents FGA per service; drifts                                                                                                               |

### 4.6 Observability — OpenTelemetry, vendor-neutral

| Option                              | Pros                                                                                                       | Cons                                                                            |
| ----------------------------------- | ---------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| **OTel SDK + OTLP export** (chosen) | Vendor-neutral (mandated by the plan); traces/metrics/logs correlated; one instrumentation for any backend | Slightly more setup than a vendor agent (hidden inside the SDK's one-line init) |
| Vendor APM agent                    | Fast start                                                                                                 | Lock-in, per-host pricing at hospital scale, PHI-scrubbing controls vary        |

Guardrails baked in: no high-cardinality tenant/patient identifiers as metric labels (tenant allowed on _traces_, bounded top-N on metrics); telemetry never blocks a request; PHI scrubbing applies to span attributes and events, not just logs.

### 4.7 Error contract — one envelope, defined in the SDK

Single JSON error envelope (code, message, requestId, details[]) produced by the Go `httpkit` and consumed/typed by the Web SDK. Pros: uniform client handling, supportable errors (request ID on screen ↔ trace in backend). Cons: none material; the risk is _not_ having it.

### 4.8 Frontend coupling — React-first, framework-agnostic core

| Option                                                                    | Pros                                                                                                       | Cons                                                             |
| ------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------- |
| **Core packages framework-agnostic (TS), React bindings on top** (chosen) | Auth/telemetry/i18n logic reusable by future mobile web-views or non-React surfaces; React hooks stay thin | Slightly more package structure                                  |
| React-only everything                                                     | Simpler today                                                                                              | Locks logic into React lifecycles; mobile SDK restarts from zero |

### 4.9 Distribution — GitHub Packages, pinned versions, no floating tags

Consistent with the existing `@tesserix/web` + `PKG_READ_TOKEN` pattern. Pros: already operational, private, org-scoped. Cons: token management in CI (already solved via GCP Secret Manager). Floating (`latest`, `v1`) consumption is banned — every consumer pins exact versions; upgrades arrive as PRs (renovate-style automation later).

---

## 5. What is IN each SDK (summary)

Cross-cutting concerns and their owning packages — details in [go-sdk.md](go-sdk.md) / [web-sdk.md](web-sdk.md):

| Concern                 | Go SDK                                                                                                      | Web SDK                                                                                              |
| ----------------------- | ----------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| Tenant & data isolation | `tenant` (context + guard), `database` (RLS binding), `cache` (namespaced keys), `ratelimit` (per-tenant)   | `auth` (tenant-scoped session), API clients carry tenant implicitly — never as a user-editable input |
| Security                | `authn` (Keycloak OIDC), `authz` (OpenFGA), `httpkit` (headers, CORS, timeouts), `config` (secret hygiene)  | `auth` (BFF sessions, guards), CSP/security-header presets in the app template                       |
| Privacy / PHI           | `logging` (redaction), `telemetry` (attribute scrubbing), `storage` (classification tags, AV quarantine)    | `telemetry` (scrubbing, clinical-screen opt-out)                                                     |
| Observability           | `telemetry` (OTel), `logging`, `httpkit` (RED metrics), golden dashboards (DevEx #716)                      | `telemetry` (web vitals, error reporting)                                                            |
| Reliability             | `events` (outbox, retries, DLQ), `httpkit` (timeouts), `cache`/`ratelimit` (breakers), `database` (pooling) | `data` (retry/backoff, offline cache), `flags` (kill switches)                                       |
| Audit & compliance      | `audit` (hash-chained emitter)                                                                              | — (audit is server-side truth)                                                                       |
| Consistency / DevEx     | `config`, `i18n`+`countryprofile`, `testkit`, `create-helivanta-service`                                          | `tokens`, `components`, `forms`, `i18n`, `api-client` codegen, `create-helivanta-app`                      |

**Explicitly OUT of the SDK** (with where it lives instead): domain models & workflows (product services); FHIR resource business mapping (MediCore); Helm charts & infra (tesserix-k8s); SQL schemas & RLS policies (tesserix-k8s `db-schema-bootstrap` — app repos never carry SQL); Keycloak realm config (identity infra); UI screens (portals).

---

## 6. Dependency rules

```
product services ──▶ helivanta-go-sdk ──▶ third-party libs
portals ──▶ @tesserix/helivanta-* ──▶ third-party libs
```

- The SDK **never** imports product code, and packages never import "up" (e.g. `logging` cannot import `httpkit`).
- Allowed intra-SDK edges are documented per package (see go-sdk.md §"Layering"); cycles are CI-blocked.
- Product repos may not import the third-party libs the SDK wraps for wrapped concerns (lint rule): e.g. no direct `jwt` parsing, no raw `slog.New`, no direct `pgxpool.New`. Direct use of _unwrapped_ libs is fine.

## 7. Versioning & releases (summary — full policy in #668)

- Semver per language ecosystem; **breaking = major**, including behavioural tightenings (e.g. a new redaction rule is _minor_; removing a redaction escape hatch is _major_).
- Go: tags on the single module; Web: changesets per package with automated changelogs.
- Deprecation: minimum one minor release with warnings before removal; upgrade notes mandatory in every release.
- Security fixes: patch releases backported to the last two minors.

## 8. Risks & mitigations

| Risk                                            | Mitigation                                                                                                                                     |
| ----------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| SDK team becomes a bottleneck                   | Contribution model: product teams PR, platform reviews; CODEOWNERS per package; SLA on review                                                  |
| Over-abstraction before real consumers          | Rule of three; packages graduate from `experimental/` only when 2+ services consume them                                                       |
| Breaking release stalls every team              | Pinned versions; canary adoption order (starter-template sample service first); contract tests in SDK CI against a sample consumer             |
| Heavy dependency graph                          | Leaf-package placement of heavy deps; periodic `go mod graph` budget review                                                                    |
| RLS policy gaps (SDK binds, table lacks policy) | CI check in tesserix-k8s: every tenant-owning table must carry the RLS policy pair; adversarial cross-tenant test suite (#694 testkit harness) |

# ADR-0004: Helivanta owns its platform packages; no go-shared dependency

- **Status:** Accepted (2026-08-13). Amends ADR-0001.
- **Context:** ADR-0001 closed with "SDK packages (go-shared, @tesserix/web)
  stay in their own repos". Issues #676–#695 were filed against that reading:
  a `[Go SDK]` series describing a separately-versioned Go module, published
  on merge, consumed by service repos, with a `create-helivanta-service` scaffold
  generating a Dockerfile, Helm chart and Argo CD stub per service.
  None of that describes what was built. `backend/go.mod` declares
  `github.com/tesserix/helivanta` and has **no go-shared requirement** — not in
  `go.mod`, not in `go.sum`. The shared capability lives in `backend/pkg/`:
  `authn` (OIDC token verification, revocation — GIP at the time, Zitadel since ADR-0006), `authz` (OpenFGA client and
  Gin middleware), `events` (outbox, JetStream consumers, DLQ, broadcast),
  `logging` (slog with PHI redaction), `tenantdb` (pooling, migrations,
  `WithTenant`/`WithSystem`/`WithAdmin`, `LintRLS`) — roughly 8,200 lines.
  The practice was settled before it was written down: #678
  "[Go SDK] Structured logging with PHI redaction" was closed by building
  `pkg/logging` here, and #778 by adding the `hmslog:"phi"` tag to it.
  go-shared is also not neutral ground. It serves the marketplace product's
  ~30 microservices, whose auth (per-product GIP pools), tenancy model and
  release cadence are not Helivanta's. Helivanta is one Go module with one deployable.
  The coupling that justifies a shared library across many services does not
  exist here.
- **Decision:** Helivanta owns its platform packages in `backend/pkg/`. It takes no
  dependency on go-shared, and publishes none. New shared capability is added
  under `backend/pkg/` and constrained by `internal/archtest` — modules never
  import each other, and cross-module flow is events or a `platform`-owned
  narrow interface. Where an issue says `[Go SDK]`, read `backend/pkg/`;
  where it says "service repo", read "module". The scaffold is
  `make new-module NAME=<name>`, not a service template.
  ADR-0001 is otherwise unchanged: `@tesserix/web` remains its own repo, and
  deployment manifests remain in tesserix-k8s.
- **Consequences:** a platform package and every consumer of it change in one
  PR, which is what the vertical-slice rule already requires and what a
  published module would have made impossible — no version bump, no
  `repository_dispatch`, no fleet upgrade. The cost is duplication: Helivanta and
  the marketplace will each solve outbox delivery, RLS binding and OpenFGA
  wrapping, and the two will drift. That is accepted. Extracting a shared
  module later, with two working implementations to compare, is a better
  decision than sharing one now on the assumption the requirements match —
  they demonstrably do not, since GIP-tenant auth and forced-RLS tenancy are
  Helivanta-specific. Consequently #676, #680–#685, #694 and #695 are closed as
  delivered, superseded or premised on service repos that do not exist; the
  unbuilt items keep their issues and are now understood as `backend/pkg/`
  work: #677 config, #679 OpenTelemetry, #686 audit emitter, #687 Redis,
  #688 object storage, #689 rate limiting, #690 i18n/country profile,
  #691 GrowthBook, #692 notifications, #693 payments.

# HMS Backend Standards & Guardrails

Date: 2026-08-04
Status: approved
Builds on: phase 2 backend (PR #755) and the frontend standards (PR #757 — same enforcement philosophy applied to Go)

## Goal

Codify how HMS backend modules are built so every contributor — human or
AI — produces the same patterns: machine-enforced lint and architecture
guardrails, shared handler helpers that kill the copy-paste, one test
scaffolding, a module generator, and docs/agent enforcement mirroring
the frontend standards.

## Decisions

- **D1 — Guardrails first.** Sequencing: lint + architecture tests land
  before helpers/generator/docs, so everything later is built under the
  gates.
- **D2 — golangci-lint is the lint stack** (errcheck, govet,
  staticcheck, revive, gosec, sqlclosecheck, depguard, misspell,
  unconvert), wired into CI. `go vet` alone is today's only check.
- **D3 — Module boundaries are machine-enforced.** A depguard rule plus
  an architecture test make `internal/modules/X` importing
  `internal/modules/Y` a build failure, not a convention.
- **D4 — Shared helpers end handler copy-paste.** `respond` envelope
  helpers and a `tenantPrincipal` extractor; the per-handler
  `uuid.Parse(p.TenantID)` + 401 dance and `gin.H{"error": ...}`
  literals disappear.
- **D5 — One test scaffolding.** `staticVerifier`/`setup`/`do` (today
  copy-pasted across four module test packages) move into
  `internal/testutil`; module tests import it.
- **D6 — Structured logging is slog-only** with standard fields
  (`tenant_id`, `module`, `event_id` where applicable); error wrapping
  is `fmt.Errorf("context: %w", err)`; a request-ID middleware tags
  every request log line.
- **D7 — Coverage gate:** 70% minimum per package for `internal/modules/*`
  and `pkg/*` (main.go/config exempt), enforced in CI.

## Deliverables

### 1. Lint stack (guardrail)

- `.golangci.yml` at `backend/`: enable errcheck, govet, staticcheck,
  revive, gosec, sqlclosecheck, misspell, unconvert, depguard.
- depguard: deny `github.com/tesserix/hms/internal/modules/*` from
  importing any *other* `internal/modules/*` package (self-imports
  allowed); deny `logrus` everywhere (slog only).
- CI backend job adds `golangci-lint run` before tests. Existing
  violations get fixed as part of the rollout task.

### 2. Architecture tests (guardrail)

`backend/internal/archtest/arch_test.go` (test-only package):

- **Module isolation:** parse the import graph of
  `internal/modules/*` (go/packages) and fail on cross-module imports —
  redundant with depguard by design (defense in depth; the test's
  failure message explains the rule).
- **Migration ID uniqueness:** instantiate every registered module,
  collect `Migrations()` IDs plus `events.Migrations()`, fail on
  duplicates.
- **Subject naming:** every consumer subject and published subject
  constant matches `^hms\.[a-z]+\.[a-z]+\.[a-z_]+\.v\d+$`; consumer
  names match `^[a-z]+-[a-z-]+$`.
- **RLS:** boot-time linter already exists; the arch test re-runs
  `LintRLS` against a migrated testcontainer to catch it at test time
  too (no waiting for a deploy to find out).

### 3. Shared handler helpers (`internal/platform/respond` + authn addition)

- `respond.OK(c, data)` / `respond.Created(c, data)` /
  `respond.Accepted(c, data)` — success envelope.
- `respond.Error(c, status, code, message)` plus shorthands
  `respond.NotFound(c, resource)`, `respond.Conflict(c, message)`,
  `respond.BadRequest(c, err)`, `respond.Internal(c, publicMessage)` —
  the `{"error", "message"}` envelope in one place.
- `authn.TenantPrincipal(c) (Principal, uuid.UUID, bool)` — extracts
  the principal, parses the tenant UUID, writes the 401 envelope and
  aborts when invalid; handlers become
  `p, tenantID, ok := authn.TenantPrincipal(c); if !ok { return }`.
- Status-code semantics documented and enforced by convention: 404 (not
  403) for cross-tenant probes, 409 for state-transition conflicts
  with a `status = 'pending'`-guarded UPDATE, 202 for async creates,
  list endpoints newest-first `LIMIT 100`.
- All four modules (reference, medicore, pharmacy, lab) migrate to the
  helpers as the reference implementations.

### 4. Test scaffolding consolidation

- `internal/testutil` gains: `StaticVerifier` (token→tenant map
  implementing `authn.TokenVerifier`), `ModuleHarness(t, mods...)`
  returning router + db + bus + ctx (the current `setup` function), and
  `Do(r, method, path, token, body)`.
- The four module test packages and the journey test drop their local
  copies (closes the parked phase-2 finding).

### 5. Observability standard

- slog everywhere; `logger.With("module", name)` at module
  registration; consumer logs carry `event_id` and `tenant_id`.
- `internal/platform/requestid` middleware: accept/generate
  `X-Request-ID`, put it in the Gin context and response header, and
  into a request-scoped slog logger.
- Error wrapping convention: `%w` with context strings; no
  `panic`/`Fatal` outside `main`.

### 6. Coverage gate

- CI computes per-package coverage (`go test -coverprofile`); a small
  script fails the job when any `internal/modules/*` or `pkg/*`
  package is below 70%. `cmd/*`, `internal/config`, `internal/archtest`
  exempt.

### 7. `make new-module NAME=<name>` generator

- `backend/scripts/new-module.sh` (or Go script): stamps
  `internal/modules/<name>/module.go` + `module_test.go` from the
  medicore shapes (migration with forced-RLS boilerplate + CHECK
  status column, routes using `respond` + `authn.TenantPrincipal`,
  consumer stub, tests using `testutil.ModuleHarness`), validates the
  name, refuses collisions, prints the follow-ups (register in
  `cmd/api/main.go`, subject naming, zone app if needed).

### 8. Docs + agent enforcement

- `docs/standards/backend.md`: module anatomy, RLS checklist, event
  contract rules (subjects, versioning, idempotent consumers, DLQ),
  status-code semantics, helper usage, logging fields, testing
  requirements (module tests + arch tests + coverage), generator
  pointer.
- `CLAUDE.md` gains a backend rules section; `.claude/skills/hms-backend`
  skill mirrors hms-frontend (rule table + reference files).

## Out of scope

- OpenFGA authorization model (issue #1 — own phase; the standards note
  where authz middleware will slot in).
- Distributed tracing/OTel and metrics endpoints (future phase).
- Repository-layer abstraction changes (GORM usage stays as-is).
- go-shared extraction (HMS keeps its own platform packages for now).

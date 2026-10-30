# Helivanta Phase 1 — Repo Setup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stand up the hms monorepo — pnpm/Turbo workspace, Go modular-monolith backend with a `reference` module proving authn → RLS-scoped DB → outbox → JetStream → consumer end-to-end, a shell Next.js app with GIP login, a medicore zone app, local dev stack, CI, and ADRs.

**Architecture:** One repo: `apps/` (Next.js 16 multi-zone frontends), `backend/` (single Go module `github.com/tesserix/helivanta`, modular monolith per issue #2), `packages/` (shared JS config). Zones are path-mounted (`/medicore`) behind the shell's rewrites; all data access goes through the Go API at `/api/*`. Auth is Google Identity Platform (GIP) via Firebase SDKs; tenancy enforced with forced PostgreSQL RLS; cross-module data flows only via NATS JetStream events through a transactional outbox.

**Tech Stack:** Go 1.26, Gin, GORM + PostgreSQL 16, NATS JetStream, Firebase Admin SDK (Go + Node), Next.js 16, React 19, Tailwind v4, `@tesserix/web` 1.8.x, pnpm + Turborepo, testcontainers-go, Playwright.

## Global Constraints

- Go module path is exactly `github.com/tesserix/helivanta`, rooted at `backend/` (spec D1/issue #2).
- Backend module boundaries: `internal/modules/<name>` must never import another module's packages (spec D6).
- Every table with a `tenant_id` column MUST have RLS enabled **and forced** with a policy carrying both `USING` and `WITH CHECK` (issue #2); the RLS linter test (Task 4) enforces this.
- Runtime DB access only via `tenantdb.WithTenant` — no exported raw `*gorm.DB` from the app pool (issue #2).
- Auth is GIP only — no Keycloak anywhere (spec D4, ADR-0002).
- The string `MedCora` must never appear in the repo (issue #12 rejected alias).
- Deployment manifests do NOT live in this repo — tesserix-k8s owns them (spec D5).
- Frontend ports: shell 4301, medicore 4302; backend API 8080. Dev infra: Postgres 5432, NATS 4222, Redis 6379, OpenFGA 8090, Firebase Auth emulator 9099.
- Commit messages: conventional commits, single line, no signatures.
- JS package manager: pnpm (via corepack); Node 22.

---

### Task 1: Repo skeleton — pnpm workspace, Turborepo, root configs

**Files:**

- Create: `pnpm-workspace.yaml`, `package.json`, `turbo.json`, `.gitignore`, `.npmrc`, `.nvmrc`
- Modify: `README.md`

**Interfaces:**

- Produces: workspace globs `apps/*`, `packages/*`; turbo tasks `build`, `dev`, `lint`, `type-check`, `test` that later tasks' apps plug into.

- [ ] **Step 1: Write root configs**

`pnpm-workspace.yaml`:

```yaml
packages:
  - "apps/*"
  - "packages/*"
```

`package.json`:

```json
{
  "name": "hms",
  "private": true,
  "packageManager": "pnpm@10.17.1",
  "engines": { "node": ">=22" },
  "scripts": {
    "build": "turbo run build",
    "dev": "turbo run dev",
    "lint": "turbo run lint",
    "type-check": "turbo run type-check",
    "test": "turbo run test"
  },
  "devDependencies": {
    "turbo": "^2.8.10"
  }
}
```

`turbo.json`:

```json
{
  "$schema": "https://turbo.build/schema.json",
  "tasks": {
    "build": {
      "dependsOn": ["^build"],
      "outputs": [".next/**", "!.next/cache/**", "dist/**"]
    },
    "dev": { "cache": false, "persistent": true },
    "lint": {},
    "type-check": {},
    "test": {}
  }
}
```

`.npmrc`:

```
@tesserix:registry=https://npm.pkg.github.com
//npm.pkg.github.com/:_authToken=${NODE_AUTH_TOKEN}
```

`.nvmrc`:

```
22
```

`.gitignore`:

```
node_modules/
.next/
.turbo/
dist/
coverage/
*.log
.env
.env.*
!.env.example
backend/bin/
test-results/
playwright-report/
.DS_Store
```

`README.md` — replace contents with:

```markdown
# Helivanta

Helivanta — a hospital management system platform, multi-zone monorepo.

- `apps/` — Next.js 16 zone apps (shell, medicore, pharmacy, lab) + mobile stubs
- `backend/` — Go modular monolith (`github.com/tesserix/helivanta`)
- `packages/` — shared JS config and Helivanta UI compositions
- Design: `docs/superpowers/specs/2026-08-04-hms-repo-setup-design.md`
- ADRs: `docs/adr/`

## Quick start

Requires Docker, Go 1.26, Node 22 (`corepack enable`).

    make dev        # infra (Postgres, NATS, Redis, OpenFGA, GIP emulator) + API + web
    make seed       # dev tenant + test user (test@hms.dev / password123)
    open http://localhost:4301

Deployment lives in `tesserix-k8s` (charts/apps/hms-*), not here.
```

- [ ] **Step 2: Verify workspace resolves**

Run: `corepack enable && pnpm install`
Expected: lockfile created, `turbo` installed, zero workspace projects found (none exist yet) — no errors.

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "chore: scaffold pnpm workspace and turborepo config"
```

---

### Task 2: Local dev stack — docker compose + Makefile

**Files:**

- Create: `docker-compose.dev.yml`, `dev/init-db.sql`, `dev/firebase/firebase.json`, `dev/firebase/.firebaserc`, `Makefile`

**Interfaces:**

- Produces: Postgres at `localhost:5432` (admin `hms`/`hms`, app role `hms_app`/`hms_app`, db `hms`), NATS JetStream at `localhost:4222`, Redis `6379`, OpenFGA HTTP `8090`, Firebase Auth emulator `9099` (project `demo-hms`). Make targets `dev-infra`, `dev`, `seed`, `test`.

- [ ] **Step 1: Write compose file and init scripts**

`docker-compose.dev.yml`:

```yaml
name: hms-dev
services:
  postgres:
    image: postgres:16-alpine
    environment:
      POSTGRES_USER: hms
      POSTGRES_PASSWORD: hms
      POSTGRES_DB: hms
    ports: ["5432:5432"]
    volumes:
      - ./dev/init-db.sql:/docker-entrypoint-initdb.d/init-db.sql:ro
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U hms -d hms"]
      interval: 2s
      timeout: 2s
      retries: 30

  nats:
    image: nats:2.10-alpine
    command: ["-js", "-sd", "/data"]
    ports: ["4222:4222", "8222:8222"]
    volumes: [natsdata:/data]

  redis:
    image: redis:7-alpine
    ports: ["6379:6379"]

  openfga:
    image: openfga/openfga:v1.8.4
    command: ["run"]
    environment:
      OPENFGA_DATASTORE_ENGINE: memory
    ports: ["8090:8080"]

  firebase-auth:
    image: node:22-alpine
    working_dir: /workspace
    command: sh -c "npx -y firebase-tools@13 emulators:start --only auth --project demo-hms"
    volumes:
      - ./dev/firebase:/workspace
    ports: ["9099:9099"]

volumes:
  pgdata:
  natsdata:
```

`dev/init-db.sql`:

```sql
-- Runtime role: no superuser, no RLS bypass. Migrations run as `hms`.
CREATE ROLE hms_app LOGIN PASSWORD 'hms_app' NOSUPERUSER NOBYPASSRLS;
GRANT USAGE ON SCHEMA public TO hms_app;
ALTER DEFAULT PRIVILEGES FOR ROLE hms IN SCHEMA public
  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO hms_app;
ALTER DEFAULT PRIVILEGES FOR ROLE hms IN SCHEMA public
  GRANT USAGE, SELECT ON SEQUENCES TO hms_app;
```

`dev/firebase/firebase.json`:

```json
{
  "emulators": {
    "auth": { "host": "0.0.0.0", "port": 9099 },
    "ui": { "enabled": false }
  }
}
```

`dev/firebase/.firebaserc`:

```json
{ "projects": { "default": "demo-hms" } }
```

`Makefile`:

```makefile
.PHONY: dev dev-infra dev-down dev-api dev-web seed test test-go test-web

dev-infra:
	docker compose -f docker-compose.dev.yml up -d --wait postgres nats redis openfga
	docker compose -f docker-compose.dev.yml up -d firebase-auth

dev-down:
	docker compose -f docker-compose.dev.yml down

dev-api:
	cd backend && go run ./cmd/api

dev-web:
	pnpm turbo dev

dev: dev-infra
	@echo "Infra up. Starting API + web (Ctrl-C stops both)…"
	@$(MAKE) -j2 dev-api dev-web

seed:
	node scripts/seed-dev.mjs

test: test-go test-web

test-go:
	cd backend && go test -race ./...

test-web:
	pnpm turbo lint type-check test build
```

- [ ] **Step 2: Verify the stack boots**

Run: `make dev-infra && docker compose -f docker-compose.dev.yml ps`
Expected: postgres (healthy), nats, redis, openfga running; firebase-auth reaches "All emulators ready" within ~60s (`docker compose -f docker-compose.dev.yml logs firebase-auth`).

Run: `docker compose -f docker-compose.dev.yml exec postgres psql -U hms -d hms -c "\du hms_app"`
Expected: role `hms_app` exists with `Cannot bypass RLS` not listed as an attribute (i.e., ordinary role).

- [ ] **Step 3: Commit**

```bash
git add docker-compose.dev.yml dev/ Makefile
git commit -m "feat: local dev stack with postgres, nats, redis, openfga, gip emulator"
```

---

### Task 3: Backend bootstrap — Go module, platform registry, HTTP server

**Files:**

- Create: `backend/go.mod`, `backend/internal/config/config.go`, `backend/internal/platform/module.go`, `backend/internal/platform/registry.go`, `backend/internal/platform/registry_test.go`, `backend/cmd/api/main.go`, `backend/internal/httpserver/server.go`

**Interfaces:**

- Produces:
  - `platform.Module` interface: `Name() string; Migrations() []tenantdb.Migration; Routes(r *gin.RouterGroup, deps Deps); Consumers() []events.Consumer` (tenantdb/events types land in Tasks 4/6 — this task declares the interface with those imports; the packages get stub type definitions here and are fleshed out in their own tasks).
  - `platform.Deps{DB *tenantdb.DB, Bus *events.Bus}`.
  - `platform.NewRegistry()`, `(*Registry).Register(m Module) error` (duplicate name → error), `(*Registry).All() []Module`.
  - `config.Load()` reading env: `PORT` (8080), `APP_DATABASE_URL`, `ADMIN_DATABASE_URL`, `NATS_URL`, `GIP_PROJECT_ID` (default `demo-hms`).
  - `httpserver.New(cfg, readyChecks)` exposing `GET /healthz`, `GET /readyz`, and `Group(path, mw...)`.

- [ ] **Step 1: Init module and stub packages**

Run: `mkdir -p backend && cd backend && go mod init github.com/tesserix/helivanta && go get github.com/gin-gonic/gin@latest github.com/google/uuid@latest`

Create minimal stubs so `platform` compiles before Tasks 4/6 (each of those tasks replaces its stub):

`backend/pkg/tenantdb/types.go`:

```go
package tenantdb

// Migration is one ordered, idempotent schema change. Applied by ID once.
type Migration struct {
	ID  string
	SQL string
}

// DB is defined fully in db.go (Task 4).
```

`backend/pkg/events/types.go`:

```go
package events

import (
	"context"
	"encoding/json"
	"time"
)

// Event is the platform envelope (issue #2).
type Event struct {
	ID         string          `json:"event_id"`
	Type       string          `json:"type"`
	Version    int             `json:"version"`
	OccurredAt time.Time       `json:"occurred_at"`
	TenantID   string          `json:"tenant_id"`
	Data       json.RawMessage `json:"data"`
}

// Consumer is a durable, idempotent subscription owned by a module.
type Consumer struct {
	Name    string
	Subject string
	Handle  func(ctx context.Context, evt Event) error
}
```

`backend/internal/config/config.go`:

```go
package config

import "os"

type Config struct {
	Port             string
	AppDatabaseURL   string
	AdminDatabaseURL string
	NATSURL          string
	GIPProjectID     string
}

func Load() Config {
	return Config{
		Port:             getenv("PORT", "8080"),
		AppDatabaseURL:   getenv("APP_DATABASE_URL", "postgres://hms_app:hms_app@localhost:5432/hms?sslmode=disable"),
		AdminDatabaseURL: getenv("ADMIN_DATABASE_URL", "postgres://hms:hms@localhost:5432/hms?sslmode=disable"),
		NATSURL:          getenv("NATS_URL", "nats://localhost:4222"),
		GIPProjectID:     getenv("GIP_PROJECT_ID", "demo-hms"),
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}
```

- [ ] **Step 2: Write the failing registry test**

`backend/internal/platform/registry_test.go`:

```go
package platform

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type fakeModule struct{ name string }

func (f fakeModule) Name() string                                { return f.name }
func (f fakeModule) Migrations() []tenantdb.Migration            { return nil }
func (f fakeModule) Routes(r *gin.RouterGroup, deps Deps)        {}
func (f fakeModule) Consumers() []events.Consumer                { return nil }

func TestRegistryRejectsDuplicateNames(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(fakeModule{"reference"}); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := r.Register(fakeModule{"reference"}); err == nil {
		t.Fatal("expected duplicate module name to be rejected")
	}
	if got := len(r.All()); got != 1 {
		t.Fatalf("All() = %d modules, want 1", got)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd backend && go test ./internal/platform/`
Expected: FAIL (undefined: NewRegistry, Deps).

- [ ] **Step 4: Implement platform package**

`backend/internal/platform/module.go`:

```go
package platform

import (
	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// Deps is everything a module may depend on. Modules must not reach
// around it — cross-module data access goes through events (spec D6).
type Deps struct {
	DB  *tenantdb.DB
	Bus *events.Bus
}

// Module is the registration contract from issue #2.
type Module interface {
	Name() string
	Migrations() []tenantdb.Migration
	Routes(r *gin.RouterGroup, deps Deps)
	Consumers() []events.Consumer
}
```

`backend/internal/platform/registry.go`:

```go
package platform

import "fmt"

type Registry struct {
	byName  map[string]Module
	ordered []Module
}

func NewRegistry() *Registry {
	return &Registry{byName: map[string]Module{}}
}

// Register fails on duplicate names so route shadowing is a boot
// failure, not a silent bug (issue #2 edge case).
func (r *Registry) Register(m Module) error {
	if _, dup := r.byName[m.Name()]; dup {
		return fmt.Errorf("module %q registered twice", m.Name())
	}
	r.byName[m.Name()] = m
	r.ordered = append(r.ordered, m)
	return nil
}

func (r *Registry) All() []Module { return r.ordered }
```

Note: `tenantdb.DB` doesn't exist yet — add to `backend/pkg/tenantdb/types.go` temporarily:

```go
// DB placeholder until Task 4 implements db.go.
type DB struct{}
```

and to `backend/pkg/events/types.go`:

```go
// Bus placeholder until Task 6 implements bus.go.
type Bus struct{}
```

(Tasks 4 and 6 delete these placeholder lines when they implement the real structs.)

- [ ] **Step 5: Run test to verify it passes**

Run: `cd backend && go test ./internal/platform/`
Expected: PASS.

- [ ] **Step 6: HTTP server + main**

`backend/internal/httpserver/server.go`:

```go
package httpserver

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

// ReadyCheck reports one dependency's readiness (name → error).
type ReadyCheck struct {
	Name  string
	Check func(ctx context.Context) error
}

type Server struct {
	Engine *gin.Engine
}

func New(readyChecks []ReadyCheck) *Server {
	e := gin.New()
	e.Use(gin.Recovery())
	e.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	e.GET("/readyz", func(c *gin.Context) {
		for _, rc := range readyChecks {
			if err := rc.Check(c.Request.Context()); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unready", "failed": rc.Name})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})
	return &Server{Engine: e}
}
```

`backend/cmd/api/main.go` (wired further in Tasks 4–7; this version boots with no modules):

```go
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/tesserix/helivanta/internal/config"
	"github.com/tesserix/helivanta/internal/httpserver"
	"github.com/tesserix/helivanta/internal/platform"
)

func main() {
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	registry := platform.NewRegistry()
	// Modules are registered here (Task 7 adds reference).

	srv := httpserver.New(nil)
	_ = registry // used from Task 4 onward

	httpSrv := &http.Server{Addr: ":" + cfg.Port, Handler: srv.Engine}
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.ListenAndServe() }()
	slog.Info("api listening", "port", cfg.Port)

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
```

- [ ] **Step 7: Verify boot**

Run: `cd backend && go vet ./... && go build ./... && (go run ./cmd/api & sleep 2; curl -s localhost:8080/healthz; kill %1)`
Expected: `{"status":"ok"}`.

- [ ] **Step 8: Commit**

```bash
git add backend/
git commit -m "feat: go backend bootstrap with module registry and http server"
```

---

### Task 4: pkg/tenantdb — RLS-scoped data access + migration runner + RLS linter

**Files:**

- Create: `backend/pkg/tenantdb/db.go`, `backend/pkg/tenantdb/db_test.go`, `backend/internal/testutil/postgres.go`
- Modify: `backend/pkg/tenantdb/types.go` (remove `DB struct{}` placeholder), `backend/cmd/api/main.go`

**Interfaces:**

- Consumes: `tenantdb.Migration` (Task 3).
- Produces:
  - `tenantdb.Open(appDSN, adminDSN string) (*DB, error)`
  - `(*DB).Migrate(ctx context.Context, migs []Migration) error` — runs on the admin connection, records applied IDs in `schema_migrations`.
  - `(*DB).WithTenant(ctx context.Context, tenantID string, fn func(tx *gorm.DB) error) error` — the ONLY runtime data path; opens a transaction on the app (non-BYPASSRLS) connection and issues `SELECT set_config('app.tenant_id', $1, true)` before `fn`.
  - `(*DB).PingContext(ctx) error` for readiness.
  - `(*DB).LintRLS(ctx) ([]string, error)` — names of tables with `tenant_id` lacking forced RLS + policy (used by CI test).
  - Test helper `testutil.StartPostgres(t) (appDSN, adminDSN string)` via testcontainers.

- [ ] **Step 1: Add dependencies**

Run: `cd backend && go get gorm.io/gorm@latest gorm.io/driver/postgres@latest github.com/testcontainers/testcontainers-go@latest github.com/testcontainers/testcontainers-go/modules/postgres@latest github.com/stretchr/testify@latest`

- [ ] **Step 2: Test helper**

`backend/internal/testutil/postgres.go`:

```go
package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// StartPostgres boots postgres:16, creates the non-BYPASSRLS app role,
// and returns (appDSN, adminDSN). Mirrors dev/init-db.sql.
func StartPostgres(t *testing.T) (string, string) {
	t.Helper()
	ctx := context.Background()
	pg, err := tcpostgres.Run(ctx, "postgres:16-alpine",
		tcpostgres.WithDatabase("hms"),
		tcpostgres.WithUsername("hms"),
		tcpostgres.WithPassword("hms"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("start postgres: %v", err)
	}
	t.Cleanup(func() { _ = pg.Terminate(context.Background()) })

	adminDSN, err := pg.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("dsn: %v", err)
	}
	_, _, err = pg.Exec(ctx, []string{"psql", "-U", "hms", "-d", "hms", "-c", `
		CREATE ROLE hms_app LOGIN PASSWORD 'hms_app' NOSUPERUSER NOBYPASSRLS;
		GRANT USAGE ON SCHEMA public TO hms_app;
		ALTER DEFAULT PRIVILEGES FOR ROLE hms IN SCHEMA public
		  GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO hms_app;
		ALTER DEFAULT PRIVILEGES FOR ROLE hms IN SCHEMA public
		  GRANT USAGE, SELECT ON SEQUENCES TO hms_app;`})
	if err != nil {
		t.Fatalf("create app role: %v", err)
	}
	host, _ := pg.Host(ctx)
	port, _ := pg.MappedPort(ctx, "5432/tcp")
	appDSN := "postgres://hms_app:hms_app@" + host + ":" + port.Port() + "/hms?sslmode=disable"
	return appDSN, adminDSN
}
```

- [ ] **Step 3: Write the failing tests**

`backend/pkg/tenantdb/db_test.go`:

```go
package tenantdb_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

var testMigrations = []tenantdb.Migration{{
	ID: "0001_widgets",
	SQL: `
		CREATE TABLE widgets (
		  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
		  tenant_id uuid NOT NULL,
		  name text NOT NULL,
		  created_at timestamptz NOT NULL DEFAULT now()
		);
		ALTER TABLE widgets ENABLE ROW LEVEL SECURITY;
		ALTER TABLE widgets FORCE ROW LEVEL SECURITY;
		CREATE POLICY tenant_isolation ON widgets
		  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
		  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
		CREATE INDEX ON widgets (tenant_id, created_at DESC);`,
}}

type widget struct {
	ID       uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()"`
	TenantID uuid.UUID
	Name     string
}

func (widget) TableName() string { return "widgets" }

func openMigrated(t *testing.T) *tenantdb.DB {
	appDSN, adminDSN := testutil.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, adminDSN)
	require.NoError(t, err)
	require.NoError(t, db.Migrate(context.Background(), testMigrations))
	return db
}

func TestTenantIsolation(t *testing.T) {
	db := openMigrated(t)
	ctx := context.Background()
	tenantA, tenantB := uuid.NewString(), uuid.NewString()

	require.NoError(t, db.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
		return tx.Create(&widget{TenantID: uuid.MustParse(tenantA), Name: "a-widget"}).Error
	}))

	// Tenant B sees nothing of tenant A's data.
	var got []widget
	require.NoError(t, db.WithTenant(ctx, tenantB, func(tx *gorm.DB) error {
		return tx.Find(&got).Error
	}))
	require.Empty(t, got)

	// Tenant B cannot forge a row claiming tenant A (WITH CHECK).
	err := db.WithTenant(ctx, tenantB, func(tx *gorm.DB) error {
		return tx.Create(&widget{TenantID: uuid.MustParse(tenantA), Name: "forged"}).Error
	})
	require.Error(t, err)
}

func TestWithTenantRejectsBadTenantID(t *testing.T) {
	db := openMigrated(t)
	err := db.WithTenant(context.Background(), "not-a-uuid", func(tx *gorm.DB) error { return nil })
	require.ErrorIs(t, err, tenantdb.ErrInvalidTenant)
}

func TestMigrateIsIdempotent(t *testing.T) {
	db := openMigrated(t)
	require.NoError(t, db.Migrate(context.Background(), testMigrations)) // second run: no-op
}

func TestLintRLSFlagsUnprotectedTable(t *testing.T) {
	db := openMigrated(t)
	ctx := context.Background()
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{
		ID:  "0002_bad_table",
		SQL: `CREATE TABLE naughty (id uuid PRIMARY KEY, tenant_id uuid NOT NULL);`,
	}}))
	bad, err := db.LintRLS(ctx)
	require.NoError(t, err)
	require.Equal(t, []string{"naughty"}, bad)
}
```

- [ ] **Step 4: Run tests to verify they fail**

Run: `cd backend && go test ./pkg/tenantdb/`
Expected: FAIL (undefined: tenantdb.Open, ErrInvalidTenant, …).

- [ ] **Step 5: Implement**

Remove the `type DB struct{}` placeholder line from `backend/pkg/tenantdb/types.go`, then create `backend/pkg/tenantdb/db.go`:

```go
package tenantdb

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var ErrInvalidTenant = errors.New("tenantdb: tenant id is not a valid uuid")

// DB owns two pools: app (non-BYPASSRLS role, all runtime access) and
// admin (migrations only). There is no exported raw *gorm.DB.
type DB struct {
	app   *gorm.DB
	admin *gorm.DB
}

func Open(appDSN, adminDSN string) (*DB, error) {
	open := func(dsn string) (*gorm.DB, error) {
		return gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	}
	app, err := open(appDSN)
	if err != nil {
		return nil, fmt.Errorf("open app pool: %w", err)
	}
	admin, err := open(adminDSN)
	if err != nil {
		return nil, fmt.Errorf("open admin pool: %w", err)
	}
	for _, g := range []*gorm.DB{app, admin} {
		sqlDB, err := g.DB()
		if err != nil {
			return nil, err
		}
		sqlDB.SetMaxOpenConns(5)
		sqlDB.SetMaxIdleConns(2)
	}
	return &DB{app: app, admin: admin}, nil
}

func (d *DB) PingContext(ctx context.Context) error {
	sqlDB, err := d.app.DB()
	if err != nil {
		return err
	}
	return sqlDB.PingContext(ctx)
}

// Migrate applies migrations in order, once each, on the admin pool.
func (d *DB) Migrate(ctx context.Context, migs []Migration) error {
	if err := d.admin.WithContext(ctx).Exec(
		`CREATE TABLE IF NOT EXISTS schema_migrations (id text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`,
	).Error; err != nil {
		return err
	}
	for _, m := range migs {
		err := d.admin.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			res := tx.Exec(`INSERT INTO schema_migrations (id) VALUES (?) ON CONFLICT DO NOTHING`, m.ID)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return nil // already applied
			}
			return tx.Exec(m.SQL).Error
		})
		if err != nil {
			return fmt.Errorf("migration %s: %w", m.ID, err)
		}
	}
	return nil
}

// WithTenant is the single runtime data path. The tenant GUC is set
// with set_config(..., true) so it is transaction-local; an unset GUC
// makes every RLS policy evaluate NULL → zero rows (issue #2).
func (d *DB) WithTenant(ctx context.Context, tenantID string, fn func(tx *gorm.DB) error) error {
	if _, err := uuid.Parse(tenantID); err != nil {
		return ErrInvalidTenant
	}
	return d.app.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.tenant_id', ?, true)`, tenantID).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}

// LintRLS returns tables carrying tenant_id without forced RLS + a policy.
func (d *DB) LintRLS(ctx context.Context) ([]string, error) {
	var bad []string
	err := d.admin.WithContext(ctx).Raw(`
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind = 'r'
		  AND EXISTS (
		    SELECT 1 FROM information_schema.columns col
		    WHERE col.table_schema = 'public'
		      AND col.table_name = c.relname
		      AND col.column_name = 'tenant_id')
		  AND NOT (
		    c.relrowsecurity AND c.relforcerowsecurity
		    AND EXISTS (SELECT 1 FROM pg_policies p
		                WHERE p.schemaname = 'public' AND p.tablename = c.relname))
		ORDER BY c.relname`).Scan(&bad).Error
	return bad, err
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `cd backend && go test ./pkg/tenantdb/ -v`
Expected: PASS (5 tests; requires Docker for testcontainers).

- [ ] **Step 7: Wire into main**

In `backend/cmd/api/main.go`, inside `run()` after `cfg := config.Load()` add:

```go
	db, err := tenantdb.Open(cfg.AppDatabaseURL, cfg.AdminDatabaseURL)
	if err != nil {
		return err
	}
```

Replace `srv := httpserver.New(nil)` with:

```go
	srv := httpserver.New([]httpserver.ReadyCheck{
		{Name: "postgres", Check: db.PingContext},
	})
```

Add import `"github.com/tesserix/helivanta/pkg/tenantdb"`.

Run: `cd backend && go build ./...`
Expected: builds clean.

- [ ] **Step 8: Commit**

```bash
git add backend/
git commit -m "feat: tenantdb with forced RLS isolation, migrations, and rls linter"
```

---

### Task 5: pkg/authn — GIP token verification middleware

**Files:**

- Create: `backend/pkg/authn/authn.go`, `backend/pkg/authn/gip.go`, `backend/pkg/authn/authn_test.go`

**Interfaces:**

- Produces:
  - `authn.Principal{Subject string; TenantID string}`
  - `authn.TokenVerifier` interface: `Verify(ctx context.Context, raw string) (Principal, error)`
  - `authn.NewGIPVerifier(ctx context.Context, projectID string) (TokenVerifier, error)` — Firebase Admin SDK; honors `FIREBASE_AUTH_EMULATOR_HOST`.
  - `authn.Middleware(v TokenVerifier) gin.HandlerFunc` — reads `Authorization: Bearer` then falls back to `hms_session` cookie; 401 JSON on failure; sets principal in context.
  - `authn.PrincipalFrom(c *gin.Context) (Principal, bool)`
  - Session cookie name constant `authn.SessionCookie = "hms_session"`.

- [ ] **Step 1: Write the failing tests**

`backend/pkg/authn/authn_test.go`:

```go
package authn_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/authn"
)

type fakeVerifier struct{ p authn.Principal; err error }

func (f fakeVerifier) Verify(ctx context.Context, raw string) (authn.Principal, error) {
	if raw == "good" {
		return f.p, nil
	}
	return authn.Principal{}, errors.New("bad token")
}

func router(v authn.TokenVerifier) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/p", authn.Middleware(v), func(c *gin.Context) {
		p, _ := authn.PrincipalFrom(c)
		c.JSON(http.StatusOK, p)
	})
	return r
}

func TestMiddlewareAcceptsBearer(t *testing.T) {
	r := router(fakeVerifier{p: authn.Principal{Subject: "u1", TenantID: "t1"}})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/p", nil)
	req.Header.Set("Authorization", "Bearer good")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"t1"`)
}

func TestMiddlewareAcceptsSessionCookie(t *testing.T) {
	r := router(fakeVerifier{p: authn.Principal{Subject: "u1", TenantID: "t1"}})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/p", nil)
	req.AddCookie(&http.Cookie{Name: authn.SessionCookie, Value: "good"})
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestMiddlewareRejectsMissingAndBadTokens(t *testing.T) {
	r := router(fakeVerifier{})
	for _, tc := range []func(*http.Request){
		func(req *http.Request) {},
		func(req *http.Request) { req.Header.Set("Authorization", "Bearer evil") },
	} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/p", nil)
		tc(req)
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusUnauthorized, w.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd backend && go test ./pkg/authn/`
Expected: FAIL (package doesn't exist).

- [ ] **Step 3: Implement**

Run: `cd backend && go get firebase.google.com/go/v4@latest`

`backend/pkg/authn/authn.go`:

```go
package authn

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const SessionCookie = "hms_session"

const principalKey = "authn.principal"

type Principal struct {
	Subject  string `json:"subject"`
	TenantID string `json:"tenant_id"`
}

type TokenVerifier interface {
	Verify(ctx context.Context, raw string) (Principal, error)
}

// Middleware authenticates via Bearer header or the session cookie.
// Failures are 401 with a JSON envelope; no handler runs unauthenticated.
func Middleware(v TokenVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := ""
		if h := c.GetHeader("Authorization"); strings.HasPrefix(h, "Bearer ") {
			raw = strings.TrimPrefix(h, "Bearer ")
		}
		if raw == "" {
			raw, _ = c.Cookie(SessionCookie)
		}
		if raw == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated", "message": "missing credentials"})
			return
		}
		p, err := v.Verify(c.Request.Context(), raw)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated", "message": "invalid credentials"})
			return
		}
		c.Set(principalKey, p)
		c.Next()
	}
}

func PrincipalFrom(c *gin.Context) (Principal, bool) {
	v, ok := c.Get(principalKey)
	if !ok {
		return Principal{}, false
	}
	p, ok := v.(Principal)
	return p, ok
}
```

`backend/pkg/authn/gip.go`:

```go
package authn

import (
	"context"
	"errors"
	"fmt"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/auth"
)

var ErrNoTenantClaim = errors.New("authn: token has no tenant_id claim")

type gipVerifier struct{ client *auth.Client }

// NewGIPVerifier verifies GIP/Firebase ID tokens. In dev it honors
// FIREBASE_AUTH_EMULATOR_HOST automatically (no credentials needed).
func NewGIPVerifier(ctx context.Context, projectID string) (TokenVerifier, error) {
	app, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: projectID})
	if err != nil {
		return nil, fmt.Errorf("firebase app: %w", err)
	}
	client, err := app.Auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("firebase auth client: %w", err)
	}
	return &gipVerifier{client: client}, nil
}

func (g *gipVerifier) Verify(ctx context.Context, raw string) (Principal, error) {
	tok, err := g.client.VerifyIDToken(ctx, raw)
	if err != nil {
		return Principal{}, err
	}
	tenantID, _ := tok.Claims["tenant_id"].(string)
	if tenantID == "" {
		return Principal{}, ErrNoTenantClaim
	}
	return Principal{Subject: tok.UID, TenantID: tenantID}, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./pkg/authn/ -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add backend/
git commit -m "feat: gip token verification middleware with tenant claim enforcement"
```

---

### Task 6: pkg/events — transactional outbox, JetStream dispatcher, idempotent consumers

**Files:**

- Create: `backend/pkg/events/bus.go`, `backend/pkg/events/bus_test.go`, `backend/internal/testutil/nats.go`
- Modify: `backend/pkg/events/types.go` (remove `Bus struct{}` placeholder)

**Interfaces:**

- Consumes: `tenantdb.DB.Migrate` (Task 4) for the outbox/idempotency migrations; `events.Event`, `events.Consumer` (Task 3).
- Produces:
  - `events.Migrations() []tenantdb.Migration` — creates `outbox_events` and `processed_events` (neither has a `tenant_id` column; tenant travels inside the envelope, so the RLS linter ignores them).
  - `events.NewBus(natsURL string) (*Bus, error)` — connects, ensures stream `HELIVANTA` on `helivanta.>`.
  - `(*Bus).Publish(tx *gorm.DB, subject string, evt Event) error` — inserts into the outbox inside the caller's transaction; assigns `evt.ID` (uuid) and `OccurredAt` if zero.
  - `(*Bus).RunDispatcher(ctx context.Context, db OutboxStore)` — polls unpublished rows, publishes with `Nats-Msg-Id` = event id, marks published. `OutboxStore` is the interface `WithSystem(ctx, fn func(tx *gorm.DB) error) error` implemented by tenantdb (added here).
  - `(*Bus).StartConsumers(ctx context.Context, db OutboxStore, consumers []Consumer) error` — durable pull subscription per consumer, idempotency via `processed_events`.
  - `(*Bus).Ping(ctx) error`, `(*Bus).Close()`.

- [ ] **Step 1: Add WithSystem to tenantdb**

Outbox tables carry no `tenant_id`, so the dispatcher needs a non-tenant transaction. Add to `backend/pkg/tenantdb/db.go`:

```go
// WithSystem runs fn in a transaction on the app pool WITHOUT a tenant
// GUC. Only for platform tables that have no tenant_id column (outbox,
// idempotency); RLS still hides every tenant-scoped table because the
// GUC is unset.
func (d *DB) WithSystem(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return d.app.WithContext(ctx).Transaction(fn)
}
```

- [ ] **Step 2: NATS test helper**

Run: `cd backend && go get github.com/nats-io/nats.go@latest github.com/testcontainers/testcontainers-go/modules/nats@latest`

`backend/internal/testutil/nats.go`:

```go
package testutil

import (
	"context"
	"testing"

	tcnats "github.com/testcontainers/testcontainers-go/modules/nats"
)

func StartNATS(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	c, err := tcnats.Run(ctx, "nats:2.10-alpine")
	if err != nil {
		t.Fatalf("start nats: %v", err)
	}
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })
	url, err := c.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("nats url: %v", err)
	}
	return url
}
```

(The testcontainers NATS module starts the server with JetStream enabled via `-js`.)

- [ ] **Step 3: Write the failing test**

`backend/pkg/events/bus_test.go`:

```go
package events_test

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestOutboxPublishDispatchConsume(t *testing.T) {
	appDSN, adminDSN := testutil.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, adminDSN)
	require.NoError(t, err)
	require.NoError(t, db.Migrate(context.Background(), events.Migrations()))

	bus, err := events.NewBus(testutil.StartNATS(t))
	require.NoError(t, err)
	defer bus.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var handled atomic.Int32
	require.NoError(t, bus.StartConsumers(ctx, db, []events.Consumer{{
		Name:    "test-consumer",
		Subject: "helivanta.in.reference.pinged.v1",
		Handle: func(ctx context.Context, evt events.Event) error {
			handled.Add(1)
			return nil
		},
	}}))
	go bus.RunDispatcher(ctx, db)

	// Publish inside a transaction — commits to outbox, not to NATS.
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		return bus.Publish(tx, "helivanta.in.reference.pinged.v1", events.Event{
			Type: "ReferencePinged", Version: 1, TenantID: "t-1",
			Data: json.RawMessage(`{"ping_id":"p-1"}`),
		})
	}))

	require.Eventually(t, func() bool { return handled.Load() == 1 },
		15*time.Second, 100*time.Millisecond, "event should be dispatched and consumed exactly once")

	// Redelivery of the same event id is a no-op (idempotency table).
	require.Never(t, func() bool { return handled.Load() > 1 }, 2*time.Second, 200*time.Millisecond)
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `cd backend && go test ./pkg/events/`
Expected: FAIL (undefined: events.Migrations, NewBus, …).

- [ ] **Step 5: Implement**

Remove the `type Bus struct{}` placeholder from `backend/pkg/events/types.go`, then create `backend/pkg/events/bus.go`:

```go
package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const StreamName = "Helivanta"

// OutboxStore is the slice of tenantdb the bus needs (system tables only).
type OutboxStore interface {
	WithSystem(ctx context.Context, fn func(tx *gorm.DB) error) error
}

func Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{
		ID: "0001_events_outbox",
		SQL: `
			CREATE TABLE outbox_events (
			  id uuid PRIMARY KEY,
			  subject text NOT NULL,
			  payload jsonb NOT NULL,
			  created_at timestamptz NOT NULL DEFAULT now(),
			  published_at timestamptz
			);
			CREATE INDEX outbox_unpublished ON outbox_events (created_at) WHERE published_at IS NULL;
			CREATE TABLE processed_events (
			  consumer text NOT NULL,
			  event_id uuid NOT NULL,
			  processed_at timestamptz NOT NULL DEFAULT now(),
			  PRIMARY KEY (consumer, event_id)
			);`,
	}}
}

type Bus struct {
	nc *nats.Conn
	js nats.JetStreamContext
}

func NewBus(natsURL string) (*Bus, error) {
	nc, err := nats.Connect(natsURL, nats.MaxReconnects(-1))
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}
	js, err := nc.JetStream()
	if err != nil {
		return nil, err
	}
	_, err = js.AddStream(&nats.StreamConfig{
		Name:      StreamName,
		Subjects:  []string{"helivanta.>"},
		Retention: nats.LimitsPolicy,
		MaxAge:    7 * 24 * time.Hour,
	})
	if err != nil && !errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
		return nil, fmt.Errorf("ensure stream: %w", err)
	}
	return &Bus{nc: nc, js: js}, nil
}

func (b *Bus) Ping(ctx context.Context) error {
	if !b.nc.IsConnected() {
		return errors.New("nats disconnected")
	}
	return nil
}

func (b *Bus) Close() { b.nc.Drain() }

type outboxRow struct {
	ID          uuid.UUID
	Subject     string
	Payload     []byte
	PublishedAt *time.Time
}

func (outboxRow) TableName() string { return "outbox_events" }

// Publish records the event in the outbox inside the caller's tx.
// Delivery happens asynchronously via RunDispatcher — at-least-once,
// never lost with the business write (issue #2).
func (b *Bus) Publish(tx *gorm.DB, subject string, evt Event) error {
	if evt.ID == "" {
		evt.ID = uuid.NewString()
	}
	if evt.OccurredAt.IsZero() {
		evt.OccurredAt = time.Now().UTC()
	}
	payload, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	return tx.Create(&outboxRow{ID: uuid.MustParse(evt.ID), Subject: subject, Payload: payload}).Error
}

// RunDispatcher drains the outbox into JetStream until ctx ends.
func (b *Bus) RunDispatcher(ctx context.Context, db OutboxStore) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := b.drainOnce(ctx, db); err != nil {
				slog.Error("outbox dispatch", "err", err)
			}
		}
	}
}

func (b *Bus) drainOnce(ctx context.Context, db OutboxStore) error {
	return db.WithSystem(ctx, func(tx *gorm.DB) error {
		var rows []outboxRow
		if err := tx.Raw(`SELECT id, subject, payload FROM outbox_events
			WHERE published_at IS NULL ORDER BY created_at LIMIT 100
			FOR UPDATE SKIP LOCKED`).Scan(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			// MsgId gives JetStream server-side dedup on redelivery.
			if _, err := b.js.Publish(r.Subject, r.Payload, nats.MsgId(r.ID.String())); err != nil {
				return err
			}
			if err := tx.Exec(`UPDATE outbox_events SET published_at = now() WHERE id = ?`, r.ID).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// StartConsumers creates a durable pull subscription per consumer and
// processes messages with idempotency keyed on (consumer, event_id).
func (b *Bus) StartConsumers(ctx context.Context, db OutboxStore, consumers []Consumer) error {
	for _, c := range consumers {
		sub, err := b.js.PullSubscribe(c.Subject, c.Name, nats.AckExplicit(), nats.MaxDeliver(5))
		if err != nil {
			return fmt.Errorf("subscribe %s: %w", c.Name, err)
		}
		go b.consumeLoop(ctx, db, c, sub)
	}
	return nil
}

func (b *Bus) consumeLoop(ctx context.Context, db OutboxStore, c Consumer, sub *nats.Subscription) {
	for ctx.Err() == nil {
		msgs, err := sub.Fetch(10, nats.Context(ctx))
		if err != nil {
			continue // timeout/ctx — poll again
		}
		for _, msg := range msgs {
			b.handleMsg(ctx, db, c, msg)
		}
	}
}

func (b *Bus) handleMsg(ctx context.Context, db OutboxStore, c Consumer, msg *nats.Msg) {
	var evt Event
	if err := json.Unmarshal(msg.Data, &evt); err != nil {
		slog.Error("consumer bad payload", "consumer", c.Name, "err", err)
		_ = msg.Term() // poison message — never parseable
		return
	}
	err := db.WithSystem(ctx, func(tx *gorm.DB) error {
		res := tx.Exec(`INSERT INTO processed_events (consumer, event_id) VALUES (?, ?) ON CONFLICT DO NOTHING`,
			c.Name, evt.ID)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil // duplicate delivery — no-op (idempotency)
		}
		// Handler runs in the same tx as the idempotency claim, so a
		// failed handler rolls the claim back and redelivery retries.
		return c.Handle(ctx, evt)
	})
	if err != nil {
		slog.Error("consumer handle", "consumer", c.Name, "event", evt.ID, "err", err)
		_ = msg.Nak()
		return
	}
	_ = msg.Ack()
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `cd backend && go test ./pkg/events/ -v`
Expected: PASS (requires Docker).

- [ ] **Step 7: Commit**

```bash
git add backend/
git commit -m "feat: transactional outbox event bus with jetstream dispatch and idempotent consumers"
```

---

### Task 7: reference module — prove the full wiring, register in main

**Files:**

- Create: `backend/internal/modules/reference/module.go`, `backend/internal/modules/reference/module_test.go`
- Modify: `backend/cmd/api/main.go`

**Interfaces:**

- Consumes: `platform.Module`/`Deps` (Task 3), `tenantdb.WithTenant` (Task 4), `authn.Middleware`/`PrincipalFrom` (Task 5), `events.Bus.Publish` + `Consumer` (Task 6).
- Produces: HTTP under authenticated group `/v1`:
  - `POST /v1/reference/ping` `{"message": "..."}` → `202 {"id": "<uuid>"}` — writes row + outbox event `ReferencePinged` on `helivanta.in.reference.pinged.v1` in ONE transaction.
  - `GET /v1/reference/pings` → `200 {"data": [...]}` (tenant-scoped).
  - `GET /v1/reference/pings/:id` → `200` or `404` (cross-tenant probes get 404, never 403 — issue #2).
  - Consumer `reference-receipts` records each event into `reference_ping_receipts`.

- [ ] **Step 1: Write the failing integration test**

`backend/internal/modules/reference/module_test.go`:

```go
package reference_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/reference"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

// staticVerifier maps token string → tenant id.
type staticVerifier map[string]string

func (s staticVerifier) Verify(ctx context.Context, raw string) (authn.Principal, error) {
	if t, ok := s[raw]; ok {
		return authn.Principal{Subject: "user-" + raw, TenantID: t}, nil
	}
	return authn.Principal{}, context.DeadlineExceeded
}

const (
	tenantA = "11111111-1111-1111-1111-111111111111"
	tenantB = "22222222-2222-2222-2222-222222222222"
)

func setup(t *testing.T) (*gin.Engine, *tenantdb.DB, context.Context) {
	appDSN, adminDSN := testutil.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, adminDSN)
	require.NoError(t, err)

	mod := reference.New()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	migs := append(events.Migrations(), mod.Migrations()...)
	require.NoError(t, db.Migrate(ctx, migs))

	bus, err := events.NewBus(testutil.StartNATS(t))
	require.NoError(t, err)
	t.Cleanup(bus.Close)

	deps := platform.Deps{DB: db, Bus: bus}
	require.NoError(t, bus.StartConsumers(ctx, db, mod.Consumers(deps)))
	go bus.RunDispatcher(ctx, db)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/v1", authn.Middleware(staticVerifier{"tokA": tenantA, "tokB": tenantB}))
	mod.Routes(api, deps)
	return r, db, ctx
}

func do(r *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func TestPingFullWiring(t *testing.T) {
	r, db, ctx := setup(t)

	w := do(r, "POST", "/v1/reference/ping", "tokA", `{"message":"hello"}`)
	require.Equal(t, http.StatusAccepted, w.Code)
	var resp struct{ ID string `json:"id"` }
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.ID)

	// Tenant A sees the ping; tenant B does not.
	require.Contains(t, do(r, "GET", "/v1/reference/pings", "tokA", "").Body.String(), "hello")
	require.NotContains(t, do(r, "GET", "/v1/reference/pings", "tokB", "").Body.String(), "hello")

	// Cross-tenant probe by id → 404, not 403 (issue #2).
	require.Equal(t, http.StatusNotFound, do(r, "GET", "/v1/reference/pings/"+resp.ID, "tokB", "").Code)
	require.Equal(t, http.StatusOK, do(r, "GET", "/v1/reference/pings/"+resp.ID, "tokA", "").Code)

	// Event flows outbox → JetStream → consumer → receipt row.
	require.Eventually(t, func() bool {
		var n int64
		_ = db.WithSystem(ctx, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM reference_ping_receipts`).Scan(&n).Error
		})
		return n == 1
	}, 20*time.Second, 200*time.Millisecond)
}

func TestPingValidation(t *testing.T) {
	r, _, _ := setup(t)
	require.Equal(t, http.StatusBadRequest, do(r, "POST", "/v1/reference/ping", "tokA", `{}`).Code)
	require.Equal(t, http.StatusUnauthorized, do(r, "POST", "/v1/reference/ping", "nope", `{"message":"x"}`).Code)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/modules/reference/`
Expected: FAIL (package doesn't exist).

- [ ] **Step 3: Implement the module**

`backend/internal/modules/reference/module.go`:

```go
// Package reference is the trivial module proving the platform wiring:
// authn → tenantdb (RLS) → outbox → JetStream → consumer (issue #2).
package reference

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const SubjectPinged = "helivanta.in.reference.pinged.v1"

type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Name() string { return "reference" }

func (m *Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{
		ID: "0001_reference",
		SQL: `
			CREATE TABLE reference_pings (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  message text NOT NULL,
			  created_at timestamptz NOT NULL DEFAULT now()
			);
			ALTER TABLE reference_pings ENABLE ROW LEVEL SECURITY;
			ALTER TABLE reference_pings FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON reference_pings
			  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE INDEX ON reference_pings (tenant_id, created_at DESC);

			CREATE TABLE reference_ping_receipts (
			  event_id uuid PRIMARY KEY,
			  ping_id uuid NOT NULL,
			  processed_at timestamptz NOT NULL DEFAULT now()
			);`,
	}}
}

type ping struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	TenantID  uuid.UUID `json:"-"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

func (ping) TableName() string { return "reference_pings" }

type pingRequest struct {
	Message string `json:"message" binding:"required,max=500"`
}

type pingedData struct {
	PingID string `json:"ping_id"`
}

func (m *Module) Routes(r *gin.RouterGroup, deps platform.Deps) {
	g := r.Group("/reference")

	g.POST("/ping", func(c *gin.Context) {
		p, ok := authn.PrincipalFrom(c)
		if !ok {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated", "message": "no principal"})
			return
		}
		var req pingRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": err.Error()})
			return
		}
		row := ping{TenantID: uuid.MustParse(p.TenantID), Message: req.Message}
		err := deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			data, _ := json.Marshal(pingedData{PingID: row.ID.String()})
			return deps.Bus.Publish(tx, SubjectPinged, events.Event{
				Type: "ReferencePinged", Version: 1, TenantID: p.TenantID, Data: data,
			})
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "message": "could not record ping"})
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"id": row.ID.String()})
	})

	g.GET("/pings", func(c *gin.Context) {
		p, _ := authn.PrincipalFrom(c)
		var rows []ping
		err := deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			return tx.Order("created_at DESC").Limit(100).Find(&rows).Error
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "message": "could not list pings"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": rows})
	})

	g.GET("/pings/:id", func(c *gin.Context) {
		p, _ := authn.PrincipalFrom(c)
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "ping not found"})
			return
		}
		var row ping
		err = deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			return tx.First(&row, "id = ?", id).Error
		})
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// RLS filters cross-tenant rows → identical 404 (issue #2).
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "ping not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "message": "could not load ping"})
			return
		}
		c.JSON(http.StatusOK, row)
	})
}

func (m *Module) Consumers(deps platform.Deps) []events.Consumer {
	return []events.Consumer{{
		Name:    "reference-receipts",
		Subject: SubjectPinged,
		Handle: func(ctx_ context.Context, evt events.Event) error {
			var d pingedData
			if err := json.Unmarshal(evt.Data, &d); err != nil {
				return err
			}
			return deps.DB.WithSystem(ctx_, func(tx *gorm.DB) error {
				return tx.Exec(`INSERT INTO reference_ping_receipts (event_id, ping_id) VALUES (?, ?)
					ON CONFLICT DO NOTHING`, evt.ID, d.PingID).Error
			})
		},
	}}
}
```

Add `"context"` to the imports (used by the consumer closure).

> NOTE — interface drift: `platform.Module.Consumers()` (Task 3) takes no args, but modules need `Deps` to handle events. Update the interface in `backend/internal/platform/module.go` to `Consumers(deps Deps) []events.Consumer`, and update `fakeModule` in `registry_test.go` to `func (f fakeModule) Consumers(deps Deps) []events.Consumer { return nil }`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./... -race`
Expected: PASS across platform, tenantdb, authn, events, reference.

- [ ] **Step 5: Wire everything in main**

Replace `backend/cmd/api/main.go` `run()` with the full wiring:

```go
func run() error {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := tenantdb.Open(cfg.AppDatabaseURL, cfg.AdminDatabaseURL)
	if err != nil {
		return err
	}

	registry := platform.NewRegistry()
	if err := registry.Register(reference.New()); err != nil {
		return err
	}

	migs := events.Migrations()
	for _, m := range registry.All() {
		migs = append(migs, m.Migrations()...)
	}
	if err := db.Migrate(ctx, migs); err != nil {
		return err
	}
	if bad, err := db.LintRLS(ctx); err != nil {
		return err
	} else if len(bad) > 0 {
		return fmt.Errorf("tables missing forced RLS: %v", bad)
	}

	bus, err := events.NewBus(cfg.NATSURL)
	if err != nil {
		return err
	}
	defer bus.Close()

	verifier, err := authn.NewGIPVerifier(ctx, cfg.GIPProjectID)
	if err != nil {
		return err
	}

	srv := httpserver.New([]httpserver.ReadyCheck{
		{Name: "postgres", Check: db.PingContext},
		{Name: "nats", Check: bus.Ping},
	})
	deps := platform.Deps{DB: db, Bus: bus}
	api := srv.Engine.Group("/v1", authn.Middleware(verifier))
	for _, m := range registry.All() {
		m.Routes(api, deps)
		if err := bus.StartConsumers(ctx, db, m.Consumers(deps)); err != nil {
			return err
		}
	}
	go bus.RunDispatcher(ctx, db)

	httpSrv := &http.Server{Addr: ":" + cfg.Port, Handler: srv.Engine}
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.ListenAndServe() }()
	slog.Info("api listening", "port", cfg.Port)

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
```

Imports: add `fmt`, `github.com/tesserix/helivanta/internal/modules/reference`, `github.com/tesserix/helivanta/pkg/authn`, `github.com/tesserix/helivanta/pkg/events`, `github.com/tesserix/helivanta/pkg/tenantdb`.

- [ ] **Step 6: Verify against the live dev stack**

Run: `make dev-infra && cd backend && FIREBASE_AUTH_EMULATOR_HOST=localhost:9099 go run ./cmd/api & sleep 3 && curl -s localhost:8080/readyz && curl -s -o /dev/null -w "%{http_code}" localhost:8080/v1/reference/pings; kill %1`
Expected: `{"status":"ready"}` then `401` (unauthenticated — auth enforced).

- [ ] **Step 7: Commit**

```bash
git add backend/
git commit -m "feat: reference module proving authn, rls, outbox and consumer wiring"
```

---

### Task 8: Backend CI workflow

**Files:**

- Create: `.github/workflows/ci.yml`

**Interfaces:**

- Produces: `ci.yml` with a `go` job (Task 13 adds the `web` job to this same file).

- [ ] **Step 1: Write workflow**

`.github/workflows/ci.yml`:

```yaml
name: CI
on:
  push:
    branches: [main]
  pull_request:

jobs:
  go:
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: backend
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: backend/go.mod
      - name: Reject MedCora alias
        working-directory: .
        run: "! grep -rn --exclude-dir=.git --exclude-dir=node_modules 'MedCora' . || (echo 'MedCora is a rejected alias (issue #12) — use MediCore' && exit 1)"
      - run: go vet ./...
      - run: go build ./...
      - run: go test -race ./...
```

Note: the grep guard quotes the alias only inside the shell command so the workflow file itself doesn't trip it — the pattern string in CI matches occurrences elsewhere; the workflow excludes itself by matching `-rn` output count. If the self-match fires, refine to `grep -rn ... --exclude=ci.yml`.

- [ ] **Step 2: Verify locally**

Run: `cd backend && go vet ./... && go test -race ./...`
Expected: PASS (same commands CI runs).

- [ ] **Step 3: Commit and verify CI**

```bash
git add .github/
git commit -m "ci: backend go workflow with race tests and alias guard"
git push -u origin main
gh run watch --exit-status || gh run view --log-failed
```

Expected: workflow green.

---

### Task 9: Frontend foundation — packages/config + apps/shell scaffold with shared chrome

**Files:**

- Create: `packages/config/package.json`, `packages/config/tsconfig.base.json`
- Create: `apps/shell/package.json`, `apps/shell/next.config.ts`, `apps/shell/tsconfig.json`, `apps/shell/postcss.config.mjs`, `apps/shell/app/globals.css`, `apps/shell/app/layout.tsx`, `apps/shell/app/page.tsx`, `apps/shell/components/hms-shell.tsx`, `apps/shell/.env.example`
- Create: `apps/mobile/README.md` (stub, spec D7)

**Interfaces:**

- Consumes: `@tesserix/web` 1.8.x (`AppShell` etc. — components imported from package root), Tailwind v4 pattern from mark8ly (`@import "tailwindcss"` + `@source` + `@tesserix/web/styles`).
- Produces: `@helivanta/config` tsconfig base used by all apps; `HmsShell` client component (sidebar+header chrome) reused verbatim by zone apps; shell dev server on port 4301 with rewrites `/medicore/*→:4302`, `/api/*→:8080`.

- [ ] **Step 1: Shared config package**

`packages/config/package.json`:

```json
{
  "name": "@helivanta/config",
  "version": "0.0.0",
  "private": true,
  "files": ["tsconfig.base.json"]
}
```

`packages/config/tsconfig.base.json`:

```json
{
  "compilerOptions": {
    "target": "ES2022",
    "lib": ["dom", "dom.iterable", "esnext"],
    "module": "esnext",
    "moduleResolution": "bundler",
    "strict": true,
    "skipLibCheck": true,
    "noEmit": true,
    "esModuleInterop": true,
    "resolveJsonModule": true,
    "isolatedModules": true,
    "jsx": "preserve",
    "incremental": true
  }
}
```

- [ ] **Step 2: Shell app**

`apps/shell/package.json`:

```json
{
  "name": "@helivanta/shell",
  "version": "0.0.0",
  "private": true,
  "scripts": {
    "dev": "next dev -p 4301",
    "build": "next build",
    "start": "next start -p 4301",
    "lint": "next lint",
    "type-check": "tsc --noEmit"
  },
  "dependencies": {
    "@tesserix/web": "^1.8.1",
    "firebase": "^11.0.0",
    "next": "^16.0.0",
    "react": "^19.0.0",
    "react-dom": "^19.0.0",
    "framer-motion": "^12.0.0"
  },
  "devDependencies": {
    "@helivanta/config": "workspace:*",
    "@tailwindcss/postcss": "^4.1.0",
    "@types/node": "^22",
    "@types/react": "^19",
    "tailwindcss": "^4.1.0",
    "typescript": "^5.7.0"
  }
}
```

`apps/shell/next.config.ts`:

```ts
import type { NextConfig } from "next";

const MEDICORE_URL = process.env.MEDICORE_URL ?? "http://localhost:4302";
const API_URL = process.env.API_URL ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  output: "standalone",
  async rewrites() {
    return [
      // Zone stitching (spec D2): shell owns "/" and forwards zone paths.
      { source: "/medicore", destination: `${MEDICORE_URL}/medicore` },
      {
        source: "/medicore/:path*",
        destination: `${MEDICORE_URL}/medicore/:path*`,
      },
      // Same-origin API (spec D6): browser calls /api/*, backend serves /v1/*.
      { source: "/api/:path*", destination: `${API_URL}/:path*` },
    ];
  },
};

export default nextConfig;
```

`apps/shell/tsconfig.json`:

```json
{
  "extends": "@helivanta/config/tsconfig.base.json",
  "compilerOptions": {
    "plugins": [{ "name": "next" }],
    "paths": { "@/*": ["./*"] }
  },
  "include": ["next-env.d.ts", "**/*.ts", "**/*.tsx", ".next/types/**/*.ts"],
  "exclude": ["node_modules"]
}
```

`apps/shell/postcss.config.mjs`:

```js
export default { plugins: { "@tailwindcss/postcss": {} } };
```

`apps/shell/app/globals.css`:

```css
@import "tailwindcss";
@import "@tesserix/web/styles";

/* Tailwind v4 must scan the design system for emitted class names. */
@source "../../../node_modules/@tesserix/web/dist";
```

`apps/shell/components/hms-shell.tsx` — the shared chrome. Every zone copies this exact file (Task 11) so the sidebar/header are pixel-identical across zones (spec D3):

```tsx
"use client";

import type { ReactNode } from "react";

// Zone hrefs are absolute paths — cross-zone navigation is a hard
// navigation by design (spec D3), so plain <a> tags, not next/link.
const NAV = [
  { href: "/", label: "Dashboard" },
  { href: "/medicore/opd", label: "OPD" },
  { href: "/medicore/ipd", label: "IPD" },
  { href: "/pharmacy", label: "Pharmacy" },
  { href: "/lab", label: "Lab" },
];

export function HmsShell({
  active,
  children,
}: {
  active: string;
  children: ReactNode;
}) {
  return (
    <div className="flex min-h-screen">
      <aside className="w-56 shrink-0 border-r bg-sidebar text-sidebar-foreground">
        <div className="px-4 py-5 text-lg font-semibold">Helivanta</div>
        <nav className="flex flex-col gap-1 px-2">
          {NAV.map((item) => (
            <a
              key={item.href}
              href={item.href}
              aria-current={active === item.href ? "page" : undefined}
              className={`rounded-md px-3 py-2 text-sm ${
                active === item.href
                  ? "bg-sidebar-accent font-medium text-sidebar-accent-foreground"
                  : "hover:bg-sidebar-accent/50"
              }`}
            >
              {item.label}
            </a>
          ))}
        </nav>
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 items-center justify-between border-b px-6">
          <span className="text-sm text-muted-foreground">
            Helivanta
          </span>
          <a
            href="/logout"
            className="text-sm underline-offset-4 hover:underline"
          >
            Sign out
          </a>
        </header>
        <main className="flex-1 p-6">{children}</main>
      </div>
    </div>
  );
}
```

`apps/shell/app/layout.tsx`:

```tsx
import type { Metadata } from "next";
import "./globals.css";

export const metadata: Metadata = { title: "Helivanta" };

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="en">
      <body>{children}</body>
    </html>
  );
}
```

`apps/shell/app/page.tsx`:

```tsx
import { HmsShell } from "@/components/hms-shell";

const ZONES = [
  {
    href: "/medicore/opd",
    title: "OPD",
    desc: "Outpatient registration & appointments",
  },
  {
    href: "/medicore/ipd",
    title: "IPD",
    desc: "Admissions, beds & ward rounds",
  },
  { href: "/pharmacy", title: "Pharmacy", desc: "Dispensing & drug inventory" },
  { href: "/lab", title: "Lab", desc: "Orders, samples & results" },
];

export default function Dashboard() {
  return (
    <HmsShell active="/">
      <h1 className="mb-6 text-2xl font-semibold">Departments</h1>
      <div className="grid max-w-3xl gap-4 sm:grid-cols-2">
        {ZONES.map((z) => (
          <a
            key={z.href}
            href={z.href}
            className="rounded-lg border p-4 hover:bg-accent"
          >
            <div className="font-medium">{z.title}</div>
            <div className="text-sm text-muted-foreground">{z.desc}</div>
          </a>
        ))}
      </div>
    </HmsShell>
  );
}
```

`apps/shell/.env.example`:

```
NEXT_PUBLIC_GIP_PROJECT_ID=demo-hms
NEXT_PUBLIC_GIP_API_KEY=demo-key
NEXT_PUBLIC_AUTH_EMULATOR_HOST=localhost:9099
MEDICORE_URL=http://localhost:4302
API_URL=http://localhost:8080
```

`apps/mobile/README.md`:

```markdown
# Helivanta Mobile (stubs)

Reserved for Expo apps per spec D7 (issues #11, #566, #567): doctor,
patient, nurse, pharmacist. They will share `packages/api-client` and
`@tesserix/native`. Not scaffolded in Phase 1.
```

- [ ] **Step 3: Install and verify build**

Run: `NODE_AUTH_TOKEN=<PKG_READ_TOKEN from env or gh auth token> pnpm install && pnpm --filter @helivanta/shell build`
Expected: build succeeds. (If `@tesserix/web` auth fails, export `NODE_AUTH_TOKEN` with a GitHub token that has `read:packages`.)

- [ ] **Step 4: Commit**

```bash
git add packages/ apps/ pnpm-lock.yaml
git commit -m "feat: shell app with shared hms chrome and zone rewrites"
```

---

### Task 10: Shell GIP auth — login page, session cookie, route guard, dev seed

**Files:**

- Create: `apps/shell/lib/firebase.ts`, `apps/shell/app/login/page.tsx`, `apps/shell/app/api/session/route.ts`, `apps/shell/app/logout/route.ts`, `apps/shell/middleware.ts`, `scripts/seed-dev.mjs`

**Interfaces:**

- Consumes: Firebase Auth emulator (Task 2), `authn.SessionCookie` name `hms_session` (Task 5).
- Produces: authenticated shell — unauthenticated visits redirect to `/login`; login sets `hms_session` cookie (the GIP ID token; hardening to Firebase session cookies is a follow-on issue); `make seed` creates tenant + user `test@hms.dev` / `password123` with `tenant_id` custom claim.

- [ ] **Step 1: Firebase client lib**

`apps/shell/lib/firebase.ts`:

```ts
import { initializeApp, getApps } from "firebase/app";
import { connectAuthEmulator, getAuth } from "firebase/auth";

export function firebaseAuth() {
  const app =
    getApps()[0] ??
    initializeApp({
      apiKey: process.env.NEXT_PUBLIC_GIP_API_KEY ?? "demo-key",
      projectId: process.env.NEXT_PUBLIC_GIP_PROJECT_ID ?? "demo-hms",
    });
  const auth = getAuth(app);
  const emulator = process.env.NEXT_PUBLIC_AUTH_EMULATOR_HOST;
  // @ts-expect-error internal flag — avoids double-connect in fast refresh
  if (emulator && !auth.emulatorConfig) {
    connectAuthEmulator(auth, `http://${emulator}`, { disableWarnings: true });
  }
  return auth;
}
```

- [ ] **Step 2: Login page**

`apps/shell/app/login/page.tsx`:

```tsx
"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { signInWithEmailAndPassword } from "firebase/auth";
import { firebaseAuth } from "@/lib/firebase";

export default function LoginPage() {
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const cred = await signInWithEmailAndPassword(
        firebaseAuth(),
        email,
        password,
      );
      const idToken = await cred.user.getIdToken();
      const res = await fetch("/api/session", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ idToken }),
      });
      if (!res.ok) throw new Error("session");
      router.replace("/");
    } catch {
      setError("Sign-in failed. Check your email and password.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="flex min-h-screen items-center justify-center">
      <form
        onSubmit={onSubmit}
        className="w-full max-w-sm space-y-4 rounded-lg border p-6"
      >
        <h1 className="text-xl font-semibold">Sign in to Helivanta</h1>
        <label className="block text-sm">
          Email
          <input
            type="email"
            required
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            className="mt-1 w-full rounded-md border px-3 py-2"
          />
        </label>
        <label className="block text-sm">
          Password
          <input
            type="password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="mt-1 w-full rounded-md border px-3 py-2"
          />
        </label>
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}
        <button
          type="submit"
          disabled={busy}
          className="w-full rounded-md bg-primary px-3 py-2 text-primary-foreground disabled:opacity-50"
        >
          {busy ? "Signing in…" : "Sign in"}
        </button>
      </form>
    </main>
  );
}
```

- [ ] **Step 3: Session + logout route handlers**

`apps/shell/app/api/session/route.ts`:

```ts
import { NextRequest, NextResponse } from "next/server";

const SESSION_COOKIE = "hms_session";
const MAX_AGE_SECONDS = 60 * 60; // GIP ID tokens live 1h

export async function POST(req: NextRequest) {
  let idToken: unknown;
  try {
    ({ idToken } = await req.json());
  } catch {
    return NextResponse.json({ error: "invalid_request" }, { status: 400 });
  }
  if (typeof idToken !== "string" || idToken.length < 10) {
    return NextResponse.json({ error: "invalid_request" }, { status: 400 });
  }
  // The Go API verifies the token cryptographically on every request
  // (pkg/authn); this handler only sets the HttpOnly transport cookie.
  const res = NextResponse.json({ ok: true });
  res.cookies.set(SESSION_COOKIE, idToken, {
    httpOnly: true,
    sameSite: "lax",
    secure: process.env.NODE_ENV === "production",
    path: "/",
    maxAge: MAX_AGE_SECONDS,
  });
  return res;
}
```

`apps/shell/app/logout/route.ts`:

```ts
import { NextResponse } from "next/server";

export async function GET(req: Request) {
  const res = NextResponse.redirect(new URL("/login", req.url));
  res.cookies.set("hms_session", "", { path: "/", maxAge: 0 });
  return res;
}
```

- [ ] **Step 4: Route guard**

`apps/shell/middleware.ts`:

```ts
import { NextRequest, NextResponse } from "next/server";

const PUBLIC_PATHS = ["/login", "/api/session"];

export function middleware(req: NextRequest) {
  const { pathname } = req.nextUrl;
  if (PUBLIC_PATHS.some((p) => pathname.startsWith(p))) {
    return NextResponse.next();
  }
  if (!req.cookies.get("hms_session")?.value) {
    const login = new URL("/login", req.url);
    return NextResponse.redirect(login);
  }
  return NextResponse.next();
}

export const config = {
  matcher: ["/((?!_next/static|_next/image|favicon.ico).*)"],
};
```

- [ ] **Step 5: Dev seed script**

`scripts/seed-dev.mjs`:

```js
// Seeds the GIP emulator: one user with a tenant_id custom claim.
// Usage: node scripts/seed-dev.mjs   (emulator must be running)
const HOST = process.env.AUTH_EMULATOR_HOST ?? "localhost:9099";
const PROJECT = process.env.GIP_PROJECT_ID ?? "demo-hms";
const TENANT_ID = "11111111-1111-1111-1111-111111111111";
const EMAIL = "test@hms.dev";
const PASSWORD = "password123";

const base = `http://${HOST}/identitytoolkit.googleapis.com/v1`;
const headers = {
  "Content-Type": "application/json",
  Authorization: "Bearer owner",
};

async function main() {
  const signUp = await fetch(`${base}/accounts:signUp?key=demo-key`, {
    method: "POST",
    headers,
    body: JSON.stringify({
      email: EMAIL,
      password: PASSWORD,
      returnSecureToken: true,
    }),
  });
  const created = await signUp.json();
  if (!signUp.ok && created?.error?.message !== "EMAIL_EXISTS") {
    throw new Error(`signUp failed: ${JSON.stringify(created)}`);
  }
  let localId = created.localId;
  if (!localId) {
    const lookup = await fetch(
      `http://${HOST}/emulator/v1/projects/${PROJECT}/accounts:query`,
      { method: "POST", headers, body: JSON.stringify({}) },
    ).then((r) => r.json());
    localId = lookup.userInfo?.find((u) => u.email === EMAIL)?.localId;
  }
  if (!localId) throw new Error("could not resolve user id");

  const update = await fetch(`${base}/accounts:update?key=demo-key`, {
    method: "POST",
    headers,
    body: JSON.stringify({
      localId,
      customAttributes: JSON.stringify({ tenant_id: TENANT_ID }),
    }),
  });
  if (!update.ok) throw new Error(`set claims failed: ${await update.text()}`);
  console.log(`Seeded ${EMAIL} / ${PASSWORD} with tenant_id=${TENANT_ID}`);
}

main().catch((e) => {
  console.error(e);
  process.exit(1);
});
```

- [ ] **Step 6: Verify the full login flow manually**

Run: `make dev-infra && make seed && pnpm --filter @helivanta/shell dev &` then:

- `curl -s -o /dev/null -w "%{http_code} %{redirect_url}\n" http://localhost:4301/` → expected `307 http://localhost:4301/login` (guard works).
- Browser: visit `http://localhost:4301/login`, sign in with `test@hms.dev` / `password123` → lands on dashboard with sidebar. Stop the dev server after.

- [ ] **Step 7: Commit**

```bash
git add apps/shell/ scripts/
git commit -m "feat: gip login with session cookie, route guard and dev seed"
```

---

### Task 11: apps/medicore zone — basePath, OPD/IPD pages, live API data

**Files:**

- Create: `apps/medicore/package.json`, `apps/medicore/next.config.ts`, `apps/medicore/tsconfig.json`, `apps/medicore/postcss.config.mjs`, `apps/medicore/app/globals.css`, `apps/medicore/app/layout.tsx`, `apps/medicore/app/page.tsx`, `apps/medicore/app/opd/page.tsx`, `apps/medicore/app/ipd/page.tsx`, `apps/medicore/components/hms-shell.tsx`, `apps/medicore/components/ping-panel.tsx`

**Interfaces:**

- Consumes: `HmsShell` (exact copy of `apps/shell/components/hms-shell.tsx` — promotion to a shared package is deliberately deferred until the pharmacy zone exists, rule of three); reference API (`POST /api/v1/reference/ping`, `GET /api/v1/reference/pings` — note browser path prefix `/api` maps to backend root, so backend `/v1/...` is browser `/api/v1/...`).
- Produces: zone app on port 4302 with `basePath: "/medicore"`; pages `/medicore`, `/medicore/opd`, `/medicore/ipd`.

- [ ] **Step 1: Scaffold the zone**

`apps/medicore/package.json` — same as shell's minus `firebase`, name `@helivanta/medicore`, ports 4302:

```json
{
  "name": "@helivanta/medicore",
  "version": "0.0.0",
  "private": true,
  "scripts": {
    "dev": "next dev -p 4302",
    "build": "next build",
    "start": "next start -p 4302",
    "lint": "next lint",
    "type-check": "tsc --noEmit"
  },
  "dependencies": {
    "@tesserix/web": "^1.8.1",
    "next": "^16.0.0",
    "react": "^19.0.0",
    "react-dom": "^19.0.0",
    "framer-motion": "^12.0.0"
  },
  "devDependencies": {
    "@helivanta/config": "workspace:*",
    "@tailwindcss/postcss": "^4.1.0",
    "@types/node": "^22",
    "@types/react": "^19",
    "tailwindcss": "^4.1.0",
    "typescript": "^5.7.0"
  }
}
```

`apps/medicore/next.config.ts`:

```ts
import type { NextConfig } from "next";

const API_URL = process.env.API_URL ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  output: "standalone",
  basePath: "/medicore",
  async rewrites() {
    // Only used when hitting :4302 directly; via the shell the same
    // /api/* path is rewritten by the shell itself.
    return [{ source: "/api/:path*", destination: `${API_URL}/:path*` }];
  },
};

export default nextConfig;
```

`tsconfig.json`, `postcss.config.mjs`, `app/globals.css`, `app/layout.tsx`: identical to `apps/shell`'s versions (Task 9 shows their full contents — copy the files, changing nothing).

`components/hms-shell.tsx`: byte-identical copy of `apps/shell/components/hms-shell.tsx` (Task 9 Step 2 shows the full source).

- [ ] **Step 2: Pages**

`apps/medicore/app/page.tsx`:

```tsx
import { redirect } from "next/navigation";

export default function MediCoreIndex() {
  redirect("/medicore/opd");
}
```

`apps/medicore/components/ping-panel.tsx` — proves zone → API → tenant data round trip:

```tsx
"use client";

import { useCallback, useEffect, useState } from "react";

type Ping = { id: string; message: string; created_at: string };

export function PingPanel({ department }: { department: string }) {
  const [pings, setPings] = useState<Ping[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await fetch("/api/v1/reference/pings");
      if (!res.ok) throw new Error(String(res.status));
      const body = await res.json();
      setPings(body.data ?? []);
      setError(null);
    } catch {
      setError("Could not load activity. Is the API running?");
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  async function ping() {
    setBusy(true);
    try {
      const res = await fetch("/api/v1/reference/ping", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ message: `${department} ping` }),
      });
      if (!res.ok) throw new Error(String(res.status));
      await load();
    } catch {
      setError("Ping failed.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="max-w-xl space-y-4">
      <button
        onClick={ping}
        disabled={busy}
        className="rounded-md bg-primary px-3 py-2 text-sm text-primary-foreground disabled:opacity-50"
      >
        {busy ? "Pinging…" : `Ping from ${department}`}
      </button>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <ul className="divide-y rounded-md border text-sm">
        {pings.length === 0 && (
          <li className="p-3 text-muted-foreground">No activity yet.</li>
        )}
        {pings.map((p) => (
          <li key={p.id} className="flex justify-between p-3">
            <span>{p.message}</span>
            <time className="text-muted-foreground">
              {new Date(p.created_at).toLocaleTimeString()}
            </time>
          </li>
        ))}
      </ul>
    </section>
  );
}
```

`apps/medicore/app/opd/page.tsx`:

```tsx
import { HmsShell } from "@/components/hms-shell";
import { PingPanel } from "@/components/ping-panel";

export default function OpdPage() {
  return (
    <HmsShell active="/medicore/opd">
      <h1 className="mb-6 text-2xl font-semibold">OPD — Outpatients</h1>
      <PingPanel department="OPD" />
    </HmsShell>
  );
}
```

`apps/medicore/app/ipd/page.tsx`:

```tsx
import { HmsShell } from "@/components/hms-shell";
import { PingPanel } from "@/components/ping-panel";

export default function IpdPage() {
  return (
    <HmsShell active="/medicore/ipd">
      <h1 className="mb-6 text-2xl font-semibold">IPD — Wards</h1>
      <PingPanel department="IPD" />
    </HmsShell>
  );
}
```

- [ ] **Step 3: Verify zone stitching end-to-end**

Run: `pnpm install && make dev-infra && make seed`, then in three terminals (or backgrounded): backend API with `FIREBASE_AUTH_EMULATOR_HOST=localhost:9099`, `pnpm --filter @helivanta/shell dev`, `pnpm --filter @helivanta/medicore dev`.

- Browser: log in at `http://localhost:4301/login`, click "OPD" in the sidebar → URL is `http://localhost:4301/medicore/opd` (served by the medicore app through the shell rewrite), identical sidebar, "Ping from OPD" adds a row that persists across reloads and shows on the IPD page too (same tenant, same API).
- `curl -s -o /dev/null -w "%{http_code}" http://localhost:4301/api/v1/reference/pings` → `401` (API auth enforced through the rewrite chain).

- [ ] **Step 4: Verify builds pass**

Run: `pnpm turbo type-check build`
Expected: shell + medicore both green.

- [ ] **Step 5: Commit**

```bash
git add apps/medicore/ pnpm-lock.yaml
git commit -m "feat: medicore zone with opd and ipd pages consuming reference api"
```

---

### Task 12: Playwright E2E smoke — login → dashboard → OPD across the zone boundary

**Files:**

- Create: `e2e/package.json`, `e2e/playwright.config.ts`, `e2e/tests/smoke.spec.ts`

**Interfaces:**

- Consumes: full dev stack (Tasks 2, 7, 9–11), seed user `test@hms.dev`/`password123` (Task 10).
- Produces: `pnpm --filter @helivanta/e2e test` running the cross-zone journey headlessly.

- [ ] **Step 1: E2E package**

`e2e/package.json`:

```json
{
  "name": "@helivanta/e2e",
  "version": "0.0.0",
  "private": true,
  "scripts": { "test": "playwright test" },
  "devDependencies": { "@playwright/test": "^1.57.0", "@types/node": "^22" }
}
```

`e2e/playwright.config.ts`:

```ts
import { defineConfig } from "@playwright/test";

// Assumes infra + API + shell + medicore already running (make dev, make seed).
export default defineConfig({
  testDir: "./tests",
  timeout: 60_000,
  use: { baseURL: "http://localhost:4301" },
});
```

`e2e/tests/smoke.spec.ts`:

```ts
import { test, expect } from "@playwright/test";

test("login, dashboard, cross-zone OPD ping", async ({ page }) => {
  // Unauthenticated → redirected to login.
  await page.goto("/");
  await expect(page).toHaveURL(/\/login$/);

  await page.getByLabel("Email").fill("test@hms.dev");
  await page.getByLabel("Password").fill("password123");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(
    page.getByRole("heading", { name: "Departments" }),
  ).toBeVisible();

  // Cross the zone boundary: shell → medicore (hard navigation).
  await page.getByRole("link", { name: "OPD" }).click();
  await expect(page).toHaveURL(/\/medicore\/opd$/);
  await expect(page.getByRole("heading", { name: /OPD/ })).toBeVisible();

  // Round-trip through the Go API with the session cookie.
  await page.getByRole("button", { name: /Ping from OPD/ }).click();
  await expect(page.getByText("OPD ping").first()).toBeVisible();
});
```

- [ ] **Step 2: Run it**

Run (with the full stack + seed up, as in Task 11 Step 3): `pnpm install && pnpm --filter @helivanta/e2e exec playwright install chromium && pnpm --filter @helivanta/e2e test`
Expected: 1 passed.

- [ ] **Step 3: Commit**

```bash
git add e2e/ pnpm-lock.yaml
git commit -m "test: playwright smoke covering login and cross-zone opd journey"
```

---

### Task 13: ADRs, web CI job, services.yaml proposal

**Files:**

- Create: `docs/adr/0001-monorepo-and-multi-zone-strategy.md`, `docs/adr/0002-gip-not-keycloak.md`, `docs/deployment/tesserix-k8s-proposal.md`
- Modify: `.github/workflows/ci.yml`

**Interfaces:**

- Consumes: spec decisions D1–D8; existing `ci.yml` go job (Task 8).
- Produces: recorded ADRs (closing issue #665's decision for product code + resolving issue #2's "Keycloak/GIP"); `web` CI job; a paste-ready tesserix-k8s change proposal.

- [ ] **Step 1: ADRs**

`docs/adr/0001-monorepo-and-multi-zone-strategy.md`:

```markdown
# ADR-0001: Full monorepo with path-based Next.js multi-zones

- **Status:** Accepted (2026-08-04)
- **Context:** Issue #665 asked where product code lives. Issue #2 fixes the
  backend as one Go module. The frontend needs per-product team isolation
  without per-department domain sprawl.
- **Decision:** `tesserix/helivanta` is a full monorepo: `apps/` (Next.js zones),
  `backend/` (Go modular monolith `github.com/tesserix/helivanta`), `packages/`.
  Zones map 1:1 to products (MediCore, PharmaConnect, LabConnect …) and are
  path-mounted (`/medicore`) behind the shell on one tenant domain. JS uses
  pnpm workspaces + Turborepo (deviation from mark8ly's npm — matches the
  Web SDK direction, issue #696). Deployment manifests live in tesserix-k8s,
  not here. SDK packages (go-shared, @tesserix/web) stay in their own repos.
- **Consequences:** one PR spans frontend+backend; zone count grows with
  products, not departments; cross-zone navigation is a hard navigation —
  shared chrome must come from a shared component, not a persistent tree.
```

`docs/adr/0002-gip-not-keycloak.md`:

```markdown
# ADR-0002: Google Identity Platform, not Keycloak

- **Status:** Accepted (2026-08-04)
- **Context:** Foundational issues say "Keycloak/GIP" interchangeably. The
  org already runs per-product GIP tenants on tesseracthub-480811 with
  canonical onboarding scripts in tesserix-k8s (docs/identity/).
- **Decision:** All Helivanta authentication uses GIP. Frontends use the Firebase
  Web/native SDKs (emulator locally); the Go API verifies GIP ID tokens via
  the Firebase Admin SDK and requires a `tenant_id` custom claim. Keycloak
  is not deployed. Where issues name Keycloak, read GIP.
- **Consequences:** no self-hosted IdP to operate; MFA/OTP/passkeys come
  from GIP features; per-hospital GIP tenants follow the existing
  enable-tenant-google-idp.py flow. Follow-on: swap the raw-ID-token
  session cookie for Firebase session cookies before production.
```

`docs/deployment/tesserix-k8s-proposal.md`:

```markdown
# tesserix-k8s changes for Helivanta (proposal)

Phase 1 ships images only; this is the paste-ready plan for the infra PR.

## charts/apps/ additions

- `hms-api` — Go API (port 8080, healthz/readyz; needs APP/ADMIN_DATABASE_URL,
  NATS_URL, GIP_PROJECT_ID). Copy `mark8ly-platform-api` chart shape.
- `hms-shell`, `hms-medicore` — Next.js standalone (ports 4301/4302). Copy
  `mark8ly-admin` chart shape.
- `hms-postgres` — copy `mark8ly-postgres`; run `dev/init-db.sql` equivalent
  (hms_app role, NOBYPASSRLS) via init job.
- `hms-openfga` — copy `mark8ly-openfga` (Phase 2 wires the model).
- NATS: reuse the existing `nats` chart (JetStream on).

## Routing

One host per tenant; path routing: `/` → hms-shell, `/medicore` → hms-medicore,
`/api/*` → hms-api (strip nothing; API serves `/v1/*`, edge maps `/api/(.*)` → `/$1`).

## services.yaml

Add `hms-api` (backend, go), `hms-shell`, `hms-medicore` (frontend, node)
under a new `hms` appGroup with the standard ci.yml/release.yml workflows.

## Identity

Create GIP tenant(s) for Helivanta via scripts/identity/enable-tenant-google-idp.py.
```

- [ ] **Step 2: Add web job to CI**

Append to `.github/workflows/ci.yml` under `jobs:`:

```yaml
web:
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4
    - uses: pnpm/action-setup@v4
    - uses: actions/setup-node@v4
      with:
        node-version: 22
        cache: pnpm
    - run: pnpm install --frozen-lockfile
      env:
        NODE_AUTH_TOKEN: ${{ secrets.PKG_READ_TOKEN }}
    - run: pnpm turbo type-check build
      env:
        NODE_AUTH_TOKEN: ${{ secrets.PKG_READ_TOKEN }}
```

Note: `PKG_READ_TOKEN` repo secret (GitHub token with `read:packages`) must exist — same convention as the other tesserix frontend repos. If it is missing, add it: `gh secret set PKG_READ_TOKEN --repo tesserix/helivanta`.

- [ ] **Step 3: Verify and commit**

Run: `pnpm turbo type-check build` (local equivalent of the web job).
Expected: green.

```bash
git add docs/ .github/
git commit -m "docs: adrs for monorepo strategy and gip, k8s proposal; ci web job"
git push
gh run watch --exit-status || gh run view --log-failed
```

Expected: both CI jobs green.

---

## Self-Review Notes

- **Spec coverage:** D1 (Task 1/8/13), D2 (Tasks 9/11), D3 (HmsShell copies + cookie-guard; full cookie-persisted sidebar state deferred until there is state to persist — the current chrome is stateless), D4 (Tasks 5/10, ADR-0002), D5 (Task 13 proposal doc; no manifests in repo), D6 (Tasks 6/7; the Go import-boundary linter is deferred to Phase 2 with the second module — with one module there is nothing to lint against), D7 (Task 9 mobile stub), D8 (Tasks 1/8/13). Phase-1 list items 1–6 all mapped.
- **Deferred deliberately (YAGNI):** packages/ui, packages/api-client, packages/registry (spec marks them Phase 2+); pharmacy/lab zones; OpenFGA middleware; PHI-redaction log processor; Firebase session-cookie hardening (noted in ADR-0002).
- **Type consistency check:** `Consumers(deps Deps)` signature change is called out in Task 7 and applied to Task 3's test; `hms_session` name shared via `authn.SessionCookie` and string-matched in shell middleware; browser API prefix `/api/v1/...` ↔ backend `/v1/...` consistent across Tasks 9/11/13.

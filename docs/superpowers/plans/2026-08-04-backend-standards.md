# HMS Backend Standards Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Machine-enforced backend standards: golangci-lint + depguard boundaries, architecture tests, `respond`/`TenantPrincipal` helpers replacing handler copy-paste, consolidated test scaffolding, slog + request-ID observability, a 70% coverage gate, a `make new-module` generator, and docs/CLAUDE.md/skill enforcement.

**Architecture:** Guardrails land first (lint, arch tests) so later work is built under the gates; helpers and testutil then migrate all four modules as reference implementations; generator + docs encode the result. Everything lives in `backend/` except docs and the skill.

**Tech Stack:** Go 1.26, golangci-lint v2 (brew local / official GitHub Action in CI), golang.org/x/tools/go/packages (arch test), testcontainers (existing), bash + go tooling for the coverage gate and generator.

**Spec:** `docs/superpowers/specs/2026-08-04-backend-standards-design.md`

## Global Constraints

- Go module `github.com/tesserix/hms` rooted at `backend/`; all backend commands run from `backend/`.
- Modules in `internal/modules/*` must never import another module's package — after this plan, that is lint- AND test-enforced.
- Migration IDs globally unique; every tenant table forced-RLS (existing `LintRLS` stays authoritative).
- Event subjects `^hms\.[a-z]+\.[a-z]+\.[a-z_]+\.v\d+$`; consumer names `^[a-z]+-[a-z-]+$`.
- Status-code semantics: 404 for missing AND cross-tenant, 409 for state-transition conflicts (guarded UPDATE), 202 async creates, lists newest-first LIMIT 100.
- Logging: slog only (logrus banned by depguard); errors wrap with `%w`; no panic/Fatal outside `main`.
- Tests need Docker (testcontainers); full suite with `-race`. E2E and frontend untouched by this plan.
- Coverage floor 70% for `internal/modules/*` and `pkg/*`; exempt `cmd/*`, `internal/config`, `internal/archtest`, `internal/testutil`, `internal/journey`.
- Conventional single-line commits, no signatures, no Co-Authored-By.
- The existing HTTP response shapes must not change (E2E + frontend depend on them): success bodies stay exactly `{"data": ...}` for lists, `{"id": ...}`/row objects elsewhere; errors stay `{"error": code, "message": text}`.

---

### Task 1: golangci-lint — config, CI, violation cleanup

**Files:**

- Create: `backend/.golangci.yml`
- Modify: `.github/workflows/ci.yml` (go job), `Makefile` (add `lint-go` target); any Go files needing violation fixes

**Interfaces:**

- Produces: `golangci-lint run` clean from `backend/`; depguard rules that Task 2's arch test mirrors; CI gate.

- [ ] **Step 1: Install the tool locally**

Run: `brew install golangci-lint && golangci-lint version`
Expected: v2.x installed. (The config below uses the v2 schema; if brew delivers v1.6x instead, translate the config to the v1 schema — the linter set and depguard rules are what matter.)

- [ ] **Step 2: Write `backend/.golangci.yml`**

```yaml
version: "2"

linters:
  default: none
  enable:
    - errcheck
    - govet
    - staticcheck
    - revive
    - gosec
    - sqlclosecheck
    - misspell
    - unconvert
    - depguard
  settings:
    depguard:
      rules:
        module-isolation:
          files:
            - "**/internal/modules/**"
          deny:
            - pkg: "github.com/tesserix/hms/internal/modules"
              desc: "modules must not import other modules — cross-module data flows only via events (spec D3)"
        no-logrus:
          deny:
            - pkg: "github.com/sirupsen/logrus"
              desc: "slog only (spec D6)"
    revive:
      rules:
        - name: exported
          disabled: true
  exclusions:
    rules:
      - path: _test\.go
        linters:
          - gosec
          - errcheck

run:
  timeout: 5m
```

Note on depguard module-isolation: depguard denies by import-path prefix, so `github.com/tesserix/hms/internal/modules` would also deny a module importing ITSELF and same-module subpackages. Current modules are single-package so this cannot trigger falsely today; the arch test in Task 2 carries the precise same-module allowance. If depguard's rule proves too coarse when a module gains subpackages, scope the deny per-module at that time — do not silently drop the rule.

- [ ] **Step 3: Run and fix violations**

Run: `cd backend && golangci-lint run ./... 2>&1 | tee /tmp/lint-report.txt`

Fix every finding. Expected classes (from code reading): errcheck on ignored `Close()`/`Publish()` style calls (fix with explicit `_ =` only where discard is deliberate AND add a short comment why; otherwise handle the error), gosec G404-style findings in non-crypto contexts (annotate `//nolint:gosec // <reason>` only with justification), staticcheck simplifications. NEVER blanket-disable a linter to get green; every `//nolint` carries a reason.

Run until clean: `golangci-lint run ./...` → no findings. Then `go test -race ./...` still green.

- [ ] **Step 4: Wire CI and Makefile**

In `.github/workflows/ci.yml` go job, after the `setup-go` step add:

```yaml
- name: golangci-lint
  uses: golangci/golangci-lint-action@v8
  with:
    working-directory: backend
```

(Keep `go vet` — it is redundant with govet in the linter but cheap; remove the standalone `- run: go vet ./...` line to avoid double work.)

Makefile: add

```makefile
lint-go:
	cd backend && golangci-lint run ./...
```

and add `lint-go` to the `test` target's prerequisites line? No — keep `test` as-is; document `make lint-go` in Task 8's docs.

- [ ] **Step 5: Commit**

```bash
git add backend/.golangci.yml .github/workflows/ci.yml Makefile backend/
git commit -m "feat: golangci-lint with depguard module isolation wired into ci"
```

---

### Task 2: Architecture tests

**Files:**

- Create: `backend/internal/archtest/arch_test.go`, `backend/internal/archtest/rls_test.go`
- Modify: `backend/go.mod` (adds golang.org/x/tools)

**Interfaces:**

- Consumes: `platform.Module` (`Name/Migrations/Routes/Consumers`), module constructors `reference.New()`, `medicore.New()`, `pharmacy.New()`, `lab.New()`, `events.Migrations()`, `events.Consumer{Name,Subject}`, module `Subject*` exported constants, `tenantdb.Open/Migrate/LintRLS`, `testutil.StartPostgres`.
- Produces: test-only package `archtest` (all files `package archtest`) that fails the build on: cross-module imports, duplicate migration IDs, malformed subjects/consumer names, RLS gaps.

- [ ] **Step 1: Write the import-graph + registry tests**

Run first: `cd backend && go get golang.org/x/tools@latest`

`backend/internal/archtest/arch_test.go`:

```go
// Package archtest enforces the architecture rules that lint alone
// cannot express precisely (same-module imports allowed, cross-module
// denied) and the registry-level invariants (unique migration IDs,
// subject/consumer naming).
package archtest

import (
	"regexp"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"

	"github.com/tesserix/hms/internal/modules/lab"
	"github.com/tesserix/hms/internal/modules/medicore"
	"github.com/tesserix/hms/internal/modules/pharmacy"
	"github.com/tesserix/hms/internal/modules/reference"
	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/pkg/events"
)

const modulesPrefix = "github.com/tesserix/hms/internal/modules/"

// allModules must list every registered module. cmd/api/main.go is the
// runtime source of truth; keep them in sync (the generator prints a
// reminder).
func allModules() []platform.Module {
	return []platform.Module{reference.New(), medicore.New(), pharmacy.New(), lab.New()}
}

func moduleOf(pkgPath string) string {
	rest := strings.TrimPrefix(pkgPath, modulesPrefix)
	if rest == pkgPath {
		return ""
	}
	return strings.SplitN(rest, "/", 2)[0]
}

func TestModulesDoNotImportEachOther(t *testing.T) {
	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedName | packages.NeedImports}, modulesPrefix+"...")
	if err != nil {
		t.Fatalf("load packages: %v", err)
	}
	for _, p := range pkgs {
		from := moduleOf(p.PkgPath)
		for imp := range p.Imports {
			to := moduleOf(imp)
			if to != "" && to != from {
				t.Errorf("module %q imports module %q (%s -> %s): cross-module data flows only via events", from, to, p.PkgPath, imp)
			}
		}
	}
}

func TestMigrationIDsAreGloballyUnique(t *testing.T) {
	seen := map[string]string{}
	record := func(owner, id string) {
		if prev, dup := seen[id]; dup {
			t.Errorf("migration id %q used by both %s and %s", id, prev, owner)
		}
		seen[id] = owner
	}
	for _, m := range events.Migrations() {
		record("events", m.ID)
	}
	for _, mod := range allModules() {
		for _, m := range mod.Migrations() {
			record(mod.Name(), m.ID)
		}
	}
}

var (
	subjectRe  = regexp.MustCompile(`^hms\.[a-z]+\.[a-z]+\.[a-z_]+\.v\d+$`)
	consumerRe = regexp.MustCompile(`^[a-z]+-[a-z-]+$`)
)

func TestConsumerContracts(t *testing.T) {
	deps := platform.Deps{} // consumers are declared statically; nil deps fine for inspection
	for _, mod := range allModules() {
		for _, c := range mod.Consumers(deps) {
			if !consumerRe.MatchString(c.Name) {
				t.Errorf("module %s consumer name %q must match %s", mod.Name(), c.Name, consumerRe)
			}
			if !subjectRe.MatchString(c.Subject) {
				t.Errorf("module %s consumer subject %q must match %s", mod.Name(), c.Subject, subjectRe)
			}
		}
	}
}

func TestPublishedSubjectConstants(t *testing.T) {
	for name, s := range map[string]string{
		"reference.SubjectPinged":          reference.SubjectPinged,
		"medicore.SubjectVisitCreated":     medicore.SubjectVisitCreated,
		"pharmacy.SubjectDispenseRecorded": pharmacy.SubjectDispenseRecorded,
		"lab.SubjectResultReady":           lab.SubjectResultReady,
	} {
		if !subjectRe.MatchString(s) {
			t.Errorf("%s = %q must match %s", name, s, subjectRe)
		}
	}
}
```

- [ ] **Step 2: Write the RLS arch test**

`backend/internal/archtest/rls_test.go`:

```go
package archtest

import (
	"context"
	"testing"
	"time"

	"github.com/tesserix/hms/internal/testutil"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
)

// TestAllMigrationsPassRLSLint applies every migration to a fresh
// database and re-runs the boot-time RLS linter, so an unprotected
// tenant table fails in CI, not at deploy.
func TestAllMigrationsPassRLSLint(t *testing.T) {
	appDSN, adminDSN := testutil.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, adminDSN)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	migs := events.Migrations()
	for _, mod := range allModules() {
		migs = append(migs, mod.Migrations()...)
	}
	if err := db.Migrate(ctx, migs); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	bad, err := db.LintRLS(ctx)
	if err != nil {
		t.Fatalf("lint: %v", err)
	}
	if len(bad) > 0 {
		t.Errorf("tables missing forced RLS: %v", bad)
	}
}
```

- [ ] **Step 3: Run the tests**

Run: `cd backend && go test ./internal/archtest/ -v`
Expected: PASS (all current code conforms). Sanity-check the isolation test bites: temporarily add `_ "github.com/tesserix/hms/internal/modules/medicore"` to pharmacy's module.go, re-run, watch it FAIL, revert.

- [ ] **Step 4: Verify golangci + suite**

Run: `cd backend && golangci-lint run ./... && go test -race ./internal/archtest/`
Expected: clean/PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/internal/archtest backend/go.mod backend/go.sum
git commit -m "test: architecture tests for module isolation, migrations, subjects and rls"
```

---

### Task 3: respond helpers + TenantPrincipal, migrate all modules

**Files:**

- Create: `backend/internal/platform/respond/respond.go`, `backend/internal/platform/respond/respond_test.go`
- Modify: `backend/pkg/authn/authn.go` (add TenantPrincipal + test in authn_test.go), all four `backend/internal/modules/*/module.go`

**Interfaces:**

- Produces (used by Task 7 generator and all modules):
  - `respond.OK(c *gin.Context, data any)` → 200 raw `data` when it is a full body (used as `respond.OK(c, gin.H{"data": rows})` for lists — the helper does NOT wrap, preserving existing shapes).
  - `respond.Created(c, data any)` → 201; `respond.Accepted(c, data any)` → 202.
  - `respond.Error(c, status int, code, message string)` → `{"error": code, "message": message}` via AbortWithStatusJSON.
  - Shorthands: `respond.NotFound(c, resource string)` (404, code "not_found", message "<resource> not found"), `respond.Conflict(c, message string)` (409, "conflict"), `respond.BadRequest(c, err error)` (400, "invalid_request", err.Error()), `respond.Internal(c, message string)` (500, "internal"), `respond.Unauthenticated(c, message string)` (401, "unauthenticated").
  - `authn.TenantPrincipal(c *gin.Context) (Principal, uuid.UUID, bool)` — false ⇒ 401 already written and request aborted.

- [ ] **Step 1: Write the failing respond tests**

`backend/internal/platform/respond/respond_test.go`:

```go
package respond_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/platform/respond"
)

func run(h gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/t", h)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/t", nil))
	return w
}

func TestSuccessHelpersPreserveShapes(t *testing.T) {
	w := run(func(c *gin.Context) { respond.OK(c, gin.H{"data": []string{"a"}}) })
	require.Equal(t, http.StatusOK, w.Code)
	require.JSONEq(t, `{"data":["a"]}`, w.Body.String())

	w = run(func(c *gin.Context) { respond.Accepted(c, gin.H{"id": "x"}) })
	require.Equal(t, http.StatusAccepted, w.Code)
	require.JSONEq(t, `{"id":"x"}`, w.Body.String())
}

func TestErrorHelpers(t *testing.T) {
	w := run(func(c *gin.Context) { respond.NotFound(c, "ping") })
	require.Equal(t, http.StatusNotFound, w.Code)
	require.JSONEq(t, `{"error":"not_found","message":"ping not found"}`, w.Body.String())

	w = run(func(c *gin.Context) { respond.Conflict(c, "already dispensed") })
	require.Equal(t, http.StatusConflict, w.Code)
	require.JSONEq(t, `{"error":"conflict","message":"already dispensed"}`, w.Body.String())

	w = run(func(c *gin.Context) { respond.BadRequest(c, errors.New("bad field")) })
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.JSONEq(t, `{"error":"invalid_request","message":"bad field"}`, w.Body.String())

	w = run(func(c *gin.Context) { respond.Internal(c, "could not record ping") })
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.JSONEq(t, `{"error":"internal","message":"could not record ping"}`, w.Body.String())
}
```

Add to `backend/pkg/authn/authn_test.go`:

```go
func TestTenantPrincipal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/p", authn.Middleware(fakeVerifier{p: authn.Principal{Subject: "u1", TenantID: "11111111-1111-1111-1111-111111111111"}}), func(c *gin.Context) {
		_, tenantID, ok := authn.TenantPrincipal(c)
		require.True(t, ok)
		c.JSON(http.StatusOK, gin.H{"tenant": tenantID.String()})
	})
	r.GET("/bad", authn.Middleware(fakeVerifier{p: authn.Principal{Subject: "u1", TenantID: "not-a-uuid"}}), func(c *gin.Context) {
		if _, _, ok := authn.TenantPrincipal(c); !ok {
			return
		}
		c.JSON(http.StatusOK, gin.H{})
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/p", nil)
	req.Header.Set("Authorization", "Bearer good")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "11111111")

	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/bad", nil)
	req.Header.Set("Authorization", "Bearer good")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Contains(t, w.Body.String(), "unauthenticated")
}
```

- [ ] **Step 2: Run to verify failures**

Run: `cd backend && go test ./internal/platform/respond/ ./pkg/authn/`
Expected: FAIL (package respond missing; TenantPrincipal undefined).

- [ ] **Step 3: Implement**

`backend/internal/platform/respond/respond.go`:

```go
// Package respond centralizes the HTTP envelopes. Success helpers pass
// the body through unchanged (shapes are frozen by the frontend/E2E);
// error helpers own the {"error","message"} envelope.
package respond

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func OK(c *gin.Context, data any)       { c.JSON(http.StatusOK, data) }
func Created(c *gin.Context, data any)  { c.JSON(http.StatusCreated, data) }
func Accepted(c *gin.Context, data any) { c.JSON(http.StatusAccepted, data) }

func Error(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": code, "message": message})
}

func NotFound(c *gin.Context, resource string) {
	Error(c, http.StatusNotFound, "not_found", resource+" not found")
}

func Conflict(c *gin.Context, message string) {
	Error(c, http.StatusConflict, "conflict", message)
}

func BadRequest(c *gin.Context, err error) {
	Error(c, http.StatusBadRequest, "invalid_request", err.Error())
}

func Internal(c *gin.Context, message string) {
	Error(c, http.StatusInternalServerError, "internal", message)
}

func Unauthenticated(c *gin.Context, message string) {
	Error(c, http.StatusUnauthorized, "unauthenticated", message)
}
```

Append to `backend/pkg/authn/authn.go`:

```go
// TenantPrincipal extracts the authenticated principal and its tenant
// UUID. On a missing principal or malformed tenant claim it writes the
// 401 envelope, aborts, and returns ok=false — callers just return.
func TenantPrincipal(c *gin.Context) (Principal, uuid.UUID, bool) {
	p, ok := PrincipalFrom(c)
	if !ok {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated", "message": "missing principal"})
		return Principal{}, uuid.Nil, false
	}
	tenantID, err := uuid.Parse(p.TenantID)
	if err != nil {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated", "message": "invalid tenant"})
		return Principal{}, uuid.Nil, false
	}
	return p, tenantID, true
}
```

(Add `"github.com/google/uuid"` to authn imports; authn must not import respond — pkg/ must not depend on internal/.)

- [ ] **Step 4: Run to verify pass**

Run: `cd backend && go test ./internal/platform/respond/ ./pkg/authn/ -v`
Expected: PASS.

- [ ] **Step 5: Migrate all four modules**

In `reference`, `medicore`, `pharmacy`, `lab` module.go handlers, mechanically replace:

- the principal+uuid dance → `p, tenantUUID, ok := authn.TenantPrincipal(c); if !ok { return }` (keep using both or `_` the unused one)
- `c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", ...})` → `respond.BadRequest(c, err)`
- 404 literals → `respond.NotFound(c, "ping"|"visit"|"dispense"|"order")` (messages must stay byte-identical: "ping not found", "dispense not found", "order not found" — the helper composes them identically; visits have no 404 path)
- 409 literals → `respond.Conflict(c, "already dispensed")` / `respond.Conflict(c, "result already recorded")`
- 500 literals → `respond.Internal(c, "could not record ping")` etc. — keep each existing message string exactly
- success writes → `respond.OK/Accepted/Created` with the SAME bodies as today.

Do not change any route path, status, or body. Existing module tests are the safety net.

- [ ] **Step 6: Full module suite**

Run: `cd backend && go test -race ./internal/modules/... ./internal/journey/ && golangci-lint run ./...`
Expected: all PASS, lint clean.

- [ ] **Step 7: Commit**

```bash
git add backend/
git commit -m "feat: respond helpers and tenant principal extractor across all modules"
```

---

### Task 4: Test scaffolding consolidation

**Files:**

- Create: `backend/internal/testutil/harness.go`
- Modify: `backend/internal/modules/{reference,medicore,pharmacy,lab}/module_test.go`, `backend/internal/journey/journey_test.go`

**Interfaces:**

- Produces:
  - `testutil.StaticVerifier map[string]string` implementing `authn.TokenVerifier` (token → tenant id; unknown token → error).
  - `testutil.TenantA = "11111111-1111-1111-1111-111111111111"`, `testutil.TenantB = "22222222-2222-2222-2222-222222222222"`.
  - `testutil.ModuleHarness(t, tokens map[string]string, mods ...platform.Module) (r *gin.Engine, db *tenantdb.DB, bus *events.Bus, ctx context.Context)` — starts Postgres+NATS containers, migrates events+module migrations, asserts LintRLS clean, mounts routes under `/v1` with `authn.Middleware(StaticVerifier(tokens))`, starts consumers + dispatcher.
  - `testutil.Do(r *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder`.

- [ ] **Step 1: Write `backend/internal/testutil/harness.go`**

```go
package testutil

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
)

const (
	TenantA = "11111111-1111-1111-1111-111111111111"
	TenantB = "22222222-2222-2222-2222-222222222222"
)

// StaticVerifier maps bearer tokens to tenant ids for tests.
type StaticVerifier map[string]string

func (s StaticVerifier) Verify(ctx context.Context, raw string) (authn.Principal, error) {
	if tenant, ok := s[raw]; ok {
		return authn.Principal{Subject: "user-" + raw, TenantID: tenant}, nil
	}
	return authn.Principal{}, context.DeadlineExceeded
}

// ModuleHarness boots the full module stack (Postgres, NATS, routes,
// consumers, dispatcher) for the given modules. One call replaces the
// setup() previously copy-pasted per module test package.
func ModuleHarness(t *testing.T, tokens map[string]string, mods ...platform.Module) (*gin.Engine, *tenantdb.DB, *events.Bus, context.Context) {
	t.Helper()
	appDSN, adminDSN := StartPostgres(t)
	db, err := tenantdb.Open(appDSN, adminDSN)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)

	migs := events.Migrations()
	for _, m := range mods {
		migs = append(migs, m.Migrations()...)
	}
	require.NoError(t, db.Migrate(ctx, migs))
	bad, err := db.LintRLS(ctx)
	require.NoError(t, err)
	require.Empty(t, bad, "tenant tables must carry forced RLS")

	bus, err := events.NewBus(StartNATS(t))
	require.NoError(t, err)
	t.Cleanup(bus.Close)

	deps := platform.Deps{DB: db, Bus: bus}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/v1", authn.Middleware(StaticVerifier(tokens)))
	for _, m := range mods {
		m.Routes(api, deps)
		require.NoError(t, bus.StartConsumers(ctx, db, m.Consumers(deps)))
	}
	go bus.RunDispatcher(ctx, db)
	return r, db, bus, ctx
}

// Do issues an authenticated JSON request against the harness router.
func Do(r *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}
```

- [ ] **Step 2: Migrate the five test packages**

In each of the four module_test.go files and journey_test.go: delete the local `staticVerifier`, `tenantA/tenantB` consts, `setup`, `do` (and pharmacy/lab's `busRef` — the harness returns the bus); replace calls with `r, db, ctx := harness()` style wrappers:

```go
func setup(t *testing.T) (*gin.Engine, *tenantdb.DB, *events.Bus, context.Context) {
	return testutil.ModuleHarness(t,
		map[string]string{"tokA": testutil.TenantA, "tokB": testutil.TenantB},
		pharmacy.New())
}
```

(each package keeps a thin `setup` returning exactly what its tests use; `do(...)` calls become `testutil.Do(...)` — a local `var do = testutil.Do` alias keeps diffs small). Reference's test also passes `"tokBad": "not-a-uuid"` in its token map. The journey test passes all three modules to one harness.

- [ ] **Step 3: Verify**

Run: `cd backend && go test -race ./internal/modules/... ./internal/journey/ ./internal/archtest/ && golangci-lint run ./...`
Expected: all PASS; net LOC drop (~160 lines of duplication gone).

- [ ] **Step 4: Commit**

```bash
git add backend/
git commit -m "refactor: shared module test harness replaces per-package scaffolding"
```

---

### Task 5: Observability — request IDs and standard log fields

**Files:**

- Create: `backend/internal/platform/requestid/requestid.go`, `backend/internal/platform/requestid/requestid_test.go`
- Modify: `backend/cmd/api/main.go` (mount middleware), `backend/pkg/events/bus.go` (consumer log fields)

**Interfaces:**

- Produces:
  - `requestid.Middleware() gin.HandlerFunc` — reads `X-Request-ID` or generates a UUID; sets it on the response header, the Gin context (key `requestid.Key = "request_id"`), and stores a request-scoped `*slog.Logger` (`slog.Default().With("request_id", id)`) retrievable via `requestid.Logger(c) *slog.Logger`.
  - Consumer logs in bus.go include `"consumer"`, `"event_id"`, `"tenant_id"` fields on the existing error paths (they already log consumer + event; add tenant).

- [ ] **Step 1: Write the failing test**

`backend/internal/platform/requestid/requestid_test.go`:

```go
package requestid_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/platform/requestid"
)

func TestMiddlewareGeneratesAndEchoesIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(requestid.Middleware())
	r.GET("/t", func(c *gin.Context) {
		require.NotNil(t, requestid.Logger(c))
		id := c.GetString(requestid.Key)
		require.NotEmpty(t, id)
		c.String(http.StatusOK, id)
	})

	// Generated when absent.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/t", nil))
	require.Equal(t, w.Body.String(), w.Header().Get("X-Request-ID"))
	require.NotEmpty(t, w.Body.String())

	// Propagated when present.
	w = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.Header.Set("X-Request-ID", "req-123")
	r.ServeHTTP(w, req)
	require.Equal(t, "req-123", w.Body.String())
	require.Equal(t, "req-123", w.Header().Get("X-Request-ID"))
}
```

- [ ] **Step 2: Run to verify it fails** — `cd backend && go test ./internal/platform/requestid/` → FAIL (package missing).

- [ ] **Step 3: Implement**

`backend/internal/platform/requestid/requestid.go`:

```go
// Package requestid tags every request with an id for log correlation.
package requestid

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const Key = "request_id"

const loggerKey = "request_logger"

func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
			id = uuid.NewString()
		}
		c.Set(Key, id)
		c.Set(loggerKey, slog.Default().With("request_id", id))
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

// Logger returns the request-scoped logger (falls back to the default).
func Logger(c *gin.Context) *slog.Logger {
	if v, ok := c.Get(loggerKey); ok {
		if l, ok := v.(*slog.Logger); ok {
			return l
		}
	}
	return slog.Default()
}
```

- [ ] **Step 4: Wire and extend**

`cmd/api/main.go`: after `srv := httpserver.New(...)` add `srv.Engine.Use(requestid.Middleware())` (import the package). In `pkg/events/bus.go` `handleMsg`'s two error logs (`consumer handle`, dead-letter/DLQ logs), add `"tenant_id", evt.TenantID` to the field list.

- [ ] **Step 5: Verify** — `cd backend && go test ./internal/platform/requestid/ -v && go test -race ./pkg/events/ && go build ./... && golangci-lint run ./...` → PASS/clean. Boot check: `(go run ./cmd/api & sleep 3; curl -si localhost:8080/healthz | grep -i x-request-id; kill %1)` → header present.

- [ ] **Step 6: Commit**

```bash
git add backend/
git commit -m "feat: request id middleware and tenant field on consumer logs"
```

---

### Task 6: Coverage gate

**Files:**

- Create: `backend/scripts/coverage-gate.sh`
- Modify: `.github/workflows/ci.yml` (go job), `Makefile` (`coverage-go` target)

**Interfaces:**

- Produces: `./scripts/coverage-gate.sh` (run from backend/) — runs `go test -race -coverprofile`, computes per-package coverage, exits 1 listing every `internal/modules/*` or `pkg/*` package under 70%. Exempt: `cmd/`, `internal/config`, `internal/archtest`, `internal/testutil`, `internal/journey`, `internal/platform/respond`? NO — respond is pkg-like internal code with tests; only the listed exemptions apply (`internal/platform/*` IS gated).

- [ ] **Step 1: Write `backend/scripts/coverage-gate.sh`**

```bash
#!/usr/bin/env bash
# Per-package coverage gate: internal/modules/* and pkg/* and
# internal/platform/* must be >= FLOOR. cmd/, config, archtest,
# testutil and journey are exempt (test-only or wiring).
set -euo pipefail
FLOOR="${COVERAGE_FLOOR:-70}"

go test -race -count=1 -coverprofile=/tmp/hms-cover.out ./... >/tmp/hms-cover-run.log 2>&1 || {
  cat /tmp/hms-cover-run.log
  exit 1
}

fail=0
while read -r pkg cov; do
  case "$pkg" in
    github.com/tesserix/hms/internal/modules/*|github.com/tesserix/hms/pkg/*|github.com/tesserix/hms/internal/platform*) ;;
    *) continue ;;
  esac
  pct="${cov%\%}"
  if awk "BEGIN{exit !($pct < $FLOOR)}"; then
    echo "FAIL coverage $pkg: ${pct}% < ${FLOOR}%"
    fail=1
  else
    echo "ok   coverage $pkg: ${pct}%"
  fi
done < <(go tool cover -func=/tmp/hms-cover.out >/dev/null 2>&1; go test -count=1 -cover ./... 2>/dev/null | awk '/coverage:/{print $2, $5}')

exit $fail
```

Note: the double-test-run above is wasteful — implement it as ONE run: parse the `ok  <pkg>  coverage: NN.N% of statements` lines from the `-cover` run directly:

```bash
#!/usr/bin/env bash
set -euo pipefail
FLOOR="${COVERAGE_FLOOR:-70}"
out=$(go test -race -count=1 -cover ./... 2>&1) || { echo "$out"; exit 1; }
echo "$out"
fail=0
while read -r pkg pct; do
  case "$pkg" in
    github.com/tesserix/hms/internal/modules/*|github.com/tesserix/hms/pkg/*|github.com/tesserix/hms/internal/platform/*) ;;
    *) continue ;;
  esac
  p="${pct%\%}"
  if awk "BEGIN{exit !($p < $FLOOR)}"; then
    echo "FAIL coverage $pkg: ${pct} < ${FLOOR}%"
    fail=1
  fi
done < <(echo "$out" | awk '$1=="ok" && /coverage:/{for(i=1;i<=NF;i++) if($i=="coverage:") print $2, $(i+1)}')
exit $fail
```

Use the second version. `chmod +x backend/scripts/coverage-gate.sh`.

- [ ] **Step 2: Run and fix any shortfalls**

Run: `cd backend && ./scripts/coverage-gate.sh`
Expected: likely PASS (modules are well-tested). If a gated package is under 70% (candidates: `internal/platform` registry, `internal/httpserver` — wait, httpserver is `internal/httpserver`, NOT gated; `internal/platform` root package IS gated): add the missing unit tests (e.g. registry duplicate-name test exists; httpserver ready-check tests if needed) rather than lowering the floor. Document actual numbers in the report.

- [ ] **Step 3: Wire CI + Makefile**

CI go job: replace `- run: go test -race ./...` with `- run: ./scripts/coverage-gate.sh`. Makefile:

```makefile
coverage-go:
	cd backend && ./scripts/coverage-gate.sh
```

- [ ] **Step 4: Commit**

```bash
git add backend/scripts/coverage-gate.sh .github/workflows/ci.yml Makefile backend/
git commit -m "feat: per-package coverage gate at 70 percent for modules and platform"
```

---

### Task 7: `make new-module` generator

**Files:**

- Create: `backend/scripts/new-module.sh`
- Modify: `Makefile` (`new-module` target)

**Interfaces:**

- Consumes: the final-state medicore module + test shapes (post Tasks 3–4: respond helpers, TenantPrincipal, testutil.ModuleHarness).
- Produces: `make new-module NAME=radiology` stamps `backend/internal/modules/radiology/{module.go,module_test.go}` with: `0001_radiology` migration creating `radiology_items` with full forced-RLS boilerplate + status CHECK column, routes (POST create → Accepted, GET list → OK with `{"data": ...}`, POST :id transition → guarded UPDATE with 404/409 via respond), a consumer stub named `radiology-intake` on a TODO-free placeholder subject `hms.in.radiology.item_created.v1`, publishing constant `SubjectItemCreated`, and tests using `testutil.ModuleHarness` covering create/list/isolation/transition-409. Prints follow-ups: register in `cmd/api/main.go` AND in `internal/archtest/arch_test.go` `allModules()`, rename the domain nouns, run the suite.

- [ ] **Step 1: Write the generator**

`backend/scripts/new-module.sh` — bash, no deps. Structure:

```bash
#!/usr/bin/env bash
set -euo pipefail
NAME="${1:?usage: new-module.sh <name>}"
[[ "$NAME" =~ ^[a-z][a-z0-9]*$ ]] || { echo "name must be lowercase alphanumeric, starting with a letter"; exit 1; }
DIR="internal/modules/$NAME"
[[ -e "$DIR" ]] && { echo "module $NAME already exists"; exit 1; }
mkdir -p "$DIR"
cat > "$DIR/module.go" <<EOF
...template...
EOF
cat > "$DIR/module_test.go" <<EOF
...template...
EOF
gofmt -w "$DIR"
echo "Created $DIR. Follow-ups:"
echo "1. Register ${NAME}.New() in cmd/api/main.go (registry loop)"
echo "2. Add ${NAME}.New() to allModules() in internal/archtest/arch_test.go"
echo "3. Rename the placeholder 'item' domain to your real nouns"
echo "4. cd backend && go test -race ./internal/modules/$NAME/ ./internal/archtest/"
```

The module.go template is the medicore/pharmacy shape with `item` as the entity: table `<name>_items` (`id`, `tenant_id`, `name text`, `status CHECK ('pending','done') DEFAULT 'pending'`, `done_at timestamptz`, `created_at`) + forced RLS + `(tenant_id, created_at DESC)` index; `SubjectItemCreated = "hms.in.<name>.item_created.v1"`; POST `/<name>/items` (binding `name` required max=200) → create + publish in one WithTenant tx → `respond.Accepted(c, gin.H{"id": ...})`; GET `/<name>/items` → newest-first LIMIT 100 → `respond.OK(c, gin.H{"data": rows})`; POST `/<name>/items/:id/done` → First (404 via `respond.NotFound(c, "item")`), status guard (409 `respond.Conflict(c, "already done")`), guarded UPDATE `WHERE id = ? AND status = 'pending'` with RowsAffected==0 → 409, publish only on success; consumer stub `<name>-intake` on `SubjectItemCreated` inserting nothing (body `return nil` with a comment telling the developer to replace it — the stub must still satisfy the arch-test naming rules).

The module_test.go template mirrors pharmacy's post-Task-4 shape via `testutil.ModuleHarness` with tests: create+list+tenant-isolation, transition 404 (cross-tenant), 409 on repeat, outbox row for the subject.

Write the FULL templates into the script at implementation time by copying the then-current medicore/pharmacy code and renaming; the shapes above are binding.

- [ ] **Step 2: Smoke test**

```bash
cd backend && ./scripts/new-module.sh testmod
go test -race ./internal/modules/testmod/
golangci-lint run ./internal/modules/testmod/
rm -rf internal/modules/testmod
git status --short   # only scripts/new-module.sh + Makefile
```

Expected: generated module compiles, its tests PASS against real containers, lint clean, cleanup complete. (No main.go registration needed for the smoke test — module tests run the harness directly.)

- [ ] **Step 3: Makefile target**

```makefile
new-module:
	cd backend && ./scripts/new-module.sh $(NAME)
```

- [ ] **Step 4: Commit**

```bash
git add backend/scripts/new-module.sh Makefile
git commit -m "feat: new-module generator stamping standards-compliant backend modules"
```

---

### Task 8: docs/standards/backend.md, CLAUDE.md section, hms-backend skill

**Files:**

- Create: `docs/standards/backend.md`, `.claude/skills/hms-backend/SKILL.md`
- Modify: `CLAUDE.md` (append backend section replacing the current single backend line)

**Interfaces:**

- Consumes: everything from Tasks 1–7 (cite real paths; verify each exists).
- Produces: the backend standards documentation set.

- [ ] **Step 1: Write `docs/standards/backend.md`**

Sections (expand each into binding prose + snippets, mirroring docs/standards/frontend.md's register):

1. Module anatomy — `internal/modules/<name>/module.go` implementing `platform.Module`; single package per module; registration in `cmd/api/main.go` AND `internal/archtest/arch_test.go`; `make new-module NAME=<name>` pointer.
2. Isolation rules — no cross-module imports (depguard + arch test); cross-module data via events only.
3. Data access — `WithTenant` only for tenant data, `WithSystem` only for platform tables; no exported raw `*gorm.DB`; forced-RLS checklist with the exact migration boilerplate block.
4. HTTP semantics — respond helpers table; 404-not-403 cross-tenant; 409 via guarded UPDATE (show the pharmacy snippet); 202 async creates; lists newest-first LIMIT 100; response shapes frozen.
5. Auth — `authn.TenantPrincipal` usage snippet; middleware chain.
6. Events — subject regex, versioning (breaking change ⇒ new `.vN` subject + new consumer), consumer naming, idempotency (processed_events claim is platform-provided; handlers run in the claim tx), DLQ behavior, outbox publishing inside the business tx.
7. Migrations — `NNNN_<module>` IDs, append-only, never edit applied migrations, arch test enforces uniqueness.
8. Logging — slog only (depguard-enforced), request-scoped logger via `requestid.Logger(c)`, standard fields (`request_id`, `module`, `event_id`, `tenant_id`), `%w` wrapping, no panic outside main.
9. Testing — `testutil.ModuleHarness` usage snippet; required cases per module (CRUD happy path, tenant isolation, cross-tenant 404, transition 409, outbox assertion); arch tests; coverage gate (70%, `./scripts/coverage-gate.sh`); commands (`make lint-go`, `make coverage-go`, `go test -race ./...`).
10. Checklist for a new module (10-line summary).

- [ ] **Step 2: Update `CLAUDE.md`**

Replace the existing single line "- Backend: modules under `backend/internal/modules/*` never import each other; tenant tables need forced RLS (see phase specs in docs/superpowers/specs/)." with:

```markdown
## Backend rules

Full document: docs/standards/backend.md

- New modules: `make new-module NAME=<name>`; register in cmd/api/main.go AND internal/archtest/arch_test.go allModules().
- Modules never import other modules (lint + arch-test enforced). Cross-module data flows via events only.
- Tenant data only via `WithTenant`; every tenant table gets the forced-RLS boilerplate; migration IDs `NNNN_<module>`, append-only.
- Handlers: `authn.TenantPrincipal(c)` for identity; `respond.*` helpers for every response; 404 (never 403) for cross-tenant, 409 via status-guarded UPDATE, 202 for async creates.
- Events: subjects `hms.<dir>.<module>.<event>.vN`; consumers `<module>-<purpose>`; publish through the outbox inside the business tx; handlers must be idempotent.
- slog only (logrus banned); request-scoped logger via `requestid.Logger(c)`; wrap errors with `%w`.
- Before done: `make lint-go` clean, `cd backend && ./scripts/coverage-gate.sh` green (70% floor), `go test -race ./...` green.
```

- [ ] **Step 3: Write `.claude/skills/hms-backend/SKILL.md`**

```markdown
---
name: hms-backend
description: Use when writing or modifying any HMS backend Go code (backend/**) — loads the binding standards, reference implementations, and enforcement gates.
---

# HMS backend standards

Read `docs/standards/backend.md` for the full rules. Quick table:

| Concern    | Rule                                                     | Reference                                        |
| ---------- | -------------------------------------------------------- | ------------------------------------------------ |
| New module | make new-module NAME=<n>; register in main.go + archtest | backend/scripts/new-module.sh                    |
| Isolation  | no cross-module imports; events only                     | backend/internal/archtest/arch_test.go           |
| Data       | WithTenant only; forced RLS boilerplate                  | backend/internal/modules/medicore/module.go      |
| HTTP       | respond.* helpers; 404 cross-tenant; 409 guarded UPDATE  | backend/internal/platform/respond/respond.go     |
| Auth       | authn.TenantPrincipal(c)                                 | backend/pkg/authn/authn.go                       |
| Events     | subject regex + outbox in business tx                    | backend/internal/modules/pharmacy/module.go      |
| Logging    | slog + requestid.Logger(c)                               | backend/internal/platform/requestid/requestid.go |
| Tests      | testutil.ModuleHarness per module                        | backend/internal/modules/pharmacy/module_test.go |
| Gates      | make lint-go; scripts/coverage-gate.sh; go test -race    | backend/.golangci.yml                            |

Never: logrus, panic outside main, raw gin.H error envelopes, cross-module imports, editing applied migrations, 403 for cross-tenant.
```

- [ ] **Step 4: Verify cited paths exist; run `pnpm turbo format:check` (docs are prettier-ignored? .prettierignore covers backend/ but docs/ is formatted — run `npx prettier --write docs/standards/backend.md CLAUDE.md .claude/skills/hms-backend/SKILL.md` then format:check)**

- [ ] **Step 5: Commit**

```bash
git add docs/standards CLAUDE.md .claude
git commit -m "docs: backend standards, agent rules and hms-backend skill"
```

---

### Task 9: Full verification

**Files:** none — verification + small fix-forwards only.

- [ ] **Step 1: Gates**

```bash
cd backend && golangci-lint run ./... && ./scripts/coverage-gate.sh && go test -race -count=1 ./...
cd .. && pnpm turbo lint format:check type-check test build
```

Expected: all green (frontend untouched — confirms no accidental damage).

- [ ] **Step 2: Live journey**

Infra containers shared/running (`make dev-infra` verifies; `make seed` idempotent). Start `make dev-api` + `pnpm turbo dev` in background shells, wait for readiness, then `cd e2e && npx playwright test` → journey passes (proves the respond/TenantPrincipal migration changed no observable behavior). Verify `curl -si localhost:8080/healthz | grep -i x-request-id` shows the new header. Kill background shells; leave infra running.

- [ ] **Step 3: Commit any fix-forwards** (`fix:` single-line), else nothing.

---

## Self-review notes

- Spec coverage: D1/deliverable 1 → Task 1; deliverable 2 → Task 2; 3 → Task 3; 4 → Task 4; 5 (observability) → Task 5; 6 (coverage) → Task 6; 7 (generator) → Task 7; 8 (docs/skill) → Task 8; verification → Task 9.
- Name consistency: `respond.OK/Created/Accepted/Error/NotFound/Conflict/BadRequest/Internal/Unauthenticated`, `authn.TenantPrincipal`, `testutil.StaticVerifier/ModuleHarness/Do/TenantA/TenantB`, `requestid.Middleware/Logger/Key`, `allModules()` — used identically across tasks.
- Frozen-shape constraint stated globally and re-stated in Task 3 (messages byte-identical).
- Known risk: golangci-lint v1-vs-v2 config schema (Task 1 Step 1 notes the adaptation); depguard prefix coarseness documented with the arch test as the precise layer.

# Foundation Hardening (Tier 0) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the eight Tier 0 findings from the foundation audit (#774) so patient registration (#70) is built on a foundation that fails loudly instead of silently.

**Architecture:** Three independent groups on one branch. Fail-fast guards refuse to start or refuse to die. RLS correctness moves the tenancy predicate into a function and inverts the lint so it protects every table rather than only the ones that remembered to opt in. The module template stops propagating three defects into the twenty-five modules that follow.

**Tech Stack:** Go 1.26, Gin, GORM, Postgres 16, NATS JetStream, testcontainers, bash.

## Global Constraints

- Spec: `docs/superpowers/specs/2026-08-12-foundation-hardening-design.md`. Read it before starting.
- Branch `feat/774-foundation-hardening` exists with the spec committed. Work there. Do not branch again.
- Commit messages: single line, conventional-commit prefix, **no signatures, no AI attribution, no Co-Authored-By trailer**.
- Backend standard: `docs/standards/backend.md` is binding. Migrations are **append-only** — never edit an applied migration; add a new one.
- Migration IDs are `NNNN_<owner>`, globally unique, enforced by `TestMigrationIDsAreGloballyUnique`.
- `make lint-go` must stay at 0 issues; `cd backend && ./scripts/coverage-gate.sh` must stay green (70% floor); `go test -race ./...` must pass.
- Modules must not import each other — enforced by `TestModulesDoNotImportEachOther` and a depguard rule. `pkg/*` and `internal/platform/*` are not modules and may be imported freely.
- Tests need Docker (testcontainers). The dev stack may occupy ports; tests use their own containers.
- `HELIVANTA_ENV` defaults to `production`. Dev opts in explicitly.

---

### Task 1: An environment concept

**Files:**
- Modify: `backend/internal/config/config.go`
- Test: `backend/internal/config/config_test.go` (create)
- Modify: `Makefile` (repo root)

**Interfaces:**
- Consumes: nothing.
- Produces: `config.Config.Env string`, `func (c Config) IsDev() bool`. Tasks 2 and 3 rely on `IsDev()`.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/config/config_test.go`:

```go
package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/hms/internal/config"
)

// The default must be production. A guard that defaults to permissive
// protects nothing, because the deployment that forgets to set the
// variable is exactly the one that needed protecting.
func TestEnvDefaultsToProduction(t *testing.T) {
	t.Setenv("HELIVANTA_ENV", "")
	cfg := config.Load()
	require.Equal(t, "production", cfg.Env)
	require.False(t, cfg.IsDev())
}

func TestIsDevOnlyForExactDev(t *testing.T) {
	for _, tc := range []struct {
		env   string
		isDev bool
	}{
		{"dev", true},
		{"development", false},
		{"Dev", false},
		{"production", false},
		{"staging", false},
	} {
		t.Setenv("HELIVANTA_ENV", tc.env)
		require.Equal(t, tc.isDev, config.Load().IsDev(), "HELIVANTA_ENV=%q", tc.env)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd backend && go test ./internal/config/ -run TestEnv -v`
Expected: FAIL — `cfg.Env` undefined.

- [ ] **Step 3: Implement**

In `backend/internal/config/config.go`, add `Env` as the first field of `Config`, load it, and add the helper:

```go
type Config struct {
	Env              string
	Port             string
	// ...unchanged...
}
```

In `Load()`, as the first entry:

```go
		Env: getenv("HELIVANTA_ENV", "production"),
```

At the end of the file:

```go
// IsDev reports whether this process is running in a developer
// environment. It defaults to false: the guards that consult it disable
// production safety checks, so an unset or misspelled HELIVANTA_ENV must fail
// closed rather than silently unlock them.
func (c Config) IsDev() bool { return c.Env == "dev" }
```

- [ ] **Step 4: Run the tests**

Run: `cd backend && go test ./internal/config/ -v`
Expected: PASS

- [ ] **Step 5: Set HELIVANTA_ENV=dev for the dev targets**

In the repo-root `Makefile`, `dev-api` currently reads:

```make
dev-api:
	cd backend && FIREBASE_AUTH_EMULATOR_HOST=$${FIREBASE_AUTH_EMULATOR_HOST:-localhost:$(HELIVANTA_GIP_PORT)} PORT=$${PORT:-$(HELIVANTA_API_PORT)} go run ./cmd/api
```

Add `HELIVANTA_ENV`:

```make
# HELIVANTA_ENV=dev is required here: the API refuses to start with
# FIREBASE_AUTH_EMULATOR_HOST set outside dev, because the emulator makes
# ID token signature verification a no-op.
dev-api:
	cd backend && HELIVANTA_ENV=$${HELIVANTA_ENV:-dev} FIREBASE_AUTH_EMULATOR_HOST=$${FIREBASE_AUTH_EMULATOR_HOST:-localhost:$(HELIVANTA_GIP_PORT)} PORT=$${PORT:-$(HELIVANTA_API_PORT)} go run ./cmd/api
```

- [ ] **Step 6: Commit**

```bash
git add backend/internal/config/config.go backend/internal/config/config_test.go Makefile
git commit -m "feat: HELIVANTA_ENV defaulting to production, with IsDev for the safety guards"
```

---

### Task 2: Refuse the auth emulator outside dev

**Files:**
- Modify: `backend/pkg/authn/gip.go`
- Test: `backend/pkg/authn/gip_test.go` (create, or append if it exists)
- Modify: `backend/cmd/api/main.go`
- Modify: `apps/shell/lib/firebase.ts`

**Interfaces:**
- Consumes: `config.Config.IsDev()` from Task 1.
- Produces: `authn.NewGIPVerifier(ctx context.Context, projectID string, allowEmulator bool) (TokenVerifier, error)` and `authn.NewGIPMinter(ctx context.Context, projectID string, allowEmulator bool) (TokenMinter, error)`. Both signatures gain the third parameter.

- [ ] **Step 1: Write the failing test**

Create `backend/pkg/authn/gip_test.go` (if the file exists, append the two functions):

```go
package authn_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/hms/pkg/authn"
)

// firebase-admin-go checks FIREBASE_AUTH_EMULATOR_HOST when the client is
// constructed; when it is set, VerifyIDToken skips signature verification
// entirely and trusts the decoded claims. A forged token would be
// accepted. Constructing a verifier must therefore refuse outside dev.
func TestNewGIPVerifierRefusesEmulatorOutsideDev(t *testing.T) {
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", "localhost:9099")

	_, err := authn.NewGIPVerifier(context.Background(), "demo-hms", false)

	require.Error(t, err)
	require.Contains(t, err.Error(), "FIREBASE_AUTH_EMULATOR_HOST")
}

func TestNewGIPMinterRefusesEmulatorOutsideDev(t *testing.T) {
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", "localhost:9099")

	_, err := authn.NewGIPMinter(context.Background(), "demo-hms", false)

	require.Error(t, err)
	require.Contains(t, err.Error(), "FIREBASE_AUTH_EMULATOR_HOST")
}

// With the emulator explicitly allowed the guard must not fire. The
// constructor may still fail for unrelated reasons in a sandbox, so this
// asserts only that the failure is not the guard.
func TestNewGIPVerifierAllowsEmulatorInDev(t *testing.T) {
	t.Setenv("FIREBASE_AUTH_EMULATOR_HOST", "localhost:9099")

	_, err := authn.NewGIPVerifier(context.Background(), "demo-hms", true)

	if err != nil {
		require.False(t, strings.Contains(err.Error(), "FIREBASE_AUTH_EMULATOR_HOST"),
			"guard fired despite allowEmulator=true: %v", err)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `cd backend && go test ./pkg/authn/ -run TestNewGIP -v`
Expected: FAIL — too many arguments to `NewGIPVerifier`.

- [ ] **Step 3: Implement the guard**

In `backend/pkg/authn/gip.go`, add `"os"` to the imports, change both constructors to take `allowEmulator bool` and pass it through, and add the check to `newAuthClient`:

```go
func NewGIPVerifier(ctx context.Context, projectID string, allowEmulator bool) (TokenVerifier, error) {
	client, err := newAuthClient(ctx, projectID, allowEmulator)
	if err != nil {
		return nil, err
	}
	return &gipVerifier{client: client}, nil
}

func NewGIPMinter(ctx context.Context, projectID string, allowEmulator bool) (TokenMinter, error) {
	client, err := newAuthClient(ctx, projectID, allowEmulator)
	if err != nil {
		return nil, err
	}
	return &gipMinter{client: client}, nil
}

func newAuthClient(ctx context.Context, projectID string, allowEmulator bool) (*auth.Client, error) {
	// The Admin SDK reads this variable at construction. When it is set,
	// VerifyIDToken decodes the JWT and trusts it — no RSA signature
	// check at all. Reaching production, that turns "forge a token with
	// any tenant_id" into full access to every tenant's data, with a
	// clean boot and a green readiness probe. Refuse instead.
	if host := os.Getenv("FIREBASE_AUTH_EMULATOR_HOST"); host != "" && !allowEmulator {
		return nil, fmt.Errorf(
			"authn: FIREBASE_AUTH_EMULATOR_HOST=%q is set outside a dev environment; "+
				"the emulator makes ID token signature verification a no-op, so any "+
				"forged token would be accepted. Unset it, or set HELIVANTA_ENV=dev", host)
	}
	app, err := firebase.NewApp(ctx, &firebase.Config{ProjectID: projectID})
	if err != nil {
		return nil, fmt.Errorf("firebase app: %w", err)
	}
	client, err := app.Auth(ctx)
	if err != nil {
		return nil, fmt.Errorf("firebase auth client: %w", err)
	}
	return client, nil
}
```

Also update the two doc comments on the constructors that currently say the emulator is honoured "automatically" — they now describe a guarded opt-in.

- [ ] **Step 4: Run the tests**

Run: `cd backend && go test ./pkg/authn/ -v`
Expected: PASS

- [ ] **Step 5: Update the call sites**

In `backend/cmd/api/main.go`, find the `NewGIPVerifier` and `NewGIPMinter` calls and pass `cfg.IsDev()` as the third argument. Build to find any other call sites:

Run: `cd backend && go build ./... 2>&1 | head`
Fix every reported call site the same way.

- [ ] **Step 6: Remove the implicit emulator fallback in the shell**

`apps/shell/lib/firebase.ts` currently infers the emulator from `NODE_ENV`:

```ts
  const emulator =
    process.env.NEXT_PUBLIC_AUTH_EMULATOR_HOST ??
    (process.env.NODE_ENV !== "production" ? "localhost:9099" : undefined);
```

Replace with an explicit-only form:

```ts
  // Explicit only — never inferred from NODE_ENV. A production build with
  // a non-production NODE_ENV would otherwise silently point sign-in at a
  // local emulator. `make up` sets this variable for dev.
  const emulator = process.env.NEXT_PUBLIC_AUTH_EMULATOR_HOST;
```

- [ ] **Step 7: Verify the frontend still builds and its tests pass**

Run: `pnpm turbo type-check test --filter=@hms/shell`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add backend/pkg/authn/gip.go backend/pkg/authn/gip_test.go backend/cmd/api/main.go apps/shell/lib/firebase.ts
git commit -m "fix: refuse to build a GIP client with the auth emulator outside dev"
```

---

### Task 3: Refuse an app pool that can bypass RLS

**Files:**
- Modify: `backend/pkg/tenantdb/db.go`
- Test: `backend/pkg/tenantdb/db_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `tenantdb.Open` gains a failure mode. No signature change.

- [ ] **Step 1: Write the failing test**

Read the top of `backend/pkg/tenantdb/db_test.go` to see how it obtains its DSNs (it uses the shared testcontainers helper). Add a test that passes the **admin** DSN as the app DSN:

```go
// Tenant isolation rests entirely on APP_DATABASE_URL naming a role that
// cannot bypass RLS. Nothing asserted that at runtime, and the two
// default DSNs differ only by username — so pasting the admin URL into
// APP_DATABASE_URL silently disabled isolation with every test still
// green. Open must refuse.
func TestOpenRefusesAppPoolThatCanBypassRLS(t *testing.T) {
	adminDSN := <the admin DSN this file already builds>

	_, err := tenantdb.Open(adminDSN, adminDSN)

	require.Error(t, err)
	require.Contains(t, err.Error(), "bypass")
}
```

Replace `<the admin DSN this file already builds>` with whatever the existing setup in this file returns — do not invent a new container.

- [ ] **Step 2: Run it to verify it fails**

Run: `cd backend && go test ./pkg/tenantdb/ -run TestOpenRefuses -v`
Expected: FAIL — `Open` returns no error.

- [ ] **Step 3: Implement**

In `backend/pkg/tenantdb/db.go`, add the probe and call it from `Open` for the **app pool only** — the admin pool is meant to be privileged:

```go
// assertNoRLSBypass fails when the pool's role can see through row-level
// security. FORCE ROW LEVEL SECURITY already covers the table-owner case;
// this covers BYPASSRLS and superuser, and together they close both.
//
// Without it, an APP_DATABASE_URL naming the admin role serves every
// tenant's data from every endpoint, while every test passes and LintRLS
// stays green — it inspects table metadata, not the connecting role.
func assertNoRLSBypass(g *gorm.DB) error {
	var caps struct {
		Bypass bool
		Super  bool
	}
	err := g.Raw(`SELECT rolbypassrls AS bypass, rolsuper AS super
		FROM pg_roles WHERE rolname = current_user`).Scan(&caps).Error
	if err != nil {
		return fmt.Errorf("probe app role capabilities: %w", err)
	}
	if caps.Bypass || caps.Super {
		return fmt.Errorf("tenantdb: APP_DATABASE_URL connects as a role that can bypass "+
			"row-level security (bypassrls=%v superuser=%v); tenant isolation would be "+
			"silently disabled. Point it at the non-superuser application role", caps.Bypass, caps.Super)
	}
	return nil
}
```

In `Open`, after the pool-sizing loop and before the `return`:

```go
	if err := assertNoRLSBypass(app); err != nil {
		return nil, err
	}
	return &DB{app: app, admin: admin}, nil
```

- [ ] **Step 4: Run the tests**

Run: `cd backend && go test ./pkg/tenantdb/ -v`
Expected: PASS — including the existing isolation tests, which use the correct `hms_app` role.

- [ ] **Step 5: Confirm nothing else opened with the wrong role**

Run: `cd backend && go test ./... 2>&1 | grep -E "FAIL|bypass" | head`
Expected: no `bypass` failures. If a test fails here, it was opening the app pool with an admin DSN and the guard has found a real problem — fix the test's DSN, not the guard.

- [ ] **Step 6: Commit**

```bash
git add backend/pkg/tenantdb/db.go backend/pkg/tenantdb/db_test.go
git commit -m "fix: refuse an app database pool whose role can bypass row-level security"
```

---

### Task 4: A panicking consumer must not kill the API

**Files:**
- Modify: `backend/pkg/events/bus.go`
- Test: `backend/pkg/events/panic_test.go` (create)

**Interfaces:**
- Consumes: nothing.
- Produces: no exported change.

- [ ] **Step 1: Write the failing test**

Read `backend/pkg/events/dlq_test.go` first — it already sets up a bus, a failing consumer and a DLQ subscription, and this test follows the same shape. Create `backend/pkg/events/panic_test.go`:

```go
package events_test

// A consumer handler panicking must dead-letter one event, not terminate
// the process. GORM's Transaction recovers and re-panics, and
// gin.Recovery() covers only HTTP handlers, so before this guard a
// malformed-but-parseable payload took down every module for every
// tenant. If the recover is missing, this test does not fail — the test
// binary crashes, which is the point.
func TestPanickingConsumerDoesNotKillTheProcess(t *testing.T) {
	// Set up exactly as dlq_test.go does, but register a consumer whose
	// Handle panics instead of returning an error:
	//
	//   Handle: func(ctx context.Context, tx *gorm.DB, evt events.Event) error {
	//       panic("boom")
	//   }
	//
	// Then publish one event through the outbox, drive the dispatcher,
	// and assert:
	//   1. the test reaches its assertions at all (no crash), and
	//   2. the event is eventually dead-lettered on hms.dlq.<consumer>,
	//      using the same DLQ subscription dlq_test.go sets up.
}
```

Write it out fully, mirroring `dlq_test.go`'s setup and timeouts rather than inventing new ones.

- [ ] **Step 2: Run it to verify it fails**

Run: `cd backend && go test ./pkg/events/ -run TestPanickingConsumer -v`
Expected: the test binary **panics and crashes** (`panic: boom`) rather than reporting a normal failure. That crash is the bug being fixed.

- [ ] **Step 3: Implement**

In `backend/pkg/events/bus.go`, add `"runtime/debug"` to the imports.

Add a recover at the top of `handleMsg`:

```go
func (b *Bus) handleMsg(ctx context.Context, db OutboxStore, c Consumer, msg *nats.Msg) {
	// A handler panic must cost one event, not the process. Nak so
	// redelivery and eventually the DLQ take over.
	defer func() {
		if r := recover(); r != nil {
			slog.Error("consumer panic", "consumer", c.Name,
				"panic", fmt.Sprint(r), "stack", string(debug.Stack()))
			_ = msg.Nak()
		}
	}()
	var evt Event
	// ...rest unchanged...
```

Add `"fmt"` to the imports if it is not already there.

Wrap the dispatcher's drain the same way. Replace the `case <-ticker.C:` body in `RunDispatcher`:

```go
		case <-ticker.C:
			b.drainSafely(loopCtx, db)
```

and add:

```go
// drainSafely runs one drain pass, containing a panic to this tick. The
// dispatcher goroutine is started with a bare `go` in main, so a panic
// here would otherwise terminate the process.
func (b *Bus) drainSafely(ctx context.Context, db OutboxStore) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("outbox dispatch panic", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	if err := b.drainOnce(ctx, db); err != nil {
		slog.Error("outbox dispatch", "err", err)
	}
}
```

- [ ] **Step 4: Run the tests**

Run: `cd backend && go test ./pkg/events/ -race -v`
Expected: PASS, no crash.

- [ ] **Step 5: Commit**

```bash
git add backend/pkg/events/bus.go backend/pkg/events/panic_test.go
git commit -m "fix: contain consumer and dispatcher panics instead of killing the api"
```

---

### Task 5: The tenancy predicate becomes a function

**Files:**
- Create: `backend/pkg/tenantdb/migrations.go`
- Create: `backend/internal/bootstrap/migrations.go`
- Modify: `backend/cmd/api/main.go`, `backend/cmd/migrate/main.go`, `backend/internal/testutil/harness.go`, `backend/internal/archtest/arch_test.go`, `backend/internal/archtest/rls_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: SQL function `hms_tenant_visible(row_tenant uuid) RETURNS boolean`; `tenantdb.Migrations() []Migration` with ID `0001_platform_rls`; `bootstrap.PlatformMigrations() []tenantdb.Migration`. Task 6 uses the function; Task 7 requires it in the lint.

- [ ] **Step 1: Add the platform migration**

Create `backend/pkg/tenantdb/migrations.go`:

```go
package tenantdb

// Migrations returns the platform-owned schema this package requires.
//
// The tenancy predicate lives in a function rather than inlined into every
// policy so that widening it later — a hospital-group tenant seeing its
// member hospitals — is one function replacement instead of one ALTER
// POLICY per table across every module, with no way to verify completeness.
// Migrations are append-only, so that retrofit is exactly the kind of
// change that cannot be made cheaply after the fact.
//
// pkg/events owns 0001_events_outbox on the same principle: a pkg package
// may own a namespaced migration.
func Migrations() []Migration {
	return []Migration{{
		ID: "0001_platform_rls",
		SQL: `
			CREATE FUNCTION hms_tenant_visible(row_tenant uuid) RETURNS boolean
			  LANGUAGE sql STABLE PARALLEL SAFE AS
			$$ SELECT row_tenant = current_setting('app.tenant_id', true)::uuid $$;`,
	}}
}
```

- [ ] **Step 2: Add the accumulator**

Create `backend/internal/bootstrap/migrations.go`:

```go
package bootstrap

import (
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
)

// PlatformMigrations returns the migrations owned by platform packages, in
// apply order, before any module's.
//
// It exists because events.Migrations() was hand-written in five places.
// Adding a second platform source meant remembering all five: missing the
// test harness breaks every module test at db.Migrate, and missing the RLS
// arch test silently stops the lint covering the new schema.
func PlatformMigrations() []tenantdb.Migration {
	migs := tenantdb.Migrations()
	return append(migs, events.Migrations()...)
}
```

- [ ] **Step 3: Route every call site through it**

Replace `events.Migrations()` with `bootstrap.PlatformMigrations()` in:

- `backend/cmd/api/main.go` — `migs := events.Migrations()`
- `backend/cmd/migrate/main.go` — `migs := events.Migrations()`
- `backend/internal/testutil/harness.go` — `migs := events.Migrations()`
- `backend/internal/archtest/rls_test.go` — `migs := events.Migrations()`

Fix the now-unused `events` imports.

`backend/internal/archtest/arch_test.go:86` is different — it iterates `events.Migrations()` to register IDs under an owner label for the uniqueness test. Add `tenantdb.Migrations()` there under the owner `"tenantdb"`, alongside the existing `"events"` registration, rather than switching it to the accumulator: that test deliberately enumerates sources itself.

Leave `backend/pkg/events/bus_test.go` alone — it tests `events` in isolation and should not depend on `bootstrap`.

- [ ] **Step 4: Verify migrations apply and IDs stay unique**

Run: `cd backend && go test ./internal/archtest/ -v`
Expected: PASS, including `TestMigrationIDsAreGloballyUnique`.

Run: `cd backend && go test ./internal/modules/... 2>&1 | tail -8`
Expected: PASS — the harness now applies the new platform migration before every module test.

- [ ] **Step 5: Commit**

```bash
git add backend/pkg/tenantdb/migrations.go backend/internal/bootstrap/migrations.go backend/cmd backend/internal/testutil/harness.go backend/internal/archtest
git commit -m "feat: hms_tenant_visible predicate function and one platform migration accumulator"
```

---

### Task 6: Every module adopts the function

**Files:**
- Modify: `backend/internal/modules/{iam,medicore,pharmacy,lab,reference}/module.go`

**Interfaces:**
- Consumes: `hms_tenant_visible` from Task 5.
- Produces: every existing tenant table's policy uses the function in `USING`. Task 7's lint depends on this being complete.

- [ ] **Step 1: Enumerate the tables**

Do not trust a list from memory. For each module, read its existing `Migrations()` and write down every `CREATE TABLE` that has a `tenant_id`:

Run: `cd backend && grep -n "CREATE TABLE" internal/modules/*/module.go`

Expected, for reference: `iam_members`, `medicore_visits`, `pharmacy_medications`, `pharmacy_dispenses`, `lab_orders`, `reference_pings`, and `reference_ping_receipts` (which has **no** `tenant_id` — handled in Step 3).

- [ ] **Step 2: Add a policy migration per module**

For each of `iam`, `medicore`, `pharmacy`, `lab`, append a migration to that module's `Migrations()` slice. Use the next free number for that module — `0002_<module>` for all four, since each currently has only `0001_<module>`. For example, in `medicore`:

```go
		{
			ID: "0002_medicore",
			// USING moves to the shared predicate so group-tenant
			// visibility can later be enabled in one place. WITH CHECK
			// deliberately stays pinned to strict equality: reads may
			// widen, writes must always land in exactly one tenant.
			SQL: `
				ALTER POLICY tenant_isolation ON medicore_visits
				  USING (hms_tenant_visible(tenant_id))
				  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);`,
		},
```

`pharmacy`'s migration carries two `ALTER POLICY` statements, one per table. Keep each module's SQL to its own tables — a module altering another's policy would couple the schemas even though the arch test only checks Go imports.

- [ ] **Step 3: Give `reference_ping_receipts` a tenant**

It is a per-tenant idempotency ledger with no `tenant_id`, which is why the current lint cannot see it. In `reference`'s new `0002_reference` migration, before its `ALTER POLICY` statements:

```sql
ALTER TABLE reference_ping_receipts ADD COLUMN tenant_id uuid;

-- Derive the tenant from the ping the receipt refers to. Receipts whose
-- ping no longer exists cannot be attributed to a tenant and are dropped;
-- they are consumer bookkeeping, not clinical data.
UPDATE reference_ping_receipts r
   SET tenant_id = p.tenant_id
  FROM reference_pings p
 WHERE p.id = r.ping_id;
DELETE FROM reference_ping_receipts WHERE tenant_id IS NULL;

ALTER TABLE reference_ping_receipts ALTER COLUMN tenant_id SET NOT NULL;
ALTER TABLE reference_ping_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE reference_ping_receipts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON reference_ping_receipts
  USING (hms_tenant_visible(tenant_id))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
CREATE INDEX ON reference_ping_receipts (tenant_id, processed_at DESC);
```

Then update the module's consumer insert so it writes `tenant_id`. Find it with:

Run: `cd backend && grep -n "reference_ping_receipts" internal/modules/reference/module.go`

The insert must include `tenant_id` sourced from the event's tenant — the consumer transaction already sets `app.tenant_id`, but the column is `NOT NULL` and must be written explicitly.

- [ ] **Step 4: Verify**

Run: `cd backend && go test ./internal/modules/... ./internal/archtest/ 2>&1 | tail -10`
Expected: PASS. The existing RLS lint still passes (it does not yet require the function — that is Task 7).

- [ ] **Step 5: Prove the policies actually changed**

Run: `cd backend && go test ./internal/archtest/ -run TestAllMigrationsPassRLSLint -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add backend/internal/modules
git commit -m "feat: adopt the hms_tenant_visible predicate and scope reference receipts by tenant"
```

---

### Task 7: Invert the RLS lint

**Files:**
- Modify: `backend/pkg/tenantdb/db.go`
- Test: `backend/pkg/tenantdb/db_test.go`

**Interfaces:**
- Consumes: Tasks 5 and 6 — every existing tenant table must already use the function, or this task's own test suite fails.
- Produces: `LintRLS` keeps its `([]string, error)` signature; the strings become `"<table>: <reason>"`.

- [ ] **Step 1: Write the failing tests**

`db_test.go` already has a group of `naughty_*` tables asserting the lint catches missing/partial policies (`naughty_none`, `naughty_not_forced`, `naughty_using_only`, `naughty_check_only`). Add two more cases in the same style:

```go
// The failure a hurried module author actually makes: forget tenant_id
// entirely. The old lint could not see this — it only inspected tables
// that already had the column — so the table had no tenancy, no RLS and
// no policy, and every check passed green.
CREATE TABLE naughty_no_tenant_column (id uuid PRIMARY KEY, note text);

// A policy that inlines the old predicate instead of calling the shared
// function. Allowed to exist, it would silently opt out of any future
// group-visibility change.
CREATE TABLE naughty_inlined_predicate (id uuid PRIMARY KEY, tenant_id uuid NOT NULL);
ALTER TABLE naughty_inlined_predicate ENABLE ROW LEVEL SECURITY;
ALTER TABLE naughty_inlined_predicate FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON naughty_inlined_predicate
  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
```

Assert both table names appear in the lint's output, following the assertion style already used for the existing naughty tables.

- [ ] **Step 2: Run to verify they fail**

Run: `cd backend && go test ./pkg/tenantdb/ -run TestLint -v`
Expected: FAIL — neither new table is reported.

- [ ] **Step 3: Implement the inversion**

Replace `LintRLS` in `backend/pkg/tenantdb/db.go`:

```go
// lintAllowlist names the tables that legitimately carry no tenant_id.
//
// outbox_events is here under protest: it holds event payloads that today
// include patient names, outside any RLS boundary, readable by any
// WithSystem transaction. Giving it a tenant_id changes the dispatcher's
// access path, so it is tracked separately on #774 rather than fixed here.
var lintAllowlist = map[string]bool{
	"schema_migrations": true,
	"outbox_events":     true,
	"processed_events":  true,
}

// LintRLS returns every table that is not properly tenant-isolated, each
// as "<table>: <reason>".
//
// It enumerates all tables and subtracts an allowlist, rather than
// inspecting only tables that already have a tenant_id. The old direction
// protected the tables someone remembered to mark; this one protects
// everything, and a module that simply forgets the column now fails.
func (d *DB) LintRLS(ctx context.Context) ([]string, error) {
	type row struct {
		Relname      string
		HasTenant    bool
		RLS          bool
		Forced       bool
		HasPolicy    bool
		UsesFunction bool
	}
	var rows []row
	err := d.admin.WithContext(ctx).Raw(`
		SELECT c.relname,
		       EXISTS (SELECT 1 FROM information_schema.columns col
		               WHERE col.table_schema = 'public'
		                 AND col.table_name = c.relname
		                 AND col.column_name = 'tenant_id') AS has_tenant,
		       c.relrowsecurity        AS rls,
		       c.relforcerowsecurity   AS forced,
		       EXISTS (SELECT 1 FROM pg_policies p
		               WHERE p.schemaname = 'public' AND p.tablename = c.relname
		                 AND p.qual IS NOT NULL AND p.with_check IS NOT NULL) AS has_policy,
		       EXISTS (SELECT 1 FROM pg_policies p
		               WHERE p.schemaname = 'public' AND p.tablename = c.relname
		                 AND p.qual LIKE '%hms_tenant_visible%') AS uses_function
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind = 'r'
		ORDER BY c.relname`).Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	var bad []string
	for _, r := range rows {
		if lintAllowlist[r.Relname] {
			continue
		}
		switch {
		case !r.HasTenant:
			bad = append(bad, r.Relname+": no tenant_id column")
		case !r.RLS || !r.Forced:
			bad = append(bad, r.Relname+": row-level security not enabled and forced")
		case !r.HasPolicy:
			bad = append(bad, r.Relname+": no policy with both USING and WITH CHECK")
		case !r.UsesFunction:
			bad = append(bad, r.Relname+": USING does not call hms_tenant_visible")
		}
	}
	return bad, nil
}
```

- [ ] **Step 4: Run the tests**

Run: `cd backend && go test ./pkg/tenantdb/ -v`
Expected: PASS — all six naughty tables reported, the legitimate ones not.

- [ ] **Step 5: Run everything that calls the lint**

Run: `cd backend && go test ./internal/... 2>&1 | tail -10`
Expected: PASS. The lint runs at every module harness setup and in `TestAllMigrationsPassRLSLint`, so a real gap anywhere surfaces here. If a module table fails, Task 6 missed it — fix the migration, not the lint.

- [ ] **Step 6: Commit**

```bash
git add backend/pkg/tenantdb/db.go backend/pkg/tenantdb/db_test.go
git commit -m "feat: lint every table for tenant isolation instead of only the marked ones"
```

---

### Task 8: Errors stop being discarded

**Files:**
- Modify: `backend/internal/platform/respond/respond.go`
- Modify: all five `backend/internal/modules/*/module.go` (and `iam/me.go`, `iam/roles.go` if they call `respond.Internal`)
- Modify: `backend/scripts/new-module.sh`

**Interfaces:**
- Consumes: nothing.
- Produces: `respond.InternalErr(c *gin.Context, err error, message string)`. `respond.Internal` is **deleted** — Task 9 and Task 10 must use `InternalErr`.

- [ ] **Step 1: Add the helper and delete the old one**

In `backend/internal/platform/respond/respond.go`, add the `requestid` import and replace `Internal`:

```go
// InternalErr logs the underlying cause against the request id and returns
// the client-safe message.
//
// It replaces a plain Internal(c, msg): 15 call sites discarded their
// error, so a production 500 gave the client a generic string and the
// operator nothing — no error text, no SQLSTATE, no request id. The
// discarding form is deliberately not offered, so it cannot come back.
func InternalErr(c *gin.Context, err error, message string) {
	requestid.Logger(c).ErrorContext(c.Request.Context(), message, "err", err)
	Error(c, http.StatusInternalServerError, "internal", message)
}
```

Delete the `Internal` function entirely.

- [ ] **Step 2: Verify the compiler finds every call site**

Run: `cd backend && go build ./... 2>&1 | tee /tmp/internal-sites.txt | head -20`
Expected: compile errors at each `respond.Internal` call — 15 across the modules.

- [ ] **Step 3: Convert every site**

Each site currently looks like:

```go
		if err != nil {
			respond.Internal(c, "could not create visit")
			return
		}
```

Becomes:

```go
		if err != nil {
			respond.InternalErr(c, err, "could not create visit")
			return
		}
```

Keep the existing message text unchanged — the strings are user-facing and some are asserted in tests. Work through `/tmp/internal-sites.txt` until the build is clean.

- [ ] **Step 4: Update the generator**

In `backend/scripts/new-module.sh`, the template has three `respond.Internal(` calls (around lines 152, 168, 219). Convert each to `respond.InternalErr(c, err, "...")` with the same message.

- [ ] **Step 5: Verify**

Run: `cd backend && go build ./... && go test ./internal/... 2>&1 | tail -8`
Expected: build clean, tests PASS.

Run: `cd backend && grep -rn "respond.Internal(" --include='*.go' . ; grep -n "respond.Internal(" scripts/new-module.sh`
Expected: no output from either — the discarding form is gone.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/platform/respond/respond.go backend/internal/modules backend/scripts/new-module.sh
git commit -m "feat: respond.InternalErr logging the cause, replacing the discarding form"
```

---

### Task 9: Handlers become named methods

**Files:**
- Modify: `backend/internal/modules/{iam,medicore,pharmacy,lab,reference}/`, splitting each into `module.go` plus one file per aggregate

**Interfaces:**
- Consumes: `respond.InternalErr` from Task 8.
- Produces: no exported change. Each module's `Routes` becomes a route table; handlers become methods.

- [ ] **Step 1: Convert `pharmacy` first, as the exemplar**

`pharmacy` is the fullest module — create, list, a guarded transition, and a consumer. Create `backend/internal/modules/pharmacy/medications.go` and `dispenses.go`, moving each closure body into a named method on a small struct that holds only what those handlers use:

```go
// dispenses.go
package pharmacy

type dispenseHandlers struct {
	db  *tenantdb.DB
	bus *events.Bus
}

// list returns this tenant's dispenses, newest first.
func (h *dispenseHandlers) list(c *gin.Context) {
	// ...body moved verbatim from the closure, with deps.DB -> h.db...
}

// fulfil marks a pending dispense dispensed. The guarded UPDATE is what
// makes concurrent fulfilment safe: the loser matches zero rows and gets
// a 409 rather than double-dispensing.
func (h *dispenseHandlers) fulfil(c *gin.Context) {
	// ...body moved verbatim...
}
```

`module.go`'s `Routes` becomes a table:

```go
func (m *Module) Routes(r *platform.Router, deps platform.Deps) {
	g := r.Group("/pharmacy")
	meds := &medicationHandlers{db: deps.DB}
	disp := &dispenseHandlers{db: deps.DB, bus: deps.Bus}

	g.POST("/medications", PermMedicationWrite, meds.create)
	g.GET("/medications", PermMedicationRead, meds.list)
	g.GET("/dispenses", PermDispenseRead, disp.list)
	g.POST("/dispenses/:id/dispense", PermDispenseFulfil, disp.fulfil)
}
```

Move bodies **verbatim** apart from the `deps.X` → `h.x` substitution. This task changes structure, not behaviour.

- [ ] **Step 2: Verify pharmacy is unchanged behaviourally**

Run: `cd backend && go test ./internal/modules/pharmacy/ -v`
Expected: PASS with no test edits. If a test needed changing, something moved that should not have.

- [ ] **Step 3: Convert the remaining four**

Same treatment for `medicore` (visits), `lab` (orders), `reference` (pings), and `iam`. `iam` already splits across `module.go`, `me.go`, `roles.go`, `sync.go` — keep those files and convert their closures to methods in place rather than reorganising further.

Keep consumers in a `consumers.go` per module where a module has any.

- [ ] **Step 4: Verify each module as you go**

Run: `cd backend && go test ./internal/modules/... -v 2>&1 | tail -12`
Expected: PASS, no test edits anywhere.

- [ ] **Step 5: Confirm the arch tests still hold**

Run: `cd backend && go test ./internal/archtest/ -v`
Expected: PASS — particularly `TestModulesDoNotUseRawGinGroups` and the permission-matrix oracle, which must be unaffected.

- [ ] **Step 6: Commit**

```bash
git add backend/internal/modules
git commit -m "refactor: module handlers as named methods so they have names, tests and profiles"
```

---

### Task 10: Prove the concurrency guard exists

**Files:**
- Modify: `backend/scripts/new-module.sh`
- Test: `backend/internal/modules/pharmacy/module_test.go`, `backend/internal/modules/lab/module_test.go`

**Interfaces:**
- Consumes: Task 9's handler shape.
- Produces: a concurrency test in the generator template that every future module inherits.

- [ ] **Step 1: Write the failing test for pharmacy**

The `RowsAffected == 0` branch that returns 409 is currently untested: `TestDispenseFlow` dispenses sequentially, so the second call exits at the fast-path status check and never reaches the guarded UPDATE. Add to `backend/internal/modules/pharmacy/module_test.go`:

```go
// Exactly one of N concurrent fulfilments may win. The guarded UPDATE is
// what enforces it — under READ COMMITTED the losers re-evaluate
// "WHERE status = 'pending'" after the winner commits and match zero
// rows. Delete that branch and this test fails; the sequential flow test
// does not, because it never reaches the UPDATE.
func TestConcurrentDispenseYieldsExactlyOneWinner(t *testing.T) {
	h := setup(t)
	// ...create a medication and a pending dispense using the same
	// helpers TestDispenseFlow uses; capture its id...

	const n = 8
	codes := make(chan int, n)
	var start sync.WaitGroup
	start.Add(1)
	for i := 0; i < n; i++ {
		go func() {
			start.Wait()
			codes <- do(h, "POST", "/v1/pharmacy/dispenses/"+id+"/dispense", tokenAdmin, "").Code
		}()
	}
	start.Done()

	ok, conflict, other := 0, 0, 0
	for i := 0; i < n; i++ {
		switch c := <-codes; c {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			conflict++
		default:
			other++
			t.Logf("unexpected status %d", c)
		}
	}
	require.Equal(t, 1, ok, "exactly one fulfilment must win")
	require.Equal(t, n-1, conflict, "every loser must get 409")
	require.Zero(t, other)
}
```

Adapt `setup`, `do` and the token variable to whatever this file already uses — do not introduce new helpers.

- [ ] **Step 2: Run it and confirm it passes against the current guard**

Run: `cd backend && go test ./internal/modules/pharmacy/ -run TestConcurrentDispense -race -v`
Expected: PASS.

Note the connection pool is capped at 5 while this fires 8 requests; they queue rather than fail. If the test times out rather than failing, raise its timeout — do not lower `n`, since the point is contention.

- [ ] **Step 3: Prove the test is not vacuous**

Temporarily delete the `if result.RowsAffected == 0 { ... }` branch in `pharmacy`'s fulfil handler and re-run.
Expected: FAIL (more than one 200). Restore the branch immediately and re-run to confirm PASS.

This step is the whole point of the task — a test that cannot fail is worse than no test.

- [ ] **Step 4: Add the equivalent to lab**

`lab` has the same guarded transition on its result endpoint. Add the matching test, adapted to its route and helpers.

Run: `cd backend && go test ./internal/modules/lab/ -race -v`
Expected: PASS.

- [ ] **Step 5: Add it to the generator template**

In `backend/scripts/new-module.sh`, inside the `module_test.go` heredoc, add the same concurrent-transition test against the generated `done` endpoint, so every future module inherits one. Add `sync` and `net/http` to the template's imports if absent.

- [ ] **Step 6: Verify the generator produces a working module**

```bash
cd backend && ./scripts/new-module.sh scratchcheck
go test ./internal/modules/scratchcheck/ -race -v
```
Expected: PASS, including the new concurrency test.

Then remove it — it must not be committed:

```bash
rm -rf backend/internal/modules/scratchcheck
git status --porcelain   # expect no scratchcheck entries
```

Note the generated module is not registered in `bootstrap.Modules()` or `archtest.allModules()`, so `go test ./...` would fail its parity tests while it exists. Test the package directly, as above.

- [ ] **Step 7: Commit**

```bash
git add backend/scripts/new-module.sh backend/internal/modules/pharmacy/module_test.go backend/internal/modules/lab/module_test.go
git commit -m "test: prove the guarded transition actually serialises concurrent writers"
```

---

### Task 11: Full verification and pull request

**Files:** none — verification only.

- [ ] **Step 1: Run the whole gate**

```bash
cd backend && go build ./... && go test -race ./... 2>&1 | tail -20
make lint-go
cd backend && ./scripts/coverage-gate.sh
cd .. && pnpm turbo lint type-check test build
make test-scripts
```
Expected: all green.

- [ ] **Step 2: Prove the guards actually refuse**

```bash
cd backend
HELIVANTA_ENV=production FIREBASE_AUTH_EMULATOR_HOST=localhost:19099 go run ./cmd/api 2>&1 | head -3
```
Expected: refuses to start, naming `FIREBASE_AUTH_EMULATOR_HOST`. (Use whichever emulator port your stack runs on.)

```bash
cd backend
HELIVANTA_ENV=dev APP_DATABASE_URL="$ADMIN_DATABASE_URL" go run ./cmd/api 2>&1 | head -3
```
Expected: refuses to start, naming the bypass capability.

- [ ] **Step 3: Prove the stack still runs end to end**

```bash
make down
RESET_YES=1 make reset
make up          # separate terminal
make verify-local
```
Expected: every check ok, including the authenticated round trip.

- [ ] **Step 4: Open the pull request**

```bash
git push -u origin feat/774-foundation-hardening
gh pr create --title "feat: foundation hardening — fail-fast guards, RLS correctness, module template" --body "$(cat <<'BODY'
Closes #774 Tier 0. Unblocks #70.

## Summary

Eight findings from the five-specialist foundation audit, in three groups.

**Fail-fast guards.** `HELIVANTA_ENV` (defaulting to `production`) gates two new refusals: constructing a GIP client with `FIREBASE_AUTH_EMULATOR_HOST` set outside dev — the emulator makes ID token signature verification a no-op, so a forged token would be accepted — and opening an app database pool whose role can bypass RLS, which would silently serve every tenant's data with every test still green. Consumer and dispatcher panics are now contained, so a malformed payload dead-letters one event instead of terminating the API for all tenants.

**RLS correctness.** The tenancy predicate moves into `hms_tenant_visible(uuid)`, adopted by every module's policies via a per-module migration, so widening it for hospital groups later is one function replacement rather than an ALTER POLICY per table across thirty modules. `WITH CHECK` deliberately stays pinned to strict equality — reads may widen, writes must not. `LintRLS` inverts: it now enumerates every table and subtracts an allowlist, so a module that forgets `tenant_id` entirely fails, which the old direction could not see. `reference_ping_receipts` gains the tenant column it always needed.

**Module template.** `respond.InternalErr` logs the cause against the request id; the discarding form is deleted so it cannot return. Handlers become named methods, one file per aggregate, so they appear in stack traces and profiles, can be unit-tested without a container, and `Routes` reads as an auditable path-to-permission table. A concurrent-transition test proves the guarded UPDATE actually serialises writers.

## Test evidence

- The guards are tested by asserting they **refuse**: emulator set outside dev, an app DSN naming a bypassing role, a panicking consumer that nak's while the process survives.
- The lint gains two cases: a table with no `tenant_id`, and a policy that inlines the old predicate. Both must fail.
- The concurrency test was verified non-vacuous by deleting the `RowsAffected == 0` branch and confirming it fails, then restoring it.
- Existing suites pass unedited through the handler conversion — it changes structure, not behaviour.
- `make verify-local` green from cold, including the authenticated round trip.

## Notes for review

- Five modules were converted rather than just the generator template. Leaving four on closures and the template on methods is the two-patterns-forever problem this is meant to prevent.
- `outbox_events` stays on the lint allowlist. It holds patient names outside RLS, which is real, but giving it a `tenant_id` changes the dispatcher's access path — tracked on #774 Tier 2.
- `HELIVANTA_ENV` defaulting to `production` means a bare `go run ./cmd/api` against the emulator now refuses until you set it. `make dev-api` sets it.
BODY
)"
```

- [ ] **Step 5: Check CI**

Run: `gh pr checks --watch`
Expected: `go`, `web` and `scripts` pass. If Actions is blocked by the org billing limit — it has been on #769, #771 and #773 — record local verification as a PR comment instead.

---

## Self-Review

**Spec coverage:**

| Spec section | Task |
| --- | --- |
| A1 environment concept | 1 |
| A2 emulator guard, backend + frontend | 2 |
| A3 app-pool RLS assertion | 3 |
| A4 consumer and dispatcher recover | 4 |
| B1 `hms_tenant_visible` | 5 |
| B2 per-module policy adoption | 6 |
| B3 `reference_ping_receipts` tenant | 6 |
| B4 inverted lint incl. function requirement | 7 |
| B5 `PlatformMigrations()` accumulator, 5 call sites | 5 |
| C1 `InternalErr`, delete `Internal`, 15 sites, generator | 8 |
| C2 named handler methods, all five modules | 9 |
| C3 concurrent transition test, template + pharmacy + lab | 10 |
| Testing section (refusal tests) | 1–4, 7, 10 |

No spec requirement is unmapped.

**Placeholder scan:** two steps intentionally direct the implementer to read an existing file rather than reproducing it — Task 3 Step 1 (the DSN helper in `db_test.go`) and Task 4 Step 1 (the setup in `dlq_test.go`). Both name the exact file and what to copy, because inventing a second container helper would be worse than reusing the established one. Every other step carries literal content.

**Type consistency:** `IsDev()` defined in Task 1 is used in Task 2. `allowEmulator bool` is the third parameter in both constructors. `hms_tenant_visible` is spelled identically in Tasks 5, 6 and 7. `PlatformMigrations()` returns `[]tenantdb.Migration`, matching what the five call sites pass to `db.Migrate`. `InternalErr(c, err, message)` has the same argument order in Tasks 8, 9 and 10.

**Ordering dependencies:** 6 requires 5 (the function must exist); 7 requires 6 (existing tables must already comply or its own suite fails); 9 requires 8 (conversion uses the new helper); 10 requires 9. Tasks 1–4 are independent of 5–10 and of each other except 2 and 3 requiring 1.

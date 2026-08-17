# Foundation hardening (Tier 0) — design

Resolves: [#774](https://github.com/tesserix/helivanta/issues/774) Tier 0 — [DevEx]
Foundation audit. Blocks [#70](https://github.com/tesserix/helivanta/issues/70)
patient registration.

Date: 2026-08-12

## Problem

Five modules exist; about thirty are planned. A five-specialist review of the
foundation (architecture, data modelling, Go, security, audit placement) found
gaps that are cheap now and expensive once the module count grows. Eight of
them block patient registration, which alone adds roughly eight tables and the
largest handler set in the codebase.

Every finding this spec addresses was reproduced against the repository or its
running database, not accepted on a reviewer's word. The full register, with
verification notes and the Tier 1/Tier 2 items deferred elsewhere, is on #774.

The enforcement machinery already in place is good and none of this argues
against it: compile-time permission declaration, forced RLS linted in every
module harness, the AST-based `WithAdmin` allowlist, the hand-transcribed
permission matrix, and the transactional outbox. These are scale gaps, not
care gaps.

## Scope

Three independent groups, one spec, one plan, one branch. They share a
motivation and all block #70; splitting them would triple the ceremony for
about a day's work.

Out of scope, tracked on #774 Tier 2: session revocation, PHI in event
payloads, the `authz.Public` membership hole, rate limiting, connection-pool
sizing, reconciler write amplification, permission-resolution cost, the tenant
table, the event contract registry, the pagination contract, outbox retention,
telemetry, and encryption-at-rest posture.

---

## Part A — fail-fast guards

### A1. An environment concept

`config.Config` gains `Env`, read from `HELIVANTA_ENV`, **defaulting to
`production`**, with a `cfg.IsDev()` helper.

The default is the whole point. A guard that defaults to permissive protects
nothing, because the deployment that forgets to set the variable is exactly the
one that needed protecting. Defaulting to `production` means the failure lands
on a developer, loudly and immediately, rather than on a hospital, silently.

`make dev-api` and `internal/testutil` set `HELIVANTA_ENV=dev`. A bare
`go run ./cmd/api` against the emulator will refuse to start until the
developer sets it, which is the intended cost.

### A2. Emulator guard

`firebase-admin-go` checks `FIREBASE_AUTH_EMULATOR_HOST` when the client is
constructed. When it is set, `VerifyIDToken` **skips RSA signature
verification** and trusts the decoded claims. Nothing in the repository asserts
it is unset, and `Makefile` sets it routinely, so it is already in the muscle
memory of the project.

The consequence of that variable reaching production is total: forge a JWT with
any `sub` and any `tenant_id`, send it as a bearer token, and read or write
every tenant's data. The process boots cleanly and `/readyz` is green.

`NewGIPVerifier` and `NewGIPMinter` take an explicit `allowEmulator bool`
parameter. When `FIREBASE_AUTH_EMULATOR_HOST` is set and the flag is false,
construction returns an error naming the variable and stating that it disables
signature verification. `cmd/api` passes `cfg.IsDev()`.

A parameter rather than reading `HELIVANTA_ENV` inside the package: `pkg/` should not
reach into the process environment, and a parameter is testable without
`t.Setenv`.

**Frontend mirror.** `apps/shell/lib/firebase.ts` currently falls back to
`localhost:9099` whenever `NODE_ENV !== "production"`. The implicit fallback is
removed — the emulator is used only when `NEXT_PUBLIC_AUTH_EMULATOR_HOST` is
explicitly set, never inferred. `make up` already sets it.

### A3. Assert the app pool cannot bypass RLS

Tenant isolation rests entirely on `APP_DATABASE_URL` naming a role that is
`NOSUPERUSER NOBYPASSRLS` and does not own the tables. That property is
established in `dev/init-db.sql` and `internal/testinfra/containers.go`, both
non-production, and asserted nowhere at runtime. The two default DSNs differ
only by username.

If the admin URL is pasted into `APP_DATABASE_URL`, or production is stood up
with a single database user, every `WithTenant` call runs as an RLS-bypassing
role. Every test still passes, `LintRLS` still passes because it inspects table
metadata rather than the connecting role, and there is no log line. Tenant
isolation collapses completely with zero signal.

`tenantdb.Open` probes `pg_roles` for `rolbypassrls` and `rolsuper` on
`current_user`, **on the app pool only** — the admin pool is meant to be
privileged — and returns an error refusing to start.

Table ownership is not probed. `FORCE ROW LEVEL SECURITY` already covers the
owner case, and the probe plus `FORCE` closes both paths.

### A4. Recover in the consumer path

`pkg/events/bus.go` has no `recover()` anywhere. GORM's `Transaction` recovers,
rolls back and re-panics; `gin.Recovery()` covers only HTTP handlers. So a
panic in any consumer handler — reachable from a parseable but malformed event
payload — terminates the entire API process: all modules, all tenants.

`handleMsg` and the dispatcher loop recover, log consumer, event id, tenant id
and stack, and `Nak` so redelivery and eventually the DLQ handle the message. A
poison payload should dead-letter one event, not the hospital's API.

---

## Part B — RLS correctness

### B1. The predicate becomes a function

Every policy inlines `tenant_id = current_setting('app.tenant_id', true)::uuid`.
Migrations are append-only, so the day a group tenant must see its member
hospitals — a stated roadmap requirement — that predicate is wrong on every
table in the system, and fixing it is one `ALTER POLICY` per table across
thirty modules with no way to verify completeness.

`pkg/tenantdb` owns a new platform migration `0001_platform_rls` creating:

```sql
CREATE FUNCTION hms_tenant_visible(row_tenant uuid) RETURNS boolean
  LANGUAGE sql STABLE PARALLEL SAFE AS
$$ SELECT row_tenant = current_setting('app.tenant_id', true)::uuid $$;
```

`pkg/events` already owns `0001_events_outbox`, so a `pkg/` package owning a
namespaced migration is established precedent.

### B2. Each module adopts it

Platform migrations run before module migrations, so a single platform
migration cannot alter policies on module tables — on a fresh database those
tables do not exist yet. Instead each module adds its own migration altering
its own policies:

```sql
ALTER POLICY tenant_isolation ON <table>
  USING (hms_tenant_visible(tenant_id))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
```

Ordering works on both a fresh and an existing database, and each module owns
its own schema exactly as the standards require.

**`WITH CHECK` deliberately stays pinned to strict equality.** Reads may widen
to a hospital group later; writes must always land in exactly one tenant. The
asymmetry is the point and must survive future edits.

### B3. `reference_ping_receipts` gains a tenant

It has no `tenant_id` at all today. It is a per-tenant idempotency ledger and
should have been scoped from the start. Adding the column and policy in the
same module migration keeps it off the lint's allowlist.

### B4. Invert the lint

`LintRLS` only inspects tables that already have a `tenant_id` column, so a
module that forgets the column produces a table with no tenancy, no RLS and no
policy — and passes the lint, the harness assertion and the arch test, all
green. That is the mistake a hurried module author actually makes.

The lint inverts: enumerate every `public` table, subtract an explicit
allowlist, and fail anything remaining that lacks `tenant_id`, forced RLS, a
policy carrying both `USING` and `WITH CHECK`, **or** whose `USING` clause does
not reference `hms_tenant_visible`.

Allowlist: `schema_migrations`, `outbox_events`, `processed_events`.

`outbox_events` is allowlisted with a comment naming #774. It holds patient
names outside any RLS boundary, which is a real finding — but giving it a
`tenant_id` changes the dispatcher's access path and deserves its own change.

### B5. One platform-migration accumulator

`events.Migrations()` is hand-written in five places: `cmd/api/main.go`,
`cmd/migrate/main.go`, `internal/testutil/harness.go`,
`internal/archtest/arch_test.go` and `internal/archtest/rls_test.go`.

Adding a second platform migration means remembering all five. Missing
`harness.go` breaks every module test confusingly at `db.Migrate`; missing
`rls_test.go` silently stops the RLS lint covering the new table.

`bootstrap.PlatformMigrations()` returns platform migrations in order (events,
then rls) and all five call sites route through it. `rls_test.go` keeps
iterating its own `allModules()` — its independence from `bootstrap` is
deliberate — and sources only the platform prefix from one place.

---

## Part C — module template

### C1. Errors stop being discarded

There are 15 `respond.Internal` call sites across the modules; exactly two
places in all of `internal/modules/` log anything. A production 500 gives the
client a generic string and the operator nothing: no error text, no SQLSTATE,
no request id.

```go
func InternalErr(c *gin.Context, err error, message string) {
    requestid.Logger(c).ErrorContext(c.Request.Context(), message, "err", err)
    Error(c, http.StatusInternalServerError, "internal", message)
}
```

**`respond.Internal` is deleted rather than kept alongside.** All 15 sites have
an error in scope; leaving the discarding form available means it gets used
again, and the generator would have to choose between them.

### C2. Named handler methods

Handlers are anonymous closures inside `Routes`. `pharmacy` is already 115
lines for four endpoints. A realistic module — billing, appointments, inpatient
— is 25 to 40 endpoints, which is a 700-to-900-line function containing forty
closures.

The consequences are concrete, not stylistic: every stack trace and pprof entry
reads `Routes.func17`; no handler can be tested without the full testcontainers
harness because a closure has no name to call; a handler's dependency footprint
is invisible because every closure captures all of `deps`; and two engineers
adding endpoints edit the same function body.

The generator emits `module.go` plus one file per aggregate plus
`consumers.go`, with handlers as methods on a small struct holding only what
they use. `Routes` becomes a route table a reviewer can scan for
path-to-permission pairs, which is the whole point of `platform.Router`.

**All five existing modules are converted, not just the template.** Leaving
four on closures and the generator on methods is exactly the
two-patterns-forever problem this prevents. The conversion is mechanical and
the module tests prove behaviour is unchanged. Converting medicore is arguably
wasted effort given #70 rewrites it, but a consistent codebase is worth the
hour.

The one-package-per-module rule is unchanged — this is a file split within the
package, matching what `iam` already does.

### C3. A concurrent transition test

`grep` for `t.Parallel`, `sync.WaitGroup` or `go func` across every `*_test.go`
returns nothing. `go test -race` runs with nothing racy pointed at it.

The `RowsAffected == 0` line that closes the 409 race is therefore untested:
`TestDispenseFlow` dispenses sequentially, so the second call returns at the
fast-path status check and never reaches the guarded UPDATE. Delete those lines
from all three modules and the suite stays green.

The generator template gains a test that fires eight concurrent transitions and
asserts exactly one 200 and seven 409s, and the same test is added to pharmacy
and lab now. It fails if the guard is removed, and it gives the race detector
something real.

---

## Error handling

Every guard fails closed and says why. The emulator guard names the variable
and what it disables. The role probe names the capability it found and states
that isolation would be silently disabled. The lint names each offending table
and which requirement it missed. The consumer recover logs enough to identify
the event and nak's rather than terminating.

## Testing

Each guard is tested by asserting it **refuses**, because a guard that never
fires is indistinguishable from one that does not work:

- emulator variable set with `allowEmulator=false` → constructor errors
- app DSN naming a role with `BYPASSRLS` → `Open` errors
- a panicking consumer handler → message nak'd, process survives, error logged
- a table with no `tenant_id` → lint fails
- a table whose policy inlines the old predicate → lint fails
- eight concurrent transitions → exactly one 200, seven 409s

Existing suites must stay green throughout: the conversion in C2 changes
structure, not behaviour.

## Files

New:

- `backend/pkg/tenantdb/migrations.go` — `0001_platform_rls`
- `backend/internal/bootstrap/migrations.go` — `PlatformMigrations()`
- per-module aggregate files created by the C2 split

Changed:

- `backend/internal/config/config.go` — `Env`, `IsDev()`
- `backend/pkg/authn/gip.go` — `allowEmulator` on both constructors
- `backend/pkg/tenantdb/db.go` — role probe in `Open`, inverted `LintRLS`
- `backend/pkg/events/bus.go` — recover in `handleMsg` and dispatcher
- `backend/internal/platform/respond/respond.go` — `InternalErr`, drop `Internal`
- all five modules — new policy migration, handler split, 15 call sites
- `backend/scripts/new-module.sh` — new shape, concurrency test
- `cmd/api`, `cmd/migrate`, `internal/testutil/harness.go`, both archtest files
- `apps/shell/lib/firebase.ts` — remove the implicit emulator fallback
- `Makefile` — `HELIVANTA_ENV=dev` for dev targets

## Known limitations

- The role probe checks capabilities, not table ownership. `FORCE ROW LEVEL
  SECURITY` covers ownership, so the combination is sound, but the probe alone
  is not a complete statement about the role.
- `outbox_events` remains outside RLS and still carries patient names. Tracked
  on #774; not fixed here.
- `hms_tenant_visible` returns the same strict equality it replaces. This
  change buys the ability to widen later in one place; it does not widen
  anything now.
- Converting all five modules touches working code. The module tests are the
  safety net, and they are integration tests that exercise real HTTP paths.

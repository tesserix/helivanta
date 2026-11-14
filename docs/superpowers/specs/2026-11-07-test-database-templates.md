# Migrated test databases cloned from a template (#1620)

Issue: #1620.

## Why

`./scripts/coverage-gate.sh` runs every package with `-timeout=20m`. The
workforce package took 703–919s on `main` and then hit 1200s on two runs,
including a docs-only commit (runs 42335220272 and 42342722257). `main` goes red
for reasons unrelated to the change being tested.

## What exists (checked against source and measured on 2026-11-07)

- `testutil.NewHarness` gets an empty database from `testinfra.StartPostgres`.
  It then applies `bootstrap.PlatformMigrations()` plus every module's
  migrations, inside a 90-second context, for each test.
- One full workforce run created **298 harness databases**. It passed in 768s
  on a 2-CPU colima VM (without `-race`).
- Each workforce harness applies **100 migrations in 1.02–1.07s**, with or
  without `-race`. That is ~310s, **40% of the package**. The empty database
  costs ~65ms and opening the pools ~20–50ms.
- `CREATE DATABASE … TEMPLATE` from the migrated 19.6 MB database takes
  **35–61ms** (`WAL_LOG` and `FILE_COPY` are the same at this size). Dropping
  the clone takes 9–20ms.
- No migration changes anything at database level: no `ALTER DATABASE`,
  `GRANT … ON DATABASE` or database comment. `drugmaster` checks
  `current_database() LIKE 'hms\_test\_%'` when a row is inserted, not during
  migration.
- Tests that connect worker roles to "this test's database" read its name from
  `current_database()`, so a clone with a different name than the template
  works unchanged.

## Decision

D1. **Template per distinct migration set, per test binary.** `testinfra` adds
`StartMigratedPostgres(t, owner, key, build)`. The first call for a key creates
a template database, `hms_test_template_<n>`, with the same setup as
`newDatabase` (including the production-owner variant), and passes its three
DSNs to `build`. The harness's `build` runs `Migrate` and then `LintRLS`, and
returns an error for any table the lint reports. After `build` succeeds, the
template is closed to connections. Every call, including the first, returns a fresh
`CREATE DATABASE hms_test_<n> TEMPLATE … OWNER …` that is dropped in
`t.Cleanup`, as today. The name stays `hms_test_` so the drug master's
test-only fixture check behaves the same.

D2. **The key is derived, never written by hand.** The harness hashes the JSON
encoding of the full `[]tenantdb.Migration` list (ID, SQL, concurrent indexes,
and any field added later) with SHA-256. `testinfra` keys templates by owner as
well as by that hash, so no caller can share a template across owners.
Changing, reordering, adding or removing a migration, or switching the owner,
gives a new template. Two harnesses share a template only when they would have
run byte-identical migrations as the same owner.

D3. **The clone's ledger is checked, not migrated again.** After cloning, the
harness reads `schema_migrations` and fails the test unless its IDs equal the
migration list exactly. It names every missing and every unexpected ID.

- It does not call `Migrate` again. On a correct clone that applies nothing and
  costs ~190ms per harness, 57s of one workforce run. On a wrong clone it would
  apply the missing migrations and hide a broken template, the fallback D5
  rules out.
- `LintRLS` is not run again either. It reads only the database's catalog. The
  clone copies that catalog from a template nobody can connect to (D6), which
  carries no database-level state (D4). The lint result is therefore the
  template's.
- `LintDirectedProvenance` still runs on every clone. Its table list comes from
  the modules' `DirectedWriteTables()` declarations, which the key does not
  cover.

D4. **Fail closed on state a clone cannot inherit.** A template copies
everything inside the database but not its ACL (`pg_database.datacl`), its
settings (`pg_db_role_setting`) or its comment (`pg_shdescription`). If one of
those is set after `build`, the template is refused with an error that names
it. Without this, a future migration with `ALTER DATABASE … SET` would pass
against the template and silently be missing from every clone.

D5. **Template failures are reported to every caller.** If `build` fails,
including on a table without forced RLS, each test using that key fails with
the same error, like the shared server's `pgErr`. No test retries the build or
falls back to migrating an empty database. A fallback would hide a broken template behind 1-second migrations.

D6. **Connections.** The template is built through the caller's own pools,
which `build` closes. Before cloning, any remaining backends on the template
are terminated, and the template is marked `ALLOW_CONNECTIONS false` and
`IS_TEMPLATE true`. Nothing can then connect while a clone is being made, so a
clone cannot fail with "source database is being accessed by other users".

## Measured

The full workforce package without `-race`, on the same 2-CPU colima VM, one run
each:

| Harness database                              | Package time |
| --------------------------------------------- | ------------ |
| Empty database, migrated per test (`main`)    | 739.7s       |
| Template clone, then `Migrate` and both lints | 574.2s       |
| Template clone, ledger check (this change)    | 349.7s       |

Per-harness phases with the second design, over 45 harnesses: clone 94ms,
no-op `Migrate` 191ms, lints 66ms. D3 removes the second and most of the third.

## Not covered

- Tests that call `StartPostgres` directly and migrate it themselves (migration
  runner, RLS lint and concurrent-index tests) keep the empty database they
  test against.
- Time spent in the test bodies, and the package's growth. Nothing stops new
  tests from using up the margin again.
- Running tests in parallel within a package. `t.Parallel()` is unused today.
  It would make every shared fixture's isolation a question for each test, and
  it is a separate change.

## Verification

- `testinfra` tests, run against the shared server:
  - a clone has every table and the ledger of its template;
  - two keys get two templates;
  - one key builds once;
  - rows written in one clone are not visible in another, or in the template;
  - a build error reaches every caller of that key;
  - a template with a database setting is refused;
  - the production-owner variant gives a clone owned by `helivanta`.
- `testutil` tests:
  - two harnesses with the same modules share a template, and one with a
    different module list gets its own;
  - the template build rejects a table without forced RLS and names it;
  - a clone whose ledger lacks a migration, or holds one the list does not
    have, fails with that ID.
- Full workforce package timed before and after on the same machine. Then
  `./scripts/coverage-gate.sh`, `go test -race ./...` and `make lint-go`.

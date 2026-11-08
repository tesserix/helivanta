# Migrated test databases cloned from a template (#1620)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** stop paying ~1s of migrations per harness. Build one template per
migration set per test binary and clone it for each test.

**Architecture:** `testinfra` builds templates and clones them, keyed by
`(Owner, key)`. `testutil.NewHarness` derives the key from its migration list
and passes a build function that applies it and runs `LintRLS`. Each clone's
`schema_migrations` must match the list exactly. Nothing else changes.

**Tech Stack:** Go 1.26, testcontainers (postgres:16-alpine), gorm, testify.

**Spec:** `docs/superpowers/specs/2026-11-07-test-database-templates.md`

## Global Constraints

- Template and clone names start with `hms_test_` (the drug master fixture check).
- A build that fails, panics or calls `t.FailNow` leaves an error that every later caller of the key receives. No fallback to migrating an empty database.
- The template is closed to connections before the first clone.
- A template with database ACL, settings or a comment is refused.
- Tests that call `StartPostgres` directly keep an empty database.

## Tasks

### Task 1: `testinfra` templates

- [x] Red: `internal/testinfra/templates_test.go` covers:
  - a clone holds the template's tables and rows;
  - the template refuses connections;
  - one key builds once;
  - two keys build twice;
  - clones are isolated from each other and from the template;
  - a build error reaches every caller;
  - database settings, ACL and comment are each refused;
  - the production owner owns the clone and its `public` schema.
- [x] Green: in `internal/testinfra/templates.go`:
  - add `Owner`, `StartMigratedPostgres` and `migratedDatabase`;
  - factor database preparation and production ownership out of
    `newDatabase` and `StartProductionOwnerPostgres` so both paths share them.
- [x] Existing `testinfra` tests stay green.

### Task 2: harness uses templates

- [x] Red: `internal/testutil/harness_key_test.go` and `harness_template_test.go` cover:
  - JSON round-trips every `tenantdb.Migration` field (`testing/quick`), so a new field cannot be left out of the key;
  - `migrationKey` changes when a migration is reordered, removed, edited or its index changes;
  - two harnesses with the same unique migration create one template;
  - a different migration creates another.
- [x] Green: `NewHarness` gets its database from `StartMigratedPostgres`, then
      still migrates and lints the clone.

### Task 2b: lint once, check the clone's ledger

Phase timing showed the clone's no-op `Migrate` at ~190ms and both lints at
~65ms per harness, against ~95ms for the clone itself.

- [x] Red: `internal/testutil/harness_clone_test.go` covers:
  - the template build rejects a table without forced RLS and names it;
  - `verifyCloneLedger` passes on a clone, then reports a deleted ID as missing
    and an added ID as unexpected.
  - Both failed against a stub on the assertion, not on setup.
- [x] Green: `migrateTemplate` runs `LintRLS` after `Migrate`. `NewHarness`
      calls `verifyCloneLedger` instead of `Migrate` and `LintRLS`, and keeps
      `LintDirectedProvenance` per clone.

### Task 3: measure and gate

- [x] Time the full workforce package before and after on the same machine:
      739.7s on `main`, 349.7s with this change (no `-race`, 2-CPU colima).
- [ ] Run `./scripts/coverage-gate.sh`, `go test -race ./...` and `make lint-go`.

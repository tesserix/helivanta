# Structured Logging with PHI Redaction — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

> **⚠️ PART E: THE PLAN BELOW WAS WITHDRAWN; TAG REDACTION NOW SHIPS BY A DIFFERENT DESIGN.**
>
> Task 8 below (reflection-based `hmslog:"phi"` redaction) was implemented,
> reviewed twice, found to carry five Critical defects, and **withdrawn**. Do not
> resume it. `hmslog:"phi"` masking **does** ship, via the marshal-then-mask design
> in issue #778: `encoding/json` renders the value, and the rendered bytes are
> masked at the JSON paths the tags identify. See `backend/pkg/logging/phitag.go`
> and `docs/standards/backend.md` for the authoritative contract. Task 8 and the
> "Tags:" block below are preserved for history only.

**Goal:** Give the Helivanta backend one JSON slog pipeline with tenant/subject correlation and PHI redaction, and mechanically protect the single clause that today prevents GORM from dumping patient rows into the logs.

**Architecture:** Five parts were planned, sequenced so the urgent guard lands first in its own PR. Part A adds an arch test plus a captured-stderr property test around `tenantdb.Open`'s `logger.Silent`. Parts B–D build `backend/pkg/logging`: a JSON handler with `LOG_LEVEL` control, request-scoped correlation fields wired in the platform middleware chain, and **pattern redaction at the writer** (screening the serialised bytes slog actually emits). **Part E — tag redaction at the handler (`hmslog:"phi"`, for the names and dates patterns cannot match) — ships, but not as planned below: the reflection design in Task 8 was WITHDRAWN after review and replaced by the marshal-then-mask implementation under issue #778. See the banner above.**

**Why redaction is split across two layers** — this was learned during implementation, not designed up front. A single `slog.Handler` that inspected attribute *values* was built, reviewed adversarially and taken through two fix rounds; both rounds closed every finding and both introduced new leaks of the same class, because the handler screened a *proxy* for what slog would emit (`fmt.Sprint`, `%g`, a reflection-rebuilt struct, `MarshalText`) and slog then emitted something else. In the worst case the redactor itself published a patient's name that the type's own `MarshalJSON` had withheld. Screening the emitted bytes removes the guess entirely; reflection survives only where bytes genuinely cannot help, which is struct tags. See the spec's Part D for the full table.

**Tech Stack:** Go 1.26, `log/slog`, `gorm.io/gorm`, `gin-gonic/gin`, `go/ast` (arch tests), `testify/require`, testcontainers via `internal/testutil`.

**Spec:** `docs/superpowers/specs/2026-08-13-structured-logging-phi-redaction-design.md`
**Issue:** [#678](https://github.com/tesserix/helivanta/issues/678) (already assigned to `mahesh-sangawar`)

## Global Constraints

- Go 1.26. `slog` only — `logrus` is a hard `depguard` error repo-wide (`backend/.golangci.yml`, rule `no-logrus`).
- Modules never import other modules. `internal/` may import `pkg/`; `pkg/` must **not** import `internal/`. Part C's enrichment therefore lives in `internal/platform/requestid`, not `pkg/authn`.
- New migration IDs: none in this plan. No schema changes.
- Wrap errors with `%w`.
- Before any task is done: `make lint-go` clean, `cd backend && ./scripts/coverage-gate.sh` green (70% floor), `cd backend && go test -race ./...` green.
- Commit messages are single-line conventional commits, no attribution, no `--signoff`.
- Local stack ports are non-default (5432/6379/8080 are taken by an unrelated project). Start it with:
  `HELIVANTA_PG_PORT=15432 HELIVANTA_NATS_PORT=14222 HELIVANTA_NATS_MONITOR_PORT=18222 HELIVANTA_REDIS_PORT=16379 HELIVANTA_OPENFGA_PORT=18090 HELIVANTA_GIP_PORT=19099 HELIVANTA_API_PORT=18080 make up`
- CI is blocked org-wide by a billing/spending-limit issue. Record local verification output as a PR comment.
- **Every test in this plan must be proven non-vacuous**: break the assertion or the guard, watch the test fail, restore. Steps that require this say so explicitly. Four inert assertions have already been found on this codebase.

## Relationship to the spec

The spec was amended on 2026-08-13, mid-implementation, and this plan follows the
amended version. Two changes:

1. **Part D moved from the handler to the writer.** Recorded in the spec under "Why
   the writer and not the handler — this was learned, not designed", with the table of
   four leaks that motivated it. Tasks 6 and 7 implement the amended design; the
   original reflection-based implementation is preserved on the branch
   `backup/678-redaction-reflection` for reference and is not merged.
2. **Part E was added** for `hmslog:"phi"` tag redaction. Issue #678's primary acceptance
   criterion is *"a handler logs a struct containing a field tagged as PHI → the PHI
   field is redacted"*, and its scope line reads *"redaction hooks for tagged PHI fields
   and known patterns"*. Pattern matching alone cannot satisfy that — a patient name
   carries no pattern — so the tag layer was intended to close the issue's headline AC.
   Task 8 implemented it. **That implementation was WITHDRAWN after two review rounds
   found five Critical defects, and replaced under issue #778 by the marshal-then-mask
   design — see the banner at the top of this document.** The issue's headline AC is met
   by the replacement; the Part A GORM guard remains the primary control for bulk
   patient data.

## File Structure

**New:**

| File | Responsibility |
|---|---|
| `backend/pkg/logging/logging.go` | `New(level string) *slog.Logger` — level parsing, JSON handler, both redaction layers |
| `backend/pkg/logging/redact.go` | The pattern set, `RedactString`, the byte-level redacting writer, the counter |
| `backend/pkg/logging/phitag.go` | The `hmslog:"phi"` handler. **Not the design planned here** — the reflection walker was withdrawn; what exists is the marshal-then-mask implementation from #778: per-type JSON path set plus a token-level masking walk. |
| `backend/pkg/logging/logging_test.go` | Level parsing, JSON shape, fallback, end-to-end redaction across every rendering slog produces |
| `backend/pkg/logging/redact_test.go` | Pattern table, JSON-validity of the writer's output, number-token quoting, escape tracking, counter |
| `backend/pkg/logging/phitag_test.go` (plus `phitag_internal_test.go`, `phitag_conflict_test.go`) | Shipped under #778: tagged struct, nested, collections, maps, groups, pre-bound attrs, self-marshalling passthrough, byte-identical passthrough for untagged types, and a differential fuzz against `encoding/json`'s field naming. |

**Modified:**

| File | Change |
|---|---|
| `backend/pkg/tenantdb/db.go` | Comment explaining `logger.Silent` is a PHI control |
| `backend/pkg/tenantdb/db_test.go` | Captured-output property test |
| `backend/internal/archtest/arch_test.go` | `gorm.Open` allowlist test + its discriminating unit test |
| `backend/internal/config/config.go` | `LogLevel` field |
| `backend/cmd/api/main.go` | `slog.SetDefault` at boot; `requestid.PrincipalMiddleware()` in the `/v1` chain |
| `backend/cmd/migrate/main.go` | `slog.SetDefault` at boot |
| `backend/internal/platform/requestid/requestid.go` | `Enrich` + `PrincipalMiddleware` |
| `backend/internal/platform/requestid/requestid_test.go` | Correlation-field tests |
| `docs/standards/backend.md` | Logging section: JSON, `LOG_LEVEL`, redaction, correlation fields |

## Delivery

- **PR 1 — Part A (urgent).** Tasks 1–2. Merges on its own.
- **PR 2 — Parts B, C, D.** Tasks 3–9. One coherent `pkg/logging` deliverable plus its wiring.

Do not run `gh pr merge` while `make up` is starting — the checkout races the sub-make.

---

## Task 1: Restrict `gorm.Open` to an allowlist, and say why the logger is silent

**Files:**
- Modify: `backend/pkg/tenantdb/db.go:25-28`
- Modify: `backend/internal/archtest/arch_test.go` (append at end of file)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: nothing later tasks depend on.

**Background the implementer needs:** `backend/internal/archtest/arch_test.go` already
contains `TestWithAdminIsOnlyCalledFromTheAllowlist` (line ~265) and its AST helper
`sourceReferencesWithAdmin` (line ~241). This task mirrors that structure exactly —
walk every `.go` file under the repo root `../..`, skip `internal/archtest/` to avoid
self-matching, allowlist by relative slash-path, and parse with `go/parser` +
`ast.Inspect` rather than a text scan. The existing imports (`go/ast`, `go/parser`,
`go/token`, `io/fs`, `os`, `path/filepath`, `strings`, `fmt`) are already present.

- [ ] **Step 1: Write the failing discriminating unit test for the detector**

Append to `backend/internal/archtest/arch_test.go`:

```go
// TestSourceCallsGormOpenDetectsBothSpellings is the discriminating test for
// the detector itself. A plain text scan for "gorm.Open(" misses the aliased
// import form, which compiles and runs identically. Both must be caught, and
// source that merely mentions a different Open must pass clean.
func TestSourceCallsGormOpenDetectsBothSpellings(t *testing.T) {
	const plainFixture = `package fixture

import "gorm.io/gorm"

func f(d gorm.Dialector) { gorm.Open(d) }
`
	const aliasedFixture = `package fixture

import g "gorm.io/gorm"

func f(d g.Dialector) { g.Open(d) }
`
	const cleanFixture = `package fixture

import "os"

func f() { os.Open("x") }
`
	tests := []struct {
		name string
		src  string
		want bool
	}{
		{"plain gorm.Open", plainFixture, true},
		{"aliased import still resolves to the gorm package", aliasedFixture, true},
		{"an unrelated Open is not a match", cleanFixture, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := sourceCallsGormOpen([]byte(tc.src))
			if err != nil {
				t.Fatalf("parse fixture: %v", err)
			}
			if got != tc.want {
				t.Errorf("sourceCallsGormOpen = %v, want %v", got, tc.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run it and confirm it fails to compile**

```bash
cd backend && go test ./internal/archtest/ -run TestSourceCallsGormOpenDetectsBothSpellings -v
```

Expected: build failure — `undefined: sourceCallsGormOpen`.

- [ ] **Step 3: Implement the detector**

Append to `backend/internal/archtest/arch_test.go`:

```go
// gormOpenAllowlist is exactly the files permitted to call gorm.Open.
// Every call site decides, in its gorm.Config, whether GORM's logger
// echoes executed SQL — and GORM inlines parameter values into that SQL,
// so a non-silent pool is a full patient-data dump on the error and
// slow-query paths. Keeping the set of call sites to two means the
// decision is reviewable; adding a third is a PHI decision, not a
// convenience, and belongs in review rather than in this map.
var gormOpenAllowlist = map[string]bool{
	"pkg/tenantdb/db.go":                    true,
	"internal/testinfra/containers_test.go": true,
}

// sourceCallsGormOpen parses src and reports whether it calls Open on the
// gorm package, under whatever local name the file imports it as. Matching
// on the import path rather than the literal identifier "gorm" is what
// makes an aliased import (`import g "gorm.io/gorm"`, then `g.Open(...)`)
// resolve the same as the ordinary spelling; a text scan for "gorm.Open("
// would see nothing there at all.
func sourceCallsGormOpen(src []byte) (bool, error) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.SkipObjectResolution)
	if err != nil {
		return false, err
	}
	names := map[string]bool{}
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil || path != "gorm.io/gorm" {
			continue
		}
		if imp.Name != nil {
			names[imp.Name.Name] = true
		} else {
			names["gorm"] = true
		}
	}
	if len(names) == 0 {
		return false, nil
	}
	found := false
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Open" {
			return true
		}
		if ident, ok := sel.X.(*ast.Ident); ok && names[ident.Name] {
			found = true
		}
		return true
	})
	return found, nil
}
```

Add `"strconv"` to the import block at the top of the file (after `"path/filepath"`,
keeping the group sorted).

- [ ] **Step 4: Run the detector test and confirm it passes**

```bash
cd backend && go test ./internal/archtest/ -run TestSourceCallsGormOpenDetectsBothSpellings -v
```

Expected: PASS, all three sub-tests.

- [ ] **Step 5: Write the allowlist walk test**

Append to `backend/internal/archtest/arch_test.go`:

```go
// TestGormOpenIsOnlyCalledFromTheAllowlist protects a PHI control that is
// otherwise invisible. GORM's logger renders executed SQL with parameter
// values inlined — `INSERT INTO patients VALUES (2,'HQ-OPD-0001427','Suresh
// Kumar')` — on its error and slow-query paths. The only thing keeping that
// out of the logs is logger.Silent in each gorm.Config, and a new pool
// opened anywhere else would default to logger.Warn and start emitting.
// tenantdb.Open cannot defend against a call site it does not own, so the
// defence has to be here.
func TestGormOpenIsOnlyCalledFromTheAllowlist(t *testing.T) {
	root := "../.."
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		// archtest's own source necessarily mentions gorm.Open (this check,
		// its allowlist, its fixtures). Excluding the package avoids the
		// self-match rather than allowlisting it, which would otherwise read
		// as "archtest may open pools".
		if strings.HasPrefix(rel, "internal/archtest/") {
			return nil
		}
		if gormOpenAllowlist[rel] {
			return nil
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		found, err := sourceCallsGormOpen(src)
		if err != nil {
			return fmt.Errorf("parse %s: %w", rel, err)
		}
		if found {
			t.Errorf("%s calls gorm.Open. GORM's logger inlines parameter values into the "+
				"SQL it echoes, so a pool opened without logger.Silent writes patient data "+
				"to the logs on every failing or slow query. Use tenantdb.Open. If you "+
				"genuinely need another pool, bring it to review — don't add this file to "+
				"gormOpenAllowlist in arch_test.go on your own.", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo: %v", err)
	}
}
```

- [ ] **Step 6: Run it and confirm it passes on the current tree**

```bash
cd backend && go test ./internal/archtest/ -run TestGormOpenIsOnlyCalledFromTheAllowlist -v
```

Expected: PASS (the only two call sites are both allowlisted).

- [ ] **Step 7: Prove the walk test can fail — do not skip this**

Temporarily remove `"pkg/tenantdb/db.go": true,` from `gormOpenAllowlist`, re-run:

```bash
cd backend && go test ./internal/archtest/ -run TestGormOpenIsOnlyCalledFromTheAllowlist -v
```

Expected: FAIL naming `pkg/tenantdb/db.go`. **Restore the line and re-run to confirm
PASS.** If removing the entry does not fail the test, the walk is inert — fix it before
continuing.

- [ ] **Step 8: Add the comment to `tenantdb.Open`**

In `backend/pkg/tenantdb/db.go`, replace the `open` closure (currently lines 25–28):

```go
func Open(appDSN, adminDSN string) (*DB, error) {
	// logger.Silent is a PHI control, not a noise preference. GORM's logger
	// renders the executed SQL with parameter values inlined — verified
	// against the dev database, an errored insert logs
	// `INSERT INTO phi_probe VALUES (2,'HQ-OPD-0001427','Suresh Kumar')` —
	// and it does so on the error and slow-query paths, which is exactly
	// where a debugging session lives. Raising this to logger.Info or
	// logger.Warn to see a slow query would dump every column of every
	// failing write, patient names included, into the log stream. Do not.
	// Two things stop that regression:
	// TestGormOpenIsOnlyCalledFromTheAllowlist in internal/archtest keeps
	// the set of pools to this file and one test helper, and
	// TestOpenNeverLogsQueryParameters below asserts the property against
	// a real Postgres.
	open := func(dsn string) (*gorm.DB, error) {
		return gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	}
```

- [ ] **Step 9: Run the full backend suite and the linter**

```bash
cd backend && go build ./... && go test -race ./internal/archtest/ ./pkg/tenantdb/ && cd .. && make lint-go
```

Expected: all green.

- [ ] **Step 10: Commit**

```bash
git add backend/internal/archtest/arch_test.go backend/pkg/tenantdb/db.go
git commit -m "feat: restrict gorm.Open to an allowlist so the PHI-silencing logger config cannot be bypassed"
```

---

## Task 2: Assert the property — no query parameter ever reaches stdout or stderr

**Files:**
- Modify: `backend/pkg/tenantdb/db_test.go` (append)

**Interfaces:**
- Consumes: `tenantdb.Open`, `tenantdb.DB.WithTenant`, `tenantdb.Migrations()`,
  `testutil.StartPostgres(t) (appDSN, adminDSN string)` — all already exist and are
  used by `openMigrated` at the top of the same file.
- Produces: nothing later tasks depend on.

**Background the implementer needs:** GORM's `logger.Default` is built at package init
as `log.New(os.Stderr, ...)`. It captured the `*os.File` for **file descriptor 2** at
that moment, so reassigning the `os.Stderr` variable in a test does **not** redirect it.
The capture must happen at the file-descriptor level: `syscall.Dup` the original fds to
save them, `syscall.Dup2` a pipe's write end over fds 1 and 2, run the workload, then
restore. This works on darwin and linux, which are the only platforms this repo builds
on.

The test needs a table with a UNIQUE constraint so a second insert errors. `widgets`
(defined at the top of `db_test.go`) has only a primary key with a generated default,
so add a dedicated probe table.

- [ ] **Step 1: Write the failing test**

Append to `backend/pkg/tenantdb/db_test.go`:

```go
// phiProbeMigration is a tenant table with a UNIQUE constraint, so a
// duplicate insert fails and drives GORM down the error path — the path
// where its logger echoes the executed SQL with parameters inlined.
var phiProbeMigration = []tenantdb.Migration{{
	ID: "0002_phi_probe",
	SQL: `
		CREATE TABLE phi_probe (
		  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
		  tenant_id uuid NOT NULL,
		  mrn text NOT NULL,
		  patient_name text NOT NULL,
		  UNIQUE (tenant_id, mrn)
		);
		ALTER TABLE phi_probe ENABLE ROW LEVEL SECURITY;
		ALTER TABLE phi_probe FORCE ROW LEVEL SECURITY;
		CREATE POLICY tenant_isolation ON phi_probe
		  USING (hms_tenant_visible(tenant_id))
		  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);`,
}}

type phiProbe struct {
	TenantID    uuid.UUID
	MRN         string `gorm:"column:mrn"`
	PatientName string
}

func (phiProbe) TableName() string { return "phi_probe" }

// captureStdoutStderr redirects file descriptors 1 and 2 to a pipe for the
// duration of fn and returns everything written to them. Reassigning the
// os.Stdout / os.Stderr variables would not work here: GORM's logger.Default
// is constructed at package init from log.New(os.Stderr, ...) and holds the
// *os.File for fd 2 from that moment. Only replacing the descriptor itself
// reaches it.
func captureStdoutStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)

	savedOut, err := syscall.Dup(syscall.Stdout)
	require.NoError(t, err)
	savedErr, err := syscall.Dup(syscall.Stderr)
	require.NoError(t, err)

	require.NoError(t, syscall.Dup2(int(w.Fd()), syscall.Stdout))
	require.NoError(t, syscall.Dup2(int(w.Fd()), syscall.Stderr))

	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()

	// Restore before closing the pipe, so a later failure message from the
	// test framework still has somewhere to go.
	require.NoError(t, syscall.Dup2(savedOut, syscall.Stdout))
	require.NoError(t, syscall.Dup2(savedErr, syscall.Stderr))
	_ = syscall.Close(savedOut)
	_ = syscall.Close(savedErr)
	require.NoError(t, w.Close())
	out := <-done
	require.NoError(t, r.Close())
	return out
}

// TestOpenNeverLogsQueryParameters is the property behind the logger.Silent
// clause in tenantdb.Open. The arch test keeps gorm.Open to two call sites;
// this asserts what those sites must actually achieve, and it keeps holding
// if GORM's logging is ever reworked.
//
// The control sentinel is what makes this non-vacuous: it proves the capture
// is wired to the descriptors GORM writes to. Without it, a broken capture
// returning "" would satisfy the PHI assertion perfectly.
func TestOpenNeverLogsQueryParameters(t *testing.T) {
	const patientName = "Suresh Kumar PHI-CANARY"
	const mrn = "HQ-OPD-0001427"
	const controlSentinel = "capture-control-sentinel"

	appDSN, adminDSN := testutil.StartPostgres(t)
	tenantID := uuid.NewString()

	var insertErr error
	out := captureStdoutStderr(t, func() {
		fmt.Fprintln(os.Stderr, controlSentinel)

		db, err := tenantdb.Open(appDSN, adminDSN)
		if err != nil {
			insertErr = err
			return
		}
		ctx := context.Background()
		if err := db.Migrate(ctx, tenantdb.Migrations()); err != nil {
			insertErr = err
			return
		}
		if err := db.Migrate(ctx, phiProbeMigration); err != nil {
			insertErr = err
			return
		}
		row := phiProbe{TenantID: uuid.MustParse(tenantID), MRN: mrn, PatientName: patientName}
		if err := db.WithTenant(ctx, tenantID, func(tx *gorm.DB) error {
			return tx.Create(&row).Error
		}); err != nil {
			insertErr = err
			return
		}
		// Second insert violates UNIQUE (tenant_id, mrn): GORM's error path.
		insertErr = db.WithTenant(ctx, tenantID, func(tx *gorm.DB) error {
			return tx.Create(&phiProbe{TenantID: uuid.MustParse(tenantID), MRN: mrn, PatientName: patientName}).Error
		})
	})

	require.Error(t, insertErr, "the duplicate insert must fail, or GORM's error logging path never ran")
	require.Contains(t, out, controlSentinel,
		"capture is not attached to the descriptors under test; the PHI assertion below would pass vacuously")
	require.NotContains(t, out, patientName, "patient name reached the log stream")
	require.NotContains(t, out, mrn, "medical record number reached the log stream")
}
```

Add these to the import block of `db_test.go`, keeping groups sorted: `"bytes"`,
`"fmt"`, `"io"`, `"os"`, `"syscall"`.

- [ ] **Step 2: Run it**

```bash
cd backend && go test ./pkg/tenantdb/ -run TestOpenNeverLogsQueryParameters -v
```

Expected: PASS. (The guard already exists, so this test passes on first run — which is
exactly why Step 3 is mandatory.)

- [ ] **Step 3: Prove the test can fail — do not skip this**

In `backend/pkg/tenantdb/db.go`, temporarily change `logger.Silent` to `logger.Info`,
then re-run:

```bash
cd backend && go test ./pkg/tenantdb/ -run TestOpenNeverLogsQueryParameters -v
```

Expected: FAIL on `patient name reached the log stream`. Paste the failing GORM log
line into the PR comment as evidence — it is the empirical proof the guard matters.
**Restore `logger.Silent` and re-run to confirm PASS.** If `logger.Info` does not fail
the test, the capture is broken; fix it before continuing.

- [ ] **Step 4: Run the package under the race detector**

```bash
cd backend && go test -race ./pkg/tenantdb/ && cd .. && make lint-go
```

Expected: green. The capture goroutine and the restore must not race.

- [ ] **Step 5: Commit**

```bash
git add backend/pkg/tenantdb/db_test.go
git commit -m "test: assert tenantdb pools never write query parameters to stdout or stderr"
```

- [ ] **Step 6: Open PR 1**

```bash
cd backend && ./scripts/coverage-gate.sh && cd .. && make lint-go && cd backend && go test -race ./... 2>&1 | tail -40
```

Then branch, push and open the PR with body `Closes #678` **omitted** (the issue stays
open for PR 2) — instead reference it as `Part A of #678`. Post the local verification
output, including the Step 3 failing GORM line, as a PR comment.

---

## Task 3: `LOG_LEVEL` config and the JSON logger constructor

**Files:**
- Create: `backend/pkg/logging/logging.go`
- Create: `backend/pkg/logging/logging_test.go`
- Modify: `backend/internal/config/config.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `logging.New(level string) *slog.Logger`
  - `logging.ParseLevel(level string) (slog.Level, bool)`
  - `logging.NewWithWriter(w io.Writer, level string) *slog.Logger` — same as `New` but
    writes to `w`; exists so tests can capture output. `New` is `NewWithWriter(os.Stdout, level)`.
  - `config.Config.LogLevel string`

Task 7 replaces the handler `NewWithWriter` builds with the redacting wrapper; keep the
construction in one place so that change is a one-line edit.

- [ ] **Step 1: Write the failing tests**

Create `backend/pkg/logging/logging_test.go`:

```go
package logging_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/logging"
)

func TestParseLevel(t *testing.T) {
	for _, tc := range []struct {
		in    string
		want  slog.Level
		valid bool
	}{
		{"debug", slog.LevelDebug, true},
		{"DEBUG", slog.LevelDebug, true},
		{"  info  ", slog.LevelInfo, true},
		{"warn", slog.LevelWarn, true},
		{"warning", slog.LevelWarn, true},
		{"error", slog.LevelError, true},
		{"", slog.LevelInfo, false},
		{"lodebug", slog.LevelInfo, false},
		{"verbose", slog.LevelInfo, false},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := logging.ParseLevel(tc.in)
			require.Equal(t, tc.valid, ok)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestNewEmitsJSONWithStandardFields(t *testing.T) {
	var buf bytes.Buffer
	logging.NewWithWriter(&buf, "info").Info("hello", "k", "v")

	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line), "output must be one JSON object per line")
	require.Equal(t, "INFO", line["level"])
	require.Equal(t, "hello", line["msg"])
	require.Equal(t, "v", line["k"])
	require.NotEmpty(t, line["time"])
}

func TestNewRespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	l := logging.NewWithWriter(&buf, "warn")
	l.Info("suppressed")
	require.Empty(t, buf.String(), "info must be below the configured warn threshold")
	l.Warn("emitted")
	require.Contains(t, buf.String(), "emitted")
}

// An unrecognised LOG_LEVEL degrades to info with a warning rather than
// refusing to boot. A hospital's API must not fail to start over a typo in a
// log level — the opposite of the HELIVANTA_ENV guards, where a wrong value would
// disable tenant isolation.
func TestUnrecognisedLevelFallsBackToInfoAndWarns(t *testing.T) {
	var buf bytes.Buffer
	l := logging.NewWithWriter(&buf, "verbose")
	require.Contains(t, buf.String(), "unrecognised LOG_LEVEL")
	require.Contains(t, buf.String(), "verbose", "the rejected value belongs in the warning")

	buf.Reset()
	l.Info("still logging")
	require.Contains(t, buf.String(), "still logging", "must degrade to info, not to silence")
}

// An empty LOG_LEVEL is the ordinary unset case, not a mistake, so it must
// not produce a warning on every boot.
func TestEmptyLevelIsSilentlyInfo(t *testing.T) {
	var buf bytes.Buffer
	l := logging.NewWithWriter(&buf, "")
	require.Empty(t, buf.String())
	l.Info("x")
	require.Contains(t, buf.String(), "x")
	require.False(t, strings.Contains(buf.String(), "unrecognised"))
}
```

- [ ] **Step 2: Run and confirm failure**

```bash
cd backend && go test ./pkg/logging/ -v
```

Expected: build failure — package `logging` does not exist.

- [ ] **Step 3: Implement `pkg/logging/logging.go`**

```go
// Package logging builds the process logger: JSON to stdout, level from
// LOG_LEVEL, every attribute passed through PHI redaction on the way out.
//
// It is the one place a *slog.Logger is constructed. Handlers and modules
// take the request-scoped logger from internal/platform/requestid instead,
// which is this logger pre-bound with request_id, tenant_id and subject.
package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// New returns the process logger, writing JSON to stdout.
func New(level string) *slog.Logger {
	return NewWithWriter(os.Stdout, level)
}

// NewWithWriter is New with the destination injected, so tests can capture
// output without touching os.Stdout.
func NewWithWriter(w io.Writer, level string) *slog.Logger {
	lvl, ok := ParseLevel(level)
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lvl})
	l := slog.New(handler)
	if !ok && strings.TrimSpace(level) != "" {
		// Warn rather than fail: a mistyped log level cannot compromise
		// tenant isolation, and a hospital's API should not refuse to boot
		// over one. This is deliberately the opposite call from the HELIVANTA_ENV
		// guards, where a wrong value silently disables safety checks.
		l.Warn("unrecognised LOG_LEVEL; defaulting to info", "value", level)
	}
	return l
}

// ParseLevel maps a LOG_LEVEL string to a slog.Level. The bool reports
// whether the input was recognised; an unrecognised or empty value yields
// slog.LevelInfo with ok=false, and the caller decides whether that warrants
// a warning (an empty value is the ordinary unset case, not a mistake).
func ParseLevel(level string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn", "warning":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	default:
		return slog.LevelInfo, false
	}
}
```

- [ ] **Step 4: Run and confirm the tests pass**

```bash
cd backend && go test ./pkg/logging/ -v
```

Expected: PASS.

- [ ] **Step 5: Prove one assertion can fail**

Temporarily change `slog.NewJSONHandler` to `slog.NewTextHandler` and re-run.
Expected: `TestNewEmitsJSONWithStandardFields` FAILS on the `json.Unmarshal`.
**Restore and re-run.**

- [ ] **Step 6: Add `LogLevel` to config**

In `backend/internal/config/config.go`, add the field to the struct after `Port`:

```go
	LogLevel         string
```

and to `Load()`:

```go
		LogLevel:         getenv("LOG_LEVEL", "info"),
```

- [ ] **Step 7: Verify it builds and commit**

```bash
cd backend && go build ./... && go test ./pkg/logging/ ./internal/config/ && cd .. && make lint-go
git add backend/pkg/logging/ backend/internal/config/config.go
git commit -m "feat: add pkg/logging with JSON output and LOG_LEVEL control"
```

---

## Task 4: Set the default logger at boot in both commands

**Files:**
- Modify: `backend/cmd/api/main.go`
- Modify: `backend/cmd/migrate/main.go`

**Interfaces:**
- Consumes: `logging.New(level string) *slog.Logger` (Task 3), `config.Config.LogLevel` (Task 3).
- Produces: nothing later tasks depend on, but Task 5's `requestid` enrichment builds on
  `slog.Default()` already being the JSON logger.

**Why both:** `slog.Default()` is what `requestid.Middleware` snapshots per request and
what every `slog.Info` in the tree resolves to. Until `SetDefault` runs, every line is
plain text on stderr and none of it is redacted.

- [ ] **Step 1: Wire `cmd/api`**

In `backend/cmd/api/main.go`, add `"github.com/tesserix/helivanta/pkg/logging"` to the imports
and insert immediately after `cfg := config.Load()`, **before** the existing
`slog.Info("resolved environment", ...)` line:

```go
	// Before anything else logs: until this runs, slog.Default() is the
	// unconfigured text handler on stderr and nothing is redacted.
	slog.SetDefault(logging.New(cfg.LogLevel))
```

- [ ] **Step 2: Wire `cmd/migrate`**

In `backend/cmd/migrate/main.go`, add the same import and insert after
`cfg := config.Load()`:

```go
	slog.SetDefault(logging.New(cfg.LogLevel))
```

Note `main()` in `cmd/migrate` calls `slog.Error` on the error returned by `run()`,
which happens after `SetDefault` has run inside `run()` — so failures are JSON too,
except a failure in `config.Load()` itself, which cannot fail.

- [ ] **Step 3: Verify by running the API against the local stack**

```bash
cd /Users/Mahesh.Sangawar/personal/tesserix-new/hms
HELIVANTA_PG_PORT=15432 HELIVANTA_NATS_PORT=14222 HELIVANTA_NATS_MONITOR_PORT=18222 HELIVANTA_REDIS_PORT=16379 \
  HELIVANTA_OPENFGA_PORT=18090 HELIVANTA_GIP_PORT=19099 HELIVANTA_API_PORT=18080 make up
```

Then confirm the boot lines are JSON, not text:

```bash
cd backend && LOG_LEVEL=debug PORT=18081 \
  APP_DATABASE_URL='postgres://hms_app:hms_app@localhost:15432/hms?sslmode=disable' \
  ADMIN_DATABASE_URL='postgres://hms:hms@localhost:15432/hms?sslmode=disable' \
  NATS_URL=nats://localhost:14222 OPENFGA_URL=http://localhost:18090 HELIVANTA_ENV=dev \
  go run ./cmd/api 2>&1 | head -5
```

Expected: lines of the shape `{"time":"...","level":"INFO","msg":"resolved environment",...}`.
Save this output for the PR comment. Stop the process with Ctrl-C.

- [ ] **Step 4: Confirm the fallback does not stop the boot**

Re-run the same command with `LOG_LEVEL=verbose`. Expected: a
`{"level":"WARN","msg":"unrecognised LOG_LEVEL; defaulting to info","value":"verbose"}`
line, followed by the normal boot sequence. The process must still start.

- [ ] **Step 5: Commit**

```bash
cd backend && go build ./... && cd .. && make lint-go
git add backend/cmd/api/main.go backend/cmd/migrate/main.go
git commit -m "feat: install the JSON logger as slog default at api and migrate startup"
```

---

## Task 5: Tenant and subject correlation on the request-scoped logger

**Files:**
- Modify: `backend/internal/platform/requestid/requestid.go`
- Modify: `backend/internal/platform/requestid/requestid_test.go`
- Modify: `backend/cmd/api/main.go`

**Interfaces:**
- Consumes: `authn.PrincipalFrom(c) (authn.Principal, bool)` with fields `Subject`,
  `TenantID` — both `string` (`backend/pkg/authn/authn.go:17`).
- Produces:
  - `requestid.Enrich(c *gin.Context, args ...any)`
  - `requestid.PrincipalMiddleware() gin.HandlerFunc`

**Why here and not in `pkg/authn`:** `internal/` may import `pkg/`; the reverse is the
dependency inversion the foundation audit flagged (`pkg/authz` already imports
`internal/platform/respond`). `internal/platform/requestid` importing `pkg/authn` points
the arrow the right way and needs no new coupling in `pkg/`.

**Safety note for the reviewer:** `subject` is a GIP UID — pseudonymous by construction —
and `tenant_id` is a UUID. Neither is patient data. Together with `request_id` they
answer "which hospital, which user, which request" without naming anyone.

- [ ] **Step 1: Write the failing tests**

Append to `backend/internal/platform/requestid/requestid_test.go`:

```go
// captureLogger swaps slog.Default for one writing JSON to buf, restoring
// the original when the test ends.
func captureLogger(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestPrincipalMiddlewareAddsTenantAndSubject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buf := captureLogger(t)

	r := gin.New()
	r.Use(requestid.Middleware())
	r.Use(func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{Subject: "gip-uid-42", TenantID: "11111111-1111-1111-1111-111111111111"})
		c.Next()
	})
	r.Use(requestid.PrincipalMiddleware())
	r.GET("/t", func(c *gin.Context) {
		requestid.Logger(c).Info("handled")
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/t", nil)
	req.Header.Set("X-Request-ID", "req-abc")
	r.ServeHTTP(httptest.NewRecorder(), req)

	var line map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &line))
	require.Equal(t, "handled", line["msg"])
	require.Equal(t, "req-abc", line["request_id"])
	require.Equal(t, "11111111-1111-1111-1111-111111111111", line["tenant_id"])
	require.Equal(t, "gip-uid-42", line["subject"])
}

// The middleware runs on unauthenticated paths too (a 401 still logs). With
// no principal it must leave the logger exactly as requestid.Middleware left
// it, not attach empty fields that would read as a real tenant of "".
func TestPrincipalMiddlewareIsANoOpWithoutAPrincipal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buf := captureLogger(t)

	r := gin.New()
	r.Use(requestid.Middleware(), requestid.PrincipalMiddleware())
	r.GET("/t", func(c *gin.Context) {
		requestid.Logger(c).Info("handled")
		c.Status(http.StatusOK)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/t", nil))

	var line map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &line))
	require.NotEmpty(t, line["request_id"])
	_, hasTenant := line["tenant_id"]
	require.False(t, hasTenant, "an absent principal must not produce an empty tenant_id field")
	_, hasSubject := line["subject"]
	require.False(t, hasSubject)
}

func TestEnrichIsScopedToTheRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buf := captureLogger(t)

	r := gin.New()
	r.Use(requestid.Middleware())
	r.GET("/t", func(c *gin.Context) {
		requestid.Enrich(c, "module", "medicore")
		requestid.Logger(c).Info("handled")
		c.Status(http.StatusOK)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/t", nil))

	var line map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &line))
	require.Equal(t, "medicore", line["module"])

	// A second request must not inherit the first request's field.
	buf.Reset()
	r2 := gin.New()
	r2.Use(requestid.Middleware())
	r2.GET("/t", func(c *gin.Context) {
		requestid.Logger(c).Info("handled")
		c.Status(http.StatusOK)
	})
	r2.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/t", nil))
	line = map[string]any{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &line))
	_, leaked := line["module"]
	require.False(t, leaked, "Enrich must not mutate a logger shared across requests")
}
```

Add to that file's imports: `"bytes"`, `"encoding/json"`, `"log/slog"`, `"strings"`,
and `"github.com/tesserix/helivanta/pkg/authn"`.

- [ ] **Step 2: Run and confirm failure**

```bash
cd backend && go test ./internal/platform/requestid/ -v
```

Expected: build failure — `undefined: requestid.PrincipalMiddleware`, `requestid.Enrich`.

- [ ] **Step 3: Implement**

In `backend/internal/platform/requestid/requestid.go`, add `"github.com/tesserix/helivanta/pkg/authn"`
to the imports and append:

```go
// Enrich binds additional fields to the request-scoped logger for the rest
// of this request. slog.Logger.With returns a new logger rather than
// mutating the receiver, so this cannot leak fields into another request.
func Enrich(c *gin.Context, args ...any) {
	if len(args) == 0 {
		return
	}
	c.Set(loggerKey, Logger(c).With(args...))
}

// PrincipalMiddleware adds tenant_id and subject to the request logger once
// authn has populated the context. It must be registered after
// authn.Middleware and before the handlers.
//
// This lives in the platform layer rather than in pkg/authn deliberately.
// internal/ may import pkg/; the reverse is the dependency inversion the
// foundation audit flagged, and having pkg/authn reach into
// internal/platform/requestid would deepen it for no benefit.
//
// Neither field is patient data: subject is a GIP UID, pseudonymous by
// construction, and tenant_id is a UUID. With request_id they answer which
// hospital, which user, which request — without naming anyone.
func PrincipalMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Unauthenticated requests still log (a 401 is worth a line). An
		// absent principal leaves the logger untouched rather than binding
		// empty strings, which would read in a query as a real tenant of "".
		if p, ok := authn.PrincipalFrom(c); ok {
			Enrich(c, "tenant_id", p.TenantID, "subject", p.Subject)
		}
		c.Next()
	}
}
```

- [ ] **Step 4: Run and confirm the tests pass**

```bash
cd backend && go test -race ./internal/platform/requestid/ -v
```

Expected: PASS.

- [ ] **Step 5: Prove a test can fail**

Temporarily change `Enrich` to `c.Set(loggerKey, Logger(c))` (dropping the fields) and
re-run. Expected: `TestPrincipalMiddlewareAddsTenantAndSubject` and
`TestEnrichIsScopedToTheRequest` FAIL. **Restore and re-run.**

- [ ] **Step 6: Wire it into the `/v1` chain**

In `backend/cmd/api/main.go`, change the router construction:

```go
	api := platform.NewRouter(srv.Engine.Group("/v1",
		authn.Middleware(verifier),
		requestid.PrincipalMiddleware(),
		authz.Middleware(fga),
	))
```

`requestid` is already imported in that file.

- [ ] **Step 7: Verify end to end against the running stack**

With the stack up (see Task 4 Step 3), make one authenticated request through the API
rewrite the way `scripts/verify-local.sh` does, and confirm the emitted line carries all
three fields:

```bash
cd backend && ./../scripts/verify-local.sh 2>&1 | tail -20
```

Then grep the API output for a line containing `request_id`, `tenant_id` and `subject`
together. Save it for the PR comment.

- [ ] **Step 8: Commit**

```bash
cd backend && go build ./... && go test -race ./internal/platform/... && cd .. && make lint-go
git add backend/internal/platform/requestid/ backend/cmd/api/main.go
git commit -m "feat: correlate request logs with tenant_id and subject once authn has run"
```

---

## Task 6: The pattern set and the byte-level redacting writer

**Files:**
- Create: `backend/pkg/logging/redact.go`
- Create: `backend/pkg/logging/redact_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `logging.RedactString(s string) (string, int)` — masks every known PHI
    pattern in `s`, returning the result and how many masks it applied.
  - `logging.NewRedactingWriter(w io.Writer) io.Writer` — wraps `w` so every
    line written through it is pattern-redacted, with JSON number tokens
    correctly re-quoted.
  - `logging.RedactionCount() uint64`

**Read this before writing code — it is why this task looks the way it does.**

An earlier implementation of Part D was a `slog.Handler` wrapper that inspected
attribute *values* and decided whether to mask them. It was taken through two
adversarial review rounds. Both closed every finding they were given, and both
introduced new leaks of the same class, because the handler screened a *proxy*
for what slog would emit and slog then emitted something else:

| Screened | Emitted | Result |
|---|---|---|
| `fmt.Sprint(v)` | `encoding/json` | `[]*Patient` leaked |
| `%g` | decimal | `float64(123456789012)` leaked |
| reflection-rebuilt struct | `MarshalJSON` | a patient **name** was published *by the redactor* |
| `MarshalText()` | `Error()` | error text leaked |

This task screens the bytes slog actually writes. There is no proxy and
therefore no guess. Do not reintroduce value inspection here — Task 8 handles
the one thing bytes cannot see (struct tags), and nothing else needs it.

**The one hard part:** a pattern match *outside* a JSON string is a number
token. Substituting a bare marker there produces invalid JSON:

```
{"aadhaar":123456789012}  →  {"aadhaar":[REDACTED:aadhaar]}   ← INVALID
{"aadhaar":123456789012}  →  {"aadhaar":"[REDACTED:aadhaar]"} ← correct
```

So the writer tracks whether each match sits inside a JSON string (scanning for
unescaped `"`) and quotes the marker when it does not. Every test asserts the
output still parses as JSON — that assertion is what keeps this honest.

- [ ] **Step 1: Write the failing pattern tests**

Create `backend/pkg/logging/redact_test.go`:

```go
package logging_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/logging"
)

func TestRedactPatterns(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"aadhaar bare", "id 123456789012 end", "id [REDACTED:aadhaar] end"},
		{"aadhaar spaced", "id 1234 5678 9012 end", "id [REDACTED:aadhaar] end"},
		{"aadhaar hyphenated", "id 1234-5678-9012 end", "id [REDACTED:aadhaar] end"},
		{"abha 14 digits", "abha 12345678901234 end", "abha [REDACTED:abha] end"},
		{"abha hyphenated", "abha 12-3456-7890-1234 end", "abha [REDACTED:abha] end"},
		{"mobile plus91", "call +919876543210 now", "call [REDACTED:mobile] now"},
		{"mobile plus91 spaced", "call +91 9876543210 now", "call [REDACTED:mobile] now"},
		{"mobile bare 10 digit", "call 9876543210 now", "call [REDACTED:mobile] now"},
		{"mobile 5-5 grouping", "call 98765 43210 now", "call [REDACTED:mobile] now"},
		{"mobile plus91 5-5", "call +91 98765 43210 now", "call [REDACTED:mobile] now"},
		{"mobile starting five", "call 5876543210 now", "call [REDACTED:mobile] now"},
		{"two values in one string", "a 123456789012 b 9876543210", "a [REDACTED:aadhaar] b [REDACTED:mobile]"},
		// Two matches of the SAME pattern sharing one separator. The first
		// match consumes the space, so a single pass would leave the second
		// number in the clear — an entirely ordinary thing to log.
		{"adjacent mobiles", "9876543210 9876543211", "[REDACTED:mobile] [REDACTED:mobile]"},
		{"three adjacent mobiles", "9876543210 9876543211 9876543212",
			"[REDACTED:mobile] [REDACTED:mobile] [REDACTED:mobile]"},
		// Non-matches must survive untouched, or every log line becomes noise.
		{"short number", "count 12345", "count 12345"},
		{"mobile-length starting four", "code 4876543210", "code 4876543210"},
		{"iso timestamp untouched", "at 2026-08-13T01:02:03Z", "at 2026-08-13T01:02:03Z"},
		{"plain prose", "patient admitted", "patient admitted"},
		// The regression that would silently destroy correlation: a UUID
		// contains a boundary-delimited twelve-digit run, so a \b-anchored
		// Aadhaar pattern masks every tenant_id in every log line.
		{"uuid untouched", "t 11111111-1111-1111-1111-111111111111", "t 11111111-1111-1111-1111-111111111111"},
		{"bare uuid untouched", "11111111-1111-1111-1111-111111111111", "11111111-1111-1111-1111-111111111111"},
		{"digits inside a longer token untouched", "ref_9876543210_x", "ref_9876543210_x"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, n := logging.RedactString(tc.in)
			require.Equal(t, tc.want, got)
			if tc.in == tc.want {
				require.Zero(t, n, "a clean string must not increment the counter")
			} else {
				require.Positive(t, n)
			}
		})
	}
}

// A 14-digit ABHA must come out as an ABHA, not as an Aadhaar that chewed
// twelve of its digits.
func TestLongerIdentifiersWinOverShorterOnes(t *testing.T) {
	got, _ := logging.RedactString("12345678901234")
	require.Equal(t, "[REDACTED:abha]", got)
}

// +919876543210 contains a run of exactly twelve digits. If Aadhaar is tried
// before the international mobile form, the number is masked under the wrong
// label — still safe, but the marker is supposed to say what fired.
func TestInternationalMobileIsNotMistakenForAnAadhaar(t *testing.T) {
	got, _ := logging.RedactString("call +919876543210")
	require.Equal(t, "call [REDACTED:mobile]", got)
}
```

- [ ] **Step 2: Run and confirm failure**

```bash
cd backend && go test ./pkg/logging/ -run TestRedact -v
```

Expected: build failure — `undefined: logging.RedactString`.

- [ ] **Step 3: Implement the pattern half of `pkg/logging/redact.go`**

```go
package logging

import (
	"bytes"
	"io"
	"regexp"
	"sync/atomic"
)

// bounded wraps a core pattern in explicit neighbour groups so a match only
// counts when it is a whole token.
//
// `\b` is not enough, and a UUID is why. Go's regexp is RE2 — no lookaround
// — so `\b\d{4}[-\s]?\d{4}[-\s]?\d{4}\b` happily matches the first thirteen
// characters of `11111111-1111-1111-1111-111111111111`, because a word
// boundary sits between `1` and the following `-`. Every tenant_id in every
// log line would come out as `[REDACTED:aadhaar]` and correlation — the
// entire point of Part C — would be destroyed by Part D. Requiring the
// neighbouring character to be outside [0-9A-Za-z_-] rejects it: the
// candidate is followed by a hyphen, so it is part of a longer token, so it
// is not an Aadhaar.
//
// What it costs, stated so a future widening is a deliberate decision:
// hyphen-adjacent PHI is a blind spot. `9876543210-9876543211` and
// `phone-9876543210` are both left in the clear. Do not widen the neighbour
// class without re-deriving the UUID case above.
//
// Group 1 is the preceding character (or start), group 2 the candidate,
// group 3 the following character (or end). 1 and 3 are preserved on
// replacement; only 2 is masked.
func bounded(core string) *regexp.Regexp {
	return regexp.MustCompile(`(^|[^0-9A-Za-z_-])(` + core + `)($|[^0-9A-Za-z_-])`)
}

// Order matters and is load-bearing in one direction: the +91 mobile form
// must be tried before Aadhaar, because `+919876543210` contains a run of
// exactly twelve digits and would otherwise be masked as an Aadhaar. The
// ABHA/Aadhaar ordering is belt-and-braces — bounded() already stops a
// 12-digit pattern from biting into a 14-digit run — but a longest-first
// list is the property worth stating.
var redactionPatterns = []struct {
	name string
	re   *regexp.Regexp
}{
	// Indian mobile, international form: +91 then 10 digits beginning 5-9,
	// accepting the conventional 5-5 grouping. First, so it wins the
	// twelve-digit run it contains.
	{"mobile", bounded(`\+91[-\s]?[5-9]\d{4}[-\s]?\d{5}`)},
	// ABHA: 14 digits, optionally grouped 2-4-4-4 by hyphens or spaces.
	{"abha", bounded(`\d{2}[-\s]?\d{4}[-\s]?\d{4}[-\s]?\d{4}`)},
	// Aadhaar: 12 digits, optionally grouped 4-4-4.
	{"aadhaar", bounded(`\d{4}[-\s]?\d{4}[-\s]?\d{4}`)},
	// Indian mobile, bare: 10 digits beginning 5-9, 5-5 grouping accepted.
	{"mobile", bounded(`[5-9]\d{4}[-\s]?\d{5}`)},
}

var redactions atomic.Uint64

// RedactionCount reports how many values this process has masked, across both
// the byte layer and the tag handler. It is an in-process counter with an
// accessor rather than a metric because no metrics system exists yet — #679 is
// unbuilt, and inventing a metrics dependency here would be worse than leaving
// an honest seam for it to wire up.
func RedactionCount() uint64 { return redactions.Load() }

// RedactString masks every known PHI pattern in s, returning the result and
// the number of masks applied.
func RedactString(s string) (string, int) {
	n := 0
	out := s
	for _, p := range redactionPatterns {
		marker := "[REDACTED:" + p.name + "]"
		re := p.re
		// Each pattern is applied to a fixpoint, not once. Because RE2 has no
		// lookbehind, a match consumes the character on either side to prove
		// it is a whole token — so in `9876543210 9876543211` the shared
		// space is eaten by the first match and the second is not seen on
		// that pass. The replacement puts the neighbour characters back, so
		// re-running over the output catches it. Two adjacent phone numbers
		// is an entirely ordinary thing to log; leaking the second one is
		// not an acceptable edge case.
		//
		// The loop terminates because every iteration that changes anything
		// strictly reduces the number of digit runs, and the marker it
		// substitutes contains no digits. The bound is belt-and-braces
		// against a future pattern that does not have that property.
		for i := 0; i < 100; i++ {
			before := n
			out = re.ReplaceAllStringFunc(out, func(m string) string {
				// The neighbour characters are part of the match so RE2 can
				// express "whole token" without lookaround; they are not part
				// of the secret, so put them back.
				groups := re.FindStringSubmatch(m)
				n++
				if len(groups) != 4 {
					// Cannot happen for a string the same regexp just
					// matched, but mask the lot rather than return it raw.
					return marker
				}
				return groups[1] + marker + groups[3]
			})
			if n == before {
				break
			}
		}
	}
	redactions.Add(uint64(n))
	return out, n
}
```

- [ ] **Step 4: Run the pattern tests**

```bash
cd backend && go test ./pkg/logging/ -run TestRedact -v
```

Expected: PASS. If a row fails, fix the expression, not the expectation —
every row is a deliberate case.

- [ ] **Step 5: Write the failing writer tests**

Append to `backend/pkg/logging/redact_test.go`:

```go
// through writes one line through a redacting writer and returns the output.
func through(t *testing.T, line string) string {
	t.Helper()
	var buf bytes.Buffer
	w := logging.NewRedactingWriter(&buf)
	n, err := w.Write([]byte(line))
	require.NoError(t, err)
	// io.Writer's contract: a successful Write reports len(p), whatever the
	// wrapped writer received. Returning the post-redaction length would make
	// callers believe a short write occurred.
	require.Equal(t, len(line), n, "Write must report the input length")
	return buf.String()
}

// requireValidJSON is the assertion that keeps the number-token handling
// honest. Substituting a bare marker for a JSON number produces syntactically
// invalid output, and slog would replace the whole record with !ERROR — a
// redaction control that silently deletes log records.
func requireValidJSON(t *testing.T, line string) map[string]any {
	t.Helper()
	var parsed map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(line)), &parsed),
		"output is not valid JSON: %s", line)
	return parsed
}

func TestWriterRedactsInsideStringValues(t *testing.T) {
	out := through(t, `{"msg":"call 9876543210 now"}`+"\n")
	parsed := requireValidJSON(t, out)
	require.Equal(t, "call [REDACTED:mobile] now", parsed["msg"])
}

// A bare JSON number is the case that produces invalid output if the marker
// is substituted unquoted.
func TestWriterQuotesTheMarkerWhenTheMatchIsANumberToken(t *testing.T) {
	out := through(t, `{"aadhaar":123456789012}`+"\n")
	parsed := requireValidJSON(t, out)
	require.Equal(t, "[REDACTED:aadhaar]", parsed["aadhaar"],
		"a masked number must become a JSON string, not a bare token")
}

func TestWriterHandlesNumberAndStringOnTheSameLine(t *testing.T) {
	out := through(t, `{"n":9876543210,"s":"call 9876543211"}`+"\n")
	parsed := requireValidJSON(t, out)
	require.Equal(t, "[REDACTED:mobile]", parsed["n"])
	require.Equal(t, "call [REDACTED:mobile]", parsed["s"])
}

func TestWriterRedactsKeysAsWellAsValues(t *testing.T) {
	out := through(t, `{"9876543210":"v"}`+"\n")
	parsed := requireValidJSON(t, out)
	_, raw := parsed["9876543210"]
	require.False(t, raw, "the raw key survived: %s", out)
	require.Equal(t, "v", parsed["[REDACTED:mobile]"])
}

// Correlation must survive redaction. A tenant_id is a UUID and a request_id
// may be one too; masking either destroys the feature Part C exists for.
func TestWriterLeavesCorrelationFieldsAlone(t *testing.T) {
	const tenantID = "11111111-1111-1111-1111-111111111111"
	out := through(t, `{"tenant_id":"`+tenantID+`","count":42,"ok":true}`+"\n")
	parsed := requireValidJSON(t, out)
	require.Equal(t, tenantID, parsed["tenant_id"])
	require.Equal(t, float64(42), parsed["count"])
	require.Equal(t, true, parsed["ok"])
}

// An escaped quote inside a string must not be mistaken for the string's end,
// or the in-string tracking desynchronises and every following number token
// is misclassified.
func TestWriterTracksEscapedQuotes(t *testing.T) {
	out := through(t, `{"msg":"he said \"9876543210\" loudly","n":123456789012}`+"\n")
	parsed := requireValidJSON(t, out)
	require.Equal(t, `he said "[REDACTED:mobile]" loudly`, parsed["msg"])
	require.Equal(t, "[REDACTED:aadhaar]", parsed["n"])
}

// A trailing backslash before the closing quote is an escaped backslash, not
// an escaped quote — the classic off-by-one in this kind of scanner.
func TestWriterTracksEscapedBackslashes(t *testing.T) {
	out := through(t, `{"msg":"path\\","n":123456789012}`+"\n")
	parsed := requireValidJSON(t, out)
	require.Equal(t, `path\`, parsed["msg"])
	require.Equal(t, "[REDACTED:aadhaar]", parsed["n"])
}

func TestWriterPassesCleanLinesThroughByteForByte(t *testing.T) {
	const line = `{"time":"2026-08-13T01:02:03Z","level":"INFO","msg":"ok","ward":"ward-3"}` + "\n"
	require.Equal(t, line, through(t, line), "a line with no PHI must be untouched")
}

func TestWriterCountsRedactions(t *testing.T) {
	before := logging.RedactionCount()
	through(t, `{"a":"9876543210","b":"123456789012"}`+"\n")
	require.Equal(t, before+2, logging.RedactionCount())
}
```

- [ ] **Step 6: Run and confirm failure**

```bash
cd backend && go test ./pkg/logging/ -run TestWriter -v
```

Expected: build failure — `undefined: logging.NewRedactingWriter`.

- [ ] **Step 7: Implement the writer**

Append to `backend/pkg/logging/redact.go`:

```go
// NewRedactingWriter wraps w so that every line written through it is
// pattern-redacted.
//
// This sits at the writer rather than at the handler deliberately. A handler
// that inspects attribute values has to predict how slog will render each one
// — json.Marshaler vs encoding.TextMarshaler vs error vs fmt.Stringer, value
// receiver vs pointer receiver — and every version of that prediction written
// for this package leaked PHI in a different way (see the design spec's Part
// D). Here there is nothing to predict: these are the bytes.
//
// slog's JSON handler emits one Write per record under its own mutex, so this
// sees exactly one complete line at a time and needs no buffering or locking.
func NewRedactingWriter(w io.Writer) io.Writer { return &redactingWriter{inner: w} }

type redactingWriter struct{ inner io.Writer }

func (rw *redactingWriter) Write(p []byte) (int, error) {
	out := redactJSONLine(string(p))
	if _, err := rw.inner.Write([]byte(out)); err != nil {
		return 0, err
	}
	// io.Writer's contract is that a successful Write returns len(p). The
	// redacted line is a different length, and reporting that length would
	// read to any caller as a short write.
	return len(p), nil
}

// redactJSONLine masks PHI in one serialised log line, quoting the marker
// when the match sits outside a JSON string.
//
// The distinction matters because a bare marker substituted for a number
// token is not valid JSON: `{"aadhaar":123456789012}` must become
// `{"aadhaar":"[REDACTED:aadhaar]"}`, never `{"aadhaar":[REDACTED:aadhaar]}`.
// Emitting the latter would make slog's own encoder reject the record, and a
// redaction control that silently deletes log lines is its own incident.
func redactJSONLine(line string) string {
	var out bytes.Buffer
	out.Grow(len(line))

	inString := false
	escaped := false
	segStart := 0

	// flush redacts the segment [segStart,end) and appends it. Segments are
	// split at every string boundary so each one is wholly inside or wholly
	// outside a JSON string, which is what makes the quoting decision local.
	flush := func(end int, quoted bool) {
		if end <= segStart {
			return
		}
		seg := line[segStart:end]
		red, n := RedactString(seg)
		if n > 0 && !quoted {
			// A number token became a marker; it needs quotes to stay JSON.
			red = quoteBareMarkers(red)
		}
		out.WriteString(red)
	}

	for i := 0; i < len(line); i++ {
		c := line[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				// Close the string: flush its contents, then the quote.
				flush(i, true)
				out.WriteByte('"')
				segStart = i + 1
				inString = false
			}
			continue
		}
		if c == '"' {
			flush(i, false)
			out.WriteByte('"')
			segStart = i + 1
			inString = true
		}
	}
	flush(len(line), inString)
	return out.String()
}

// quoteBareMarkers wraps any redaction marker that is not already inside
// quotes, so a masked JSON number stays a valid JSON value.
func quoteBareMarkers(s string) string {
	return bareMarker.ReplaceAllString(s, `"$1"`)
}

var bareMarker = regexp.MustCompile(`(\[REDACTED:[a-z]+\])`)
```

- [ ] **Step 8: Run the whole package**

```bash
cd backend && go test -race ./pkg/logging/ -v
```

Expected: PASS.

- [ ] **Step 9: Prove the tests can fail — do not skip this**

Run each, confirm the named test FAILS, restore after each:

1. Delete the last (bare) `mobile` entry from `redactionPatterns` →
   `TestRedactPatterns` fails on the bare-10-digit rows.
2. Move the international `mobile` entry below `aadhaar` →
   `TestInternationalMobileIsNotMistakenForAnAadhaar` fails.
3. Replace `bounded(core)` with `regexp.MustCompile("(^|\\b)(" + core + ")(\\b|$)")` →
   the `uuid untouched` rows fail. This is the exact regression the helper
   exists to prevent, and the one that would have shipped a broken `tenant_id`
   on every line.
4. Change the fixpoint loop to a single pass (`for i := 0; i < 1; i++`) →
   `TestRedactPatterns/adjacent_mobiles` fails.
5. Make `flush` ignore its `quoted` argument and never call
   `quoteBareMarkers` → `TestWriterQuotesTheMarkerWhenTheMatchIsANumberToken`
   fails on invalid JSON.
6. Remove the `case c == '\\': escaped = true` arm →
   `TestWriterTracksEscapedQuotes` fails.
7. Make `Write` return the redacted length instead of `len(p)` →
   `through`'s length assertion fails.

Record which check caught which in the report.

- [ ] **Step 10: Commit**

```bash
cd .. && make lint-go && cd backend
git add backend/pkg/logging/redact.go backend/pkg/logging/redact_test.go
git commit -m "feat: redact aadhaar, abha and mobile patterns in the serialised log line"
```

---

## Task 7: Route the process logger through the redacting writer

**Files:**
- Modify: `backend/pkg/logging/logging.go`
- Modify: `backend/pkg/logging/logging_test.go`

**Interfaces:**
- Consumes: `logging.NewRedactingWriter` (Task 6).
- Produces: no new symbols; `New` and `NewWithWriter` keep their signatures.

- [ ] **Step 1: Write the failing tests**

Append to `backend/pkg/logging/logging_test.go`:

```go
// The wiring test: pkg/logging's own constructor must produce a redacting
// logger. Every unit in redact_test.go can pass while New still hands out a
// bare JSON handler, and that gap is the whole bug.
func TestNewRedacts(t *testing.T) {
	var buf bytes.Buffer
	logging.NewWithWriter(&buf, "info").Info("ok", "phone", "9876543210")
	require.Contains(t, buf.String(), "[REDACTED:mobile]")
	require.NotContains(t, buf.String(), "9876543210")
}

// Part D must not eat Part C. The correlation fields pass through the same
// redaction as everything else, and a tenant_id UUID contains a
// boundary-delimited twelve-digit run — so a careless Aadhaar pattern would
// mask the very field that makes an incident traceable.
func TestCorrelationFieldsSurviveRedaction(t *testing.T) {
	const tenantID = "11111111-1111-1111-1111-111111111111"
	var buf bytes.Buffer
	logging.NewWithWriter(&buf, "info").
		With("request_id", "req-abc", "tenant_id", tenantID, "subject", "gip-uid-42").
		Info("handled")

	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line), "raw: %s", buf.String())
	require.Equal(t, "req-abc", line["request_id"])
	require.Equal(t, tenantID, line["tenant_id"], "redaction destroyed the tenant correlation field")
	require.Equal(t, "gip-uid-42", line["subject"])
}

// Whatever slog renders a value as, the writer sees the final bytes — which
// is the entire reason redaction lives there. These are the shapes that
// defeated the previous handler-level design; each must come out masked and
// each line must still parse as JSON.
func TestNewRedactsEveryRenderingSlogProduces(t *testing.T) {
	phone := "9876543210"
	for _, tc := range []struct {
		name string
		emit func(l *slog.Logger)
	}{
		{"pointer field in a struct", func(l *slog.Logger) {
			l.Info("m", "k", struct{ Phone *string }{&phone})
		}},
		{"slice of pointers", func(l *slog.Logger) {
			l.Info("m", "k", []*struct{ Phone string }{{Phone: phone}})
		}},
		{"map of pointers", func(l *slog.Logger) {
			l.Info("m", "k", map[string]*string{"a": &phone})
		}},
		{"float64", func(l *slog.Logger) { l.Info("m", "aadhaar", float64(123456789012)) }},
		{"int64", func(l *slog.Logger) { l.Info("m", "aadhaar", int64(123456789012)) }},
		{"wrapped error", func(l *slog.Logger) {
			l.Error("m", "err", fmt.Errorf("saving: %w", errors.New("dup mobile "+phone)))
		}},
		{"error with a divergent Stringer", func(l *slog.Logger) {
			l.Error("m", "err", errDivergentStringer{})
		}},
		{"json.Marshaler over unexported state", func(l *slog.Logger) {
			l.Info("m", "k", marshalerOverUnexported{phone: phone})
		}},
		{"encoding.TextMarshaler over unexported state", func(l *slog.Logger) {
			l.Info("m", "k", textMarshalerOverUnexported{phone: phone})
		}},
		{"numeric marshaller", func(l *slog.Logger) { l.Info("m", "k", big.NewInt(9876543210)) }},
		{"group", func(l *slog.Logger) {
			l.Info("m", slog.Group("g", slog.String("a", "123456789012")))
		}},
		{"message text", func(l *slog.Logger) { l.Info("lookup failed for " + phone) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			tc.emit(logging.NewWithWriter(&buf, "info"))
			require.NotContains(t, buf.String(), phone, "raw: %s", buf.String())
			require.NotContains(t, buf.String(), "123456789012", "raw: %s", buf.String())
			require.Contains(t, buf.String(), "[REDACTED:", "raw: %s", buf.String())
			var parsed map[string]any
			require.NoError(t, json.Unmarshal(buf.Bytes(), &parsed),
				"redaction produced invalid JSON: %s", buf.String())
			require.NotContains(t, buf.String(), "!ERROR",
				"redaction destroyed the record: %s", buf.String())
		})
	}
}

type errDivergentStringer struct{}

func (errDivergentStringer) Error() string  { return "failed for patient 9876543210" }
func (errDivergentStringer) String() string { return "failed" }

type marshalerOverUnexported struct{ phone string }

func (m marshalerOverUnexported) MarshalJSON() ([]byte, error) {
	return []byte(`{"phone":"` + m.phone + `"}`), nil
}

type textMarshalerOverUnexported struct{ phone string }

func (m textMarshalerOverUnexported) MarshalText() ([]byte, error) {
	return []byte("p " + m.phone), nil
}

// A shared logger is used from every request goroutine at once.
func TestNewIsSafeForConcurrentUse(t *testing.T) {
	var mu sync.Mutex
	var buf bytes.Buffer
	l := logging.NewWithWriter(&lockedWriter{w: &buf, mu: &mu}, "info")
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				l.Info("ok", "phone", "9876543210", "tenant_id", "11111111-1111-1111-1111-111111111111")
			}
		}()
	}
	wg.Wait()
	require.NotContains(t, buf.String(), "9876543210")
	require.Contains(t, buf.String(), "11111111-1111-1111-1111-111111111111")
}

type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (lw *lockedWriter) Write(p []byte) (int, error) {
	lw.mu.Lock()
	defer lw.mu.Unlock()
	return lw.w.Write(p)
}
```

Add to that file's imports: `"errors"`, `"fmt"`, `"math/big"`, `"sync"`.

- [ ] **Step 2: Run and confirm failure**

```bash
cd backend && go test ./pkg/logging/ -run 'TestNewRedacts|TestCorrelation|TestNewIsSafe' -v
```

Expected: FAIL — the raw values are present.

- [ ] **Step 3: Wrap the writer**

In `backend/pkg/logging/logging.go`, change the one construction line:

```go
	handler := slog.NewJSONHandler(NewRedactingWriter(w), &slog.HandlerOptions{Level: lvl})
```

- [ ] **Step 4: Run the whole package under the race detector**

```bash
cd backend && go test -race ./pkg/logging/ -v
```

Expected: PASS, including `TestNewEmitsJSONWithStandardFields` — the writer
must not break the JSON shape or the level threshold.

- [ ] **Step 5: Prove it can fail**

Revert Step 3 to the bare `slog.NewJSONHandler(w, ...)` and re-run.
Expected: `TestNewRedacts` and every subtest of
`TestNewRedactsEveryRenderingSlogProduces` FAIL. **Restore and re-run.**

- [ ] **Step 6: Commit**

```bash
cd .. && make lint-go
git add backend/pkg/logging/logging.go backend/pkg/logging/logging_test.go
git commit -m "feat: route the process logger through the redacting writer"
```

---

## Task 8: `hmslog:"phi"` tag redaction — **WITHDRAWN as written; replaced under #778**

> **The steps below were withdrawn and must not be followed.** This task's
> reflection-based design was implemented, taken through two adversarial review rounds,
> and both rounds found new Critical leaks of the same class (see "Why this layer
> exists at all" and the earlier design note above). It was pulled from the branch
> rather than merged.
>
> **A different implementation did ship**, under issue **#778**, on this same branch:
> `backend/pkg/logging/phitag.go` and its tests now exist, and
> `logging.NewPHITagHandler` is wired into `logging.NewWithWriter`. It does *not*
> reconstruct the value from reflection. It marshals with `encoding/json` first, then
> masks the rendered JSON at the paths the tags identify — see the "marshal first,
> then mask" section of the design spec and the package doc comment at the top of
> `phitag.go`. The steps below are preserved only as a record of what was attempted
> and why it failed review.

**Files:**
- Create: `backend/pkg/logging/phitag.go`
- Create: `backend/pkg/logging/phitag_test.go`
- Modify: `backend/pkg/logging/logging.go`

**Interfaces:**
- Consumes: `logging.RedactString` and the `redactions` counter (Task 6);
  `NewWithWriter`'s handler construction (Task 7).
- Produces: `logging.NewPHITagHandler(inner slog.Handler) slog.Handler`.

**Why this layer exists at all.** A name, a date of birth and an address have
no shape to match on, so Task 6's byte layer cannot see them. The only way to
know they are PHI is for the type to say so. This is the mechanism behind issue
#678's primary acceptance criterion.

**Scope discipline — read this.** An earlier design had a handler reflecting
over *every* logged value. It leaked four different ways and was replaced by
Task 6. This handler reflects only over values whose type carries a `phi` tag
somewhere, and returns everything else untouched for the byte layer to screen.
Do not broaden it. In particular it must **decline to rewrite** any value
implementing `json.Marshaler` or `encoding.TextMarshaler`: reconstructing such
a type from its exported fields publishes what its own marshaller withheld —
during the earlier implementation that behaviour published a patient's name
alongside a correctly-masked phone number.

- [ ] **Step 1: Write the failing tests**

Create `backend/pkg/logging/phitag_test.go`:

```go
package logging_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/logging"
)

type patient struct {
	ID    string
	Name  string `hmslog:"phi"`
	DOB   string `hmslog:"phi"`
	Ward  string
	Notes notes
}

type notes struct {
	Complaint string `hmslog:"phi"`
	Triage    string
}

type visit struct {
	Ref     string
	Patient *patient
}

type plain struct {
	A string
	B int
}

// tagLine emits one record through the tag handler over a JSON handler.
func tagLine(t *testing.T, emit func(l *slog.Logger)) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	emit(slog.New(logging.NewPHITagHandler(inner)))
	var line map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &line), "raw: %s", buf.String())
	return line
}

func TestTaggedFieldsAreRedacted(t *testing.T) {
	p := patient{ID: "p-1", Name: "Suresh Kumar", DOB: "1971-03-04", Ward: "ward-3"}
	p.Notes = notes{Complaint: "chest pain", Triage: "amber"}

	line := tagLine(t, func(l *slog.Logger) { l.Info("ok", "patient", p) })
	got, ok := line["patient"].(map[string]any)
	require.True(t, ok, "tagged struct should render as an object: %v", line)

	require.Equal(t, "[REDACTED:phi]", got["Name"])
	require.Equal(t, "[REDACTED:phi]", got["DOB"])
	require.Equal(t, "p-1", got["ID"], "untagged fields must survive — a fully-masked struct is useless")
	require.Equal(t, "ward-3", got["Ward"])

	nested, ok := got["Notes"].(map[string]any)
	require.True(t, ok, "nested struct should be walked: %v", got)
	require.Equal(t, "[REDACTED:phi]", nested["Complaint"])
	require.Equal(t, "amber", nested["Triage"])
}

func TestTaggedFieldsAreRedactedThroughAPointer(t *testing.T) {
	line := tagLine(t, func(l *slog.Logger) {
		l.Info("ok", "visit", visit{Ref: "v-9", Patient: &patient{ID: "p-1", Name: "Suresh Kumar"}})
	})
	got := line["visit"].(map[string]any)
	require.Equal(t, "v-9", got["Ref"])
	inner, ok := got["Patient"].(map[string]any)
	require.True(t, ok, "pointer to a tagged struct must be walked: %v", got)
	require.Equal(t, "[REDACTED:phi]", inner["Name"])
	require.Equal(t, "p-1", inner["ID"])
}

// A struct with no phi tag anywhere must pass through untouched, keeping its
// ordinary rendering — this handler is not a general-purpose reformatter, and
// that is exactly the overreach that broke the previous design.
func TestUntaggedStructsAreNotRewritten(t *testing.T) {
	line := tagLine(t, func(l *slog.Logger) { l.Info("ok", "p", plain{A: "x", B: 2}) })
	require.NotContains(t, line, "REDACTED")
	got, ok := line["p"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "x", got["A"])
	require.Equal(t, float64(2), got["B"])
}

func TestTagHandlerRedactsInsideGroups(t *testing.T) {
	line := tagLine(t, func(l *slog.Logger) {
		l.Info("ok", slog.Group("g", slog.Any("p", patient{ID: "p-1", Name: "Suresh Kumar"})))
	})
	group := line["g"].(map[string]any)
	got := group["p"].(map[string]any)
	require.Equal(t, "[REDACTED:phi]", got["Name"])
}

func TestTagHandlerRedactsPreBoundAttrs(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, nil)
	slog.New(logging.NewPHITagHandler(inner)).
		With("patient", patient{ID: "p-1", Name: "Suresh Kumar"}).
		Info("ok")
	require.NotContains(t, buf.String(), "Suresh Kumar", "raw: %s", buf.String())
	require.Contains(t, buf.String(), "[REDACTED:phi]")
}

// A type that marshals itself must be left alone. Rebuilding it from exported
// fields publishes whatever its MarshalJSON deliberately omitted — which is
// how the previous design published a patient's name.
type selfMarshalling struct {
	Name string `hmslog:"phi"`
	Ref  string
}

func (s selfMarshalling) MarshalJSON() ([]byte, error) {
	return []byte(`"ref:` + s.Ref + `"`), nil
}

func TestSelfMarshallingTypesAreNotRewritten(t *testing.T) {
	line := tagLine(t, func(l *slog.Logger) {
		l.Info("ok", "k", selfMarshalling{Name: "Suresh Kumar", Ref: "r-1"})
	})
	require.Equal(t, "ref:r-1", line["k"],
		"the type's own marshalling must be respected, not reconstructed")
}

func TestTaggedRedactionIncrementsTheCounter(t *testing.T) {
	before := logging.RedactionCount()
	tagLine(t, func(l *slog.Logger) {
		l.Info("ok", "p", patient{Name: "Suresh Kumar", DOB: "1971-03-04"})
	})
	require.GreaterOrEqual(t, logging.RedactionCount(), before+2)
}

// Self-referential types must not hang the logger.
type cyclic struct {
	Name string `hmslog:"phi"`
	Next *cyclic
}

func TestCyclicStructuresTerminate(t *testing.T) {
	c := &cyclic{Name: "Suresh Kumar"}
	c.Next = c
	line := tagLine(t, func(l *slog.Logger) { l.Info("ok", "c", c) })
	require.NotContains(t, line, "Suresh Kumar")
}
```

- [ ] **Step 2: Run and confirm failure**

```bash
cd backend && go test ./pkg/logging/ -run 'Tagged|Untagged|TagHandler|SelfMarshalling|Cyclic' -v
```

Expected: build failure — `undefined: logging.NewPHITagHandler`.

- [ ] **Step 3: Implement `pkg/logging/phitag.go`**

```go
package logging

import (
	"context"
	"encoding"
	"encoding/json"
	"log/slog"
	"reflect"
	"sync"
)

// phiTag marks a struct field as protected health information:
//
//	type Patient struct {
//	    ID   string
//	    Name string `hmslog:"phi"`
//	}
//
// This is the half of redaction that patterns cannot do. A name, a date of
// birth and an address have no shape to match on; the only way to know they
// are PHI is for the type to say so.
const (
	phiTag      = "hmslog"
	phiTagValue = "phi"
	phiMarker   = "[REDACTED:phi]"
)

// maxPHIDepth bounds the walk so a self-referential value cannot hang a
// request. Nothing legitimately logged nests this deeply.
const maxPHIDepth = 16

// taggedTypes caches, per reflect.Type, whether that type carries a phi tag
// anywhere in its field graph. Without it every logged struct pays a full
// reflective walk even when nothing in it is ever redacted, on every line.
var taggedTypes sync.Map // reflect.Type -> bool

// PHITagHandler masks struct fields tagged hmslog:"phi" before the record is
// serialised.
//
// It is deliberately narrow. It reflects only over values whose type carries a
// phi tag somewhere, and returns everything else untouched — pattern redaction
// happens downstream at the writer (see NewRedactingWriter), where it screens
// the emitted bytes rather than guessing at them. An earlier design had a
// handler reflecting over every logged value; it leaked four separate ways and
// was replaced. Do not broaden this one.
type PHITagHandler struct{ inner slog.Handler }

// NewPHITagHandler wraps inner so tagged fields are masked.
func NewPHITagHandler(inner slog.Handler) slog.Handler { return &PHITagHandler{inner: inner} }

func (h *PHITagHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.inner.Enabled(ctx, l)
}

func (h *PHITagHandler) Handle(ctx context.Context, r slog.Record) error {
	out := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	r.Attrs(func(a slog.Attr) bool {
		out.AddAttrs(maskAttr(a))
		return true
	})
	return h.inner.Handle(ctx, out)
}

// WithAttrs masks pre-bound attributes too — a patient bound once with
// logger.With would otherwise appear unmasked on every line it reaches.
func (h *PHITagHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	safe := make([]slog.Attr, 0, len(attrs))
	for _, a := range attrs {
		safe = append(safe, maskAttr(a))
	}
	return &PHITagHandler{inner: h.inner.WithAttrs(safe)}
}

func (h *PHITagHandler) WithGroup(name string) slog.Handler {
	return &PHITagHandler{inner: h.inner.WithGroup(name)}
}

// maskAttr returns a masked copy of a. If the walk cannot process the value it
// leaves the attribute alone rather than dropping it: the byte layer still
// screens the result, so a failure here degrades to pattern-only coverage
// rather than to a lost field.
func maskAttr(a slog.Attr) (out slog.Attr) {
	defer func() {
		if rec := recover(); rec != nil {
			out = a
		}
	}()
	switch a.Value.Kind() {
	case slog.KindGroup:
		attrs := a.Value.Group()
		safe := make([]slog.Attr, 0, len(attrs))
		for _, g := range attrs {
			safe = append(safe, maskAttr(g))
		}
		return slog.Attr{Key: a.Key, Value: slog.GroupValue(safe...)}
	case slog.KindLogValuer:
		return maskAttr(slog.Attr{Key: a.Key, Value: a.Value.Resolve()})
	case slog.KindAny:
		if v, ok := maskTagged(a.Value.Any(), 0); ok {
			return slog.Attr{Key: a.Key, Value: v}
		}
	}
	return a
}

// maskTagged renders v as a slog group with phi-tagged fields masked,
// reporting ok=false for anything this handler must not rewrite. A false
// result means the value keeps its ordinary rendering and the byte layer
// screens it.
func maskTagged(v any, depth int) (slog.Value, bool) {
	if v == nil || depth > maxPHIDepth {
		return slog.Value{}, false
	}
	// A type that marshals itself is rendered by that method. Rebuilding it
	// from exported fields would publish whatever its marshaller deliberately
	// omitted — during an earlier implementation that behaviour published a
	// patient's name next to a correctly-masked phone number.
	switch v.(type) {
	case json.Marshaler, encoding.TextMarshaler:
		return slog.Value{}, false
	}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return slog.Value{}, false
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct || !hasPHITag(rv.Type()) {
		return slog.Value{}, false
	}
	return slog.GroupValue(structAttrs(rv, depth)...), true
}

func hasPHITag(t reflect.Type) bool {
	if cached, ok := taggedTypes.Load(t); ok {
		return cached.(bool)
	}
	// Seed false before recursing so a self-referential type terminates
	// instead of looping forever.
	taggedTypes.Store(t, false)
	found := false
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if f.Tag.Get(phiTag) == phiTagValue {
			found = true
			break
		}
		ft := f.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct && hasPHITag(ft) {
			found = true
			break
		}
	}
	taggedTypes.Store(t, found)
	return found
}

// structAttrs converts one struct value to attrs, masking tagged fields and
// leaving everything else to render normally.
func structAttrs(rv reflect.Value, depth int) []slog.Attr {
	t := rv.Type()
	attrs := make([]slog.Attr, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if f.Tag.Get(phiTag) == phiTagValue {
			redactions.Add(1)
			attrs = append(attrs, slog.String(f.Name, phiMarker))
			continue
		}
		fv := rv.Field(i)
		if v, ok := maskTagged(fv.Interface(), depth+1); ok {
			attrs = append(attrs, slog.Attr{Key: f.Name, Value: v})
			continue
		}
		attrs = append(attrs, slog.Any(f.Name, fv.Interface()))
	}
	return attrs
}
```

- [ ] **Step 4: Wire it into the constructor**

In `backend/pkg/logging/logging.go`, wrap the handler:

```go
	handler := NewPHITagHandler(slog.NewJSONHandler(NewRedactingWriter(w), &slog.HandlerOptions{Level: lvl}))
```

The order is deliberate: tags are masked while the value is still a Go value,
and the writer screens the serialised result afterwards, so anything the tag
layer declined to touch is still pattern-checked.

- [ ] **Step 5: Run the whole package under the race detector**

```bash
cd backend && go test -race -count=1 ./pkg/logging/ -v
```

Expected: PASS. Every Task 6 and Task 7 test must still pass — in particular
`TestUntaggedStructsAreNotRewritten` and
`TestNewRedactsEveryRenderingSlogProduces`.

- [ ] **Step 6: Prove the tests can fail — do not skip this**

1. Change `phiTagValue` to `"phix"` → `TestTaggedFieldsAreRedacted` fails.
2. Delete the `json.Marshaler, encoding.TextMarshaler` case from `maskTagged` →
   `TestSelfMarshallingTypesAreNotRewritten` fails, and the failure output
   shows the name being published. Record that output; it is the evidence for
   why the case exists.
3. Make `maskTagged` return `ok=true` for any struct → `TestUntaggedStructsAreNotRewritten` fails.
4. Remove the `depth > maxPHIDepth` guard → `TestCyclicStructuresTerminate` hangs or overflows.

Restore after each and confirm a clean run.

- [ ] **Step 7: Commit — DID NOT HAPPEN (task withdrawn)**

The commit below was never made. It is left here only to show what Step 7 would have
been if the task had passed review; it did not. The replacement implementation was
committed under #778 instead (see the banner above).

```bash
cd .. && make lint-go
git add backend/pkg/logging/phitag.go backend/pkg/logging/phitag_test.go backend/pkg/logging/logging.go
git commit -m "feat: mask struct fields tagged hmslog:phi, which patterns cannot catch"
```

---

## Task 9: Document the logging contract, verify, and open PR 2

**Files:**
- Modify: `docs/standards/backend.md` (the logging section, around lines 508–563)

**Interfaces:** none.

- [ ] **Step 1: Update the standards document**

In the logging section of `docs/standards/backend.md`, after the existing `slog`-only
paragraph, add:

```markdown
### Output, level and redaction

`pkg/logging.New(level)` builds the process logger: a JSON handler on stdout,
wrapped in a redacting handler. `cmd/api` and `cmd/migrate` install it with
`slog.SetDefault` as their first act, before anything else logs.

`LOG_LEVEL` selects the threshold — `debug`, `info`, `warn`/`warning`, `error`,
case-insensitive. An unrecognised value degrades to `info` with a warning and
the process still boots: a mistyped log level cannot compromise tenant
isolation, and a hospital's API must not fail to start over a typo. This is
deliberately the opposite call from the `HELIVANTA_ENV` guards.

Every emitted line — message and attributes, recursively through groups,
wrapped errors and struct fields — passes through redaction:

- **Patterns:** Aadhaar (12 digits), ABHA (14 digits), Indian mobile (`+91`
  forms and bare 10-digit numbers beginning 5–9). Masked as
  `[REDACTED:aadhaar]` and so on.
- **Tags:** the plan's reflection design was withdrawn, but `hmslog:"phi"` masking
  **does** ship, by the marshal-then-mask design in #778 (`phitag.go`). Names, dates of
  birth and addresses are masked where a field is tagged. The authoritative contract —
  including the three things the layer cannot see — is
  `docs/standards/backend.md`; do not treat this plan as the reference. The Part A
  GORM guard remains the primary control for bulk patient data; the tag layer is
  defence in depth.

False positives are expected: a legitimate 12-digit identifier will be masked.
That is the correct direction to fail, and the marker names the pattern that
fired so it is obvious what happened.

`logging.RedactionCount()` returns the process's mask count. It is an
in-process counter, not a metric, until #679 provides a sink.

**Redaction is defence in depth, not the defence.** It only sees what passes
through `slog`. Anything a dependency writes straight to a file descriptor
bypasses it entirely — which is exactly why GORM's logger needed its own guard
(`logger.Silent` in `pkg/tenantdb.Open`, protected by
`TestGormOpenIsOnlyCalledFromTheAllowlist` and
`TestOpenNeverLogsQueryParameters`) rather than relying on this.

Redaction runs on every attribute of every emitted line. At `info` level and
current volumes that is not a concern. A future high-volume path — #438's
read-access audit is the likely first — should measure it rather than inherit
it silently.

### Correlation fields

Every request line carries `request_id` (stamped by `requestid.Middleware`
before auth runs). Once `authn` has populated the context,
`requestid.PrincipalMiddleware` adds `tenant_id` and `subject`, so a line
answers which hospital, which user, which request. Neither is patient data:
`subject` is a pseudonymous GIP UID and `tenant_id` is a UUID.

That enrichment lives in `internal/platform/requestid`, not `pkg/authn`:
`internal/` may import `pkg/`, and the reverse is a dependency inversion the
foundation audit already flagged.

Add your own request-scoped fields with `requestid.Enrich(c, "module",
"medicore")` — it returns a new logger rather than mutating a shared one, so
it cannot leak into another request.
```

If `.env.example` (or the equivalent local-dev env file the repo ships) exists, add
`LOG_LEVEL=debug` to it with a one-line comment.

- [ ] **Step 2: Full verification sweep**

```bash
cd /Users/Mahesh.Sangawar/personal/tesserix-new/hms
make lint-go
cd backend && go test -race ./... 2>&1 | tail -40
cd backend && ./scripts/coverage-gate.sh
```

All three must be green. Capture the output.

- [ ] **Step 3: End-to-end check against the running stack**

With the stack up on the non-default ports, run the API and confirm from real output:

1. Boot lines are JSON.
2. A request line carries `request_id`, `tenant_id` and `subject`.
3. `scripts/verify-local.sh` still passes its authenticated round trip.

- [ ] **Step 4: Commit and open PR 2**

```bash
git add docs/standards/backend.md
git commit -m "docs: record the JSON logging, correlation and PHI redaction contract"
```

Push the branch and open the PR with `Closes #678` in the body. Because CI is blocked by
the org's billing limit, post a PR comment containing:

- the `make lint-go`, `go test -race ./...` and `coverage-gate.sh` output;
- the failing GORM log line from Task 2 Step 3, as evidence the guard is load-bearing;
- the list of break-it/restore checks from Tasks 1, 2, 3, 5, 6 and 8, naming which test
  caught which break;
- a sample JSON boot line and a sample request line showing all three correlation fields.

- [ ] **Step 5: Merge**

Confirm `make up` is not mid-startup before running `gh pr merge --delete-branch` — the
checkout races the sub-make and it reads the wrong Makefile.

---

## Known limitations (carry into the PR body)

- Pattern redaction cannot detect names, dates of birth or addresses. The `hmslog:"phi"`
  tag layer (#778) covers them **only where a field is tagged**, and cannot see PHI
  reached through `any`, through a self-marshalling type, or already flattened into a
  string. The GORM guard (Tasks 1–2) is what protects the bulk case, by keeping
  patient data out of the log stream rather than by redacting it downstream.
- The redaction counter is in-process only until #679 provides a metrics sink.
- The arch test allowlists `gorm.Open` **call sites**; it does not verify the
  **arguments** at those sites. The stderr property test covers the argument. Both exist
  because neither covers the other's failure.
- Redaction applies only to what passes through `slog`. Anything a dependency writes
  directly to a file descriptor bypasses it — which is exactly why the GORM logger
  needed its own guard (Tasks 1–2) rather than relying on this.
- Redaction runs each pattern to a fixpoint (bounded at 100 iterations per pattern per
  string) so that adjacent matches sharing a separator are all caught. A pathological
  string could hit that bound; it would be under-redacted rather than looping.
- The shipped tag layer computes no paths beneath a type implementing `json.Marshaler`
  or `encoding.TextMarshaler`, because such a type renders however it likes. A tagged
  field *of* such a type is masked whole; PHI emitted by a custom marshaller of an
  untagged field gets pattern coverage from the byte layer only. The withdrawn
  reflection design failed the other way — it reconstructed such types from their
  exported fields and published what their own marshaller withheld.
- `bounded()` makes hyphen-adjacent PHI a blind spot: `9876543210-9876543211` and
  `phone-9876543210` are left in the clear. This is the unavoidable other side of the
  UUID false-positive fix, without which every `tenant_id` would be masked and
  correlation destroyed. Do not widen the neighbour class without re-deriving that case.
- Sampling guidance for high-volume paths (issue scope line 4) is not implemented — no
  high-volume path exists yet. A short note recording that, the measured per-line
  redaction cost, and #438 as the likely first candidate to require it, is in
  `docs/standards/backend.md`.

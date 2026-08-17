# Event Contract Registry Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give every event one subject constant and one payload type, so a publisher renaming a field breaks the build instead of silently writing an empty patient name.

**Architecture:** Each publishing module gains a `contract/` subpackage holding its events' subject constants and payload types. Consumers import it directly, under exactly one narrow exception to the module-isolation rule: a cross-module import is legal only when the path ends in `/contract`. Arch tests police that exception so it cannot widen — contract packages may declare only const/type/var and import only `{time, uuid}`. `platform.Module` gains `Publishes()`, from which three CI checks and the event catalogue derive.

**Tech Stack:** Go 1.26, `golang.org/x/tools/go/packages`, golangci-lint (depguard), NATS JetStream, testcontainers.

**Spec:** `docs/superpowers/specs/2026-08-14-event-contract-registry-design.md`
**Issue:** #827. Branch: `feat/827-event-contract-registry`.

## Global Constraints

- `docs/standards/engineering-principles.md` is binding. Especially §1 (no minimal/MVP solutions), §4 (compile error > boot failure > CI failure > convention), §5 (verify the claim, not a proxy; prove every assertion can fail).
- **Every new assertion must be observed failing before it passes.** Break it, watch it go red, restore.
- `make lint-go` clean; `cd backend && go test -count=1 -race ./...` green; `cd backend && ./scripts/coverage-gate.sh` green; `pnpm turbo lint type-check test build` green (no frontend change expected, but the gate runs).
- **The isolation exception is exactly one rule:** a cross-module import is legal only when the imported path ends in `/contract`. Everything else stays forbidden.
- **Contract packages are data only:** declare only `const`, `type`, `var` — no `func`, no methods. Import only `{time, github.com/google/uuid}`.
- **Import alias is mandatory:** `<module>contract` (`medicorecontract`, `pharmacycontract`). Two packages both named `contract` will not compile unaliased.
- Migrations are append-only; this plan adds none.
- No behaviour changes at runtime. Every guard here is a compile error or a CI failure.

---

## The seven events

Established by reading the code on 2026-08-14. **The spec says "five subjects"; that is wrong and Task 7 corrects it.**

| Subject | Publisher | Consumers | Payload type today |
|---|---|---|---|
| `helivanta.in.medicore.visit_created.v1` | medicore | pharmacy, lab | `VisitCreatedData` (medicore) + 2 private copies |
| `helivanta.in.pharmacy.dispense_recorded.v1` | pharmacy | *none* | `dispenseRecordedData` (private) |
| `helivanta.in.lab.result_ready.v1` | lab | *none* | `resultReadyData` (private) |
| `helivanta.in.iam.member_granted.v1` | iam | iam (`iam-fga-sync`) | `MemberChangedData` |
| `helivanta.in.iam.member_revoked.v1` | iam | iam (`iam-fga-sync-revoke`) | `MemberChangedData` |
| `helivanta.in.iam.credential_revoked.v1` | iam | iam (broadcast) | `CredentialRevokedData` |
| `helivanta.in.reference.pinged.v1` | reference | reference | `pingedData` (private) |

Two events have **no consumers**. That is legal — the design says so explicitly — and the CI check in Task 4 must not fail them.

Four payload types are currently unexported and become exported when they move into a contract package: `dispenseRecordedData` → `DispenseRecordedData`, `resultReadyData` → `ResultReadyData`, `pingedData` → `PingedData`.

---

## File Structure

**New:**

| File | Responsibility |
|---|---|
| `backend/internal/modules/medicore/contract/events.go` | `SubjectVisitCreated`, `VisitCreatedData` |
| `backend/internal/modules/pharmacy/contract/events.go` | `SubjectDispenseRecorded`, `DispenseRecordedData` |
| `backend/internal/modules/lab/contract/events.go` | `SubjectResultReady`, `ResultReadyData` |
| `backend/internal/modules/iam/contract/events.go` | `SubjectMemberGranted`, `SubjectMemberRevoked`, `SubjectCredentialRevoked`, `MemberChangedData`, `CredentialRevokedData` |
| `backend/internal/modules/reference/contract/events.go` | `SubjectPinged`, `PingedData` |
| `backend/internal/archtest/contract_test.go` | The data-only arch tests (T6, T7) and the narrow-exception test (T8) |
| `backend/internal/archtest/events_test.go` | The publish/subscribe CI checks (T3, T4, T5) and the unmarshal-type check (T2) |

**Modified:**

| File | Change |
|---|---|
| `backend/.golangci.yml` | depguard gains the `/contract` allowance |
| `backend/internal/archtest/arch_test.go` | `TestModulesDoNotImportEachOther` skips `/contract` imports |
| `backend/internal/platform/module.go` | `Module` interface gains `Publishes() []string` |
| `backend/internal/modules/medicore/module.go`, `visits.go` | Subject and payload move to `contract`; `Publishes()` added |
| `backend/internal/modules/pharmacy/module.go`, `dispenses.go`, `consumers.go` | Duplicate subject + payload deleted; imports medicore's contract |
| `backend/internal/modules/lab/module.go`, `orders.go`, `consumers.go` | Same |
| `backend/internal/modules/iam/module.go`, `signout.go`, `sync.go` | Subjects and payloads move to `contract` |
| `backend/internal/modules/reference/module.go`, `pings.go`, `consumers.go` | Same |
| `backend/scripts/new-module.sh` | Template emits a contract package and `Publishes()` |
| `docs/standards/backend.md` | The rule |
| `docs/superpowers/specs/2026-08-14-event-contract-registry-design.md` | Corrections (Task 7) |

---

## Task 1: `medicore/contract` and the isolation exception

**Files:**
- Create: `backend/internal/modules/medicore/contract/events.go`
- Modify: `backend/internal/modules/medicore/module.go:13`, `visits.go:33-38`, `backend/internal/modules/pharmacy/module.go:12-17`, `pharmacy/consumers.go:13-30`, `backend/internal/modules/lab/module.go:12-15`, `lab/consumers.go:13-30`, `backend/.golangci.yml:20-27`, `backend/internal/archtest/arch_test.go:67-81`

**Interfaces:**
- Produces: `medicorecontract.SubjectVisitCreated` (string const), `medicorecontract.VisitCreatedData{VisitID, PatientName string}`.
- Consumes: nothing from other tasks.

This task delivers the headline property on its own: after it, renaming a field in medicore breaks the lab and pharmacy builds.

- [ ] **Step 1: Create the contract package**

`backend/internal/modules/medicore/contract/events.go`:

```go
// Package contract is medicore's published event interface: the subject
// constants and payload types other modules may depend on.
//
// It is the ONE part of a module other modules may import (see
// docs/standards/backend.md and internal/archtest/contract_test.go).
// That exception exists so a publisher and its consumers share one
// definition instead of three copies — before this, a renamed field
// left consumers unmarshalling into a struct whose json tags no longer
// matched, which encoding/json reports as no error at all and a zero
// value, writing an empty patient name into a real clinical record
// (#827).
//
// Data only: consts, types and vars. No funcs, no methods, and no
// imports beyond time and uuid. A method here would be behaviour
// crossing the module boundary, which is what the isolation rule exists
// to stop; an import of medicore itself would re-open that boundary
// transitively, because this package is importable by everyone.
package contract

// SubjectVisitCreated is published when a visit opens. Pharmacy and lab
// consume it to open their pending work.
const SubjectVisitCreated = "helivanta.in.medicore.visit_created.v1"

// VisitCreatedData is the v1 payload of SubjectVisitCreated.
//
// The json tags are the wire contract. Renaming a field here is a
// breaking change to every consumer, and is meant to break their build —
// that is the whole point of this package existing.
type VisitCreatedData struct {
	VisitID     string `json:"visit_id"`
	PatientName string `json:"patient_name"`
}
```

- [ ] **Step 2: Point medicore at it and confirm the build fails for the right reason**

Delete `SubjectVisitCreated` from `medicore/module.go:13` and `VisitCreatedData` from `medicore/visits.go`. Add the import to both files:

```go
	medicorecontract "github.com/tesserix/helivanta/internal/modules/medicore/contract"
```

and use `medicorecontract.SubjectVisitCreated` / `medicorecontract.VisitCreatedData`.

Run:

```bash
cd backend && go build ./...
```

Expected: FAIL in `pharmacy` and `lab` — they still declare their own copies, which is fine, but `medicore`'s own test files referencing the old names will not resolve. Fix those references to the contract package. Do **not** touch pharmacy or lab yet.

- [ ] **Step 3: Allow the exception in depguard**

`backend/.golangci.yml`, in the `module-isolation` rule:

```yaml
        module-isolation:
          files:
            - "**/internal/modules/**"
          deny:
            - pkg: "github.com/tesserix/helivanta/internal/modules"
              desc: "modules must not import other modules — cross-module data flows only via events (spec D3)"
          allow:
            # The ONE exception: a module's contract package is its
            # published event interface — subject constants and payload
            # types, data only. internal/archtest/contract_test.go
            # enforces that it stays data only; without that test this
            # allowance would be a hole in module isolation rather than a
            # narrow exception to it (#827).
            - "github.com/tesserix/helivanta/internal/modules/medicore/contract"
```

**Only medicore's entry, at this point.** The other four contract packages do not exist until Task 3, and listing paths for packages that are not there would be an unverifiable claim in a config file — add each entry in the task that creates the package.

Note: depguard's `allow` is a prefix list, so each package is named explicitly. If a future module's contract must be added and someone forgets, the lint fails loudly at the import — which is the correct direction.

- [ ] **Step 4: Allow the exception in the arch test**

`backend/internal/archtest/arch_test.go`, in `TestModulesDoNotImportEachOther`:

```go
// isContractImport reports whether imp is a module's published contract
// package — the one cross-module import permitted.
//
// Suffix match on "/contract" rather than an allowlist of paths: the
// point is the *shape* of the exception, not which modules currently use
// it, and a new module's contract should be legal to import the day it
// exists. What keeps this from being a hole is contract_test.go, which
// enforces that anything living behind this name is data only.
func isContractImport(imp string) bool {
	return strings.HasSuffix(imp, "/contract")
}

func TestModulesDoNotImportEachOther(t *testing.T) {
	pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedName | packages.NeedImports, Tests: true}, modulesPrefix+"...")
	if err != nil {
		t.Fatalf("load packages: %v", err)
	}
	for _, p := range pkgs {
		from := moduleOf(p.PkgPath)
		for imp := range p.Imports {
			to := moduleOf(imp)
			if to == "" || to == from {
				continue
			}
			if isContractImport(imp) {
				continue
			}
			t.Errorf("module %q imports module %q (%s -> %s): cross-module data flows only via events, except a module's /contract package", from, to, p.PkgPath, imp)
		}
	}
}
```

- [ ] **Step 5: Migrate pharmacy's consumer, deleting its duplicates**

In `pharmacy/module.go`, delete the `subjectVisitCreated` constant entirely. In `pharmacy/consumers.go`, delete the local `visitCreatedData` struct and use the contract:

```go
import (
	"context"
	"encoding/json"

	"gorm.io/gorm"

	medicorecontract "github.com/tesserix/helivanta/internal/modules/medicore/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/events"
)

func (m *Module) Consumers(deps platform.Deps) []events.Consumer {
	return []events.Consumer{{
		Name:    "pharmacy-visit-intake",
		Subject: medicorecontract.SubjectVisitCreated,
		Handle: func(ctx context.Context, tx *gorm.DB, evt events.Event) error {
			var d medicorecontract.VisitCreatedData
			if err := json.Unmarshal(evt.Data, &d); err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO pharmacy_dispenses (tenant_id, visit_id, patient_name)
				VALUES (?, ?, ?)`, evt.TenantID, d.VisitID, d.PatientName).Error
		},
	}}
}
```

Read the existing file first — the SQL and column list above must match what is actually there, not this sketch.

- [ ] **Step 6: Migrate lab's consumer the same way**

Delete `subjectVisitCreated` from `lab/module.go` and `visitCreatedData` from `lab/consumers.go`, importing `medicorecontract` in its place. Keep `SubjectResultReady` where it is for now — Task 3 moves it.

- [ ] **Step 7: Build and run the suite**

```bash
cd backend && go build ./... && go vet ./... && go test -count=1 -race ./...
cd /Users/Mahesh.Sangawar/personal/tesserix-new/hms && make lint-go
```

Expected: all green. `grep -rn "subjectVisitCreated\|visitCreatedData" backend --include="*.go"` must return nothing.

- [ ] **Step 8: Prove the headline property — the mutation that justifies the whole design**

In `medicore/contract/events.go`, rename `PatientName` to `Patient` (field name only, leave the json tag):

```bash
cd backend && go build ./... 2>&1 | head -10
```

Expected: FAIL, naming `pharmacy/consumers.go` and `lab/consumers.go` with "d.PatientName undefined". Restore, rebuild, confirm green.

Record the exact output — this is the evidence the issue's primary acceptance criterion is met.

- [ ] **Step 9: Prove the exception is narrow**

Temporarily add to `lab/consumers.go`:

```go
	_ "github.com/tesserix/helivanta/internal/modules/medicore"
```

Run:

```bash
cd backend && go test ./internal/archtest/ -run TestModulesDoNotImportEachOther -v
cd /Users/Mahesh.Sangawar/personal/tesserix-new/hms && make lint-go
```

Expected: BOTH fail — the arch test naming `lab imports medicore`, and depguard rejecting the import. Remove it and confirm both go green.

This proves the exception admits `/contract` and nothing else. Without it, Step 4's `continue` could have been written to skip too much and no test would notice.

- [ ] **Step 10: Commit**

```bash
git add backend/internal/modules/ backend/.golangci.yml backend/internal/archtest/arch_test.go
git commit -m "feat: give medicore a contract package so its consumers share one payload type instead of copying it (#827)"
```

---

## Task 2: police the exception

**Files:**
- Create: `backend/internal/archtest/contract_test.go`

**Interfaces:**
- Consumes: the contract packages from Task 1.
- Produces: nothing consumed by later tasks.

Task 1 opened a door. This shuts it to everything except data.

- [ ] **Step 1: Write the failing data-only test**

```go
package archtest

import (
	"go/ast"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/tools/go/packages"
)

// contractAllowedImports is everything a contract package may import.
//
// Deliberately tiny. A contract describes a shape; it does not do
// anything. Anything beyond these two either implies behaviour (gorm,
// gin, pkg/events) or re-opens module isolation transitively (any
// internal/ path), because contract packages are importable by every
// module.
var contractAllowedImports = map[string]string{
	"time":                    "timestamps in payloads",
	"github.com/google/uuid":  "identifiers in payloads",
}

func loadContractPackages(t *testing.T) []*packages.Package {
	t.Helper()
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedImports | packages.NeedSyntax | packages.NeedFiles,
	}, modulesPrefix+"...")
	require.NoError(t, err)

	var out []*packages.Package
	for _, p := range pkgs {
		if strings.HasSuffix(p.PkgPath, "/contract") {
			out = append(out, p)
		}
	}
	require.NotEmpty(t, out, "no contract packages found — this test would pass vacuously")
	return out
}

// TestContractPackagesDeclareOnlyData is what keeps the module-isolation
// exception narrow. A func or method on a contract type is behaviour
// crossing the module boundary — exactly what the isolation rule exists
// to stop — arriving through the one door that rule leaves open.
func TestContractPackagesDeclareOnlyData(t *testing.T) {
	for _, p := range loadContractPackages(t) {
		for _, file := range p.Syntax {
			for _, decl := range file.Decls {
				fn, isFunc := decl.(*ast.FuncDecl)
				if !isFunc {
					continue
				}
				t.Errorf("%s declares func %q: a contract package is data only (const, type, var). Behaviour belongs in the module, or in pkg/ if it is genuinely shared",
					p.PkgPath, fn.Name.Name)
			}
		}
	}
}

// TestContractPackagesImportAlmostNothing is the other half. A contract
// that could import its own module would make every module reachable
// from every other module transitively, since contract packages are
// importable by all of them.
func TestContractPackagesImportAlmostNothing(t *testing.T) {
	for _, p := range loadContractPackages(t) {
		for imp := range p.Imports {
			if _, ok := contractAllowedImports[imp]; ok {
				continue
			}
			t.Errorf("%s imports %q: a contract package may import only %v — anything else either implies behaviour or re-opens module isolation transitively",
				p.PkgPath, imp, keysOfStringMap(contractAllowedImports))
		}
	}
}
```

Add `keysOfStringMap(m map[string]string) []string` returning sorted keys, or reuse an existing helper if `arch_test.go` already has one — read it first.

- [ ] **Step 2: Run and confirm it passes**

```bash
cd backend && go test ./internal/archtest/ -run TestContractPackages -v
```

Expected: PASS — Task 1's contract package is already data only.

- [ ] **Step 3: Prove both tests can fail**

Add to `medicore/contract/events.go`:

```go
func (v VisitCreatedData) Describe() string { return v.PatientName }
```

Run the tests: `TestContractPackagesDeclareOnlyData` must fail naming `Describe`. Remove it.

Then add an import:

```go
import "gorm.io/gorm"

var _ = gorm.ErrRecordNotFound
```

`TestContractPackagesImportAlmostNothing` must fail naming `gorm.io/gorm`. Remove both, confirm green.

- [ ] **Step 4: Prove the loader is not vacuous**

Temporarily change `loadContractPackages`'s suffix from `/contract` to `/nonexistent`. The `require.NotEmpty` must fail. Restore.

Without this, a typo in the suffix would make both tests pass while checking nothing — the exact failure mode §5 warns about.

- [ ] **Step 5: Commit**

```bash
cd backend && go test -race ./internal/archtest/ && cd .. && make lint-go
git add backend/internal/archtest/contract_test.go
git commit -m "test: keep the contract exception data only, so module isolation cannot widen through it (#827)"
```

---

## Task 3: the remaining four contract packages

**Files:**
- Create: `backend/internal/modules/pharmacy/contract/events.go`, `lab/contract/events.go`, `iam/contract/events.go`, `reference/contract/events.go`
- Modify: `pharmacy/module.go`, `dispenses.go`; `lab/module.go`, `orders.go`; `iam/module.go`, `signout.go`, `sync.go`; `reference/module.go`, `pings.go`, `consumers.go`

**Interfaces:**
- Produces: `pharmacycontract.SubjectDispenseRecorded`, `DispenseRecordedData{DispenseID, VisitID string}`; `labcontract.SubjectResultReady`, `ResultReadyData{OrderID, VisitID string}`; `iamcontract.SubjectMemberGranted`, `SubjectMemberRevoked`, `SubjectCredentialRevoked`, `MemberChangedData{Subject, RoleKey string}`, `CredentialRevokedData{Subject string}`; `referencecontract.SubjectPinged`, `PingedData{PingID string}`.

Four of these events are intra-module today. They get contract packages anyway: an intra-module event becomes cross-module the moment a second module wants it, and at that point the choice is to move the contract (touching publisher, new consumer and every test) or to copy it — which is how the current state arose. It is also forced by Task 4's check that every `Publishes()` entry is a contract constant.

- [ ] **Step 1: Create `pharmacy/contract/events.go`**

```go
// Package contract is pharmacy's published event interface. See
// medicore/contract for why this package exists and why it is data only.
package contract

// SubjectDispenseRecorded is published when a pharmacist records a
// dispense. No module consumes it today; publishing it is how the
// dispense becomes visible to anything added later without changing
// pharmacy.
const SubjectDispenseRecorded = "helivanta.in.pharmacy.dispense_recorded.v1"

// DispenseRecordedData is the v1 payload of SubjectDispenseRecorded.
type DispenseRecordedData struct {
	DispenseID string `json:"dispense_id"`
	VisitID    string `json:"visit_id"`
}
```

Delete `SubjectDispenseRecorded` from `pharmacy/module.go` and `dispenseRecordedData` from `dispenses.go`; import as `pharmacycontract` and use `pharmacycontract.DispenseRecordedData`.

**The type is now exported where it was `dispenseRecordedData`.** Check every reference including tests.

- [ ] **Step 2: Create `lab/contract/events.go`**

```go
// Package contract is lab's published event interface. See
// medicore/contract for why this package exists and why it is data only.
package contract

// SubjectResultReady is published when a lab result is recorded. No
// module consumes it today.
const SubjectResultReady = "helivanta.in.lab.result_ready.v1"

// ResultReadyData is the v1 payload of SubjectResultReady.
type ResultReadyData struct {
	OrderID string `json:"order_id"`
	VisitID string `json:"visit_id"`
}
```

Delete `SubjectResultReady` from `lab/module.go` and `resultReadyData` from `orders.go`.

- [ ] **Step 3: Create `iam/contract/events.go`**

```go
// Package contract is iam's published event interface. See
// medicore/contract for why this package exists and why it is data only.
package contract

const (
	// SubjectMemberGranted and SubjectMemberRevoked drive the FGA sync
	// consumers that keep OpenFGA's tuples matching Postgres.
	SubjectMemberGranted = "helivanta.in.iam.member_granted.v1"
	SubjectMemberRevoked = "helivanta.in.iam.member_revoked.v1"

	// SubjectCredentialRevoked is a broadcast, not a work queue: every
	// replica receives it and drops its cached revocation watermark
	// (#781). The durable truth is Postgres, so a dropped delivery
	// degrades to the cache TTL rather than to incorrectness.
	SubjectCredentialRevoked = "helivanta.in.iam.credential_revoked.v1" //nolint:gosec // an event subject name, not a credential value

)

// MemberChangedData is the v1 payload of both member_granted and
// member_revoked — the two events carry the same shape and differ only
// in what the consumer does with it.
type MemberChangedData struct {
	Subject string `json:"subject"`
	RoleKey string `json:"role_key"`
}

// CredentialRevokedData is the v1 payload of credential_revoked. It
// carries only the subject: every replica needs to know which cache
// entry to drop, and nothing else about a revocation belongs on a bus.
type CredentialRevokedData struct {
	Subject string `json:"subject"`
}
```

Note the `//nolint:gosec` — the existing constant carries one because gosec flags identifiers containing "credential". Carry the comment across or lint will fail.

Delete the three subjects and both payload types from `iam/module.go` and `iam/signout.go`; update `sync.go` and the `Broadcasts` handler in `module.go`.

- [ ] **Step 4: Create `reference/contract/events.go`**

```go
// Package contract is reference's published event interface. See
// medicore/contract for why this package exists and why it is data only.
package contract

// SubjectPinged is published when a ping is recorded. reference consumes
// its own event — it is the module that proves the platform wiring end
// to end (issue #2).
const SubjectPinged = "helivanta.in.reference.pinged.v1"

// PingedData is the v1 payload of SubjectPinged.
type PingedData struct {
	PingID string `json:"ping_id"`
}
```

Delete `SubjectPinged` from `reference/module.go` and `pingedData` from `pings.go`; update `consumers.go`.

- [ ] **Step 4a: Extend the depguard allow list to the four new packages**

Task 1 added only medicore's entry, because the others did not exist. Add them now:

```yaml
            - "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
            - "github.com/tesserix/helivanta/internal/modules/lab/contract"
            - "github.com/tesserix/helivanta/internal/modules/iam/contract"
            - "github.com/tesserix/helivanta/internal/modules/reference/contract"
```

None of these four is imported across a module boundary today — every one is consumed by its own module, which needs no allowance. They are listed so that the first cross-module consumer of any of them is a code change in one place rather than a lint failure someone has to diagnose. If that reasoning does not hold when you get here (for example depguard rejects an unused allow entry), list only what is imported and say so in your report.

- [ ] **Step 5: Build, test, lint**

```bash
cd backend && go build ./... && go vet ./... && go test -count=1 -race ./...
cd /Users/Mahesh.Sangawar/personal/tesserix-new/hms && make lint-go
```

Expected: green. Then confirm nothing was left behind:

```bash
grep -rn "dispenseRecordedData\|resultReadyData\|pingedData" backend --include="*.go"
```

Expected: no output.

- [ ] **Step 6: Prove the arch tests still hold**

```bash
cd backend && go test -race ./internal/archtest/ -run 'TestContractPackages|TestModulesDoNotImportEachOther' -v
```

Expected: PASS for all four new packages — the data-only test now covers five packages, not one. Confirm the count by adding a temporary `t.Logf("%d contract packages", len(...))` in `loadContractPackages` and checking it prints 5. Remove the log.

- [ ] **Step 7: Commit**

```bash
git add backend/internal/modules/
git commit -m "feat: give every module a contract package, so an intra-module event needs no move when a consumer appears (#827)"
```

---

## Task 4: `Publishes()` and the subscribe/publish checks

**Files:**
- Modify: `backend/internal/platform/module.go`, every `internal/modules/*/module.go`, `backend/internal/platform/registry_test.go`, `backend/internal/archtest/arch_test.go`
- Create: `backend/internal/archtest/events_test.go`

**Interfaces:**
- Consumes: the contract packages from Tasks 1 and 3.
- Produces: `platform.Module.Publishes() []string`.

- [ ] **Step 1: Add the interface method**

`backend/internal/platform/module.go`:

```go
	// Publishes declares every subject this module publishes.
	//
	// Declarative like Permissions and Migrations, and for the same
	// reason: it makes a property checkable that is otherwise scattered
	// across call sites. Two CI checks build on it — that every consumer
	// subscribes to something a module actually publishes, and that no
	// two modules publish the same subject — and the event catalogue
	// (#667) is its union, so the catalogue cannot drift from the code.
	//
	// Entries must be constants from this module's own contract package,
	// not bare strings; TestPublishesUsesContractConstants enforces it.
	// A module that publishes nothing returns nil.
	Publishes() []string
```

- [ ] **Step 2: Implement it on all five modules**

medicore:

```go
func (m *Module) Publishes() []string {
	return []string{medicorecontract.SubjectVisitCreated}
}
```

pharmacy: `[]string{pharmacycontract.SubjectDispenseRecorded}`.
lab: `[]string{labcontract.SubjectResultReady}`.
iam: `[]string{iamcontract.SubjectMemberGranted, iamcontract.SubjectMemberRevoked, iamcontract.SubjectCredentialRevoked}`.
reference: `[]string{referencecontract.SubjectPinged}`.

Every test double implementing `platform.Module` also needs the method — `go build ./...` will find them. There are doubles in `internal/platform/reconcile_test.go`, `registry_test.go` and `internal/archtest/`; return `nil` from each.

- [ ] **Step 3: Write the failing CI checks**

`backend/internal/archtest/events_test.go`:

```go
// TestEveryConsumedSubjectIsPublished closes the quiet half of #827. A
// publisher bumping .v1 to .v2, or deleting an event, leaves its
// consumers subscribed to a subject nobody sends. There is no wrong
// data to notice — pharmacy and lab intake simply stops.
func TestEveryConsumedSubjectIsPublished(t *testing.T) {
	published := map[string]string{} // subject -> publishing module
	for _, m := range allModules() {
		for _, s := range m.Publishes() {
			published[s] = m.Name()
		}
	}

	for _, m := range allModules() {
		for _, c := range m.Consumers(platform.Deps{}) {
			_, ok := published[c.Subject]
			require.True(t, ok,
				"consumer %q in module %q subscribes to %q, which no module publishes — it will receive nothing, silently",
				c.Name, m.Name(), c.Subject)
		}
		for _, b := range m.Broadcasts(platform.Deps{}) {
			_, ok := published[b.Subject]
			require.True(t, ok,
				"broadcast in module %q subscribes to %q, which no module publishes",
				m.Name(), b.Subject)
		}
	}
}

// TestNoSubjectIsPublishedByTwoModules keeps ownership unambiguous: a
// subject names one publisher's contract, and two modules publishing it
// means a consumer cannot know whose payload shape it is getting.
func TestNoSubjectIsPublishedByTwoModules(t *testing.T) {
	owner := map[string]string{}
	for _, m := range allModules() {
		for _, s := range m.Publishes() {
			if prev, dup := owner[s]; dup {
				t.Errorf("subject %q is published by both %q and %q: one subject, one publisher, one contract", s, prev, m.Name())
				continue
			}
			owner[s] = m.Name()
		}
	}
}

// TestPublishesUsesContractConstants stops Publishes() drifting from the
// contract. A bare string here would satisfy the two checks above while
// no longer matching what the publisher actually sends.
func TestPublishesUsesContractConstants(t *testing.T) {
	contractSubjects := map[string]bool{}
	for _, p := range loadContractPackages(t) {
		for _, file := range p.Syntax {
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.CONST {
					continue
				}
				for _, spec := range gen.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, v := range vs.Values {
						lit, ok := v.(*ast.BasicLit)
						if !ok || lit.Kind != token.STRING {
							continue
						}
						contractSubjects[strings.Trim(lit.Value, `"`)] = true
					}
				}
			}
		}
	}
	require.NotEmpty(t, contractSubjects, "no contract constants found — this test would pass vacuously")

	for _, m := range allModules() {
		for _, s := range m.Publishes() {
			require.True(t, contractSubjects[s],
				"module %q publishes %q, which is not a constant in any contract package — declare it in %s/contract so publisher and consumers share one definition",
				m.Name(), s, m.Name())
		}
	}
}
```

- [ ] **Step 4: Run and confirm they pass**

```bash
cd backend && go test -race ./internal/archtest/ -run 'TestEveryConsumedSubject|TestNoSubjectIsPublished|TestPublishesUsesContract' -v
```

Expected: PASS. The two consumerless events (`dispense_recorded`, `result_ready`) must not fail anything — nothing checks that a published subject has a consumer, deliberately.

- [ ] **Step 5: Prove each of the three can fail**

1. Change lab's consumer subject to `"helivanta.in.medicore.visit_created.v2"`. `TestEveryConsumedSubjectIsPublished` must fail naming the consumer and the subject. Restore.
2. Add `medicorecontract.SubjectVisitCreated` to pharmacy's `Publishes()`. `TestNoSubjectIsPublishedByTwoModules` must fail naming both modules. Restore.
3. Change medicore's `Publishes()` to `[]string{"helivanta.in.medicore.visit_created.v1"}` — the same value as a bare string. `TestPublishesUsesContractConstants` must **pass** (the string matches a contract constant), which shows this check verifies the *value*, not the *reference*. Then change it to `"helivanta.in.medicore.visit_created.v9"` and confirm it fails. Restore.

Step 5.3 matters: it establishes exactly what the check does and does not prove, so the limitation is discovered here rather than believed away. Record it.

- [ ] **Step 6: Commit**

```bash
cd backend && go build ./... && go test -count=1 -race ./... && cd .. && make lint-go
git add backend/internal/platform/module.go backend/internal/modules/ backend/internal/archtest/
git commit -m "feat: declare what each module publishes, so a consumer cannot subscribe to nothing (#827)"
```

---

## Task 5: the consumer unmarshal-type check

**Files:**
- Modify: `backend/internal/archtest/events_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–4.

This is T2, the standing guard against someone re-introducing a local payload copy later. The spec justifies it explicitly against the AST check rejected in D3 — read that section before starting, and if the check proves fragile in practice, say so rather than forcing it.

- [ ] **Step 1: Write the check**

```go
// TestConsumersUnmarshalIntoContractTypes is the standing guard against
// a local payload copy coming back. Tasks 1 and 3 deleted the copies;
// nothing yet stops a new consumer declaring its own struct and
// unmarshalling into that, which is exactly the state #827 fixed.
//
// This resolves the TYPE of the second argument to
// json.Unmarshal(evt.Data, &d) using go/packages type information —
// which the type checker has already computed. That is why it is
// acceptable where walking bus.Publish call sites was not: that check
// had to resolve a constant VALUE through arbitrary indirection and
// fails opaquely. This fails loudly: a type it cannot resolve is a
// failure, not a skip.
func TestConsumersUnmarshalIntoContractTypes(t *testing.T) {
	pkgs, err := packages.Load(&packages.Config{
		Mode: packages.NeedName | packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports,
	}, modulesPrefix+"...")
	require.NoError(t, err)

	checked := 0
	for _, p := range pkgs {
		if strings.HasSuffix(p.PkgPath, "/contract") || strings.HasSuffix(p.PkgPath, ".test") {
			continue
		}
		for _, file := range p.Syntax {
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || len(call.Args) != 2 {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || sel.Sel.Name != "Unmarshal" {
					return true
				}
				pkgIdent, ok := sel.X.(*ast.Ident)
				if !ok || pkgIdent.Name != "json" {
					return true
				}
				// Only calls unmarshalling an event payload.
				argSel, ok := call.Args[0].(*ast.SelectorExpr)
				if !ok || argSel.Sel.Name != "Data" {
					return true
				}

				checked++
				typ := p.TypesInfo.TypeOf(call.Args[1])
				require.NotNil(t, typ, "%s: cannot resolve the type unmarshalled from evt.Data", p.PkgPath)

				name := typ.String() // e.g. *github.com/.../medicore/contract.VisitCreatedData
				require.Contains(t, name, "/contract.",
					"%s unmarshals an event payload into %s, which is not a contract type — declare the payload in the publishing module's contract package so a renamed field breaks this build instead of writing a zero value (#827)",
					p.PkgPath, name)
				return true
			})
		}
	}
	require.Positive(t, checked, "no json.Unmarshal(evt.Data, …) call sites found — this test would pass vacuously")
}
```

- [ ] **Step 2: Run it**

```bash
cd backend && go test ./internal/archtest/ -run TestConsumersUnmarshalIntoContractTypes -v
```

Expected: PASS, with `checked` > 0. If it fails on a call site that is legitimately not an event payload, narrow the match rather than loosening the assertion, and say which call site in your report.

- [ ] **Step 3: Prove it can fail**

In `lab/consumers.go`, temporarily reintroduce a local copy:

```go
type localVisitCreated struct {
	VisitID     string `json:"visit_id"`
	PatientName string `json:"patient_name"`
}
```

and unmarshal into it. The test must fail naming `lab` and the local type. Restore.

- [ ] **Step 4: Prove it is not vacuous**

Change the `argSel.Sel.Name != "Data"` guard to `!= "NotAField"`. `require.Positive(t, checked, …)` must fail. Restore.

- [ ] **Step 5: Commit**

```bash
cd backend && go test -race ./internal/archtest/ && cd .. && make lint-go
git add backend/internal/archtest/events_test.go
git commit -m "test: require consumers to unmarshal into contract types, so a local copy cannot come back (#827)"
```

---

## Task 6: generator and standards

**Files:**
- Modify: `backend/scripts/new-module.sh`, `docs/standards/backend.md`

- [ ] **Step 1: Update the generator template**

The generated module must produce `internal/modules/<name>/contract/events.go`:

```go
// Package contract is __NAME__'s published event interface. Data only:
// consts, types and vars, importing nothing beyond time and uuid. See
// docs/standards/backend.md and internal/archtest/contract_test.go.
package contract

// SubjectItemCreated is published when an item is created.
const SubjectItemCreated = "helivanta.in.__NAME__.item_created.v1"

// SubjectItemDone is published when an item transitions to done.
const SubjectItemDone = "helivanta.in.__NAME__.item_done.v1"

// ItemCreatedData is the v1 payload of SubjectItemCreated.
type ItemCreatedData struct {
	ItemID string `json:"item_id"`
}

// ItemDoneData is the v1 payload of SubjectItemDone.
type ItemDoneData struct {
	ItemID string `json:"item_id"`
}
```

and `module.go` must gain:

```go
func (m *Module) Publishes() []string {
	return []string{contract.SubjectItemCreated, contract.SubjectItemDone}
}
```

with the subject constants and payload types removed from `module.go` itself. Add the contract import, aliased `__NAME__contract`.

**Also add the new contract package to `backend/.golangci.yml`'s depguard allow list** — or the generated module's own consumer import will fail lint. If that manual step is unavoidable, the generator must print a reminder, exactly as it already prints one about `allModules()`.

- [ ] **Step 2: Prove the generator's output works**

```bash
cd backend && ./scripts/new-module.sh scratchev && go build ./... && go test ./internal/modules/scratchev/
```

Expected: builds and its generated tests pass. Then register it temporarily in `internal/bootstrap/modules.go` and `allModules()`, and run:

```bash
cd backend && go test -race ./internal/archtest/
```

Expected: every check passes with no allowlist edit beyond the depguard entry — proving a new module is contract-shaped by default. Unregister, `rm -rf internal/modules/scratchev`, revert the depguard entry, and confirm the tree is clean before the final gates.

Do not skip this. The generator was found broken on `main` during #816 precisely because nobody built its output.

- [ ] **Step 3: Document the rule**

Add to `docs/standards/backend.md`, in the events section:

```markdown
**An event's subject and payload live in the publishing module's `contract`
package, and nowhere else.** `internal/modules/<module>/contract` is the one
part of a module that other modules may import — the exception exists so a
publisher and its consumers share one definition instead of copies, because a
renamed field in a copied struct is not an error to `encoding/json`, just a
zero value written into a real record (#827).

Contract packages are **data only**: `const`, `type`, `var`, no funcs or
methods, importing nothing beyond `time` and `github.com/google/uuid`.
`TestContractPackagesDeclareOnlyData` and `TestContractPackagesImportAlmostNothing`
enforce it; without them the exception would be a hole in module isolation
rather than a narrow opening in it.

Import a foreign contract aliased `<module>contract` — two packages both named
`contract` will not compile unaliased, and `contract.VisitCreatedData` does not
tell a reader whose contract it is.

Every module declares `Publishes() []string`, using constants from its own
contract package. `TestEveryConsumedSubjectIsPublished` fails any consumer
subscribed to a subject no module publishes; `TestNoSubjectIsPublishedByTwoModules`
keeps one subject to one publisher. A published subject with no consumer is
legal and checked by nothing.
```

- [ ] **Step 4: Commit**

```bash
cd backend && go test -count=1 -race ./... && cd .. && make lint-go
git add backend/scripts/new-module.sh docs/standards/backend.md
git commit -m "feat: generate new modules with a contract package and Publishes (#827)"
```

---

## Task 7: verification, spec corrections and PR

- [ ] **Step 1: Full gates**

```bash
cd backend && go build ./... && go vet ./... && go test -count=1 -race ./... && ./scripts/coverage-gate.sh
cd /Users/Mahesh.Sangawar/personal/tesserix-new/hms && make lint-go
pnpm turbo lint type-check test build
```

No frontend change is expected; the gate runs to prove that.

- [ ] **Step 2: Verify the events still flow end to end**

```bash
export HELIVANTA_PG_PORT=15432 HELIVANTA_NATS_PORT=14222 HELIVANTA_NATS_MONITOR_PORT=18222 \
  HELIVANTA_REDIS_PORT=16379 HELIVANTA_OPENFGA_PORT=18090 HELIVANTA_GIP_PORT=19099 \
  HELIVANTA_API_PORT=18080 NODE_AUTH_TOKEN=$(gh auth token)
make up && ./scripts/verify-local.sh
pnpm --filter e2e exec playwright test --reporter=list
```

Run from the repo root. `journey.spec.ts` is the one that matters — it creates a visit and asserts it lands in pharmacy and lab, which is `visit_created` crossing two module boundaries through the contract package. Note: `pnpm turbo build` overwrites the dev servers' `.next` output, so build first, restart `make dev-web`, then run E2E.

- [ ] **Step 3: Correct the spec**

Change the header from `approved` to `implemented` and fix what the implementation found:

- **The spec says "five subjects"; there are seven.** `lab` also publishes `result_ready`, and both it and `dispense_recorded` have no consumers. Correct the count, the migration table, and the sentence claiming only `visit_created` is duplicated (that part is right — correct only the count).
- Record what `TestPublishesUsesContractConstants` actually proves: it compares the *value* against contract constants, so a bare string equal to a contract constant passes. It catches drift, not indirection.
- Record whether T2 (the unmarshal-type check) proved fragile, and if it did, that enforcement of the payload-sharing property degrades to convention plus review.
- Record any depguard manual step the generator could not automate.

- [ ] **Step 4: Push and open the PR**

```bash
git add -A && git commit -m "docs: mark the event contract registry design implemented"
git push -u origin feat/827-event-contract-registry
```

PR body must carry: link to #827; the reproduction (`unmarshal error: <nil>`, `patient_name=""`); the seven events and which were duplicated; why the contract lives in the publishing module rather than centrally, citing ADR-0005; the isolation exception and the two arch tests that keep it narrow; the `Publishes()` checks and exactly what they do and do not prove; the limitations below; and which assertions were observed failing, with the Task 1 Step 8 build failure quoted.

Close with `Closes #827`. **Do not merge.**

---

## Known limitations (carry into the PR body)

- **A declared publication is not proven to happen.** `Publishes()` is a declaration; a module could list a subject it never publishes and CI would be satisfied. The AST alternative was rejected in the spec's D3 as brittle.
- **`TestPublishesUsesContractConstants` compares values, not references.** A bare string equal to a contract constant passes. It catches drift, not indirection.
- **No versioning or upcasting.** `.v1` and `.v2` coexisting during a rollout is #667's schema-evolution territory.
- **PHI in event payloads is untouched** — a separate #774 Tier 2 item. `visit_created` still carries `patient_name`.
- **Payload types carry no behaviour** by construction. Shared validation must live in `pkg/`.
- **depguard's allow list names each contract package explicitly**, so adding a module requires an entry. The generator prints a reminder; nothing fails at generation time.

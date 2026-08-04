# HMS Phase 2 — Pharmacy & Lab Zones Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship pharmacy and lab zones with a real thin-slice domain (visits → pending dispenses/orders via JetStream), tenant-scoped event consumers, and a shared two-rail sidebar (`@hms/ui`) using tesserix-home's color tokens.

**Architecture:** Three new Go modules (`medicore`, `pharmacy`, `lab`) follow the `reference` module pattern; medicore publishes `visit_created`, pharmacy and lab consume it into pending work rows (the bus now sets the tenant GUC inside consumer transactions). Frontend: new workspace package `packages/ui` holds the two-rail chrome + zone registry + sidebar tokens; new zone apps `apps/pharmacy` (:4303) and `apps/lab` (:4304) are stitched via shell rewrites.

**Tech Stack:** Go 1.26, Gin, GORM + PostgreSQL 16 (forced RLS), NATS JetStream, testcontainers-go, Next.js 16, React 19, Tailwind v4, `@tesserix/web` 1.8.x, lucide-react, pnpm + Turborepo, Playwright.

**Spec:** `docs/superpowers/specs/2026-08-04-phase2-pharmacy-lab-zones-design.md`

## Global Constraints

- Go module path is exactly `github.com/tesserix/hms`, rooted at `backend/`.
- Backend module boundaries: `internal/modules/<name>` must never import another module's packages; cross-module data flows only via events.
- Every table with a `tenant_id` column MUST have RLS enabled **and forced** with a policy carrying both `USING` and `WITH CHECK`; the RLS linter runs at boot and in tests.
- Runtime DB access only via `tenantdb.WithTenant` / `WithSystem` — no exported raw `*gorm.DB`.
- Migration IDs are globally unique: this phase uses `0001_medicore`, `0001_pharmacy`, `0001_lab`.
- Auth is GIP only. The string `MedCora` must never appear in the repo.
- All sidebar links are plain `<a>` tags (hard navigation across zones, spec D5).
- Frontend ports: shell 4301, medicore 4302, pharmacy 4303, lab 4304; backend API 8080. Dev infra: Postgres 5432, NATS 4222, Redis 6379, OpenFGA 8090, Firebase Auth emulator 9099.
- Commit messages: conventional commits, single line, no signatures.
- JS package manager: pnpm via corepack; Node 22. Run `pnpm install` from the repo root after adding any workspace package.
- Backend tests require Docker (testcontainers). Run them with `cd backend && go test ./<pkg>/`.

---

### Task 1: events — tenant GUC inside consumer transactions

**Files:**
- Modify: `backend/pkg/events/bus.go` (function `handleMsg`)
- Test: `backend/pkg/events/bus_test.go` (append one test)

**Interfaces:**
- Consumes: existing `handleMsg(ctx, db, c, msg)` which runs the idempotency claim and `c.Handle(ctx, tx, evt)` in one `WithSystem` transaction.
- Produces: inside that same transaction, when `evt.TenantID` parses as a UUID, the GUC `app.tenant_id` is set transaction-locally before `Handle` runs — so consumer handlers can INSERT/SELECT tenant-scoped (RLS-forced) rows for the event's tenant. Invalid/empty tenant → GUC stays unset (RLS yields zero rows), unchanged behavior.

- [ ] **Step 1: Write the failing test**

Append to `backend/pkg/events/bus_test.go`:

```go
func TestConsumerTenantScopedWrite(t *testing.T) {
	appDSN, adminDSN := testutil.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, adminDSN)
	require.NoError(t, err)

	migs := append(events.Migrations(), tenantdb.Migration{
		ID: "0002_consumer_widgets",
		SQL: `
			CREATE TABLE consumer_widgets (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  note text NOT NULL,
			  created_at timestamptz NOT NULL DEFAULT now()
			);
			ALTER TABLE consumer_widgets ENABLE ROW LEVEL SECURITY;
			ALTER TABLE consumer_widgets FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON consumer_widgets
			  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);`,
	})
	require.NoError(t, db.Migrate(context.Background(), migs))

	bus, err := events.NewBus(testutil.StartNATS(t))
	require.NoError(t, err)
	defer bus.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tenantA, tenantB := "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"

	require.NoError(t, bus.StartConsumers(ctx, db, []events.Consumer{{
		Name:    "tenant-write-consumer",
		Subject: "hms.in.test.tenantwrite.v1",
		Handle: func(ctx context.Context, tx *gorm.DB, evt events.Event) error {
			// Relies on the bus having set app.tenant_id from evt.TenantID.
			return tx.Exec(`INSERT INTO consumer_widgets (tenant_id, note) VALUES (?, 'from-consumer')`,
				evt.TenantID).Error
		},
	}}))
	go bus.RunDispatcher(ctx, db)

	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		return bus.Publish(tx, "hms.in.test.tenantwrite.v1", events.Event{
			Type: "TenantWrite", Version: 1, TenantID: tenantA,
			Data: json.RawMessage(`{}`),
		})
	}))

	// Row lands for tenant A…
	require.Eventually(t, func() bool {
		var n int64
		_ = db.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM consumer_widgets`).Scan(&n).Error
		})
		return n == 1
	}, 15*time.Second, 100*time.Millisecond)

	// …and tenant B sees nothing (RLS held inside the consumer).
	var nB int64
	require.NoError(t, db.WithTenant(ctx, tenantB, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM consumer_widgets`).Scan(&nB).Error
	}))
	require.Zero(t, nB)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./pkg/events/ -run TestConsumerTenantScopedWrite -v`
Expected: FAIL — the consumer INSERT violates the RLS policy (or times out on `Eventually`) because `app.tenant_id` is unset in the consumer transaction.

- [ ] **Step 3: Implement**

In `backend/pkg/events/bus.go` `handleMsg`, inside the `db.WithSystem(ctx, func(tx *gorm.DB) error {` closure, immediately BEFORE the `INSERT INTO processed_events` idempotency claim, add:

```go
		// Scope the whole consumer tx (claim + handler) to the event's
		// tenant so handlers can write RLS-forced rows (phase 2 D4).
		// Invalid/empty tenant → GUC stays unset → tenant tables read
		// as empty and reject writes, same as before.
		if _, err := uuid.Parse(evt.TenantID); err == nil {
			if err := tx.Exec(`SELECT set_config('app.tenant_id', ?, true)`, evt.TenantID).Error; err != nil {
				return err
			}
		}
```

`bus.go` already imports `github.com/google/uuid`; if not, add it.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./pkg/events/ -v`
Expected: PASS — the new test AND the pre-existing `TestOutboxPublishDispatchConsume` (regression: the GUC set must not break non-tenant consumers).

- [ ] **Step 5: Commit**

```bash
git add backend/pkg/events/
git commit -m "feat: scope consumer transactions to the event tenant for rls writes"
```

---

### Task 2: medicore backend module — visits + visit_created event

**Files:**
- Create: `backend/internal/modules/medicore/module.go`
- Test: `backend/internal/modules/medicore/module_test.go`

**Interfaces:**
- Consumes: `platform.Module`/`platform.Deps` (`Deps{DB *tenantdb.DB, Bus *events.Bus}`), `authn.PrincipalFrom(c) (authn.Principal, bool)` where `Principal{Subject, TenantID string}`, `deps.DB.WithTenant(ctx, tenantID, fn)`, `deps.Bus.Publish(tx, subject, events.Event{Type, Version, TenantID, Data})`.
- Produces:
  - `medicore.New() *Module` implementing `platform.Module` with `Name() == "medicore"`.
  - Routes: `POST /medicore/visits` body `{"patient_name": string (required, max 200), "department": "OPD"|"IPD"}` → `202 {"id": "<uuid>"}`; `GET /medicore/visits` → `200 {"data": [{id, patient_name, department, status, created_at}]}` newest-first, limit 100.
  - Event `hms.in.medicore.visit_created.v1`, type `VisitCreated` v1, data `{"visit_id": string, "patient_name": string, "department": string}` — Tasks 3 and 4 consume this; the constant is `medicore.SubjectVisitCreated` but consumers repeat the string literal (modules must not import each other).

- [ ] **Step 1: Write the failing test**

`backend/internal/modules/medicore/module_test.go`:

```go
package medicore_test

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

	"github.com/tesserix/hms/internal/modules/medicore"
	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/internal/testutil"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
	"gorm.io/gorm"
)

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

	mod := medicore.New()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)

	migs := append(events.Migrations(), mod.Migrations()...)
	require.NoError(t, db.Migrate(ctx, migs))
	bad, err := db.LintRLS(ctx)
	require.NoError(t, err)
	require.Empty(t, bad, "medicore tables must carry forced RLS")

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

func TestCreateAndListVisits(t *testing.T) {
	r, db, ctx := setup(t)

	w := do(r, "POST", "/v1/medicore/visits", "tokA", `{"patient_name":"Asha Rao","department":"OPD"}`)
	require.Equal(t, http.StatusAccepted, w.Code)
	var resp struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.ID)

	// Tenant isolation on list.
	require.Contains(t, do(r, "GET", "/v1/medicore/visits", "tokA", "").Body.String(), "Asha Rao")
	require.NotContains(t, do(r, "GET", "/v1/medicore/visits", "tokB", "").Body.String(), "Asha Rao")

	// visit_created reached the outbox and got published (peek the
	// outbox row directly — no consumer in this module).
	require.Eventually(t, func() bool {
		var n int64
		_ = db.WithSystem(ctx, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM outbox_events
				WHERE subject = 'hms.in.medicore.visit_created.v1' AND published_at IS NOT NULL`).Scan(&n).Error
		})
		return n == 1
	}, 20*time.Second, 200*time.Millisecond)
}

func TestVisitValidation(t *testing.T) {
	r, _, _ := setup(t)
	require.Equal(t, http.StatusBadRequest, do(r, "POST", "/v1/medicore/visits", "tokA", `{}`).Code)
	require.Equal(t, http.StatusBadRequest,
		do(r, "POST", "/v1/medicore/visits", "tokA", `{"patient_name":"X","department":"ICU"}`).Code)
	require.Equal(t, http.StatusUnauthorized,
		do(r, "POST", "/v1/medicore/visits", "nope", `{"patient_name":"X","department":"OPD"}`).Code)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/modules/medicore/`
Expected: FAIL — package `medicore` does not exist.

- [ ] **Step 3: Implement the module**

`backend/internal/modules/medicore/module.go`:

```go
// Package medicore owns clinical visits (OPD/IPD). Creating a visit
// publishes visit_created, which pharmacy and lab consume to open
// pending work — the phase 2 cross-zone journey.
package medicore

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
)

const SubjectVisitCreated = "hms.in.medicore.visit_created.v1"

type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Name() string { return "medicore" }

func (m *Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{
		ID: "0001_medicore",
		SQL: `
			CREATE TABLE medicore_visits (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  patient_name text NOT NULL,
			  department text NOT NULL CHECK (department IN ('OPD','IPD')),
			  status text NOT NULL DEFAULT 'open',
			  created_at timestamptz NOT NULL DEFAULT now()
			);
			ALTER TABLE medicore_visits ENABLE ROW LEVEL SECURITY;
			ALTER TABLE medicore_visits FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON medicore_visits
			  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE INDEX ON medicore_visits (tenant_id, created_at DESC);`,
	}}
}

type visit struct {
	ID          uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	TenantID    uuid.UUID `json:"-"`
	PatientName string    `json:"patient_name"`
	Department  string    `json:"department"`
	Status      string    `gorm:"default:open" json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

func (visit) TableName() string { return "medicore_visits" }

type createVisitRequest struct {
	PatientName string `json:"patient_name" binding:"required,max=200"`
	Department  string `json:"department" binding:"required,oneof=OPD IPD"`
}

// VisitCreatedData is the v1 payload of visit_created.
type VisitCreatedData struct {
	VisitID     string `json:"visit_id"`
	PatientName string `json:"patient_name"`
	Department  string `json:"department"`
}

func (m *Module) Routes(r *gin.RouterGroup, deps platform.Deps) {
	g := r.Group("/medicore")

	g.POST("/visits", func(c *gin.Context) {
		p, _ := authn.PrincipalFrom(c)
		tenantUUID, err := uuid.Parse(p.TenantID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated", "message": "invalid tenant"})
			return
		}
		var req createVisitRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": err.Error()})
			return
		}
		row := visit{TenantID: tenantUUID, PatientName: req.PatientName, Department: req.Department}
		err = deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			data, err := json.Marshal(VisitCreatedData{
				VisitID: row.ID.String(), PatientName: row.PatientName, Department: row.Department,
			})
			if err != nil {
				return err
			}
			return deps.Bus.Publish(tx, SubjectVisitCreated, events.Event{
				Type: "VisitCreated", Version: 1, TenantID: p.TenantID, Data: data,
			})
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "message": "could not create visit"})
			return
		}
		c.JSON(http.StatusAccepted, gin.H{"id": row.ID.String()})
	})

	g.GET("/visits", func(c *gin.Context) {
		p, _ := authn.PrincipalFrom(c)
		var rows []visit
		err := deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			return tx.Order("created_at DESC").Limit(100).Find(&rows).Error
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "message": "could not list visits"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": rows})
	})
}

func (m *Module) Consumers(deps platform.Deps) []events.Consumer { return nil }
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/modules/medicore/ -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/modules/medicore/
git commit -m "feat: medicore module with visits and visit_created event"
```

---

### Task 3: pharmacy backend module — medications, dispenses, visit intake

**Files:**
- Create: `backend/internal/modules/pharmacy/module.go`
- Test: `backend/internal/modules/pharmacy/module_test.go`

**Interfaces:**
- Consumes: same platform/authn/events surfaces as Task 2. Consumes the event subject string `"hms.in.medicore.visit_created.v1"` with data `{"visit_id","patient_name","department"}` (repeated literal — no import of medicore).
- Produces:
  - `pharmacy.New() *Module`, `Name() == "pharmacy"`.
  - Routes: `POST /pharmacy/medications` `{"name": required max 200, "strength": max 100}` → `201 {"id"}`; `GET /pharmacy/medications` → `{"data":[...]}`; `GET /pharmacy/dispenses` → `{"data":[{id, visit_id, patient_name, medication, status, dispensed_at, created_at}]}`; `POST /pharmacy/dispenses/:id/dispense` `{"medication": required max 200}` → `200` on success, `404` unknown/cross-tenant id, `409 {"error":"conflict"}` if already dispensed.
  - Consumer `pharmacy-visit-intake` creating a pending dispense per visit (relies on Task 1's tenant GUC).
  - Event `hms.in.pharmacy.dispense_recorded.v1`, type `DispenseRecorded` v1, data `{"dispense_id","visit_id"}`.

- [ ] **Step 1: Write the failing test**

`backend/internal/modules/pharmacy/module_test.go` — same `staticVerifier`/`setup`/`do` scaffolding as Task 2 Step 1 with these differences: import `"github.com/tesserix/hms/internal/modules/pharmacy"` instead of medicore and build `mod := pharmacy.New()`. Then:

```go
func TestVisitIntakeCreatesPendingDispense(t *testing.T) {
	r, db, ctx := setup(t)

	// Simulate medicore publishing visit_created (module boundary: we
	// publish the envelope, not import medicore).
	visitID := uuid.NewString()
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		data, _ := json.Marshal(map[string]string{
			"visit_id": visitID, "patient_name": "Asha Rao", "department": "OPD",
		})
		return busRef.Publish(tx, "hms.in.medicore.visit_created.v1", events.Event{
			Type: "VisitCreated", Version: 1, TenantID: tenantA, Data: data,
		})
	}))

	// Pending dispense appears for tenant A…
	require.Eventually(t, func() bool {
		return strings.Contains(do(r, "GET", "/v1/pharmacy/dispenses", "tokA", "").Body.String(), visitID)
	}, 20*time.Second, 200*time.Millisecond)
	require.Contains(t, do(r, "GET", "/v1/pharmacy/dispenses", "tokA", "").Body.String(), `"pending"`)
	// …and not for tenant B.
	require.NotContains(t, do(r, "GET", "/v1/pharmacy/dispenses", "tokB", "").Body.String(), visitID)
}

func TestDispenseFlow(t *testing.T) {
	r, db, ctx := setup(t)

	visitID := uuid.NewString()
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		data, _ := json.Marshal(map[string]string{
			"visit_id": visitID, "patient_name": "Asha Rao", "department": "OPD",
		})
		return busRef.Publish(tx, "hms.in.medicore.visit_created.v1", events.Event{
			Type: "VisitCreated", Version: 1, TenantID: tenantA, Data: data,
		})
	}))

	var dispenseID string
	require.Eventually(t, func() bool {
		var resp struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		_ = json.Unmarshal(do(r, "GET", "/v1/pharmacy/dispenses", "tokA", "").Body.Bytes(), &resp)
		if len(resp.Data) == 0 {
			return false
		}
		dispenseID = resp.Data[0].ID
		return true
	}, 20*time.Second, 200*time.Millisecond)

	// Cross-tenant dispense probe → 404.
	require.Equal(t, http.StatusNotFound,
		do(r, "POST", "/v1/pharmacy/dispenses/"+dispenseID+"/dispense", "tokB", `{"medication":"Paracetamol 500mg"}`).Code)

	// Dispense succeeds once…
	require.Equal(t, http.StatusOK,
		do(r, "POST", "/v1/pharmacy/dispenses/"+dispenseID+"/dispense", "tokA", `{"medication":"Paracetamol 500mg"}`).Code)
	require.Contains(t, do(r, "GET", "/v1/pharmacy/dispenses", "tokA", "").Body.String(), `"dispensed"`)

	// …and 409s on repeat.
	require.Equal(t, http.StatusConflict,
		do(r, "POST", "/v1/pharmacy/dispenses/"+dispenseID+"/dispense", "tokA", `{"medication":"Paracetamol 500mg"}`).Code)

	// dispense_recorded hit the outbox.
	require.Eventually(t, func() bool {
		var n int64
		_ = db.WithSystem(ctx, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM outbox_events
				WHERE subject = 'hms.in.pharmacy.dispense_recorded.v1'`).Scan(&n).Error
		})
		return n == 1
	}, 10*time.Second, 200*time.Millisecond)
}

func TestMedicationsCrud(t *testing.T) {
	r, _, _ := setup(t)
	require.Equal(t, http.StatusCreated,
		do(r, "POST", "/v1/pharmacy/medications", "tokA", `{"name":"Paracetamol","strength":"500mg"}`).Code)
	require.Equal(t, http.StatusBadRequest,
		do(r, "POST", "/v1/pharmacy/medications", "tokA", `{}`).Code)
	require.Contains(t, do(r, "GET", "/v1/pharmacy/medications", "tokA", "").Body.String(), "Paracetamol")
	require.NotContains(t, do(r, "GET", "/v1/pharmacy/medications", "tokB", "").Body.String(), "Paracetamol")
}
```

For the publish helper the test needs the bus: in `setup`, assign the bus to a package-level `var busRef *events.Bus` after `events.NewBus(...)` (`busRef = bus`) so tests can publish envelopes. Keep `setup` returning `(*gin.Engine, *tenantdb.DB, context.Context)` as in Task 2.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/modules/pharmacy/`
Expected: FAIL — package `pharmacy` does not exist.

- [ ] **Step 3: Implement the module**

`backend/internal/modules/pharmacy/module.go`:

```go
// Package pharmacy owns medications and dispense tasks. It consumes
// medicore's visit_created to open a pending dispense per visit and
// publishes dispense_recorded when the pharmacist dispenses.
package pharmacy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
)

const (
	// SubjectVisitCreated is medicore's subject, repeated by value —
	// modules must not import each other (spec D6 / phase 1).
	subjectVisitCreated      = "hms.in.medicore.visit_created.v1"
	SubjectDispenseRecorded  = "hms.in.pharmacy.dispense_recorded.v1"
)

type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Name() string { return "pharmacy" }

func (m *Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{
		ID: "0001_pharmacy",
		SQL: `
			CREATE TABLE pharmacy_medications (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  name text NOT NULL,
			  strength text NOT NULL DEFAULT '',
			  created_at timestamptz NOT NULL DEFAULT now()
			);
			ALTER TABLE pharmacy_medications ENABLE ROW LEVEL SECURITY;
			ALTER TABLE pharmacy_medications FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON pharmacy_medications
			  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE INDEX ON pharmacy_medications (tenant_id, created_at DESC);

			CREATE TABLE pharmacy_dispenses (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  visit_id uuid NOT NULL,
			  patient_name text NOT NULL,
			  medication text NOT NULL DEFAULT '',
			  status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','dispensed')),
			  dispensed_at timestamptz,
			  created_at timestamptz NOT NULL DEFAULT now()
			);
			ALTER TABLE pharmacy_dispenses ENABLE ROW LEVEL SECURITY;
			ALTER TABLE pharmacy_dispenses FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON pharmacy_dispenses
			  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE INDEX ON pharmacy_dispenses (tenant_id, created_at DESC);`,
	}}
}

type medication struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	TenantID  uuid.UUID `json:"-"`
	Name      string    `json:"name"`
	Strength  string    `json:"strength"`
	CreatedAt time.Time `json:"created_at"`
}

func (medication) TableName() string { return "pharmacy_medications" }

type dispense struct {
	ID          uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	TenantID    uuid.UUID  `json:"-"`
	VisitID     uuid.UUID  `json:"visit_id"`
	PatientName string     `json:"patient_name"`
	Medication  string     `json:"medication"`
	Status      string     `gorm:"default:pending" json:"status"`
	DispensedAt *time.Time `json:"dispensed_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (dispense) TableName() string { return "pharmacy_dispenses" }

type createMedicationRequest struct {
	Name     string `json:"name" binding:"required,max=200"`
	Strength string `json:"strength" binding:"max=100"`
}

type dispenseRequest struct {
	Medication string `json:"medication" binding:"required,max=200"`
}

type visitCreatedData struct {
	VisitID     string `json:"visit_id"`
	PatientName string `json:"patient_name"`
}

type dispenseRecordedData struct {
	DispenseID string `json:"dispense_id"`
	VisitID    string `json:"visit_id"`
}

func (m *Module) Routes(r *gin.RouterGroup, deps platform.Deps) {
	g := r.Group("/pharmacy")

	g.POST("/medications", func(c *gin.Context) {
		p, _ := authn.PrincipalFrom(c)
		tenantUUID, err := uuid.Parse(p.TenantID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated", "message": "invalid tenant"})
			return
		}
		var req createMedicationRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": err.Error()})
			return
		}
		row := medication{TenantID: tenantUUID, Name: req.Name, Strength: req.Strength}
		err = deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			return tx.Create(&row).Error
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "message": "could not create medication"})
			return
		}
		c.JSON(http.StatusCreated, gin.H{"id": row.ID.String()})
	})

	g.GET("/medications", func(c *gin.Context) {
		p, _ := authn.PrincipalFrom(c)
		var rows []medication
		err := deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			return tx.Order("created_at DESC").Limit(100).Find(&rows).Error
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "message": "could not list medications"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": rows})
	})

	g.GET("/dispenses", func(c *gin.Context) {
		p, _ := authn.PrincipalFrom(c)
		var rows []dispense
		err := deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			return tx.Order("created_at DESC").Limit(100).Find(&rows).Error
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "message": "could not list dispenses"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": rows})
	})

	g.POST("/dispenses/:id/dispense", func(c *gin.Context) {
		p, _ := authn.PrincipalFrom(c)
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "dispense not found"})
			return
		}
		var req dispenseRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": err.Error()})
			return
		}
		var status int
		err = deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			var row dispense
			if err := tx.First(&row, "id = ?", id).Error; err != nil {
				return err
			}
			if row.Status != "pending" {
				status = http.StatusConflict
				return nil
			}
			now := time.Now().UTC()
			if err := tx.Model(&dispense{}).Where("id = ?", id).
				Updates(map[string]any{"status": "dispensed", "medication": req.Medication, "dispensed_at": now}).Error; err != nil {
				return err
			}
			data, err := json.Marshal(dispenseRecordedData{DispenseID: row.ID.String(), VisitID: row.VisitID.String()})
			if err != nil {
				return err
			}
			return deps.Bus.Publish(tx, SubjectDispenseRecorded, events.Event{
				Type: "DispenseRecorded", Version: 1, TenantID: p.TenantID, Data: data,
			})
		})
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "dispense not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "message": "could not dispense"})
			return
		}
		if status == http.StatusConflict {
			c.JSON(http.StatusConflict, gin.H{"error": "conflict", "message": "already dispensed"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": id.String(), "status": "dispensed"})
	})
}

func (m *Module) Consumers(deps platform.Deps) []events.Consumer {
	return []events.Consumer{{
		Name:    "pharmacy-visit-intake",
		Subject: subjectVisitCreated,
		Handle: func(ctx context.Context, tx *gorm.DB, evt events.Event) error {
			var d visitCreatedData
			if err := json.Unmarshal(evt.Data, &d); err != nil {
				return err
			}
			// The bus scoped this tx to evt.TenantID (Task 1), so this
			// RLS-forced insert lands under the visit's tenant.
			return tx.Exec(`INSERT INTO pharmacy_dispenses (tenant_id, visit_id, patient_name)
				VALUES (?, ?, ?)`, evt.TenantID, d.VisitID, d.PatientName).Error
		},
	}}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/modules/pharmacy/ -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/modules/pharmacy/
git commit -m "feat: pharmacy module with medications, dispenses and visit intake"
```

---

### Task 4: lab backend module — orders, results, visit intake

**Files:**
- Create: `backend/internal/modules/lab/module.go`
- Test: `backend/internal/modules/lab/module_test.go`

**Interfaces:**
- Consumes: same surfaces as Task 3; event subject literal `"hms.in.medicore.visit_created.v1"`.
- Produces:
  - `lab.New() *Module`, `Name() == "lab"`.
  - Routes: `GET /lab/orders` → `{"data":[{id, visit_id, patient_name, test_name, status, result_value, resulted_at, created_at}]}`; `POST /lab/orders/:id/result` `{"result_value": required max 500}` → `200`, `404` unknown/cross-tenant, `409` already completed.
  - Consumer `lab-visit-intake` creating a pending order (test_name default `CBC`) per visit.
  - Event `hms.in.lab.result_ready.v1`, type `ResultReady` v1, data `{"order_id","visit_id"}`.

- [ ] **Step 1: Write the failing test**

`backend/internal/modules/lab/module_test.go` — same `staticVerifier`/`setup`/`do`/`busRef` scaffolding as Task 3 Step 1, importing `"github.com/tesserix/hms/internal/modules/lab"` and building `mod := lab.New()`. Tests:

```go
func TestVisitIntakeCreatesPendingOrder(t *testing.T) {
	r, db, ctx := setup(t)

	visitID := uuid.NewString()
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		data, _ := json.Marshal(map[string]string{
			"visit_id": visitID, "patient_name": "Asha Rao", "department": "OPD",
		})
		return busRef.Publish(tx, "hms.in.medicore.visit_created.v1", events.Event{
			Type: "VisitCreated", Version: 1, TenantID: tenantA, Data: data,
		})
	}))

	require.Eventually(t, func() bool {
		return strings.Contains(do(r, "GET", "/v1/lab/orders", "tokA", "").Body.String(), visitID)
	}, 20*time.Second, 200*time.Millisecond)
	body := do(r, "GET", "/v1/lab/orders", "tokA", "").Body.String()
	require.Contains(t, body, `"pending"`)
	require.Contains(t, body, `"CBC"`)
	require.NotContains(t, do(r, "GET", "/v1/lab/orders", "tokB", "").Body.String(), visitID)
}

func TestResultFlow(t *testing.T) {
	r, db, ctx := setup(t)

	visitID := uuid.NewString()
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		data, _ := json.Marshal(map[string]string{
			"visit_id": visitID, "patient_name": "Asha Rao", "department": "OPD",
		})
		return busRef.Publish(tx, "hms.in.medicore.visit_created.v1", events.Event{
			Type: "VisitCreated", Version: 1, TenantID: tenantA, Data: data,
		})
	}))

	var orderID string
	require.Eventually(t, func() bool {
		var resp struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		_ = json.Unmarshal(do(r, "GET", "/v1/lab/orders", "tokA", "").Body.Bytes(), &resp)
		if len(resp.Data) == 0 {
			return false
		}
		orderID = resp.Data[0].ID
		return true
	}, 20*time.Second, 200*time.Millisecond)

	require.Equal(t, http.StatusNotFound,
		do(r, "POST", "/v1/lab/orders/"+orderID+"/result", "tokB", `{"result_value":"WBC 6.1"}`).Code)
	require.Equal(t, http.StatusBadRequest,
		do(r, "POST", "/v1/lab/orders/"+orderID+"/result", "tokA", `{}`).Code)

	require.Equal(t, http.StatusOK,
		do(r, "POST", "/v1/lab/orders/"+orderID+"/result", "tokA", `{"result_value":"WBC 6.1"}`).Code)
	body := do(r, "GET", "/v1/lab/orders", "tokA", "").Body.String()
	require.Contains(t, body, `"completed"`)
	require.Contains(t, body, "WBC 6.1")

	require.Equal(t, http.StatusConflict,
		do(r, "POST", "/v1/lab/orders/"+orderID+"/result", "tokA", `{"result_value":"again"}`).Code)

	require.Eventually(t, func() bool {
		var n int64
		_ = db.WithSystem(ctx, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM outbox_events
				WHERE subject = 'hms.in.lab.result_ready.v1'`).Scan(&n).Error
		})
		return n == 1
	}, 10*time.Second, 200*time.Millisecond)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/modules/lab/`
Expected: FAIL — package `lab` does not exist.

- [ ] **Step 3: Implement the module**

`backend/internal/modules/lab/module.go`:

```go
// Package lab owns test orders and results. It consumes medicore's
// visit_created to open a pending order and publishes result_ready
// when a result is recorded.
package lab

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
)

const (
	subjectVisitCreated = "hms.in.medicore.visit_created.v1"
	SubjectResultReady  = "hms.in.lab.result_ready.v1"
)

type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Name() string { return "lab" }

func (m *Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{
		ID: "0001_lab",
		SQL: `
			CREATE TABLE lab_orders (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  visit_id uuid NOT NULL,
			  patient_name text NOT NULL,
			  test_name text NOT NULL DEFAULT 'CBC',
			  status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','completed')),
			  result_value text,
			  resulted_at timestamptz,
			  created_at timestamptz NOT NULL DEFAULT now()
			);
			ALTER TABLE lab_orders ENABLE ROW LEVEL SECURITY;
			ALTER TABLE lab_orders FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON lab_orders
			  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE INDEX ON lab_orders (tenant_id, created_at DESC);`,
	}}
}

type order struct {
	ID          uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	TenantID    uuid.UUID  `json:"-"`
	VisitID     uuid.UUID  `json:"visit_id"`
	PatientName string     `json:"patient_name"`
	TestName    string     `gorm:"default:CBC" json:"test_name"`
	Status      string     `gorm:"default:pending" json:"status"`
	ResultValue *string    `json:"result_value"`
	ResultedAt  *time.Time `json:"resulted_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (order) TableName() string { return "lab_orders" }

type resultRequest struct {
	ResultValue string `json:"result_value" binding:"required,max=500"`
}

type visitCreatedData struct {
	VisitID     string `json:"visit_id"`
	PatientName string `json:"patient_name"`
}

type resultReadyData struct {
	OrderID string `json:"order_id"`
	VisitID string `json:"visit_id"`
}

func (m *Module) Routes(r *gin.RouterGroup, deps platform.Deps) {
	g := r.Group("/lab")

	g.GET("/orders", func(c *gin.Context) {
		p, _ := authn.PrincipalFrom(c)
		var rows []order
		err := deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			return tx.Order("created_at DESC").Limit(100).Find(&rows).Error
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "message": "could not list orders"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"data": rows})
	})

	g.POST("/orders/:id/result", func(c *gin.Context) {
		p, _ := authn.PrincipalFrom(c)
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "order not found"})
			return
		}
		var req resultRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_request", "message": err.Error()})
			return
		}
		var status int
		err = deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			var row order
			if err := tx.First(&row, "id = ?", id).Error; err != nil {
				return err
			}
			if row.Status != "pending" {
				status = http.StatusConflict
				return nil
			}
			now := time.Now().UTC()
			if err := tx.Model(&order{}).Where("id = ?", id).
				Updates(map[string]any{"status": "completed", "result_value": req.ResultValue, "resulted_at": now}).Error; err != nil {
				return err
			}
			data, err := json.Marshal(resultReadyData{OrderID: row.ID.String(), VisitID: row.VisitID.String()})
			if err != nil {
				return err
			}
			return deps.Bus.Publish(tx, SubjectResultReady, events.Event{
				Type: "ResultReady", Version: 1, TenantID: p.TenantID, Data: data,
			})
		})
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not_found", "message": "order not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal", "message": "could not record result"})
			return
		}
		if status == http.StatusConflict {
			c.JSON(http.StatusConflict, gin.H{"error": "conflict", "message": "result already recorded"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": id.String(), "status": "completed"})
	})
}

func (m *Module) Consumers(deps platform.Deps) []events.Consumer {
	return []events.Consumer{{
		Name:    "lab-visit-intake",
		Subject: subjectVisitCreated,
		Handle: func(ctx context.Context, tx *gorm.DB, evt events.Event) error {
			var d visitCreatedData
			if err := json.Unmarshal(evt.Data, &d); err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO lab_orders (tenant_id, visit_id, patient_name)
				VALUES (?, ?, ?)`, evt.TenantID, d.VisitID, d.PatientName).Error
		},
	}}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/modules/lab/ -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add backend/internal/modules/lab/
git commit -m "feat: lab module with orders, results and visit intake"
```

---

### Task 5: Register modules + cross-module journey integration test

**Files:**
- Modify: `backend/cmd/api/main.go` (registration block)
- Create: `backend/internal/journey/journey_test.go` (test-only package)

**Interfaces:**
- Consumes: `medicore.New()`, `pharmacy.New()`, `lab.New()` (Tasks 2–4), the registry/migration/consumer wiring already in `run()`.
- Produces: the running API serves all four modules; the journey test proves visit → pending dispense AND pending lab order for the right tenant only.

- [ ] **Step 1: Register the modules in main**

In `backend/cmd/api/main.go`, extend the imports with:

```go
	"github.com/tesserix/hms/internal/modules/lab"
	"github.com/tesserix/hms/internal/modules/medicore"
	"github.com/tesserix/hms/internal/modules/pharmacy"
```

and replace the single registration block

```go
	registry := platform.NewRegistry()
	if err := registry.Register(reference.New()); err != nil {
		return err
	}
```

with:

```go
	registry := platform.NewRegistry()
	for _, mod := range []platform.Module{reference.New(), medicore.New(), pharmacy.New(), lab.New()} {
		if err := registry.Register(mod); err != nil {
			return err
		}
	}
```

Run: `cd backend && go vet ./... && go build ./...`
Expected: clean.

- [ ] **Step 2: Write the journey integration test**

`backend/internal/journey/journey_test.go` (directory contains only this file; internal test package `journey` is valid):

```go
// Package journey holds the phase 2 cross-module integration test:
// medicore visit_created fans out to pharmacy AND lab via JetStream.
package journey

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

	"github.com/tesserix/hms/internal/modules/lab"
	"github.com/tesserix/hms/internal/modules/medicore"
	"github.com/tesserix/hms/internal/modules/pharmacy"
	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/internal/testutil"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
)

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

func do(r *gin.Engine, method, path, token, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

func TestVisitFansOutToPharmacyAndLab(t *testing.T) {
	appDSN, adminDSN := testutil.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, adminDSN)
	require.NoError(t, err)

	mods := []platform.Module{medicore.New(), pharmacy.New(), lab.New()}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)

	migs := events.Migrations()
	for _, m := range mods {
		migs = append(migs, m.Migrations()...)
	}
	require.NoError(t, db.Migrate(ctx, migs))

	bus, err := events.NewBus(testutil.StartNATS(t))
	require.NoError(t, err)
	t.Cleanup(bus.Close)

	deps := platform.Deps{DB: db, Bus: bus}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/v1", authn.Middleware(staticVerifier{"tokA": tenantA, "tokB": tenantB}))
	for _, m := range mods {
		m.Routes(api, deps)
		require.NoError(t, bus.StartConsumers(ctx, db, m.Consumers(deps)))
	}
	go bus.RunDispatcher(ctx, db)

	// Create a visit as tenant A.
	w := do(r, "POST", "/v1/medicore/visits", "tokA", `{"patient_name":"Asha Rao","department":"OPD"}`)
	require.Equal(t, http.StatusAccepted, w.Code)
	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))

	// Both downstream modules open pending work for tenant A.
	require.Eventually(t, func() bool {
		return strings.Contains(do(r, "GET", "/v1/pharmacy/dispenses", "tokA", "").Body.String(), created.ID)
	}, 30*time.Second, 200*time.Millisecond, "pharmacy should open a pending dispense")
	require.Eventually(t, func() bool {
		return strings.Contains(do(r, "GET", "/v1/lab/orders", "tokA", "").Body.String(), created.ID)
	}, 30*time.Second, 200*time.Millisecond, "lab should open a pending order")

	// Tenant B sees none of it.
	require.NotContains(t, do(r, "GET", "/v1/pharmacy/dispenses", "tokB", "").Body.String(), created.ID)
	require.NotContains(t, do(r, "GET", "/v1/lab/orders", "tokB", "").Body.String(), created.ID)
}
```

- [ ] **Step 3: Run the test**

Run: `cd backend && go test ./internal/journey/ -v`
Expected: PASS.

- [ ] **Step 4: Run the full backend suite**

Run: `cd backend && go test -race ./...`
Expected: all packages PASS.

- [ ] **Step 5: Commit**

```bash
git add backend/
git commit -m "feat: register phase 2 modules and add cross-module journey test"
```

---

### Task 6: `packages/ui` — @hms/ui with two-rail sidebar, zone registry, tokens

**Files:**
- Create: `packages/ui/package.json`, `packages/ui/tsconfig.json`, `packages/ui/src/index.ts`, `packages/ui/src/zones.ts`, `packages/ui/src/hms-shell.tsx`, `packages/ui/styles.css`

**Interfaces:**
- Produces (consumed by Tasks 7–9):
  - `import { HmsShell } from "@hms/ui"` — props `{ active: string; children: React.ReactNode }` where `active` is the current absolute path (e.g. `/medicore/opd`). Server-component-safe (no hooks). Renders icon rail + secondary panel + header + `<main>`.
  - `import { ZONES, activeZone } from "@hms/ui"` — `Zone = { key, label, icon, href, pages: {label, href}[] }`; `activeZone(path: string): Zone`.
  - CSS: apps add `@import "@hms/ui/styles.css";` after `@tesserix/web/styles` and `@source "../../../packages/ui/src";` so Tailwind emits the package's classes.
  - Apps must add `transpilePackages: ["@hms/ui"]` to `next.config.ts` (source package, no build step).

- [ ] **Step 1: Package scaffolding**

`packages/ui/package.json`:

```json
{
  "name": "@hms/ui",
  "version": "0.0.0",
  "private": true,
  "exports": {
    ".": "./src/index.ts",
    "./styles.css": "./styles.css"
  },
  "scripts": {
    "type-check": "tsc --noEmit"
  },
  "dependencies": {
    "lucide-react": "^0.469.0"
  },
  "peerDependencies": {
    "react": "^19.0.0"
  },
  "devDependencies": {
    "@hms/config": "workspace:*",
    "@types/react": "^19",
    "typescript": "^5.7.0"
  }
}
```

`packages/ui/tsconfig.json`:

```json
{
  "extends": "@hms/config/tsconfig.base.json",
  "compilerOptions": {
    "jsx": "preserve",
    "noEmit": true
  },
  "include": ["src"]
}
```

(If `packages/config/tsconfig.base.json` lacks `"jsx"` or DOM libs, mirror the compilerOptions from `apps/medicore/tsconfig.json` instead of extending — copy its `compilerOptions` block verbatim and keep `"noEmit": true`.)

- [ ] **Step 2: Zone registry**

`packages/ui/src/zones.ts`:

```ts
import { FlaskConical, HeartPulse, LayoutDashboard, Pill, type LucideIcon } from "lucide-react";

export type ZonePage = { label: string; href: string };

export type Zone = {
  key: string;
  label: string;
  icon: LucideIcon;
  href: string;
  pages: ZonePage[];
};

// Single source of zone navigation. Adding a zone is one entry here.
// All hrefs are absolute paths; every link renders as a plain <a> so
// cross-zone navigation is a hard navigation (phase 1 spec D3).
export const ZONES: Zone[] = [
  {
    key: "dashboard",
    label: "Dashboard",
    icon: LayoutDashboard,
    href: "/",
    pages: [{ label: "Departments", href: "/" }],
  },
  {
    key: "medicore",
    label: "MediCore",
    icon: HeartPulse,
    href: "/medicore/opd",
    pages: [
      { label: "OPD", href: "/medicore/opd" },
      { label: "IPD", href: "/medicore/ipd" },
    ],
  },
  {
    key: "pharmacy",
    label: "Pharmacy",
    icon: Pill,
    href: "/pharmacy",
    pages: [
      { label: "Dispenses", href: "/pharmacy" },
      { label: "Medications", href: "/pharmacy/medications" },
    ],
  },
  {
    key: "lab",
    label: "Lab",
    icon: FlaskConical,
    href: "/lab",
    pages: [{ label: "Orders", href: "/lab" }],
  },
];

export function activeZone(path: string): Zone {
  if (path.startsWith("/medicore")) return ZONES[1];
  if (path.startsWith("/pharmacy")) return ZONES[2];
  if (path.startsWith("/lab")) return ZONES[3];
  return ZONES[0];
}
```

- [ ] **Step 3: Two-rail shell component**

`packages/ui/src/hms-shell.tsx`:

```tsx
import type { ReactNode } from "react";
import { LogOut } from "lucide-react";
import { ZONES, activeZone } from "./zones";

// Two-rail chrome ported from tesserix-home's AdminSidebar: a 4rem icon
// rail switching zones + a 14rem panel listing the active zone's pages.
// No hooks — active state comes in as a prop, so this stays a server
// component and works identically in every zone app.
export function HmsShell({ active, children }: { active: string; children: ReactNode }) {
  const zone = activeZone(active);

  return (
    <div className="flex min-h-screen">
      {/* Left rail: zone switcher */}
      <aside className="flex w-16 shrink-0 flex-col items-center border-r border-sidebar-border bg-sidebar">
        <div className="flex h-16 items-center justify-center">
          <a
            href="/"
            aria-label="HMS home"
            className="flex h-9 w-9 items-center justify-center rounded-lg text-lg font-semibold text-sidebar-primary"
          >
            H
          </a>
        </div>
        <nav aria-label="Zones" className="flex flex-1 flex-col items-center gap-2 py-4">
          {ZONES.map((z) => {
            const isActive = z.key === zone.key;
            return (
              <a
                key={z.key}
                href={z.href}
                title={z.label}
                aria-label={z.label}
                aria-current={isActive ? "page" : undefined}
                className={`flex h-10 w-10 items-center justify-center rounded-lg transition-colors ${
                  isActive
                    ? "bg-sidebar-accent text-sidebar-accent-foreground"
                    : "text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
                }`}
              >
                <z.icon className="h-5 w-5" aria-hidden="true" />
              </a>
            );
          })}
        </nav>
        <div className="flex flex-col items-center pb-4">
          <a
            href="/logout"
            title="Sign out"
            aria-label="Sign out"
            className="flex h-10 w-10 items-center justify-center rounded-lg text-sidebar-foreground/70 transition-colors hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
          >
            <LogOut className="h-4 w-4" aria-hidden="true" />
          </a>
        </div>
      </aside>

      {/* Secondary panel: active zone's pages */}
      <aside className="flex w-56 shrink-0 flex-col border-r border-sidebar-border bg-sidebar">
        <div className="flex h-16 items-center px-5">
          <h2 className="text-sm font-semibold text-sidebar-foreground">{zone.label}</h2>
        </div>
        <div className="border-t border-sidebar-border" />
        <nav aria-label={zone.label} className="flex flex-col gap-1 px-3 py-4">
          {zone.pages.map((pageLink) => {
            const isActive = active === pageLink.href;
            return (
              <a
                key={pageLink.href}
                href={pageLink.href}
                aria-current={isActive ? "page" : undefined}
                className={`rounded-lg px-3 py-2 text-sm font-medium transition-colors ${
                  isActive
                    ? "bg-sidebar-accent text-sidebar-foreground"
                    : "text-sidebar-foreground/70 hover:bg-sidebar-accent/50 hover:text-sidebar-foreground"
                }`}
              >
                {pageLink.label}
              </a>
            );
          })}
        </nav>
      </aside>

      {/* Content */}
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 items-center justify-between border-b px-6">
          <span className="text-sm text-muted-foreground">Hospital Management System</span>
          <a href="/logout" className="text-sm underline-offset-4 hover:underline">
            Sign out
          </a>
        </header>
        <main className="flex-1 p-6">{children}</main>
      </div>
    </div>
  );
}
```

`packages/ui/src/index.ts`:

```ts
export { HmsShell } from "./hms-shell";
export { ZONES, activeZone, type Zone, type ZonePage } from "./zones";
```

- [ ] **Step 4: Sidebar tokens**

`packages/ui/styles.css` (values copied from tesserix-home `apps/web/app/globals.css` light theme):

```css
/* HMS sidebar tokens — tesserix-home dark-slate rail on light content.
   Layer AFTER @tesserix/web/styles so these win. */
:root,
[data-theme="default"] {
  --sidebar: #0f172a;
  --sidebar-foreground: #e2e8f0;
  --sidebar-primary: #ffffff;
  --sidebar-primary-foreground: #0f172a;
  --sidebar-accent: #1e293b;
  --sidebar-accent-foreground: #f1f5f9;
  --sidebar-border: #1e293b;
  --sidebar-ring: #ffffff;
}
```

- [ ] **Step 5: Install and type-check**

Run: `pnpm install && pnpm --filter @hms/ui type-check`
Expected: lockfile updated with `@hms/ui` + lucide-react; type-check passes. If the extended tsconfig lacks JSX/DOM settings, apply the fallback noted in Step 1.

- [ ] **Step 6: Commit**

```bash
git add packages/ui pnpm-lock.yaml
git commit -m "feat: hms ui package with two-rail sidebar and zone registry"
```

---

### Task 7: Migrate shell + medicore apps to @hms/ui

**Files:**
- Delete: `apps/shell/components/hms-shell.tsx`, `apps/medicore/components/hms-shell.tsx`
- Modify: `apps/shell/package.json`, `apps/medicore/package.json` (add `"@hms/ui": "workspace:*"` to dependencies), `apps/shell/next.config.ts`, `apps/medicore/next.config.ts` (add `transpilePackages: ["@hms/ui"]`), `apps/shell/app/globals.css`, `apps/medicore/app/globals.css`, `apps/shell/app/page.tsx`, `apps/medicore/app/opd/page.tsx`, `apps/medicore/app/ipd/page.tsx`

**Interfaces:**
- Consumes: `HmsShell` from `@hms/ui` (Task 6).
- Produces: both apps render the shared two-rail chrome; no local `hms-shell.tsx` copies remain in the repo.

- [ ] **Step 1: Add the dependency and transpile config**

In both `apps/shell/package.json` and `apps/medicore/package.json`, add to `dependencies`:

```json
    "@hms/ui": "workspace:*",
```

In both `next.config.ts` files, add inside the `nextConfig` object (top level, alongside `output`):

```ts
  transpilePackages: ["@hms/ui"],
```

Run: `pnpm install`

- [ ] **Step 2: Wire the CSS**

In BOTH `apps/shell/app/globals.css` and `apps/medicore/app/globals.css`, append after the existing `@source` line:

```css
@import "@hms/ui/styles.css";

/* Scan the shared UI package for emitted class names. */
@source "../../../packages/ui/src";
```

(Tailwind v4 hoists `@import`; keeping it adjacent to the other imports is fine — the `:root` token block just needs to appear after `@tesserix/web/styles` in the final cascade, which import order guarantees.)

- [ ] **Step 3: Swap imports and delete the copies**

- `apps/shell/app/page.tsx`: change `import { HmsShell } from "@/components/hms-shell";` to `import { HmsShell } from "@hms/ui";` (rest unchanged).
- `apps/medicore/app/opd/page.tsx` and `apps/medicore/app/ipd/page.tsx`: same import swap.
- Delete `apps/shell/components/hms-shell.tsx` and `apps/medicore/components/hms-shell.tsx`.
- Grep guard: `grep -rn "components/hms-shell" apps/ --include="*.tsx" --include="*.ts"` must return nothing.

- [ ] **Step 4: Verify build**

Run: `pnpm turbo type-check build --filter=@hms/shell --filter=@hms/medicore`
(If the workspace package names differ — check the `"name"` field in each app's package.json, e.g. `@hms/medicore` — use those names.)
Expected: both apps type-check and build clean.

- [ ] **Step 5: Commit**

```bash
git add apps/shell apps/medicore pnpm-lock.yaml
git commit -m "refactor: shell and medicore consume shared two-rail chrome from @hms/ui"
```

---

### Task 8: apps/pharmacy zone app + shell rewrite

**Files:**
- Create: `apps/pharmacy/package.json`, `apps/pharmacy/next.config.ts`, `apps/pharmacy/tsconfig.json`, `apps/pharmacy/postcss.config.mjs`, `apps/pharmacy/next-env.d.ts` (generated), `apps/pharmacy/app/layout.tsx`, `apps/pharmacy/app/globals.css`, `apps/pharmacy/app/page.tsx`, `apps/pharmacy/app/medications/page.tsx`, `apps/pharmacy/components/dispense-list.tsx`, `apps/pharmacy/components/medications-panel.tsx`
- Modify: `apps/shell/next.config.ts` (pharmacy rewrite)

**Interfaces:**
- Consumes: `HmsShell` from `@hms/ui`; API routes `GET /api/v1/pharmacy/dispenses`, `POST /api/v1/pharmacy/dispenses/:id/dispense`, `GET/POST /api/v1/pharmacy/medications` (same-origin fetches; session cookie flows automatically).
- Produces: pharmacy zone at `/pharmacy` (port 4303) with Dispenses and Medications pages; shell stitches `/pharmacy/*`.

- [ ] **Step 1: Clone the app scaffolding from medicore**

Copy these files from `apps/medicore/` to `apps/pharmacy/`, then apply the listed edits: `tsconfig.json` (unchanged), `postcss.config.mjs` (unchanged), `app/layout.tsx` (unchanged), `app/globals.css` (use the Task 7 Step 2 version with the `@hms/ui` import — note the app is one level deep so the source path stays `../../../packages/ui/src`).

`apps/pharmacy/package.json`:

```json
{
  "name": "@hms/pharmacy",
  "version": "0.0.0",
  "private": true,
  "scripts": {
    "dev": "next dev -p 4303",
    "build": "next build",
    "start": "next start -p 4303",
    "type-check": "tsc --noEmit"
  },
  "dependencies": {
    "@hms/ui": "workspace:*",
    "@tesserix/web": "^1.8.0",
    "next": "^16.0.0",
    "react": "^19.0.0",
    "react-dom": "^19.0.0"
  },
  "devDependencies": {
    "@hms/config": "workspace:*",
    "@tailwindcss/postcss": "^4.1.0",
    "@types/node": "^22",
    "@types/react": "^19",
    "tailwindcss": "^4.1.0",
    "typescript": "^5.7.0"
  }
}
```

`apps/pharmacy/next.config.ts`:

```ts
import type { NextConfig } from "next";

const API_URL = process.env.API_URL ?? "http://localhost:8080";

const nextConfig: NextConfig = {
  output: "standalone",
  basePath: "/pharmacy",
  transpilePackages: ["@hms/ui"],
  async rewrites() {
    // Only used when hitting :4303 directly; via the shell the same
    // /api/* path is rewritten by the shell itself.
    return [
      {
        source: "/api/:path*",
        destination: `${API_URL}/:path*`,
        basePath: false,
      },
    ];
  },
};

export default nextConfig;
```

- [ ] **Step 2: Pages and components**

`apps/pharmacy/app/page.tsx`:

```tsx
import { HmsShell } from "@hms/ui";
import { DispenseList } from "@/components/dispense-list";

export default function DispensesPage() {
  return (
    <HmsShell active="/pharmacy">
      <h1 className="mb-6 text-2xl font-semibold">Pharmacy — Dispenses</h1>
      <DispenseList />
    </HmsShell>
  );
}
```

`apps/pharmacy/app/medications/page.tsx`:

```tsx
import { HmsShell } from "@hms/ui";
import { MedicationsPanel } from "@/components/medications-panel";

export default function MedicationsPage() {
  return (
    <HmsShell active="/pharmacy/medications">
      <h1 className="mb-6 text-2xl font-semibold">Pharmacy — Medications</h1>
      <MedicationsPanel />
    </HmsShell>
  );
}
```

`apps/pharmacy/components/dispense-list.tsx`:

```tsx
"use client";

import { useCallback, useEffect, useState } from "react";

type Dispense = {
  id: string;
  visit_id: string;
  patient_name: string;
  medication: string;
  status: "pending" | "dispensed";
  dispensed_at: string | null;
  created_at: string;
};

export function DispenseList() {
  const [rows, setRows] = useState<Dispense[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [busyID, setBusyID] = useState<string | null>(null);
  const [medication, setMedication] = useState("Paracetamol 500mg");

  const load = useCallback(async () => {
    try {
      const res = await fetch("/api/v1/pharmacy/dispenses");
      if (!res.ok) throw new Error(String(res.status));
      const body = await res.json();
      setRows(body.data ?? []);
      setError(null);
    } catch {
      setError("Could not load dispenses. Is the API running?");
    }
  }, []);

  useEffect(() => {
    void load();
    const timer = setInterval(() => void load(), 3000);
    return () => clearInterval(timer);
  }, [load]);

  async function dispense(id: string) {
    setBusyID(id);
    try {
      const res = await fetch(`/api/v1/pharmacy/dispenses/${id}/dispense`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ medication }),
      });
      if (!res.ok) throw new Error(String(res.status));
      await load();
    } catch {
      setError("Dispense failed.");
    } finally {
      setBusyID(null);
    }
  }

  return (
    <section className="max-w-3xl space-y-4">
      <label className="flex max-w-sm flex-col gap-1 text-sm">
        Medication
        <input
          value={medication}
          onChange={(e) => setMedication(e.target.value)}
          className="rounded-md border px-3 py-2"
        />
      </label>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <ul className="divide-y rounded-md border text-sm">
        {rows.length === 0 && <li className="p-3 text-muted-foreground">No dispense tasks yet.</li>}
        {rows.map((d) => (
          <li key={d.id} className="flex items-center justify-between gap-4 p-3">
            <div className="min-w-0">
              <div className="font-medium">{d.patient_name}</div>
              <div className="text-muted-foreground">
                {d.status === "dispensed" ? `Dispensed ${d.medication}` : "Pending dispense"}
              </div>
            </div>
            {d.status === "pending" ? (
              <button
                onClick={() => dispense(d.id)}
                disabled={busyID === d.id}
                className="rounded-md bg-primary px-3 py-2 text-primary-foreground disabled:opacity-50"
              >
                {busyID === d.id ? "Dispensing…" : "Dispense"}
              </button>
            ) : (
              <span className="text-muted-foreground">Done</span>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}
```

`apps/pharmacy/components/medications-panel.tsx`:

```tsx
"use client";

import { useCallback, useEffect, useState } from "react";

type Medication = { id: string; name: string; strength: string; created_at: string };

export function MedicationsPanel() {
  const [rows, setRows] = useState<Medication[]>([]);
  const [name, setName] = useState("");
  const [strength, setStrength] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await fetch("/api/v1/pharmacy/medications");
      if (!res.ok) throw new Error(String(res.status));
      const body = await res.json();
      setRows(body.data ?? []);
      setError(null);
    } catch {
      setError("Could not load medications. Is the API running?");
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  async function add(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      const res = await fetch("/api/v1/pharmacy/medications", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ name, strength }),
      });
      if (!res.ok) throw new Error(String(res.status));
      setName("");
      setStrength("");
      await load();
    } catch {
      setError("Could not add medication.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="max-w-xl space-y-4">
      <form onSubmit={add} className="flex flex-wrap items-end gap-3">
        <label className="flex flex-col gap-1 text-sm">
          Name
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            className="rounded-md border px-3 py-2"
          />
        </label>
        <label className="flex flex-col gap-1 text-sm">
          Strength
          <input
            value={strength}
            onChange={(e) => setStrength(e.target.value)}
            className="rounded-md border px-3 py-2"
          />
        </label>
        <button
          type="submit"
          disabled={busy}
          className="rounded-md bg-primary px-3 py-2 text-sm text-primary-foreground disabled:opacity-50"
        >
          {busy ? "Adding…" : "Add medication"}
        </button>
      </form>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <ul className="divide-y rounded-md border text-sm">
        {rows.length === 0 && <li className="p-3 text-muted-foreground">No medications yet.</li>}
        {rows.map((m) => (
          <li key={m.id} className="flex justify-between p-3">
            <span>{m.name}</span>
            <span className="text-muted-foreground">{m.strength}</span>
          </li>
        ))}
      </ul>
    </section>
  );
}
```

- [ ] **Step 3: Shell rewrite**

In `apps/shell/next.config.ts`, add below `MEDICORE_URL`:

```ts
const PHARMACY_URL = process.env.PHARMACY_URL ?? "http://localhost:4303";
```

and in `rewrites()`, after the medicore entries:

```ts
      { source: "/pharmacy", destination: `${PHARMACY_URL}/pharmacy` },
      { source: "/pharmacy/:path*", destination: `${PHARMACY_URL}/pharmacy/:path*` },
```

- [ ] **Step 4: Verify build**

Run: `pnpm install && pnpm turbo type-check build --filter=@hms/pharmacy --filter=@hms/shell`
Expected: clean builds.

- [ ] **Step 5: Commit**

```bash
git add apps/pharmacy apps/shell/next.config.ts pnpm-lock.yaml
git commit -m "feat: pharmacy zone app with dispenses and medications pages"
```

---

### Task 9: apps/lab zone app + shell rewrite

**Files:**
- Create: `apps/lab/package.json`, `apps/lab/next.config.ts`, `apps/lab/tsconfig.json`, `apps/lab/postcss.config.mjs`, `apps/lab/app/layout.tsx`, `apps/lab/app/globals.css`, `apps/lab/app/page.tsx`, `apps/lab/components/order-list.tsx`
- Modify: `apps/shell/next.config.ts` (lab rewrite)

**Interfaces:**
- Consumes: `HmsShell` from `@hms/ui`; API routes `GET /api/v1/lab/orders`, `POST /api/v1/lab/orders/:id/result`.
- Produces: lab zone at `/lab` (port 4304) with the Orders page; shell stitches `/lab/*`.

- [ ] **Step 1: App scaffolding**

Clone scaffolding exactly as Task 8 Step 1 (tsconfig, postcss, layout, globals.css) with these substitutions: package name `@hms/lab`, ports 4304, `basePath: "/lab"`.

`apps/lab/package.json` — same shape as Task 8's with:

```json
  "name": "@hms/lab",
  "scripts": {
    "dev": "next dev -p 4304",
    "build": "next build",
    "start": "next start -p 4304",
    "type-check": "tsc --noEmit"
  },
```

`apps/lab/next.config.ts` — same as Task 8's with `basePath: "/lab"`.

- [ ] **Step 2: Orders page**

`apps/lab/app/page.tsx`:

```tsx
import { HmsShell } from "@hms/ui";
import { OrderList } from "@/components/order-list";

export default function OrdersPage() {
  return (
    <HmsShell active="/lab">
      <h1 className="mb-6 text-2xl font-semibold">Lab — Orders</h1>
      <OrderList />
    </HmsShell>
  );
}
```

`apps/lab/components/order-list.tsx`:

```tsx
"use client";

import { useCallback, useEffect, useState } from "react";

type Order = {
  id: string;
  visit_id: string;
  patient_name: string;
  test_name: string;
  status: "pending" | "completed";
  result_value: string | null;
  resulted_at: string | null;
  created_at: string;
};

export function OrderList() {
  const [rows, setRows] = useState<Order[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [busyID, setBusyID] = useState<string | null>(null);
  const [drafts, setDrafts] = useState<Record<string, string>>({});

  const load = useCallback(async () => {
    try {
      const res = await fetch("/api/v1/lab/orders");
      if (!res.ok) throw new Error(String(res.status));
      const body = await res.json();
      setRows(body.data ?? []);
      setError(null);
    } catch {
      setError("Could not load orders. Is the API running?");
    }
  }, []);

  useEffect(() => {
    void load();
    const timer = setInterval(() => void load(), 3000);
    return () => clearInterval(timer);
  }, [load]);

  async function saveResult(id: string) {
    setBusyID(id);
    try {
      const res = await fetch(`/api/v1/lab/orders/${id}/result`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ result_value: drafts[id] ?? "" }),
      });
      if (!res.ok) throw new Error(String(res.status));
      await load();
    } catch {
      setError("Could not save result.");
    } finally {
      setBusyID(null);
    }
  }

  return (
    <section className="max-w-3xl space-y-4">
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <ul className="divide-y rounded-md border text-sm">
        {rows.length === 0 && <li className="p-3 text-muted-foreground">No lab orders yet.</li>}
        {rows.map((o) => (
          <li key={o.id} className="flex items-center justify-between gap-4 p-3">
            <div className="min-w-0">
              <div className="font-medium">
                {o.patient_name} — {o.test_name}
              </div>
              <div className="text-muted-foreground">
                {o.status === "completed" ? `Result: ${o.result_value}` : "Awaiting result"}
              </div>
            </div>
            {o.status === "pending" ? (
              <div className="flex items-center gap-2">
                <label className="sr-only" htmlFor={`result-${o.id}`}>
                  Result for {o.patient_name}
                </label>
                <input
                  id={`result-${o.id}`}
                  value={drafts[o.id] ?? ""}
                  onChange={(e) => setDrafts((d) => ({ ...d, [o.id]: e.target.value }))}
                  placeholder="e.g. WBC 6.1"
                  className="w-40 rounded-md border px-3 py-2"
                />
                <button
                  onClick={() => saveResult(o.id)}
                  disabled={busyID === o.id || !(drafts[o.id] ?? "").trim()}
                  className="rounded-md bg-primary px-3 py-2 text-primary-foreground disabled:opacity-50"
                >
                  {busyID === o.id ? "Saving…" : "Save result"}
                </button>
              </div>
            ) : (
              <span className="text-muted-foreground">Completed</span>
            )}
          </li>
        ))}
      </ul>
    </section>
  );
}
```

- [ ] **Step 3: Shell rewrite**

In `apps/shell/next.config.ts`, add below `PHARMACY_URL`:

```ts
const LAB_URL = process.env.LAB_URL ?? "http://localhost:4304";
```

and in `rewrites()`, after the pharmacy entries:

```ts
      { source: "/lab", destination: `${LAB_URL}/lab` },
      { source: "/lab/:path*", destination: `${LAB_URL}/lab/:path*` },
```

- [ ] **Step 4: Verify build**

Run: `pnpm install && pnpm turbo type-check build --filter=@hms/lab --filter=@hms/shell`
Expected: clean builds.

- [ ] **Step 5: Commit**

```bash
git add apps/lab apps/shell/next.config.ts pnpm-lock.yaml
git commit -m "feat: lab zone app with orders and results page"
```

---

### Task 10: medicore OPD — new visit form + visit list

**Files:**
- Create: `apps/medicore/components/visit-panel.tsx`
- Modify: `apps/medicore/app/opd/page.tsx`

**Interfaces:**
- Consumes: `POST /api/v1/medicore/visits` `{"patient_name","department"}` → `202 {"id"}`; `GET /api/v1/medicore/visits` → `{"data":[{id, patient_name, department, status, created_at}]}` (Task 2).
- Produces: OPD page where the E2E test creates a visit by filling "Patient name" and clicking "Create visit", then sees the patient in the visit list.

- [ ] **Step 1: Visit panel component**

`apps/medicore/components/visit-panel.tsx`:

```tsx
"use client";

import { useCallback, useEffect, useState } from "react";

type Visit = {
  id: string;
  patient_name: string;
  department: string;
  status: string;
  created_at: string;
};

export function VisitPanel({ department }: { department: "OPD" | "IPD" }) {
  const [visits, setVisits] = useState<Visit[]>([]);
  const [patientName, setPatientName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      const res = await fetch("/api/v1/medicore/visits");
      if (!res.ok) throw new Error(String(res.status));
      const body = await res.json();
      setVisits(body.data ?? []);
      setError(null);
    } catch {
      setError("Could not load visits. Is the API running?");
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  async function createVisit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    try {
      const res = await fetch("/api/v1/medicore/visits", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ patient_name: patientName, department }),
      });
      if (!res.ok) throw new Error(String(res.status));
      setPatientName("");
      await load();
    } catch {
      setError("Could not create visit.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="max-w-xl space-y-4">
      <form onSubmit={createVisit} className="flex items-end gap-3">
        <label className="flex flex-col gap-1 text-sm">
          Patient name
          <input
            value={patientName}
            onChange={(e) => setPatientName(e.target.value)}
            required
            maxLength={200}
            className="rounded-md border px-3 py-2"
          />
        </label>
        <button
          type="submit"
          disabled={busy}
          className="rounded-md bg-primary px-3 py-2 text-sm text-primary-foreground disabled:opacity-50"
        >
          {busy ? "Creating…" : "Create visit"}
        </button>
      </form>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <ul className="divide-y rounded-md border text-sm">
        {visits.length === 0 && <li className="p-3 text-muted-foreground">No visits yet.</li>}
        {visits.map((v) => (
          <li key={v.id} className="flex justify-between p-3">
            <span>
              {v.patient_name} <span className="text-muted-foreground">({v.department})</span>
            </span>
            <time className="text-muted-foreground">{new Date(v.created_at).toLocaleTimeString()}</time>
          </li>
        ))}
      </ul>
    </section>
  );
}
```

- [ ] **Step 2: Mount it on the OPD page**

`apps/medicore/app/opd/page.tsx`:

```tsx
import { HmsShell } from "@hms/ui";
import { PingPanel } from "@/components/ping-panel";
import { VisitPanel } from "@/components/visit-panel";

export default function OpdPage() {
  return (
    <HmsShell active="/medicore/opd">
      <h1 className="mb-6 text-2xl font-semibold">OPD — Outpatients</h1>
      <div className="space-y-10">
        <VisitPanel department="OPD" />
        <PingPanel department="OPD" />
      </div>
    </HmsShell>
  );
}
```

- [ ] **Step 3: Verify build**

Run: `pnpm turbo type-check build --filter=@hms/medicore`
Expected: clean.

- [ ] **Step 4: Commit**

```bash
git add apps/medicore
git commit -m "feat: opd visit creation form and visit list"
```

---

### Task 11: E2E — extend Playwright smoke to the full journey

**Files:**
- Modify: `e2e/tests/smoke.spec.ts`

**Interfaces:**
- Consumes: UI from Tasks 7–10 running behind the shell at `http://localhost:4301` (config unchanged); seeded user `test@hms.dev` / `password123`.
- Produces: one spec covering login → OPD visit → pharmacy dispense → lab result. Event propagation is async — assertions use Playwright auto-retrying `expect` with generous timeouts, and the zone pages self-refresh every 3s.

- [ ] **Step 1: Extend the spec**

Replace the body of `e2e/tests/smoke.spec.ts` with:

```ts
import { test, expect } from "@playwright/test";

test("login, OPD visit, pharmacy dispense, lab result", async ({ page }) => {
  const patient = `E2E Patient ${Date.now()}`;

  // Unauthenticated → redirected to login.
  await page.goto("/");
  await expect(page).toHaveURL(/\/login$/);

  await page.getByLabel("Email").fill("test@hms.dev");
  await page.getByLabel("Password").fill("password123");
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("heading", { name: "Departments" })).toBeVisible();

  // Shell → medicore (hard navigation across the zone boundary).
  await page.getByRole("link", { name: "OPD", exact: true }).first().click();
  await expect(page).toHaveURL(/\/medicore\/opd$/);

  // Create the OPD visit.
  await page.getByLabel("Patient name").fill(patient);
  await page.getByRole("button", { name: "Create visit" }).click();
  await expect(page.getByText(patient).first()).toBeVisible();

  // Pharmacy zone: visit_created fans out asynchronously; the page
  // polls every 3s, so just wait for the patient to appear.
  await page.goto("/pharmacy");
  await expect(page.getByText(patient).first()).toBeVisible({ timeout: 30_000 });
  await page
    .locator("li", { hasText: patient })
    .getByRole("button", { name: "Dispense" })
    .click();
  await expect(page.locator("li", { hasText: patient }).getByText(/Dispensed/)).toBeVisible();

  // Lab zone: pending order for the same visit; record a result.
  await page.goto("/lab");
  await expect(page.getByText(patient).first()).toBeVisible({ timeout: 30_000 });
  const orderRow = page.locator("li", { hasText: patient });
  await orderRow.getByLabel(`Result for ${patient}`).fill("WBC 6.1");
  await orderRow.getByRole("button", { name: "Save result" }).click();
  await expect(orderRow.getByText("Result: WBC 6.1")).toBeVisible();
});
```

- [ ] **Step 2: Commit** (the run happens in Task 12 against the live stack)

```bash
git add e2e/tests/smoke.spec.ts
git commit -m "test: e2e journey covering opd visit, pharmacy dispense and lab result"
```

---

### Task 12: Local full-stack verification

**Files:** none created — this task runs the stack and the E2E suite. Fix-forward any failures (small fixes belong in this task; structural problems reopen the owning task).

- [ ] **Step 1: Full test suites**

Run: `cd backend && go test -race ./...`
Expected: PASS.
Run: `pnpm turbo lint type-check build` (from repo root; skip `lint` if no app defines it)
Expected: PASS for shell, medicore, pharmacy, lab, @hms/ui.

- [ ] **Step 2: Boot infra + API + web**

```bash
make dev-infra
# wait for firebase emulator: docker compose -f docker-compose.dev.yml logs firebase-auth | grep -i ready
make seed
```

Then in background terminals (or `make dev`): `make dev-api` and `pnpm turbo dev` (starts shell 4301, medicore 4302, pharmacy 4303, lab 4304).

Verify readiness: `curl -s localhost:8080/readyz` → `{"status":"ready"}`.

- [ ] **Step 3: API-level journey probe**

Sign in via the emulator REST API and drive the journey with curl:

```bash
TOKEN=$(curl -s "http://localhost:9099/identitytoolkit.googleapis.com/v1/accounts:signInWithPassword?key=demo-key" \
  -H 'Content-Type: application/json' \
  -d '{"email":"test@hms.dev","password":"password123","returnSecureToken":true}' | node -pe 'JSON.parse(require("fs").readFileSync(0)).idToken')

# Create a visit
curl -s -X POST localhost:8080/v1/medicore/visits -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' -d '{"patient_name":"Curl Patient","department":"OPD"}'

# Within ~2s both fan-outs appear:
sleep 3
curl -s localhost:8080/v1/pharmacy/dispenses -H "Authorization: Bearer $TOKEN"   # → pending row
curl -s localhost:8080/v1/lab/orders -H "Authorization: Bearer $TOKEN"           # → pending CBC order
```

Expected: the dispense and order lists each contain "Curl Patient" with status `pending`.

- [ ] **Step 4: Browser E2E**

Run: `cd e2e && pnpm install && npx playwright test`
Expected: the journey spec passes. Also eyeball `http://localhost:4301` — the two-rail sidebar shows the dark slate rail (`#0f172a`), zone icons switch the secondary panel, and `/pharmacy` + `/lab` render inside the shell chrome.

- [ ] **Step 5: Tear down and final commit**

```bash
make dev-down
git status   # any fix-forward changes:
git add -A && git commit -m "fix: phase 2 verification fixes"   # only if there are changes
```

---

## Self-review notes

- Spec coverage: D4 → Task 1; medicore/pharmacy/lab modules → Tasks 2–4; registration + journey test → Task 5; `@hms/ui` chrome + tokens + registry (D3/D5) → Task 6; app migration → Task 7; new zones + shell rewrites → Tasks 8–9; OPD visit UI → Task 10; E2E journey → Task 11; local run verification (user request) → Task 12.
- Naming consistency: `HmsShell{active}`, `ZONES`/`activeZone`, subjects `hms.in.medicore.visit_created.v1` / `hms.in.pharmacy.dispense_recorded.v1` / `hms.in.lab.result_ready.v1`, consumers `pharmacy-visit-intake` / `lab-visit-intake`, migration IDs `0001_medicore|pharmacy|lab` — used identically across tasks.
- Deliberate scope cuts (per spec): no consumers for `dispense_recorded`/`result_ready`, no mobile drawer, no OpenFGA.

# Directed Cross-Tenant Events Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let one tenant's event create a record in another tenant, exactly once, only on declared subjects, always with provenance, without widening any RLS read.

**Architecture:** The event envelope gains a destination distinct from the origin. The bus refuses a destination it was not told to allow — fail-closed, before the capability to honour one even exists — then scopes the consumer transaction to the destination when it is allowed. Modules declare which of their subjects may be directed and which tables accept directed writes; the registry validates the first at boot and `LintRLS`'s sibling check validates the second against the live schema.

**Tech Stack:** Go 1.26, Gin, GORM, PostgreSQL 16 (forced RLS), NATS JetStream, testify, testcontainers.

**Spec:** `docs/superpowers/specs/2026-08-22-directed-cross-tenant-events-design.md`

**Issue:** [#932](https://github.com/tesserix/helivanta/issues/932)

## Global Constraints

- Go 1.26. `slog` only — logrus is banned.
- Modules never import other modules, except a module's own `contract` package. Enforced by `internal/archtest`.
- Migration IDs are `NNNN_<module>`, append-only. Never edit a shipped migration.
- Tenant data only via `WithTenant`. Every tenant table gets RLS enabled **and** forced.
- `WITH CHECK` must never call `hms_tenant_visible`. Reads may widen; writes stay pinned to one tenant. This plan changes no policy.
- Contract packages are data only: consts, types, vars. No funcs, no methods.
- Errors wrapped with `%w`.
- Before done: `make lint-go` clean, `cd backend && ./scripts/coverage-gate.sh` green (70% floor), `go test -race ./...` green.
- Every new assertion must be proven capable of failing — by mutation, not by inspection.

---

### Task 1: The bus refuses an undeclared destination

Fail-closed first. After this task the envelope can carry a destination and the bus will **refuse every one of them**, because nothing has declared any subject directed yet. The capability to honour one arrives in Task 2.

**Files:**
- Modify: `backend/pkg/events/types.go`
- Modify: `backend/pkg/events/bus.go`
- Test: `backend/pkg/events/directed_test.go` (create)

**Interfaces:**
- Consumes: nothing.
- Produces: `events.Event.DestinationTenantID string`; `func (b *Bus) AllowDirected(subjects ...string)`; unexported `func (b *Bus) directedAllowed(subject string) bool`.

- [ ] **Step 1: Write the failing test**

Create `backend/pkg/events/directed_test.go`:

```go
package events_test

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// TestDirectedEventOnUndeclaredSubjectIsRefused is the fail-closed
// default: a destination the bus was never told to allow must not reach
// the handler at all. This is what makes a wiring regression — the
// AllowDirected call being dropped from main.go — safe rather than
// silently permissive.
func TestDirectedEventOnUndeclaredSubjectIsRefused(t *testing.T) {
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)
	require.NoError(t, db.Migrate(context.Background(),
		append(tenantdb.Migrations(), events.Migrations()...)))

	natsURL := testinfra.StartNATS(t)
	bus, err := events.NewBusInNamespace(natsURL, t.Name())
	require.NoError(t, err)
	defer bus.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Deliberately no AllowDirected call.
	var handled atomic.Int32
	require.NoError(t, bus.StartConsumers(ctx, db, []events.Consumer{{
		Name:    "refusal-consumer",
		Subject: "helivanta.in.reference.pinged.v1",
		Handle: func(context.Context, *gorm.DB, events.Event) error {
			handled.Add(1)
			return nil
		},
	}}))
	go bus.RunDispatcher(ctx, db)

	origin, destination := uuid.NewString(), uuid.NewString()
	evt := events.Event{
		ID: uuid.NewString(), Type: "ReferencePinged", Version: 1,
		OccurredAt: time.Now().UTC(),
		TenantID:   origin, DestinationTenantID: destination,
		Data: json.RawMessage(`{"ping_id":"p-1"}`),
	}
	require.NoError(t, db.WithTenant(ctx, origin, func(tx *gorm.DB) error {
		return bus.Publish(tx, "helivanta.in.reference.pinged.v1", evt)
	}))

	// Give the dispatcher and consumer ample time to have done the wrong
	// thing, then assert they did not.
	require.Never(t, func() bool { return handled.Load() > 0 },
		5*time.Second, 100*time.Millisecond,
		"a directed event on an undeclared subject reached the handler")
}

// TestUndirectedEventIsUnaffected pins the regression: adding the field
// must not change any existing event's path.
func TestUndirectedEventIsUnaffected(t *testing.T) {
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)
	require.NoError(t, db.Migrate(context.Background(),
		append(tenantdb.Migrations(), events.Migrations()...)))

	natsURL := testinfra.StartNATS(t)
	bus, err := events.NewBusInNamespace(natsURL, t.Name())
	require.NoError(t, err)
	defer bus.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	var handled atomic.Int32
	require.NoError(t, bus.StartConsumers(ctx, db, []events.Consumer{{
		Name:    "undirected-consumer",
		Subject: "helivanta.in.reference.pinged.v1",
		Handle: func(context.Context, *gorm.DB, events.Event) error {
			handled.Add(1)
			return nil
		},
	}}))
	go bus.RunDispatcher(ctx, db)

	origin := uuid.NewString()
	require.NoError(t, db.WithTenant(ctx, origin, func(tx *gorm.DB) error {
		return bus.Publish(tx, "helivanta.in.reference.pinged.v1", events.Event{
			ID: uuid.NewString(), Type: "ReferencePinged", Version: 1,
			OccurredAt: time.Now().UTC(), TenantID: origin,
			Data: json.RawMessage(`{"ping_id":"p-1"}`),
		})
	}))

	require.Eventually(t, func() bool { return handled.Load() == 1 },
		20*time.Second, 100*time.Millisecond)
}

// TestDirectedEnvelopeOmitsFieldWhenEmpty proves the wire format of every
// existing event is byte-identical. omitempty is load-bearing: a new
// always-present key would change every stored outbox payload.
func TestDirectedEnvelopeOmitsFieldWhenEmpty(t *testing.T) {
	b, err := json.Marshal(events.Event{ID: "e-1", TenantID: "t-1"})
	require.NoError(t, err)
	require.NotContains(t, string(b), "destination_tenant_id")
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./pkg/events/ -run 'TestDirected|TestUndirected' -v`
Expected: compile failure — `unknown field DestinationTenantID in struct literal`.

- [ ] **Step 3: Add the envelope field**

In `backend/pkg/events/types.go`, inside `type Event struct`, after `TenantID`:

```go
	// DestinationTenantID, when set, is the tenant whose data this event
	// creates. TenantID stays the ORIGIN — who published, and therefore
	// who is accountable for the disclosure — so a cross-organisation
	// handoff is auditable from the envelope alone (design D1).
	//
	// omitempty is load-bearing, not cosmetic: without it every event
	// ever published would gain a key, changing the bytes stored in
	// outbox_events for payloads that have not otherwise changed.
	DestinationTenantID string `json:"destination_tenant_id,omitempty"`
```

- [ ] **Step 4: Add the allowlist to the Bus and refuse in handleMsg**

In `backend/pkg/events/bus.go`, add to `type Bus struct`, after `ns`:

```go
	// directed is the set of subjects permitted to carry a
	// DestinationTenantID (design D4). It starts EMPTY and is populated
	// from the module registry at boot, so a subject is non-directed
	// unless something declared it — and dropping the wiring makes every
	// directed event fail loudly rather than silently cross a tenant
	// boundary.
	directed map[string]struct{}
```

Add, after the `Subject` method:

```go
// AllowDirected permits these subjects to carry a DestinationTenantID.
//
// Called once at boot from the module registry's declarations. Nothing
// else may call it: the point of the allowlist is that the set of
// subjects able to cross a tenant boundary is small, reviewed and
// greppable, which a runtime mutation would undo.
func (b *Bus) AllowDirected(subjects ...string) {
	if b.directed == nil {
		b.directed = make(map[string]struct{}, len(subjects))
	}
	for _, s := range subjects {
		b.directed[s] = struct{}{}
	}
}

func (b *Bus) directedAllowed(subject string) bool {
	_, ok := b.directed[subject]
	return ok
}
```

In `handleMsg`, immediately after the `json.Unmarshal` error branch and before `runConsumerTx`:

```go
	if evt.DestinationTenantID != "" && !b.directedAllowed(c.Subject) {
		// Term, not Nak: redelivery cannot make an undeclared subject
		// declared, so retrying five times only delays the log line.
		slog.Error("refused directed event on undeclared subject",
			"consumer", c.Name, "subject", c.Subject, "event_id", evt.ID,
			"tenant_id", evt.TenantID, "destination_tenant_id", evt.DestinationTenantID)
		_ = msg.Term()
		return
	}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd backend && go test ./pkg/events/ -run 'TestDirected|TestUndirected' -v`
Expected: PASS (all three).

- [ ] **Step 6: Prove the refusal test can fail**

Temporarily change the guard to `if false && evt.DestinationTenantID != "" && ...`, re-run
`TestDirectedEventOnUndeclaredSubjectIsRefused`, and confirm it FAILS. Revert the change and confirm it passes again.

- [ ] **Step 7: Run the full events suite**

Run: `cd backend && go test -race ./pkg/events/...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/pkg/events/types.go backend/pkg/events/bus.go backend/pkg/events/directed_test.go
git commit -m "feat(932): refuse directed events on undeclared subjects"
```

---

### Task 2: Honour a declared destination, scoped to exactly one tenant

**Files:**
- Modify: `backend/pkg/events/bus.go`
- Test: `backend/pkg/events/directed_test.go`

**Interfaces:**
- Consumes: `Event.DestinationTenantID`, `Bus.AllowDirected`, `Bus.directedAllowed` (Task 1).
- Produces: unexported `func scopeTenant(evt Event) string`.

- [ ] **Step 1: Write the failing tests**

Append to `backend/pkg/events/directed_test.go`:

```go
// directedTestTable is created per test rather than reusing a module's
// table: pkg/events must not import internal/modules.
const directedTestTable = `
CREATE TABLE IF NOT EXISTS directed_probe (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  origin_tenant_id uuid NOT NULL,
  origin_record_id uuid NOT NULL,
  note text NOT NULL
);
ALTER TABLE directed_probe ENABLE ROW LEVEL SECURITY;
ALTER TABLE directed_probe FORCE ROW LEVEL SECURITY;
DROP POLICY IF EXISTS tenant_isolation ON directed_probe;
CREATE POLICY tenant_isolation ON directed_probe
  USING (hms_tenant_visible(tenant_id))
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);`

// TestDirectedWriteLandsInDestinationAndIsInvisibleToOrigin is the claim
// the whole design exists to support, asserted on rows rather than on the
// code that wrote them. The second half matters more than the first: a
// mechanism that writes to the destination but leaves the row readable
// from the origin has widened access, which is exactly what this design
// refuses to do.
func TestDirectedWriteLandsInDestinationAndIsInvisibleToOrigin(t *testing.T) {
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	require.NoError(t, db.Migrate(ctx, append(tenantdb.Migrations(), events.Migrations()...)))
	require.NoError(t, db.WithAdmin(ctx, func(tx *gorm.DB) error {
		return tx.Exec(directedTestTable).Error
	}))

	natsURL := testinfra.StartNATS(t)
	bus, err := events.NewBusInNamespace(natsURL, t.Name())
	require.NoError(t, err)
	defer bus.Close()

	const subject = "helivanta.in.reference.pinged.v1"
	bus.AllowDirected(subject)

	origin, destination := uuid.NewString(), uuid.NewString()
	pingID := uuid.NewString()

	require.NoError(t, bus.StartConsumers(ctx, db, []events.Consumer{{
		Name:    "directed-probe-consumer",
		Subject: subject,
		Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
			return tx.Exec(
				`INSERT INTO directed_probe (tenant_id, origin_tenant_id, origin_record_id, note)
				 VALUES (?, ?, ?, ?)`,
				evt.DestinationTenantID, evt.TenantID, pingID, "routed").Error
		},
	}}))
	go bus.RunDispatcher(ctx, db)

	require.NoError(t, db.WithTenant(ctx, origin, func(tx *gorm.DB) error {
		return bus.Publish(tx, subject, events.Event{
			ID: uuid.NewString(), Type: "ReferencePinged", Version: 1,
			OccurredAt: time.Now().UTC(),
			TenantID:   origin, DestinationTenantID: destination,
			Data: json.RawMessage(`{"ping_id":"` + pingID + `"}`),
		})
	}))

	countAs := func(tenant string) int {
		var n int
		require.NoError(t, db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM directed_probe`).Scan(&n).Error
		}))
		return n
	}

	require.Eventually(t, func() bool { return countAs(destination) == 1 },
		30*time.Second, 200*time.Millisecond,
		"directed write never landed in the destination tenant")
	require.Equal(t, 0, countAs(origin),
		"the origin tenant can read the row it disclosed — access was widened, not copied")

	// Provenance is readable from the row itself, not reconstructed from logs.
	var gotOrigin string
	require.NoError(t, db.WithTenant(ctx, destination, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT origin_tenant_id::text FROM directed_probe`).Scan(&gotOrigin).Error
	}))
	require.Equal(t, origin, gotOrigin)
}

// TestDirectedEventWithUnparseableDestinationIsTerminated: a destination
// that is not a UUID is a poison envelope. It must not fall through to
// the origin's scope, which would write the row into the WRONG tenant —
// the most dangerous available failure mode.
func TestDirectedEventWithUnparseableDestinationIsTerminated(t *testing.T) {
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	require.NoError(t, db.Migrate(ctx, append(tenantdb.Migrations(), events.Migrations()...)))

	natsURL := testinfra.StartNATS(t)
	bus, err := events.NewBusInNamespace(natsURL, t.Name())
	require.NoError(t, err)
	defer bus.Close()

	const subject = "helivanta.in.reference.pinged.v1"
	bus.AllowDirected(subject)

	var handled atomic.Int32
	require.NoError(t, bus.StartConsumers(ctx, db, []events.Consumer{{
		Name:    "bad-destination-consumer",
		Subject: subject,
		Handle: func(context.Context, *gorm.DB, events.Event) error {
			handled.Add(1)
			return nil
		},
	}}))
	go bus.RunDispatcher(ctx, db)

	origin := uuid.NewString()
	require.NoError(t, db.WithTenant(ctx, origin, func(tx *gorm.DB) error {
		return bus.Publish(tx, subject, events.Event{
			ID: uuid.NewString(), Type: "ReferencePinged", Version: 1,
			OccurredAt: time.Now().UTC(),
			TenantID:   origin, DestinationTenantID: "not-a-uuid",
			Data: json.RawMessage(`{"ping_id":"p-1"}`),
		})
	}))

	require.Never(t, func() bool { return handled.Load() > 0 },
		5*time.Second, 100*time.Millisecond,
		"an event with an unparseable destination reached the handler")
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./pkg/events/ -run 'TestDirectedWrite|TestDirectedEventWithUnparseable' -v`
Expected: `TestDirectedWriteLandsInDestination...` FAILS — the row lands in the origin, so `countAs(destination)` stays 0. `TestDirectedEventWithUnparseable...` FAILS — the handler runs under the origin scope.

- [ ] **Step 3: Add scopeTenant and use it**

In `backend/pkg/events/bus.go`, add above `runConsumerTx`:

```go
// scopeTenant is the tenant whose data this event creates: the
// destination when directed, the origin otherwise. Exactly one tenant,
// always — the consumer tx is never scoped to both and never to neither
// (design D2).
func scopeTenant(evt Event) string {
	if evt.DestinationTenantID != "" {
		return evt.DestinationTenantID
	}
	return evt.TenantID
}
```

In `runConsumerTx`, replace the tenant-scoping block:

```go
		// Scope the whole consumer tx (claim + handler) to the tenant
		// whose data this event creates — the destination for a directed
		// event, the origin otherwise (phase 2 D4, design D2).
		// Invalid/empty → GUC stays unset → tenant tables read as empty
		// and reject writes, same as before.
		if scope := scopeTenant(evt); scope != "" {
			if _, err := uuid.Parse(scope); err == nil {
				if err := tx.Exec(`SELECT set_config('app.tenant_id', ?, true)`, scope).Error; err != nil {
					return err
				}
			}
		}
```

- [ ] **Step 4: Terminate an unparseable destination in handleMsg**

In `backend/pkg/events/bus.go`, extend the guard added in Task 1 so it also catches a malformed destination — replace that block with:

```go
	if evt.DestinationTenantID != "" {
		if !b.directedAllowed(c.Subject) {
			// Term, not Nak: redelivery cannot make an undeclared subject
			// declared, so retrying five times only delays the log line.
			slog.Error("refused directed event on undeclared subject",
				"consumer", c.Name, "subject", c.Subject, "event_id", evt.ID,
				"tenant_id", evt.TenantID, "destination_tenant_id", evt.DestinationTenantID)
			_ = msg.Term()
			return
		}
		if _, err := uuid.Parse(evt.DestinationTenantID); err != nil {
			// Falling through would scope the tx to the ORIGIN and write
			// the row into the wrong tenant — silently, and looking like
			// success. Terminate instead.
			slog.Error("refused directed event with unparseable destination",
				"consumer", c.Name, "subject", c.Subject, "event_id", evt.ID,
				"tenant_id", evt.TenantID, "destination_tenant_id", evt.DestinationTenantID)
			_ = msg.Term()
			return
		}
	}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd backend && go test ./pkg/events/ -run 'TestDirected|TestUndirected' -v`
Expected: PASS (all five).

- [ ] **Step 6: Prove the isolation assertion can fail**

Temporarily change `scopeTenant` to always `return evt.TenantID`, re-run
`TestDirectedWriteLandsInDestinationAndIsInvisibleToOrigin`, and confirm it FAILS on the destination count. Then change the consumer's insert in the test to use `evt.TenantID` for `tenant_id` and confirm the **origin-count** assertion fails. Revert both.

- [ ] **Step 7: Run the full events suite with race**

Run: `cd backend && go test -race ./pkg/events/...`
Expected: PASS.

- [ ] **Step 8: Commit**

```bash
git add backend/pkg/events/bus.go backend/pkg/events/directed_test.go
git commit -m "feat(932): scope the consumer tx to an allowed destination tenant"
```

---

### Task 3: Modules declare directed subjects; the registry validates at boot

**Files:**
- Modify: `backend/internal/platform/module.go`
- Modify: `backend/internal/platform/registry.go`
- Modify: `backend/internal/modules/reference/module.go`, `medicore/module.go`, `pharmacy/module.go`, `lab/module.go`, `iam/module.go`
- Modify: `backend/cmd/api/main.go:432` region
- Test: `backend/internal/platform/registry_test.go`, `backend/internal/archtest/events_test.go`

**Interfaces:**
- Consumes: `Bus.AllowDirected` (Task 1).
- Produces: `platform.Module.DirectedSubjects() []string`; `func (r *Registry) DirectedSubjects() []string`.

- [ ] **Step 1: Write the failing tests**

Append to `backend/internal/platform/registry_test.go`:

```go
// TestRegisterRejectsDirectedSubjectNotPublished makes a misdeclaration a
// BOOT failure rather than a runtime surprise. A subject declared
// directed but never published is either a typo or a deleted event, and
// either way the allowlist would be permitting something that no longer
// means what it says.
func TestRegisterRejectsDirectedSubjectNotPublished(t *testing.T) {
	r := platform.NewRegistry()
	err := r.Register(&fakeModule{
		name:      "bad",
		publishes: []string{"helivanta.in.bad.thing.v1"},
		directed:  []string{"helivanta.in.bad.typo.v1"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "helivanta.in.bad.typo.v1")
}

func TestRegisterAcceptsDirectedSubjectThatIsPublished(t *testing.T) {
	r := platform.NewRegistry()
	require.NoError(t, r.Register(&fakeModule{
		name:      "good",
		publishes: []string{"helivanta.in.good.thing.v1"},
		directed:  []string{"helivanta.in.good.thing.v1"},
	}))
	require.Equal(t, []string{"helivanta.in.good.thing.v1"}, r.DirectedSubjects())
}
```

If `registry_test.go` has no `fakeModule` covering these fields, add one there implementing every `platform.Module` method — `Name`, `Migrations`, `Permissions`, `Routes`, `Publishes`, `DirectedSubjects`, `DirectedWriteTables` (Task 4 adds this; return nil for now and revisit), `Consumers`, `Broadcasts` — returning the struct fields for `name`, `publishes`, `directed` and nil/no-op for the rest.

Append to `backend/internal/archtest/events_test.go`:

```go
// TestDirectedSubjectsAreDeclaredAndPublished is the whole-tree version of
// the registry's per-module check: it runs over the REAL modules, so a
// directed subject added to a module without adding it to Publishes()
// fails CI even if nobody boots the binary.
func TestDirectedSubjectsAreDeclaredAndPublished(t *testing.T) {
	for _, m := range allModules() {
		published := map[string]bool{}
		for _, s := range m.Publishes() {
			published[s] = true
		}
		for _, d := range m.DirectedSubjects() {
			require.True(t, published[d],
				"module %q declares %q directed but does not publish it", m.Name(), d)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./internal/platform/ ./internal/archtest/ -run 'Directed' -v`
Expected: compile failure — `DirectedSubjects` undefined on `platform.Module`.

- [ ] **Step 3: Add the interface method**

In `backend/internal/platform/module.go`, inside `type Module interface`, after `Publishes()`:

```go
	// DirectedSubjects declares which of this module's Publishes() may
	// carry a DestinationTenantID — that is, may create a record in a
	// tenant other than the one that published (design D4, #932).
	//
	// Must be a subset of Publishes(); Registry.Register fails at boot
	// otherwise, and archtest checks the same property over the real
	// modules. Entries must be constants from this module's own contract
	// package, like Publishes().
	//
	// This list is deliberately the smallest reviewable surface in the
	// codebase: everything on it can cross an organisational boundary.
	// A module with no cross-tenant events returns nil.
	DirectedSubjects() []string
```

- [ ] **Step 4: Implement on every module**

In each of `medicore`, `pharmacy`, `lab`, `iam`, add next to `Publishes`:

```go
// DirectedSubjects declares none: this module publishes nothing that
// creates data in another tenant.
func (m *Module) DirectedSubjects() []string { return nil }
```

Add the same to `reference/module.go` for now — Task 5 replaces its body.

- [ ] **Step 5: Validate in the registry**

In `backend/internal/platform/registry.go`, replace `Register` and add `DirectedSubjects`:

```go
// Register fails on duplicate names so route shadowing is a boot
// failure, not a silent bug (issue #2 edge case), and on a directed
// subject the module does not publish (#932): the allowlist may only
// name events that actually exist.
func (r *Registry) Register(m Module) error {
	if _, dup := r.byName[m.Name()]; dup {
		return fmt.Errorf("module %q registered twice", m.Name())
	}
	published := make(map[string]struct{}, len(m.Publishes()))
	for _, s := range m.Publishes() {
		published[s] = struct{}{}
	}
	for _, d := range m.DirectedSubjects() {
		if _, ok := published[d]; !ok {
			return fmt.Errorf("module %q declares %q directed but does not publish it", m.Name(), d)
		}
	}
	r.byName[m.Name()] = m
	r.ordered = append(r.ordered, m)
	return nil
}

// DirectedSubjects is the union of every registered module's
// declarations — the exact set main.go hands to Bus.AllowDirected, so
// the allowlist the bus enforces and the one modules declare cannot
// diverge.
func (r *Registry) DirectedSubjects() []string {
	var out []string
	for _, m := range r.ordered {
		out = append(out, m.DirectedSubjects()...)
	}
	return out
}
```

- [ ] **Step 6: Wire it in main.go**

In `backend/cmd/api/main.go`, immediately after the `bus, err := events.NewBus(cfg.NATSURL)` block and its error check (around line 217), add:

```go
	// The bus's directed allowlist starts empty and is populated here,
	// from the module declarations the registry already validated. If
	// this call is ever dropped, every directed event is refused rather
	// than silently permitted — see Bus.AllowDirected (#932).
	bus.AllowDirected(registry.DirectedSubjects()...)
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `cd backend && go test ./internal/platform/ ./internal/archtest/ -run 'Directed' -v`
Expected: PASS.

- [ ] **Step 8: Prove the boot check can fail**

Temporarily add `referencecontract.SubjectPinged + ".typo"` to `reference`'s `DirectedSubjects()`, run
`cd backend && go test ./internal/archtest/ -run TestDirectedSubjectsAreDeclaredAndPublished`, and confirm it FAILS. Revert.

- [ ] **Step 9: Build and run the full suite**

Run: `cd backend && go build ./... && go test -race ./internal/... ./pkg/...`
Expected: PASS.

- [ ] **Step 10: Commit**

```bash
git add backend/internal/platform backend/internal/modules backend/internal/archtest backend/cmd/api/main.go
git commit -m "feat(932): modules declare directed subjects, validated at boot"
```

---

### Task 4: Provenance columns are declared and linted

**Files:**
- Modify: `backend/internal/platform/module.go`
- Modify: `backend/internal/modules/*/module.go` (all five)
- Modify: `backend/pkg/tenantdb/db.go`
- Modify: `backend/cmd/api/main.go` (beside the existing `LintRLS` call, ~line 211)
- Modify: `backend/cmd/migrate/main.go`
- Test: `backend/pkg/tenantdb/db_test.go`

**Interfaces:**
- Consumes: `platform.Module` (Task 3).
- Produces: `platform.Module.DirectedWriteTables() []string`; `func (r *Registry) DirectedWriteTables() []string`; `func (d *DB) LintDirectedProvenance(ctx context.Context, tables []string) ([]string, error)`.

`LintDirectedProvenance` is a sibling of `LintRLS` rather than a new parameter on it. `LintRLS` has eight call sites across `cmd/`, `internal/testinfra`, `internal/testutil` and tests; widening its signature would churn all of them to serve one caller.

- [ ] **Step 1: Write the failing tests**

Append to `backend/pkg/tenantdb/db_test.go`:

```go
// TestLintDirectedProvenanceFlagsNullableColumn: provenance that can be
// NULL is provenance that will be NULL. The column existing is not the
// control; NOT NULL is.
func TestLintDirectedProvenanceFlagsNullableColumn(t *testing.T) {
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, db.Migrate(ctx, tenantdb.Migrations()))
	require.NoError(t, db.WithAdmin(ctx, func(tx *gorm.DB) error {
		return tx.Exec(`
			CREATE TABLE nullable_provenance (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  origin_tenant_id uuid,
			  origin_record_id uuid NOT NULL
			)`).Error
	}))

	bad, err := db.LintDirectedProvenance(ctx, []string{"nullable_provenance"})
	require.NoError(t, err)
	require.Len(t, bad, 1)
	require.Contains(t, bad[0], "origin_tenant_id")
}

func TestLintDirectedProvenanceFlagsMissingColumn(t *testing.T) {
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, db.Migrate(ctx, tenantdb.Migrations()))
	require.NoError(t, db.WithAdmin(ctx, func(tx *gorm.DB) error {
		return tx.Exec(`
			CREATE TABLE missing_provenance (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL
			)`).Error
	}))

	bad, err := db.LintDirectedProvenance(ctx, []string{"missing_provenance"})
	require.NoError(t, err)
	require.Len(t, bad, 1)
}

func TestLintDirectedProvenanceFlagsUnknownTable(t *testing.T) {
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, db.Migrate(ctx, tenantdb.Migrations()))

	// A declared table that does not exist is a typo in the declaration —
	// and would otherwise lint clean by vacuously passing every check.
	bad, err := db.LintDirectedProvenance(ctx, []string{"no_such_table"})
	require.NoError(t, err)
	require.Len(t, bad, 1)
	require.Contains(t, bad[0], "does not exist")
}

func TestLintDirectedProvenancePassesCompliantTable(t *testing.T) {
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, db.Migrate(ctx, tenantdb.Migrations()))
	require.NoError(t, db.WithAdmin(ctx, func(tx *gorm.DB) error {
		return tx.Exec(`
			CREATE TABLE good_provenance (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  origin_tenant_id uuid NOT NULL,
			  origin_record_id uuid NOT NULL
			)`).Error
	}))

	bad, err := db.LintDirectedProvenance(ctx, []string{"good_provenance"})
	require.NoError(t, err)
	require.Empty(t, bad)
}

func TestLintDirectedProvenanceIsNoOpWithNoDeclarations(t *testing.T) {
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)
	bad, err := db.LintDirectedProvenance(context.Background(), nil)
	require.NoError(t, err)
	require.Empty(t, bad)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd backend && go test ./pkg/tenantdb/ -run TestLintDirectedProvenance -v`
Expected: compile failure — `db.LintDirectedProvenance undefined`.

- [ ] **Step 3: Implement the lint**

In `backend/pkg/tenantdb/db.go`, add after `LintRLS`:

```go
// LintDirectedProvenance returns every declared directed-write table that
// cannot carry provenance, each as "<table>: <reason>".
//
// A table that accepts a write on behalf of another tenant must record
// WHICH tenant and WHICH record it came from, and must record them
// NOT NULL — a nullable provenance column is provenance that will
// eventually be NULL on the row someone needs during an audit (design D5).
//
// Separate from LintRLS rather than folded into it: LintRLS enumerates
// every table and subtracts an allowlist, whereas this checks only the
// tables modules declared, so the two have different inputs. It runs
// beside LintRLS at boot and in cmd/migrate.
func (d *DB) LintDirectedProvenance(ctx context.Context, tables []string) ([]string, error) {
	if len(tables) == 0 {
		return nil, nil
	}
	type row struct {
		Relname        string
		TableExists    bool
		OriginTenantOK bool
		OriginRecordOK bool
	}
	var rows []row
	err := d.admin.WithContext(ctx).Raw(`
		SELECT t.relname,
		       EXISTS (SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		               WHERE n.nspname = 'public' AND c.relname = t.relname
		                 AND c.relkind = 'r') AS table_exists,
		       EXISTS (SELECT 1 FROM information_schema.columns col
		               WHERE col.table_schema = 'public' AND col.table_name = t.relname
		                 AND col.column_name = 'origin_tenant_id'
		                 AND col.is_nullable = 'NO') AS origin_tenant_ok,
		       EXISTS (SELECT 1 FROM information_schema.columns col
		               WHERE col.table_schema = 'public' AND col.table_name = t.relname
		                 AND col.column_name = 'origin_record_id'
		                 AND col.is_nullable = 'NO') AS origin_record_ok
		  FROM unnest(string_to_array(?, ',')) AS t(relname)`,
		strings.Join(tables, ",")).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("lint directed provenance: %w", err)
	}

	var bad []string
	for _, r := range rows {
		switch {
		case !r.TableExists:
			bad = append(bad, r.Relname+": declared as a directed-write table but does not exist")
		case !r.OriginTenantOK:
			bad = append(bad, r.Relname+": origin_tenant_id is missing or nullable")
		case !r.OriginRecordOK:
			bad = append(bad, r.Relname+": origin_record_id is missing or nullable")
		}
	}
	return bad, nil
}
```

Ensure `strings` and `fmt` are imported in `db.go`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd backend && go test ./pkg/tenantdb/ -run TestLintDirectedProvenance -v`
Expected: PASS (all five).

- [ ] **Step 5: Add the module declaration**

In `backend/internal/platform/module.go`, inside `type Module interface`, after `DirectedSubjects()`:

```go
	// DirectedWriteTables declares every table this module's consumers
	// write into on behalf of ANOTHER tenant. Each must carry NOT NULL
	// origin_tenant_id and origin_record_id; DB.LintDirectedProvenance
	// enforces it at boot (design D5, #932).
	//
	// Declared rather than inferred: which table a handler writes into is
	// not statically knowable from a tx.Exec, so inference would silently
	// cover nothing the day a handler gained a second INSERT.
	//
	// A module with no cross-tenant writes returns nil.
	DirectedWriteTables() []string
```

In `backend/internal/platform/registry.go`, add:

```go
// DirectedWriteTables is the union of every registered module's
// declarations, for DB.LintDirectedProvenance.
func (r *Registry) DirectedWriteTables() []string {
	var out []string
	for _, m := range r.ordered {
		out = append(out, m.DirectedWriteTables()...)
	}
	return out
}
```

Add to each of `medicore`, `pharmacy`, `lab`, `iam`, `reference`:

```go
// DirectedWriteTables declares none: this module writes no table on
// behalf of another tenant.
func (m *Module) DirectedWriteTables() []string { return nil }
```

Update `fakeModule` in `registry_test.go` to implement it.

- [ ] **Step 6: Run the lint at boot**

In `backend/cmd/api/main.go`, directly after the existing `LintRLS` block (~line 211), add:

```go
	// Beside LintRLS and for the same reason: a control that only runs
	// when someone remembers to run it is not a control. registry is
	// already built at this point, so the declared set is the real one.
	if bad, err := db.LintDirectedProvenance(ctx, registry.DirectedWriteTables()); err != nil {
		return fmt.Errorf("lint directed provenance: %w", err)
	} else if len(bad) > 0 {
		return fmt.Errorf("directed-write tables cannot carry provenance: %v", bad)
	}
```

If `registry` is constructed *after* the `LintRLS` call in this file, move this new block to just after the `registry, err := bootstrap.NewRegistry(...)` line instead — it only needs `db` and `registry`.

Add the equivalent to `backend/cmd/migrate/main.go` beside its `LintRLS` call, using the same registry source that file already uses. If `cmd/migrate` has no registry, skip it there and note in the commit message that boot is the enforcement point.

- [ ] **Step 7: Build and run the full suite**

Run: `cd backend && go build ./... && go test -race ./internal/... ./pkg/...`
Expected: PASS.

- [ ] **Step 8: Prove the boot lint can fail**

Temporarily make `reference` return `[]string{"reference_pings"}` from `DirectedWriteTables()` (that table has no provenance columns), run `cd backend && go run ./cmd/api` against the dev stack, and confirm it refuses to boot with `origin_tenant_id is missing or nullable`. Revert.

- [ ] **Step 9: Commit**

```bash
git add backend/internal/platform backend/internal/modules backend/pkg/tenantdb backend/cmd
git commit -m "feat(932): lint provenance columns on declared directed-write tables"
```

---

### Task 5: Prove the wiring end to end in the reference module

`reference` exists to prove the platform stack end to end (issue #2). It gains a directed publish and a directed consumer so S4 inherits a primitive already exercised through the real path — outbox → dispatcher → JetStream → consumer → RLS-forced cross-tenant write.

**No HTTP route is added.** Every `reference` route is `authz.Public` because the module exposes no tenant data; a public endpoint that creates a row in a caller-named tenant would be a genuine hole. The proof is an integration test that publishes through the outbox inside a real `WithTenant` transaction, which exercises the identical path.

**Files:**
- Modify: `backend/internal/modules/reference/contract/events.go`
- Modify: `backend/internal/modules/reference/module.go`
- Modify: `backend/internal/modules/reference/consumers.go`
- Test: `backend/internal/modules/reference/module_test.go`

**Interfaces:**
- Consumes: everything from Tasks 1–4.
- Produces: `referencecontract.SubjectPingForwarded`, `referencecontract.PingForwardedData{PingID, Message string}`; table `reference_forwarded_pings`.

- [ ] **Step 1: Write the failing test**

Append to `backend/internal/modules/reference/module_test.go`, following the harness that file already uses to build a db + bus (mirror the existing test's setup exactly rather than inventing a new one):

```go
// TestForwardedPingLandsInDestinationTenant is the end-to-end proof of
// #932 through the real path: a business tx publishes to the outbox, the
// dispatcher delivers via JetStream, and the consumer writes an
// RLS-forced row into a DIFFERENT tenant, invisible to the publisher.
func TestForwardedPingLandsInDestinationTenant(t *testing.T) {
	// ... build db, bus, ctx exactly as the existing reference tests do ...
	bus.AllowDirected(referencecontract.SubjectPingForwarded)
	require.NoError(t, bus.StartConsumers(ctx, db, reference.New().Consumers(platform.Deps{})))
	go bus.RunDispatcher(ctx, db)

	origin, destination := uuid.NewString(), uuid.NewString()
	pingID := uuid.New()

	data, err := json.Marshal(referencecontract.PingForwardedData{
		PingID: pingID.String(), Message: "forwarded",
	})
	require.NoError(t, err)

	require.NoError(t, db.WithTenant(ctx, origin, func(tx *gorm.DB) error {
		return bus.Publish(tx, referencecontract.SubjectPingForwarded, events.Event{
			Type: "ReferencePingForwarded", Version: 1,
			TenantID: origin, DestinationTenantID: destination, Data: data,
		})
	}))

	countAs := func(tenant string) int {
		var n int
		require.NoError(t, db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT count(*) FROM reference_forwarded_pings`).Scan(&n).Error
		}))
		return n
	}

	require.Eventually(t, func() bool { return countAs(destination) == 1 },
		30*time.Second, 200*time.Millisecond)
	require.Equal(t, 0, countAs(origin),
		"the publishing tenant can read the row it forwarded")

	var originTenant, originRecord string
	require.NoError(t, db.WithTenant(ctx, destination, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT origin_tenant_id::text, origin_record_id::text
		               FROM reference_forwarded_pings`).Row().Scan(&originTenant, &originRecord)
	}))
	require.Equal(t, origin, originTenant)
	require.Equal(t, pingID.String(), originRecord)
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd backend && go test ./internal/modules/reference/ -run TestForwardedPing -v`
Expected: compile failure — `SubjectPingForwarded` undefined.

- [ ] **Step 3: Add the contract**

Append to `backend/internal/modules/reference/contract/events.go`:

```go
// SubjectPingForwarded is published when a ping is forwarded to another
// tenant. It is the reference module's proof of the directed
// cross-tenant path (#932): the consumer writes into the event's
// DESTINATION tenant, not its origin.
const SubjectPingForwarded = "helivanta.in.reference.ping_forwarded.v1"

// PingForwardedData is the v1 payload of SubjectPingForwarded.
//
// Purpose-limited by construction: it carries what the receiving tenant
// needs to act, and nothing else about the origin's ping.
type PingForwardedData struct {
	PingID  string `json:"ping_id"`
	Message string `json:"message"`
}
```

- [ ] **Step 4: Add the migration**

In `backend/internal/modules/reference/module.go`, append a third entry to the `Migrations()` slice (append-only — do not touch `0001` or `0002`):

```go
	}, {
		// #932. The proof table for directed cross-tenant writes.
		//
		// tenant_id is the DESTINATION — the tenant this row belongs to,
		// and the only one whose RLS context can read it. origin_* record
		// who disclosed it and which record it came from, NOT NULL so the
		// answer exists on every row (design D5); LintDirectedProvenance
		// fails the boot if either is dropped or made nullable.
		//
		// The policy is the ordinary shape: USING widens through
		// hms_tenant_visible, WITH CHECK stays pinned to strict equality.
		// A directed write needs NO policy exception — that it does not
		// is the evidence this is a copy, not a widening (design D6).
		ID: "0003_reference",
		SQL: `
			CREATE TABLE reference_forwarded_pings (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  origin_tenant_id uuid NOT NULL,
			  origin_record_id uuid NOT NULL,
			  message text NOT NULL,
			  created_at timestamptz NOT NULL DEFAULT now()
			);
			ALTER TABLE reference_forwarded_pings ENABLE ROW LEVEL SECURITY;
			ALTER TABLE reference_forwarded_pings FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON reference_forwarded_pings
			  USING (hms_tenant_visible(tenant_id))
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE INDEX ON reference_forwarded_pings (tenant_id, created_at DESC);
			CREATE INDEX ON reference_forwarded_pings (origin_tenant_id, origin_record_id);`,
	}}
```

- [ ] **Step 5: Declare the subject and the table**

In `backend/internal/modules/reference/module.go`, replace `Publishes`, `DirectedSubjects` and `DirectedWriteTables`:

```go
// Publishes declares the two events reference emits. reference consumes
// both of its own events — see Consumers in consumers.go.
func (m *Module) Publishes() []string {
	return []string{
		referencecontract.SubjectPinged,
		referencecontract.SubjectPingForwarded,
	}
}

// DirectedSubjects: ping_forwarded is the one reference event that
// creates data in a tenant other than the publisher's (#932).
func (m *Module) DirectedSubjects() []string {
	return []string{referencecontract.SubjectPingForwarded}
}

// DirectedWriteTables: the table reference-forwarded writes into on
// behalf of the destination tenant.
func (m *Module) DirectedWriteTables() []string {
	return []string{"reference_forwarded_pings"}
}
```

- [ ] **Step 6: Add the consumer**

In `backend/internal/modules/reference/consumers.go`, add a second entry to the returned slice:

```go
	}, {
		Name:    "reference-forwarded",
		Subject: referencecontract.SubjectPingForwarded,
		Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
			var d referencecontract.PingForwardedData
			if err := json.Unmarshal(evt.Data, &d); err != nil {
				return err
			}
			// The bus scoped this tx to evt.DestinationTenantID (#932),
			// so tenant_id must be the DESTINATION — using evt.TenantID
			// here would be rejected by WITH CHECK, loudly, which is the
			// intended failure mode.
			return tx.Exec(`INSERT INTO reference_forwarded_pings
				(tenant_id, origin_tenant_id, origin_record_id, message)
				VALUES (?, ?, ?, ?)`,
				evt.DestinationTenantID, evt.TenantID, d.PingID, d.Message).Error
		},
	}}
```

- [ ] **Step 7: Run the test to verify it passes**

Run: `cd backend && go test ./internal/modules/reference/ -run TestForwardedPing -v`
Expected: PASS.

- [ ] **Step 8: Prove the assertions can fail**

Change the consumer's `tenant_id` value from `evt.DestinationTenantID` to `evt.TenantID`, re-run, and confirm the test FAILS with an RLS `WITH CHECK` violation — proving the policy, not just the test, is doing the work. Revert.

Then drop `NOT NULL` from `origin_tenant_id` in the `0003_reference` migration on a fresh database and confirm boot fails with `origin_tenant_id is missing or nullable`. Revert.

- [ ] **Step 9: Run every gate**

```bash
cd backend
make -C .. lint-go
go test -race ./...
./scripts/coverage-gate.sh
```
Expected: all green. `TestEveryConsumedSubjectIsPublished` and `TestPublishesUsesContractConstants` must pass unchanged — they now cover the new subject too.

- [ ] **Step 10: Commit and open the PR**

```bash
git add backend/internal/modules/reference
git commit -m "feat(932): prove directed cross-tenant writes end to end in reference"
git push -u origin spec/932-directed-cross-tenant-events
```

PR body must include `Closes #932`, and must state what the slice does not cover — no prescription semantics, no directory, no holds, no patient identity, and **no destination-existence validation** (deferred to #13; a directed write to an unknown tenant creates a row visible to nobody, which is safe but silent, and must be closed before S4 reaches production).

---

## Self-Review

**Spec coverage:** D1 → Task 1 Step 3. D2 → Task 2 Step 3. D3 → unchanged by design; pinned by `TestUndirectedEventIsUnaffected` and by Task 2's `WithTenant(origin)` publish, which would fail the `WITH CHECK` if the outbox row were stamped with the destination. D4 → Tasks 1 and 3 (runtime + boot). D5 → Task 4. D6 → no policy is altered anywhere in this plan; Task 5 Step 4's migration uses the ordinary shape, and Task 5 Step 8 proves `WITH CHECK` rejects a mis-scoped write. D7 → Task 2 Steps 1 and 4. D8 → Task 5.

**Testing section coverage:** items 1–7 of the spec's testing list map to Task 2 Step 1, Task 3 Step 8, Task 1 Step 1, Task 5 Step 8, Task 4 Step 1, Task 2 Step 1, and Task 1 Step 1 respectively.

**Type consistency:** `DestinationTenantID` (field), `AllowDirected` / `directedAllowed` / `scopeTenant` (bus), `DirectedSubjects` / `DirectedWriteTables` (module + registry), `LintDirectedProvenance` (tenantdb), `SubjectPingForwarded` / `PingForwardedData` (contract), `reference_forwarded_pings` (table) — each spelled identically at every appearance.

**Known plan-time uncertainty, to resolve during execution rather than guess at now:** the exact construction order of `db`, `registry` and `bus` in `cmd/api/main.go` (Task 3 Step 6 and Task 4 Step 6 each say where to move the call if the order differs), and whether `cmd/migrate` has a registry to lint against (Task 4 Step 6 gives the fallback).

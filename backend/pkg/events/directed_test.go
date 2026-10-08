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
// directed subjects never reaching the constructor in main.go — safe
// rather than silently permissive.
func TestDirectedEventOnUndeclaredSubjectIsRefused(t *testing.T) {
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)
	require.NoError(t, db.Migrate(context.Background(),
		append(tenantdb.Migrations(), events.Migrations()...)))

	natsURL := testinfra.StartNATS(t)
	bus, err := events.NewBusInNamespace(natsURL, testinfra.IsolationKey(t))
	require.NoError(t, err)
	defer bus.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Deliberately no directed subjects passed to the constructor.
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
	bus, err := events.NewBusInNamespace(natsURL, testinfra.IsolationKey(t))
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

// directedProbeMigration is created per test rather than reusing a
// module's table: pkg/events must not import internal/modules. It is a
// tenantdb.Migration — not a bare db.WithAdmin(...) exec — because
// WithAdmin bypasses RLS entirely and internal/archtest's
// TestWithAdminIsOnlyCalledFromTheAllowlist forbids calling it from
// outside pkg/tenantdb (the allowlist is deliberately empty). Migrate
// runs this DDL on the admin/owner pool internally, which is exactly why
// pkg/tenantdb/ is the allowed directory: this file gets the DDL it
// needs without pkg/events ever referencing WithAdmin itself.
var directedProbeMigration = tenantdb.Migration{
	ID: "0005_directed_probe_test_only",
	SQL: `
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
  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);`,
}

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
	migs := append(tenantdb.Migrations(), events.Migrations()...)
	migs = append(migs, directedProbeMigration)
	require.NoError(t, db.Migrate(ctx, migs))

	natsURL := testinfra.StartNATS(t)
	const subject = "helivanta.in.reference.pinged.v1"
	bus, err := events.NewBusInNamespace(natsURL, testinfra.IsolationKey(t), subject)
	require.NoError(t, err)
	defer bus.Close()

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
	const subject = "helivanta.in.reference.pinged.v1"
	bus, err := events.NewBusInNamespace(natsURL, testinfra.IsolationKey(t), subject)
	require.NoError(t, err)
	defer bus.Close()

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

// TestDirectedInsertWithoutProvenanceIsRejectedByTheDatabase is spec
// Testing item 4, and it is a different claim from the four
// LintDirectedProvenance tests: those assert the SCHEMA LINT notices a
// nullable or missing column at boot. This asserts the running database
// refuses the row — so a handler that forgets a provenance value gets an
// error from Postgres, not a row with a NULL origin that nobody notices
// until an audit years later. The rejection must come from the database
// and not from any Go-side check, which is why the INSERT here is raw
// SQL issued inside the destination tenant's own transaction.
func TestDirectedInsertWithoutProvenanceIsRejectedByTheDatabase(t *testing.T) {
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	migs := append(tenantdb.Migrations(), events.Migrations()...)
	migs = append(migs, directedProbeMigration)
	require.NoError(t, db.Migrate(ctx, migs))

	destination, origin := uuid.NewString(), uuid.NewString()

	// Control: the complete row is accepted, so the failures below are
	// the missing provenance and not the fixture being broken.
	require.NoError(t, db.WithTenant(ctx, destination, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO directed_probe (tenant_id, origin_tenant_id, origin_record_id, note)
			VALUES (?, ?, ?, 'complete')`, destination, origin, uuid.NewString()).Error
	}))

	for _, tc := range []struct {
		name string
		sql  string
		args []any
	}{{
		name: "origin_tenant_id omitted",
		sql: `INSERT INTO directed_probe (tenant_id, origin_record_id, note)
		      VALUES (?, ?, 'no origin tenant')`,
		args: []any{destination, uuid.NewString()},
	}, {
		name: "origin_record_id omitted",
		sql: `INSERT INTO directed_probe (tenant_id, origin_tenant_id, note)
		      VALUES (?, ?, 'no origin record')`,
		args: []any{destination, origin},
	}} {
		t.Run(tc.name, func(t *testing.T) {
			err := db.WithTenant(ctx, destination, func(tx *gorm.DB) error {
				return tx.Exec(tc.sql, tc.args...).Error
			})
			require.Error(t, err, "the database accepted a directed row with no provenance")
			require.Contains(t, err.Error(), "null value in column",
				"the rejection must be the NOT NULL constraint, not some other failure")
		})
	}

	// And nothing partial landed: the control row is the only one.
	var n int
	require.NoError(t, db.WithTenant(ctx, destination, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM directed_probe`).Scan(&n).Error
	}))
	require.Equal(t, 1, n)
}

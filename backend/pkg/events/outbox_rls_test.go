package events

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// package events (white-box, not events_test): TestDispatcherPublishesEveryTenant
// and TestTenantlessEventStillPublishes call drainOnce directly for a single,
// deterministic drain pass — the design this file pins rests on exactly one
// tick of the dispatcher doing the right thing, not on a background loop
// eventually getting there.

// setUpOutboxHarness boots Postgres+NATS, migrates the platform predicate
// (hms_tenant_visible) and the outbox (0001_events_outbox +
// 0002_events_outbox_tenant, #835 Task 1), and returns a Bus in its own
// subject namespace.
func setUpOutboxHarness(t *testing.T) (*tenantdb.DB, *Bus, string) {
	t.Helper()
	appDSN, adminDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, adminDSN)
	require.NoError(t, err)
	// tenantdb.Migrations() must run first: outbox_events' policy
	// (0002_events_outbox_tenant) calls hms_tenant_visible, which only
	// tenantdb.Migrations() defines.
	require.NoError(t, db.Migrate(context.Background(), append(tenantdb.Migrations(), Migrations()...)))

	natsURL := testinfra.StartNATS(t)
	bus, err := NewBusInNamespace(natsURL, t.Name())
	require.NoError(t, err)
	t.Cleanup(bus.Close)

	return db, bus, natsURL
}

// subscribeRaw opens a plain core-NATS subscription on subject, resolved
// through bus.Subject so it lines up with the namespaced subject the bus
// actually publishes on. Subscribing directly, rather than through
// StartConsumers, keeps these tests independent of consumer/idempotency
// machinery this file isn't about.
func subscribeRaw(t *testing.T, natsURL string, bus *Bus, subject string) *nats.Subscription {
	t.Helper()
	nc, err := nats.Connect(natsURL)
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	sub, err := nc.SubscribeSync(bus.Subject(subject))
	require.NoError(t, err)
	t.Cleanup(func() { _ = sub.Unsubscribe() })
	return sub
}

// TestDispatcherPublishesEveryTenant is the test the whole design rests on
// (spec D1, plan trap 1). drainOnce reads the outbox across every tenant by
// construction — that is its entire job — and it can only do that from
// WithAdmin. Reverting drainOnce to WithSystem must make this test observe
// ZERO published events, not an error: that silence, on a green suite
// otherwise, is exactly the hazard D1 exists to close.
func TestDispatcherPublishesEveryTenant(t *testing.T) {
	db, bus, natsURL := setUpOutboxHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tenantA, tenantB := uuid.NewString(), uuid.NewString()
	const subject = "hms.in.test.dispatchall.v1"
	sub := subscribeRaw(t, natsURL, bus, subject)

	require.NoError(t, db.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
		return bus.Publish(tx, subject, Event{Type: "DispatchAllTest", Version: 1, TenantID: tenantA, Data: json.RawMessage(`{}`)})
	}))
	require.NoError(t, db.WithTenant(ctx, tenantB, func(tx *gorm.DB) error {
		return bus.Publish(tx, subject, Event{Type: "DispatchAllTest", Version: 1, TenantID: tenantB, Data: json.RawMessage(`{}`)})
	}))

	// ONE drain pass — not a loop, not require.Eventually. If this drains
	// zero rows because it silently can't see them, that failure must show
	// up here, not be masked by retries.
	require.NoError(t, bus.drainOnce(ctx, db))

	seenTenants := map[string]bool{}
	for range 2 {
		msg, err := sub.NextMsg(10 * time.Second)
		require.NoError(t, err, "expected both tenants' events to reach the stream from one drain pass")
		var evt Event
		require.NoError(t, json.Unmarshal(msg.Data, &evt))
		seenTenants[evt.TenantID] = true
	}
	require.True(t, seenTenants[tenantA], "tenant A's event must have been published")
	require.True(t, seenTenants[tenantB], "tenant B's event must have been published")

	_, err := sub.NextMsg(500 * time.Millisecond)
	require.Error(t, err, "expected exactly the two published events, no more")
}

// TestTenantlessEventStillPublishes is trap 2: SubjectCredentialRevoked
// (iam/signout.go) publishes with no TenantID at all, because revocation
// is subject-scoped and ends every session for a subject in every tenant.
// If the outbox policy's WITH CHECK does not explicitly permit a NULL
// tenant_id, that INSERT is rejected inside the sign-out transaction and
// sign-out 500s. Both the real shape (WithSystem, no GUC — what
// iam/signout.go actually does) and the more general shape (WithTenant, a
// GUC set to some real tenant, but the event itself still carries none)
// are covered: a symmetric policy would reject both identically, since
// `NULL = anything` is NULL regardless of what the GUC holds.
func TestTenantlessEventStillPublishes(t *testing.T) {
	db, bus, natsURL := setUpOutboxHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	const subject = "hms.in.test.tenantless.v1"
	sub := subscribeRaw(t, natsURL, bus, subject)

	// The real shape: iam/signout.go's revoke() publishes from inside
	// WithSystem, with no TenantID.
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		return bus.Publish(tx, subject, Event{Type: "TenantlessTest", Version: 1, Data: json.RawMessage(`{}`)})
	}), "a tenant-less event must still insert from inside WithSystem, exactly as sign-out needs")

	// The more general shape: a transaction scoped to a real tenant GUC,
	// still publishing an event that itself carries none.
	tenantA := uuid.NewString()
	require.NoError(t, db.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
		return bus.Publish(tx, subject, Event{Type: "TenantlessTest", Version: 1, Data: json.RawMessage(`{}`)})
	}), "a tenant-less event must still insert from inside a tenant-scoped transaction")

	require.NoError(t, bus.drainOnce(ctx, db))

	for range 2 {
		_, err := sub.NextMsg(10 * time.Second)
		require.NoError(t, err, "both tenant-less events must still reach the stream")
	}
}

// TestOutboxRowIsInvisibleToAnotherTenant proves the ordinary tenant
// isolation case: tenant B must not be able to read tenant A's outbox row,
// the same guarantee every other tenant table gets.
func TestOutboxRowIsInvisibleToAnotherTenant(t *testing.T) {
	db, bus, _ := setUpOutboxHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tenantA, tenantB := uuid.NewString(), uuid.NewString()
	const subject = "hms.in.test.crosstenant.v1"

	require.NoError(t, db.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
		return bus.Publish(tx, subject, Event{Type: "CrossTenantTest", Version: 1, TenantID: tenantA, Data: json.RawMessage(`{}`)})
	}))

	var nA int64
	require.NoError(t, db.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM outbox_events WHERE subject = ?`, subject).Scan(&nA).Error
	}))
	require.Equal(t, int64(1), nA, "sanity: the row exists and its own tenant can see it")

	var nB int64
	require.NoError(t, db.WithTenant(ctx, tenantB, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM outbox_events WHERE subject = ?`, subject).Scan(&nB).Error
	}))
	require.Zero(t, nB, "tenant B must not see tenant A's outbox row")
}

// TestOutboxRowIsInvisibleUnderWithSystem is the reader the allowlist
// comment on outbox_events warned about (pkg/tenantdb/db.go, before Task 1):
// "readable by any WithSystem transaction". A module handler reaching for
// WithSystem — the ordinary path for a platform table with no tenant_id,
// which outbox_events used to be — must no longer be able to browse
// another tenant's payloads now that it has one.
func TestOutboxRowIsInvisibleUnderWithSystem(t *testing.T) {
	db, bus, _ := setUpOutboxHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tenantA := uuid.NewString()
	const subject = "hms.in.test.systemreadprobe.v1"

	require.NoError(t, db.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
		return bus.Publish(tx, subject, Event{Type: "SystemReadProbeTest", Version: 1, TenantID: tenantA, Data: json.RawMessage(`{}`)})
	}))

	var nTenant int64
	require.NoError(t, db.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM outbox_events WHERE subject = ?`, subject).Scan(&nTenant).Error
	}))
	require.Equal(t, int64(1), nTenant, "sanity: the row exists and its own tenant can see it")

	var nSystem int64
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM outbox_events WHERE subject = ?`, subject).Scan(&nSystem).Error
	}))
	require.Zero(t, nSystem, "a WithSystem reader must not see another tenant's outbox row")
}

// TestTenantlessRowIsReadableByNobody is D1a's strictest case: a
// tenant-less row (NULL tenant_id) must be invisible to every reader
// EXCEPT the dispatcher's WithAdmin — not WithSystem, not any tenant's
// WithTenant. hms_tenant_visible(NULL) evaluates NULL, not true, so this
// falls out of the same USING clause as the ordinary case, but it is worth
// pinning on its own: it is the strictest, not the loosest, outcome for a
// platform-wide event that belongs to no single tenant.
func TestTenantlessRowIsReadableByNobody(t *testing.T) {
	db, bus, natsURL := setUpOutboxHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tenantA := uuid.NewString()
	const subject = "hms.in.test.tenantlessreadprobe.v1"
	sub := subscribeRaw(t, natsURL, bus, subject)

	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		return bus.Publish(tx, subject, Event{Type: "TenantlessReadProbeTest", Version: 1, Data: json.RawMessage(`{}`)})
	}))

	// Prove the row really exists — not merely "no row was ever written,
	// so of course nothing reads it" — by watching the ONE reader design
	// spec D1 says is still allowed to see it: the dispatcher, via
	// WithAdmin.
	require.NoError(t, bus.drainOnce(ctx, db))
	_, err := sub.NextMsg(10 * time.Second)
	require.NoError(t, err, "sanity: the tenant-less row exists and the dispatcher can see and publish it")

	var nSystem int64
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM outbox_events WHERE subject = ?`, subject).Scan(&nSystem).Error
	}))
	require.Zero(t, nSystem, "a tenant-less row must not be visible under WithSystem")

	var nTenant int64
	require.NoError(t, db.WithTenant(ctx, tenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM outbox_events WHERE subject = ?`, subject).Scan(&nTenant).Error
	}))
	require.Zero(t, nTenant, "a tenant-less row must not be visible under any tenant's WithTenant either")
}

// TestCrossTenantWriteIsRejectedNotMalformed pins the KIND of failure, not
// merely that one happens.
//
// The WITH CHECK clause cannot call hms_tenant_visible — LintRLS fails that
// shape, because reads may widen to hospital groups later while writes must
// stay pinned to exactly one tenant — so it calls current_setting directly and
// did not inherit 0002_platform_rls's NULLIF fix.
//
// That mattered because an "unset" custom GUC is only NULL the first time a
// session ever references it. After any WithTenant transaction commits, the
// name reverts to '' for the rest of that pooled connection's life, and
// ''::uuid is a hard type error rather than a non-match. So the SAME cross-
// tenant write reported "new row violates row-level security policy" on a
// fresh connection and "invalid input syntax for type uuid" on a reused one —
// which of the two a caller saw depended on pool scheduling.
//
// A security control that reports a type error for a policy violation is worse
// than one that merely rejects: nothing downstream can tell tenancy enforcement
// from a malformed value, and the operator reading the log has no reason to
// suspect tenancy at all. This asserts the rejection is an RLS rejection.
func TestCrossTenantWriteIsRejectedNotMalformed(t *testing.T) {
	db, _, _ := setUpOutboxHarness(t)
	ctx := context.Background()

	tenantA := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	tenantB := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	// Dirty this pooled connection exactly as ordinary traffic does: any
	// committed WithTenant transaction leaves app.tenant_id as '' behind it.
	require.NoError(t, db.WithTenant(ctx, tenantA.String(), func(tx *gorm.DB) error {
		return tx.Exec(`SELECT 1`).Error
	}))

	// Now attempt the write UNDER WithSystem — no GUC of its own, so the
	// policy sees whatever the pooled connection was left holding. This is
	// the shape that actually reaches the bare cast: inside WithTenant the
	// GUC is set to a real uuid and the clause never sees ''. Publishing a
	// tenant-carrying event under WithSystem is exactly what several module
	// tests did before Task 1.
	err := db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO outbox_events (id, subject, payload, tenant_id)
			VALUES (?, 'probe', '{}'::jsonb, ?)`, uuid.New(), tenantB).Error
	})

	require.Error(t, err, "writing another tenant's row must be refused")
	require.Contains(t, err.Error(), "row-level security",
		"the refusal must be an RLS rejection")
	require.NotContains(t, err.Error(), "invalid input syntax",
		"a policy violation must never surface as a uuid cast error: that is the "+
			"pooled-connection defect 0003_events_outbox_tenant_check fixes, and it "+
			"makes enforcement indistinguishable from malformed input")
}

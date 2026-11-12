package workforce_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/modules/workforce"                            //nolint:depguard // module under test
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract" //nolint:depguard // published contract under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func (h *establishmentHarness) marker(t *testing.T, id string) (announced int64, attempts int, next *time.Time) {
	t.Helper()
	var row struct {
		AnnouncedVersion int64
		Attempts         int
		NextAttemptAt    *time.Time
	}
	require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT announced_version,attempts,next_attempt_at FROM workforce_hospital_name_announcements WHERE tenant_id=? AND hospital_id=?`, testutil.TenantA, id).Scan(&row).Error
	}))
	return row.AnnouncedVersion, row.Attempts, row.NextAttemptAt
}

func receiptConsumer(t *testing.T, h *establishmentHarness) events.Consumer {
	t.Helper()
	for _, c := range h.module.Consumers(platform.Deps{}) {
		if c.Name == "workforce-hospital-name-receipt" {
			return c
		}
	}
	require.FailNow(t, "workforce-hospital-name-receipt consumer is not registered")
	return events.Consumer{}
}

func (h *establishmentHarness) receiptEvent(t *testing.T, id string, version int64) events.Event {
	t.Helper()
	data, err := json.Marshal(pharmacycontract.HospitalNameReceivedData{HospitalID: id, NameVersion: version})
	require.NoError(t, err)
	s := pharmacycontract.SubjectHospitalNameReceived
	return events.Event{Type: s, Version: 1, TenantID: testutil.TenantA, Data: data}
}

func (h *establishmentHarness) deliver(t *testing.T, evt events.Event) error {
	t.Helper()
	c := receiptConsumer(t, h)
	return h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error { return c.Handle(t.Context(), tx, evt) })
}

func (h *establishmentHarness) receipt(t *testing.T, id string, version int64) error {
	t.Helper()
	return h.deliver(t, h.receiptEvent(t, id, version))
}

func announcedHarness(t *testing.T) *establishmentHarness {
	t.Helper()
	h := newEstablishmentRouter(t, workforce.New())
	h.rename(t, "Alpha Hospital")
	require.NoError(t, workforce.AnnounceHospitalNamesOnce(t.Context(), h.db))
	return h
}

func TestClaimStartsUnconfirmed(t *testing.T) {
	h := announcedHarness(t)
	announced, attempts, next := h.marker(t, h.hospital)
	require.EqualValues(t, 1, announced)
	require.Zero(t, attempts)
	require.NotNil(t, next)
	require.WithinDuration(t, time.Now().Add(2*time.Minute), *next, 10*time.Second)
}

func TestReceiptClearsTheAnnouncedVersion(t *testing.T) {
	h := announcedHarness(t)
	require.NoError(t, h.receipt(t, h.hospital, 1))
	_, _, next := h.marker(t, h.hospital)
	require.Nil(t, next)
}

func TestReceiptForANewerVersionClears(t *testing.T) {
	h := announcedHarness(t)
	require.NoError(t, h.receipt(t, h.hospital, 5))
	_, _, next := h.marker(t, h.hospital)
	require.Nil(t, next)
}

func TestReceiptForAnOlderVersionKeepsResending(t *testing.T) {
	h := announcedHarness(t)
	h.rename(t, "Beta Hospital")
	require.NoError(t, workforce.AnnounceHospitalNamesOnce(t.Context(), h.db))
	require.NoError(t, h.receipt(t, h.hospital, 1))
	announced, _, next := h.marker(t, h.hospital)
	require.EqualValues(t, 2, announced)
	require.NotNil(t, next)
}

func TestReceiptForAnUnknownHospitalIsANoOp(t *testing.T) {
	h := announcedHarness(t)
	other := uuid.NewString()
	require.NoError(t, h.receipt(t, other, 1))
	announced, _, _ := h.marker(t, other)
	require.Zero(t, announced, "no row is created")
}

func TestInvalidReceiptIsRejected(t *testing.T) {
	h := announcedHarness(t)
	for name, mutate := range map[string]func(*events.Event){
		"wrong type":  func(e *events.Event) { e.Type = "other" },
		"version 2":   func(e *events.Event) { e.Version = 2 },
		"destination": func(e *events.Event) { e.DestinationTenantID = testutil.TenantA },
		"not a uuid":  func(e *events.Event) { e.Data = json.RawMessage(`{"hospital_id":"x","name_version":1}`) },
		"nil uuid": func(e *events.Event) {
			e.Data = json.RawMessage(`{"hospital_id":"` + uuid.Nil.String() + `","name_version":1}`)
		},
		"version zero": func(e *events.Event) {
			e.Data = json.RawMessage(`{"hospital_id":"` + h.hospital + `","name_version":0}`)
		},
		"malformed data": func(e *events.Event) { e.Data = json.RawMessage(`[`) },
	} {
		t.Run(name, func(t *testing.T) {
			evt := h.receiptEvent(t, h.hospital, 1)
			mutate(&evt)
			require.Error(t, h.deliver(t, evt))
		})
	}
	_, _, next := h.marker(t, h.hospital)
	require.NotNil(t, next, "a rejected receipt confirms nothing")
}

type workforceBeforeReceipts struct{ *workforce.Module }

func (m *workforceBeforeReceipts) Migrations() []tenantdb.Migration {
	var before []tenantdb.Migration
	for _, migration := range m.Module.Migrations() {
		if migration.ID == "0101_workforce_hospital_name_receipts" {
			break
		}
		before = append(before, migration)
	}
	return before
}

func TestMigrationMakesExistingMarkersDue(t *testing.T) {
	h := newEstablishmentRouter(t, &workforceBeforeReceipts{workforce.New()})
	h.rename(t, "Alpha Hospital")
	// A marker as the pre-0101 announcer wrote it: the new announcer's claim needs the new columns.
	require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO workforce_hospital_name_announcements(tenant_id,hospital_id,announced_version) VALUES(?,?,1)`, testutil.TenantA, h.hospital).Error
	}))
	var real []tenantdb.Migration
	for _, m := range workforce.New().Migrations() {
		if m.ID == "0101_workforce_hospital_name_receipts" {
			real = append(real, m)
		}
	}
	require.Len(t, real, 1)
	require.NoError(t, h.db.Migrate(t.Context(), real))
	_, attempts, next := h.marker(t, h.hospital)
	require.Zero(t, attempts)
	require.NotNil(t, next)
	require.False(t, next.After(time.Now()), "an existing marker is due immediately")
	var index string
	var granted bool
	require.NoError(t, h.db.WithAdmin(t.Context(), func(tx *gorm.DB) error {
		if err := tx.Raw(`SELECT indexdef FROM pg_indexes WHERE tablename='workforce_hospital_name_announcements' AND indexname='workforce_hospital_name_announcements_due'`).Scan(&index).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT has_column_privilege('helivanta_system','workforce_hospital_name_announcements','next_attempt_at','SELECT')`).Scan(&granted).Error
	}))
	require.Contains(t, index, "WHERE (next_attempt_at IS NOT NULL)", "the scan's partial due index exists")
	require.True(t, granted, "the cross-tenant scan can read next_attempt_at")
}

func (h *establishmentHarness) makeDue(t *testing.T, id string) {
	t.Helper()
	require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE workforce_hospital_name_announcements SET next_attempt_at=now()-interval '1 second' WHERE tenant_id=? AND hospital_id=?`, testutil.TenantA, id).Error
	}))
}

func (h *establishmentHarness) pass(t *testing.T) {
	t.Helper()
	require.NoError(t, workforce.AnnounceHospitalNamesOnce(t.Context(), h.db))
}

func TestDueUnconfirmedNameIsResent(t *testing.T) {
	h := announcedHarness(t)
	h.makeDue(t, h.hospital)
	h.pass(t)
	sent := workforcecontract.HospitalNamedData{HospitalID: h.hospital, Name: "Alpha Hospital", NameVersion: 1}
	require.Equal(t, []workforcecontract.HospitalNamedData{sent, sent}, h.announced(t, h.hospital))
	_, attempts, next := h.marker(t, h.hospital)
	require.Equal(t, 1, attempts)
	require.NotNil(t, next)
	require.WithinDuration(t, time.Now().Add(2*time.Minute), *next, 10*time.Second)

	h.makeDue(t, h.hospital)
	h.pass(t)
	require.Len(t, h.announced(t, h.hospital), 3)
	_, attempts, next = h.marker(t, h.hospital)
	require.Equal(t, 2, attempts)
	require.NotNil(t, next)
	require.WithinDuration(t, time.Now().Add(4*time.Minute), *next, 10*time.Second, "the delay doubles")
}

func TestConfirmedNameIsNotResent(t *testing.T) {
	h := announcedHarness(t)
	require.NoError(t, h.receipt(t, h.hospital, 1))
	require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE workforce_hospital_name_announcements SET updated_at=now()-interval '1 hour' WHERE tenant_id=? AND hospital_id=?`, testutil.TenantA, h.hospital).Error
	}))
	h.pass(t)
	require.Len(t, h.announced(t, h.hospital), 1, "a confirmed name is never due")
}

func TestNotYetDueNameIsNotResent(t *testing.T) {
	h := announcedHarness(t)
	h.pass(t)
	require.Len(t, h.announced(t, h.hospital), 1)
}

func TestRenameSupersedesAnUnconfirmedVersion(t *testing.T) {
	h := announcedHarness(t)
	h.rename(t, "Beta Hospital")
	h.makeDue(t, h.hospital)
	h.pass(t)
	v1 := workforcecontract.HospitalNamedData{HospitalID: h.hospital, Name: "Alpha Hospital", NameVersion: 1}
	v2 := workforcecontract.HospitalNamedData{HospitalID: h.hospital, Name: "Beta Hospital", NameVersion: 2}
	require.Equal(t, []workforcecontract.HospitalNamedData{v1, v2}, h.announced(t, h.hospital))
	announced, attempts, _ := h.marker(t, h.hospital)
	require.EqualValues(t, 2, announced)
	require.Zero(t, attempts, "the claim starts the new version afresh")

	h.makeDue(t, h.hospital)
	h.pass(t)
	require.Equal(t, []workforcecontract.HospitalNamedData{v1, v2, v2}, h.announced(t, h.hospital), "v1 is never re-sent")
}

// Both passes scan the marker as due, then queue on the hospital lock; only
// the locked re-check stops the second from re-sending what the first did.
func TestResendRacingPassesSendOnce(t *testing.T) {
	h := announcedHarness(t)
	h.makeDue(t, h.hospital)
	done := make(chan error, 2)
	require.NoError(t, h.tx(t, h.hospital, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT 1 FROM workforce_hospitals WHERE tenant_id=? AND id=? FOR UPDATE`, testutil.TenantA, h.hospital).Error; err != nil {
			return err
		}
		for range 2 {
			go func() { done <- workforce.AnnounceHospitalNamesOnce(t.Context(), h.db) }()
		}
		waitForLockWaiters(t, h, 2)
		return nil
	}))
	require.NoError(t, <-done)
	require.NoError(t, <-done)
	require.Len(t, h.announced(t, h.hospital), 2, "two replicas re-sent the same due version")
	_, attempts, _ := h.marker(t, h.hospital)
	require.Equal(t, 1, attempts)
}

func TestFirstAnnouncementsComeBeforeResends(t *testing.T) {
	insert := func(t *testing.T, h *establishmentHarness) string {
		id := uuid.Must(uuid.NewV7()).String()
		require.NoError(t, h.tx(t, id, func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO workforce_hospitals(tenant_id,id,name) VALUES(?,?,'Other Hospital')`, testutil.TenantA, id).Error
		}))
		return id
	}
	t.Run("new hospital", func(t *testing.T) {
		h := announcedHarness(t)
		h.makeDue(t, h.hospital)
		fresh := insert(t, h)
		require.NoError(t, workforce.AnnounceHospitalNames(t.Context(), h.db, 1))
		require.Len(t, h.announced(t, fresh), 1)
		require.Len(t, h.announced(t, h.hospital), 1, "the due re-send waits")
	})
	t.Run("renamed hospital not yet due", func(t *testing.T) {
		// The renamed hospital's marker is due later than the other one's, so
		// only the unannounced-first ordering puts it ahead.
		h := newEstablishmentRouter(t, workforce.New())
		h.rename(t, "Alpha Hospital")
		due := insert(t, h)
		h.pass(t)
		h.makeDue(t, due)
		h.rename(t, "Beta Hospital")
		require.NoError(t, workforce.AnnounceHospitalNames(t.Context(), h.db, 1))
		require.Len(t, h.announced(t, h.hospital), 2)
		require.Len(t, h.announced(t, due), 1, "the due re-send waits")
	})
}

// waitForLockWaiters returns once n sessions in this test's database wait on a lock.
func waitForLockWaiters(t *testing.T, h *establishmentHarness, n int64) {
	t.Helper()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		var waiting int64
		assert.NoError(c, h.db.WithTenant(t.Context(), testutil.TenantA, func(q *gorm.DB) error {
			return q.Raw(`SELECT count(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE NOT l.granted AND a.datname=current_database()`).Scan(&waiting).Error
		}))
		assert.GreaterOrEqual(c, waiting, n)
	}, 10*time.Second, 10*time.Millisecond)
}

// A confirmation that commits after the pass's scan, while the pass waits on
// the hospital lock, must stop that pass's re-send: the locked re-check sees it.
func TestConfirmationDuringAPassStopsItsResend(t *testing.T) {
	h := announcedHarness(t)
	h.makeDue(t, h.hospital)
	done := make(chan error, 1)
	require.NoError(t, h.tx(t, h.hospital, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT 1 FROM workforce_hospitals WHERE tenant_id=? AND id=? FOR UPDATE`, testutil.TenantA, h.hospital).Error; err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE workforce_hospital_name_announcements SET next_attempt_at=NULL WHERE tenant_id=? AND hospital_id=?`, testutil.TenantA, h.hospital).Error; err != nil {
			return err
		}
		go func() { done <- workforce.AnnounceHospitalNamesOnce(t.Context(), h.db) }()
		// The pass has scanned (it saw the marker due) once it waits on our lock.
		waitForLockWaiters(t, h, 1)
		return nil
	}))
	require.NoError(t, <-done)
	require.Len(t, h.announced(t, h.hospital), 1, "a name confirmed during the pass is not re-sent")
}

// captureLogs sends slog to a buffer for the rest of the test.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

// A re-send whose transaction fails at commit must not be logged as sent.
func TestRolledBackResendIsNotLogged(t *testing.T) {
	h := announcedHarness(t)
	h.makeDue(t, h.hospital)
	// A deferred constraint trigger fails the commit after the re-send's update and enqueue ran.
	require.NoError(t, h.db.WithAdmin(t.Context(), func(tx *gorm.DB) error {
		return tx.Exec(`CREATE FUNCTION test_fail_commit() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RAISE EXCEPTION 'commit refused'; END$$;
CREATE CONSTRAINT TRIGGER test_fail_commit AFTER UPDATE ON workforce_hospital_name_announcements DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION test_fail_commit()`).Error
	}))
	t.Cleanup(func() {
		_ = h.db.WithAdmin(context.Background(), func(tx *gorm.DB) error {
			return tx.Exec(`DROP TRIGGER test_fail_commit ON workforce_hospital_name_announcements; DROP FUNCTION test_fail_commit()`).Error
		})
	})
	logs := captureLogs(t)
	h.pass(t)
	require.Contains(t, logs.String(), "retrying later")
	require.NotContains(t, logs.String(), "re-sent unconfirmed hospital name", "a rolled-back re-send was logged")
	_, attempts, _ := h.marker(t, h.hospital)
	require.Zero(t, attempts)
	require.Len(t, h.announced(t, h.hospital), 1)
}

func TestCancelledScanNamesItsStep(t *testing.T) {
	h := announcedHarness(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := workforce.AnnounceHospitalNamesOnce(ctx, h.db)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorContains(t, err, "scan due hospital names")
}

package workforce_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
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
		if migration.ID == "0099_workforce_hospital_name_receipts" {
			break
		}
		before = append(before, migration)
	}
	return before
}

func TestMigrationMakesExistingMarkersDue(t *testing.T) {
	h := newEstablishmentRouter(t, &workforceBeforeReceipts{workforce.New()})
	h.rename(t, "Alpha Hospital")
	// A marker as the pre-0099 announcer wrote it: the new announcer's claim needs the new columns.
	require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO workforce_hospital_name_announcements(tenant_id,hospital_id,announced_version) VALUES(?,?,1)`, testutil.TenantA, h.hospital).Error
	}))
	var real []tenantdb.Migration
	for _, m := range workforce.New().Migrations() {
		if m.ID == "0099_workforce_hospital_name_receipts" {
			real = append(real, m)
		}
	}
	require.Len(t, real, 1)
	require.NoError(t, h.db.Migrate(t.Context(), real))
	_, attempts, next := h.marker(t, h.hospital)
	require.Zero(t, attempts)
	require.NotNil(t, next)
	require.False(t, next.After(time.Now()), "an existing marker is due immediately")
}

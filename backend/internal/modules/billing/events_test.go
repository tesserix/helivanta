package billing_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	billingcontract "github.com/tesserix/helivanta/internal/modules/billing/contract"
	"github.com/tesserix/helivanta/internal/testutil"
)

// published reads the outbox for subject events whose data key is id. Each
// payload must decode strictly into T and carry exactly T's fields, so the
// triggers and the contract cannot drift apart.
func published[T any](t *testing.T, w *bedWorld, subject, key, id string) []T {
	t.Helper()
	var rows []string
	require.NoError(t, w.db.WithTenant(w.ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT (payload->'data')::text FROM outbox_events WHERE subject=? AND payload->'data'->>?=?`, subject, key, id).Scan(&rows).Error
	}))
	fields := map[string]bool{}
	typ := reflect.TypeFor[T]()
	for i := range typ.NumField() {
		fields[strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]] = true
	}
	out := make([]T, 0, len(rows))
	for _, raw := range rows {
		var keys map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(raw), &keys))
		require.Len(t, keys, len(fields), "the payload carries exactly the contract's fields: "+raw)
		dec := json.NewDecoder(strings.NewReader(raw))
		dec.DisallowUnknownFields()
		var v T
		require.NoError(t, dec.Decode(&v), raw)
		out = append(out, v)
	}
	return out
}

func TestAShiftPublishesItsLife(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 5000)
	counter, shiftID := w.counterWithShift(t, "clerk", "IPD-1", 1000)
	status, _ := w.put("clerk", "/counters/"+counter+"/shifts/"+shiftID, `{"opening_float_minor":1000}`)
	require.Equal(t, http.StatusOK, status, "a retried open replays")
	opened := published[billingcontract.ShiftOpenedData](t, w, billingcontract.SubjectShiftOpened, "shift_id", shiftID)
	require.Len(t, opened, 1, "one event, however often the open is retried")
	require.Equal(t, counter, opened[0].CounterID)
	require.Equal(t, "user-clerk", opened[0].Cashier)
	require.Equal(t, int64(1000), opened[0].OpeningFloatMinor)
	require.Equal(t, "INR", opened[0].Currency)
	require.Nil(t, opened[0].HandoverID)
	require.False(t, opened[0].OpenedAt.IsZero())

	var envelope struct{ ID, Type, Version, Tenant string }
	require.NoError(t, w.db.WithTenant(w.ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT id::text AS id,payload->>'type' AS type,payload->>'version' AS version,payload->>'tenant_id' AS tenant FROM outbox_events
			WHERE subject=? AND payload->'data'->>'shift_id'=? AND payload->>'event_id'=id::text`, billingcontract.SubjectShiftOpened, shiftID).Scan(&envelope).Error
	}))
	require.Equal(t, "ShiftOpened", envelope.Type, "the envelope's event_id is the outbox row's id")
	require.Equal(t, "1", envelope.Version)
	require.Equal(t, testutil.TenantA, envelope.Tenant)

	status, body := w.pay("clerk", admission, 3000)
	require.Equal(t, http.StatusCreated, status, body)
	picked := v7()
	status, body = w.pickup("admin", shiftID, picked, `{"note:2000":1}`)
	require.Equal(t, http.StatusCreated, status, body)
	status, _ = w.pickup("admin", shiftID, picked, `{"note:2000":1}`)
	require.Equal(t, http.StatusOK, status, "a retried pickup replays")
	fix := v7()
	status, body = w.put("admin", "/cash-pickups/"+picked+"/corrections/"+fix, `{"amount_minor":500,"reason":"Counted 500 short at the safe"}`)
	require.Equal(t, http.StatusCreated, status, body)
	pickups := published[billingcontract.CashPickupRecordedData](t, w, billingcontract.SubjectCashPickupRecorded, "shift_id", shiftID)
	require.Len(t, pickups, 2)
	byID := map[string]billingcontract.CashPickupRecordedData{pickups[0].PickupID: pickups[0], pickups[1].PickupID: pickups[1]}
	require.Equal(t, billingcontract.PickupKindPickup, byID[picked].Kind)
	require.Equal(t, int64(2000), byID[picked].AmountMinor)
	require.Equal(t, "user-admin", byID[picked].RecordedBy)
	require.Nil(t, byID[picked].CorrectsID)
	require.Equal(t, billingcontract.PickupKindCorrection, byID[fix].Kind)
	require.Equal(t, int64(500), byID[fix].AmountMinor)
	require.Equal(t, picked, *byID[fix].CorrectsID, "and no reason: free text stays in billing")

	// Nothing tells the expected cash while the drawer is open.
	require.Empty(t, published[billingcontract.ShiftVarianceRecordedData](t, w, billingcontract.SubjectShiftVarianceRecorded, "shift_id", shiftID))
	require.Empty(t, published[billingcontract.ShiftClosedData](t, w, billingcontract.SubjectShiftClosed, "shift_id", shiftID))

	status, _ = w.put("clerk2", "/cash-shifts/"+shiftID+"/close", closeBody(2000))
	require.Equal(t, http.StatusNotFound, status, "another cashier's close is refused")
	require.Empty(t, published[billingcontract.ShiftVarianceRecordedData](t, w, billingcontract.SubjectShiftVarianceRecorded, "shift_id", shiftID), "and publishes nothing")

	// Expected: 1000 float + 3000 taken - 2000 picked up + 500 put back.
	status, body = w.put("clerk", "/cash-shifts/"+shiftID+"/close", closeBody(2000))
	require.Equal(t, http.StatusOK, status, body)
	status, _ = w.put("clerk", "/cash-shifts/"+shiftID+"/close", closeBody(2000))
	require.Equal(t, http.StatusOK, status, "a retried close replays")
	variance := published[billingcontract.ShiftVarianceRecordedData](t, w, billingcontract.SubjectShiftVarianceRecorded, "shift_id", shiftID)
	require.Len(t, variance, 1)
	require.Equal(t, int64(2500), variance[0].ExpectedMinor)
	require.Equal(t, int64(2000), variance[0].DeclaredMinor)
	require.Equal(t, int64(-500), variance[0].VarianceMinor)
	require.False(t, variance[0].HandedOver)
	require.Empty(t, published[billingcontract.ShiftClosedData](t, w, billingcontract.SubjectShiftClosed, "shift_id", shiftID), "not closed until authorised")

	auth := `{"reason":"Shortage written off after recount"}`
	status, body = w.put("admin", "/cash-shifts/"+shiftID+"/authorisation", auth)
	require.Equal(t, http.StatusOK, status, body)
	status, _ = w.put("admin", "/cash-shifts/"+shiftID+"/authorisation", auth)
	require.Equal(t, http.StatusOK, status, "a retried authorisation replays")
	closed := published[billingcontract.ShiftClosedData](t, w, billingcontract.SubjectShiftClosed, "shift_id", shiftID)
	require.Len(t, closed, 1)
	require.Equal(t, int64(-500), closed[0].VarianceMinor)
	require.Equal(t, int64(1000), closed[0].OpeningFloatMinor)
	require.Equal(t, "user-admin", *closed[0].AuthorisedBy)
	require.NotNil(t, closed[0].AuthorisedAt)
	require.Len(t, published[billingcontract.ShiftVarianceRecordedData](t, w, billingcontract.SubjectShiftVarianceRecorded, "shift_id", shiftID), 1)

	_, exact := w.counterWithShift(t, "clerk2", "IPD-2", 1000)
	status, body = w.put("clerk2", "/cash-shifts/"+exact+"/close", closeBody(1000))
	require.Equal(t, http.StatusOK, status, body)
	require.Empty(t, published[billingcontract.ShiftVarianceRecordedData](t, w, billingcontract.SubjectShiftVarianceRecorded, "shift_id", exact), "a count that matches has no variance")
	closed = published[billingcontract.ShiftClosedData](t, w, billingcontract.SubjectShiftClosed, "shift_id", exact)
	require.Len(t, closed, 1)
	require.Zero(t, closed[0].VarianceMinor)
	require.Nil(t, closed[0].AuthorisedBy)
	require.Nil(t, closed[0].AuthorisedAt)

	// A change that fails after its trigger ran takes its event with it.
	lost, failed := v7(), errors.New("after the trigger")
	err := w.as("user-clerk", func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO billing_cash_shifts (tenant_id,hospital_id,id,counter_id,cashier,currency,opening_float_minor)
			VALUES (?,?,?,?,'user-clerk','INR',0)`, testutil.TenantA, w.hospital, lost, counter).Error; err != nil {
			return err
		}
		var written int64
		if err := tx.Raw(`SELECT count(*) FROM outbox_events WHERE subject=? AND payload->'data'->>'shift_id'=?`, billingcontract.SubjectShiftOpened, lost).Scan(&written).Error; err != nil {
			return err
		}
		require.Equal(t, int64(1), written, "the trigger wrote the event inside the transaction")
		return failed
	})
	require.ErrorIs(t, err, failed)
	require.Empty(t, published[billingcontract.ShiftOpenedData](t, w, billingcontract.SubjectShiftOpened, "shift_id", lost), "rolled back with the shift")
}

func TestAHandoverPublishesTheChangeOfCashier(t *testing.T) {
	w := newBedWorld(t)
	admission, patient := uuid.NewString(), uuid.NewString()
	w.charge(admission, patient, 5000)
	_, shiftID := w.counterWithShift(t, "clerk", "IPD-1", 1000)
	status, body := w.pay("clerk", admission, 3000)
	require.Equal(t, http.StatusCreated, status, body)

	withdrawn := v7()
	status, body = w.offer("clerk", shiftID, withdrawn, "user-clerk2", `{"note:2000":2}`)
	require.Equal(t, http.StatusCreated, status, body)
	status, body = w.put("clerk", "/cash-handovers/"+withdrawn+"/withdrawal", "")
	require.Equal(t, http.StatusOK, status, body)
	require.Empty(t, published[billingcontract.ShiftHandedOverData](t, w, billingcontract.SubjectShiftHandedOver, "handover_id", withdrawn), "an offer only pauses the drawer")

	id, next := v7(), v7()
	status, body = w.offer("clerk", shiftID, id, "user-clerk2", `{"note:2000":2}`)
	require.Equal(t, http.StatusCreated, status, body)
	status, body = w.accept("clerk2", id, next, `{"note:2000":2}`)
	require.Equal(t, http.StatusOK, status, body)
	status, _ = w.accept("clerk2", id, next, `{"note:2000":2}`)
	require.Equal(t, http.StatusOK, status, "a retried acceptance replays")

	handed := published[billingcontract.ShiftHandedOverData](t, w, billingcontract.SubjectShiftHandedOver, "handover_id", id)
	require.Len(t, handed, 1)
	require.Equal(t, shiftID, handed[0].FromShiftID)
	require.Equal(t, next, handed[0].ToShiftID)
	require.Equal(t, "user-clerk", handed[0].FromCashier)
	require.Equal(t, "user-clerk2", handed[0].ToCashier)
	require.Equal(t, int64(4000), handed[0].AgreedMinor)
	require.Nil(t, handed[0].SettledBy)
	closed := published[billingcontract.ShiftClosedData](t, w, billingcontract.SubjectShiftClosed, "shift_id", shiftID)
	require.Len(t, closed, 1)
	require.True(t, closed[0].HandedOver)
	require.Equal(t, int64(4000), closed[0].ExpectedMinor)
	opened := published[billingcontract.ShiftOpenedData](t, w, billingcontract.SubjectShiftOpened, "shift_id", next)
	require.Len(t, opened, 1)
	require.Equal(t, id, *opened[0].HandoverID)
	require.Equal(t, "user-clerk2", opened[0].Cashier)
	require.Equal(t, int64(4000), opened[0].OpeningFloatMinor)

	// Back the other way, disputed and settled short by a supervisor.
	back, third := v7(), v7()
	status, body = w.offer("clerk2", next, back, "user-clerk", `{"note:2000":2}`)
	require.Equal(t, http.StatusCreated, status, body)
	status, body = w.accept("clerk", back, third, `{"note:2000":1,"note:1000":2}`)
	require.Equal(t, http.StatusOK, status, body)
	require.Empty(t, published[billingcontract.ShiftHandedOverData](t, w, billingcontract.SubjectShiftHandedOver, "handover_id", back), "a dispute only pauses the drawer")
	status, body = w.put("admin", "/cash-handovers/"+back+"/settlement", `{"counts":{"note:2000":1,"note:1000":1},"reason":"Recounted at the counter with both cashiers"}`)
	require.Equal(t, http.StatusOK, status, body)

	handed = published[billingcontract.ShiftHandedOverData](t, w, billingcontract.SubjectShiftHandedOver, "handover_id", back)
	require.Len(t, handed, 1)
	require.Equal(t, "user-admin", *handed[0].SettledBy)
	require.Equal(t, int64(3000), handed[0].AgreedMinor)
	variance := published[billingcontract.ShiftVarianceRecordedData](t, w, billingcontract.SubjectShiftVarianceRecorded, "shift_id", next)
	require.Len(t, variance, 1)
	require.Equal(t, int64(-1000), variance[0].VarianceMinor)
	require.True(t, variance[0].HandedOver)
	require.Empty(t, published[billingcontract.ShiftClosedData](t, w, billingcontract.SubjectShiftClosed, "shift_id", next))
	require.Len(t, published[billingcontract.ShiftOpenedData](t, w, billingcontract.SubjectShiftOpened, "shift_id", third), 1)
}

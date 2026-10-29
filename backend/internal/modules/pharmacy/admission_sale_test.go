package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	medicorecontract "github.com/tesserix/helivanta/internal/modules/medicore/contract"
	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // module under test
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// #1387: ward medicine billed to the admission occupying a bed.

type ward struct {
	w        saleWorld
	t        *testing.T
	hospital string
	handle   func(name string, v any) error
}

func newWard(t *testing.T, w saleWorld) ward {
	consumers := map[string]events.Consumer{}
	for _, c := range pharmacy.New().Consumers(platform.Deps{}) {
		consumers[c.Name] = c
	}
	types := map[string]string{
		"pharmacy-patient-registered":    patientmastercontract.TypePatientRegistered,
		"pharmacy-bed-occupancy-started": "BedOccupancyStarted",
		"pharmacy-bed-occupancy-ended":   "BedOccupancyEnded",
	}
	handle := func(name string, v any) error {
		data, err := json.Marshal(v)
		require.NoError(t, err)
		c := consumers[name]
		typ := types[name]
		if typ == "" {
			typ = c.Subject
		}
		return w.db.WithTenant(tenantdb.WithActor(t.Context(), "system:"+name), testutil.TenantA, func(tx *gorm.DB) error {
			return c.Handle(t.Context(), tx, events.Event{Type: typ, Version: 1, TenantID: testutil.TenantA, Data: data})
		})
	}
	return ward{w: w, t: t, hospital: uuid.NewString(), handle: handle}
}

func (d ward) verify() {
	root := "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/hospital-affiliations/"
	affiliation, round := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	require.Equal(d.t, http.StatusOK, do(d.w.r, "PUT", root+affiliation, "editor", fmt.Sprintf(`{"hospital_id":%q,"relationship":"owner"}`, d.hospital)).Code)
	require.Equal(d.t, http.StatusOK, do(d.w.r, "PUT", root+affiliation+"/verification-requests/"+round, "editor", `{}`).Code)
	require.NoError(d.t, d.handle("pharmacy-affiliation-decision", patientmastercontract.PharmacyAffiliationDecidedData{RequestID: round, AffiliationID: affiliation, HospitalID: d.hospital,
		DecisionID: uuid.Must(uuid.NewV7()).String(), Outcome: "accepted", DecidedAt: time.Now().UTC().Format(time.RFC3339Nano)}))
}

type stay struct {
	occupancy, admission, patient, bed string
	startedAt                          time.Time
}

func newStay() stay {
	// Nanoseconds on purpose: the event may be finer than Postgres stores.
	at := time.Now().UTC().Truncate(time.Second).Add(123456789 * time.Nanosecond)
	return stay{occupancy: uuid.NewString(), admission: uuid.NewString(), patient: uuid.NewString(), bed: uuid.NewString(), startedAt: at}
}

func (d ward) admit(room, bed string) stay {
	s := newStay()
	require.NoError(d.t, d.handle("pharmacy-patient-registered", patientmastercontract.PatientRegisteredData{PatientID: s.patient, HospitalID: d.hospital}))
	d.start(s, room, bed)
	return s
}

func (d ward) start(s stay, room, bed string) {
	require.NoError(d.t, d.handle("pharmacy-bed-occupancy-started", medicorecontract.BedOccupancyStartedData{OccupancyID: s.occupancy, AdmissionID: s.admission, PatientID: s.patient,
		HospitalID: d.hospital, WardID: uuid.NewString(), RoomID: uuid.NewString(), BedID: s.bed, WardCode: "W-A", RoomCode: room, BedCode: bed, StartedAt: s.startedAt.Format(time.RFC3339Nano)}))
}

func (d ward) end(s stay) {
	require.NoError(d.t, d.handle("pharmacy-bed-occupancy-ended", medicorecontract.BedOccupancyEndedData{OccupancyID: s.occupancy, AdmissionID: s.admission, HospitalID: d.hospital,
		BedID: s.bed, EndedAt: time.Now().UTC().Format(time.RFC3339Nano), End: "release"}))
}

func (d ward) resolve(query string) (int, string) {
	res := do(d.w.r, "GET", licencePath("/admission-occupant?hospital_id="+d.hospital+"&"+query), "pharmacist", "")
	return res.Code, res.Body.String()
}

func admittedSale(product, occupancy string) string {
	return fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":2}],"tenders":[{"kind":"admission_account","amount_minor":2500}],"admission":{"occupancy_id":%q}}`, product, occupancy)
}

func TestWardMedicineIsBilledToTheAdmissionInTheBed(t *testing.T) {
	w := newSaleWorld(t, true)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 30)
	d := newWard(t, w)
	first := d.admit("R-1", "A-1")

	code, out := d.resolve("bed_code=A-1")
	require.Equal(t, http.StatusNotFound, code, "a hospital that has not verified the pharmacy shows nothing")
	require.NotContains(t, out, first.patient)
	d.verify()
	code, out = d.resolve("bed_code=A-1")
	require.Equal(t, http.StatusOK, code, out)
	require.Contains(t, out, `"admission_id":"`+first.admission+`"`)
	require.Contains(t, out, `"patient_id":"`+first.patient+`"`)
	require.Contains(t, out, `"bed_code":"A-1"`)
	require.NotContains(t, out, "started_at", "the resolve answers ids and codes, not when the stay began")
	code, out = d.resolve("bed_code=A-9")
	require.Equal(t, http.StatusNotFound, code)
	require.Contains(t, out, "no_occupant")
	var audited []string
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT resource_id::text FROM audit_events WHERE resource_type='pharmacy_admission_resolve' AND actor_subject='user-pharmacist' ORDER BY occurred_at,id`).Scan(&audited).Error
	}))
	require.Equal(t, []string{first.occupancy, d.hospital}, audited, "every resolve is audited, a miss against the hospital")
	code, _ = d.resolve("room_code=R-1")
	require.Equal(t, http.StatusOK, code, "one occupied bed in the room")
	second := d.admit("R-1", "A-2")
	code, out = d.resolve("room_code=R-1")
	require.Equal(t, http.StatusConflict, code, out)
	require.Contains(t, out, "ambiguous")
	require.NotContains(t, out, first.patient)
	code, _ = d.resolve("bed_code=A-1&room_code=R-1")
	require.Equal(t, http.StatusBadRequest, code)

	// Billed to the account: nothing is collected.
	id := uuid.Must(uuid.NewV7()).String()
	res := do(w.r, "PUT", salePath("/"+id), "pharmacist", admittedSale(p, first.occupancy))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	posted := res.Body.String()
	require.Contains(t, posted, `"admission_id":"`+first.admission+`"`)
	require.Contains(t, posted, `"admission_bed_code":"A-1"`)
	require.Contains(t, posted, `"patient_id":"`+first.patient+`"`, "the occupant is the sale's registered patient")
	require.Contains(t, posted, `"kind":"admission_account"`)
	require.Contains(t, posted, `"admission_resolved_at":"`)
	replay := do(w.r, "PUT", salePath("/"+id), "pharmacist", admittedSale(p, first.occupancy))
	require.Equal(t, http.StatusOK, replay.Code)
	require.JSONEq(t, posted, replay.Body.String())

	var charges []string
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT (payload->'data')::text FROM outbox_events WHERE subject=? ORDER BY created_at`, pharmacycontract.SubjectAdmissionChargePosted).Scan(&charges).Error
	}))
	require.Len(t, charges, 1, "a replay publishes nothing new")
	var charge pharmacycontract.AdmissionChargePostedData
	require.NoError(t, json.Unmarshal([]byte(charges[0]), &charge))
	require.Equal(t, id, charge.SaleID)
	require.Equal(t, first.admission, charge.AdmissionID)
	require.Equal(t, d.hospital, charge.HospitalID)
	require.EqualValues(t, 2500, charge.AmountMinor)

	_, days, report := dailyReport(t, w, "pharmacist", "?from=2026-11-01&to=2026-11-01")
	require.Len(t, days, 1, report)
	require.Contains(t, report, `"admission_account_minor":2500`, "billed to the account, not collected, and shown as such")

	// What may not be sold.
	cash := strings.Replace(admittedSale(p, second.occupancy), `{"kind":"admission_account","amount_minor":2500}`, `{"kind":"cash","amount_minor":2500,"tendered_minor":2500}`, 1)
	code, _, out = w.sell(t, cash)
	require.Equal(t, http.StatusBadRequest, code, out, "an admitted sale is never paid in cash")
	code, _, out = w.sell(t, fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":2}],"tenders":[{"kind":"admission_account","amount_minor":2500}]}`, p))
	require.Equal(t, http.StatusBadRequest, code, out, "only an admitted sale is billed to the account")
	both := strings.Replace(admittedSale(p, second.occupancy), `"admission":`, `"buyer":{"name":"TEST Walk In"},"admission":`, 1)
	code, _, out = w.sell(t, both)
	require.Equal(t, http.StatusBadRequest, code, out)
	d.end(second)
	code, _, out = w.sell(t, admittedSale(p, second.occupancy))
	require.Equal(t, http.StatusConflict, code, out)
	require.Contains(t, out, "no longer in that bed", "a stay that ended between finding and posting refuses the sale")
	code, _, out = w.sell(t, admittedSale(p, uuid.NewString()))
	require.Equal(t, http.StatusConflict, code, out)
	// The database refuses a sale on an ended stay without the handler: a
	// copy of the posted sale, renumbered, naming the stay that has ended.
	err := catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_sales SELECT (jsonb_populate_record(NULL::pharmacy_sales, to_jsonb(s) || jsonb_build_object(
 'id', ?::text, 'sequence_number', s.sequence_number+1000, 'invoice_number', regexp_replace(s.invoice_number,'[0-9]+$',(s.sequence_number+1000)::text),
 'intent_sha256', repeat('d',64), 'actor_subject', 'user-editor', 'admission_id', ?::text, 'admission_occupancy_id', ?::text,
 'patient_id', ?::text, 'admission_bed_code', 'A-2'))).* FROM pharmacy_sales s WHERE s.id=?`, uuid.Must(uuid.NewV7()).String(), second.admission, second.occupancy, second.patient, id).Error
	})
	require.ErrorContains(t, err, "no longer in that bed")

	// A return goes back to the account, never in money.
	cashReturn := returnBody(id, 1, w.quarantine, 1250)
	ret := do(w.r, "PUT", licencePath("/credit-notes/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", cashReturn)
	require.Equal(t, http.StatusConflict, ret.Code, ret.Body.String())
	require.Contains(t, ret.Body.String(), "goes back to the admission account")
	accountReturn := strings.Replace(cashReturn, `{"kind":"cash","amount_minor":1250}`, `{"kind":"admission_account","amount_minor":1250}`, 1)
	ret = do(w.r, "PUT", licencePath("/credit-notes/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", accountReturn)
	require.Equal(t, http.StatusCreated, ret.Code, ret.Body.String())
	var credited struct{ ID string }
	require.NoError(t, json.Unmarshal(ret.Body.Bytes(), &credited))
	require.ErrorContains(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_credit_note_refunds (tenant_id,outlet_id,credit_note_id,refund_number,kind,amount_minor) SELECT tenant_id,outlet_id,id,5,'cash',1 FROM pharmacy_credit_notes WHERE id=?`, credited.ID).Error
	}), "goes back to the admission account", "a cash refund of an admitted sale is refused without the handler")
	var reversals []string
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT (payload->'data')::text FROM outbox_events WHERE subject=?`, pharmacycontract.SubjectAdmissionChargeReversed).Scan(&reversals).Error
	}))
	require.Len(t, reversals, 1)
	var reversal pharmacycontract.AdmissionChargeReversedData
	require.NoError(t, json.Unmarshal([]byte(reversals[0]), &reversal))
	require.Equal(t, id, reversal.SaleID)
	require.Equal(t, first.admission, reversal.AdmissionID)
	require.EqualValues(t, 1250, reversal.AmountMinor)

	// A walk-in sale cannot be refunded to an account.
	walkIn := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, http.StatusCreated, do(w.r, "PUT", salePath("/"+walkIn), "pharmacist", cashSale(p, 1, 1250, 1250)).Code)
	bad := strings.Replace(returnBody(walkIn, 1, w.quarantine, 1250), `{"kind":"cash","amount_minor":1250}`, `{"kind":"admission_account","amount_minor":1250}`, 1)
	ret = do(w.r, "PUT", licencePath("/credit-notes/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", bad)
	require.Equal(t, http.StatusConflict, ret.Code, ret.Body.String())
}

func TestBedOccupancyProjectionTakesEventsInAnyOrder(t *testing.T) {
	w := newSaleWorld(t, true)
	d := newWard(t, w)
	d.verify()
	s := newStay()
	d.end(s)
	code, _ := d.resolve("bed_code=B-1")
	require.Equal(t, http.StatusNotFound, code)
	d.start(s, "R-2", "B-1")
	code, _ = d.resolve("bed_code=B-1")
	require.Equal(t, http.StatusNotFound, code, "an end that arrived first keeps the stay closed")
	d.start(s, "R-2", "B-1")
	d.end(s)
	var rows int64
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM pharmacy_bed_occupancies WHERE occupancy_id=? AND started_at IS NOT NULL AND ended_at IS NOT NULL`, s.occupancy).Scan(&rows).Error
	}))
	require.EqualValues(t, 1, rows, "redelivery is idempotent")
	require.Error(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE pharmacy_bed_occupancies SET bed_code='X-1' WHERE occupancy_id=?`, s.occupancy).Error
	}), "a stay's start is fixed")
}

func TestAStayEndingWhileTheSalePostsRefusesIt(t *testing.T) {
	w := newSaleWorld(t, true)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 30)
	d := newWard(t, w)
	d.verify()
	s := d.admit("R-1", "A-1")
	// The sale holds the stay; the end waits for it, or the sale sees the end.
	held, let, sold := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		sold <- catalogueTx(t, w.db, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT 1 FROM pharmacy_bed_occupancies WHERE occupancy_id=? FOR SHARE`, s.occupancy).Error; err != nil {
				return err
			}
			close(held)
			<-let
			return nil
		})
	}()
	<-held
	ended := make(chan struct{})
	go func() { d.end(s); close(ended) }()
	select {
	case <-ended:
		t.Fatal("the stay ended while a sale held it")
	case <-time.After(300 * time.Millisecond):
	}
	close(let)
	require.NoError(t, <-sold)
	<-ended
	code, _, out := w.sell(t, admittedSale(p, s.occupancy))
	require.Equal(t, http.StatusConflict, code, out)
	require.Contains(t, out, "no longer in that bed")

	// A replayed start that differs is refused, not silently ignored.
	require.Error(t, d.handle("pharmacy-bed-occupancy-started", medicorecontract.BedOccupancyStartedData{OccupancyID: s.occupancy, AdmissionID: s.admission, PatientID: uuid.NewString(),
		HospitalID: d.hospital, WardID: uuid.NewString(), RoomID: uuid.NewString(), BedID: s.bed, WardCode: "W-A", RoomCode: "R-1", BedCode: "A-1", StartedAt: time.Now().UTC().Format(time.RFC3339Nano)}))
}

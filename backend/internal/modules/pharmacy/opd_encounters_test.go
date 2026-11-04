package pharmacy_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	medicorecontract "github.com/tesserix/helivanta/internal/modules/medicore/contract"           //nolint:depguard // external test feeding the consumer its published contract
	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract" //nolint:depguard // external test feeding the consumer its published contract
)

// #987 slice 2: pharmacy's projection of MediCore's OPD encounters.

type opd struct {
	d                         ward
	encounter, patient, place string
}

func (d ward) newOPD(patient string) opd {
	return opd{d: d, encounter: uuid.NewString(), patient: patient, place: d.hospital}
}

func (o opd) opened() error {
	return o.d.handle("pharmacy-encounter-opened", medicorecontract.EncounterOpenedData{EncounterID: o.encounter, HospitalID: o.place, PatientID: o.patient})
}

func (o opd) moved(from, to string, version int) error {
	return o.d.handle("pharmacy-encounter-status-changed", medicorecontract.EncounterStatusChangedData{EncounterID: o.encounter, HospitalID: o.place, FromStatus: from, ToStatus: to, Version: version})
}

type heldEncounter struct {
	HospitalID string  `gorm:"column:hospital_id"`
	PatientID  *string `gorm:"column:patient_id"`
	Status     *string `gorm:"column:status"`
	Version    *int    `gorm:"column:version"`
}

func (o opd) held(t *testing.T) heldEncounter {
	t.Helper()
	var h heldEncounter
	require.NoError(t, catalogueTx(t, o.d.w.db, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT hospital_id::text, patient_id::text, status, version FROM pharmacy_opd_encounters WHERE encounter_id=?`, o.encounter).Scan(&h).Error
	}))
	return h
}

func TestTheOPDEncounterProjectionTakesEventsInAnyOrder(t *testing.T) {
	w := newSaleWorld(t, true)
	d := newWard(t, w)
	patient := uuid.NewString()

	// Opened, then moved.
	a := d.newOPD(patient)
	require.NoError(t, a.opened())
	h := a.held(t)
	require.Equal(t, patient, *h.PatientID)
	require.Equal(t, "arrived", *h.Status, "an encounter opens arrived")
	require.Equal(t, 1, *h.Version)
	require.NoError(t, a.moved("arrived", "in_consultation", 2))
	require.NoError(t, a.moved("in_consultation", "closed", 3))
	require.Equal(t, "closed", *a.held(t).Status)

	// A status change that arrives first holds the status; the opening
	// that follows fills the patient and keeps the later status.
	b := d.newOPD(patient)
	require.NoError(t, b.moved("arrived", "cancelled", 2))
	h = b.held(t)
	require.Nil(t, h.PatientID, "not yet opened")
	require.Equal(t, "cancelled", *h.Status)
	require.NoError(t, b.opened())
	h = b.held(t)
	require.Equal(t, patient, *h.PatientID)
	require.Equal(t, "cancelled", *h.Status, "the opening does not undo a later status")
	require.Equal(t, 2, *h.Version)

	// An older status arriving last changes nothing; redelivery is idempotent.
	require.NoError(t, a.moved("arrived", "in_consultation", 2))
	require.NoError(t, a.opened())
	require.NoError(t, a.moved("in_consultation", "closed", 3))
	h = a.held(t)
	require.Equal(t, "closed", *h.Status)
	require.Equal(t, 3, *h.Version)
}

func TestAConflictingEncounterEventIsRefused(t *testing.T) {
	w := newSaleWorld(t, true)
	d := newWard(t, w)
	a := d.newOPD(uuid.NewString())
	require.NoError(t, a.opened())
	other := a
	other.patient = uuid.NewString()
	require.Error(t, other.opened(), "one encounter, one patient")
	elsewhere := a
	elsewhere.place = uuid.NewString()
	require.Error(t, elsewhere.opened(), "one encounter, one hospital")
	require.Error(t, elsewhere.moved("arrived", "closed", 9), "a status from another hospital")
	require.NoError(t, a.moved("arrived", "in_consultation", 2))
	require.Error(t, a.moved("arrived", "closed", 2), "one version, one status")
	require.Error(t, a.moved("arrived", "nonsense", 3), "a status MediCore never publishes")
	require.Error(t, d.handle("pharmacy-encounter-opened", medicorecontract.EncounterOpenedData{EncounterID: "nope", HospitalID: d.hospital, PatientID: a.patient}))
	h := a.held(t)
	require.Equal(t, a.patient, *h.PatientID)
	require.Equal(t, "in_consultation", *h.Status)
	require.Error(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Exec(`DELETE FROM pharmacy_opd_encounters WHERE encounter_id=?`, a.encounter).Error
	}), "the projection is retained")
}

// encounterWorld has a registered patient of a verified hospital and stock
// to sell them.
type encounterWorld struct {
	saleWorld
	d                ward
	product, patient string
}

func newEncounterWorld(t *testing.T) encounterWorld {
	t.Helper()
	w := encounterWorld{saleWorld: newSaleWorld(t, true)}
	w.product = w.saleWorld.product(t, "TEST-PCM-500", "none")
	w.shelve(t, w.product, "2028-06-30", 12500, 30)
	w.d = newWard(t, w.saleWorld)
	w.d.verify()
	w.patient = w.d.register(t)
	return w
}

func (d ward) register(t *testing.T) string {
	t.Helper()
	patient := uuid.NewString()
	require.NoError(t, d.handle("pharmacy-patient-registered", patientmastercontract.PatientRegisteredData{PatientID: patient, HospitalID: d.hospital}))
	return patient
}

// encounterSale is a cash sale of two units to patient at hospital citing
// encounter; an empty encounter cites none.
func encounterSale(product, patient, hospital, encounter string) string {
	cite := ""
	if encounter != "" {
		cite = fmt.Sprintf(`,"encounter":{"encounter_id":%q}`, encounter)
	}
	return fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":2}],"tenders":[{"kind":"cash","amount_minor":2500,"tendered_minor":2500}],"patient":{"patient_id":%q,"hospital_id":%q}%s}`,
		product, patient, hospital, cite)
}

func TestASaleCitesItsPatientsOPDEncounter(t *testing.T) {
	w := newEncounterWorld(t)
	visit := w.d.newOPD(w.patient)
	require.NoError(t, visit.opened())
	id := uuid.Must(uuid.NewV7()).String()
	res := do(w.r, "PUT", salePath("/"+id), "pharmacist", encounterSale(w.product, w.patient, w.d.hospital, visit.encounter))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), `"encounter_id":"`+visit.encounter+`"`)
	res = do(w.r, "GET", salePath("/"+id), "pharmacist", "")
	require.Contains(t, res.Body.String(), `"encounter_id":"`+visit.encounter+`"`, "the invoice read carries the link")
	// A replay of the same sale with another encounter is a different intent.
	other := w.d.newOPD(w.patient)
	require.NoError(t, other.opened())
	res = do(w.r, "PUT", salePath("/"+id), "pharmacist", encounterSale(w.product, w.patient, w.d.hospital, other.encounter))
	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())

	// Patients collect medicines after the consultation: a closed encounter is citable.
	require.NoError(t, visit.moved("arrived", "in_consultation", 2))
	require.NoError(t, visit.moved("in_consultation", "closed", 3))
	code, _, out := w.sell(t, encounterSale(w.product, w.patient, w.d.hospital, visit.encounter))
	require.Equal(t, http.StatusCreated, code, out)
	// Citing is optional.
	code, _, out = w.sell(t, encounterSale(w.product, w.patient, w.d.hospital, ""))
	require.Equal(t, http.StatusCreated, code, out)
	require.Contains(t, out, `"encounter_id":null`)
}

func TestASaleCannotCiteAnEncounterThatIsNotItsPatientsVisit(t *testing.T) {
	w := newEncounterWorld(t)
	refused := func(why, body, reason string) {
		t.Helper()
		code, _, out := w.sell(t, body)
		require.Equal(t, http.StatusConflict, code, why+": "+out)
		require.Contains(t, out, reason, why)
	}
	const (
		unknown      = "the pharmacy has not received that visit"
		notTheirs    = "that visit is not this patient's at this hospital"
		didNotHappen = "that visit was cancelled, or the patient left without being seen"
	)
	cancelled := w.d.newOPD(w.patient)
	require.NoError(t, cancelled.opened())
	require.NoError(t, cancelled.moved("arrived", "cancelled", 2))
	refused("cancelled", encounterSale(w.product, w.patient, w.d.hospital, cancelled.encounter), didNotHappen)
	left := w.d.newOPD(w.patient)
	require.NoError(t, left.opened())
	require.NoError(t, left.moved("arrived", "left_without_being_seen", 2))
	refused("left without being seen", encounterSale(w.product, w.patient, w.d.hospital, left.encounter), didNotHappen)
	refused("unknown", encounterSale(w.product, w.patient, w.d.hospital, uuid.NewString()), unknown)
	unopened := w.d.newOPD(w.patient)
	require.NoError(t, unopened.moved("arrived", "in_consultation", 2))
	refused("not yet opened", encounterSale(w.product, w.patient, w.d.hospital, unopened.encounter), unknown)
	someoneElse := w.d.newOPD(w.d.register(t))
	require.NoError(t, someoneElse.opened())
	refused("another patient's", encounterSale(w.product, w.patient, w.d.hospital, someoneElse.encounter), notTheirs)
	// The same patient's visit at a hospital the sale does not name.
	elsewhere := w.d.newOPD(w.patient)
	elsewhere.place = uuid.NewString()
	require.NoError(t, elsewhere.opened())
	refused("another hospital's", encounterSale(w.product, w.patient, w.d.hospital, elsewhere.encounter), notTheirs)

	// Only a registered patient's sale may cite a visit.
	visit := w.d.newOPD(w.patient)
	require.NoError(t, visit.opened())
	cite := fmt.Sprintf(`,"encounter":{"encounter_id":%q}`, visit.encounter)
	for why, body := range map[string]string{
		"walk-in": fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":2}],"tenders":[{"kind":"cash","amount_minor":2500,"tendered_minor":2500}]%s}`, w.product, cite),
		"buyer":   fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":2}],"tenders":[{"kind":"cash","amount_minor":2500,"tendered_minor":2500}],"buyer":{"name":"TEST Walk In"}%s}`, w.product, cite),
		"admission": fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":2}],"tenders":[{"kind":"admission_account","amount_minor":2500}],"admission":{"occupancy_id":%q}%s}`,
			w.product, uuid.NewString(), cite),
	} {
		code, _, out := w.sell(t, body)
		require.Equal(t, http.StatusBadRequest, code, why+": "+out)
	}
	var cited int64
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM pharmacy_sales WHERE encounter_id IS NOT NULL`).Scan(&cited).Error
	}))
	require.Zero(t, cited, "no refused sale was posted")
}

// The database holds the rule itself: an admission sale never cites an OPD
// visit, whatever posts it.
func TestTheDatabaseRefusesAnAdmissionSaleCitingAVisit(t *testing.T) {
	w := newEncounterWorld(t)
	stay := w.d.admit("R-1", "B-7")
	visit := w.d.newOPD(stay.patient)
	require.NoError(t, visit.opened())
	admitted := uuid.Must(uuid.NewV7()).String()
	res := do(w.r, "PUT", salePath("/"+admitted), "pharmacist", admittedSale(w.product, stay.occupancy))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	err := catalogueTx(t, w.db, func(tx *gorm.DB) error {
		if err := tx.Exec(`CREATE TEMP TABLE copied ON COMMIT DROP AS SELECT * FROM pharmacy_sales WHERE id=?`, admitted).Error; err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE copied SET id=?, sequence_number=999, invoice_number=regexp_replace(invoice_number,'/[0-9]+$','/999'), encounter_id=?, posted_xact=pg_current_xact_id(), actor_subject='user-editor'`,
			uuid.Must(uuid.NewV7()).String(), visit.encounter).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO pharmacy_sales SELECT * FROM copied`).Error
	})
	require.ErrorContains(t, err, "pharmacy_sale_encounter_patient_sale")
}

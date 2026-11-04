package pharmacy_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	medicorecontract "github.com/tesserix/helivanta/internal/modules/medicore/contract" //nolint:depguard // external test feeding the consumer its published contract
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

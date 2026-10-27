package medicore_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	drugmastercontract "github.com/tesserix/helivanta/internal/modules/drugmaster/contract"
	"github.com/tesserix/helivanta/internal/modules/medicore" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// #1373: allergy status keeps unknown distinct from "no known allergy",
// entries name drug master molecules, and weight is an attributed
// observation; all scoped to a hospital the caller is granted at.

func clinicalSetup(t *testing.T, recording bool) (*gin.Engine, *tenantdb.DB, context.Context) {
	clinical := []authz.Permission{medicore.PermAllergyRead, medicore.PermAllergyRecord, medicore.PermObservationRead, medicore.PermObservationRecord}
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		AllergyRecordingEnabled: recording,
		Tokens:                  map[string]string{"doc": testutil.TenantA, "nurse": testutil.TenantA, "admin": testutil.TenantA, "clerk": testutil.TenantA, "docB": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"doc":   append([]authz.Permission{medicore.PermVisitCreate, medicore.PermVisitRead}, clinical...),
			"nurse": clinical,
			"admin": {medicore.PermAccessManage},
			"clerk": {medicore.PermVisitRead},
			"docB":  append([]authz.Permission{medicore.PermAccessManage}, clinical...),
		},
		Modules: []platform.Module{medicore.New()},
	})
	for _, subject := range []string{"user-doc", "user-nurse", "user-clerk"} {
		require.Equal(t, http.StatusOK, do(r, "PUT", "/v1/medicore/hospitals/"+hospitalA.String()+"/access/"+subject, "admin", "").Code)
	}
	return r, db, ctx
}

// molecule runs medicore's own consumer for a drug master announcement.
func molecule(t *testing.T, db *tenantdb.DB, ctx context.Context, name, atc string) uuid.UUID {
	t.Helper()
	id := uuid.Must(uuid.NewV7())
	data, err := json.Marshal(drugmastercontract.MoleculePublishedData{MoleculeID: id.String(), INNName: name, ATCCode: atc, ReleaseID: uuid.Must(uuid.NewV7()).String()})
	require.NoError(t, err)
	var handle func(context.Context, *gorm.DB, events.Event) error
	for _, c := range medicore.New().Consumers(platform.Deps{}) {
		if c.Name == "medicore-molecule-published" {
			handle = c.Handle
		}
	}
	require.NotNil(t, handle)
	evt := events.Event{Type: drugmastercontract.SubjectMoleculePublished, Version: 1, Data: data}
	for range 2 { // redelivery is a no-op
		require.NoError(t, db.WithSystem(tenantdb.WithActor(ctx, "system:medicore-molecule-published"), func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.actor_subject', 'system:medicore-molecule-published', true)`).Error; err != nil {
				return err
			}
			return handle(ctx, tx, evt)
		}))
	}
	return id
}

func patientURL(hospital, patient uuid.UUID) string {
	return "/v1/medicore/hospitals/" + hospital.String() + "/patients/" + patient.String()
}

type statusResp struct {
	Status  string `json:"status"`
	NoKnown *struct {
		RecordedBy string `json:"recorded_by"`
	} `json:"no_known_allergy"`
	Entries []struct {
		ID             string  `json:"id"`
		Version        int     `json:"version"`
		Status         string  `json:"status"`
		SubstanceKind  string  `json:"substance_kind"`
		MoleculeID     *string `json:"molecule_id"`
		SubstanceLabel string  `json:"substance_label"`
		Severity       string  `json:"severity"`
		RecordedBy     string  `json:"recorded_by"`
		NeedsCoding    bool    `json:"needs_coding"`
	} `json:"entries"`
	NeedsCoding      bool `json:"needs_coding"`
	RecordingEnabled bool `json:"recording_enabled"`
}

func allergyStatus(t *testing.T, r *gin.Engine, token string, patient uuid.UUID) statusResp {
	t.Helper()
	w := do(r, "GET", patientURL(hospitalA, patient)+"/allergies", token, "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var s statusResp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &s))
	return s
}

func codedAllergy(molecule uuid.UUID) string {
	return fmt.Sprintf(`{"substance_kind":"drug_molecule","molecule_id":%q,"reaction":"urticaria","severity":"moderate","source":"patient_reported"}`, molecule)
}

func readAudits(t *testing.T, db *tenantdb.DB, ctx context.Context, patient uuid.UUID) int64 {
	var n int64
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM medicore_clinical_read_audit WHERE patient_id = ?`, patient).Scan(&n).Error
	}))
	return n
}

// Review focus 1: nothing recorded reads as unknown, never as none, and
// every read is audited.
func TestAPatientWithNoRecordIsUnknown(t *testing.T) {
	r, db, ctx := clinicalSetup(t, true)
	patient := registered(t, db, ctx, testutil.TenantA, hospitalA)
	s := allergyStatus(t, r, "nurse", patient)
	require.Equal(t, "unknown", s.Status)
	require.True(t, s.RecordingEnabled)
	require.Nil(t, s.NoKnown)
	require.Empty(t, s.Entries)
	require.Equal(t, int64(1), readAudits(t, db, ctx, patient))
}

// Review focus 2: "no known allergy" and allergies cannot both stand; the
// newer fact wins, and a resolved allergy leaves unknown, not "none".
func TestNoKnownAllergyAndAllergiesExcludeEachOther(t *testing.T) {
	r, db, ctx := clinicalSetup(t, true)
	patient := registered(t, db, ctx, testutil.TenantA, hospitalA)
	amox := molecule(t, db, ctx, "Amoxicillin", "J01CA04")
	base := patientURL(hospitalA, patient)

	nka := uuid.Must(uuid.NewV7())
	w := do(r, "PUT", base+"/no-known-allergy/"+nka.String(), "nurse", `{}`)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Equal(t, http.StatusOK, do(r, "PUT", base+"/no-known-allergy/"+nka.String(), "nurse", `{}`).Code, "replay")
	s := allergyStatus(t, r, "doc", patient)
	require.Equal(t, "no_known_allergy", s.Status)
	require.Equal(t, "user-nurse", s.NoKnown.RecordedBy)

	entry := uuid.Must(uuid.NewV7())
	w = do(r, "PUT", base+"/allergies/"+entry.String(), "doc", codedAllergy(amox))
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	s = allergyStatus(t, r, "doc", patient)
	require.Equal(t, "allergies_recorded", s.Status, "a recorded allergy supersedes no known allergy")
	require.Nil(t, s.NoKnown)
	require.Len(t, s.Entries, 1)
	require.Equal(t, "Amoxicillin", s.Entries[0].SubstanceLabel, "the label is the drug master's name")

	w = do(r, "PUT", base+"/no-known-allergy/"+uuid.Must(uuid.NewV7()).String(), "nurse", `{}`)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())

	w = do(r, "POST", base+"/allergies/"+entry.String()+"/corrections", "doc", `{"expected_version":1,"status":"resolved","reason":"tolerated a full course in 2026"}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "unknown", allergyStatus(t, r, "doc", patient).Status, "a resolved allergy is not a reassurance")

	w = do(r, "PUT", base+"/no-known-allergy/"+uuid.Must(uuid.NewV7()).String(), "nurse", `{}`)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.Equal(t, "no_known_allergy", allergyStatus(t, r, "doc", patient).Status)
}

// Review focus 3: only published molecules can be coded; free text is on
// the record but flagged.
func TestAllergySubstancesAreCodedOrFlagged(t *testing.T) {
	r, db, ctx := clinicalSetup(t, true)
	patient := registered(t, db, ctx, testutil.TenantA, hospitalA)
	base := patientURL(hospitalA, patient)

	w := do(r, "PUT", base+"/allergies/"+uuid.Must(uuid.NewV7()).String(), "doc", codedAllergy(uuid.Must(uuid.NewV7())))
	require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())

	w = do(r, "PUT", base+"/allergies/"+uuid.Must(uuid.NewV7()).String(), "doc",
		`{"substance_kind":"drug_unlisted","substance_label":"Brand X syrup","reaction":"rash","severity":"mild","source":"patient_reported"}`)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	w = do(r, "PUT", base+"/allergies/"+uuid.Must(uuid.NewV7()).String(), "doc",
		`{"substance_kind":"non_drug","non_drug_category":"food","substance_label":"Peanut","reaction":"anaphylaxis","severity":"life_threatening","source":"clinician_observed"}`)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	s := allergyStatus(t, r, "doc", patient)
	require.True(t, s.NeedsCoding, "an uncoded drug allergy blocks prescribing checks")
	flagged := 0
	for _, e := range s.Entries {
		if e.NeedsCoding {
			flagged++
			require.Equal(t, "drug_unlisted", e.SubstanceKind)
		}
	}
	require.Equal(t, 1, flagged)

	for _, body := range []string{
		`{"substance_kind":"drug_unlisted","reaction":"rash","severity":"mild","source":"patient_reported"}`,
		`{"substance_kind":"non_drug","substance_label":"x","reaction":"rash","severity":"mild","source":"patient_reported"}`,
		`{"substance_kind":"drug_unlisted","substance_label":"x","reaction":"itchy","severity":"mild","source":"patient_reported"}`,
		`{"substance_kind":"drug_unlisted","substance_label":"x","reaction":"rash","severity":"mild","source":"patient_reported","onset_date":"2999-01-01"}`,
	} {
		require.Equal(t, http.StatusBadRequest, do(r, "PUT", base+"/allergies/"+uuid.Must(uuid.NewV7()).String(), "doc", body).Code, body)
	}
}

// Review focus 4: corrections keep history and refuse stale versions;
// recording is idempotent per intent.
func TestAllergyCorrectionsKeepHistory(t *testing.T) {
	r, db, ctx := clinicalSetup(t, true)
	patient := registered(t, db, ctx, testutil.TenantA, hospitalA)
	amox := molecule(t, db, ctx, "Amoxicillin", "J01CA04")
	base := patientURL(hospitalA, patient)
	entry := uuid.Must(uuid.NewV7())
	require.Equal(t, http.StatusCreated, do(r, "PUT", base+"/allergies/"+entry.String(), "doc", codedAllergy(amox)).Code)
	require.Equal(t, http.StatusOK, do(r, "PUT", base+"/allergies/"+entry.String(), "doc", codedAllergy(amox)).Code, "same intent replays")
	require.Equal(t, http.StatusConflict, do(r, "PUT", base+"/allergies/"+entry.String(), "nurse", codedAllergy(amox)).Code, "another recorder is a different intent")

	correction := func(version int) int {
		return do(r, "POST", base+"/allergies/"+entry.String()+"/corrections", "doc",
			fmt.Sprintf(`{"expected_version":%d,"status":"entered_in_error","reason":"wrong patient"}`, version)).Code
	}
	require.Equal(t, http.StatusConflict, correction(2), "stale version")
	require.Equal(t, http.StatusOK, correction(1))
	require.Equal(t, http.StatusConflict, correction(2), "an entry in error is not corrected again")

	w := do(r, "GET", base+"/allergies/"+entry.String()+"/history", "nurse", "")
	require.Equal(t, http.StatusOK, w.Code)
	var h struct {
		Data []struct {
			Version int     `json:"version"`
			Status  string  `json:"status"`
			Reason  *string `json:"reason"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &h))
	require.Len(t, h.Data, 2)
	require.Equal(t, "active", h.Data[0].Status)
	require.Equal(t, "entered_in_error", h.Data[1].Status)
	require.Equal(t, "wrong patient", *h.Data[1].Reason)

	// The database refuses edits and deletes outright.
	err := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE medicore_allergy_entries SET severity = 'mild' WHERE id = ?`, entry).Error
	})
	require.ErrorContains(t, err, "never changed or removed")
}

// Review focus 5: recording waits for clinical approval; reading and
// "no known allergy" do not.
func TestAllergyRecordingIsOffUntilApproved(t *testing.T) {
	r, db, ctx := clinicalSetup(t, false)
	patient := registered(t, db, ctx, testutil.TenantA, hospitalA)
	amox := molecule(t, db, ctx, "Amoxicillin", "J01CA04")
	base := patientURL(hospitalA, patient)
	w := do(r, "PUT", base+"/allergies/"+uuid.Must(uuid.NewV7()).String(), "doc", codedAllergy(amox))
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Contains(t, w.Body.String(), "allergy_recording_disabled")
	off := allergyStatus(t, r, "doc", patient)
	require.Equal(t, "unknown", off.Status)
	require.False(t, off.RecordingEnabled)
	require.Equal(t, http.StatusCreated, do(r, "PUT", base+"/no-known-allergy/"+uuid.Must(uuid.NewV7()).String(), "nurse", `{}`).Code)
}

func TestClinicalRecordsAreScopedToTheHospitalAndPatient(t *testing.T) {
	r, db, ctx := clinicalSetup(t, true)
	patient := registered(t, db, ctx, testutil.TenantA, hospitalA)
	elsewhere := registered(t, db, ctx, testutil.TenantA, hospitalB)
	otherTenant := registered(t, db, ctx, testutil.TenantB, hospitalA)
	for _, p := range []uuid.UUID{elsewhere, otherTenant, uuid.Must(uuid.NewV7())} {
		require.Equal(t, http.StatusNotFound, do(r, "GET", patientURL(hospitalA, p)+"/allergies", "doc", "").Code)
		require.Equal(t, http.StatusNotFound, do(r, "GET", patientURL(hospitalA, p)+"/weights/latest", "doc", "").Code)
	}
	require.Equal(t, http.StatusNotFound, do(r, "GET", patientURL(hospitalB, elsewhere)+"/allergies", "doc", "").Code, "no grant at hospital B")
	require.Equal(t, http.StatusNotFound, do(r, "GET", patientURL(hospitalA, patient)+"/allergies", "docB", "").Code, "another tenant")
	require.Equal(t, http.StatusForbidden, do(r, "GET", patientURL(hospitalA, patient)+"/allergies", "clerk", "").Code, "no clinical permission")

	// An encounter of another patient cannot be named.
	other := registered(t, db, ctx, testutil.TenantA, hospitalA)
	enc := uuid.Must(uuid.NewV7())
	require.Equal(t, http.StatusCreated, do(r, "PUT", encountersURL(hospitalA)+"/"+enc.String(), "doc", openBody(other, "in_person", "first")).Code)
	w := do(r, "PUT", patientURL(hospitalA, patient)+"/weights/"+uuid.Must(uuid.NewV7()).String(), "nurse",
		fmt.Sprintf(`{"value":"20","unit":"kg","measured_at":%q,"method":"measured","encounter_id":%q}`, time.Now().UTC().Format(time.RFC3339), enc))
	require.Equal(t, http.StatusUnprocessableEntity, w.Code, w.Body.String())
}

func TestWeightIsAnAttributedObservation(t *testing.T) {
	r, db, ctx := clinicalSetup(t, true)
	patient := registered(t, db, ctx, testutil.TenantA, hospitalA)
	base := patientURL(hospitalA, patient)
	now := time.Now().UTC()
	record := func(id uuid.UUID, value, unit string, at time.Time) *httpResult {
		w := do(r, "PUT", base+"/weights/"+id.String(), "nurse",
			fmt.Sprintf(`{"value":%q,"unit":%q,"measured_at":%q,"method":"measured"}`, value, unit, at.Format(time.RFC3339)))
		return &httpResult{w.Code, w.Body.Bytes()}
	}
	require.Equal(t, http.StatusNotFound, do(r, "GET", base+"/weights/latest", "doc", "").Code, "no weight is not a weight")

	older := uuid.Must(uuid.NewV7())
	res := record(older, "12500", "g", now.Add(-72*time.Hour))
	require.Equal(t, http.StatusCreated, res.code, string(res.body))
	var got struct {
		WeightKg string `json:"weight_kg"`
	}
	require.NoError(t, json.Unmarshal(res.body, &got))
	require.Equal(t, "12.500", got.WeightKg, "grams become kilograms exactly")

	newer := uuid.Must(uuid.NewV7())
	require.Equal(t, http.StatusCreated, record(newer, "13.1", "kg", now.Add(-time.Hour)).code)
	require.Equal(t, http.StatusOK, record(newer, "13.1", "kg", now.Add(-time.Hour)).code, "replay")
	require.Equal(t, http.StatusConflict, record(newer, "13.2", "kg", now.Add(-time.Hour)).code)

	latest := func() (string, int) {
		w := do(r, "GET", base+"/weights/latest", "doc", "")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var l struct {
			WeightKg string `json:"weight_kg"`
			AgeDays  int    `json:"age_days"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &l))
		return l.WeightKg, l.AgeDays
	}
	kg, age := latest()
	require.Equal(t, "13.100", kg)
	require.Zero(t, age)

	require.Equal(t, http.StatusOK, do(r, "POST", base+"/weights/"+newer.String()+"/entered-in-error", "doc", `{"reason":"scale not zeroed"}`).Code)
	require.Equal(t, http.StatusConflict, do(r, "POST", base+"/weights/"+newer.String()+"/entered-in-error", "doc", `{"reason":"again"}`).Code)
	kg, age = latest()
	require.Equal(t, "12.500", kg, "latest skips a weight entered in error")
	require.Equal(t, 3, age)

	w := do(r, "GET", base+"/weights", "doc", "")
	require.Equal(t, http.StatusOK, w.Code)
	var list struct {
		Data []struct {
			ID             string `json:"id"`
			EnteredInError bool   `json:"entered_in_error"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Len(t, list.Data, 2, "history keeps the weight entered in error")
	require.Equal(t, newer.String(), list.Data[0].ID)
	require.True(t, list.Data[0].EnteredInError)

	for _, tc := range []struct{ value, unit string }{{"0.1", "kg"}, {"501", "kg"}, {"12.5005", "kg"}, {"-3", "kg"}, {"12.5", "lb"}} {
		require.Equal(t, http.StatusBadRequest, record(uuid.Must(uuid.NewV7()), tc.value, tc.unit, now).code, tc)
	}
	require.Equal(t, http.StatusBadRequest, record(uuid.Must(uuid.NewV7()), "13", "kg", now.Add(time.Hour)).code, "a future measurement")
}

type httpResult struct {
	code int
	body []byte
}

func TestOnlyTheMoleculeConsumerProjectsMolecules(t *testing.T) {
	_, db, ctx := clinicalSetup(t, true)
	molecule(t, db, ctx, "Amoxicillin", "J01CA04")
	err := db.WithTenant(tenantdb.WithActor(ctx, "user-doc"), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO medicore_drug_molecules (id, inn_name, atc_code) VALUES (?, 'Fake', '')`, uuid.Must(uuid.NewV7())).Error
	})
	require.ErrorContains(t, err, "row-level security", "a request cannot add a molecule")
	var n int64
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM medicore_drug_molecules`).Scan(&n).Error
	}))
	require.Equal(t, int64(1), n, "redelivery added nothing")
}

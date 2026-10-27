package medicore_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/medicore" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// #80 slice 1: an OPD encounter belongs to one hospital and one patient
// registered there, and only staff granted MediCore access there see it.

var (
	hospitalA = uuid.MustParse("0199e000-0000-7000-8000-00000000a001")
	hospitalB = uuid.MustParse("0199e000-0000-7000-8000-00000000b001")
)

func encounterSetup(t *testing.T) (*gin.Engine, *tenantdb.DB, context.Context) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"doc": testutil.TenantA, "nurse": testutil.TenantA, "admin": testutil.TenantA, "outsider": testutil.TenantA, "docB": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"doc":      {medicore.PermVisitCreate, medicore.PermVisitRead, medicore.PermVisitUpdate},
			"nurse":    {medicore.PermVisitRead, medicore.PermVisitUpdate},
			"admin":    {medicore.PermAccessManage},
			"outsider": {medicore.PermVisitCreate, medicore.PermVisitRead, medicore.PermVisitUpdate},
			"docB":     {medicore.PermVisitCreate, medicore.PermVisitRead, medicore.PermVisitUpdate, medicore.PermAccessManage},
		},
		Modules: []platform.Module{medicore.New()},
	})
	for _, subject := range []string{"user-doc", "user-nurse"} {
		require.Equal(t, http.StatusOK, do(r, "PUT", "/v1/medicore/hospitals/"+hospitalA.String()+"/access/"+subject, "admin", "").Code)
	}
	return r, db, ctx
}

// registered runs medicore's own consumer for a patientmaster registration.
func registered(t *testing.T, db *tenantdb.DB, ctx context.Context, tenant string, hospital uuid.UUID) uuid.UUID {
	t.Helper()
	patient := uuid.Must(uuid.NewV7())
	data, err := json.Marshal(patientmastercontract.PatientRegisteredData{PatientID: patient.String(), HospitalID: hospital.String()})
	require.NoError(t, err)
	evt := events.Event{Type: "PatientRegistered", Version: 1, TenantID: tenant, Data: data}
	handle := medicore.New().Consumers(platform.Deps{})[0].Handle
	for range 2 { // redelivery is a no-op
		require.NoError(t, db.WithTenant(ctx, tenant, func(tx *gorm.DB) error { return handle(ctx, tx, evt) }))
	}
	return patient
}

func openBody(patient uuid.UUID, mode, kind string) string {
	return fmt.Sprintf(`{"patient_id":%q,"consult_mode":%q,"visit_type":%q}`, patient, mode, kind)
}

func encountersURL(hospital uuid.UUID) string {
	return "/v1/medicore/hospitals/" + hospital.String() + "/encounters"
}

type encounterResp struct {
	ID          string  `json:"id"`
	HospitalID  string  `json:"hospital_id"`
	PatientID   string  `json:"patient_id"`
	ConsultMode string  `json:"consult_mode"`
	VisitType   string  `json:"visit_type"`
	Status      string  `json:"status"`
	Version     int     `json:"version"`
	ClosedAt    *string `json:"closed_at"`
}

func decodeEncounter(t *testing.T, body []byte) encounterResp {
	t.Helper()
	var e encounterResp
	require.NoError(t, json.Unmarshal(body, &e))
	return e
}

func outboxCount(t *testing.T, db *tenantdb.DB, ctx context.Context, subject string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM outbox_events WHERE subject = ?`, subject).Scan(&n).Error
	}))
	return n
}

func TestEncounterOpensOnlyForAPatientRegisteredAtThatHospital(t *testing.T) {
	r, db, ctx := encounterSetup(t)
	patient := registered(t, db, ctx, testutil.TenantA, hospitalA)
	elsewhere := registered(t, db, ctx, testutil.TenantA, hospitalB)
	otherTenant := registered(t, db, ctx, testutil.TenantB, hospitalA)

	id := uuid.Must(uuid.NewV7())
	w := do(r, "PUT", encountersURL(hospitalA)+"/"+id.String(), "doc", openBody(patient, "teleconsultation", "first"))
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	got := decodeEncounter(t, w.Body.Bytes())
	require.Equal(t, encounterResp{ID: id.String(), HospitalID: hospitalA.String(), PatientID: patient.String(), ConsultMode: "teleconsultation", VisitType: "first", Status: "arrived", Version: 1}, got)
	require.Equal(t, int64(1), outboxCount(t, db, ctx, "helivanta.in.medicore.encounter_opened.v1"))
	require.Zero(t, outboxCount(t, db, ctx, "helivanta.in.medicore.visit_created.v1"), "an encounter is not a legacy visit")

	// Same intent replays; a different intent under the same id is refused.
	replay := do(r, "PUT", encountersURL(hospitalA)+"/"+id.String(), "doc", openBody(patient, "teleconsultation", "first"))
	require.Equal(t, http.StatusOK, replay.Code)
	require.Equal(t, got, decodeEncounter(t, replay.Body.Bytes()))
	require.Equal(t, http.StatusConflict, do(r, "PUT", encountersURL(hospitalA)+"/"+id.String(), "doc", openBody(patient, "in_person", "first")).Code)
	require.Equal(t, int64(1), outboxCount(t, db, ctx, "helivanta.in.medicore.encounter_opened.v1"))

	// A patient registered at another hospital, in another tenant, or not at all.
	for _, p := range []uuid.UUID{elsewhere, otherTenant, uuid.Must(uuid.NewV7())} {
		w := do(r, "PUT", encountersURL(hospitalA)+"/"+uuid.Must(uuid.NewV7()).String(), "doc", openBody(p, "in_person", "first"))
		require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	}
	// An id that is not a UUIDv7, and an invalid body.
	require.Equal(t, http.StatusNotFound, do(r, "PUT", encountersURL(hospitalA)+"/"+uuid.New().String(), "doc", openBody(patient, "in_person", "first")).Code)
	require.Equal(t, http.StatusBadRequest, do(r, "PUT", encountersURL(hospitalA)+"/"+uuid.Must(uuid.NewV7()).String(), "doc", openBody(patient, "video", "first")).Code)
}

func TestEncounterNeedsAHospitalGrant(t *testing.T) {
	r, db, ctx := encounterSetup(t)
	patient := registered(t, db, ctx, testutil.TenantA, hospitalA)
	id := uuid.Must(uuid.NewV7())
	require.Equal(t, http.StatusCreated, do(r, "PUT", encountersURL(hospitalA)+"/"+id.String(), "doc", openBody(patient, "in_person", "first")).Code)

	// Same tenant and permissions, no grant: every route is 404, even for a known id.
	for _, req := range [][3]string{
		{"GET", "/v1/medicore/hospitals/" + hospitalA.String() + "/access", ""},
		{"GET", encountersURL(hospitalA), ""},
		{"GET", encountersURL(hospitalA) + "/" + id.String(), ""},
		{"PUT", encountersURL(hospitalA) + "/" + uuid.Must(uuid.NewV7()).String(), openBody(patient, "in_person", "first")},
		{"POST", encountersURL(hospitalA) + "/" + id.String() + "/transitions", `{"to":"in_consultation","expected_version":1}`},
	} {
		require.Equal(t, http.StatusNotFound, do(r, req[0], req[1], "outsider", req[2]).Code, req[1])
	}
	// Another tenant holding a grant for the same hospital id sees nothing of tenant A's.
	require.Equal(t, http.StatusOK, do(r, "PUT", "/v1/medicore/hospitals/"+hospitalA.String()+"/access/user-docB", "docB", "").Code)
	require.Equal(t, http.StatusNotFound, do(r, "GET", encountersURL(hospitalA)+"/"+id.String(), "docB", "").Code)
	require.NotContains(t, do(r, "GET", encountersURL(hospitalA), "docB", "").Body.String(), id.String())

	// Revoking takes effect on the next request.
	require.Equal(t, http.StatusOK, do(r, "DELETE", "/v1/medicore/hospitals/"+hospitalA.String()+"/access/user-doc", "admin", "").Code)
	require.Equal(t, http.StatusNotFound, do(r, "GET", encountersURL(hospitalA)+"/"+id.String(), "doc", "").Code)

	// The legacy visit list never shows an encounter.
	require.NotContains(t, do(r, "GET", "/v1/medicore/visits", "nurse", "").Body.String(), id.String())
}

func TestEncounterMovesAlongItsLifecycleOnly(t *testing.T) {
	r, db, ctx := encounterSetup(t)
	patient := registered(t, db, ctx, testutil.TenantA, hospitalA)
	id := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, http.StatusCreated, do(r, "PUT", encountersURL(hospitalA)+"/"+id, "doc", openBody(patient, "in_person", "follow_up")).Code)
	move := func(token, body string) *httptest.ResponseRecorder {
		return do(r, "POST", encountersURL(hospitalA)+"/"+id+"/transitions", token, body)
	}

	require.Equal(t, http.StatusConflict, move("doc", `{"to":"closed","expected_version":1}`).Code, "arrived cannot close")
	require.Equal(t, http.StatusBadRequest, move("doc", `{"to":"cancelled","expected_version":1}`).Code, "cancelling needs a reason")
	w := move("nurse", `{"to":"in_consultation","expected_version":1}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "in_consultation", decodeEncounter(t, w.Body.Bytes()).Status)
	require.Equal(t, http.StatusConflict, move("doc", `{"to":"closed","expected_version":1}`).Code, "stale version")

	w = move("doc", `{"to":"closed","expected_version":2}`)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	closed := decodeEncounter(t, w.Body.Bytes())
	require.Equal(t, "closed", closed.Status)
	require.Equal(t, 3, closed.Version)
	require.NotNil(t, closed.ClosedAt)
	require.Equal(t, http.StatusConflict, move("doc", `{"to":"cancelled","expected_version":3,"reason":"x"}`).Code, "closed is final")
	require.Equal(t, int64(2), outboxCount(t, db, ctx, "helivanta.in.medicore.encounter_status_changed.v1"))

	// Filtered list.
	require.Contains(t, do(r, "GET", encountersURL(hospitalA)+"?status=closed", "nurse", "").Body.String(), id)
	require.NotContains(t, do(r, "GET", encountersURL(hospitalA)+"?status=arrived", "nurse", "").Body.String(), id)
	require.Equal(t, http.StatusBadRequest, do(r, "GET", encountersURL(hospitalA)+"?status=open", "nurse", "").Code)

	// The database guard itself refuses any change that bypasses the
	// transition path, and keeps the history (#80 E5). Each statement would
	// pass every other constraint, so only the guard can be what stops it.
	live := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, http.StatusCreated, do(r, "PUT", encountersURL(hospitalA)+"/"+live, "doc", openBody(patient, "in_person", "first")).Code)
	require.Equal(t, http.StatusOK, do(r, "POST", encountersURL(hospitalA)+"/"+live+"/transitions", "doc", `{"to":"in_consultation","expected_version":1}`).Code)
	actor := tenantdb.WithActor(ctx, "user-doc")
	for sql, message := range map[string]string{
		`UPDATE medicore_visits SET status = 'closed', version = version + 1, closed_at = now() WHERE id = '` + live + `'`: "encounter changes only by an attributable transition",
		`UPDATE medicore_visits SET consult_mode = 'teleconsultation' WHERE id = '` + live + `'`:                           "encounter changes only by an attributable transition",
		`DELETE FROM medicore_visits WHERE id = '` + id + `'`:                                                              "encounter history is retained",
		`DELETE FROM medicore_encounter_transitions WHERE encounter_id = '` + id + `'`:                                     "encounter history is retained",
		// A history row for a move that never happened (review minor).
		`INSERT INTO medicore_encounter_transitions (tenant_id, encounter_id, version, from_status, to_status, actor_subject)
		 VALUES ('` + testutil.TenantA + `', '` + live + `', 3, 'arrived', 'in_consultation', 'user-doc')`: "transition does not match the encounter",
	} {
		err := db.WithTenant(actor, testutil.TenantA, func(tx *gorm.DB) error { return tx.Exec(sql).Error })
		var pgErr *pgconn.PgError
		require.ErrorAs(t, err, &pgErr, sql)
		require.Equal(t, "23514", pgErr.Code, sql)
		require.Equal(t, message, pgErr.Message, sql)
	}
	var history []string
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT from_status || '>' || to_status || ':' || actor_subject FROM medicore_encounter_transitions WHERE encounter_id = ? ORDER BY version`, id).Scan(&history).Error
	}))
	require.Equal(t, []string{"arrived>in_consultation:user-nurse", "in_consultation>closed:user-doc"}, history)
}

// Two concurrent opens under one id: the one that loses the insert race
// replays the winner instead of failing (review finding 2). The race is
// forced, not hoped for: the winner's row is inserted and held uncommitted
// while the request runs, so the request misses it on read and then waits
// on its insert until the winner commits.
func TestAnOpenThatLosesTheInsertRaceReplays(t *testing.T) {
	r, db, ctx := encounterSetup(t)
	patient := registered(t, db, ctx, testutil.TenantA, hospitalA)
	id := uuid.Must(uuid.NewV7())
	sum := sha256.Sum256([]byte(strings.Join([]string{hospitalA.String(), patient.String(), "in_person", "first"}, "|")))
	inserted, release, committed := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		committed <- db.WithTenant(tenantdb.WithActor(ctx, "user-doc"), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`INSERT INTO medicore_visits (id, tenant_id, hospital_id, patient_id, department, consult_mode, visit_type, status, version, created_by, intent_sha256)
				VALUES (?, ?, ?, ?, 'OPD', 'in_person', 'first', 'arrived', 1, 'user-doc', ?)`, id, testutil.TenantA, hospitalA, patient, hex.EncodeToString(sum[:])).Error; err != nil {
				close(inserted)
				return err
			}
			close(inserted)
			<-release
			return nil
		})
	}()
	<-inserted
	response := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response <- do(r, "PUT", encountersURL(hospitalA)+"/"+id.String(), "doc", openBody(patient, "in_person", "first"))
	}()
	time.Sleep(500 * time.Millisecond) // the request is now blocked on the winner's key
	close(release)
	require.NoError(t, <-committed)
	w := <-response
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, id.String(), decodeEncounter(t, w.Body.Bytes()).ID)
	require.Zero(t, outboxCount(t, db, ctx, "helivanta.in.medicore.encounter_opened.v1"), "the loser publishes nothing")
}

// Legacy visits stay creatable after the migration, never appear on hospital
// routes and cannot be moved through them (spec Verification).
func TestLegacyVisitsStayLegacy(t *testing.T) {
	r, _, _ := encounterSetup(t)
	w := do(r, "POST", "/v1/medicore/visits", "doc", `{"patient_name":"TEST Legacy","department":"IPD"}`)
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
	var created struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	require.Contains(t, do(r, "GET", "/v1/medicore/visits", "doc", "").Body.String(), "TEST Legacy")
	require.NotContains(t, do(r, "GET", encountersURL(hospitalA), "doc", "").Body.String(), created.ID)
	require.Equal(t, http.StatusNotFound, do(r, "GET", encountersURL(hospitalA)+"/"+created.ID, "doc", "").Code)
	require.Equal(t, http.StatusNotFound, do(r, "POST", encountersURL(hospitalA)+"/"+created.ID+"/transitions", "doc", `{"to":"in_consultation","expected_version":1}`).Code)
}

// An unknown status is a bad request, not a conflict (review minor).
func TestUnknownTransitionTargetIsABadRequest(t *testing.T) {
	r, db, ctx := encounterSetup(t)
	patient := registered(t, db, ctx, testutil.TenantA, hospitalA)
	id := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, http.StatusCreated, do(r, "PUT", encountersURL(hospitalA)+"/"+id, "doc", openBody(patient, "in_person", "first")).Code)
	require.Equal(t, http.StatusBadRequest, do(r, "POST", encountersURL(hospitalA)+"/"+id+"/transitions", "doc", `{"to":"discharged","expected_version":1}`).Code)
}

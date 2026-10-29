package medicore_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/medicore" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// #106 slice 1: beds, admissions and who occupies a bed.

type bedWorld struct {
	r   *gin.Engine
	db  *tenantdb.DB
	ctx context.Context
}

func newBedWorld(t *testing.T) bedWorld {
	t.Helper()
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA, "desk": testutil.TenantA, "nurse": testutil.TenantA, "reader": testutil.TenantA, "outsider": testutil.TenantA, "deskB": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"admin":    {medicore.PermAccessManage, medicore.PermBedConfigure, medicore.PermVisitRead},
			"desk":     {medicore.PermAdmissionManage, medicore.PermVisitRead},
			"nurse":    {medicore.PermAdmissionManage, medicore.PermVisitRead},
			"reader":   {medicore.PermVisitRead},
			"outsider": {medicore.PermAdmissionManage, medicore.PermVisitRead},
			"deskB":    {medicore.PermAdmissionManage, medicore.PermVisitRead},
		},
		Modules: []platform.Module{medicore.New()},
	})
	for _, subject := range []string{"user-admin", "user-desk", "user-nurse", "user-reader"} {
		require.Equal(t, http.StatusOK, do(r, "PUT", "/v1/medicore/hospitals/"+hospitalA.String()+"/access/"+subject, "admin", "").Code)
	}
	return bedWorld{r, db, ctx}
}

func newID() string { return uuid.Must(uuid.NewV7()).String() }

func (w bedWorld) url(path string) string {
	return "/v1/medicore/hospitals/" + hospitalA.String() + path
}

// bed sets up a ward, room and bed and returns the bed's id and room's id.
func (w bedWorld) room(t *testing.T, code string, beds ...string) (room string, ids []string) {
	t.Helper()
	ward, room := newID(), newID()
	res := do(w.r, "PUT", w.url("/wards/"+ward), "admin", fmt.Sprintf(`{"code":"W-%s","name":"Ward %s"}`, code, code))
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	res = do(w.r, "PUT", w.url("/wards/"+ward+"/rooms/"+room), "admin", fmt.Sprintf(`{"code":"R-%s"}`, code))
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	for _, b := range beds {
		id := newID()
		res = do(w.r, "PUT", w.url("/rooms/"+room+"/beds/"+id), "admin", fmt.Sprintf(`{"code":%q,"bed_class":"general"}`, b))
		require.Equal(t, http.StatusOK, res.Code, res.Body.String())
		ids = append(ids, id)
	}
	return room, ids
}

type bedState struct {
	State       string  `json:"state"`
	Version     int     `json:"version"`
	AdmissionID *string `json:"admission_id"`
	PatientID   *string `json:"patient_id"`
}

func (w bedWorld) bed(t *testing.T, id string) bedState {
	t.Helper()
	res := do(w.r, "GET", w.url("/beds?limit=200"), "reader", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var page struct {
		Data []struct {
			ID string `json:"id"`
			bedState
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &page))
	for _, b := range page.Data {
		if b.ID == id {
			return b.bedState
		}
	}
	t.Fatalf("bed %s not on the board", id)
	return bedState{}
}

func (w bedWorld) admit(token, admission string, patient uuid.UUID, bed string, version int) (int, string) {
	res := do(w.r, "PUT", w.url("/admissions/"+admission), token, fmt.Sprintf(`{"patient_id":%q,"bed_id":%q,"intent":"emergency","expected_bed_version":%d}`, patient, bed, version))
	return res.Code, res.Body.String()
}

func (w bedWorld) occupant(t *testing.T, path string) (int, string) {
	t.Helper()
	res := do(w.r, "GET", w.url(path), "reader", "")
	return res.Code, res.Body.String()
}

func TestBedsAdmitTransferAndRelease(t *testing.T) {
	w := newBedWorld(t)
	patient := registered(t, w.db, w.ctx, testutil.TenantA, hospitalA)
	elsewhere := registered(t, w.db, w.ctx, testutil.TenantA, hospitalB)
	roomA, beds := w.room(t, "A", "A-1", "A-2")
	_, other := w.room(t, "B", "B-1")
	a1, a2, b1 := beds[0], beds[1], other[0]
	require.Equal(t, "available", w.bed(t, a1).State)
	rooms := do(w.r, "GET", w.url("/rooms"), "reader", "")
	require.Equal(t, http.StatusOK, rooms.Code, rooms.Body.String())
	require.Contains(t, rooms.Body.String(), `"code":"R-A"`)
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", w.url("/rooms"), "outsider", "").Code)

	// Setup is idempotent and identity is fixed.
	res := do(w.r, "PUT", w.url("/rooms/"+roomA+"/beds/"+a1), "admin", `{"code":"A-1","bed_class":"general"}`)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.Equal(t, http.StatusConflict, do(w.r, "PUT", w.url("/rooms/"+roomA+"/beds/"+a1), "admin", `{"code":"A-1","bed_class":"icu"}`).Code)
	require.Equal(t, http.StatusConflict, do(w.r, "PUT", w.url("/rooms/"+roomA+"/beds/"+newID()), "admin", `{"code":"A-1","bed_class":"general"}`).Code, "a code is used once per hospital")
	require.Equal(t, http.StatusForbidden, do(w.r, "PUT", w.url("/rooms/"+roomA+"/beds/"+newID()), "desk", `{"code":"A-9","bed_class":"general"}`).Code)
	require.Equal(t, http.StatusConflict, do(w.r, "PUT", w.url("/wards/"+newID()), "admin", `{"code":"W-A","name":"Ward A"}`).Code, "a ward code is used once per hospital")

	code, out := w.occupant(t, "/beds/"+a1+"/occupant")
	require.Equal(t, http.StatusNotFound, code, out)
	require.Contains(t, out, "no_occupant")

	admission := newID()
	code, out = w.admit("reader", admission, patient, a1, 1)
	require.Equal(t, http.StatusForbidden, code, out)
	code, out = w.admit("outsider", admission, patient, a1, 1)
	require.Equal(t, http.StatusNotFound, code, out)
	require.Contains(t, out, "hospital not found", "no MediCore grant at the hospital")
	code, out = w.admit("desk", admission, elsewhere, a1, 1)
	require.Equal(t, http.StatusNotFound, code, out)
	require.Contains(t, out, "patient not found", "a patient of another hospital")
	code, out = w.admit("desk", admission, patient, a1, 9)
	require.Equal(t, http.StatusConflict, code, out)
	require.Contains(t, out, "stale_bed")

	code, out = w.admit("desk", admission, patient, a1, 1)
	require.Equal(t, http.StatusCreated, code, out)
	replay, again := w.admit("desk", admission, patient, a1, 1)
	require.Equal(t, http.StatusOK, replay, again)
	code, _ = w.admit("desk", admission, patient, a2, 1)
	require.Equal(t, http.StatusConflict, code, "the same admission id with another bed")
	code, out = w.admit("nurse", newID(), patient, a2, 1)
	require.Equal(t, http.StatusConflict, code, out)
	require.Contains(t, out, "already_admitted")
	second := registered(t, w.db, w.ctx, testutil.TenantA, hospitalA)
	code, out = w.admit("nurse", newID(), second, a1, 2)
	require.Equal(t, http.StatusConflict, code, out)
	require.Contains(t, out, "bed_not_available")

	bed := w.bed(t, a1)
	require.Equal(t, "occupied", bed.State)
	require.Equal(t, admission, *bed.AdmissionID)
	code, out = w.occupant(t, "/beds/"+a1+"/occupant")
	require.Equal(t, http.StatusOK, code, out)
	require.Contains(t, out, `"admission_id":"`+admission+`"`)
	require.Contains(t, out, `"patient_id":"`+patient.String()+`"`)
	code, out = w.occupant(t, "/rooms/"+roomA+"/occupant")
	require.Equal(t, http.StatusOK, code, out, "one occupied bed in the room")

	// A second occupied bed makes the room ambiguous, never a guess.
	code, out = w.admit("nurse", newID(), second, a2, 1)
	require.Equal(t, http.StatusCreated, code, out)
	code, out = w.occupant(t, "/rooms/"+roomA+"/occupant")
	require.Equal(t, http.StatusConflict, code, out)
	require.Contains(t, out, "ambiguous")
	require.NotContains(t, out, patient.String())

	// Transfer: the source closes and the target opens at one instant.
	transfer := newID()
	res = do(w.r, "PUT", w.url("/admissions/"+admission+"/transfers/"+transfer), "nurse", fmt.Sprintf(`{"to_bed_id":%q,"expected_bed_version":1}`, b1))
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.url("/admissions/"+admission+"/transfers/"+transfer), "nurse", fmt.Sprintf(`{"to_bed_id":%q,"expected_bed_version":1}`, b1)).Code, "a retried transfer replays")
	var adm struct {
		Occupancies []struct {
			BedID     string  `json:"bed_id"`
			StartedAt string  `json:"started_at"`
			EndedAt   *string `json:"ended_at"`
			End       *string `json:"end"`
		} `json:"occupancies"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &adm))
	require.Len(t, adm.Occupancies, 2)
	require.Equal(t, a1, adm.Occupancies[0].BedID)
	require.Equal(t, "transfer", *adm.Occupancies[0].End)
	require.Equal(t, *adm.Occupancies[0].EndedAt, adm.Occupancies[1].StartedAt, "no gap and no overlap")
	require.Nil(t, adm.Occupancies[1].EndedAt)
	require.Equal(t, "cleaning", w.bed(t, a1).State, "a vacated bed is never immediately available")
	require.Equal(t, "occupied", w.bed(t, b1).State)

	// Release ends the stay; the bed waits for cleaning.
	require.Equal(t, http.StatusBadRequest, do(w.r, "PUT", w.url("/admissions/"+admission+"/release"), "nurse", `{"reason":"other"}`).Code, "other needs a note")
	res = do(w.r, "PUT", w.url("/admissions/"+admission+"/release"), "nurse", `{"reason":"discharged"}`)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), `"status":"released"`)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", w.url("/admissions/"+admission+"/release"), "nurse", `{"reason":"discharged"}`).Code, "a retried release replays")
	require.Equal(t, http.StatusConflict, do(w.r, "PUT", w.url("/admissions/"+admission+"/release"), "nurse", `{"reason":"died"}`).Code)
	require.Equal(t, "cleaning", w.bed(t, b1).State)
	code, _ = w.occupant(t, "/beds/"+b1+"/occupant")
	require.Equal(t, http.StatusNotFound, code)

	// Ready, out of service and back: only the legal moves.
	move := func(bed, to string, reason *string, version int) (int, string) {
		body := fmt.Sprintf(`{"to":%q,"expected_version":%d}`, to, version)
		if reason != nil {
			body = fmt.Sprintf(`{"to":%q,"reason":%q,"expected_version":%d}`, to, *reason, version)
		}
		res := do(w.r, "PUT", w.url("/beds/"+bed+"/transitions/"+newID()), "nurse", body)
		return res.Code, res.Body.String()
	}
	broken := "TEST oxygen point broken"
	v := w.bed(t, b1).Version
	code, out = move(b1, "available", nil, v)
	require.Equal(t, http.StatusOK, code, out)
	code, out = move(b1, "out_of_service", &broken, v+1)
	require.Equal(t, http.StatusOK, code, out)
	code, out = move(b1, "available", nil, v+2)
	require.Equal(t, http.StatusConflict, code, "out of service returns through cleaning")
	require.Contains(t, out, "illegal_move")
	code, _ = move(b1, "out_of_service", nil, v+2)
	require.Equal(t, http.StatusBadRequest, code, "a reason is required")
	occupiedVersion := w.bed(t, a2).Version
	code, _ = move(a2, "out_of_service", &broken, occupiedVersion)
	require.Equal(t, http.StatusConflict, code, "an occupied bed cannot be taken out of service")

	// Events carry ids and times, no names, once per change.
	require.EqualValues(t, 3, outboxCount(t, w.db, w.ctx, "helivanta.in.medicore.bed_occupancy_started.v1"), "two admissions and one transfer in")
	require.EqualValues(t, 2, outboxCount(t, w.db, w.ctx, "helivanta.in.medicore.bed_occupancy_ended.v1"))
	require.EqualValues(t, 1, outboxCount(t, w.db, w.ctx, "helivanta.in.medicore.admission_released.v1"))
	var started []string
	require.NoError(t, w.db.WithTenant(w.ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT payload->'data'->>'bed_code' FROM outbox_events WHERE subject='helivanta.in.medicore.bed_occupancy_started.v1' AND payload->'data'->>'admission_id'=? ORDER BY created_at`, admission).Scan(&started).Error
	}))
	require.Equal(t, []string{"A-1", "B-1"}, started, "each stay names the bed code staff use")

	// Another tenant, and a reader without a grant, see nothing.
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", w.url("/beds"), "deskB", "").Code)
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", w.url("/admissions/"+admission), "outsider", "").Code)
}

func TestBedsHoldTheirRulesUnderConcurrencyAndInTheDatabase(t *testing.T) {
	w := newBedWorld(t)
	_, beds := w.room(t, "C", "C-1", "C-2")
	c1, c2 := beds[0], beds[1]

	// Two admissions racing for one bed: exactly one wins.
	first, second := registered(t, w.db, w.ctx, testutil.TenantA, hospitalA), registered(t, w.db, w.ctx, testutil.TenantA, hospitalA)
	var wg sync.WaitGroup
	codes := make([]int, 2)
	bodies := make([]string, 2)
	for i, p := range []uuid.UUID{first, second} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], bodies[i] = w.admit("desk", newID(), p, c1, 1)
		}()
	}
	wg.Wait()
	require.ElementsMatch(t, []int{http.StatusCreated, http.StatusConflict}, codes)
	loser := bodies[0]
	if codes[0] == http.StatusCreated {
		loser = bodies[1]
	}
	require.Contains(t, loser, "stale_bed", "the loser is told the bed moved, nothing about codes")

	// One patient admitted twice at once: exactly one admission.
	third := registered(t, w.db, w.ctx, testutil.TenantA, hospitalA)
	_, more := w.room(t, "D", "D-1")
	for i, bed := range []string{c2, more[0]} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], _ = w.admit("desk", newID(), third, bed, 1)
		}()
	}
	wg.Wait()
	require.ElementsMatch(t, []int{http.StatusCreated, http.StatusConflict}, codes)

	// A bed with a finished stay: admit to it, then transfer out.
	fourth := registered(t, w.db, w.ctx, testutil.TenantA, hospitalA)
	_, pair := w.room(t, "F", "F-1", "F-2")
	vacated := pair[0]
	stayID := newID()
	code, out := w.admit("desk", stayID, fourth, vacated, 1)
	require.Equal(t, http.StatusCreated, code, out)
	res := do(w.r, "PUT", w.url("/admissions/"+stayID+"/transfers/"+newID()), "desk", fmt.Sprintf(`{"to_bed_id":%q,"expected_bed_version":1}`, pair[1]))
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())

	asDesk := func(fn func(tx *gorm.DB) error) error {
		return w.db.WithTenant(tenantdb.WithActor(w.ctx, "user-desk"), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.medicore_subject','user-desk',true)`).Error; err != nil {
				return err
			}
			return fn(tx)
		})
	}
	_, spare := w.room(t, "E", "E-1")
	require.Error(t, asDesk(func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE medicore_beds SET state='out_of_service',version=version+1 WHERE id=?`, spare[0]).Error
	}), "an empty bed moves only with a legal transition row")
	visible := func(subject string) int64 {
		var n int64
		require.NoError(t, w.db.WithTenant(w.ctx, testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.medicore_subject',?,true)`, subject).Error; err != nil {
				return err
			}
			return tx.Raw(`SELECT count(*) FROM medicore_beds`).Scan(&n).Error
		}))
		return n
	}
	require.Positive(t, visible("user-desk"))
	require.Zero(t, visible("user-outsider"), "row security shows no bed outside the subject's grants")
	require.Error(t, asDesk(func(tx *gorm.DB) error {
		return tx.Exec(`DELETE FROM medicore_bed_transitions`).Error
	}), "bed history is retained")
	require.Error(t, asDesk(func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE medicore_bed_occupancies SET started_at=started_at-interval '1 day'`).Error
	}), "an occupancy's start is fixed")
	require.Error(t, asDesk(func(tx *gorm.DB) error {
		var b struct{ Version int }
		if err := tx.Raw(`SELECT version FROM medicore_beds WHERE id=?`, c1).Scan(&b).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO medicore_bed_transitions (tenant_id,hospital_id,id,bed_id,version,from_state,to_state,cause,actor_subject) VALUES (?,?,?,?,?,'occupied','cleaning','release','user-desk')`,
			testutil.TenantA, hospitalA, newID(), c1, b.Version+1).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE medicore_beds SET state='cleaning',version=version+1 WHERE id=?`, c1).Error
	}), "a bed cannot leave occupied while its occupancy is open")
	require.Error(t, asDesk(func(tx *gorm.DB) error {
		var open struct {
			AdmissionID string
			StartedAt   time.Time
		}
		if err := tx.Raw(`SELECT admission_id,started_at FROM medicore_bed_occupancies WHERE bed_id=? AND ended_at IS NULL`, c1).Scan(&open).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO medicore_bed_occupancies (tenant_id,hospital_id,id,bed_id,admission_id,started_at,ended_at,ended_by,ended_by_cause,started_by) VALUES (?,?,?,?,?,?,?,'user-desk','release','user-desk')`,
			testutil.TenantA, hospitalA, newID(), c1, open.AdmissionID, open.StartedAt.Add(-time.Hour), open.StartedAt.Add(time.Minute)).Error
	}), "stays in one bed never overlap")
	fresh := registered(t, w.db, w.ctx, testutil.TenantA, hospitalA)
	require.ErrorContains(t, asDesk(func(tx *gorm.DB) error {
		// A new admission with no stays, starting inside the vacated bed's
		// finished stay: only the bed's no-overlap rule stands in the way.
		admission := newID()
		if err := tx.Exec(`INSERT INTO medicore_admissions (tenant_id,hospital_id,id,patient_id,intent,intent_sha256,admitted_by) VALUES (?,?,?,?,'elective',repeat('a',64),'user-desk')`,
			testutil.TenantA, hospitalA, admission, fresh).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO medicore_bed_occupancies (tenant_id,hospital_id,id,bed_id,admission_id,started_at,started_by) SELECT tenant_id,hospital_id,?,bed_id,?,started_at+interval '1 millisecond','user-desk' FROM medicore_bed_occupancies WHERE bed_id=? AND ended_at IS NOT NULL LIMIT 1`,
			newID(), admission, vacated).Error
	}), "medicore_occupancy_bed_overlap", "a new stay cannot start inside a bed's earlier one")
	require.Error(t, asDesk(func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE medicore_admissions SET status='released',release_reason='discharged',released_by='user-desk',released_at=now() WHERE patient_id=?`, third).Error
	}), "an admission is released only out of its bed")

	// A transition the bed never made cannot commit (I-1).
	require.Error(t, asDesk(func(tx *gorm.DB) error {
		var b struct{ Version int }
		if err := tx.Raw(`SELECT version FROM medicore_beds WHERE id=?`, spare[0]).Scan(&b).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO medicore_bed_transitions (tenant_id,hospital_id,id,bed_id,version,from_state,to_state,cause,reason,actor_subject) VALUES (?,?,?,?,?,'available','out_of_service','out_of_service','TEST','user-desk')`,
			testutil.TenantA, hospitalA, newID(), spare[0], b.Version+1).Error
	}), "an unapplied transition is refused at commit")
	require.Equal(t, "available", w.bed(t, spare[0]).State)
	code, out = do(w.r, "PUT", w.url("/beds/"+spare[0]+"/transitions/"+newID()), "desk", `{"to":"out_of_service","reason":"TEST","expected_version":1}`).Code, ""
	require.Equal(t, http.StatusOK, code, "the bed is not stuck")

	// An occupancy agrees with its bed and its admission (I-2).
	cleaningBed := pair[0]
	require.Equal(t, "cleaning", w.bed(t, cleaningBed).State)
	open := registered(t, w.db, w.ctx, testutil.TenantA, hospitalA)
	require.Error(t, asDesk(func(tx *gorm.DB) error {
		admission := newID()
		if err := tx.Exec(`INSERT INTO medicore_admissions (tenant_id,hospital_id,id,patient_id,intent,intent_sha256,admitted_by) VALUES (?,?,?,?,'elective',repeat('a',64),'user-desk')`,
			testutil.TenantA, hospitalA, admission, open).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO medicore_bed_occupancies (tenant_id,hospital_id,id,bed_id,admission_id,started_at,started_by) VALUES (?,?,?,?,?,clock_timestamp(),'user-desk')`,
			testutil.TenantA, hospitalA, newID(), cleaningBed, admission).Error
	}), "a stay cannot open in a bed that is not occupied")
	res = do(w.r, "PUT", w.url("/admissions/"+stayID+"/release"), "desk", `{"reason":"discharged"}`)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	_, empty := w.room(t, "G", "G-1")
	require.Error(t, asDesk(func(tx *gorm.DB) error {
		var b struct{ Version int }
		if err := tx.Raw(`SELECT version FROM medicore_beds WHERE id=?`, empty[0]).Scan(&b).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO medicore_bed_transitions (tenant_id,hospital_id,id,bed_id,version,from_state,to_state,cause,actor_subject) VALUES (?,?,?,?,?,'available','occupied','admission','user-desk')`,
			testutil.TenantA, hospitalA, newID(), empty[0], b.Version+1).Error; err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE medicore_beds SET state='occupied',version=version+1 WHERE id=?`, empty[0]).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO medicore_bed_occupancies (tenant_id,hospital_id,id,bed_id,admission_id,started_at,started_by) VALUES (?,?,?,?,?,clock_timestamp(),'user-desk')`,
			testutil.TenantA, hospitalA, newID(), empty[0], stayID).Error
	}), "a released admission takes no bed")
	require.Error(t, asDesk(func(tx *gorm.DB) error {
		return tx.Exec(`DELETE FROM medicore_bed_occupancies`).Error
	}), "stays are retained")
	require.Error(t, asDesk(func(tx *gorm.DB) error {
		return tx.Exec(`DELETE FROM medicore_admissions`).Error
	}), "admissions are retained")

	// A transfer racing a release: one wins, the other is refused, state agrees.
	racer := registered(t, w.db, w.ctx, testutil.TenantA, hospitalA)
	_, race := w.room(t, "H", "H-1", "H-2")
	raced := newID()
	code, out = w.admit("desk", raced, racer, race[0], 1)
	require.Equal(t, http.StatusCreated, code, out)
	results := make([]int, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		results[0] = do(w.r, "PUT", w.url("/admissions/"+raced+"/transfers/"+newID()), "desk", fmt.Sprintf(`{"to_bed_id":%q,"expected_bed_version":1}`, race[1])).Code
	}()
	go func() {
		defer wg.Done()
		results[1] = do(w.r, "PUT", w.url("/admissions/"+raced+"/release"), "nurse", `{"reason":"discharged"}`).Code
	}()
	wg.Wait()
	// Either order is legal: transfer then release (both succeed, and the
	// patient leaves the second bed), or release then transfer (refused).
	// Whichever ran first, state must agree with it.
	require.Equal(t, http.StatusOK, results[1], "the release succeeds in either order")
	require.Equal(t, "cleaning", w.bed(t, race[0]).State)
	switch results[0] {
	case http.StatusOK:
		require.Equal(t, "cleaning", w.bed(t, race[1]).State, "transferred, then released from the second bed")
	case http.StatusConflict:
		require.Equal(t, "available", w.bed(t, race[1]).State, "released first; the transfer never happened")
	default:
		t.Fatalf("transfer answered %d", results[0])
	}
	var state struct {
		Status string
		Open   int64
	}
	require.NoError(t, asDesk(func(tx *gorm.DB) error {
		return tx.Raw(`SELECT a.status,(SELECT count(*) FROM medicore_bed_occupancies o WHERE o.admission_id=a.id AND o.ended_at IS NULL) AS open FROM medicore_admissions a WHERE a.id=?`, raced).Scan(&state).Error
	}))
	require.Equal(t, "released", state.Status)
	require.Zero(t, state.Open, "no stay is left open")

	// Two identical transfers at once: the second replays the first.
	again := registered(t, w.db, w.ctx, testutil.TenantA, hospitalA)
	_, dup := w.room(t, "J", "J-1", "J-2")
	moved := newID()
	code, out = w.admit("desk", moved, again, dup[0], 1)
	require.Equal(t, http.StatusCreated, code, out)
	transfer, body := newID(), fmt.Sprintf(`{"to_bed_id":%q,"expected_bed_version":1}`, dup[1])
	wg.Add(2)
	for i := range 2 {
		go func() {
			defer wg.Done()
			results[i] = do(w.r, "PUT", w.url("/admissions/"+moved+"/transfers/"+transfer), "desk", body).Code
		}()
	}
	wg.Wait()
	require.Equal(t, []int{http.StatusOK, http.StatusOK}, results, "a concurrent identical retry replays")
}

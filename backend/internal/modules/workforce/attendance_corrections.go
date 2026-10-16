package workforce

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func (r attendanceCorrection) PageKey() (time.Time, uuid.UUID)      { return r.CreatedAt, r.ID }
func (e attendanceCorrectionEvent) PageKey() (time.Time, uuid.UUID) { return e.CreatedAt, e.ID }

var (
	errCorrectionInvalid      = &rosterProblem{400, "invalid_request", "An add needs a direction and a whole-minute time between 2000 and 2199, no more than five minutes ahead; a void needs the punch to void; a retime needs both. Give a reason of 1 to 500 characters."}
	errCorrectionTarget       = &rosterProblem{400, "invalid_request", "Choose a punch of the same person to correct."}
	errCorrectionNote         = &rosterProblem{400, "invalid_request", "Use a note of at most 500 characters; rejecting a correction needs one."}
	errCorrectionReused       = &rosterProblem{409, "request_reused", "This correction id was already used with different values."}
	errCorrectionStale        = &rosterProblem{409, "stale_version", "The correction changed since you opened it. Reload before acting."}
	errCorrectionState        = &rosterProblem{409, "request_state", "The correction is no longer pending."}
	errCorrectionExists       = &rosterProblem{409, "correction_exists", "This punch already has a pending or approved correction."}
	errCorrectionTargetVoided = &rosterProblem{409, "punch_voided", "The punch to correct has already been voided."}
	errCorrectionSelfDecision = errors.New("corrections are decided by someone other than the filer")
)

// correctionDuplicate names the pending correction that would add the same punch.
type correctionDuplicate struct{ id uuid.UUID }

func (correctionDuplicate) Error() string { return "duplicate correction" }

const correctionView = `(SELECT c.tenant_id,c.hospital_id,c.id,c.staff_id,s.employee_code,s.display_name,c.kind,c.target_punch_id,t.direction AS target_direction,t.punched_at AT TIME ZONE 'UTC' AS target_punched_at,
 c.direction,c.punched_at AT TIME ZONE 'UTC' AS punched_at,c.local_date::text AS local_date,c.timezone,c.reason,c.status,c.requested_by_subject,c.decided_by_subject,c.decided_at,
 a.id AS applied_punch_id,c.version,c.created_at,c.updated_at
 FROM workforce_attendance_corrections c JOIN workforce_staff s ON (s.tenant_id,s.hospital_id,s.id)=(c.tenant_id,c.hospital_id,c.staff_id)
 LEFT JOIN workforce_attendance_punches t ON (t.tenant_id,t.hospital_id,t.id)=(c.tenant_id,c.hospital_id,c.target_punch_id)
 LEFT JOIN workforce_attendance_punches a ON (a.tenant_id,a.hospital_id,a.correction_id)=(c.tenant_id,c.hospital_id,c.id)) AS v`
const correctionColumns = "id,staff_id,employee_code,display_name,kind,target_punch_id,target_direction,target_punched_at,direction,punched_at,local_date,timezone,reason,status,requested_by_subject,decided_by_subject,decided_at,applied_punch_id,version,created_at,updated_at"

type correctionRow struct {
	StaffID            uuid.UUID
	Kind               string
	TargetPunchID      *uuid.UUID
	Direction          *string
	PunchedAt          *time.Time
	Reason             string
	Status             string
	RequestedBySubject string
	Version            int64
	CreatedAt          time.Time
}

func correctionID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("correction"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "correction")
		return uuid.Nil, false
	}
	return id, true
}

func loadCorrection(tx *gorm.DB, tenant string, hospital, id uuid.UUID) (attendanceCorrection, error) {
	var r attendanceCorrection
	return r, tx.Table(correctionView).Select(correctionColumns).Where("tenant_id=? AND hospital_id=? AND id=?", tenant, hospital, id).Take(&r).Error
}

func redactCorrection(c *gin.Context, r *attendanceCorrection) {
	if !attendanceReader(c) {
		r.Reason = ""
	}
}

func (h *handlers) publishCorrection(tx *gorm.DB, p authn.Principal, hospital, id uuid.UUID, status string, version int64) error {
	data, err := json.Marshal(workforcecontract.AttendanceCorrectionChangedData{HospitalID: hospital.String(), CorrectionID: id.String(), Status: status, Version: version})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, AttendanceCorrectionChanged, events.Event{Type: AttendanceCorrectionChanged, Version: 1, TenantID: p.TenantID, Data: data})
}

// correctionTarget loads a live punch of the person whose own back-dating window, counted from at, still covers it.
func correctionTarget(tx *gorm.DB, tenant string, hospital, staff, id uuid.UUID, at time.Time) (attendancePunch, error) {
	target, err := loadPunch(tx, tenant, hospital, id)
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		return target, errCorrectionTarget
	case err != nil:
		return target, err
	case target.StaffID != staff:
		return target, errCorrectionTarget
	case target.Voided:
		return target, errCorrectionTargetVoided
	}
	var backdate int
	if err := tx.Raw(`SELECT backdate_days FROM workforce_attendance_setting_revisions WHERE tenant_id=? AND hospital_id=? AND id=?`, tenant, hospital, target.PolicyRevisionID).Scan(&backdate).Error; err != nil {
		return target, err
	}
	inside, err := insideBackdateAt(target.LocalDate, target.Timezone, backdate, at)
	if err != nil {
		return target, err
	}
	if !inside {
		return target, errAttendanceWindow
	}
	return target, nil
}

// placeCorrected resolves a corrected punch time to its pins and refuses it outside the window counted from at or as a live duplicate.
func placeCorrected(tx *gorm.DB, p authn.Principal, hospital, staff uuid.UUID, direction string, punchedAt, at time.Time, replacing uuid.UUID) (punchPlace, error) {
	place, err := placePunch(tx, p, hospital, punchedAt)
	if err != nil {
		return place, err
	}
	inside, err := insideBackdateAt(place.localDate, place.timezone, place.backdateDays, at)
	if err != nil {
		return place, err
	}
	if !inside {
		return place, errAttendanceWindow
	}
	return place, checkDuplicatePunch(tx, p.TenantID, hospital, staff, direction, punchedAt, place.duplicateWindow, replacing)
}

// putCorrection files a correction under a client-chosen id; an identical retry returns the stored request.
func (h *handlers) putCorrection(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, ok := correctionID(c)
	if !ok {
		return
	}
	var in attendanceCorrectionInput
	if !decode(c, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	timed, targeted := in.Kind != "void", in.Kind != "add"
	valid := in.StaffID != uuid.Nil && in.Reason != "" && validLeaveText(in.Reason) &&
		(in.Direction != nil) == timed && (in.PunchedAt != nil) == timed && (in.TargetPunchID != nil) == targeted
	var at time.Time
	if valid && timed {
		at = in.PunchedAt.UTC()
		valid = at.Equal(at.Truncate(time.Minute)) && at.Year() >= 2000 && at.Year() <= 2199 && !at.After(time.Now().Add(5*time.Minute))
	}
	if valid && targeted {
		valid = *in.TargetPunchID != uuid.Nil
	}
	if !valid {
		respondCorrectionError(c, errCorrectionInvalid)
		return
	}
	var result attendanceCorrection
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_attendance", attendanceLock); err != nil {
			return err
		}
		var existing correctionRow
		if err := tx.Raw(`SELECT staff_id,kind,target_punch_id,direction,punched_at,reason FROM workforce_attendance_corrections WHERE tenant_id=? AND hospital_id=? AND id=?`, p.TenantID, hospital, id).Scan(&existing).Error; err != nil {
			return err
		}
		if existing.StaffID != uuid.Nil {
			same := existing.StaffID == in.StaffID && existing.Kind == in.Kind && existing.Reason == in.Reason &&
				equalPointer(existing.TargetPunchID, in.TargetPunchID) && equalPointer(existing.Direction, in.Direction) &&
				(existing.PunchedAt == nil) == (in.PunchedAt == nil) && (existing.PunchedAt == nil || existing.PunchedAt.Equal(at))
			if !same {
				return errCorrectionReused
			}
			var err error
			result, err = loadCorrection(tx, p.TenantID, hospital, id)
			return err
		}
		var staff int64
		if err := tx.Raw(`SELECT count(*) FROM workforce_staff WHERE tenant_id=? AND hospital_id=? AND id=?`, p.TenantID, hospital, in.StaffID).Scan(&staff).Error; err != nil {
			return err
		}
		if staff == 0 {
			return gorm.ErrRecordNotFound
		}
		now := time.Now()
		replacing := uuid.Nil
		var pins struct {
			LocalDate, Timezone                string
			SettingsRevisionID, PolicyRevision uuid.UUID
		}
		if targeted {
			if _, err := correctionTarget(tx, p.TenantID, hospital, in.StaffID, *in.TargetPunchID, now); err != nil {
				return err
			}
			var taken bool
			if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_attendance_corrections WHERE tenant_id=? AND hospital_id=? AND target_punch_id=? AND status IN ('pending','approved'))`,
				p.TenantID, hospital, *in.TargetPunchID).Scan(&taken).Error; err != nil {
				return err
			}
			if taken {
				return errCorrectionExists
			}
			replacing = *in.TargetPunchID
			if err := tx.Raw(`SELECT local_date::text AS local_date,timezone,settings_revision_id,policy_revision_id AS policy_revision FROM workforce_attendance_punches WHERE tenant_id=? AND hospital_id=? AND id=?`,
				p.TenantID, hospital, replacing).Scan(&pins).Error; err != nil {
				return err
			}
		}
		if timed {
			place, err := placeCorrected(tx, p, hospital, in.StaffID, *in.Direction, at, now, replacing)
			if err != nil {
				return err
			}
			window := time.Duration(place.duplicateWindow) * time.Minute
			var pending struct{ ID uuid.UUID }
			if err := tx.Raw(`SELECT id FROM workforce_attendance_corrections WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND status='pending' AND direction=? AND punched_at > ? AND punched_at < ? ORDER BY punched_at,id LIMIT 1`,
				p.TenantID, hospital, in.StaffID, *in.Direction, at.Add(-window), at.Add(window)).Scan(&pending).Error; err != nil {
				return err
			}
			if pending.ID != uuid.Nil {
				return correctionDuplicate{id: pending.ID}
			}
			pins.LocalDate, pins.Timezone, pins.SettingsRevisionID, pins.PolicyRevision = place.localDate, place.timezone, place.settingsRevision, place.policy
		}
		var punchedAt *time.Time
		if timed {
			punchedAt = &at
		}
		if err := tx.Exec(`INSERT INTO workforce_attendance_corrections(tenant_id,hospital_id,id,staff_id,kind,target_punch_id,direction,punched_at,local_date,timezone,settings_revision_id,policy_revision_id,reason,status,requested_by_subject,version)
 VALUES(?,?,?,?,?,?,?,?,?::date,?,?,?,?,'pending',?,1)`, p.TenantID, hospital, id, in.StaffID, in.Kind, in.TargetPunchID, in.Direction, punchedAt,
			pins.LocalDate, pins.Timezone, pins.SettingsRevisionID, pins.PolicyRevision, in.Reason, p.Subject).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_attendance_correction_events(tenant_id,hospital_id,id,correction_id,version,status,actor_subject) VALUES(?,?,?,?,1,'pending',?)`, p.TenantID, hospital, uuid.New(), id, p.Subject).Error; err != nil {
			return err
		}
		// Reasons can describe personal circumstances, so audit rows and events carry identifiers, kind and direction only.
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.attendance.correction_filed',?,jsonb_build_object('staff_id',?::uuid,'kind',?::text,'target_punch_id',?::uuid,'direction',?::text,'version',1))`,
			p.TenantID, hospital, p.Subject, id, in.StaffID, in.Kind, in.TargetPunchID, in.Direction).Error; err != nil {
			return err
		}
		if err := h.publishCorrection(tx, p, hospital, id, "pending", 1); err != nil {
			return err
		}
		var err error
		result, err = loadCorrection(tx, p.TenantID, hospital, id)
		return err
	})
	if err != nil {
		respondCorrectionError(c, err)
		return
	}
	redactCorrection(c, &result)
	respond.OK(c, result)
}

func equalPointer[T comparable](a, b *T) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}

func (h *handlers) decideCorrection(c *gin.Context) {
	var in attendanceCorrectionDecisionInput
	if !decode(c, &in) {
		return
	}
	to := map[string]string{"approve": "approved", "reject": "rejected"}[in.Decision]
	h.transitionCorrection(c, to, *in.ExpectedVersion, in.Note)
}

func (h *handlers) cancelCorrection(c *gin.Context) {
	var in attendanceCorrectionCancelInput
	if !decode(c, &in) {
		return
	}
	h.transitionCorrection(c, "cancelled", *in.ExpectedVersion, in.Note)
}

// transitionCorrection moves a pending request one step; approval writes its punch changes in the same transaction.
func (h *handlers) transitionCorrection(c *gin.Context, to string, expected int64, note string) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, ok := correctionID(c)
	if !ok {
		return
	}
	note = strings.TrimSpace(note)
	if !validLeaveText(note) || to == "rejected" && note == "" {
		respondCorrectionError(c, errCorrectionNote)
		return
	}
	decides := to != "cancelled"
	var result attendanceCorrection
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_attendance", attendanceLock); err != nil {
			return err
		}
		var r correctionRow
		if err := tx.Raw(`SELECT staff_id,kind,target_punch_id,direction,punched_at,reason,status,requested_by_subject,version,created_at
 FROM workforce_attendance_corrections WHERE tenant_id=? AND hospital_id=? AND id=? FOR UPDATE`, p.TenantID, hospital, id).Scan(&r).Error; err != nil {
			return err
		}
		if r.StaffID == uuid.Nil {
			return gorm.ErrRecordNotFound
		}
		if r.Version == expected+1 && r.Status == to {
			var replay bool
			if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_attendance_correction_events WHERE tenant_id=? AND hospital_id=? AND correction_id=? AND version=? AND actor_subject=? AND note=?)`,
				p.TenantID, hospital, id, r.Version, p.Subject, note).Scan(&replay).Error; err != nil {
				return err
			}
			if replay {
				var err error
				result, err = loadCorrection(tx, p.TenantID, hospital, id)
				return err
			}
		}
		if r.Version != expected {
			return errCorrectionStale
		}
		if r.Status != "pending" {
			return errCorrectionState
		}
		if decides && r.RequestedBySubject == p.Subject {
			return errCorrectionSelfDecision
		}
		set := "status=?,version=version+1,updated_at=clock_timestamp()"
		if decides {
			set += ",decided_by_subject=?,decided_at=clock_timestamp()"
		}
		args := []any{to}
		if decides {
			args = append(args, p.Subject)
		}
		update := tx.Exec(`UPDATE workforce_attendance_corrections SET `+set+` WHERE tenant_id=? AND hospital_id=? AND id=? AND version=? AND status='pending'`, append(args, p.TenantID, hospital, id, expected)...)
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return errCorrectionStale
		}
		version := expected + 1
		if err := tx.Exec(`INSERT INTO workforce_attendance_correction_events(tenant_id,hospital_id,id,correction_id,version,status,note,actor_subject) VALUES(?,?,?,?,?,?,?,?)`,
			p.TenantID, hospital, uuid.New(), id, version, to, note, p.Subject).Error; err != nil {
			return err
		}
		if to == "approved" {
			if err := h.applyCorrection(tx, p, hospital, id, r); err != nil {
				return err
			}
		}
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,jsonb_build_object('previous_status','pending','version',?::bigint,'kind',?::text))`,
			p.TenantID, hospital, p.Subject, "workforce.attendance.correction_"+to, id, version, r.Kind).Error; err != nil {
			return err
		}
		if err := h.publishCorrection(tx, p, hospital, id, to, version); err != nil {
			return err
		}
		var err error
		result, err = loadCorrection(tx, p.TenantID, hospital, id)
		return err
	})
	if err != nil {
		respondCorrectionError(c, err)
		return
	}
	redactCorrection(c, &result)
	respond.OK(c, result)
}

// applyCorrection voids the target and records the corrected punch, re-checked against the filing time's window and today's punches.
func (h *handlers) applyCorrection(tx *gorm.DB, p authn.Principal, hospital, id uuid.UUID, r correctionRow) error {
	replacing := uuid.Nil
	if r.TargetPunchID != nil {
		target, err := correctionTarget(tx, p.TenantID, hospital, r.StaffID, *r.TargetPunchID, r.CreatedAt)
		if err != nil {
			return err
		}
		replacing = target.ID
	}
	if r.PunchedAt != nil {
		place, err := placeCorrected(tx, p, hospital, r.StaffID, *r.Direction, r.PunchedAt.UTC(), r.CreatedAt, replacing)
		if err != nil {
			return err
		}
		if replacing != uuid.Nil {
			if err := h.voidForCorrection(tx, p, hospital, id, replacing, r); err != nil {
				return err
			}
		}
		punch := uuid.New()
		if err := tx.Exec(`INSERT INTO workforce_attendance_punches(tenant_id,hospital_id,id,staff_id,direction,punched_at,local_date,timezone,settings_revision_id,policy_revision_id,source,correction_id,reason,recorded_by_subject)
 VALUES(?,?,?,?,?,?,?::date,?,?,?,'correction',?,?,?)`, p.TenantID, hospital, punch, r.StaffID, *r.Direction, r.PunchedAt.UTC(), place.localDate, place.timezone, place.settingsRevision, place.policy, id, r.Reason, p.Subject).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.attendance.punch_recorded',?,jsonb_build_object('staff_id',?::uuid,'direction',?::text,'source','correction','correction_id',?::uuid))`,
			p.TenantID, hospital, p.Subject, punch, r.StaffID, *r.Direction, id).Error; err != nil {
			return err
		}
		return h.publishPunch(tx, p, hospital, punch, "recorded")
	}
	return h.voidForCorrection(tx, p, hospital, id, replacing, r)
}

func (h *handlers) voidForCorrection(tx *gorm.DB, p authn.Principal, hospital, id, punch uuid.UUID, r correctionRow) error {
	if err := tx.Exec(`INSERT INTO workforce_attendance_punch_voids(tenant_id,hospital_id,punch_id,reason,voided_by_subject,correction_id) VALUES(?,?,?,?,?,?)`, p.TenantID, hospital, punch, r.Reason, p.Subject, id).Error; err != nil {
		return err
	}
	if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.attendance.punch_voided',?,jsonb_build_object('staff_id',?::uuid,'correction_id',?::uuid))`,
		p.TenantID, hospital, p.Subject, punch, r.StaffID, id).Error; err != nil {
		return err
	}
	return h.publishPunch(tx, p, hospital, punch, "voided")
}

func (h *handlers) getCorrection(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, ok := correctionID(c)
	if !ok {
		return
	}
	var result attendanceCorrection
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var err error
		result, err = loadCorrection(tx, p.TenantID, hospital, id)
		return err
	})
	if err != nil {
		respondCorrectionError(c, err)
		return
	}
	redactCorrection(c, &result)
	respond.OK(c, result)
}

// listCorrections filters by status, person and affected local date, newest filing first.
func (h *handlers) listCorrections(c *gin.Context, params pagination.Params) ([]attendanceCorrection, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	where := "tenant_id=? AND hospital_id=?"
	args := []any{p.TenantID, hospital}
	valid := params.Limit <= 100
	if s := c.Query("status"); s != "" {
		valid = valid && slices.Contains(attendanceCorrectionStatuses, s)
		where += " AND status=?"
		args = append(args, s)
	}
	if s := c.Query("staff_id"); s != "" {
		staff, err := uuid.Parse(s)
		valid = valid && err == nil && staff != uuid.Nil
		where += " AND staff_id=?"
		args = append(args, staff)
	}
	var from, to time.Time
	for _, bound := range []struct {
		name, clause string
		into         *time.Time
	}{{"from", " AND local_date::date>=?::date", &from}, {"to", " AND local_date::date<=?::date", &to}} {
		s := c.Query(bound.name)
		if s == "" {
			continue
		}
		day, ok := parseWorkDate(s)
		valid = valid && ok
		*bound.into = day
		where += bound.clause
		args = append(args, s)
	}
	if !valid || !from.IsZero() && !to.IsZero() && to.Before(from) {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Use a known status, a staff_id, from and to dates between 2000 and 2199 with from not after to, and a page limit no greater than 100.")
		return nil, nil
	}
	rows := []attendanceCorrection{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Table(correctionView).Select(correctionColumns).Where(where, args...).Find(&rows).Error
	})
	if err != nil {
		respondCorrectionError(c, err)
		return nil, nil
	}
	for i := range rows {
		redactCorrection(c, &rows[i])
	}
	return rows, nil
}

func (h *handlers) correctionHistory(c *gin.Context, params pagination.Params) ([]attendanceCorrectionEvent, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	id, ok := correctionID(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	rows := []attendanceCorrectionEvent{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var exists bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_attendance_corrections WHERE tenant_id=? AND hospital_id=? AND id=?)`, p.TenantID, hospital, id).Scan(&exists).Error; err != nil {
			return err
		}
		if !exists {
			return gorm.ErrRecordNotFound
		}
		return tenantdb.ApplyKeyset(tx, params).Table("workforce_attendance_correction_events").Select("id,version,status,note,actor_subject,created_at").
			Where("tenant_id=? AND hospital_id=? AND correction_id=?", p.TenantID, hospital, id).Find(&rows).Error
	})
	if err != nil {
		respondCorrectionError(c, err)
		return nil, nil
	}
	// Decision notes may restate the reason, so they follow the same redaction.
	if !attendanceReader(c) {
		for i := range rows {
			rows[i].Note = ""
		}
	}
	return rows, nil
}

func respondCorrectionError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	var duplicate correctionDuplicate
	switch {
	case errors.Is(err, errCorrectionSelfDecision), errors.As(err, &pg) && pg.ConstraintName == "workforce_attendance_correction_separation":
		respond.Forbidden(c, "Someone other than the person who filed the correction must decide it.")
	case errors.As(err, &duplicate):
		respond.ConflictWithDetail(c, "A pending correction already adds a punch in the same direction within the duplicate window.", gin.H{"reason": "duplicate_correction", "correction_id": duplicate.id})
	case errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "workforce_attendance_corrections_pkey":
		respond.Error(c, errCorrectionReused.status, errCorrectionReused.code, errCorrectionReused.message)
	case errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "workforce_attendance_correction_target":
		respond.Error(c, errCorrectionExists.status, errCorrectionExists.code, errCorrectionExists.message)
	default:
		respondAttendanceError(c, err)
	}
}

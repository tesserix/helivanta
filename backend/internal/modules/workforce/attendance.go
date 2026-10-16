package workforce

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func (s attendanceSettings) PageKey() (time.Time, uuid.UUID) { return s.CreatedAt, s.RevisionID }
func (p attendancePunch) PageKey() (time.Time, uuid.UUID)    { return p.CreatedAt, p.ID }

// attendanceLock serialises attendance writes in a hospital, so duplicate checks see each other's punches.
const attendanceLock = "attendance"

// maxAttendanceRange bounds one person's punch and attendance queries.
const maxAttendanceRange = 42

var (
	errAttendanceSettingsInvalid = &rosterProblem{400, "invalid_request", "Use allowances within their limits, an effective date between 2000 and 2199 and confirm that you reviewed the policy."}
	errAttendanceSettingsStale   = &rosterProblem{409, "stale_version", "The attendance policy changed since you opened it. Reload before saving."}
	errAttendanceEffective       = &rosterProblem{400, "invalid_request", "The effective date cannot be earlier than the current revision's effective date."}
	errAttendancePunchInvalid    = &rosterProblem{400, "invalid_request", "Use a whole-minute punch time between 2000 and 2199, no more than five minutes ahead, and a reason of 1 to 500 characters."}
	errAttendanceVoidInvalid     = &rosterProblem{400, "invalid_request", "Give a reason of 1 to 500 characters for voiding the punch."}
	errAttendanceRange           = &rosterProblem{400, "invalid_request", "Use a staff member and from/to dates between 2000 and 2199, from not after to, at most 42 days apart, and a page limit no greater than 100."}
	errAttendanceNoPolicy        = &rosterProblem{409, "no_attendance_policy", "Review the hospital's attendance policy and roster settings effective on or before the punch date first."}
	errAttendanceWindow          = &rosterProblem{409, "outside_backdate_window", "The punch date is outside the hospital's back-dating window."}
	errAttendanceReused          = &rosterProblem{409, "request_reused", "This punch id was already used with different values."}
	errAttendanceVoided          = &rosterProblem{409, "punch_voided", "This punch was already voided with a different reason."}
	errAttendanceStaffInactive   = &rosterProblem{409, "staff_inactive", "Punches are recorded for active staff only."}
)

// attendanceDuplicate names the live punch a new punch would duplicate.
type attendanceDuplicate struct{ id uuid.UUID }

func (attendanceDuplicate) Error() string { return "duplicate punch" }

const attendanceSettingsColumns = "revision_id,early_arrival_minutes,late_departure_minutes,early_departure_grace_minutes,short_duty_minutes,duplicate_window_minutes,backdate_days,effective_on::text AS effective_on,reviewed_by_subject,version,created_at,updated_at"
const attendanceSettingsRevisionColumns = "id AS revision_id,early_arrival_minutes,late_departure_minutes,early_departure_grace_minutes,short_duty_minutes,duplicate_window_minutes,backdate_days,effective_on::text AS effective_on,reviewed_by_subject,version,created_at,created_at AS updated_at"

const attendancePunchView = `(SELECT p.tenant_id,p.hospital_id,p.id,p.staff_id,s.employee_code,s.display_name,p.direction,p.punched_at AT TIME ZONE 'UTC' AS punched_at,p.local_date::text AS local_date,
 p.timezone,p.source,p.correction_id,p.policy_revision_id,p.reason,p.recorded_by_subject,p.created_at,v.punch_id IS NOT NULL AS voided,v.reason AS void_reason,v.voided_by_subject,v.created_at AS voided_at,v.correction_id AS void_correction_id
 FROM workforce_attendance_punches p JOIN workforce_staff s ON (s.tenant_id,s.hospital_id,s.id)=(p.tenant_id,p.hospital_id,p.staff_id)
 LEFT JOIN workforce_attendance_punch_voids v ON (v.tenant_id,v.hospital_id,v.punch_id)=(p.tenant_id,p.hospital_id,p.id)) AS v`
const attendancePunchColumns = "id,staff_id,employee_code,display_name,direction,punched_at,local_date,timezone,source,correction_id,policy_revision_id,reason,recorded_by_subject,created_at,voided,void_reason,voided_by_subject,voided_at,void_correction_id"

func attendancePunchID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("punch"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "punch")
		return uuid.Nil, false
	}
	return id, true
}

// attendanceReader may see punch and void reasons, which can describe personal circumstances.
func attendanceReader(c *gin.Context) bool {
	set, ok := authz.PermissionsFrom(c)
	return ok && (set.Has(PermAttendanceRecord) || set.Has(PermAttendanceCorrect) || set.Has(PermAttendanceApprove))
}

func redactPunch(c *gin.Context, p *attendancePunch) {
	if !attendanceReader(c) {
		p.Reason = ""
		if p.VoidReason != nil {
			p.VoidReason = new(string)
		}
	}
}

func loadPunch(tx *gorm.DB, tenant string, hospital, id uuid.UUID) (attendancePunch, error) {
	var p attendancePunch
	return p, tx.Table(attendancePunchView).Select(attendancePunchColumns).Where("tenant_id=? AND hospital_id=? AND id=?", tenant, hospital, id).Take(&p).Error
}

func (h *handlers) publishPunch(tx *gorm.DB, p authn.Principal, hospital, id uuid.UUID, action string) error {
	data, err := json.Marshal(workforcecontract.AttendancePunchChangedData{HospitalID: hospital.String(), PunchID: id.String(), Action: action})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, AttendancePunchChanged, events.Event{Type: AttendancePunchChanged, Version: 1, TenantID: p.TenantID, Data: data})
}

func (h *handlers) getAttendanceSettings(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	var result attendanceSettings
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		return tx.Table("workforce_attendance_settings").Select(attendanceSettingsColumns).Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Take(&result).Error
	})
	if err != nil {
		respondAttendanceError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) attendanceSettingsHistory(c *gin.Context, params pagination.Params) ([]attendanceSettings, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	rows := []attendanceSettings{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Table("workforce_attendance_setting_revisions").Select(attendanceSettingsRevisionColumns).Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Find(&rows).Error
	})
	if err != nil {
		respondAttendanceError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) putAttendanceSettings(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	var in attendanceSettingsInput
	if !decode(c, &in) {
		return
	}
	if _, valid := parseWorkDate(in.EffectiveOn); !valid || !*in.Reviewed {
		respondAttendanceError(c, errAttendanceSettingsInvalid)
		return
	}
	values := []any{*in.EarlyArrivalMinutes, *in.LateDepartureMinutes, *in.EarlyDepartureGraceMinutes, *in.ShortDutyMinutes, *in.DuplicateWindowMinutes, *in.BackdateDays, in.EffectiveOn}
	var result attendanceSettings
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_attendance", attendanceLock); err != nil {
			return err
		}
		var old attendanceSettings
		err := tx.Table("workforce_attendance_settings").Select(attendanceSettingsColumns).Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Take(&old).Error
		version := int64(1)
		switch {
		case err == nil:
			if old.EarlyArrivalMinutes == *in.EarlyArrivalMinutes && old.LateDepartureMinutes == *in.LateDepartureMinutes && old.EarlyDepartureGraceMinutes == *in.EarlyDepartureGraceMinutes &&
				old.ShortDutyMinutes == *in.ShortDutyMinutes && old.DuplicateWindowMinutes == *in.DuplicateWindowMinutes && old.BackdateDays == *in.BackdateDays && old.EffectiveOn == in.EffectiveOn {
				result = old
				return nil
			}
			if old.Version != *in.ExpectedVersion {
				return errAttendanceSettingsStale
			}
			if in.EffectiveOn < old.EffectiveOn {
				return errAttendanceEffective
			}
			version = old.Version + 1
		case errors.Is(err, gorm.ErrRecordNotFound):
			if *in.ExpectedVersion != 0 {
				return errAttendanceSettingsStale
			}
		default:
			return err
		}
		revision := uuid.New()
		if err := tx.Exec(`INSERT INTO workforce_attendance_settings(tenant_id,hospital_id,early_arrival_minutes,late_departure_minutes,early_departure_grace_minutes,short_duty_minutes,duplicate_window_minutes,backdate_days,effective_on,reviewed_by_subject,version,revision_id)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(tenant_id,hospital_id) DO UPDATE SET early_arrival_minutes=EXCLUDED.early_arrival_minutes,late_departure_minutes=EXCLUDED.late_departure_minutes,
 early_departure_grace_minutes=EXCLUDED.early_departure_grace_minutes,short_duty_minutes=EXCLUDED.short_duty_minutes,duplicate_window_minutes=EXCLUDED.duplicate_window_minutes,backdate_days=EXCLUDED.backdate_days,
 effective_on=EXCLUDED.effective_on,reviewed_by_subject=EXCLUDED.reviewed_by_subject,version=EXCLUDED.version,revision_id=EXCLUDED.revision_id,updated_at=clock_timestamp()`,
			append(append([]any{p.TenantID, hospital}, values...), p.Subject, version, revision)...).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_attendance_setting_revisions(tenant_id,hospital_id,id,early_arrival_minutes,late_departure_minutes,early_departure_grace_minutes,short_duty_minutes,duplicate_window_minutes,backdate_days,effective_on,reviewed_by_subject,version)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, append(append([]any{p.TenantID, hospital, revision}, values...), p.Subject, version)...).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.attendance_settings.changed',?,jsonb_build_object('version',?::bigint,'previous_version',?::bigint))`,
			p.TenantID, hospital, p.Subject, revision, version, version-1).Error; err != nil {
			return err
		}
		data, err := json.Marshal(workforcecontract.AttendanceSettingsChangedData{HospitalID: hospital.String(), RevisionID: revision.String(), Version: version})
		if err != nil {
			return err
		}
		if err := h.bus.Publish(tx, AttendanceSettingsChanged, events.Event{Type: AttendanceSettingsChanged, Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
			return err
		}
		return tx.Table("workforce_attendance_settings").Select(attendanceSettingsColumns).Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Take(&result).Error
	})
	if err != nil {
		respondAttendanceError(c, err)
		return
	}
	respond.OK(c, result)
}

// punchPlace is a punch resolved to its hospital-local date and the revisions in force that day.
type punchPlace struct {
	localDate        string
	timezone         string
	settingsRevision uuid.UUID
	policy           uuid.UUID
	backdateDays     int
	duplicateWindow  int
}

// placePunch finds the roster timezone in force on the punch's local date; a date whose timezone disagrees with itself is refused.
func placePunch(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, at time.Time) (punchPlace, error) {
	var place punchPlace
	day := at.UTC().Format(time.DateOnly)
	for range 2 {
		var settings struct {
			ID       uuid.UUID
			Timezone string
		}
		if err := tx.Raw(`SELECT id,timezone FROM workforce_roster_setting_revisions WHERE tenant_id=? AND hospital_id=? AND id=workforce_roster_settings_on(?,?,?::date)`,
			p.TenantID, hospital, p.TenantID, hospital, day).Scan(&settings).Error; err != nil {
			return place, err
		}
		if settings.ID == uuid.Nil {
			return place, errAttendanceNoPolicy
		}
		loc, err := time.LoadLocation(settings.Timezone)
		if err != nil {
			return place, err
		}
		local := at.In(loc).Format(time.DateOnly)
		if local == day {
			place.localDate, place.timezone, place.settingsRevision = local, settings.Timezone, settings.ID
			break
		}
		day = local
	}
	if place.localDate == "" {
		return place, errAttendanceNoPolicy
	}
	var policy struct {
		ID                     uuid.UUID
		BackdateDays           int
		DuplicateWindowMinutes int
	}
	if err := tx.Raw(`SELECT id,backdate_days,duplicate_window_minutes FROM workforce_attendance_setting_revisions WHERE tenant_id=? AND hospital_id=? AND id=workforce_attendance_policy_on(?,?,?::date)`,
		p.TenantID, hospital, p.TenantID, hospital, place.localDate).Scan(&policy).Error; err != nil {
		return place, err
	}
	if policy.ID == uuid.Nil {
		return place, errAttendanceNoPolicy
	}
	place.policy, place.backdateDays, place.duplicateWindow = policy.ID, policy.BackdateDays, policy.DuplicateWindowMinutes
	return place, nil
}

// insideBackdate reports whether a local date is no older than the back-dating window, counted in the hospital's own today.
func insideBackdate(localDate, timezone string, backdate int) (bool, error) {
	return insideBackdateAt(localDate, timezone, backdate, time.Now())
}

// insideBackdateAt counts the window back from the hospital date of at.
func insideBackdateAt(localDate, timezone string, backdate int, at time.Time) (bool, error) {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return false, err
	}
	today, _ := time.Parse(time.DateOnly, at.In(loc).Format(time.DateOnly))
	return localDate >= today.AddDate(0, 0, -backdate).Format(time.DateOnly), nil
}

// checkDuplicatePunch refuses a live punch in the same direction within the window, other than the one being replaced.
func checkDuplicatePunch(tx *gorm.DB, tenant string, hospital, staff uuid.UUID, direction string, at time.Time, windowMinutes int, replacing uuid.UUID) error {
	window := time.Duration(windowMinutes) * time.Minute
	var duplicate struct{ ID uuid.UUID }
	if err := tx.Raw(`SELECT p.id FROM workforce_attendance_punches p WHERE p.tenant_id=? AND p.hospital_id=? AND p.staff_id=? AND p.direction=? AND p.punched_at > ? AND p.punched_at < ? AND p.id<>?
 AND NOT EXISTS(SELECT 1 FROM workforce_attendance_punch_voids v WHERE (v.tenant_id,v.hospital_id,v.punch_id)=(p.tenant_id,p.hospital_id,p.id)) ORDER BY p.punched_at,p.id LIMIT 1`,
		tenant, hospital, staff, direction, at.Add(-window), at.Add(window), replacing).Scan(&duplicate).Error; err != nil {
		return err
	}
	if duplicate.ID != uuid.Nil {
		return attendanceDuplicate{id: duplicate.ID}
	}
	return nil
}

// putPunch records a punch under a client-chosen id; an identical retry returns the stored punch.
func (h *handlers) putPunch(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, ok := attendancePunchID(c)
	if !ok {
		return
	}
	var in attendancePunchInput
	if !decode(c, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	at := in.PunchedAt.UTC()
	if in.StaffID == uuid.Nil || in.Reason == "" || !validLeaveText(in.Reason) || !at.Equal(at.Truncate(time.Minute)) ||
		at.Year() < 2000 || at.Year() > 2199 || at.After(time.Now().Add(5*time.Minute)) {
		respondAttendanceError(c, errAttendancePunchInvalid)
		return
	}
	var result attendancePunch
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_attendance", attendanceLock); err != nil {
			return err
		}
		existing, err := loadPunch(tx, p.TenantID, hospital, id)
		switch {
		case err == nil:
			if existing.StaffID != in.StaffID || existing.Direction != in.Direction || !existing.PunchedAt.Equal(at) || existing.Reason != in.Reason {
				return errAttendanceReused
			}
			result = existing
			return nil
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		}
		place, err := placePunch(tx, p, hospital, at)
		if err != nil {
			return err
		}
		inside, err := insideBackdate(place.localDate, place.timezone, place.backdateDays)
		if err != nil {
			return err
		}
		if !inside {
			return errAttendanceWindow
		}
		var person struct{ EmploymentStatus string }
		if err := tx.Raw(`SELECT employment_status FROM workforce_staff WHERE tenant_id=? AND hospital_id=? AND id=? FOR SHARE`, p.TenantID, hospital, in.StaffID).Scan(&person).Error; err != nil {
			return err
		}
		if person.EmploymentStatus == "" {
			return gorm.ErrRecordNotFound
		}
		if person.EmploymentStatus != "active" {
			return errAttendanceStaffInactive
		}
		if err := checkDuplicatePunch(tx, p.TenantID, hospital, in.StaffID, in.Direction, at, place.duplicateWindow, uuid.Nil); err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_attendance_punches(tenant_id,hospital_id,id,staff_id,direction,punched_at,local_date,timezone,settings_revision_id,policy_revision_id,source,reason,recorded_by_subject)
 VALUES(?,?,?,?,?,?,?::date,?,?,?,'manual',?,?)`, p.TenantID, hospital, id, in.StaffID, in.Direction, at, place.localDate, place.timezone, place.settingsRevision, place.policy, in.Reason, p.Subject).Error; err != nil {
			return err
		}
		// Reasons can describe personal circumstances, so audit rows and events carry identifiers and direction only.
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.attendance.punch_recorded',?,jsonb_build_object('staff_id',?::uuid,'direction',?::text,'source','manual'))`,
			p.TenantID, hospital, p.Subject, id, in.StaffID, in.Direction).Error; err != nil {
			return err
		}
		if err := h.publishPunch(tx, p, hospital, id, "recorded"); err != nil {
			return err
		}
		result, err = loadPunch(tx, p.TenantID, hospital, id)
		return err
	})
	if err != nil {
		respondAttendanceError(c, err)
		return
	}
	redactPunch(c, &result)
	respond.OK(c, result)
}

// voidPunch adds the void row; repeating it with the same reason returns the voided punch.
func (h *handlers) voidPunch(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, ok := attendancePunchID(c)
	if !ok {
		return
	}
	var in attendanceVoidInput
	if !decode(c, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Reason == "" || !validLeaveText(in.Reason) {
		respondAttendanceError(c, errAttendanceVoidInvalid)
		return
	}
	var result attendancePunch
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_attendance", attendanceLock); err != nil {
			return err
		}
		existing, err := loadPunch(tx, p.TenantID, hospital, id)
		if err != nil {
			return err
		}
		if existing.Voided {
			if *existing.VoidReason != in.Reason {
				return errAttendanceVoided
			}
			result = existing
			return nil
		}
		var backdate int
		if err := tx.Raw(`SELECT backdate_days FROM workforce_attendance_setting_revisions WHERE tenant_id=? AND hospital_id=? AND id=?`, p.TenantID, hospital, existing.PolicyRevisionID).Scan(&backdate).Error; err != nil {
			return err
		}
		inside, err := insideBackdate(existing.LocalDate, existing.Timezone, backdate)
		if err != nil {
			return err
		}
		if !inside {
			return errAttendanceWindow
		}
		if err := tx.Exec(`INSERT INTO workforce_attendance_punch_voids(tenant_id,hospital_id,punch_id,reason,voided_by_subject) VALUES(?,?,?,?,?)`, p.TenantID, hospital, id, in.Reason, p.Subject).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.attendance.punch_voided',?,jsonb_build_object('staff_id',?::uuid,'direction',?::text))`,
			p.TenantID, hospital, p.Subject, id, existing.StaffID, existing.Direction).Error; err != nil {
			return err
		}
		if err := h.publishPunch(tx, p, hospital, id, "voided"); err != nil {
			return err
		}
		result, err = loadPunch(tx, p.TenantID, hospital, id)
		return err
	})
	if err != nil {
		respondAttendanceError(c, err)
		return
	}
	redactPunch(c, &result)
	respond.OK(c, result)
}

func (h *handlers) getPunch(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, ok := attendancePunchID(c)
	if !ok {
		return
	}
	var result attendancePunch
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var err error
		result, err = loadPunch(tx, p.TenantID, hospital, id)
		return err
	})
	if err != nil {
		respondAttendanceError(c, err)
		return
	}
	redactPunch(c, &result)
	respond.OK(c, result)
}

// attendanceRange reads a required staff_id and an inclusive from/to of at most maxAttendanceRange days.
func attendanceRange(c *gin.Context, staffParam string) (uuid.UUID, string, string, bool) {
	staff, err := uuid.Parse(staffParam)
	from, okFrom := parseWorkDate(c.Query("from"))
	to, okTo := parseWorkDate(c.Query("to"))
	if err != nil || staff == uuid.Nil || !okFrom || !okTo || to.Before(from) || to.Sub(from) >= maxAttendanceRange*24*time.Hour {
		respondAttendanceError(c, errAttendanceRange)
		return uuid.Nil, "", "", false
	}
	return staff, c.Query("from"), c.Query("to"), true
}

// listPunches returns one person's punches by local date, voided ones included, most recently recorded first.
func (h *handlers) listPunches(c *gin.Context, params pagination.Params) ([]attendancePunch, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respondAttendanceError(c, errAttendanceRange)
		return nil, nil
	}
	staff, from, to, ok := attendanceRange(c, c.Query("staff_id"))
	if !ok {
		return nil, nil
	}
	rows := []attendancePunch{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Table(attendancePunchView).Select(attendancePunchColumns).
			Where("tenant_id=? AND hospital_id=? AND staff_id=? AND local_date::date BETWEEN ?::date AND ?::date", p.TenantID, hospital, staff, from, to).Find(&rows).Error
	})
	if err != nil {
		respondAttendanceError(c, err)
		return nil, nil
	}
	for i := range rows {
		redactPunch(c, &rows[i])
	}
	return rows, nil
}

func respondAttendanceError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	var duplicate attendanceDuplicate
	var problem *rosterProblem
	switch {
	case errors.As(err, &duplicate):
		respond.ConflictWithDetail(c, "This person already has a punch in the same direction within the duplicate window.", gin.H{"reason": "duplicate_punch", "punch_id": duplicate.id})
	case errors.As(err, &problem):
		respond.Error(c, problem.status, problem.code, problem.message)
	case errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "workforce_attendance_punches_pkey":
		respond.Error(c, errAttendanceReused.status, errAttendanceReused.code, errAttendanceReused.message)
	case errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "workforce_attendance_punch_voids_pkey":
		respond.Error(c, errAttendanceVoided.status, errAttendanceVoided.code, errAttendanceVoided.message)
	default:
		respondRosterError(c, err)
	}
}

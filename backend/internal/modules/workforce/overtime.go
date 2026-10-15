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
	"gorm.io/gorm/clause"

	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func (s overtimeSettings) PageKey() (time.Time, uuid.UUID) { return s.CreatedAt, s.RevisionID }
func (r overtimeRequest) PageKey() (time.Time, uuid.UUID)  { return r.CreatedAt, r.ID }
func (e overtimeEvent) PageKey() (time.Time, uuid.UUID)    { return e.CreatedAt, e.ID }

type overtimeSummaryPage struct {
	overtimeSummaryRow
	CreatedAt time.Time `json:"-"`
}

func (r overtimeSummaryPage) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.StaffID }

// overtimeLock serialises every overtime write in a hospital, so limit sums see each other's effects.
const overtimeLock = "overtime"

// maxOvertimeLead bounds how far ahead overtime can be planned.
const maxOvertimeLead = 92

var (
	errOvertimeSettingsInvalid = &rosterProblem{400, "invalid_request", "Use limits with daily ≤ weekly ≤ monthly, a filing window of 0 to 92 days, an effective date between 2000 and 2199 and confirm that you reviewed the policy."}
	errOvertimeSettingsStale   = &rosterProblem{409, "stale_version", "The overtime policy changed since you opened it. Reload before saving."}
	errOvertimeEffective       = &rosterProblem{400, "invalid_request", "The effective date cannot be earlier than the current revision's effective date."}
	errOvertimeInvalid         = &rosterProblem{400, "invalid_request", "Use a work date, start and end times that exist in the hospital timezone, a duration of 15 minutes to 24 hours and a reason of 1 to 500 characters."}
	errOvertimeNoteRequired    = &rosterProblem{400, "invalid_request", "Rejecting, revoking or approving fewer minutes than requested needs a note."}
	errOvertimeMinutes         = &rosterProblem{400, "invalid_request", "Approve between 1 minute and the requested minutes; rejections carry no approved minutes."}
	errOvertimeReused          = &rosterProblem{409, "request_reused", "This overtime id was already used with different values."}
	errOvertimeStale           = &rosterProblem{409, "stale_version", "The overtime request changed since you opened it. Reload before acting."}
	errOvertimeState           = &rosterProblem{409, "request_state", "The overtime request is no longer in a state that allows this action."}
	errOvertimeWindow          = &rosterProblem{409, "outside_filing_window", "The work date is outside the hospital's overtime filing window."}
	errOvertimeNoPolicy        = &rosterProblem{409, "no_overtime_policy", "Review the hospital's overtime policy and roster settings effective on or before the work date first."}
	errOvertimeOnLeave         = &rosterProblem{409, "on_leave", "This person has approved leave on the work date."}
	errOvertimeOverlap         = &rosterProblem{409, "overtime_overlap", "This person already has pending or approved overtime at that time."}
	errOvertimeStaffInactive   = &rosterProblem{409, "staff_inactive", "Overtime is recorded for active staff only."}
	errOvertimeSelfDecision    = errors.New("overtime is decided by someone other than the filer")
)

// overtimeDutyOverlap names the rostered duty an overtime range collides with.
type overtimeDutyOverlap struct{ ids []uuid.UUID }

func (overtimeDutyOverlap) Error() string { return "overtime overlaps rostered duty" }

// overtimeLimit reports the first period whose limit the request would exceed.
type overtimeLimit struct {
	period          string
	limit, counted  int
	periodFrom, end string
}

func (overtimeLimit) Error() string { return "overtime limit exceeded" }

const overtimeSettingsColumns = "revision_id,daily_limit_minutes,weekly_limit_minutes,monthly_limit_minutes,filing_window_days,effective_on::text AS effective_on,reviewed_by_subject,version,created_at,updated_at"
const overtimeSettingsRevisionColumns = "id AS revision_id,daily_limit_minutes,weekly_limit_minutes,monthly_limit_minutes,filing_window_days,effective_on::text AS effective_on,reviewed_by_subject,version,created_at,created_at AS updated_at"

const overtimeView = `(SELECT o.tenant_id,o.hospital_id,o.id,o.staff_id,s.employee_code,s.display_name,o.work_date::text AS work_date,o.start_time,o.end_time,o.timezone,o.starts_at AT TIME ZONE 'UTC' AS starts_at,o.ends_at AT TIME ZONE 'UTC' AS ends_at,
 o.requested_minutes,o.approved_minutes,CASE WHEN o.status='approved' THEN o.approved_minutes ELSE 0 END AS payable_minutes,o.policy_revision_id,o.reason,o.status,
 o.requested_by_subject,o.decided_by_subject,o.decided_at,o.revoked_by_subject,o.version,o.created_at,o.updated_at
 FROM workforce_overtime_requests o JOIN workforce_staff s ON (s.tenant_id,s.hospital_id,s.id)=(o.tenant_id,o.hospital_id,o.staff_id)) AS v`
const overtimeColumns = "id,staff_id,employee_code,display_name,work_date,start_time,end_time,timezone,starts_at,ends_at,requested_minutes,approved_minutes,payable_minutes,policy_revision_id,reason,status,requested_by_subject,decided_by_subject,decided_at,revoked_by_subject,version,created_at,updated_at"

// overtimeSlot is a request resolved against the hospital settings in force on its work date.
type overtimeSlot struct {
	staff            uuid.UUID
	workDate         time.Time
	start, end       string
	settingsRevision uuid.UUID
	timezone         string
	loc              *time.Location
	weekStart        int
	startsAt, endsAt time.Time
	requestedMinutes int
}

type overtimePolicy struct {
	ID                                                         uuid.UUID
	DailyLimitMinutes, WeeklyLimitMinutes, MonthlyLimitMinutes int
	FilingWindowDays                                           int
}

type overtimeRow struct {
	StaffID            uuid.UUID
	WorkDate           string
	StartTime, EndTime string
	RequestedMinutes   int
	Reason             string
	Status             string
	RequestedBySubject string
	Version            int64
}

func overtimeRequestID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("request"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "overtime request")
		return uuid.Nil, false
	}
	return id, true
}

func overtimeReader(c *gin.Context) bool {
	set, ok := authz.PermissionsFrom(c)
	return ok && (set.Has(PermOvertimeRequest) || set.Has(PermOvertimeApprove))
}

// validClock accepts exactly HH:MM on a 24-hour clock.
func validClock(s string) bool {
	t, err := time.Parse("15:04", s)
	return err == nil && t.Format("15:04") == s
}

func parseWorkDate(s string) (time.Time, bool) {
	day, err := time.Parse("2006-01-02", s)
	return day, err == nil && day.Year() >= 2000 && day.Year() <= 2199
}

// resolveOvertime places the wall-clock range on the work date in the hospital timezone in force that day.
func resolveOvertime(tx *gorm.DB, p authn.Principal, hospital, staff uuid.UUID, workDate, start, end string) (overtimeSlot, error) {
	day, ok := parseWorkDate(workDate)
	if !ok || !validClock(start) || !validClock(end) {
		return overtimeSlot{}, errOvertimeInvalid
	}
	var settings struct {
		ID        uuid.UUID
		Timezone  string
		WeekStart int
	}
	if err := tx.Raw(`SELECT id,timezone,week_start FROM workforce_roster_setting_revisions WHERE tenant_id=? AND hospital_id=? AND id=workforce_roster_settings_on(?,?,?::date)`,
		p.TenantID, hospital, p.TenantID, hospital, workDate).Scan(&settings).Error; err != nil {
		return overtimeSlot{}, err
	}
	if settings.ID == uuid.Nil {
		return overtimeSlot{}, errOvertimeNoPolicy
	}
	loc, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		return overtimeSlot{}, err
	}
	endDay := day
	if end <= start {
		endDay = day.AddDate(0, 0, 1)
	}
	startsAt, okStart := wallInstant(day, start, loc)
	endsAt, okEnd := wallInstant(endDay, end, loc)
	minutes := int(endsAt.Sub(startsAt) / time.Minute)
	if !okStart || !okEnd || minutes < 15 || minutes > 1440 {
		return overtimeSlot{}, errOvertimeInvalid
	}
	return overtimeSlot{staff: staff, workDate: day, start: start, end: end, settingsRevision: settings.ID, timezone: settings.Timezone, loc: loc,
		weekStart: settings.WeekStart, startsAt: startsAt, endsAt: endsAt, requestedMinutes: minutes}, nil
}

// checkOvertime applies the overtime rules; the exclusion constraint and table checks remain the backstop.
func checkOvertime(tx *gorm.DB, p authn.Principal, hospital, self uuid.UUID, s overtimeSlot, minutes int, filing bool) (overtimePolicy, error) {
	var policy overtimePolicy
	if err := tx.Raw(`SELECT id,daily_limit_minutes,weekly_limit_minutes,monthly_limit_minutes,filing_window_days FROM workforce_overtime_setting_revisions
 WHERE tenant_id=? AND hospital_id=? AND id=workforce_overtime_policy_on(?,?,?::date)`, p.TenantID, hospital, p.TenantID, hospital, s.workDate.Format(time.DateOnly)).Scan(&policy).Error; err != nil {
		return policy, err
	}
	if policy.ID == uuid.Nil {
		return policy, errOvertimeNoPolicy
	}
	if filing {
		now := time.Now().In(s.loc)
		today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		if s.workDate.Before(today.AddDate(0, 0, -policy.FilingWindowDays)) || s.workDate.After(today.AddDate(0, 0, maxOvertimeLead)) {
			return policy, errOvertimeWindow
		}
	}
	var person struct{ EmploymentStatus string }
	if err := tx.Raw(`SELECT employment_status FROM workforce_staff WHERE tenant_id=? AND hospital_id=? AND id=? FOR SHARE`, p.TenantID, hospital, s.staff).Scan(&person).Error; err != nil {
		return policy, err
	}
	if person.EmploymentStatus == "" {
		return policy, gorm.ErrRecordNotFound
	}
	if person.EmploymentStatus != "active" {
		return policy, errOvertimeStaffInactive
	}
	var duty []uuid.UUID
	if err := tx.Raw(`SELECT id FROM workforce_roster_allocations WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND removed_version IS NULL
 AND tstzrange(starts_at,ends_at,'[)') && tstzrange(?,?,'[)') ORDER BY id`, p.TenantID, hospital, s.staff, s.startsAt, s.endsAt).Scan(&duty).Error; err != nil {
		return policy, err
	}
	if len(duty) > 0 {
		return policy, overtimeDutyOverlap{ids: duty}
	}
	var away, clash bool
	if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_leave_requests r JOIN workforce_leave_request_days d ON (d.tenant_id,d.hospital_id,d.request_id)=(r.tenant_id,r.hospital_id,r.id)
  WHERE r.tenant_id=? AND r.hospital_id=? AND r.staff_id=? AND r.status='approved' AND d.day=?::date) AS away,
 EXISTS(SELECT 1 FROM workforce_overtime_requests WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND id<>? AND status IN ('pending','approved')
  AND tstzrange(starts_at,ends_at,'[)') && tstzrange(?,?,'[)')) AS clash`,
		p.TenantID, hospital, s.staff, s.workDate.Format(time.DateOnly), p.TenantID, hospital, s.staff, self, s.startsAt, s.endsAt).Row().Scan(&away, &clash); err != nil {
		return policy, err
	}
	if away {
		return policy, errOvertimeOnLeave
	}
	if clash {
		return policy, errOvertimeOverlap
	}
	weekday := (int(s.workDate.Weekday()) + 6) % 7
	weekFrom := s.workDate.AddDate(0, 0, -((weekday - s.weekStart + 7) % 7))
	weekTo := weekFrom.AddDate(0, 0, 6)
	monthFrom := time.Date(s.workDate.Year(), s.workDate.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthTo := monthFrom.AddDate(0, 1, -1)
	var counted struct{ Day, Week, Month int }
	if err := tx.Raw(`SELECT COALESCE(sum(m) FILTER (WHERE work_date=?::date),0) AS day,COALESCE(sum(m) FILTER (WHERE work_date BETWEEN ?::date AND ?::date),0) AS week,
 COALESCE(sum(m) FILTER (WHERE work_date BETWEEN ?::date AND ?::date),0) AS month
 FROM (SELECT work_date,CASE status WHEN 'approved' THEN approved_minutes ELSE requested_minutes END AS m FROM workforce_overtime_requests
  WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND id<>? AND status IN ('pending','approved') AND work_date BETWEEN LEAST(?::date,?::date) AND GREATEST(?::date,?::date)) AS live`,
		s.workDate.Format(time.DateOnly), weekFrom.Format(time.DateOnly), weekTo.Format(time.DateOnly), monthFrom.Format(time.DateOnly), monthTo.Format(time.DateOnly),
		p.TenantID, hospital, s.staff, self, weekFrom.Format(time.DateOnly), monthFrom.Format(time.DateOnly), weekTo.Format(time.DateOnly), monthTo.Format(time.DateOnly)).Scan(&counted).Error; err != nil {
		return policy, err
	}
	for _, period := range []overtimeLimit{
		{"day", policy.DailyLimitMinutes, counted.Day, s.workDate.Format(time.DateOnly), s.workDate.Format(time.DateOnly)},
		{"week", policy.WeeklyLimitMinutes, counted.Week, weekFrom.Format(time.DateOnly), weekTo.Format(time.DateOnly)},
		{"month", policy.MonthlyLimitMinutes, counted.Month, monthFrom.Format(time.DateOnly), monthTo.Format(time.DateOnly)},
	} {
		if period.counted+minutes > period.limit {
			return policy, period
		}
	}
	return policy, nil
}

func (h *handlers) publishOvertime(tx *gorm.DB, p authn.Principal, hospital, id uuid.UUID, status string, version int64) error {
	data, err := json.Marshal(workforcecontract.OvertimeChangedData{HospitalID: hospital.String(), RequestID: id.String(), Status: status, Version: version})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, OvertimeChanged, events.Event{Type: OvertimeChanged, Version: 1, TenantID: p.TenantID, Data: data})
}

func loadOvertime(tx *gorm.DB, tenant string, hospital, id uuid.UUID) (overtimeRequest, error) {
	var r overtimeRequest
	return r, tx.Table(overtimeView).Select(overtimeColumns).Where("tenant_id=? AND hospital_id=? AND id=?", tenant, hospital, id).Take(&r).Error
}

func respondOvertime(c *gin.Context, r overtimeRequest) {
	if !overtimeReader(c) {
		r.Reason = ""
	}
	respond.OK(c, r)
}

func (h *handlers) getOvertimeSettings(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	var result overtimeSettings
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		return tx.Table("workforce_overtime_settings").Select(overtimeSettingsColumns).Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Take(&result).Error
	})
	if err != nil {
		respondOvertimeError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) overtimeSettingsHistory(c *gin.Context, params pagination.Params) ([]overtimeSettings, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	rows := []overtimeSettings{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Table("workforce_overtime_setting_revisions").Select(overtimeSettingsRevisionColumns).Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Find(&rows).Error
	})
	if err != nil {
		respondOvertimeError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) putOvertimeSettings(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	var in overtimeSettingsInput
	if !decode(c, &in) {
		return
	}
	if _, valid := parseWorkDate(in.EffectiveOn); !valid || !*in.Reviewed || *in.DailyLimitMinutes > *in.WeeklyLimitMinutes || *in.WeeklyLimitMinutes > *in.MonthlyLimitMinutes {
		respondOvertimeError(c, errOvertimeSettingsInvalid)
		return
	}
	var result overtimeSettings
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_overtime", overtimeLock); err != nil {
			return err
		}
		var old overtimeSettings
		err := tx.Table("workforce_overtime_settings").Select(overtimeSettingsColumns).Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Take(&old).Error
		version := int64(1)
		switch {
		case err == nil:
			if old.DailyLimitMinutes == *in.DailyLimitMinutes && old.WeeklyLimitMinutes == *in.WeeklyLimitMinutes && old.MonthlyLimitMinutes == *in.MonthlyLimitMinutes &&
				old.FilingWindowDays == *in.FilingWindowDays && old.EffectiveOn == in.EffectiveOn {
				result = old
				return nil
			}
			if old.Version != *in.ExpectedVersion {
				return errOvertimeSettingsStale
			}
			if in.EffectiveOn < old.EffectiveOn {
				return errOvertimeEffective
			}
			version = old.Version + 1
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		case *in.ExpectedVersion != 0:
			return gorm.ErrRecordNotFound
		}
		revision := uuid.New()
		if err := tx.Exec(`INSERT INTO workforce_overtime_settings(tenant_id,hospital_id,daily_limit_minutes,weekly_limit_minutes,monthly_limit_minutes,filing_window_days,effective_on,reviewed_by_subject,version,revision_id) VALUES(?,?,?,?,?,?,?,?,?,?)
 ON CONFLICT(tenant_id,hospital_id) DO UPDATE SET daily_limit_minutes=EXCLUDED.daily_limit_minutes,weekly_limit_minutes=EXCLUDED.weekly_limit_minutes,monthly_limit_minutes=EXCLUDED.monthly_limit_minutes,
 filing_window_days=EXCLUDED.filing_window_days,effective_on=EXCLUDED.effective_on,reviewed_by_subject=EXCLUDED.reviewed_by_subject,version=EXCLUDED.version,revision_id=EXCLUDED.revision_id,updated_at=clock_timestamp()`,
			p.TenantID, hospital, *in.DailyLimitMinutes, *in.WeeklyLimitMinutes, *in.MonthlyLimitMinutes, *in.FilingWindowDays, in.EffectiveOn, p.Subject, version, revision).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_overtime_setting_revisions(tenant_id,hospital_id,id,daily_limit_minutes,weekly_limit_minutes,monthly_limit_minutes,filing_window_days,effective_on,reviewed_by_subject,version) VALUES(?,?,?,?,?,?,?,?,?,?)`,
			p.TenantID, hospital, revision, *in.DailyLimitMinutes, *in.WeeklyLimitMinutes, *in.MonthlyLimitMinutes, *in.FilingWindowDays, in.EffectiveOn, p.Subject, version).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.overtime_settings.changed',?,jsonb_build_object('version',?::bigint,'previous_version',?::bigint))`,
			p.TenantID, hospital, p.Subject, revision, version, version-1).Error; err != nil {
			return err
		}
		data, err := json.Marshal(workforcecontract.OvertimeSettingsChangedData{HospitalID: hospital.String(), RevisionID: revision.String(), Version: version})
		if err != nil {
			return err
		}
		if err := h.bus.Publish(tx, OvertimeSettingsChanged, events.Event{Type: OvertimeSettingsChanged, Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
			return err
		}
		return tx.Table("workforce_overtime_settings").Select(overtimeSettingsColumns).Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Take(&result).Error
	})
	if err != nil {
		respondOvertimeError(c, err)
		return
	}
	respond.OK(c, result)
}

// putOvertime files a request under a client-chosen id; an identical retry returns the stored request.
func (h *handlers) putOvertime(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, ok := overtimeRequestID(c)
	if !ok {
		return
	}
	var in overtimeRequestInput
	if !decode(c, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.Reason == "" || !validLeaveText(in.Reason) || in.StaffID == uuid.Nil {
		respondOvertimeError(c, errOvertimeInvalid)
		return
	}
	var result overtimeRequest
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_overtime", overtimeLock); err != nil {
			return err
		}
		var existing overtimeRow
		if err := tx.Raw(`SELECT staff_id,work_date::text AS work_date,start_time,end_time,reason FROM workforce_overtime_requests WHERE tenant_id=? AND hospital_id=? AND id=?`, p.TenantID, hospital, id).Scan(&existing).Error; err != nil {
			return err
		}
		if existing.StaffID != uuid.Nil {
			if existing.StaffID != in.StaffID || existing.WorkDate != in.WorkDate || existing.StartTime != in.StartTime || existing.EndTime != in.EndTime || existing.Reason != in.Reason {
				return errOvertimeReused
			}
			var err error
			result, err = loadOvertime(tx, p.TenantID, hospital, id)
			return err
		}
		slot, err := resolveOvertime(tx, p, hospital, in.StaffID, in.WorkDate, in.StartTime, in.EndTime)
		if err != nil {
			return err
		}
		if _, err := checkOvertime(tx, p, hospital, id, slot, slot.requestedMinutes, true); err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_overtime_requests(tenant_id,hospital_id,id,staff_id,work_date,start_time,end_time,settings_revision_id,timezone,starts_at,ends_at,reason,status,requested_by_subject,version)
 VALUES(?,?,?,?,?::date,?,?,?,?,?,?,?,'pending',?,1)`, p.TenantID, hospital, id, in.StaffID, in.WorkDate, in.StartTime, in.EndTime, slot.settingsRevision, slot.timezone, slot.startsAt, slot.endsAt, in.Reason, p.Subject).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_overtime_events(tenant_id,hospital_id,id,request_id,version,status,actor_subject) VALUES(?,?,?,?,1,'pending',?)`, p.TenantID, hospital, uuid.New(), id, p.Subject).Error; err != nil {
			return err
		}
		// The reason may describe personal circumstances, so audit rows and events carry identifiers and minutes only.
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.overtime.filed',?,jsonb_build_object('staff_id',?::uuid,'requested_minutes',?::int,'version',1))`,
			p.TenantID, hospital, p.Subject, id, in.StaffID, slot.requestedMinutes).Error; err != nil {
			return err
		}
		if err := h.publishOvertime(tx, p, hospital, id, "pending", 1); err != nil {
			return err
		}
		result, err = loadOvertime(tx, p.TenantID, hospital, id)
		return err
	})
	if err != nil {
		respondOvertimeError(c, err)
		return
	}
	respondOvertime(c, result)
}

type overtimeTransition struct {
	from, to string
	// separate requires the actor to differ from the filer; approves re-checks the rules and pins the policy.
	separate, decides, approves bool
}

func (h *handlers) decideOvertime(c *gin.Context) {
	var in overtimeDecisionInput
	if !decode(c, &in) {
		return
	}
	if in.Decision == "reject" {
		if in.ApprovedMinutes != nil {
			respondOvertimeError(c, errOvertimeMinutes)
			return
		}
		h.transitionOvertime(c, overtimeTransition{from: "pending", to: "rejected", separate: true, decides: true}, *in.ExpectedVersion, in.Note, nil)
		return
	}
	h.transitionOvertime(c, overtimeTransition{from: "pending", to: "approved", separate: true, decides: true, approves: true}, *in.ExpectedVersion, in.Note, in.ApprovedMinutes)
}

func (h *handlers) cancelOvertime(c *gin.Context) {
	var in overtimeCancelInput
	if !decode(c, &in) {
		return
	}
	h.transitionOvertime(c, overtimeTransition{from: "pending", to: "cancelled"}, *in.ExpectedVersion, in.Note, nil)
}

func (h *handlers) revokeOvertime(c *gin.Context) {
	var in overtimeCancelInput
	if !decode(c, &in) {
		return
	}
	h.transitionOvertime(c, overtimeTransition{from: "approved", to: "revoked", separate: true}, *in.ExpectedVersion, in.Note, nil)
}

// transitionOvertime moves a request one step; replaying the step that produced the current version returns it unchanged.
func (h *handlers) transitionOvertime(c *gin.Context, t overtimeTransition, expected int64, note string, approved *int) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, ok := overtimeRequestID(c)
	if !ok {
		return
	}
	note = strings.TrimSpace(note)
	if !validLeaveText(note) {
		respondOvertimeError(c, errOvertimeInvalid)
		return
	}
	if (t.to == "rejected" || t.to == "revoked") && note == "" {
		respondOvertimeError(c, errOvertimeNoteRequired)
		return
	}
	var result overtimeRequest
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_overtime", overtimeLock); err != nil {
			return err
		}
		var r overtimeRow
		if err := tx.Raw(`SELECT staff_id,work_date::text AS work_date,start_time,end_time,requested_minutes,status,requested_by_subject,version
 FROM workforce_overtime_requests WHERE tenant_id=? AND hospital_id=? AND id=? FOR UPDATE`, p.TenantID, hospital, id).Scan(&r).Error; err != nil {
			return err
		}
		if r.StaffID == uuid.Nil {
			return gorm.ErrRecordNotFound
		}
		minutes := r.RequestedMinutes
		if approved != nil {
			minutes = *approved
		}
		if t.approves && minutes > r.RequestedMinutes {
			return errOvertimeMinutes
		}
		if t.approves && minutes < r.RequestedMinutes && note == "" {
			return errOvertimeNoteRequired
		}
		var recorded *int
		if t.approves {
			recorded = &minutes
		}
		if r.Version == expected+1 && r.Status == t.to {
			var replay bool
			if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_overtime_events WHERE tenant_id=? AND hospital_id=? AND request_id=? AND version=? AND actor_subject=? AND note=?
 AND (?::boolean OR approved_minutes IS NOT DISTINCT FROM ?::int))`, p.TenantID, hospital, id, r.Version, p.Subject, note, !t.approves, recorded).Scan(&replay).Error; err != nil {
				return err
			}
			if replay {
				var err error
				result, err = loadOvertime(tx, p.TenantID, hospital, id)
				return err
			}
		}
		if r.Version != expected {
			return errOvertimeStale
		}
		if r.Status != t.from {
			return errOvertimeState
		}
		if t.separate && r.RequestedBySubject == p.Subject {
			return errOvertimeSelfDecision
		}
		set := "status=?,version=version+1,updated_at=clock_timestamp()"
		args := []any{t.to}
		if t.decides {
			set += ",decided_by_subject=?,decided_at=clock_timestamp()"
			args = append(args, p.Subject)
		}
		if t.approves {
			slot, err := resolveOvertime(tx, p, hospital, r.StaffID, r.WorkDate, r.StartTime, r.EndTime)
			if err != nil {
				return err
			}
			policy, err := checkOvertime(tx, p, hospital, id, slot, minutes, false)
			if err != nil {
				return err
			}
			set += ",approved_minutes=?,policy_revision_id=?"
			args = append(args, minutes, policy.ID)
		}
		if t.to == "revoked" {
			set += ",revoked_by_subject=?"
			args = append(args, p.Subject)
		}
		update := tx.Exec(`UPDATE workforce_overtime_requests SET `+set+` WHERE tenant_id=? AND hospital_id=? AND id=? AND version=? AND status=?`, append(args, p.TenantID, hospital, id, expected, t.from)...)
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return errOvertimeStale
		}
		version := expected + 1
		if err := tx.Exec(`INSERT INTO workforce_overtime_events(tenant_id,hospital_id,id,request_id,version,status,approved_minutes,note,actor_subject)
 SELECT ?,?,?,?,?,?,approved_minutes,?,? FROM workforce_overtime_requests WHERE tenant_id=? AND hospital_id=? AND id=?`,
			p.TenantID, hospital, uuid.New(), id, version, t.to, note, p.Subject, p.TenantID, hospital, id).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes)
 SELECT ?,?,?,?,?,jsonb_build_object('previous_status',?::text,'version',?::bigint,'approved_minutes',approved_minutes) FROM workforce_overtime_requests WHERE tenant_id=? AND hospital_id=? AND id=?`,
			p.TenantID, hospital, p.Subject, "workforce.overtime."+t.to, id, t.from, version, p.TenantID, hospital, id).Error; err != nil {
			return err
		}
		if err := h.publishOvertime(tx, p, hospital, id, t.to, version); err != nil {
			return err
		}
		var err error
		result, err = loadOvertime(tx, p.TenantID, hospital, id)
		return err
	})
	if err != nil {
		respondOvertimeError(c, err)
		return
	}
	respondOvertime(c, result)
}

func (h *handlers) getOvertime(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, ok := overtimeRequestID(c)
	if !ok {
		return
	}
	var result overtimeRequest
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var err error
		result, err = loadOvertime(tx, p.TenantID, hospital, id)
		return err
	})
	if err != nil {
		respondOvertimeError(c, err)
		return
	}
	respondOvertime(c, result)
}

// listOvertime filters by status, person and work dates, newest filing first.
func (h *handlers) listOvertime(c *gin.Context, params pagination.Params) ([]overtimeRequest, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	where := "tenant_id=? AND hospital_id=?"
	args := []any{p.TenantID, hospital}
	valid := params.Limit <= 100
	if s := c.Query("status"); s != "" {
		valid = valid && slices.Contains(overtimeStatuses, s)
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
	}{{"from", " AND work_date::date>=?::date", &from}, {"to", " AND work_date::date<=?::date", &to}} {
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
	rows := []overtimeRequest{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Table(overtimeView).Select(overtimeColumns).Where(where, args...).Find(&rows).Error
	})
	if err != nil {
		respondOvertimeError(c, err)
		return nil, nil
	}
	if !overtimeReader(c) {
		for i := range rows {
			rows[i].Reason = ""
		}
	}
	return rows, nil
}

func (h *handlers) overtimeHistory(c *gin.Context, params pagination.Params) ([]overtimeEvent, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	id, ok := overtimeRequestID(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	rows := []overtimeEvent{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var exists bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_overtime_requests WHERE tenant_id=? AND hospital_id=? AND id=?)`, p.TenantID, hospital, id).Scan(&exists).Error; err != nil {
			return err
		}
		if !exists {
			return gorm.ErrRecordNotFound
		}
		return tenantdb.ApplyKeyset(tx, params).Table("workforce_overtime_events").Select("id,version,status,approved_minutes,note,actor_subject,created_at").Where("tenant_id=? AND hospital_id=? AND request_id=?", p.TenantID, hospital, id).Find(&rows).Error
	})
	if err != nil {
		respondOvertimeError(c, err)
		return nil, nil
	}
	// Decision notes may restate the reason, so they follow the same redaction.
	if !overtimeReader(c) {
		for i := range rows {
			rows[i].Note = ""
		}
	}
	return rows, nil
}

// overtimeSummary totals one calendar month per person; only approved minutes of approved requests are payable.
func (h *handlers) overtimeSummary(c *gin.Context, params pagination.Params) ([]overtimeSummaryPage, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	month, err := time.Parse("2006-01", c.Query("month"))
	if err != nil || month.Year() < 2000 || month.Year() > 2199 || params.Limit > 100 {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Use a month as YYYY-MM between 2000 and 2199 and a page limit no greater than 100.")
		return nil, nil
	}
	rows := []overtimeSummaryPage{}
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		view := `(SELECT s.tenant_id,s.hospital_id,s.id,s.id AS staff_id,s.employee_code,s.display_name,s.created_at,
 COALESCE(sum(o.requested_minutes) FILTER (WHERE o.status='pending'),0)::int AS pending_minutes,
 COALESCE(sum(o.requested_minutes) FILTER (WHERE o.status='approved'),0)::int AS approved_requested_minutes,
 COALESCE(sum(o.approved_minutes) FILTER (WHERE o.status='approved'),0)::int AS payable_minutes
 FROM workforce_staff s JOIN workforce_overtime_requests o ON (o.tenant_id,o.hospital_id,o.staff_id)=(s.tenant_id,s.hospital_id,s.id)
 WHERE s.tenant_id=? AND s.hospital_id=? AND o.work_date BETWEEN ?::date AND ?::date GROUP BY s.tenant_id,s.hospital_id,s.id) AS v`
		var found []struct {
			StaffID                                                  uuid.UUID
			EmployeeCode, DisplayName                                string
			PendingMinutes, ApprovedRequestedMinutes, PayableMinutes int
			CreatedAt                                                time.Time
		}
		if err := tenantdb.ApplyKeyset(tx, params).Table(view, p.TenantID, hospital, month.Format(time.DateOnly), month.AddDate(0, 1, -1).Format(time.DateOnly)).
			Select("staff_id,employee_code,display_name,pending_minutes,approved_requested_minutes,payable_minutes,created_at").Find(&found).Error; err != nil {
			return err
		}
		for _, f := range found {
			rows = append(rows, overtimeSummaryPage{overtimeSummaryRow{StaffID: f.StaffID, EmployeeCode: f.EmployeeCode, DisplayName: f.DisplayName,
				PendingMinutes: f.PendingMinutes, ApprovedRequestedMinutes: f.ApprovedRequestedMinutes, PayableMinutes: f.PayableMinutes}, f.CreatedAt})
		}
		return nil
	})
	if err != nil {
		respondOvertimeError(c, err)
		return nil, nil
	}
	return rows, nil
}

func respondOvertimeError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	var duty overtimeDutyOverlap
	var limit overtimeLimit
	switch {
	case errors.Is(err, errOvertimeSelfDecision), errors.As(err, &pg) && pg.ConstraintName == "workforce_overtime_separation":
		respond.Forbidden(c, "Someone other than the person who filed the overtime must decide or revoke it.")
	case errors.As(err, &duty):
		respond.ConflictWithDetail(c, "This overtime overlaps the person's rostered duty.", gin.H{"reason": "overlaps_duty", "allocation_ids": duty.ids})
	case errors.As(err, &limit):
		respond.ConflictWithDetail(c, "This overtime would exceed the hospital's "+map[string]string{"day": "daily", "week": "weekly", "month": "monthly"}[limit.period]+" limit.", gin.H{"reason": "limit_exceeded", "period": limit.period,
			"limit_minutes": limit.limit, "counted_minutes": limit.counted, "period_from": limit.periodFrom, "period_to": limit.end})
	case errors.As(err, &pg) && pg.ConstraintName == "workforce_overtime_overlap":
		respond.Error(c, errOvertimeOverlap.status, errOvertimeOverlap.code, errOvertimeOverlap.message)
	case errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "workforce_overtime_requests_pkey":
		respond.Error(c, errOvertimeReused.status, errOvertimeReused.code, errOvertimeReused.message)
	default:
		respondRosterError(c, err)
	}
}

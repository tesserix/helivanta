package workforce

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func (r leaveRequest) PageKey() (time.Time, uuid.UUID)      { return r.CreatedAt, r.ID }
func (e leaveRequestEvent) PageKey() (time.Time, uuid.UUID) { return e.CreatedAt, e.ID }

var errLeaveRequestConflict = errors.New("leave request id reused with different values")
var errLeaveRequestVersion = errors.New("leave request changed")
var errLeaveRequestState = errors.New("leave request is not in the expected state")
var errLeaveSelfDecision = errors.New("leave requests must be decided by someone other than their filer or linked staff member")
var errLeaveNotEntitled = errors.New("leave type is not available to the staff member")
var errLeaveUnit = errors.New("leave type does not allow the unit")
var errLeaveCrossYear = errors.New("leave request spans leave years")
var errLeaveCalendar = errors.New("holiday calendar missing")
var errLeaveNoWorkingDay = errors.New("leave request covers only holidays")
var errLeaveOverlap = errors.New("leave request overlaps live leave")
var errLeaveClubbing = errors.New("leave request clubs a non-clubbable type")
var errLeaveProbationAccess = errors.New("probation check needs enrollment access")
var errLeaveProbation = errors.New("staff member is still on probation")
var errLeaveBalance = errors.New("leave balance is insufficient")
var errLeaveStaffInactive = errors.New("staff member is not active")
var errLeavePending = errors.New("leave year has pending requests")
var errLeaveNoteRequired = errors.New("leave decision needs a note")

var leaveRequestStatuses = []string{"pending", "approved", "rejected", "cancelled"}

// Rows show the staff member and the type code and name pinned at filing.
const leaveRequestView = `(SELECT r.tenant_id,r.hospital_id,r.id,r.staff_id,s.employee_code,s.display_name,r.leave_type_id,r.leave_type_revision_id,t.code,t.name,r.assignment_revision_id,r.leave_year,r.unit,
 r.start_on::text AS start_on,r.end_on::text AS end_on,r.session,r.minutes,r.half_days,r.reason,r.status,r.requested_by_subject,r.decided_by_subject,r.decided_at,r.version,r.created_at,r.updated_at
 FROM workforce_leave_requests r JOIN workforce_staff s ON (s.tenant_id,s.hospital_id,s.id)=(r.tenant_id,r.hospital_id,r.staff_id)
 JOIN workforce_leave_config_revisions t ON (t.tenant_id,t.hospital_id,t.id)=(r.tenant_id,r.hospital_id,r.leave_type_revision_id)) AS v`
const leaveRequestColumns = "id,staff_id,employee_code,display_name,leave_type_id,leave_type_revision_id,code,name,assignment_revision_id,leave_year,unit,start_on,end_on,session,minutes,half_days,reason,status,requested_by_subject,decided_by_subject,decided_at,version,created_at,updated_at"

type leaveRequestRow struct {
	TenantID             string
	HospitalID           uuid.UUID
	ID                   uuid.UUID
	StaffID              uuid.UUID
	LeaveTypeID          uuid.UUID
	LeaveTypeRevisionID  uuid.UUID
	AssignmentRevisionID uuid.UUID
	LeaveYear            int
	Unit                 string
	StartOn              time.Time
	EndOn                time.Time
	Session              *string
	Minutes              *int
	HalfDays             int
	Reason               string
	Status               string
	RequestedBySubject   string
	Version              int64
}

type leaveRequestDayRow struct {
	TenantID           string
	HospitalID         uuid.UUID
	RequestID          uuid.UUID
	Day                time.Time
	HalfDays           int
	Holiday            bool
	CalendarRevisionID uuid.UUID
}

// leaveTerms are the rules a request is judged by: its pinned type revision read through its pinned assignment.
type leaveTerms struct {
	TypeActive        bool
	AssignmentActive  bool
	BalanceTracked    bool
	HalfDayAllowed    bool
	ShortLeaveMinutes int
	Clubbable         bool
	InStructure       bool
	ProbationEligible bool
}

func loadLeaveTerms(tx *gorm.DB, tenant string, hospital, typeRevision, assignmentRevision uuid.UUID) (leaveTerms, error) {
	var t leaveTerms
	err := tx.Raw(`SELECT r.active AS type_active,a.active AS assignment_active,k.balance_tracked,k.half_day_allowed,k.short_leave_minutes,k.clubbable,
 e.revision_id IS NOT NULL AS in_structure,COALESCE(e.probation_eligible,false) AS probation_eligible
 FROM workforce_leave_config_revisions r JOIN workforce_leave_type_rules k ON (k.tenant_id,k.hospital_id,k.revision_id)=(r.tenant_id,r.hospital_id,r.id)
 JOIN workforce_staff_leave_assignment_revisions a ON (a.tenant_id,a.hospital_id,a.id)=(r.tenant_id,r.hospital_id,?::uuid)
 LEFT JOIN LATERAL (SELECT se.revision_id,se.probation_eligible FROM workforce_leave_structure_entries se
  JOIN workforce_leave_config_revisions st ON (st.tenant_id,st.hospital_id,st.id)=(se.tenant_id,se.hospital_id,se.leave_type_revision_id)
  WHERE (se.tenant_id,se.hospital_id,se.revision_id)=(a.tenant_id,a.hospital_id,a.structure_revision_id) AND st.config_id=r.config_id LIMIT 1) e ON true
 WHERE r.tenant_id=? AND r.hospital_id=? AND r.id=? AND r.kind='leave_type'`, assignmentRevision, tenant, hospital, typeRevision).Take(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return t, errLeaveNotEntitled
	}
	return t, err
}

// checkLeaveStanding runs at filing and again at approval, so a decision sees current employment and ledger state.
func checkLeaveStanding(tx *gorm.DB, c *gin.Context, p authn.Principal, r leaveRequestRow, t leaveTerms, filing bool) error {
	var person struct{ EmploymentStatus string }
	if err := tx.Raw(`SELECT employment_status FROM workforce_staff WHERE tenant_id=? AND hospital_id=? AND id=? FOR SHARE`, p.TenantID, r.HospitalID, r.StaffID).Scan(&person).Error; err != nil {
		return err
	}
	switch person.EmploymentStatus {
	case "":
		return gorm.ErrRecordNotFound
	case "active":
	default:
		return errLeaveStaffInactive
	}
	if !t.ProbationEligible {
		set, _ := authz.PermissionsFrom(c)
		if set.Has(PermEnrollmentManage) {
			if err := enrollmentScope(tx, p, r.HospitalID); err != nil {
				return err
			}
		} else {
			allowed, err := reviewedLeaveStanding(tx, c, p, r)
			if err != nil {
				return err
			}
			if !allowed {
				return errLeaveProbationAccess
			}
		}
		var ended bool
		if err := tx.Raw(`SELECT COALESCE((SELECT probation_end_date < ?::date FROM workforce_enrollment_profile WHERE tenant_id=? AND hospital_id=? AND staff_id=?),false)`, r.StartOn, p.TenantID, r.HospitalID, r.StaffID).Scan(&ended).Error; err != nil {
			return err
		}
		if !ended {
			return errLeaveProbation
		}
	}
	if !t.BalanceTracked || r.HalfDays == 0 {
		return nil
	}
	// Filing holds other pending requests against the balance; approval needs only the ledger, so the first approval wins.
	var available int
	if err := tx.Raw(`SELECT (COALESCE((SELECT sum(half_days) FROM workforce_leave_ledger WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND leave_type_id=? AND leave_year=?),0)
 - CASE WHEN ? THEN COALESCE((SELECT sum(half_days) FROM workforce_leave_requests WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND leave_type_id=? AND leave_year=? AND status='pending' AND id<>?),0) ELSE 0 END)::int`,
		p.TenantID, r.HospitalID, r.StaffID, r.LeaveTypeID, r.LeaveYear, filing, p.TenantID, r.HospitalID, r.StaffID, r.LeaveTypeID, r.LeaveYear, r.ID).Scan(&available).Error; err != nil {
		return err
	}
	if available < r.HalfDays {
		return errLeaveBalance
	}
	return nil
}

// planLeaveDays counts each date against the current holiday calendar of its year; optional holidays are working days.
func planLeaveDays(tx *gorm.DB, r leaveRequestRow) ([]leaveRequestDayRow, int, error) {
	years := []int{}
	for y := r.StartOn.Year(); y <= r.EndOn.Year(); y++ {
		years = append(years, y)
	}
	var calendars []struct {
		CalendarYear int
		RevisionID   uuid.UUID
	}
	if err := tx.Raw(`SELECT calendar_year,revision_id FROM workforce_leave_configs WHERE tenant_id=? AND hospital_id=? AND kind='holiday_calendar' AND active AND calendar_year IN ?`, r.TenantID, r.HospitalID, years).Scan(&calendars).Error; err != nil {
		return nil, 0, err
	}
	if len(calendars) != len(years) {
		return nil, 0, errLeaveCalendar
	}
	pinned := map[int]uuid.UUID{}
	revisions := make([]uuid.UUID, 0, len(calendars))
	for _, cal := range calendars {
		pinned[cal.CalendarYear] = cal.RevisionID
		revisions = append(revisions, cal.RevisionID)
	}
	var holidays []string
	if err := tx.Raw(`SELECT holiday_date::text FROM workforce_leave_holidays WHERE tenant_id=? AND hospital_id=? AND revision_id IN ? AND category IN ('regular','government') AND holiday_date BETWEEN ? AND ?`,
		r.TenantID, r.HospitalID, revisions, r.StartOn, r.EndOn).Scan(&holidays).Error; err != nil {
		return nil, 0, err
	}
	off := map[string]bool{}
	for _, d := range holidays {
		off[d] = true
	}
	per := 2
	switch r.Unit {
	case "half_day":
		per = 1
	case "short_leave":
		per = 0
	}
	days := []leaveRequestDayRow{}
	total, working := 0, 0
	for d := r.StartOn; !d.After(r.EndOn); d = d.AddDate(0, 0, 1) {
		holiday := off[d.Format("2006-01-02")]
		count := per
		if holiday {
			count = 0
		} else {
			working++
		}
		total += count
		days = append(days, leaveRequestDayRow{TenantID: r.TenantID, HospitalID: r.HospitalID, RequestID: r.ID, Day: d, HalfDays: count, Holiday: holiday, CalendarRevisionID: pinned[d.Year()]})
	}
	if working == 0 {
		return nil, 0, errLeaveNoWorkingDay
	}
	return days, total, nil
}

func validLeaveText(s string) bool {
	return utf8.ValidString(s) && !strings.ContainsRune(s, 0) && utf8.RuneCountInString(s) <= 500
}

// parseLeaveRequestInput checks shape only; type rules are applied once the pinned revision is known.
func parseLeaveRequestInput(in *leaveRequestInput) (time.Time, time.Time, bool) {
	in.Reason = strings.TrimSpace(in.Reason)
	start, startErr := time.Parse("2006-01-02", in.StartOn)
	end, endErr := time.Parse("2006-01-02", in.EndOn)
	if in.LeaveTypeID == uuid.Nil || startErr != nil || endErr != nil || start.Year() < 2000 || end.Before(start) || end.After(start.AddDate(0, 0, 365)) || end.Year() > 2199 || !validLeaveText(in.Reason) {
		return start, end, false
	}
	switch in.Unit {
	case "full_day":
		return start, end, in.Session == nil && in.Minutes == nil
	case "half_day":
		return start, end, in.Session != nil && in.Minutes == nil && start.Equal(end)
	default:
		return start, end, in.Session == nil && in.Minutes != nil && *in.Minutes%15 == 0 && start.Equal(end)
	}
}

func leaveRequestReader(c *gin.Context) bool {
	set, ok := authz.PermissionsFrom(c)
	return ok && (set.Has(PermLeaveRequest) || set.Has(PermLeaveApprove))
}

func loadLeaveRequestDetail(tx *gorm.DB, tenant string, hospital, staffID, id uuid.UUID) (leaveRequestDetail, error) {
	d := leaveRequestDetail{Days: []leaveRequestDay{}, RosterClashes: []leaveRosterClash{}}
	if err := tx.Table(leaveRequestView).Select(leaveRequestColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", tenant, hospital, staffID, id).Take(&d.Request).Error; err != nil {
		return d, err
	}
	if err := tx.Raw(`SELECT day::text AS day,half_days,holiday,calendar_revision_id FROM workforce_leave_request_days WHERE tenant_id=? AND hospital_id=? AND request_id=? ORDER BY day`, tenant, hospital, id).Scan(&d.Days).Error; err != nil {
		return d, err
	}
	if d.Request.Status != "pending" && d.Request.Status != "approved" {
		return d, nil
	}
	return d, tx.Raw(`SELECT a.roster_id,r.name AS roster_name,a.roster_date::text AS roster_date,a.starts_at,a.ends_at
 FROM workforce_roster_allocations a JOIN workforce_rosters r ON (r.tenant_id,r.hospital_id,r.id)=(a.tenant_id,a.hospital_id,a.roster_id)
 WHERE a.tenant_id=? AND a.hospital_id=? AND a.staff_id=? AND a.removed_version IS NULL AND r.status<>'discarded' AND a.roster_date BETWEEN ?::date AND ?::date
 ORDER BY a.roster_date,a.starts_at,a.id LIMIT 1000`, tenant, hospital, staffID, d.Request.StartOn, d.Request.EndOn).Scan(&d.RosterClashes).Error
}

func (h *handlers) publishLeaveRequest(tx *gorm.DB, p authn.Principal, r leaveRequestRow) error {
	data, err := json.Marshal(workforcecontract.LeaveRequestChangedData{HospitalID: r.HospitalID.String(), StaffID: r.StaffID.String(), RequestID: r.ID.String(), Status: r.Status, Version: r.Version})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, LeaveRequestChanged, events.Event{Type: LeaveRequestChanged, Version: 1, TenantID: p.TenantID, Data: data})
}

func leaveRequestIDs(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	staffID, err := uuid.Parse(c.Param("id"))
	if err != nil || staffID == uuid.Nil {
		respond.NotFound(c, "staff")
		return uuid.Nil, uuid.Nil, false
	}
	id, err := uuid.Parse(c.Param("request"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "leave request")
		return uuid.Nil, uuid.Nil, false
	}
	return staffID, id, true
}

// putLeaveRequest files a request under a client-chosen id; an identical retry returns the stored request.
func (h *handlers) putLeaveRequest(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staffID, id, ok := leaveRequestIDs(c)
	if !ok {
		return
	}
	var in leaveRequestInput
	if !decode(c, &in) {
		return
	}
	start, end, valid := parseLeaveRequestInput(&in)
	if !valid {
		respondLeaveRequestError(c, errLeaveInvalid)
		return
	}
	var result leaveRequestDetail
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_leave_ledger", leaveLedgerLock); err != nil {
			return err
		}
		var existing leaveRequestRow
		if err := tx.Raw(`SELECT staff_id,leave_type_id,unit,start_on,end_on,session,minutes,reason FROM workforce_leave_requests WHERE tenant_id=? AND hospital_id=? AND id=?`, p.TenantID, hospital, id).Scan(&existing).Error; err != nil {
			return err
		}
		if existing.StaffID != uuid.Nil {
			if existing.StaffID != staffID || existing.LeaveTypeID != in.LeaveTypeID || existing.Unit != in.Unit || !existing.StartOn.Equal(start) || !existing.EndOn.Equal(end) ||
				!equalPtr(existing.Session, in.Session) || !equalPtr(existing.Minutes, in.Minutes) || existing.Reason != in.Reason {
				return errLeaveRequestConflict
			}
			var err error
			result, err = loadLeaveRequestDetail(tx, p.TenantID, hospital, staffID, id)
			return err
		}
		r := leaveRequestRow{TenantID: p.TenantID, HospitalID: hospital, ID: id, StaffID: staffID, LeaveTypeID: in.LeaveTypeID, Unit: in.Unit, StartOn: start, EndOn: end, Session: in.Session, Minutes: in.Minutes, Reason: in.Reason, Status: "pending", RequestedBySubject: p.Subject, Version: 1}
		var staffExists bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_staff WHERE tenant_id=? AND hospital_id=? AND id=?)`, p.TenantID, hospital, staffID).Scan(&staffExists).Error; err != nil {
			return err
		}
		if !staffExists {
			return gorm.ErrRecordNotFound
		}
		var year struct {
			LeaveYear int
			EndOn     time.Time
			Closed    bool
		}
		if err := tx.Raw(`SELECT leave_year,end_on,closed FROM workforce_leave_years WHERE tenant_id=? AND hospital_id=? AND ?::date BETWEEN start_on AND end_on`, p.TenantID, hospital, start).Scan(&year).Error; err != nil {
			return err
		}
		if year.LeaveYear == 0 || year.Closed {
			return errLeaveYearClosed
		}
		if end.After(year.EndOn) {
			return errLeaveCrossYear
		}
		r.LeaveYear = year.LeaveYear
		// The type revision and assignment in force on the first day govern the whole request.
		var pins struct {
			TypeRevisionID       uuid.UUID
			AssignmentRevisionID uuid.UUID
		}
		if err := tx.Raw(`SELECT (SELECT id FROM workforce_leave_config_revisions WHERE tenant_id=? AND hospital_id=? AND config_id=? AND kind='leave_type' AND effective_on<=?::date ORDER BY version DESC LIMIT 1) AS type_revision_id,
 (SELECT id FROM workforce_staff_leave_assignment_revisions WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND effective_on<=?::date ORDER BY version DESC LIMIT 1) AS assignment_revision_id`,
			p.TenantID, hospital, in.LeaveTypeID, start, p.TenantID, hospital, staffID, start).Scan(&pins).Error; err != nil {
			return err
		}
		if pins.TypeRevisionID == uuid.Nil || pins.AssignmentRevisionID == uuid.Nil {
			return errLeaveNotEntitled
		}
		r.LeaveTypeRevisionID, r.AssignmentRevisionID = pins.TypeRevisionID, pins.AssignmentRevisionID
		terms, err := loadLeaveTerms(tx, p.TenantID, hospital, r.LeaveTypeRevisionID, r.AssignmentRevisionID)
		if err != nil {
			return err
		}
		if !terms.TypeActive || !terms.AssignmentActive || !terms.InStructure {
			return errLeaveNotEntitled
		}
		if in.Unit == "half_day" && !terms.HalfDayAllowed || in.Unit == "short_leave" && *in.Minutes > terms.ShortLeaveMinutes {
			return errLeaveUnit
		}
		days, total, err := planLeaveDays(tx, r)
		if err != nil {
			return err
		}
		r.HalfDays = total
		// Mirrors the exclusion constraint so the caller gets a specific answer; the constraint stays the backstop.
		var overlap bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_leave_requests WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND status IN ('pending','approved')
 AND slots && int4range((?::date - DATE '2000-01-01') * 2 + CASE WHEN ?::text = 'second_half' THEN 1 ELSE 0 END,(?::date - DATE '2000-01-01') * 2 + CASE WHEN ?::text = 'first_half' THEN 1 ELSE 2 END))`,
			p.TenantID, hospital, staffID, start, in.Session, end, in.Session).Scan(&overlap).Error; err != nil {
			return err
		}
		if overlap {
			return errLeaveOverlap
		}
		// Clubbing is refused when either side of an adjacent pair of different types is non-clubbable.
		var clubbed bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_leave_requests o JOIN workforce_leave_type_rules k ON (k.tenant_id,k.hospital_id,k.revision_id)=(o.tenant_id,o.hospital_id,o.leave_type_revision_id)
 WHERE o.tenant_id=? AND o.hospital_id=? AND o.staff_id=? AND o.status IN ('pending','approved') AND o.leave_type_id<>?
 AND (o.end_on = ?::date - 1 OR o.start_on = ?::date + 1) AND (NOT ? OR NOT k.clubbable))`,
			p.TenantID, hospital, staffID, in.LeaveTypeID, start, end, terms.Clubbable).Scan(&clubbed).Error; err != nil {
			return err
		}
		if clubbed {
			return errLeaveClubbing
		}
		if err := checkLeaveStanding(tx, c, p, r, terms, true); err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_leave_requests(tenant_id,hospital_id,id,staff_id,leave_type_id,leave_type_revision_id,assignment_revision_id,leave_year,unit,start_on,end_on,session,minutes,half_days,reason,status,requested_by_subject,version)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,'pending',?,1)`, p.TenantID, hospital, id, staffID, in.LeaveTypeID, r.LeaveTypeRevisionID, r.AssignmentRevisionID, r.LeaveYear, in.Unit, start, end, in.Session, in.Minutes, r.HalfDays, in.Reason, p.Subject).Error; err != nil {
			return err
		}
		if err := tx.Table("workforce_leave_request_days").Create(&days).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_leave_request_events(tenant_id,hospital_id,id,request_id,version,status,actor_subject) VALUES(?,?,?,?,1,'pending',?)`, p.TenantID, hospital, uuid.New(), id, p.Subject).Error; err != nil {
			return err
		}
		// The reason may describe health or family circumstances, so audit rows and events carry identifiers and counts only.
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.leave_request.filed',?,jsonb_build_object('staff_id',?::uuid,'leave_year',?::int,'half_days',?::int,'version',1))`, p.TenantID, hospital, p.Subject, id, staffID, r.LeaveYear, r.HalfDays).Error; err != nil {
			return err
		}
		if err := h.publishLeaveRequest(tx, p, r); err != nil {
			return err
		}
		result, err = loadLeaveRequestDetail(tx, p.TenantID, hospital, staffID, id)
		return err
	})
	if err != nil {
		respondLeaveRequestError(c, err)
		return
	}
	h.respondLeaveDetail(c, result)
}

func equalPtr[T comparable](a, b *T) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func (h *handlers) respondLeaveDetail(c *gin.Context, d leaveRequestDetail) {
	if !leaveRequestReader(c) {
		d.Request.Reason = ""
	}
	respond.OK(c, d)
}

type leaveTransition struct {
	from, to string
	// separate requires the actor to differ from the filer, as approval and revocation change the balance.
	separate bool
	decides  bool
	reversal bool
}

func (h *handlers) decideLeaveRequest(c *gin.Context) {
	var in leaveDecisionInput
	if !decode(c, &in) {
		return
	}
	t := leaveTransition{from: "pending", to: "approved", separate: true, decides: true}
	if in.Decision == "reject" {
		t.to = "rejected"
	}
	h.transitionLeaveRequest(c, t, *in.ExpectedVersion, in.Note)
}

func (h *handlers) cancelLeaveRequest(c *gin.Context) {
	var in leaveCancelInput
	if !decode(c, &in) {
		return
	}
	h.transitionLeaveRequest(c, leaveTransition{from: "pending", to: "cancelled"}, *in.ExpectedVersion, in.Note)
}

func (h *handlers) revokeLeaveRequest(c *gin.Context) {
	var in leaveCancelInput
	if !decode(c, &in) {
		return
	}
	h.transitionLeaveRequest(c, leaveTransition{from: "approved", to: "cancelled", separate: true, reversal: true}, *in.ExpectedVersion, in.Note)
}

// transitionLeaveRequest moves a request one step; replaying the step that produced the current version returns it unchanged.
func (h *handlers) transitionLeaveRequest(c *gin.Context, t leaveTransition, expected int64, note string) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staffID, id, ok := leaveRequestIDs(c)
	if !ok {
		return
	}
	note = strings.TrimSpace(note)
	if !validLeaveText(note) {
		respondLeaveRequestError(c, errLeaveInvalid)
		return
	}
	if (t.to == "rejected" || t.reversal) && note == "" {
		respondLeaveRequestError(c, errLeaveNoteRequired)
		return
	}
	if t.decides {
		grant, err := authz.RecheckHospitalScope(c, PermLeaveApprove, hospital.String())
		permissions, _ := authz.PermissionsFrom(c)
		if err != nil && !permissions.Has(PermLeaveApprove) {
			respondLeaveRequestError(c, authz.ErrScopedAuthorizationUnavailable)
			return
		}
		if grant.AssignmentID == "" && !permissions.Has(PermLeaveApprove) {
			respondLeaveRequestError(c, gorm.ErrRecordNotFound)
			return
		}
	}
	var result leaveRequestDetail
	run := func(ctx context.Context) error {
		return h.transaction(ctx, p, hospital, func(tx *gorm.DB) error {
			scopeEvidence, err := prepareLeaveReview(tx, c, p, hospital, staffID, id, t.decides)
			if err != nil {
				return err
			}
			var r leaveRequestRow
			if err := tx.Raw(`SELECT tenant_id,hospital_id,id,staff_id,leave_type_id,leave_type_revision_id,assignment_revision_id,leave_year,unit,start_on,end_on,session,minutes,half_days,status,requested_by_subject,version
 FROM workforce_leave_requests WHERE tenant_id=? AND hospital_id=? AND id=? AND staff_id=? FOR UPDATE`, p.TenantID, hospital, id, staffID).Scan(&r).Error; err != nil {
				return err
			}
			if r.ID == uuid.Nil {
				return gorm.ErrRecordNotFound
			}
			if t.separate {
				if err := requireIndependentLeaveActor(tx, p, hospital, staffID, r.RequestedBySubject); err != nil {
					return err
				}
			}
			if r.Version == expected+1 && r.Status == t.to {
				var replay bool
				if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_leave_request_events WHERE tenant_id=? AND hospital_id=? AND request_id=? AND version=? AND actor_subject=? AND note=?)`, p.TenantID, hospital, id, r.Version, p.Subject, note).Scan(&replay).Error; err != nil {
					return err
				}
				if replay {
					var err error
					result, err = loadLeaveRequestDetail(tx, p.TenantID, hospital, staffID, id)
					return err
				}
			}
			if r.Version != expected {
				return errLeaveRequestVersion
			}
			if r.Status != t.from {
				return errLeaveRequestState
			}
			var closed bool
			if err := tx.Raw(`SELECT closed FROM workforce_leave_years WHERE tenant_id=? AND hospital_id=? AND leave_year=?`, p.TenantID, hospital, r.LeaveYear).Scan(&closed).Error; err != nil {
				return err
			}
			if closed {
				return errLeaveYearClosed
			}
			terms, err := loadLeaveTerms(tx, p.TenantID, hospital, r.LeaveTypeRevisionID, r.AssignmentRevisionID)
			if err != nil {
				return err
			}
			if t.to == "approved" {
				if err := checkLeaveStanding(tx, c, p, r, terms, false); err != nil {
					return err
				}
			}
			set := "status=?,version=version+1,updated_at=clock_timestamp()"
			args := []any{t.to}
			if t.decides {
				set += ",decided_by_subject=?,decided_at=clock_timestamp()"
				args = append(args, p.Subject)
			}
			update := tx.Exec(`UPDATE workforce_leave_requests SET `+set+` WHERE tenant_id=? AND hospital_id=? AND id=? AND version=? AND status=?`, append(args, p.TenantID, hospital, id, expected, t.from)...)
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected != 1 {
				return errLeaveRequestVersion
			}
			r.Status, r.Version = t.to, expected+1
			if err := tx.Exec(`INSERT INTO workforce_leave_request_events(tenant_id,hospital_id,id,request_id,version,status,note,actor_subject) VALUES(?,?,?,?,?,?,?,?)`, p.TenantID, hospital, uuid.New(), id, r.Version, r.Status, note, p.Subject).Error; err != nil {
				return err
			}
			kind, amount := "", 0
			switch {
			case t.to == "approved" && terms.BalanceTracked && r.HalfDays > 0:
				kind, amount = "consumption", -r.HalfDays
			case t.reversal:
				var consumed bool
				if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_leave_ledger WHERE tenant_id=? AND hospital_id=? AND request_id=? AND kind='consumption')`, p.TenantID, hospital, id).Scan(&consumed).Error; err != nil {
					return err
				}
				if consumed {
					kind, amount = "reversal", r.HalfDays
				}
			}
			if kind != "" {
				entry := leaveLedgerRow{TenantID: p.TenantID, HospitalID: hospital, ID: uuid.New(), StaffID: staffID, LeaveTypeID: r.LeaveTypeID, LeaveTypeRevisionID: r.LeaveTypeRevisionID, LeaveYear: r.LeaveYear, Kind: kind, HalfDays: amount, EffectiveOn: r.StartOn, IdempotencyKey: kind + ":" + id.String(), RequestID: &id, ActorSubject: p.Subject}
				if err := tx.Table("workforce_leave_ledger").Create(&entry).Error; err != nil {
					return err
				}
				if err := h.publishLedger(tx, p, hospital, r.LeaveYear, "request", id, 1); err != nil {
					return err
				}
			}
			if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,jsonb_build_object('previous_status',?::text,'version',?::bigint,'ledger_half_days',?::int)||?::jsonb)`,
				p.TenantID, hospital, p.Subject, "workforce.leave_request."+t.to, id, t.from, r.Version, amount, string(scopeEvidence)).Error; err != nil {
				return err
			}
			if err := h.publishLeaveRequest(tx, p, r); err != nil {
				return err
			}
			result, err = loadLeaveRequestDetail(tx, p.TenantID, hospital, staffID, id)
			return err
		})
	}
	var err error
	if t.decides {
		err = authz.WithHospitalScopeCommand(c, PermLeaveApprove, hospital.String(), run)
	} else {
		err = run(c.Request.Context())
	}
	if err != nil {
		respondLeaveRequestError(c, err)
		return
	}
	h.respondLeaveDetail(c, result)
}

func (h *handlers) getLeaveRequest(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staffID, id, ok := leaveRequestIDs(c)
	if !ok {
		return
	}
	var result leaveRequestDetail
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var err error
		result, err = loadLeaveRequestDetail(tx, p.TenantID, hospital, staffID, id)
		return err
	})
	if err != nil {
		respondLeaveRequestError(c, err)
		return
	}
	h.respondLeaveDetail(c, result)
}

// listLeaveRequests filters by status, staff member and leave year, newest filing first.
func (h *handlers) listLeaveRequests(c *gin.Context, params pagination.Params) ([]leaveRequest, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	where := "tenant_id=? AND hospital_id=?"
	args := []any{p.TenantID, hospital}
	valid := params.Limit <= 100
	if s := c.Query("status"); s != "" {
		valid = valid && slices.Contains(leaveRequestStatuses, s)
		where += " AND status=?"
		args = append(args, s)
	}
	if s := c.Query("staff_id"); s != "" {
		staff, err := uuid.Parse(s)
		valid = valid && err == nil && staff != uuid.Nil
		where += " AND staff_id=?"
		args = append(args, staff)
	}
	if s := c.Query("leave_year"); s != "" {
		year, err := strconv.Atoi(s)
		valid = valid && err == nil && year >= 2000 && year <= 2199
		where += " AND leave_year=?"
		args = append(args, year)
	}
	if !valid {
		respond.Error(c, 400, "invalid_request", "Use a known status, a staff_id, a leave_year between 2000 and 2199 and a page limit no greater than 100.")
		return nil, nil
	}
	rows := []leaveRequest{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Table(leaveRequestView).Select(leaveRequestColumns).Where(where, args...).Find(&rows).Error
	})
	if err != nil {
		respondLeaveRequestError(c, err)
		return nil, nil
	}
	if !leaveRequestReader(c) {
		for i := range rows {
			rows[i].Reason = ""
		}
	}
	return rows, nil
}

func (h *handlers) leaveRequestHistory(c *gin.Context, params pagination.Params) ([]leaveRequestEvent, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	staffID, id, ok := leaveRequestIDs(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, 400, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	rows := []leaveRequestEvent{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var exists bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_leave_requests WHERE tenant_id=? AND hospital_id=? AND id=? AND staff_id=?)`, p.TenantID, hospital, id, staffID).Scan(&exists).Error; err != nil {
			return err
		}
		if !exists {
			return gorm.ErrRecordNotFound
		}
		return tenantdb.ApplyKeyset(tx, params).Table("workforce_leave_request_events").Select("id,version,status,note,actor_subject,created_at").Where("tenant_id=? AND hospital_id=? AND request_id=?", p.TenantID, hospital, id).Find(&rows).Error
	})
	if err != nil {
		respondLeaveRequestError(c, err)
		return nil, nil
	}
	// Decision notes may restate the reason, so they follow the same redaction.
	if !leaveRequestReader(c) {
		for i := range rows {
			rows[i].Note = ""
		}
	}
	return rows, nil
}

func respondLeaveRequestError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, authz.ErrScopedCredentialRevoked):
		respond.Error(c, 401, "unauthenticated", "Invalid credentials.")
	case errors.Is(err, authz.ErrScopedCommandDenied):
		respond.Forbidden(c, "Leave decision is not permitted.")
	case errors.Is(err, authz.ErrScopedAuthorizationUnavailable):
		respond.Error(c, 503, "scoped_authorization_unavailable", "Scoped authorization is unavailable.")
	case errors.Is(err, errLeaveInvalid):
		respond.Error(c, 400, "invalid_request", "Review the request: dates must be valid and within a year of the start, half days and short leave cover one date with a session or 15-minute multiple of minutes, and text is at most 500 characters.")
	case errors.Is(err, errLeaveNoteRequired):
		respond.Error(c, 400, "invalid_request", "Rejecting or revoking leave needs a note.")
	case errors.Is(err, errLeaveNotEntitled):
		respond.Error(c, 400, "invalid_reference", "The leave type must be active and in the staff member's active leave structure on the start date.")
	case errors.Is(err, errLeaveUnit):
		respond.Error(c, 400, "invalid_request", "This leave type does not allow that unit or that many minutes.")
	case errors.Is(err, errLeaveCrossYear):
		respond.Error(c, 400, "invalid_request", "A request must fall inside one leave year. File each year separately.")
	case errors.Is(err, errLeaveNoWorkingDay):
		respond.Error(c, 400, "invalid_request", "Every date in the request is a holiday, so there is nothing to take leave from.")
	case errors.Is(err, errLeaveSelfDecision):
		respond.Forbidden(c, "Someone other than the filer and the linked staff member must decide or revoke this request.")
	case errors.Is(err, errLeaveProbationAccess):
		respond.Forbidden(c, "This leave type is not open during probation, and confirming probation needs enrollment access.")
	case errors.Is(err, errLeaveRequestConflict):
		respond.Conflict(c, "This request id was already used with different values.")
	case errors.Is(err, errLeaveRequestVersion):
		respond.Conflict(c, "The leave request changed since you opened it. Reload before deciding.")
	case errors.Is(err, errLeaveRequestState):
		respond.Conflict(c, "The leave request is no longer in a state that allows this action.")
	case errors.Is(err, errLeaveCalendar):
		respond.Conflict(c, "Publish an active holiday calendar for every year the request touches before filing.")
	case errors.Is(err, errLeaveOverlap), errors.As(err, &pg) && pg.Code == "23P01":
		respond.Conflict(c, "The staff member already has pending or approved leave on these dates.")
	case errors.Is(err, errLeaveClubbing):
		respond.Conflict(c, "A non-clubbable leave type cannot sit next to leave of another type.")
	case errors.Is(err, errLeaveProbation):
		respond.Conflict(c, "This leave type is not open during probation, and the staff member's probation end date is missing or not before the start date.")
	case errors.Is(err, errLeaveBalance):
		respond.Conflict(c, "The balance, less other pending requests, does not cover this request.")
	case errors.Is(err, errLeaveStaffInactive):
		respond.Conflict(c, "The staff member is not active.")
	default:
		respondLeaveLedgerError(c, err)
	}
}

package workforce

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func (r leaveMonthlyReportPage) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.StaffID }
func (r leaveStatusReportRow) PageKey() (time.Time, uuid.UUID)   { return r.CreatedAt, r.ID }

// Staff rows page by the staff record, whose creation time stays out of the report.
type leaveMonthlyReportPage struct {
	leaveMonthlyReportRow
	CreatedAt time.Time `json:"-"`
}

var errLeaveReportDepartment = errors.New("department filter needs enrollment access")

type leaveReportFilter struct {
	from, to time.Time
	where    string
	args     []any
}

// Enrollment columns resolve to NULL unless the caller may read the profile, so a missing check cannot leak them.
func leaveReportProfile(enrolled bool) string {
	if !enrolled {
		return "NULL::text AS department,NULL::text AS joined_on,NULL::uuid AS department_id"
	}
	return `COALESCE(e.department,'') AS department,e.start_date::text AS joined_on,
 (SELECT m.master_id FROM workforce_hr_master_revisions m WHERE (m.tenant_id,m.hospital_id,m.id)=(e.tenant_id,e.hospital_id,e.department_revision_id)) AS department_id`
}

const leaveReportProfileJoin = ` LEFT JOIN workforce_enrollment_profile e ON (e.tenant_id,e.hospital_id,e.staff_id)=(s.tenant_id,s.hospital_id,s.id)`

func likeLeaveReport(q string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q) + "%"
}

// parseLeaveReportFilter reads the inclusive period and the filters both reports share; extra adds report-specific ones.
func parseLeaveReportFilter(c *gin.Context, params pagination.Params, extra func(*leaveReportFilter) bool) (leaveReportFilter, bool) {
	f := leaveReportFilter{where: "TRUE"}
	from, errFrom := time.Parse(time.DateOnly, c.Query("from"))
	to, errTo := time.Parse(time.DateOnly, c.Query("to"))
	valid := params.Limit <= 100 && errFrom == nil && errTo == nil && !to.Before(from) && to.Sub(from) <= 365*24*time.Hour &&
		from.Year() >= 2000 && to.Year() <= 2199
	f.from, f.to = from, to
	if q, ok := c.GetQuery("q"); ok {
		valid = valid && utf8.RuneCountInString(q) >= 1 && utf8.RuneCountInString(q) <= 100 && validLeaveText(q)
		f.where += ` AND (display_name ILIKE ? ESCAPE '\' OR employee_code ILIKE ? ESCAPE '\')`
		f.args = append(f.args, likeLeaveReport(q), likeLeaveReport(q))
	}
	for _, key := range []string{"staff_id", "department_id"} {
		if s, ok := c.GetQuery(key); ok {
			id, err := uuid.Parse(s)
			valid = valid && err == nil && id != uuid.Nil
			f.where += " AND " + key + "=?"
			f.args = append(f.args, id)
		}
	}
	if extra != nil {
		valid = extra(&f) && valid
	}
	if !valid {
		respond.Error(c, 400, "invalid_request", "Use from and to dates (YYYY-MM-DD) covering at most 366 days between 2000 and 2199, a search of 1 to 100 characters, valid ids, a known status and a page limit no greater than 100.")
	}
	return f, valid
}

// leaveReportEnrolled reports whether profile columns may be shown; a missing manager grant hides them rather than failing the report.
func leaveReportEnrolled(tx *gorm.DB, c *gin.Context, p authn.Principal, hospital uuid.UUID) (bool, error) {
	if set, ok := authz.PermissionsFrom(c); !ok || !set.Has(PermEnrollmentManage) {
		return false, nil
	}
	if err := enrollmentScope(tx, p, hospital); errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

// leaveReportScope checks hospital access, decides profile visibility and refuses a department filter the caller cannot see.
func leaveReportScope(tx *gorm.DB, c *gin.Context, p authn.Principal, hospital uuid.UUID) (bool, error) {
	if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
		return false, err
	}
	enrolled, err := leaveReportEnrolled(tx, c, p, hospital)
	if err != nil {
		return false, err
	}
	if _, filtered := c.GetQuery("department_id"); filtered && !enrolled {
		return false, errLeaveReportDepartment
	}
	return enrolled, nil
}

// auditLeaveReport records whose enrollment profile a report page disclosed.
func auditLeaveReport(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, report string, staffIDs []uuid.UUID) error {
	if len(staffIDs) == 0 {
		return nil
	}
	changes, err := json.Marshal(map[string]any{"report": report, "staff_ids": staffIDs})
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO workforce_audit (tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES (?,?,?,'workforce.enrollment.report',?,?::jsonb)`, p.TenantID, hospital, p.Subject, hospital, string(changes)).Error
}

func respondLeaveReportError(c *gin.Context, err error) {
	if errors.Is(err, errLeaveReportDepartment) {
		respond.Forbidden(c, "Filtering by department needs enrollment access for this hospital.")
		return
	}
	respondLeaveRequestError(c, err)
}

type leaveMonthlyDay struct {
	StaffID     uuid.UUID
	Day         string
	LeaveTypeID uuid.UUID
	Code        string
	Name        string
	HalfDays    int
	Minutes     int
}

// leaveMonthlyReport is report 949: approved leave per staff member and calendar month, split exactly at the period edges.
func (h *handlers) leaveMonthlyReport(c *gin.Context, params pagination.Params) ([]leaveMonthlyReportPage, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	f, ok := parseLeaveReportFilter(c, params, nil)
	if !ok {
		return nil, nil
	}
	rows := []leaveMonthlyReportPage{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		enrolled, err := leaveReportScope(tx, c, p, hospital)
		if err != nil {
			return err
		}
		view := `(SELECT s.id AS staff_id,s.employee_code,s.display_name,s.created_at,s.id,` + leaveReportProfile(enrolled) + `
 FROM workforce_staff s` + leaveReportProfileJoin + `
 WHERE s.tenant_id=? AND s.hospital_id=? AND EXISTS(SELECT 1 FROM workforce_leave_request_days d JOIN workforce_leave_requests r ON (r.tenant_id,r.hospital_id,r.id)=(d.tenant_id,d.hospital_id,d.request_id)
  WHERE d.tenant_id=s.tenant_id AND d.hospital_id=s.hospital_id AND r.staff_id=s.id AND r.status='approved' AND NOT d.holiday AND d.day BETWEEN ? AND ?)) AS v`
		var staff []struct {
			StaffID                   uuid.UUID
			EmployeeCode, DisplayName string
			Department, JoinedOn      *string
			CreatedAt                 time.Time
		}
		if err := tenantdb.ApplyKeyset(tx, params).Table(view, p.TenantID, hospital, f.from, f.to).Select("staff_id,employee_code,display_name,department,joined_on,created_at").
			Where(f.where, f.args...).Find(&staff).Error; err != nil {
			return err
		}
		for _, s := range staff {
			rows = append(rows, leaveMonthlyReportPage{leaveMonthlyReportRow{StaffID: s.StaffID, EmployeeCode: s.EmployeeCode, DisplayName: s.DisplayName, Department: s.Department, JoinedOn: s.JoinedOn}, s.CreatedAt})
		}
		page := rows[:min(len(rows), params.Limit)]
		ids := make([]uuid.UUID, len(page))
		for i, r := range page {
			ids[i] = r.StaffID
		}
		if len(ids) == 0 {
			return nil
		}
		var days []leaveMonthlyDay
		if err := tx.Raw(`SELECT r.staff_id,d.day::text AS day,r.leave_type_id,t.code,t.name,d.half_days,CASE WHEN r.unit='short_leave' THEN r.minutes ELSE 0 END AS minutes
 FROM workforce_leave_request_days d JOIN workforce_leave_requests r ON (r.tenant_id,r.hospital_id,r.id)=(d.tenant_id,d.hospital_id,d.request_id)
 JOIN workforce_leave_configs t ON (t.tenant_id,t.hospital_id,t.id)=(r.tenant_id,r.hospital_id,r.leave_type_id)
 WHERE d.tenant_id=? AND d.hospital_id=? AND r.staff_id IN ? AND r.status='approved' AND NOT d.holiday AND d.day BETWEEN ? AND ?
 ORDER BY r.staff_id,d.day,t.code,r.leave_type_id`, p.TenantID, hospital, ids, f.from, f.to).Scan(&days).Error; err != nil {
			return err
		}
		months := groupLeaveMonths(days)
		for i := range page {
			page[i].Months = append([]leaveReportMonth{}, months[page[i].StaffID]...)
		}
		if !enrolled {
			return nil
		}
		return auditLeaveReport(tx, p, hospital, "monthly", ids)
	})
	if err != nil {
		respondLeaveReportError(c, err)
		return nil, nil
	}
	return rows, nil
}

// groupLeaveMonths folds ordered day rows into months with distinct leave dates and per-type totals ordered by code.
func groupLeaveMonths(days []leaveMonthlyDay) map[uuid.UUID][]leaveReportMonth {
	out := map[uuid.UUID][]leaveReportMonth{}
	for _, d := range days {
		months := out[d.StaffID]
		if len(months) == 0 || months[len(months)-1].Month != d.Day[:7] {
			months = append(months, leaveReportMonth{Month: d.Day[:7], LeaveDates: []string{}, Types: []leaveReportType{}})
		}
		m := &months[len(months)-1]
		if !slices.Contains(m.LeaveDates, d.Day) {
			m.LeaveDates = append(m.LeaveDates, d.Day)
		}
		i := slices.IndexFunc(m.Types, func(t leaveReportType) bool { return t.LeaveTypeID == d.LeaveTypeID })
		if i < 0 {
			m.Types = append(m.Types, leaveReportType{LeaveTypeID: d.LeaveTypeID, Code: d.Code, Name: d.Name})
			i = len(m.Types) - 1
		}
		m.Types[i].HalfDays += d.HalfDays
		m.Types[i].Minutes += d.Minutes
		out[d.StaffID] = months
	}
	for _, months := range out {
		for _, m := range months {
			slices.SortFunc(m.Types, func(a, b leaveReportType) int { return strings.Compare(a.Code, b.Code) })
		}
	}
	return out
}

// leaveStatusReport is report 954: requests in any status whose dates overlap the period, newest filing first.
func (h *handlers) leaveStatusReport(c *gin.Context, params pagination.Params) ([]leaveStatusReportRow, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	f, ok := parseLeaveReportFilter(c, params, func(f *leaveReportFilter) bool {
		valid := true
		if s, ok := c.GetQuery("leave_type_id"); ok {
			id, err := uuid.Parse(s)
			valid = err == nil && id != uuid.Nil
			f.where += " AND leave_type_id=?"
			f.args = append(f.args, id)
		}
		if s, ok := c.GetQuery("status"); ok {
			valid = valid && slices.Contains(leaveRequestStatuses, s)
			f.where += " AND status=?"
			f.args = append(f.args, s)
		}
		return valid
	})
	if !ok {
		return nil, nil
	}
	rows := []leaveStatusReportRow{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		enrolled, err := leaveReportScope(tx, c, p, hospital)
		if err != nil {
			return err
		}
		// Requests span at most 366 days, so the start_on lower bound keeps the overlap scan on the index.
		view := `(SELECT r.id,r.staff_id,s.employee_code,s.display_name,` + leaveReportProfile(enrolled) + `,r.leave_type_id,t.code,t.name,r.unit,
 r.start_on::text AS start_on,r.end_on::text AS end_on,r.session,r.minutes,r.half_days,r.status,r.requested_by_subject,
 CASE WHEN r.status IN ('approved','rejected') THEN r.decided_by_subject END AS decided_by_subject,r.created_at
 FROM workforce_leave_requests r JOIN workforce_staff s ON (s.tenant_id,s.hospital_id,s.id)=(r.tenant_id,r.hospital_id,r.staff_id)` + leaveReportProfileJoin + `
 JOIN workforce_leave_config_revisions t ON (t.tenant_id,t.hospital_id,t.id)=(r.tenant_id,r.hospital_id,r.leave_type_revision_id)
 WHERE r.tenant_id=? AND r.hospital_id=? AND r.start_on <= ? AND r.end_on >= ? AND r.start_on >= ?::date - 365) AS v`
		if err := tenantdb.ApplyKeyset(tx, params).Table(view, p.TenantID, hospital, f.to, f.from, f.from).
			Select("id,staff_id,employee_code,display_name,department,joined_on,leave_type_id,code,name,unit,start_on,end_on,session,minutes,half_days,status,requested_by_subject,decided_by_subject,created_at").
			Where(f.where, f.args...).Find(&rows).Error; err != nil {
			return err
		}
		if !enrolled {
			return nil
		}
		var ids []uuid.UUID
		for _, r := range rows[:min(len(rows), params.Limit)] {
			if !slices.Contains(ids, r.StaffID) {
				ids = append(ids, r.StaffID)
			}
		}
		return auditLeaveReport(tx, p, hospital, "status", ids)
	})
	if err != nil {
		respondLeaveReportError(c, err)
		return nil, nil
	}
	return rows, nil
}

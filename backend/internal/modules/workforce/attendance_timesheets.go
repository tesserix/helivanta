package workforce

import (
	"encoding/json"
	"errors"
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

// maxPeriodDays bounds one timesheet period and one punch report.
const maxPeriodDays = 31

// timesheetBatch bounds the people derived per pass; timesheetDayBatch bounds the day rows written per statement.
const timesheetBatch = 200
const timesheetDayBatch = 1000

// unsettled statuses would freeze a guess, so any of them blocks finalisation.
var unsettled = []string{"upcoming", "in_progress", "unpublished", "no_policy"}

var (
	errPeriodInvalid   = &rosterProblem{400, "invalid_request", "Use from and to dates between 2000 and 2199, from not after to, at most 31 days, and a note of up to 500 characters."}
	errPeriodNote      = &rosterProblem{400, "invalid_request", "Give a note of 1 to 500 characters."}
	errPeriodNotClosed = &rosterProblem{409, "period_not_closed", "Only days that have ended in the hospital's timezone can be finalised."}
	errPeriodReused    = &rosterProblem{409, "request_reused", "This period id was already used with different values."}
	errPeriodStale     = &rosterProblem{409, "stale_version", "The period changed since you opened it. Reload before trying again."}
	errPeriodState     = &rosterProblem{409, "request_state", "Only a finalised period can be reopened."}
	errPeriodLocked    = &rosterProblem{409, "period_locked", "The date is inside a finalised attendance period. Reopen the period first."}
	errPeriodPage      = &rosterProblem{400, "invalid_request", "Use a page limit no greater than 100."}
	errTimesheetFilter = &rosterProblem{400, "invalid_request", "Use a valid staff member, a change of added, changed, unchanged or removed, and a page limit no greater than 100."}
	errPunchReport     = &rosterProblem{400, "invalid_request", "Use from/to dates between 2000 and 2199, from not after to, at most 31 days apart, an optional valid staff member and a page limit no greater than 100."}
)

type periodOverlap struct{ id uuid.UUID }

func (periodOverlap) Error() string { return "period overlap" }

type periodPending struct{ count int }

func (periodPending) Error() string { return "pending corrections" }

type periodNotReady struct{ counts map[string]int }

func (periodNotReady) Error() string { return "period not ready" }

type periodRow struct {
	ID                   uuid.UUID
	PeriodFrom, PeriodTo string
	Status               string
	Version              int64
	TimesheetID          uuid.UUID
	TimesheetNumber      int
	CreatedBySubject     string
	CreatedAt, UpdatedAt time.Time
}

type timesheetRow struct {
	ID, PeriodID                                                      uuid.UUID
	Number, People, Days, Shifts                                      int
	PayableMinutes, ApprovedOvertimeMinutes, UnpaidHalfDays           int
	Statuses, Exceptions, PolicyRevisionIDs, Note, FinalisedBySubject string
	CreatedAt                                                         time.Time
}

const periodColumns = "id,period_from::text AS period_from,period_to::text AS period_to,status,version,timesheet_id,timesheet_number,created_by_subject,created_at,updated_at"
const timesheetColumns = "id,period_id,number,people,days,shifts,payable_minutes,approved_overtime_minutes,unpaid_half_days,statuses::text AS statuses,exceptions::text AS exceptions,to_jsonb(policy_revision_ids)::text AS policy_revision_ids,note,finalised_by_subject,created_at"

type periodPage struct{ attendancePeriod }

func (p periodPage) PageKey() (time.Time, uuid.UUID) { return p.CreatedAt, p.ID }

type timesheetPage struct{ attendanceTimesheet }

func (t timesheetPage) PageKey() (time.Time, uuid.UUID) { return t.CreatedAt, t.ID }

func (e attendancePeriodEvent) PageKey() (time.Time, uuid.UUID) { return e.CreatedAt, e.ID }

type timesheetPersonPage struct {
	attendanceTimesheetPerson
	sortAt time.Time
}

func (p timesheetPersonPage) PageKey() (time.Time, uuid.UUID) { return p.sortAt, p.StaffID }

type timesheetDayPage struct {
	attendanceTimesheetDay
	sortAt time.Time
}

func (d timesheetDayPage) PageKey() (time.Time, uuid.UUID) { return d.sortAt, d.StaffID }

// punchReportPage is a defined type rather than an embedding, because GORM does not scan unexported embedded structs.
type punchReportPage attendancePunchReportRow

func (r punchReportPage) PageKey() (time.Time, uuid.UUID) { return r.RecordedAt, r.ID }

// periodNoteReader may see period notes, because a reopening reason can name a person's circumstances.
func periodNoteReader(c *gin.Context) bool {
	set, ok := authz.PermissionsFrom(c)
	return ok && (set.Has(PermAttendanceFinalise) || set.Has(PermAttendanceRecord) || set.Has(PermAttendanceCorrect) || set.Has(PermAttendanceApprove))
}

func periodID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("period"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "period")
		return uuid.Nil, false
	}
	return id, true
}

// timesheets converts rows in their given order, adding each version's day change counts.
func timesheets(c *gin.Context, tx *gorm.DB, tenant string, hospital uuid.UUID, rows []timesheetRow) ([]attendanceTimesheet, error) {
	out := make([]attendanceTimesheet, 0, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	var changes []struct {
		TimesheetID uuid.UUID
		Change      string
		Count       int
	}
	if err := tx.Raw(`SELECT timesheet_id,change,count(*)::int AS count FROM workforce_attendance_timesheet_days WHERE tenant_id=? AND hospital_id=? AND timesheet_id IN ? AND change<>'unchanged' GROUP BY timesheet_id,change`,
		tenant, hospital, ids).Scan(&changes).Error; err != nil {
		return nil, err
	}
	byTimesheet := map[uuid.UUID]map[string]int{}
	for _, ch := range changes {
		if byTimesheet[ch.TimesheetID] == nil {
			byTimesheet[ch.TimesheetID] = map[string]int{}
		}
		byTimesheet[ch.TimesheetID][ch.Change] = ch.Count
	}
	notes := periodNoteReader(c)
	for _, r := range rows {
		changed := byTimesheet[r.ID]
		t := attendanceTimesheet{ID: r.ID, PeriodID: r.PeriodID, Number: r.Number, People: r.People, Days: r.Days, Shifts: r.Shifts, PayableMinutes: r.PayableMinutes,
			ApprovedOvertimeMinutes: r.ApprovedOvertimeMinutes, UnpaidHalfDays: r.UnpaidHalfDays, AddedDays: changed["added"], ChangedDays: changed["changed"],
			RemovedDays: changed["removed"], Note: r.Note, FinalisedBySubject: r.FinalisedBySubject, CreatedAt: r.CreatedAt}
		if err := json.Unmarshal([]byte(r.Statuses), &t.Statuses); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(r.Exceptions), &t.Exceptions); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(r.PolicyRevisionIDs), &t.PolicyRevisionIDs); err != nil {
			return nil, err
		}
		if !notes {
			t.Note = ""
		}
		out = append(out, t)
	}
	return out, nil
}

func periods(c *gin.Context, tx *gorm.DB, tenant string, hospital uuid.UUID, rows []periodRow) ([]attendancePeriod, error) {
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.TimesheetID
	}
	var current []timesheetRow
	if len(ids) > 0 {
		if err := tx.Table("workforce_attendance_timesheets").Select(timesheetColumns).Where("tenant_id=? AND hospital_id=? AND id IN ?", tenant, hospital, ids).Find(&current).Error; err != nil {
			return nil, err
		}
	}
	loaded, err := timesheets(c, tx, tenant, hospital, current)
	if err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]attendanceTimesheet{}
	for _, t := range loaded {
		byID[t.ID] = t
	}
	out := make([]attendancePeriod, len(rows))
	for i, r := range rows {
		out[i] = attendancePeriod{ID: r.ID, From: r.PeriodFrom, To: r.PeriodTo, Status: r.Status, Version: r.Version, Timesheet: byID[r.TimesheetID],
			CreatedBySubject: r.CreatedBySubject, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
	}
	return out, nil
}

func loadPeriodRow(tx *gorm.DB, tenant string, hospital, id uuid.UUID, forUpdate bool) (periodRow, error) {
	var row periodRow
	q := tx.Table("workforce_attendance_periods").Select(periodColumns).Where("tenant_id=? AND hospital_id=? AND id=?", tenant, hospital, id)
	if forUpdate {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	return row, q.Take(&row).Error
}

func loadPeriod(c *gin.Context, tx *gorm.DB, tenant string, hospital, id uuid.UUID) (attendancePeriod, error) {
	row, err := loadPeriodRow(tx, tenant, hospital, id, false)
	if err != nil {
		return attendancePeriod{}, err
	}
	out, err := periods(c, tx, tenant, hospital, []periodRow{row})
	if err != nil {
		return attendancePeriod{}, err
	}
	return out[0], nil
}

// recordPeriod writes the transition event, an identifier-only audit row and the outbox event for the period's current version.
func (h *handlers) recordPeriod(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, r periodRow, note string) error {
	if err := tx.Exec(`INSERT INTO workforce_attendance_period_events(tenant_id,hospital_id,id,period_id,version,status,timesheet_id,note,actor_subject) VALUES(?,?,?,?,?,?,?,?,?)`,
		p.TenantID, hospital, uuid.New(), r.ID, r.Version, r.Status, r.TimesheetID, note, p.Subject).Error; err != nil {
		return err
	}
	if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,jsonb_build_object('version',?::bigint,'timesheet_id',?::uuid,'timesheet_number',?::int))`,
		p.TenantID, hospital, p.Subject, "workforce.attendance.period_"+r.Status, r.ID, r.Version, r.TimesheetID, r.TimesheetNumber).Error; err != nil {
		return err
	}
	data, err := json.Marshal(workforcecontract.AttendancePeriodChangedData{HospitalID: hospital.String(), PeriodID: r.ID.String(), Status: r.Status, Version: r.Version, TimesheetID: r.TimesheetID.String()})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, AttendancePeriodChanged, events.Event{Type: AttendancePeriodChanged, Version: 1, TenantID: p.TenantID, Data: data})
}

// periodRange accepts canonical dates spanning at most maxPeriodDays days.
func periodRange(from, to string) bool {
	start, okFrom := parseWorkDate(from)
	end, okTo := parseWorkDate(to)
	return okFrom && okTo && start.Format(time.DateOnly) == from && end.Format(time.DateOnly) == to && !end.Before(start) && end.Sub(start) < maxPeriodDays*24*time.Hour
}

// periodReady refuses a range whose last day has not ended or whose days still have pending corrections.
func periodReady(tx *gorm.DB, tenant string, hospital uuid.UUID, from, to string) error {
	var settings struct{ Timezone string }
	if err := tx.Raw(`SELECT timezone FROM workforce_roster_setting_revisions WHERE tenant_id=? AND hospital_id=? AND id=workforce_roster_settings_on(?,?,?::date)`,
		tenant, hospital, tenant, hospital, to).Scan(&settings).Error; err != nil {
		return err
	}
	if settings.Timezone == "" {
		return errAttendanceNoPolicy
	}
	loc, err := time.LoadLocation(settings.Timezone)
	if err != nil {
		return err
	}
	if to >= time.Now().In(loc).Format(time.DateOnly) {
		return errPeriodNotClosed
	}
	var pending int
	if err := tx.Raw(`SELECT count(*) FROM workforce_attendance_corrections c LEFT JOIN workforce_attendance_punches t ON (t.tenant_id,t.hospital_id,t.id)=(c.tenant_id,c.hospital_id,c.target_punch_id)
 WHERE c.tenant_id=? AND c.hospital_id=? AND c.status='pending' AND (c.local_date BETWEEN ?::date AND ?::date OR t.local_date BETWEEN ?::date AND ?::date)`,
		tenant, hospital, from, to, from, to).Scan(&pending).Error; err != nil {
		return err
	}
	if pending > 0 {
		return periodPending{pending}
	}
	return nil
}

type snapshotDay struct {
	StaffID                 uuid.UUID       `json:"staff_id"`
	Day                     string          `json:"day"`
	Shifts                  int             `json:"shifts"`
	PayableMinutes          int             `json:"payable_minutes"`
	ApprovedOvertimeMinutes int             `json:"approved_overtime_minutes"`
	UnpaidHalfDays          int             `json:"unpaid_half_days"`
	Detail                  json.RawMessage `json:"detail"`
}

type snapshotPerson struct {
	StaffID                 uuid.UUID         `json:"staff_id"`
	EmployeeCode            string            `json:"employee_code"`
	DisplayName             string            `json:"display_name"`
	SortAt                  time.Time         `json:"sort_at"`
	Days                    int               `json:"days"`
	Shifts                  int               `json:"shifts"`
	PayableMinutes          int               `json:"payable_minutes"`
	ApprovedOvertimeMinutes int               `json:"approved_overtime_minutes"`
	UnpaidHalfDays          int               `json:"unpaid_half_days"`
	Statuses                []attendanceCount `json:"statuses"`
	Exceptions              []attendanceCount `json:"exceptions"`
	days                    []snapshotDay
	removed                 []string
}

// timesheetPeople selects everyone the live sheet could show in the range, plus everyone the previous version showed.
const timesheetPeople = `SELECT s.id,s.employee_code,s.display_name,s.created_at FROM workforce_staff s WHERE s.tenant_id=@t AND s.hospital_id=@h AND (
 EXISTS(SELECT 1 FROM workforce_roster_allocations a JOIN workforce_rosters r ON (r.tenant_id,r.hospital_id,r.id)=(a.tenant_id,a.hospital_id,a.roster_id)
  WHERE (a.tenant_id,a.hospital_id,a.staff_id)=(s.tenant_id,s.hospital_id,s.id) AND a.roster_date BETWEEN CAST(@from AS date) AND CAST(@to AS date) AND a.removed_version IS NULL AND r.status<>'discarded')
 OR EXISTS(SELECT 1 FROM workforce_overtime_requests o WHERE (o.tenant_id,o.hospital_id,o.staff_id)=(s.tenant_id,s.hospital_id,s.id) AND o.work_date BETWEEN CAST(@from AS date) AND CAST(@to AS date) AND o.status='approved')
 OR EXISTS(SELECT 1 FROM workforce_attendance_punches x WHERE (x.tenant_id,x.hospital_id,x.staff_id)=(s.tenant_id,s.hospital_id,s.id) AND x.local_date BETWEEN CAST(@from AS date) AND CAST(@to AS date))
 OR EXISTS(SELECT 1 FROM workforce_attendance_timesheet_days d WHERE (d.tenant_id,d.hospital_id,d.timesheet_id,d.staff_id)=(s.tenant_id,s.hospital_id,CAST(@prev AS uuid),s.id) AND d.detail IS NOT NULL)
) ORDER BY s.created_at,s.id`

// snapshot derives every person-day in the range into timesheet id, marking days against the previous timesheet when there is one.
func snapshot(tx *gorm.DB, p authn.Principal, hospital, period, id uuid.UUID, from, to string, number int, previous uuid.UUID, note string) error {
	start, _ := parseWorkDate(from)
	end, _ := parseWorkDate(to)
	var people []attendancePerson
	if err := tx.Raw(timesheetPeople, map[string]any{"t": p.TenantID, "h": hospital, "from": from, "to": to, "prev": previous}).Scan(&people).Error; err != nil {
		return err
	}
	var before []struct {
		StaffID uuid.UUID
		Day     string
	}
	if err := tx.Raw(`SELECT staff_id,day::text AS day FROM workforce_attendance_timesheet_days WHERE tenant_id=? AND hospital_id=? AND timesheet_id=? AND detail IS NOT NULL`,
		p.TenantID, hospital, previous).Scan(&before).Error; err != nil {
		return err
	}
	earlier := map[uuid.UUID][]string{}
	for _, b := range before {
		earlier[b.StaffID] = append(earlier[b.StaffID], b.Day)
	}
	now, blocked, policies := time.Now(), map[string]int{}, map[uuid.UUID]bool{}
	var total snapshotPerson
	hospitalTally := newTally()
	var kept []*snapshotPerson
	for batch := range slices.Chunk(people, timesheetBatch) {
		inputs, timezones, err := loadAttendance(tx, p.TenantID, hospital, personIDs(batch), start, end)
		if err != nil {
			return err
		}
		for _, person := range batch {
			s := &snapshotPerson{StaffID: person.ID, EmployeeCode: person.EmployeeCode, DisplayName: person.DisplayName, SortAt: person.CreatedAt}
			t := newTally()
			derived := deriveDays(*inputs[person.ID], from, to, now)
			for date, d := range derived {
				d.StaffID, d.EmployeeCode, d.DisplayName, d.Timezone = person.ID, person.EmployeeCode, person.DisplayName, timezones[date]
				for _, shift := range d.Shifts {
					if slices.Contains(unsettled, shift.Status) {
						blocked[shift.Status]++
					}
					if shift.PolicyRevisionID != nil {
						policies[*shift.PolicyRevisionID] = true
					}
				}
				detail, err := json.Marshal(d)
				if err != nil {
					return err
				}
				row := snapshotDay{StaffID: person.ID, Day: date, Shifts: len(d.Shifts), PayableMinutes: d.PayableMinutes, ApprovedOvertimeMinutes: d.ApprovedOvertimeMinutes, UnpaidHalfDays: unpaid(*d), Detail: detail}
				s.days = append(s.days, row)
				s.Days++
				s.Shifts += row.Shifts
				s.PayableMinutes += row.PayableMinutes
				s.ApprovedOvertimeMinutes += row.ApprovedOvertimeMinutes
				s.UnpaidHalfDays += row.UnpaidHalfDays
				t.add(*d)
				hospitalTally.add(*d)
			}
			for _, day := range earlier[person.ID] {
				if _, ok := derived[day]; !ok {
					s.removed = append(s.removed, day)
				}
			}
			if len(s.days) == 0 && len(s.removed) == 0 {
				continue
			}
			s.Statuses, s.Exceptions = counts(attendanceStatuses, t.statuses), counts(attendanceExceptions, t.exceptions)
			total.Days += s.Days
			total.Shifts += s.Shifts
			total.PayableMinutes += s.PayableMinutes
			total.ApprovedOvertimeMinutes += s.ApprovedOvertimeMinutes
			total.UnpaidHalfDays += s.UnpaidHalfDays
			kept = append(kept, s)
		}
	}
	if len(blocked) > 0 {
		return periodNotReady{blocked}
	}
	revisions := make([]string, 0, len(policies))
	for rev := range policies {
		revisions = append(revisions, rev.String())
	}
	slices.Sort(revisions)
	statuses, err := json.Marshal(counts(attendanceStatuses, hospitalTally.statuses))
	if err != nil {
		return err
	}
	exceptions, err := json.Marshal(counts(attendanceExceptions, hospitalTally.exceptions))
	if err != nil {
		return err
	}
	revisionList, err := json.Marshal(revisions)
	if err != nil {
		return err
	}
	if err := tx.Exec(`INSERT INTO workforce_attendance_timesheets(tenant_id,hospital_id,id,period_id,number,people,days,shifts,payable_minutes,approved_overtime_minutes,unpaid_half_days,statuses,exceptions,policy_revision_ids,note,finalised_by_subject)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?::jsonb,?::jsonb,ARRAY(SELECT jsonb_array_elements_text(?::jsonb)::uuid),?,?)`,
		p.TenantID, hospital, id, period, number, len(kept), total.Days, total.Shifts, total.PayableMinutes, total.ApprovedOvertimeMinutes, total.UnpaidHalfDays,
		string(statuses), string(exceptions), string(revisionList), note, p.Subject).Error; err != nil {
		return err
	}
	return writeSnapshot(tx, p.TenantID, hospital, id, previous, kept)
}

// writeSnapshot inserts people, then their days marked against the previous timesheet, then the days that disappeared.
func writeSnapshot(tx *gorm.DB, tenant string, hospital, timesheet, previous uuid.UUID, people []*snapshotPerson) error {
	for batch := range slices.Chunk(people, timesheetBatch) {
		var days, removed []snapshotDay
		for _, s := range batch {
			days = append(days, s.days...)
			for _, day := range s.removed {
				removed = append(removed, snapshotDay{StaffID: s.StaffID, Day: day})
			}
		}
		rows, err := json.Marshal(batch)
		if err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_attendance_timesheet_people(tenant_id,hospital_id,timesheet_id,staff_id,employee_code,display_name,sort_at,days,shifts,payable_minutes,approved_overtime_minutes,unpaid_half_days,statuses,exceptions)
 SELECT ?,?,?,r.staff_id,r.employee_code,r.display_name,r.sort_at,r.days,r.shifts,r.payable_minutes,r.approved_overtime_minutes,r.unpaid_half_days,r.statuses,r.exceptions
 FROM jsonb_to_recordset(?::jsonb) AS r(staff_id uuid,employee_code text,display_name text,sort_at timestamptz,days int,shifts int,payable_minutes int,approved_overtime_minutes int,unpaid_half_days int,statuses jsonb,exceptions jsonb)`,
			tenant, hospital, timesheet, string(rows)).Error; err != nil {
			return err
		}
		for chunk := range slices.Chunk(days, timesheetDayBatch) {
			rows, err := json.Marshal(chunk)
			if err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO workforce_attendance_timesheet_days(tenant_id,hospital_id,timesheet_id,staff_id,day,change,shifts,payable_minutes,approved_overtime_minutes,unpaid_half_days,detail)
 SELECT ?,?,?,r.staff_id,r.day,CASE WHEN b.detail IS NULL THEN 'added' WHEN b.detail=r.detail THEN 'unchanged' ELSE 'changed' END,r.shifts,r.payable_minutes,r.approved_overtime_minutes,r.unpaid_half_days,r.detail
 FROM jsonb_to_recordset(?::jsonb) AS r(staff_id uuid,day date,shifts int,payable_minutes int,approved_overtime_minutes int,unpaid_half_days int,detail jsonb)
 LEFT JOIN workforce_attendance_timesheet_days b ON (b.tenant_id,b.hospital_id,b.timesheet_id,b.staff_id,b.day)=(?::uuid,?::uuid,?::uuid,r.staff_id,r.day)`,
				tenant, hospital, timesheet, string(rows), tenant, hospital, previous).Error; err != nil {
				return err
			}
		}
		for chunk := range slices.Chunk(removed, timesheetDayBatch) {
			rows, err := json.Marshal(chunk)
			if err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO workforce_attendance_timesheet_days(tenant_id,hospital_id,timesheet_id,staff_id,day,change,shifts,payable_minutes,approved_overtime_minutes,unpaid_half_days)
 SELECT ?,?,?,r.staff_id,r.day,'removed',0,0,0,0 FROM jsonb_to_recordset(?::jsonb) AS r(staff_id uuid,day date)`, tenant, hospital, timesheet, string(rows)).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// putPeriod closes a date range under a client-chosen id and writes timesheet 1; an identical retry returns the period.
func (h *handlers) putPeriod(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, ok := periodID(c)
	if !ok {
		return
	}
	var in attendancePeriodInput
	if !decode(c, &in) {
		return
	}
	in.Note = strings.TrimSpace(in.Note)
	if !periodRange(in.From, in.To) || !validLeaveText(in.Note) {
		respondPeriodError(c, errPeriodInvalid)
		return
	}
	var result attendancePeriod
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_attendance", attendanceLock); err != nil {
			return err
		}
		existing, err := loadPeriodRow(tx, p.TenantID, hospital, id, false)
		switch {
		case err == nil:
			var first struct{ Note string }
			if err := tx.Raw(`SELECT note FROM workforce_attendance_period_events WHERE tenant_id=? AND hospital_id=? AND period_id=? AND version=1`, p.TenantID, hospital, id).Scan(&first).Error; err != nil {
				return err
			}
			if existing.PeriodFrom != in.From || existing.PeriodTo != in.To || first.Note != in.Note {
				return errPeriodReused
			}
			result, err = loadPeriod(c, tx, p.TenantID, hospital, id)
			return err
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		}
		var overlap struct{ PeriodID uuid.UUID }
		if err := tx.Raw(`SELECT period_id FROM workforce_attendance_period_days WHERE tenant_id=? AND hospital_id=? AND day BETWEEN ?::date AND ?::date ORDER BY day LIMIT 1`,
			p.TenantID, hospital, in.From, in.To).Scan(&overlap).Error; err != nil {
			return err
		}
		if overlap.PeriodID != uuid.Nil {
			return periodOverlap{overlap.PeriodID}
		}
		if err := periodReady(tx, p.TenantID, hospital, in.From, in.To); err != nil {
			return err
		}
		// The period names its timesheet up front; the deferred foreign key checks that the timesheet exists at commit.
		row := periodRow{ID: id, Status: "finalised", Version: 1, TimesheetID: uuid.New(), TimesheetNumber: 1}
		if err := tx.Exec(`INSERT INTO workforce_attendance_periods(tenant_id,hospital_id,id,period_from,period_to,status,version,timesheet_id,timesheet_number,created_by_subject) VALUES(?,?,?,?::date,?::date,'finalised',1,?,1,?)`,
			p.TenantID, hospital, id, in.From, in.To, row.TimesheetID, p.Subject).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_attendance_period_days(tenant_id,hospital_id,day,period_id) SELECT ?,?,d::date,? FROM generate_series(?::date,?::date,interval '1 day') AS d`,
			p.TenantID, hospital, id, in.From, in.To).Error; err != nil {
			return err
		}
		if err := snapshot(tx, p, hospital, id, row.TimesheetID, in.From, in.To, 1, uuid.Nil, in.Note); err != nil {
			return err
		}
		if err := h.recordPeriod(tx, p, hospital, row, in.Note); err != nil {
			return err
		}
		result, err = loadPeriod(c, tx, p.TenantID, hospital, id)
		return err
	})
	if err != nil {
		respondPeriodError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) finalisePeriod(c *gin.Context) { h.transitionPeriod(c, "finalised") }
func (h *handlers) reopenPeriod(c *gin.Context)   { h.transitionPeriod(c, "reopened") }

// transitionPeriod reopens a finalised period, or finalises a period again into its next timesheet.
func (h *handlers) transitionPeriod(c *gin.Context, to string) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, ok := periodID(c)
	if !ok {
		return
	}
	var in attendancePeriodTransitionInput
	if !decode(c, &in) {
		return
	}
	note := strings.TrimSpace(in.Note)
	if note == "" || !validLeaveText(note) {
		respondPeriodError(c, errPeriodNote)
		return
	}
	expected := *in.ExpectedVersion
	var result attendancePeriod
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_attendance", attendanceLock); err != nil {
			return err
		}
		r, err := loadPeriodRow(tx, p.TenantID, hospital, id, true)
		if err != nil {
			return err
		}
		if r.Version == expected+1 && r.Status == to {
			var replay bool
			if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_attendance_period_events WHERE tenant_id=? AND hospital_id=? AND period_id=? AND version=? AND actor_subject=? AND note=?)`,
				p.TenantID, hospital, id, r.Version, p.Subject, note).Scan(&replay).Error; err != nil {
				return err
			}
			if replay {
				result, err = loadPeriod(c, tx, p.TenantID, hospital, id)
				return err
			}
		}
		if r.Version != expected {
			return errPeriodStale
		}
		next := r
		next.Version, next.Status = expected+1, to
		if to == "reopened" {
			if r.Status != "finalised" {
				return errPeriodState
			}
		} else {
			if err := periodReady(tx, p.TenantID, hospital, r.PeriodFrom, r.PeriodTo); err != nil {
				return err
			}
			next.TimesheetID, next.TimesheetNumber = uuid.New(), r.TimesheetNumber+1
			if err := snapshot(tx, p, hospital, id, next.TimesheetID, r.PeriodFrom, r.PeriodTo, next.TimesheetNumber, r.TimesheetID, note); err != nil {
				return err
			}
		}
		update := tx.Exec(`UPDATE workforce_attendance_periods SET status=?,version=?,timesheet_id=?,timesheet_number=?,updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND id=? AND version=?`,
			next.Status, next.Version, next.TimesheetID, next.TimesheetNumber, p.TenantID, hospital, id, expected)
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return errPeriodStale
		}
		if err := h.recordPeriod(tx, p, hospital, next, note); err != nil {
			return err
		}
		result, err = loadPeriod(c, tx, p.TenantID, hospital, id)
		return err
	})
	if err != nil {
		respondPeriodError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) getPeriod(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, ok := periodID(c)
	if !ok {
		return
	}
	var result attendancePeriod
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var err error
		result, err = loadPeriod(c, tx, p.TenantID, hospital, id)
		return err
	})
	if err != nil {
		respondPeriodError(c, err)
		return
	}
	respond.OK(c, result)
}

// listPeriods returns periods newest first, each with its current timesheet.
func (h *handlers) listPeriods(c *gin.Context, params pagination.Params) ([]periodPage, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respondPeriodError(c, errPeriodPage)
		return nil, nil
	}
	out := []periodPage{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var rows []periodRow
		if err := tenantdb.ApplyKeyset(tx, params).Table("workforce_attendance_periods").Select(periodColumns).Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Find(&rows).Error; err != nil {
			return err
		}
		loaded, err := periods(c, tx, p.TenantID, hospital, rows)
		if err != nil {
			return err
		}
		for _, period := range loaded {
			out = append(out, periodPage{period})
		}
		return nil
	})
	if err != nil {
		respondPeriodError(c, err)
		return nil, nil
	}
	return out, nil
}

// periodExists makes child listings answer 404 for a period of another hospital or tenant.
func periodExists(tx *gorm.DB, tenant string, hospital, id uuid.UUID) error {
	var exists bool
	if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_attendance_periods WHERE tenant_id=? AND hospital_id=? AND id=?)`, tenant, hospital, id).Scan(&exists).Error; err != nil {
		return err
	}
	if !exists {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func (h *handlers) periodHistory(c *gin.Context, params pagination.Params) ([]attendancePeriodEvent, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	id, ok := periodID(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respondPeriodError(c, errPeriodPage)
		return nil, nil
	}
	rows := []attendancePeriodEvent{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		if err := periodExists(tx, p.TenantID, hospital, id); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Table("workforce_attendance_period_events").Select("id,version,status,timesheet_id,note,actor_subject,created_at").
			Where("tenant_id=? AND hospital_id=? AND period_id=?", p.TenantID, hospital, id).Find(&rows).Error
	})
	if err != nil {
		respondPeriodError(c, err)
		return nil, nil
	}
	if !periodNoteReader(c) {
		for i := range rows {
			rows[i].Note = ""
		}
	}
	return rows, nil
}

func (h *handlers) listTimesheets(c *gin.Context, params pagination.Params) ([]timesheetPage, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	id, ok := periodID(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respondPeriodError(c, errPeriodPage)
		return nil, nil
	}
	out := []timesheetPage{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		if err := periodExists(tx, p.TenantID, hospital, id); err != nil {
			return err
		}
		var rows []timesheetRow
		if err := tenantdb.ApplyKeyset(tx, params).Table("workforce_attendance_timesheets").Select(timesheetColumns).
			Where("tenant_id=? AND hospital_id=? AND period_id=?", p.TenantID, hospital, id).Find(&rows).Error; err != nil {
			return err
		}
		loaded, err := timesheets(c, tx, p.TenantID, hospital, rows)
		if err != nil {
			return err
		}
		for _, t := range loaded {
			out = append(out, timesheetPage{t})
		}
		return nil
	})
	if err != nil {
		respondPeriodError(c, err)
		return nil, nil
	}
	return out, nil
}

// timesheetOf checks that the timesheet belongs to the period, so a mismatched pair answers 404.
func timesheetOf(c *gin.Context, tx *gorm.DB, tenant string, hospital uuid.UUID) (uuid.UUID, error) {
	period, errPeriod := uuid.Parse(c.Param("period"))
	timesheet, errTimesheet := uuid.Parse(c.Param("timesheet"))
	if errPeriod != nil || errTimesheet != nil {
		return uuid.Nil, gorm.ErrRecordNotFound
	}
	var exists bool
	if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_attendance_timesheets WHERE tenant_id=? AND hospital_id=? AND period_id=? AND id=?)`, tenant, hospital, period, timesheet).Scan(&exists).Error; err != nil {
		return uuid.Nil, err
	}
	if !exists {
		return uuid.Nil, gorm.ErrRecordNotFound
	}
	return timesheet, nil
}

// listTimesheetPeople pages a version's per-person totals in staff-list order.
func (h *handlers) listTimesheetPeople(c *gin.Context, params pagination.Params) ([]timesheetPersonPage, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respondPeriodError(c, errPeriodPage)
		return nil, nil
	}
	out := []timesheetPersonPage{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		timesheet, err := timesheetOf(c, tx, p.TenantID, hospital)
		if err != nil {
			return err
		}
		var rows []struct {
			ID                                                                    uuid.UUID
			EmployeeCode, DisplayName, Statuses, Exceptions                       string
			Days, Shifts, PayableMinutes, ApprovedOvertimeMinutes, UnpaidHalfDays int
			CreatedAt                                                             time.Time
		}
		view := tx.Session(&gorm.Session{NewDB: true}).Table("workforce_attendance_timesheet_people").
			Select("staff_id AS id,employee_code,display_name,statuses::text AS statuses,exceptions::text AS exceptions,days,shifts,payable_minutes,approved_overtime_minutes,unpaid_half_days,sort_at AS created_at").
			Where("tenant_id=? AND hospital_id=? AND timesheet_id=?", p.TenantID, hospital, timesheet)
		if err := tenantdb.ApplyKeyset(tx.Table("(?) AS v", view), params).Find(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			person := attendanceTimesheetPerson{StaffID: r.ID, EmployeeCode: r.EmployeeCode, DisplayName: r.DisplayName, Days: r.Days, Shifts: r.Shifts,
				PayableMinutes: r.PayableMinutes, ApprovedOvertimeMinutes: r.ApprovedOvertimeMinutes, UnpaidHalfDays: r.UnpaidHalfDays}
			if err := json.Unmarshal([]byte(r.Statuses), &person.Statuses); err != nil {
				return err
			}
			if err := json.Unmarshal([]byte(r.Exceptions), &person.Exceptions); err != nil {
				return err
			}
			out = append(out, timesheetPersonPage{person, r.CreatedAt})
		}
		return nil
	})
	if err != nil {
		respondPeriodError(c, err)
		return nil, nil
	}
	return out, nil
}

// listTimesheetDays pages a version's day rows, newest day first, with each derived day as finalised.
func (h *handlers) listTimesheetDays(c *gin.Context, params pagination.Params) ([]timesheetDayPage, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	where := "tenant_id=? AND hospital_id=? AND timesheet_id=?"
	var filters []any
	valid := params.Limit <= 100
	if s := c.Query("staff_id"); s != "" {
		staff, err := uuid.Parse(s)
		valid = valid && err == nil && staff != uuid.Nil
		where += " AND staff_id=?"
		filters = append(filters, staff)
	}
	if s := c.Query("change"); s != "" {
		valid = valid && slices.Contains(timesheetChanges, s)
		where += " AND change=?"
		filters = append(filters, s)
	}
	if !valid {
		respondPeriodError(c, errTimesheetFilter)
		return nil, nil
	}
	out := []timesheetDayPage{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		timesheet, err := timesheetOf(c, tx, p.TenantID, hospital)
		if err != nil {
			return err
		}
		var rows []struct {
			ID                                                              uuid.UUID
			Day, Change                                                     string
			Shifts, PayableMinutes, ApprovedOvertimeMinutes, UnpaidHalfDays int
			Detail                                                          *string
			CreatedAt                                                       time.Time
		}
		view := tx.Session(&gorm.Session{NewDB: true}).Table("workforce_attendance_timesheet_days").
			Select("staff_id AS id,day::text AS day,change,shifts,payable_minutes,approved_overtime_minutes,unpaid_half_days,detail::text AS detail,day::timestamp AT TIME ZONE 'UTC' AS created_at").
			Where(where, append([]any{p.TenantID, hospital, timesheet}, filters...)...)
		if err := tenantdb.ApplyKeyset(tx.Table("(?) AS v", view), params).Find(&rows).Error; err != nil {
			return err
		}
		for _, r := range rows {
			day := attendanceTimesheetDay{StaffID: r.ID, Date: r.Day, Change: r.Change, Shifts: r.Shifts, PayableMinutes: r.PayableMinutes,
				ApprovedOvertimeMinutes: r.ApprovedOvertimeMinutes, UnpaidHalfDays: r.UnpaidHalfDays}
			if r.Detail != nil {
				day.Day = &attendanceDay{}
				if err := json.Unmarshal([]byte(*r.Detail), day.Day); err != nil {
					return err
				}
			}
			out = append(out, timesheetDayPage{day, r.CreatedAt})
		}
		return nil
	})
	if err != nil {
		respondPeriodError(c, err)
		return nil, nil
	}
	return out, nil
}

// punchReportView lists punches with job category and void state; reasons are deliberately never selected.
const punchReportView = `(SELECT p.tenant_id,p.hospital_id,p.id,p.staff_id,s.employee_code,s.display_name,s.job_category,p.direction,p.punched_at AT TIME ZONE 'UTC' AS punched_at,p.local_date,
 p.local_date::text AS local_day,p.timezone,p.source,p.correction_id,p.recorded_by_subject,p.created_at,p.created_at AS recorded_at,v.punch_id IS NOT NULL AS voided,v.voided_by_subject,v.created_at AS voided_at,v.correction_id AS void_correction_id
 FROM workforce_attendance_punches p JOIN workforce_staff s ON (s.tenant_id,s.hospital_id,s.id)=(p.tenant_id,p.hospital_id,p.staff_id)
 LEFT JOIN workforce_attendance_punch_voids v ON (v.tenant_id,v.hospital_id,v.punch_id)=(p.tenant_id,p.hospital_id,p.id)) AS v`

// punchReport lists a hospital's punches over at most 31 local days, most recently recorded first.
func (h *handlers) punchReport(c *gin.Context, params pagination.Params) ([]punchReportPage, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	from, to := c.Query("from"), c.Query("to")
	where := "tenant_id=? AND hospital_id=? AND local_date BETWEEN ?::date AND ?::date"
	args := []any{p.TenantID, hospital, from, to}
	valid := params.Limit <= 100 && periodRange(from, to)
	if s := c.Query("staff_id"); s != "" {
		staff, err := uuid.Parse(s)
		valid = valid && err == nil && staff != uuid.Nil
		where += " AND staff_id=?"
		args = append(args, staff)
	}
	if !valid {
		respondPeriodError(c, errPunchReport)
		return nil, nil
	}
	rows := []punchReportPage{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Table(punchReportView).
			Select("id,staff_id,employee_code,display_name,job_category,direction,punched_at,local_day AS local_date,timezone,source,correction_id,recorded_by_subject,recorded_at,voided,voided_by_subject,voided_at,void_correction_id").
			Where(where, args...).Find(&rows).Error
	})
	if err != nil {
		respondPeriodError(c, err)
		return nil, nil
	}
	return rows, nil
}

func respondPeriodError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	var overlap periodOverlap
	var pending periodPending
	var notReady periodNotReady
	switch {
	case errors.As(err, &overlap):
		respond.ConflictWithDetail(c, "Another attendance period already covers some of these days.", gin.H{"reason": "period_overlap", "period_id": overlap.id})
	case errors.As(err, &pending):
		respond.ConflictWithDetail(c, "Decide the pending corrections for these days first.", gin.H{"reason": "pending_corrections", "count": pending.count})
	case errors.As(err, &notReady):
		respond.ConflictWithDetail(c, "Some shifts in the period are not settled yet.", gin.H{"reason": "period_not_ready", "statuses": notReady.counts})
	case errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "workforce_attendance_period_days_pkey":
		// Backstop: finalisations are serialised by the attendance lock, so only a writer that bypassed it reaches this.
		respond.ConflictWithDetail(c, "Another attendance period already covers some of these days.", gin.H{"reason": "period_overlap"})
	case errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "workforce_attendance_periods_pkey":
		respond.Error(c, errPeriodReused.status, errPeriodReused.code, errPeriodReused.message)
	default:
		respondAttendanceError(c, err)
	}
}

package workforce

import (
	"net/http"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type attendanceDayPage struct {
	attendanceDay
	CreatedAt time.Time `json:"-"`
}

func (d attendanceDayPage) PageKey() (time.Time, uuid.UUID) { return d.CreatedAt, d.StaffID }

// attendancePeople selects staff with a live allocation, approved overtime, or a punch no shift window claims on the date; it mirrors attendanceInputs.assign.
const attendancePeople = `SELECT s.tenant_id,s.hospital_id,s.id,s.employee_code,s.display_name,s.created_at FROM workforce_staff s WHERE s.tenant_id=@t AND s.hospital_id=@h AND (
 EXISTS(SELECT 1 FROM workforce_roster_allocations a JOIN workforce_rosters r ON (r.tenant_id,r.hospital_id,r.id)=(a.tenant_id,a.hospital_id,a.roster_id)
  WHERE (a.tenant_id,a.hospital_id,a.staff_id)=(s.tenant_id,s.hospital_id,s.id) AND a.roster_date=CAST(@d AS date) AND a.removed_version IS NULL AND r.status<>'discarded')
 OR EXISTS(SELECT 1 FROM workforce_overtime_requests o WHERE (o.tenant_id,o.hospital_id,o.staff_id)=(s.tenant_id,s.hospital_id,s.id) AND o.work_date=CAST(@d AS date) AND o.status='approved')
 OR EXISTS(SELECT 1 FROM workforce_attendance_punches p WHERE (p.tenant_id,p.hospital_id,p.staff_id)=(s.tenant_id,s.hospital_id,s.id) AND p.local_date=CAST(@d AS date)
  AND NOT EXISTS(SELECT 1 FROM workforce_attendance_punch_voids v WHERE (v.tenant_id,v.hospital_id,v.punch_id)=(p.tenant_id,p.hospital_id,p.id))
  AND NOT EXISTS(SELECT 1 FROM workforce_roster_allocations a JOIN workforce_rosters r ON (r.tenant_id,r.hospital_id,r.id)=(a.tenant_id,a.hospital_id,a.roster_id)
   LEFT JOIN workforce_attendance_setting_revisions pol ON (pol.tenant_id,pol.hospital_id,pol.id)=(a.tenant_id,a.hospital_id,workforce_attendance_policy_on(a.tenant_id,a.hospital_id,a.roster_date))
   WHERE (a.tenant_id,a.hospital_id,a.staff_id)=(p.tenant_id,p.hospital_id,p.staff_id) AND a.removed_version IS NULL AND r.status<>'discarded'
   AND p.punched_at BETWEEN a.starts_at - make_interval(mins => COALESCE(pol.early_arrival_minutes,0)) AND a.ends_at + make_interval(mins => COALESCE(pol.late_departure_minutes,0))))
)`

func attendancePeopleOn(tx *gorm.DB, tenant string, hospital uuid.UUID, date string) *gorm.DB {
	people := tx.Session(&gorm.Session{NewDB: true}).Raw(attendancePeople, map[string]any{"t": tenant, "h": hospital, "d": date})
	return tx.Table("(?) AS v", people).Select("id,employee_code,display_name,created_at")
}

type attendancePerson struct {
	ID                        uuid.UUID
	EmployeeCode, DisplayName string
	CreatedAt                 time.Time
}

// loadAttendance reads one query per input kind for the people over [from, to], widened so adjacent shifts can claim their punches.
func loadAttendance(tx *gorm.DB, tenant string, hospital uuid.UUID, people []uuid.UUID, from, to time.Time) (map[uuid.UUID]*attendanceInputs, map[string]string, error) {
	inputs := map[uuid.UUID]*attendanceInputs{}
	for _, id := range people {
		inputs[id] = &attendanceInputs{policies: map[string]*attendancePolicy{}, leave: map[string]leaveCover{}, overtime: map[string]int{}}
	}
	timezones := map[string]string{}
	if len(people) == 0 {
		return inputs, timezones, nil
	}
	date := func(t time.Time) string { return t.Format(time.DateOnly) }
	var allocations []attendanceAllocation
	if err := tx.Raw(`SELECT a.id,a.roster_id,a.shift_revision_id,a.staff_id,a.roster_date::text AS roster_date,r.status AS roster_status,sr.code AS shift_code,sr.name AS shift_name,
 a.starts_at,a.ends_at,sr.break_minutes,sr.grace_minutes FROM workforce_roster_allocations a
 JOIN workforce_rosters r ON (r.tenant_id,r.hospital_id,r.id)=(a.tenant_id,a.hospital_id,a.roster_id)
 JOIN workforce_shift_revisions sr ON (sr.tenant_id,sr.hospital_id,sr.id)=(a.tenant_id,a.hospital_id,a.shift_revision_id)
 WHERE a.tenant_id=? AND a.hospital_id=? AND a.staff_id IN ? AND a.removed_version IS NULL AND r.status<>'discarded' AND a.roster_date BETWEEN ?::date AND ?::date
 ORDER BY a.starts_at,a.id`, tenant, hospital, people, date(from.AddDate(0, 0, -2)), date(to.AddDate(0, 0, 1))).Scan(&allocations).Error; err != nil {
		return nil, nil, err
	}
	for _, a := range allocations {
		inputs[a.StaffID].allocations = append(inputs[a.StaffID].allocations, a)
	}
	var punches []attendanceObservation
	if err := tx.Raw(`SELECT p.id,p.staff_id,p.direction,p.punched_at,p.local_date::text AS local_date FROM workforce_attendance_punches p
 WHERE p.tenant_id=? AND p.hospital_id=? AND p.staff_id IN ? AND p.local_date BETWEEN ?::date AND ?::date
 AND NOT EXISTS(SELECT 1 FROM workforce_attendance_punch_voids v WHERE (v.tenant_id,v.hospital_id,v.punch_id)=(p.tenant_id,p.hospital_id,p.id))
 ORDER BY p.punched_at,p.id`, tenant, hospital, people, date(from.AddDate(0, 0, -2)), date(to.AddDate(0, 0, 3))).Scan(&punches).Error; err != nil {
		return nil, nil, err
	}
	for _, p := range punches {
		inputs[p.StaffID].punches = append(inputs[p.StaffID].punches, p)
	}
	var policies []struct {
		Day, Timezone                                                        string
		ID                                                                   uuid.UUID
		EarlyArrivalMinutes, LateDepartureMinutes                            int
		EarlyDepartureGraceMinutes, ShortDutyMinutes, DuplicateWindowMinutes int
	}
	if err := tx.Raw(`SELECT d::date::text AS day,COALESCE(rs.timezone,'') AS timezone,COALESCE(r.id,'00000000-0000-0000-0000-000000000000') AS id,
 COALESCE(r.early_arrival_minutes,0) AS early_arrival_minutes,COALESCE(r.late_departure_minutes,0) AS late_departure_minutes,COALESCE(r.early_departure_grace_minutes,0) AS early_departure_grace_minutes,
 COALESCE(r.short_duty_minutes,0) AS short_duty_minutes,COALESCE(r.duplicate_window_minutes,1) AS duplicate_window_minutes
 FROM generate_series(?::date,?::date,interval '1 day') AS d
 LEFT JOIN workforce_attendance_setting_revisions r ON (r.tenant_id,r.hospital_id,r.id)=(?::uuid,?::uuid,workforce_attendance_policy_on(?::uuid,?::uuid,d::date))
 LEFT JOIN workforce_roster_setting_revisions rs ON (rs.tenant_id,rs.hospital_id,rs.id)=(?::uuid,?::uuid,workforce_roster_settings_on(?::uuid,?::uuid,d::date))`,
		date(from.AddDate(0, 0, -2)), date(to.AddDate(0, 0, 1)), tenant, hospital, tenant, hospital, tenant, hospital, tenant, hospital).Scan(&policies).Error; err != nil {
		return nil, nil, err
	}
	for _, p := range policies {
		timezones[p.Day] = p.Timezone
		if p.ID == uuid.Nil {
			continue
		}
		policy := attendancePolicy{ID: p.ID, EarlyArrivalMinutes: p.EarlyArrivalMinutes, LateDepartureMinutes: p.LateDepartureMinutes,
			EarlyDepartureGraceMinutes: p.EarlyDepartureGraceMinutes, ShortDutyMinutes: p.ShortDutyMinutes, DuplicateWindowMinutes: p.DuplicateWindowMinutes}
		for _, in := range inputs {
			in.policies[p.Day] = &policy
		}
	}
	var leave []struct {
		StaffID  uuid.UUID
		Day      string
		Unit     string
		Session  *string
		Minutes  int
		HalfDays int
		Holiday  bool
	}
	if err := tx.Raw(`SELECT r.staff_id,d.day::text AS day,r.unit,r.session,COALESCE(r.minutes,0) AS minutes,d.half_days,d.holiday FROM workforce_leave_requests r
 JOIN workforce_leave_request_days d ON (d.tenant_id,d.hospital_id,d.request_id)=(r.tenant_id,r.hospital_id,r.id)
 WHERE r.tenant_id=? AND r.hospital_id=? AND r.staff_id IN ? AND r.status='approved' AND d.day BETWEEN ?::date AND ?::date`, tenant, hospital, people, date(from), date(to)).Scan(&leave).Error; err != nil {
		return nil, nil, err
	}
	for _, l := range leave {
		cover := inputs[l.StaffID].leave[l.Day]
		switch {
		case l.Holiday:
			continue
		case l.Unit == "full_day" && l.HalfDays == 2:
			cover.firstHalf, cover.secondHalf = true, true
		case l.Unit == "half_day" && l.HalfDays == 1 && l.Session != nil:
			cover.firstHalf = cover.firstHalf || *l.Session == "first_half"
			cover.secondHalf = cover.secondHalf || *l.Session == "second_half"
		case l.Unit == "short_leave":
			cover.shortMinutes += l.Minutes
		default:
			continue
		}
		inputs[l.StaffID].leave[l.Day] = cover
	}
	var overtime []struct {
		StaffID  uuid.UUID
		WorkDate string
		Minutes  int
	}
	if err := tx.Raw(`SELECT staff_id,work_date::text AS work_date,sum(approved_minutes)::int AS minutes FROM workforce_overtime_requests
 WHERE tenant_id=? AND hospital_id=? AND staff_id IN ? AND status='approved' AND work_date BETWEEN ?::date AND ?::date GROUP BY staff_id,work_date`,
		tenant, hospital, people, date(from), date(to)).Scan(&overtime).Error; err != nil {
		return nil, nil, err
	}
	for _, o := range overtime {
		inputs[o.StaffID].overtime[o.WorkDate] = o.Minutes
	}
	return inputs, timezones, nil
}

// personDay derives one person's day, giving an empty day when they only appear through an adjacent date.
func personDay(in *attendanceInputs, person attendancePerson, day string, timezone string, now time.Time) attendanceDay {
	d, ok := deriveDays(*in, day, day, now)[day]
	if !ok {
		d = &attendanceDay{Date: day, Leave: in.leave[day].kind(), ShortLeaveMinutes: in.leave[day].shortMinutes, Shifts: []attendanceShift{}, StrayPunchIDs: []uuid.UUID{}, Exceptions: []string{}}
	}
	d.StaffID, d.EmployeeCode, d.DisplayName, d.Timezone = person.ID, person.EmployeeCode, person.DisplayName, timezone
	return *d
}

func attendanceDate(c *gin.Context) (time.Time, bool) {
	day, ok := parseWorkDate(c.Query("date"))
	if !ok || day.Format(time.DateOnly) != c.Query("date") {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Use a date between 2000 and 2199 and a page limit no greater than 100.")
		return day, false
	}
	return day, true
}

func (h *handlers) listAttendance(c *gin.Context, params pagination.Params) ([]attendanceDayPage, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	day, ok := attendanceDate(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Use a date between 2000 and 2199 and a page limit no greater than 100.")
		return nil, nil
	}
	rows := []attendanceDayPage{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var people []attendancePerson
		if err := tenantdb.ApplyKeyset(attendancePeopleOn(tx, p.TenantID, hospital, day.Format(time.DateOnly)), params).Find(&people).Error; err != nil {
			return err
		}
		inputs, timezones, err := loadAttendance(tx, p.TenantID, hospital, personIDs(people), day, day)
		if err != nil {
			return err
		}
		now := time.Now()
		for _, person := range people {
			rows = append(rows, attendanceDayPage{personDay(inputs[person.ID], person, day.Format(time.DateOnly), timezones[day.Format(time.DateOnly)], now), person.CreatedAt})
		}
		return nil
	})
	if err != nil {
		respondAttendanceError(c, err)
		return nil, nil
	}
	return rows, nil
}

func personIDs(people []attendancePerson) []uuid.UUID {
	ids := make([]uuid.UUID, len(people))
	for i, p := range people {
		ids[i] = p.ID
	}
	return ids
}

// tally counts statuses and exceptions in contract order, zeros included, so reports always show every column.
type tally struct{ statuses, exceptions map[string]int }

func newTally() tally { return tally{map[string]int{}, map[string]int{}} }

func (t tally) add(d attendanceDay) {
	for _, s := range d.Shifts {
		t.statuses[s.Status]++
		for _, e := range s.Exceptions {
			t.exceptions[e]++
		}
	}
	t.exceptions["stray_punch"] += len(d.StrayPunchIDs)
}

func counts(order []string, values map[string]int) []attendanceCount {
	out := make([]attendanceCount, 0, len(order))
	for _, code := range order {
		out = append(out, attendanceCount{Code: code, Count: values[code]})
	}
	return out
}

func unpaid(d attendanceDay) int {
	n := 0
	for _, s := range d.Shifts {
		n += s.UnpaidHalfDays
	}
	return n
}

func (h *handlers) attendanceTotals(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	day, ok := attendanceDate(c)
	if !ok {
		return
	}
	date := day.Format(time.DateOnly)
	result := attendanceTotals{Date: date}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var people []attendancePerson
		if err := attendancePeopleOn(tx, p.TenantID, hospital, date).Order("created_at,id").Find(&people).Error; err != nil {
			return err
		}
		inputs, timezones, err := loadAttendance(tx, p.TenantID, hospital, personIDs(people), day, day)
		if err != nil {
			return err
		}
		t, now := newTally(), time.Now()
		for _, person := range people {
			d := personDay(inputs[person.ID], person, date, timezones[date], now)
			t.add(d)
			result.Shifts += len(d.Shifts)
			result.PayableMinutes += d.PayableMinutes
			result.ApprovedOvertimeMinutes += d.ApprovedOvertimeMinutes
			result.UnpaidHalfDays += unpaid(d)
		}
		result.People = len(people)
		result.Statuses, result.Exceptions = counts(attendanceStatuses, t.statuses), counts(attendanceExceptions, t.exceptions)
		return nil
	})
	if err != nil {
		respondAttendanceError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) staffAttendance(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staff, from, to, ok := attendanceRange(c, c.Param("id"))
	if !ok {
		return
	}
	start, _ := parseWorkDate(from)
	end, _ := parseWorkDate(to)
	var result attendancePersonRange
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var person attendancePerson
		if err := tx.Table("workforce_staff").Select("id,employee_code,display_name,created_at").Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, staff).Take(&person).Error; err != nil {
			return err
		}
		inputs, timezones, err := loadAttendance(tx, p.TenantID, hospital, []uuid.UUID{staff}, start, end)
		if err != nil {
			return err
		}
		result = attendancePersonRange{StaffID: person.ID, EmployeeCode: person.EmployeeCode, DisplayName: person.DisplayName, From: from, To: to, Days: []attendanceDay{}}
		t := newTally()
		for date, d := range deriveDays(*inputs[staff], from, to, time.Now()) {
			d.StaffID, d.EmployeeCode, d.DisplayName, d.Timezone = person.ID, person.EmployeeCode, person.DisplayName, timezones[date]
			t.add(*d)
			result.PayableMinutes += d.PayableMinutes
			result.ApprovedOvertimeMinutes += d.ApprovedOvertimeMinutes
			result.UnpaidHalfDays += unpaid(*d)
			result.Days = append(result.Days, *d)
		}
		slices.SortFunc(result.Days, func(a, b attendanceDay) int { return compareStrings(a.Date, b.Date) })
		result.Statuses, result.Exceptions = counts(attendanceStatuses, t.statuses), counts(attendanceExceptions, t.exceptions)
		return nil
	})
	if err != nil {
		respondAttendanceError(c, err)
		return
	}
	respond.OK(c, result)
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

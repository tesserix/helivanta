package workforce

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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

func (a staffLeaveAssignment) PageKey() (time.Time, uuid.UUID) { return a.CreatedAt, a.StaffID }
func (y leaveYear) PageKey() (time.Time, uuid.UUID)            { return y.CreatedAt, y.ID }
func (r leaveRun) PageKey() (time.Time, uuid.UUID)             { return r.CreatedAt, r.ID }
func (e leaveLedgerEntry) PageKey() (time.Time, uuid.UUID)     { return e.CreatedAt, e.ID }
func (b staffLeaveBalances) PageKey() (time.Time, uuid.UUID)   { return b.CreatedAt, b.StaffID }

type staffLeaveAssignmentRevision struct{ staffLeaveAssignment }

func (a staffLeaveAssignmentRevision) PageKey() (time.Time, uuid.UUID) {
	return a.CreatedAt, a.RevisionID
}

var errLeaveSettingsFixed = errors.New("leave year start is fixed once set")
var errLeaveSettingsMissing = errors.New("leave settings are required")
var errLeaveSequence = errors.New("leave months post in order into an open year")
var errLeaveIncomplete = errors.New("leave year has unposted months")
var errLeaveNextYear = errors.New("next leave year must be open")
var errLeaveYearClosed = errors.New("leave year is not open for this date")
var errLeaveFrozen = errors.New("leave assignment change overlaps posted history")
var errLeaveCorrectionConflict = errors.New("leave correction id reused with different values")
var errLeaveEarly = errors.New("leave period has not started or ended yet")

// Every ledger write takes the same hospital lock, so postings, closes, corrections and assignment changes never interleave.
const leaveLedgerLock = "leave-ledger"

const leaveYearColumns = "id,leave_year,start_on::text AS start_on,end_on::text AS end_on,posted_months,closed,closed_by_subject,closed_at,version,created_at,updated_at"
const leaveAssignmentColumns = "staff_id,revision_id,employee_code,display_name,structure_revision_id,structure_id,structure_code,structure_name,active,effective_on,reviewed_by_subject,version,created_at,updated_at"
const leaveAssignmentSelect = `(SELECT a.tenant_id,a.hospital_id,a.staff_id,s.employee_code,s.display_name,a.structure_revision_id,t.config_id AS structure_id,t.code AS structure_code,t.name AS structure_name,a.active,a.effective_on::text AS effective_on,a.reviewed_by_subject,a.version,a.created_at,`
const leaveAssignmentJoins = ` a JOIN workforce_staff s ON (s.tenant_id,s.hospital_id,s.id)=(a.tenant_id,a.hospital_id,a.staff_id)
 JOIN workforce_leave_config_revisions t ON (t.tenant_id,t.hospital_id,t.id)=(a.tenant_id,a.hospital_id,a.structure_revision_id)) AS v`
const leaveAssignmentHeadView = leaveAssignmentSelect + `a.staff_id AS id,a.revision_id,a.updated_at FROM workforce_staff_leave_assignments` + leaveAssignmentJoins
const leaveAssignmentRevisionView = leaveAssignmentSelect + `a.id,a.id AS revision_id,a.created_at AS updated_at FROM workforce_staff_leave_assignment_revisions` + leaveAssignmentJoins

// Entries show the type code and name pinned by their revision, so history reads as it was posted.
const leaveLedgerView = `(SELECT l.tenant_id,l.hospital_id,l.id,l.staff_id,l.leave_type_id,l.leave_type_revision_id,t.code,t.name,l.leave_year,l.kind,l.half_days,l.effective_on::text AS effective_on,l.reason,l.actor_subject,l.run_id,l.created_at
 FROM workforce_leave_ledger l JOIN workforce_leave_config_revisions t ON (t.tenant_id,t.hospital_id,t.id)=(l.tenant_id,l.hospital_id,l.leave_type_revision_id)) AS v`
const leaveLedgerColumns = "id,staff_id,leave_type_id,leave_type_revision_id,code,name,leave_year,kind,half_days,effective_on,reason,actor_subject,run_id,created_at"

type leaveLedgerRow struct {
	TenantID             string
	HospitalID           uuid.UUID
	ID                   uuid.UUID
	StaffID              uuid.UUID
	LeaveTypeID          uuid.UUID
	LeaveTypeRevisionID  uuid.UUID
	LeaveYear            int
	Kind                 string
	HalfDays             int
	EffectiveOn          time.Time
	IdempotencyKey       string
	AssignmentRevisionID *uuid.UUID
	RunID                *uuid.UUID
	Reason               string
	ActorSubject         string
}

func leaveYearParam(c *gin.Context) (int, bool) {
	raw := c.Param("year")
	year, err := strconv.Atoi(raw)
	if err != nil || year < 2000 || year > 2199 || strconv.Itoa(year) != raw {
		respond.NotFound(c, "leave year")
		return 0, false
	}
	return year, true
}

func loadLeaveYear(tx *gorm.DB, tenant string, hospital uuid.UUID, year int, lock bool) (leaveYear, error) {
	var row leaveYear
	q := tx.Table("workforce_leave_years").Select(leaveYearColumns)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := q.Where("tenant_id=? AND hospital_id=? AND leave_year=?", tenant, hospital, year).Take(&row).Error
	return row, err
}

// leaveToday is the date everywhere on Earth (UTC-12), so no hospital opens, posts or closes a period early.
func leaveToday(tx *gorm.DB) (string, error) {
	var today string
	err := tx.Raw(`SELECT (clock_timestamp() AT TIME ZONE 'Etc/GMT+12')::date::text`).Scan(&today).Error
	return today, err
}

func (y leaveYear) period() (leavePeriod, error) {
	start, err := time.Parse("2006-01-02", y.StartOn)
	return leavePeriod{Year: y.LeaveYear, Start: start}, err
}

// loadLeaveRevisions returns every staff member's assignment history up to `through`, with balance-tracked entries keyed by leave type.
func loadLeaveRevisions(tx *gorm.DB, tenant string, hospital uuid.UUID, through time.Time) (map[uuid.UUID][]leaveAssignmentRevision, []uuid.UUID, error) {
	var heads []struct {
		ID                  uuid.UUID
		StaffID             uuid.UUID
		StructureRevisionID uuid.UUID
		Version             int64
		EffectiveOn         time.Time
		Active              bool
	}
	if err := tx.Raw(`SELECT id,staff_id,structure_revision_id,version,effective_on,active FROM workforce_staff_leave_assignment_revisions
 WHERE tenant_id=? AND hospital_id=? AND effective_on<=? ORDER BY staff_id,version`, tenant, hospital, through).Scan(&heads).Error; err != nil {
		return nil, nil, err
	}
	structures := map[uuid.UUID]bool{}
	for _, r := range heads {
		structures[r.StructureRevisionID] = true
	}
	var entries []struct {
		RevisionID           uuid.UUID
		LeaveTypeID          uuid.UUID
		LeaveTypeRevisionID  uuid.UUID
		AnnualHalfDays       int
		CarryForwardHalfDays int
		Accrual              string
		Proration            string
	}
	if len(structures) > 0 {
		if err := tx.Raw(`SELECT e.revision_id,t.config_id AS leave_type_id,e.leave_type_revision_id,e.annual_half_days,e.carry_forward_half_days,e.accrual,e.proration
 FROM workforce_leave_structure_entries e JOIN workforce_leave_config_revisions t ON (t.tenant_id,t.hospital_id,t.id)=(e.tenant_id,e.hospital_id,e.leave_type_revision_id)
 JOIN workforce_leave_type_rules r ON (r.tenant_id,r.hospital_id,r.revision_id)=(t.tenant_id,t.hospital_id,t.id) AND r.balance_tracked
 WHERE e.tenant_id=? AND e.hospital_id=? AND e.revision_id IN ?`, tenant, hospital, sortedRevisionIDs(structures)).Scan(&entries).Error; err != nil {
			return nil, nil, err
		}
	}
	policies := map[uuid.UUID]map[uuid.UUID]leavePolicyEntry{}
	for _, e := range entries {
		if policies[e.RevisionID] == nil {
			policies[e.RevisionID] = map[uuid.UUID]leavePolicyEntry{}
		}
		policies[e.RevisionID][e.LeaveTypeID] = leavePolicyEntry{TypeRevisionID: e.LeaveTypeRevisionID, Annual: e.AnnualHalfDays, Carry: e.CarryForwardHalfDays, Accrual: e.Accrual, Proration: e.Proration}
	}
	revs := map[uuid.UUID][]leaveAssignmentRevision{}
	var staff []uuid.UUID
	for _, r := range heads {
		if revs[r.StaffID] == nil {
			staff = append(staff, r.StaffID)
		}
		revs[r.StaffID] = append(revs[r.StaffID], leaveAssignmentRevision{ID: r.ID, Version: r.Version, EffectiveOn: r.EffectiveOn, Active: r.Active, Entries: policies[r.StructureRevisionID]})
	}
	return revs, staff, nil
}

func (h *handlers) publishLedger(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, year int, source string, reference uuid.UUID, entries int) error {
	data, err := json.Marshal(workforcecontract.LeaveLedgerChangedData{HospitalID: hospital.String(), LeaveYear: year, Source: source, ReferenceID: reference.String(), Entries: entries})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, LeaveLedgerChanged, events.Event{Type: LeaveLedgerChanged, Version: 1, TenantID: p.TenantID, Data: data})
}

func (h *handlers) getLeaveSettings(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	var result leaveSettings
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		return tx.Table("workforce_leave_settings").Select("year_start_month,reviewed_by_subject,created_at").Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Take(&result).Error
	})
	if err != nil {
		respondLeaveLedgerError(c, err)
		return
	}
	respond.OK(c, result)
}

// The leave-year start anchors every posted balance, so it is set once and never edited.
func (h *handlers) putLeaveSettings(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	var in leaveSettingsInput
	if !decode(c, &in) {
		return
	}
	if !*in.Reviewed {
		respondLeaveLedgerError(c, errLeaveInvalid)
		return
	}
	var result leaveSettings
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_leave", leaveLedgerLock); err != nil {
			return err
		}
		load := func() error {
			return tx.Table("workforce_leave_settings").Select("year_start_month,reviewed_by_subject,created_at").Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Take(&result).Error
		}
		err := load()
		switch {
		case err == nil && result.YearStartMonth == *in.YearStartMonth:
			return nil
		case err == nil:
			return errLeaveSettingsFixed
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_leave_settings(tenant_id,hospital_id,year_start_month,reviewed_by_subject) VALUES(?,?,?,?)`, p.TenantID, hospital, *in.YearStartMonth, p.Subject).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.leave_settings.created',?,jsonb_build_object('year_start_month',?::int))`, p.TenantID, hospital, p.Subject, hospital, *in.YearStartMonth).Error; err != nil {
			return err
		}
		return load()
	})
	if err != nil {
		respondLeaveLedgerError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) listStaffLeaveAssignments(c *gin.Context, params pagination.Params) ([]staffLeaveAssignment, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, 400, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	rows := []staffLeaveAssignment{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Table(leaveAssignmentHeadView).Select(leaveAssignmentColumns).Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Find(&rows).Error
	})
	if err != nil {
		respondLeaveLedgerError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) staffLeaveAssignmentHistory(c *gin.Context, params pagination.Params) ([]staffLeaveAssignmentRevision, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "staff leave assignment")
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, 400, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	found := []staffLeaveAssignment{}
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var exists bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_staff_leave_assignments WHERE tenant_id=? AND hospital_id=? AND staff_id=?)`, p.TenantID, hospital, id).Scan(&exists).Error; err != nil {
			return err
		}
		if !exists {
			return gorm.ErrRecordNotFound
		}
		return tenantdb.ApplyKeyset(tx, params).Table(leaveAssignmentRevisionView).Select(leaveAssignmentColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=?", p.TenantID, hospital, id).Find(&found).Error
	})
	if err != nil {
		respondLeaveLedgerError(c, err)
		return nil, nil
	}
	rows := make([]staffLeaveAssignmentRevision, len(found))
	for i := range found {
		rows[i] = staffLeaveAssignmentRevision{found[i]}
	}
	return rows, nil
}

func (h *handlers) putStaffLeaveAssignment(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staffID, err := uuid.Parse(c.Param("id"))
	if err != nil || staffID == uuid.Nil {
		respond.NotFound(c, "staff")
		return
	}
	var in staffLeaveAssignmentInput
	if !decode(c, &in) {
		return
	}
	if in.StructureRevisionID == uuid.Nil || !validEffectiveDate(in.EffectiveOn) || !*in.Reviewed {
		respondLeaveLedgerError(c, errLeaveInvalid)
		return
	}
	var result staffLeaveAssignment
	load := func(tx *gorm.DB, into *staffLeaveAssignment) error {
		return tx.Table(leaveAssignmentHeadView).Select(leaveAssignmentColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=?", p.TenantID, hospital, staffID).Take(into).Error
	}
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_leave_ledger", leaveLedgerLock); err != nil {
			return err
		}
		var person struct{ EmploymentStatus string }
		if err := tx.Raw(`SELECT employment_status FROM workforce_staff WHERE tenant_id=? AND hospital_id=? AND id=? FOR SHARE`, p.TenantID, hospital, staffID).Scan(&person).Error; err != nil {
			return err
		}
		if person.EmploymentStatus == "" {
			return gorm.ErrRecordNotFound
		}
		var old staffLeaveAssignment
		err := load(tx, &old)
		version := int64(1)
		pinned := false
		switch {
		case err == nil:
			if old.StructureRevisionID == in.StructureRevisionID && old.Active == *in.Active && old.EffectiveOn == in.EffectiveOn {
				result = old
				return nil
			}
			if old.Version != *in.ExpectedVersion {
				return errLeaveVersion
			}
			if in.EffectiveOn < old.EffectiveOn {
				return errPolicyEffective
			}
			pinned = old.StructureRevisionID == in.StructureRevisionID
			version = old.Version + 1
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		case *in.ExpectedVersion != 0:
			return gorm.ErrRecordNotFound
		}
		if *in.Active && person.EmploymentStatus != "active" {
			return errPolicyStaffInactive
		}
		// Mirrors workforce_leave_assignment_open() so the caller gets a specific answer; the trigger stays the backstop.
		var frozen bool
		if err := tx.Raw(`SELECT (?::bigint > 1 AND ?::date <= workforce_leave_posted_through(?::uuid,?::uuid)) IS TRUE
 OR EXISTS(SELECT 1 FROM workforce_leave_years WHERE tenant_id=? AND hospital_id=? AND closed AND end_on >= ?::date)`, version, in.EffectiveOn, p.TenantID, hospital, p.TenantID, hospital, in.EffectiveOn).Scan(&frozen).Error; err != nil {
			return err
		}
		if frozen {
			return errLeaveFrozen
		}
		var structure struct {
			Current     bool
			EffectiveOn string
		}
		if err := tx.Raw(`SELECT (c.active AND c.revision_id=r.id) AS current,r.effective_on::text AS effective_on
 FROM workforce_leave_config_revisions r JOIN workforce_leave_configs c ON (c.tenant_id,c.hospital_id,c.id)=(r.tenant_id,r.hospital_id,r.config_id)
 WHERE r.tenant_id=? AND r.hospital_id=? AND r.id=? AND r.kind='structure' FOR SHARE OF c`, p.TenantID, hospital, in.StructureRevisionID).Scan(&structure).Error; err != nil {
			return err
		}
		if structure.EffectiveOn == "" || structure.EffectiveOn > in.EffectiveOn || !structure.Current && !pinned {
			return errLeaveReference
		}
		revision := uuid.New()
		if err := tx.Exec(`INSERT INTO workforce_staff_leave_assignments(tenant_id,hospital_id,staff_id,structure_revision_id,active,effective_on,reviewed_by_subject,version,revision_id) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(tenant_id,hospital_id,staff_id) DO UPDATE SET structure_revision_id=EXCLUDED.structure_revision_id,active=EXCLUDED.active,effective_on=EXCLUDED.effective_on,reviewed_by_subject=EXCLUDED.reviewed_by_subject,version=EXCLUDED.version,revision_id=EXCLUDED.revision_id,updated_at=clock_timestamp()`, p.TenantID, hospital, staffID, in.StructureRevisionID, *in.Active, in.EffectiveOn, p.Subject, version, revision).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_staff_leave_assignment_revisions(tenant_id,hospital_id,id,staff_id,structure_revision_id,active,effective_on,reviewed_by_subject,version) VALUES(?,?,?,?,?,?,?,?,?)`, p.TenantID, hospital, revision, staffID, in.StructureRevisionID, *in.Active, in.EffectiveOn, p.Subject, version).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.staff_leave_assignment.changed',?,jsonb_build_object('version',?::bigint,'previous_version',?::bigint))`, p.TenantID, hospital, p.Subject, staffID, version, version-1).Error; err != nil {
			return err
		}
		data, err := json.Marshal(workforcecontract.StaffLeaveAssignmentChangedData{HospitalID: hospital.String(), StaffID: staffID.String(), RevisionID: revision.String(), Version: version})
		if err != nil {
			return err
		}
		if err := h.bus.Publish(tx, StaffLeaveAssignmentChanged, events.Event{Type: StaffLeaveAssignmentChanged, Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
			return err
		}
		return load(tx, &result)
	})
	if err != nil {
		respondLeaveLedgerError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) listLeaveYears(c *gin.Context, params pagination.Params) ([]leaveYear, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, 400, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	rows := []leaveYear{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Table("workforce_leave_years").Select(leaveYearColumns).Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Find(&rows).Error
	})
	if err != nil {
		respondLeaveLedgerError(c, err)
		return nil, nil
	}
	return rows, nil
}

// putLeaveYear opens a year from the fixed start month; reopening an existing year returns it unchanged.
func (h *handlers) putLeaveYear(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	year, ok := leaveYearParam(c)
	if !ok {
		return
	}
	var result leaveYear
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_leave_ledger", leaveLedgerLock); err != nil {
			return err
		}
		existing, err := loadLeaveYear(tx, p.TenantID, hospital, year, false)
		if err == nil {
			result = existing
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var settings struct{ YearStartMonth int }
		if err := tx.Raw(`SELECT year_start_month FROM workforce_leave_settings WHERE tenant_id=? AND hospital_id=?`, p.TenantID, hospital).Scan(&settings).Error; err != nil {
			return err
		}
		if settings.YearStartMonth == 0 {
			return errLeaveSettingsMissing
		}
		today, err := leaveToday(tx)
		if err != nil {
			return err
		}
		if current, _ := strconv.Atoi(today[:4]); year > current+1 {
			return errLeaveEarly
		}
		id := uuid.New()
		if err := tx.Exec(`INSERT INTO workforce_leave_years(tenant_id,hospital_id,id,leave_year,start_on,end_on,posted_months,closed,version)
 SELECT ?,?,?,?::smallint,d,(d + interval '1 year' - interval '1 day')::date,0,false,1 FROM make_date(?::int,?::int,1) AS d`, p.TenantID, hospital, id, year, year, settings.YearStartMonth).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.leave_year.opened',?,jsonb_build_object('leave_year',?::int))`, p.TenantID, hospital, p.Subject, id, year).Error; err != nil {
			return err
		}
		result, err = loadLeaveYear(tx, p.TenantID, hospital, year, false)
		return err
	})
	if err != nil {
		respondLeaveLedgerError(c, err)
		return
	}
	respond.OK(c, result)
}

func loadLeaveRun(tx *gorm.DB, tenant string, hospital uuid.UUID, year, step int, into *leaveRun) error {
	return tx.Table("workforce_leave_runs").Select("id,leave_year,step,entries,actor_subject,created_at").Where("tenant_id=? AND hospital_id=? AND leave_year=? AND step=?", tenant, hospital, year, step).Take(into).Error
}

// insertLeaveRun records the run and its entries; entries were filtered against existing keys under the ledger lock, so any conflict is a fault.
func insertLeaveRun(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, year, step int, rows []leaveLedgerRow, result *leaveRun) error {
	run := uuid.New()
	if err := tx.Exec(`INSERT INTO workforce_leave_runs(tenant_id,hospital_id,id,leave_year,step,entries,actor_subject) VALUES(?,?,?,?,?,?,?)`, p.TenantID, hospital, run, year, step, len(rows), p.Subject).Error; err != nil {
		return err
	}
	for i := range rows {
		rows[i].RunID = &run
	}
	if len(rows) > 0 {
		if err := tx.Table("workforce_leave_ledger").CreateInBatches(&rows, 500).Error; err != nil {
			return err
		}
	}
	return loadLeaveRun(tx, p.TenantID, hospital, year, step, result)
}

func (h *handlers) postLeaveMonth(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	year, ok := leaveYearParam(c)
	if !ok {
		return
	}
	var in leavePostingInput
	if !decode(c, &in) {
		return
	}
	month := *in.Month
	var result leaveRun
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_leave_ledger", leaveLedgerLock); err != nil {
			return err
		}
		y, err := loadLeaveYear(tx, p.TenantID, hospital, year, true)
		if err != nil {
			return err
		}
		if month <= y.PostedMonths {
			return loadLeaveRun(tx, p.TenantID, hospital, year, month, &result)
		}
		if y.Closed || month != y.PostedMonths+1 {
			return errLeaveSequence
		}
		period, err := y.period()
		if err != nil {
			return err
		}
		today, err := leaveToday(tx)
		if err != nil {
			return err
		}
		if period.monthStart(month).Format(time.DateOnly) > today {
			return errLeaveEarly
		}
		revs, staff, err := loadLeaveRevisions(tx, p.TenantID, hospital, period.monthEnd(month))
		if err != nil {
			return err
		}
		var posted []string
		if err := tx.Table("workforce_leave_ledger").Where("tenant_id=? AND hospital_id=? AND leave_year=? AND kind IN ('allocation','accrual')", p.TenantID, hospital, year).Pluck("idempotency_key", &posted).Error; err != nil {
			return err
		}
		done := make(map[string]bool, len(posted))
		for _, k := range posted {
			done[k] = true
		}
		// Credits for earlier months appear only when a first assignment started inside them; later revisions there are refused.
		rows := []leaveLedgerRow{}
		for _, s := range staff {
			for _, cr := range leaveCredits(s, period, month, revs[s]) {
				if done[cr.Key] {
					continue
				}
				assignment := cr.AssignmentRevisionID
				rows = append(rows, leaveLedgerRow{TenantID: p.TenantID, HospitalID: hospital, ID: uuid.New(), StaffID: s, LeaveTypeID: cr.TypeID, LeaveTypeRevisionID: cr.TypeRevisionID, LeaveYear: year, Kind: cr.Kind, HalfDays: cr.HalfDays, EffectiveOn: cr.EffectiveOn, IdempotencyKey: cr.Key, AssignmentRevisionID: &assignment, ActorSubject: p.Subject})
			}
		}
		if err := insertLeaveRun(tx, p, hospital, year, month, rows, &result); err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE workforce_leave_years SET posted_months=?,version=version+1,updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND leave_year=?`, month, p.TenantID, hospital, year).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.leave_month.posted',?,jsonb_build_object('leave_year',?::int,'month',?::int,'entries',?::int))`, p.TenantID, hospital, p.Subject, result.ID, year, month, len(rows)).Error; err != nil {
			return err
		}
		return h.publishLedger(tx, p, hospital, year, "posting", result.ID, len(rows))
	})
	if err != nil {
		respondLeaveLedgerError(c, err)
		return
	}
	respond.OK(c, result)
}

// closeLeaveYear carries each positive balance up to the limit in force on the last day into the next year and expires the rest.
func (h *handlers) closeLeaveYear(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	year, ok := leaveYearParam(c)
	if !ok {
		return
	}
	var result leaveRun
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_leave_ledger", leaveLedgerLock); err != nil {
			return err
		}
		y, err := loadLeaveYear(tx, p.TenantID, hospital, year, true)
		if err != nil {
			return err
		}
		if y.Closed {
			return loadLeaveRun(tx, p.TenantID, hospital, year, 13, &result)
		}
		if y.PostedMonths != 12 {
			return errLeaveIncomplete
		}
		today, err := leaveToday(tx)
		if err != nil {
			return err
		}
		if y.EndOn >= today {
			return errLeaveEarly
		}
		next, err := loadLeaveYear(tx, p.TenantID, hospital, year+1, true)
		if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && next.Closed {
			return errLeaveNextYear
		}
		if err != nil {
			return err
		}
		period, err := y.period()
		if err != nil {
			return err
		}
		nextPeriod, err := next.period()
		if err != nil {
			return err
		}
		end := period.end()
		revs, _, err := loadLeaveRevisions(tx, p.TenantID, hospital, end)
		if err != nil {
			return err
		}
		var balances []struct {
			StaffID             uuid.UUID
			LeaveTypeID         uuid.UUID
			LeaveTypeRevisionID uuid.UUID
			HalfDays            int
		}
		if err := tx.Raw(`SELECT DISTINCT ON (staff_id,leave_type_id) staff_id,leave_type_id,leave_type_revision_id,sum(half_days) OVER (PARTITION BY staff_id,leave_type_id)::int AS half_days
 FROM workforce_leave_ledger WHERE tenant_id=? AND hospital_id=? AND leave_year=? ORDER BY staff_id,leave_type_id,effective_on DESC,created_at DESC,id DESC`, p.TenantID, hospital, year).Scan(&balances).Error; err != nil {
			return err
		}
		rows := []leaveLedgerRow{}
		entry := func(b uuid.UUID, staff, leaveType uuid.UUID, y int, kind string, amount int, on time.Time) leaveLedgerRow {
			return leaveLedgerRow{TenantID: p.TenantID, HospitalID: hospital, ID: uuid.New(), StaffID: staff, LeaveTypeID: leaveType, LeaveTypeRevisionID: b, LeaveYear: y, Kind: kind, HalfDays: amount, EffectiveOn: on, IdempotencyKey: kind + ":" + staff.String() + ":" + leaveType.String() + ":" + strconv.Itoa(y), ActorSubject: p.Subject}
		}
		for _, b := range balances {
			limit := 0
			if r := leaveInForce(revs[b.StaffID], end); r != nil && r.Active {
				limit = r.Entries[b.LeaveTypeID].Carry
			}
			carried, expired := leaveCarry(b.HalfDays, limit)
			if carried > 0 {
				rows = append(rows, entry(b.LeaveTypeRevisionID, b.StaffID, b.LeaveTypeID, year, "carry_out", -carried, end), entry(b.LeaveTypeRevisionID, b.StaffID, b.LeaveTypeID, year+1, "carry_in", carried, nextPeriod.Start))
			}
			if expired > 0 {
				rows = append(rows, entry(b.LeaveTypeRevisionID, b.StaffID, b.LeaveTypeID, year, "expiry", -expired, end))
			}
		}
		if err := insertLeaveRun(tx, p, hospital, year, 13, rows, &result); err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE workforce_leave_years SET closed=true,closed_by_subject=?,closed_at=clock_timestamp(),version=version+1,updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND leave_year=?`, p.Subject, p.TenantID, hospital, year).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.leave_year.closed',?,jsonb_build_object('leave_year',?::int,'entries',?::int))`, p.TenantID, hospital, p.Subject, y.ID, year, len(rows)).Error; err != nil {
			return err
		}
		return h.publishLedger(tx, p, hospital, year, "year_end", result.ID, len(rows))
	})
	if err != nil {
		respondLeaveLedgerError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) listLeaveRuns(c *gin.Context, params pagination.Params) ([]leaveRun, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	year, ok := leaveYearParam(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, 400, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	rows := []leaveRun{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		if _, err := loadLeaveYear(tx, p.TenantID, hospital, year, false); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Table("workforce_leave_runs").Select("id,leave_year,step,entries,actor_subject,created_at").Where("tenant_id=? AND hospital_id=? AND leave_year=?", p.TenantID, hospital, year).Find(&rows).Error
	})
	if err != nil {
		respondLeaveLedgerError(c, err)
		return nil, nil
	}
	return rows, nil
}

// putLeaveCorrection records a reasoned adjustment under a client-chosen id; a retry with the same body returns the original entry.
func (h *handlers) putLeaveCorrection(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staffID, err := uuid.Parse(c.Param("id"))
	if err != nil || staffID == uuid.Nil {
		respond.NotFound(c, "staff")
		return
	}
	entryID, err := uuid.Parse(c.Param("entry"))
	if err != nil || entryID == uuid.Nil {
		respond.NotFound(c, "leave correction")
		return
	}
	var in leaveCorrectionInput
	if !decode(c, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	effective, dateErr := time.Parse("2006-01-02", in.EffectiveOn)
	if in.LeaveTypeID == uuid.Nil || *in.HalfDays == 0 || dateErr != nil || utf8.RuneCountInString(in.Reason) < 10 || strings.ContainsRune(in.Reason, 0) {
		respondLeaveLedgerError(c, errLeaveInvalid)
		return
	}
	var result leaveLedgerEntry
	load := func(tx *gorm.DB) error {
		return tx.Table(leaveLedgerView).Select(leaveLedgerColumns).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, entryID).Take(&result).Error
	}
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_leave_ledger", leaveLedgerLock); err != nil {
			return err
		}
		var existing struct {
			StaffID     uuid.UUID
			LeaveTypeID uuid.UUID
			LeaveYear   int
			Kind        string
			HalfDays    int
			EffectiveOn string
			Reason      string
		}
		if err := tx.Raw(`SELECT staff_id,leave_type_id,leave_year,kind,half_days,effective_on::text AS effective_on,reason FROM workforce_leave_ledger WHERE tenant_id=? AND hospital_id=? AND id=?`, p.TenantID, hospital, entryID).Scan(&existing).Error; err != nil {
			return err
		}
		if existing.Kind != "" {
			if existing.Kind != "correction" || existing.StaffID != staffID || existing.LeaveTypeID != in.LeaveTypeID || existing.LeaveYear != *in.LeaveYear || existing.HalfDays != *in.HalfDays || existing.EffectiveOn != in.EffectiveOn || existing.Reason != in.Reason {
				return errLeaveCorrectionConflict
			}
			return load(tx)
		}
		var person struct{ EmploymentStatus string }
		if err := tx.Raw(`SELECT employment_status FROM workforce_staff WHERE tenant_id=? AND hospital_id=? AND id=? FOR SHARE`, p.TenantID, hospital, staffID).Scan(&person).Error; err != nil {
			return err
		}
		if person.EmploymentStatus == "" {
			return gorm.ErrRecordNotFound
		}
		y, err := loadLeaveYear(tx, p.TenantID, hospital, *in.LeaveYear, false)
		if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && (y.Closed || in.EffectiveOn < y.StartOn || in.EffectiveOn > y.EndOn) {
			return errLeaveYearClosed
		}
		if err != nil {
			return err
		}
		// The correction pins the type revision in force on its date, which must still track balances.
		var leaveType struct {
			ID             uuid.UUID
			BalanceTracked bool
		}
		if err := tx.Raw(`SELECT r.id,t.balance_tracked FROM workforce_leave_config_revisions r JOIN workforce_leave_type_rules t ON (t.tenant_id,t.hospital_id,t.revision_id)=(r.tenant_id,r.hospital_id,r.id)
 WHERE r.tenant_id=? AND r.hospital_id=? AND r.config_id=? AND r.kind='leave_type' AND r.effective_on<=? ORDER BY r.version DESC LIMIT 1`, p.TenantID, hospital, in.LeaveTypeID, in.EffectiveOn).Scan(&leaveType).Error; err != nil {
			return err
		}
		if leaveType.ID == uuid.Nil || !leaveType.BalanceTracked {
			return errLeaveReference
		}
		row := leaveLedgerRow{TenantID: p.TenantID, HospitalID: hospital, ID: entryID, StaffID: staffID, LeaveTypeID: in.LeaveTypeID, LeaveTypeRevisionID: leaveType.ID, LeaveYear: *in.LeaveYear, Kind: "correction", HalfDays: *in.HalfDays, EffectiveOn: effective, IdempotencyKey: "correction:" + entryID.String(), Reason: in.Reason, ActorSubject: p.Subject}
		if err := tx.Table("workforce_leave_ledger").Create(&row).Error; err != nil {
			return err
		}
		// The reason may name clinical or personal circumstances, so the audit row records only the amount.
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.leave_correction.recorded',?,jsonb_build_object('leave_year',?::int,'half_days',?::int))`, p.TenantID, hospital, p.Subject, entryID, *in.LeaveYear, *in.HalfDays).Error; err != nil {
			return err
		}
		if err := h.publishLedger(tx, p, hospital, *in.LeaveYear, "correction", entryID, 1); err != nil {
			return err
		}
		return load(tx)
	})
	if err != nil {
		respondLeaveLedgerError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) staffLeaveLedger(c *gin.Context, params pagination.Params) ([]leaveLedgerEntry, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	staffID, err := uuid.Parse(c.Param("id"))
	if err != nil || staffID == uuid.Nil {
		respond.NotFound(c, "staff")
		return nil, nil
	}
	year, yearErr := strconv.Atoi(c.Query("leave_year"))
	if params.Limit > 100 || yearErr != nil || year < 2000 || year > 2199 {
		respond.Error(c, 400, "invalid_request", "Use a leave_year between 2000 and 2199 and a page limit no greater than 100.")
		return nil, nil
	}
	rows := []leaveLedgerEntry{}
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var exists bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_staff WHERE tenant_id=? AND hospital_id=? AND id=?)`, p.TenantID, hospital, staffID).Scan(&exists).Error; err != nil {
			return err
		}
		if !exists {
			return gorm.ErrRecordNotFound
		}
		return tenantdb.ApplyKeyset(tx, params).Table(leaveLedgerView).Select(leaveLedgerColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND leave_year=?", p.TenantID, hospital, staffID, year).Find(&rows).Error
	})
	if err != nil {
		respondLeaveLedgerError(c, err)
		return nil, nil
	}
	// Free-text reasons are minimum-necessary to leave managers; a missing permission set redacts them too.
	if set, ok := authz.PermissionsFrom(c); !ok || !set.Has(PermLeaveCorrect) && !set.Has(PermLeaveAllocate) {
		for i := range rows {
			rows[i].Reason = ""
		}
	}
	return rows, nil
}

// leaveBalanceWindow resolves leave_year and an optional as_of inside it; the default is the year's last day.
func leaveBalanceWindow(c *gin.Context) (int, string, bool) {
	year, err := strconv.Atoi(c.Query("leave_year"))
	asOf := c.Query("as_of")
	if err != nil || year < 2000 || year > 2199 || asOf != "" && !validEffectiveDate(asOf) {
		respond.Error(c, 400, "invalid_request", "Use a leave_year between 2000 and 2199 and an as_of date inside it.")
		return 0, "", false
	}
	return year, asOf, true
}

func resolveAsOf(y leaveYear, asOf string) (string, error) {
	if asOf == "" {
		return y.EndOn, nil
	}
	if asOf < y.StartOn || asOf > y.EndOn {
		return "", errLeaveInvalid
	}
	return asOf, nil
}

func loadLeaveBalances(tx *gorm.DB, tenant string, hospital uuid.UUID, year int, asOf string, rows []staffLeaveBalances) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(rows))
	index := make(map[uuid.UUID]int, len(rows))
	for i := range rows {
		ids[i] = rows[i].StaffID
		index[ids[i]] = i
		rows[i].Balances = []leaveBalance{}
	}
	var sums []struct {
		StaffID uuid.UUID
		Balance leaveBalance `gorm:"embedded"`
	}
	if err := tx.Raw(`SELECT l.staff_id,l.leave_type_id,c.code,c.name,sum(l.half_days)::int AS half_days
 FROM workforce_leave_ledger l JOIN workforce_leave_configs c ON (c.tenant_id,c.hospital_id,c.id)=(l.tenant_id,l.hospital_id,l.leave_type_id)
 WHERE l.tenant_id=? AND l.hospital_id=? AND l.leave_year=? AND l.effective_on<=?::date AND l.staff_id IN ?
 GROUP BY l.staff_id,l.leave_type_id,c.code,c.name ORDER BY c.code`, tenant, hospital, year, asOf, ids).Scan(&sums).Error; err != nil {
		return err
	}
	for _, s := range sums {
		i := index[s.StaffID]
		rows[i].Balances = append(rows[i].Balances, s.Balance)
	}
	return nil
}

const leaveBalanceHeadView = `(SELECT a.tenant_id,a.hospital_id,a.staff_id,a.staff_id AS id,s.employee_code,s.display_name,t.name AS structure_name,a.active,a.created_at
 FROM workforce_staff_leave_assignments a JOIN workforce_staff s ON (s.tenant_id,s.hospital_id,s.id)=(a.tenant_id,a.hospital_id,a.staff_id)
 JOIN workforce_leave_config_revisions t ON (t.tenant_id,t.hospital_id,t.id)=(a.tenant_id,a.hospital_id,a.structure_revision_id)) AS v`

func (h *handlers) listLeaveBalances(c *gin.Context, params pagination.Params) ([]staffLeaveBalances, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	year, asOf, ok := leaveBalanceWindow(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, 400, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	rows := []staffLeaveBalances{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		y, err := loadLeaveYear(tx, p.TenantID, hospital, year, false)
		if err != nil {
			return err
		}
		if asOf, err = resolveAsOf(y, asOf); err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx, params).Table(leaveBalanceHeadView).Select("staff_id,employee_code,display_name,structure_name,active,created_at").Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Find(&rows).Error; err != nil {
			return err
		}
		return loadLeaveBalances(tx, p.TenantID, hospital, year, asOf, rows)
	})
	if err != nil {
		respondLeaveLedgerError(c, err)
		return nil, nil
	}
	return rows, nil
}

// getStaffLeaveBalances also answers for staff without an assignment, whose balance may come from corrections alone.
func (h *handlers) getStaffLeaveBalances(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staffID, err := uuid.Parse(c.Param("id"))
	if err != nil || staffID == uuid.Nil {
		respond.NotFound(c, "staff")
		return
	}
	year, asOf, ok := leaveBalanceWindow(c)
	if !ok {
		return
	}
	var result staffLeaveBalances
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		y, err := loadLeaveYear(tx, p.TenantID, hospital, year, false)
		if err != nil {
			return err
		}
		if asOf, err = resolveAsOf(y, asOf); err != nil {
			return err
		}
		if err := tx.Raw(`SELECT s.id AS staff_id,s.employee_code,s.display_name,COALESCE(t.name,'') AS structure_name,COALESCE(a.active,false) AS active,COALESCE(a.created_at,s.created_at) AS created_at
 FROM workforce_staff s LEFT JOIN workforce_staff_leave_assignments a ON (a.tenant_id,a.hospital_id,a.staff_id)=(s.tenant_id,s.hospital_id,s.id)
 LEFT JOIN workforce_leave_config_revisions t ON (t.tenant_id,t.hospital_id,t.id)=(a.tenant_id,a.hospital_id,a.structure_revision_id)
 WHERE s.tenant_id=? AND s.hospital_id=? AND s.id=?`, p.TenantID, hospital, staffID).Take(&result).Error; err != nil {
			return err
		}
		rows := []staffLeaveBalances{result}
		if err := loadLeaveBalances(tx, p.TenantID, hospital, year, asOf, rows); err != nil {
			return err
		}
		result = rows[0]
		return nil
	})
	if err != nil {
		respondLeaveLedgerError(c, err)
		return
	}
	respond.OK(c, result)
}

func respondLeaveLedgerError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errLeaveInvalid):
		respond.Error(c, 400, "invalid_request", "Review the request: changes must be reviewed, dates valid and inside the leave year, and corrections need a non-zero amount and a reason of at least 10 characters.")
	case errors.Is(err, errPolicyEffective):
		respond.Error(c, 400, "invalid_request", "The effective date cannot be earlier than the current assignment's effective date.")
	case errors.Is(err, errPolicyStaffInactive):
		respond.Error(c, 400, "invalid_request", "Only active staff can receive an active leave assignment.")
	case errors.Is(err, errLeaveReference):
		respond.Error(c, 400, "invalid_reference", "Select a current, active leave structure or balance-tracked leave type effective on or before the date.")
	case errors.Is(err, errLeaveVersion):
		respond.Conflict(c, "The leave assignment changed since you opened it. Reload before saving.")
	case errors.Is(err, errLeaveSettingsFixed):
		respond.Conflict(c, "The leave year start month is already set and cannot change.")
	case errors.Is(err, errLeaveSettingsMissing):
		respond.Conflict(c, "Set the leave year start month before opening a leave year.")
	case errors.Is(err, errLeaveSequence):
		respond.Conflict(c, "Post the next unposted month of an open leave year.")
	case errors.Is(err, errLeaveIncomplete):
		respond.Conflict(c, "Post all twelve months before closing the leave year.")
	case errors.Is(err, errLeaveNextYear):
		respond.Conflict(c, "Open the next leave year before closing this one, so carried balances have somewhere to land.")
	case errors.Is(err, errLeaveYearClosed):
		respond.Conflict(c, "The leave year is not open, or the date falls outside it.")
	case errors.Is(err, errLeaveEarly):
		respond.Conflict(c, "Open at most next year, post a month once it starts and close a year after it ends.")
	case errors.Is(err, errLeaveFrozen):
		respond.Conflict(c, "Assignment changes must start after the last posted month and outside closed leave years.")
	case errors.Is(err, errLeaveCorrectionConflict):
		respond.Conflict(c, "This correction id was already used with different values.")
	default:
		respondError(c, err)
	}
}

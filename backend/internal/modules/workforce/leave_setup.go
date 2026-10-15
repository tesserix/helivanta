package workforce

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func (leaveConfig) TableName() string                 { return "workforce_leave_configs" }
func (c leaveConfig) PageKey() (time.Time, uuid.UUID) { return c.CreatedAt, c.ID }

var errLeaveVersion = errors.New("leave configuration version conflict")
var errLeaveReference = errors.New("leave configuration reference is unavailable")
var errLeaveInvalid = errors.New("leave configuration is invalid")

const leaveColumns = "id,revision_id,kind,code,name,description,active,effective_on::text AS effective_on,calendar_year,reviewed_by_subject,version,created_at,updated_at"
const leaveRevisionColumns = "id,config_id,id AS revision_id,kind,code,name,description,active,effective_on::text AS effective_on,calendar_year,reviewed_by_subject,version,created_at,created_at AS updated_at"

func loadLeaveParts(tx *gorm.DB, tenant string, hospital uuid.UUID, rows []leaveConfig) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(rows))
	index := make(map[uuid.UUID]int, len(rows))
	for i := range rows {
		ids[i] = rows[i].RevisionID
		index[ids[i]] = i
		rows[i].Holidays = []leaveHoliday{}
		rows[i].Entries = []leaveStructureEntry{}
	}
	var rules []struct {
		RevisionID uuid.UUID
		Rules      leaveTypeRules `gorm:"embedded"`
	}
	if err := tx.Table("workforce_leave_type_rules").Select("revision_id,paid,balance_tracked,half_day_allowed,short_leave_minutes,clubbable").Where("tenant_id=? AND hospital_id=? AND revision_id IN ?", tenant, hospital, ids).Find(&rules).Error; err != nil {
		return err
	}
	for _, r := range rules {
		rule := r.Rules
		rows[index[r.RevisionID]].Rules = &rule
	}
	var holidays []struct {
		RevisionID uuid.UUID
		Holiday    leaveHoliday `gorm:"embedded"`
	}
	if err := tx.Table("workforce_leave_holidays").Select("revision_id,holiday_date::text AS holiday_date,name,category").Where("tenant_id=? AND hospital_id=? AND revision_id IN ?", tenant, hospital, ids).Order("holiday_date").Find(&holidays).Error; err != nil {
		return err
	}
	for _, h := range holidays {
		i := index[h.RevisionID]
		rows[i].Holidays = append(rows[i].Holidays, h.Holiday)
	}
	var entries []struct {
		RevisionID uuid.UUID
		Entry      leaveStructureEntry `gorm:"embedded"`
	}
	if err := tx.Raw(`SELECT e.revision_id,e.leave_type_revision_id,t.config_id AS leave_type_id,t.code,t.name,r.paid,r.balance_tracked,e.annual_half_days,e.accrual,e.carry_forward_half_days,e.proration,e.probation_eligible
 FROM workforce_leave_structure_entries e JOIN workforce_leave_config_revisions t ON (t.tenant_id,t.hospital_id,t.id)=(e.tenant_id,e.hospital_id,e.leave_type_revision_id)
 JOIN workforce_leave_type_rules r ON (r.tenant_id,r.hospital_id,r.revision_id)=(t.tenant_id,t.hospital_id,t.id)
 WHERE e.tenant_id=? AND e.hospital_id=? AND e.revision_id IN ? ORDER BY e.position`, tenant, hospital, ids).Scan(&entries).Error; err != nil {
		return err
	}
	for _, e := range entries {
		i := index[e.RevisionID]
		rows[i].Entries = append(rows[i].Entries, e.Entry)
	}
	return nil
}

func (h *handlers) listLeaveConfigs(c *gin.Context, params pagination.Params) ([]leaveConfig, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	kind := c.Query("kind")
	if params.Limit > 100 || kind != "" && !contains(kind, leaveConfigKinds...) {
		respond.Error(c, 400, "invalid_request", "Use a supported kind and a page limit no greater than 100.")
		return nil, nil
	}
	rows := []leaveConfig{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		q := tenantdb.ApplyKeyset(tx, params).Select(leaveColumns).Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital)
		if kind != "" {
			q = q.Where("kind=?", kind)
		}
		if err := q.Find(&rows).Error; err != nil {
			return err
		}
		return loadLeaveParts(tx, p.TenantID, hospital, rows)
	})
	if err != nil {
		respondLeaveError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) leaveConfigHistory(c *gin.Context, params pagination.Params) ([]leaveConfig, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "leave configuration")
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, 400, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	rows := []leaveConfig{}
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var head leaveConfig
		if err := tx.Select(leaveColumns).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, id).First(&head).Error; err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx, params).Table("workforce_leave_config_revisions").Select(leaveRevisionColumns).Where("tenant_id=? AND hospital_id=? AND config_id=?", p.TenantID, hospital, id).Find(&rows).Error; err != nil {
			return err
		}
		return loadLeaveParts(tx, p.TenantID, hospital, rows)
	})
	if err != nil {
		respondLeaveError(c, err)
		return nil, nil
	}
	return rows, nil
}

func leaveInputFromRecord(old leaveConfig) leaveConfigInput {
	reviewed := true
	in := leaveConfigInput{ExpectedVersion: &old.Version, Kind: old.Kind, Code: old.Code, Name: old.Name, Description: old.Description, Active: &old.Active, EffectiveOn: old.EffectiveOn, Reviewed: &reviewed, CalendarYear: old.CalendarYear, Rules: old.Rules, Holidays: []leaveHolidayInput{}, Entries: []leaveStructureEntryInput{}}
	for _, h := range old.Holidays {
		in.Holidays = append(in.Holidays, leaveHolidayInput(h))
	}
	for _, e := range old.Entries {
		in.Entries = append(in.Entries, leaveStructureEntryInput{LeaveTypeRevisionID: e.LeaveTypeRevisionID, AnnualHalfDays: &e.AnnualHalfDays, Accrual: e.Accrual, CarryForwardHalfDays: &e.CarryForwardHalfDays, Proration: e.Proration, ProbationEligible: &e.ProbationEligible})
	}
	return in
}

func normalizeLeaveConfig(in *leaveConfigInput) error {
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	in.Description = strings.TrimSpace(in.Description)
	if !validEffectiveDate(in.EffectiveOn) || in.Name == "" || !employeeCode.MatchString(in.Code) || strings.ContainsRune(in.Name, 0) || strings.ContainsRune(in.Description, 0) || !*in.Reviewed {
		return errLeaveInvalid
	}
	if in.Holidays == nil {
		in.Holidays = []leaveHolidayInput{}
	}
	if in.Entries == nil {
		in.Entries = []leaveStructureEntryInput{}
	}
	switch in.Kind {
	case "leave_type":
		if in.Rules == nil || in.CalendarYear != nil || len(in.Holidays) != 0 || len(in.Entries) != 0 || *in.Rules.ShortLeaveMinutes%15 != 0 {
			return errLeaveInvalid
		}
	case "holiday_calendar":
		if in.Rules != nil || in.CalendarYear == nil || len(in.Entries) != 0 {
			return errLeaveInvalid
		}
		year := *in.CalendarYear
		// One reviewed revision must cover the whole year, so it takes effect no later than 1 January.
		if in.EffectiveOn > time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC).Format("2006-01-02") {
			return errLeaveInvalid
		}
		seen := map[string]bool{}
		for i := range in.Holidays {
			d := &in.Holidays[i]
			d.Name = strings.Join(strings.Fields(d.Name), " ")
			date, err := time.Parse("2006-01-02", d.HolidayDate)
			if err != nil || date.Year() != year || seen[d.HolidayDate] || d.Name == "" || strings.ContainsRune(d.Name, 0) {
				return errLeaveInvalid
			}
			seen[d.HolidayDate] = true
		}
		sortHolidays(in.Holidays)
	case "structure":
		if in.Rules != nil || in.CalendarYear != nil || len(in.Holidays) != 0 || len(in.Entries) == 0 {
			return errLeaveInvalid
		}
		seen := map[uuid.UUID]bool{}
		for _, e := range in.Entries {
			if e.LeaveTypeRevisionID == uuid.Nil || seen[e.LeaveTypeRevisionID] {
				return errLeaveInvalid
			}
			seen[e.LeaveTypeRevisionID] = true
		}
	default:
		return errLeaveInvalid
	}
	return nil
}

func sortHolidays(rows []leaveHolidayInput) {
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].HolidayDate < rows[j-1].HolidayDate; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
}

// A structure may keep a type revision it already pinned; new references must be current, active and already effective.
func validateLeaveReferences(tx *gorm.DB, tenant string, hospital uuid.UUID, in leaveConfigInput, old *leaveConfig) error {
	if in.Kind != "structure" {
		return nil
	}
	previous := map[uuid.UUID]bool{}
	if old != nil && old.EffectiveOn == in.EffectiveOn && (old.Active || !*in.Active) {
		for _, e := range old.Entries {
			previous[e.LeaveTypeRevisionID] = true
		}
	}
	ids := map[uuid.UUID]bool{}
	for _, e := range in.Entries {
		ids[e.LeaveTypeRevisionID] = true
	}
	var types []struct {
		RevisionID     uuid.UUID
		ConfigID       uuid.UUID
		Current        bool
		EffectiveOn    string
		BalanceTracked bool
	}
	if err := tx.Raw(`SELECT r.id AS revision_id,r.config_id,(c.active AND c.revision_id=r.id) AS current,r.effective_on::text AS effective_on,t.balance_tracked
 FROM workforce_leave_config_revisions r JOIN workforce_leave_configs c ON (c.tenant_id,c.hospital_id,c.id)=(r.tenant_id,r.hospital_id,r.config_id)
 JOIN workforce_leave_type_rules t ON (t.tenant_id,t.hospital_id,t.revision_id)=(r.tenant_id,r.hospital_id,r.id)
 WHERE r.tenant_id=? AND r.hospital_id=? AND r.kind='leave_type' AND r.id IN ? ORDER BY c.id FOR SHARE OF c`, tenant, hospital, sortedRevisionIDs(ids)).Scan(&types).Error; err != nil {
		return err
	}
	byID := map[uuid.UUID]int{}
	for i, t := range types {
		byID[t.RevisionID] = i
	}
	configs := map[uuid.UUID]bool{}
	for _, e := range in.Entries {
		i, exists := byID[e.LeaveTypeRevisionID]
		if !exists {
			return errLeaveReference
		}
		t := types[i]
		if !t.Current && !previous[t.RevisionID] || t.EffectiveOn > in.EffectiveOn || configs[t.ConfigID] {
			return errLeaveReference
		}
		configs[t.ConfigID] = true
		if !t.BalanceTracked && (*e.AnnualHalfDays != 0 || *e.CarryForwardHalfDays != 0) {
			return errLeaveInvalid
		}
	}
	return nil
}

func insertLeaveChildren(tx *gorm.DB, tenant string, hospital, revision uuid.UUID, in leaveConfigInput) error {
	switch in.Kind {
	case "leave_type":
		r := in.Rules
		return tx.Exec(`INSERT INTO workforce_leave_type_rules(tenant_id,hospital_id,revision_id,paid,balance_tracked,half_day_allowed,short_leave_minutes,clubbable) VALUES(?,?,?,?,?,?,?,?)`, tenant, hospital, revision, *r.Paid, *r.BalanceTracked, *r.HalfDayAllowed, *r.ShortLeaveMinutes, *r.Clubbable).Error
	case "holiday_calendar":
		for _, d := range in.Holidays {
			if err := tx.Exec(`INSERT INTO workforce_leave_holidays(tenant_id,hospital_id,revision_id,holiday_date,name,category) VALUES(?,?,?,?,?,?)`, tenant, hospital, revision, d.HolidayDate, d.Name, d.Category).Error; err != nil {
				return err
			}
		}
	default:
		for i, e := range in.Entries {
			if err := tx.Exec(`INSERT INTO workforce_leave_structure_entries(tenant_id,hospital_id,revision_id,position,leave_type_revision_id,annual_half_days,accrual,carry_forward_half_days,proration,probation_eligible) VALUES(?,?,?,?,?,?,?,?,?,?)`, tenant, hospital, revision, i, e.LeaveTypeRevisionID, *e.AnnualHalfDays, e.Accrual, *e.CarryForwardHalfDays, e.Proration, *e.ProbationEligible).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func loadLeaveConfig(tx *gorm.DB, tenant string, hospital, id uuid.UUID, lock bool) (leaveConfig, error) {
	var row leaveConfig
	q := tx.Select(leaveColumns)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.Where("tenant_id=? AND hospital_id=? AND id=?", tenant, hospital, id).First(&row).Error; err != nil {
		return row, err
	}
	parts := []leaveConfig{row}
	err := loadLeaveParts(tx, tenant, hospital, parts)
	return parts[0], err
}

func (h *handlers) putLeaveConfig(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "leave configuration")
		return
	}
	var in leaveConfigInput
	if !decodeSized(c, &in, 32768) {
		return
	}
	if err := normalizeLeaveConfig(&in); err != nil {
		respondLeaveError(c, err)
		return
	}
	var result leaveConfig
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_leave", "leave-config:"+id.String()); err != nil {
			return err
		}
		old, err := loadLeaveConfig(tx, p.TenantID, hospital, id, true)
		var previous *leaveConfig
		version := int64(1)
		switch {
		case err == nil:
			if old.Kind != in.Kind || old.Code != in.Code || !reflect.DeepEqual(old.CalendarYear, in.CalendarYear) {
				return errLeaveVersion
			}
			existing := leaveInputFromRecord(old)
			existing.ExpectedVersion = in.ExpectedVersion
			if reflect.DeepEqual(existing, in) {
				result = old
				return nil
			}
			if old.Version != *in.ExpectedVersion {
				return errLeaveVersion
			}
			if in.EffectiveOn < old.EffectiveOn {
				return errPolicyEffective
			}
			previous = &old
			version = old.Version + 1
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		case *in.ExpectedVersion != 0:
			return gorm.ErrRecordNotFound
		}
		if err := validateLeaveReferences(tx, p.TenantID, hospital, in, previous); err != nil {
			return err
		}
		revision := uuid.New()
		if err := tx.Exec(`INSERT INTO workforce_leave_configs(tenant_id,hospital_id,id,kind,code,name,description,active,calendar_year,effective_on,reviewed_by_subject,version,revision_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(tenant_id,hospital_id,id) DO UPDATE SET name=EXCLUDED.name,description=EXCLUDED.description,active=EXCLUDED.active,effective_on=EXCLUDED.effective_on,reviewed_by_subject=EXCLUDED.reviewed_by_subject,version=EXCLUDED.version,revision_id=EXCLUDED.revision_id,updated_at=clock_timestamp()`, p.TenantID, hospital, id, in.Kind, in.Code, in.Name, in.Description, *in.Active, in.CalendarYear, in.EffectiveOn, p.Subject, version, revision).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_leave_config_revisions(tenant_id,hospital_id,id,config_id,kind,code,name,description,active,calendar_year,effective_on,reviewed_by_subject,version) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.TenantID, hospital, revision, id, in.Kind, in.Code, in.Name, in.Description, *in.Active, in.CalendarYear, in.EffectiveOn, p.Subject, version).Error; err != nil {
			return err
		}
		if err := insertLeaveChildren(tx, p.TenantID, hospital, revision, in); err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.leave_config.changed',?,jsonb_build_object('kind',?::text,'version',?::bigint,'previous_version',?::bigint))`, p.TenantID, hospital, p.Subject, id, in.Kind, version, version-1).Error; err != nil {
			return err
		}
		data, err := json.Marshal(workforcecontract.LeaveConfigChangedData{HospitalID: hospital.String(), ConfigID: id.String(), RevisionID: revision.String(), Kind: in.Kind, Version: version})
		if err != nil {
			return err
		}
		if err := h.bus.Publish(tx, LeaveConfigChanged, events.Event{Type: LeaveConfigChanged, Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
			return err
		}
		result, err = loadLeaveConfig(tx, p.TenantID, hospital, id, false)
		return err
	})
	if err != nil {
		respondLeaveError(c, err)
		return
	}
	respond.OK(c, result)
}

func respondLeaveError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, errLeaveInvalid):
		respond.Error(c, 400, "invalid_request", "Review the effective date, rules, holidays and entries for this kind. Holidays must be unique dates in the calendar year, and untracked leave types carry no allowance.")
	case errors.Is(err, errPolicyEffective):
		respond.Error(c, 400, "invalid_request", "The effective date cannot be earlier than the current revision's effective date.")
	case errors.Is(err, errLeaveReference):
		respond.Error(c, 400, "invalid_reference", "Select current, active leave types effective on or before the structure, each once.")
	case errors.Is(err, errLeaveVersion):
		respond.Conflict(c, "Configuration changed since you opened it, or its code, kind or year was changed. Reload before saving.")
	case errors.As(err, &pg) && pg.Code == "23505":
		respond.Conflict(c, "Code, name or calendar year already exists in this hospital and category.")
	default:
		respondError(c, err)
	}
}

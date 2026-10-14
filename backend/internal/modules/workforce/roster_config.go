package workforce

import (
	"encoding/json"
	"errors"
	"reflect"
	"sort"
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

func (rosterConfig) TableName() string                 { return "workforce_roster_configs" }
func (c rosterConfig) PageKey() (time.Time, uuid.UUID) { return c.CreatedAt, c.ID }

var errRosterVersion = errors.New("roster configuration version conflict")
var errRosterReference = errors.New("roster configuration reference is unavailable")
var errRosterInvalid = errors.New("roster configuration is invalid")

const configColumns = "id,revision_id,kind,code,name,description,active,effective_on::text AS effective_on,reviewed_by_subject,version,created_at,updated_at"
const configRevisionColumns = "id,config_id,id AS revision_id,kind,code,name,description,active,effective_on::text AS effective_on,reviewed_by_subject,version,created_at,created_at AS updated_at"

func loadConfigParts(tx *gorm.DB, tenant string, hospital uuid.UUID, rows []rosterConfig) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(rows))
	index := make(map[uuid.UUID]int, len(rows))
	for i := range rows {
		ids[i] = rows[i].RevisionID
		index[ids[i]] = i
		rows[i].Shifts = []rosterShift{}
		rows[i].Days = []rosterDay{}
	}
	var days []struct {
		RevisionID uuid.UUID
		Weekday    int
		WeeklyOff  bool
	}
	if err := tx.Table("workforce_roster_config_days").Select("revision_id,weekday,weekly_off").Where("tenant_id=? AND hospital_id=? AND revision_id IN ?", tenant, hospital, ids).Order("weekday").Find(&days).Error; err != nil {
		return err
	}
	for _, d := range days {
		i := index[d.RevisionID]
		rows[i].Days = append(rows[i].Days, rosterDay{Weekday: d.Weekday, WeeklyOff: d.WeeklyOff, Allocations: []rosterAllocation{}})
	}
	var members []struct {
		RevisionID      uuid.UUID
		Position        int
		Weekday         *int
		ShiftRevisionID uuid.UUID
		ShiftID         uuid.UUID
		Code            string
		Name            string
		StartTime       string
		EndTime         string
		DurationMinutes int
		AreaRevisionID  *uuid.UUID
		AreaID          uuid.UUID
		AreaCode        string
		AreaName        string
	}
	if err := tx.Raw(`SELECT m.revision_id,m.position,m.weekday,m.shift_revision_id,s.shift_id,s.code,s.name,s.start_time,s.end_time,s.duration_minutes,m.area_revision_id,a.config_id AS area_id,COALESCE(a.code,'') AS area_code,COALESCE(a.name,'') AS area_name
 FROM workforce_roster_config_members m JOIN workforce_shift_revisions s ON (s.tenant_id,s.hospital_id,s.id)=(m.tenant_id,m.hospital_id,m.shift_revision_id)
 LEFT JOIN workforce_roster_config_revisions a ON (a.tenant_id,a.hospital_id,a.id)=(m.tenant_id,m.hospital_id,m.area_revision_id)
 WHERE m.tenant_id=? AND m.hospital_id=? AND m.revision_id IN ? ORDER BY m.position`, tenant, hospital, ids).Scan(&members).Error; err != nil {
		return err
	}
	for _, m := range members {
		i := index[m.RevisionID]
		shift := rosterShift{RevisionID: m.ShiftRevisionID, ShiftID: m.ShiftID, Code: m.Code, Name: m.Name, StartTime: m.StartTime, EndTime: m.EndTime, DurationMinutes: m.DurationMinutes}
		if m.Weekday == nil {
			rows[i].Shifts = append(rows[i].Shifts, shift)
			continue
		}
		if m.AreaRevisionID == nil {
			return errRosterReference
		}
		for j := range rows[i].Days {
			if rows[i].Days[j].Weekday == *m.Weekday {
				rows[i].Days[j].Allocations = append(rows[i].Days[j].Allocations, rosterAllocation{ShiftRevisionID: m.ShiftRevisionID, AreaRevisionID: *m.AreaRevisionID, AreaID: m.AreaID, AreaCode: m.AreaCode, AreaName: m.AreaName, Shift: shift})
			}
		}
	}
	return nil
}
func (h *handlers) listRosterConfigs(c *gin.Context, params pagination.Params) ([]rosterConfig, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	kind := c.Query("kind")
	if params.Limit > 100 || kind != "" && !contains(kind, rosterConfigKinds...) {
		respond.Error(c, 400, "invalid_request", "Use a supported kind and a page limit no greater than 100.")
		return nil, nil
	}
	rows := []rosterConfig{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		q := tenantdb.ApplyKeyset(tx, params).Select(configColumns).Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital)
		if kind != "" {
			q = q.Where("kind=?", kind)
		}
		if err := q.Find(&rows).Error; err != nil {
			return err
		}
		return loadConfigParts(tx, p.TenantID, hospital, rows)
	})
	if err != nil {
		respondRosterConfigError(c, err)
		return nil, nil
	}
	return rows, nil
}
func (h *handlers) rosterConfigHistory(c *gin.Context, params pagination.Params) ([]rosterConfig, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "roster configuration")
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, 400, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	rows := []rosterConfig{}
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var head rosterConfig
		if err := tx.Select(configColumns).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, id).First(&head).Error; err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx, params).Table("workforce_roster_config_revisions").Select(configRevisionColumns).Where("tenant_id=? AND hospital_id=? AND config_id=?", p.TenantID, hospital, id).Find(&rows).Error; err != nil {
			return err
		}
		return loadConfigParts(tx, p.TenantID, hospital, rows)
	})
	if err != nil {
		respondRosterConfigError(c, err)
		return nil, nil
	}
	return rows, nil
}
func configInputFromRecord(old rosterConfig) rosterConfigInput {
	reviewed := true
	in := rosterConfigInput{ExpectedVersion: &old.Version, Kind: old.Kind, Code: old.Code, Name: old.Name, Description: old.Description, Active: &old.Active, EffectiveOn: old.EffectiveOn, Reviewed: &reviewed, ShiftRevisionIDs: []uuid.UUID{}, Days: []rosterDayInput{}}
	for _, s := range old.Shifts {
		in.ShiftRevisionIDs = append(in.ShiftRevisionIDs, s.RevisionID)
	}
	for _, d := range old.Days {
		day := rosterDayInput{Weekday: &d.Weekday, WeeklyOff: &d.WeeklyOff, Allocations: []rosterAllocationInput{}}
		for _, a := range d.Allocations {
			day.Allocations = append(day.Allocations, rosterAllocationInput{ShiftRevisionID: a.ShiftRevisionID, AreaRevisionID: a.AreaRevisionID})
		}
		in.Days = append(in.Days, day)
	}
	return in
}
func normalizeConfig(in *rosterConfigInput) error {
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	in.Description = strings.TrimSpace(in.Description)
	date, err := time.Parse("2006-01-02", in.EffectiveOn)
	if err != nil || date.Year() < 1 || in.Name == "" || !employeeCode.MatchString(in.Code) || strings.ContainsRune(in.Name, 0) || strings.ContainsRune(in.Description, 0) || !*in.Reviewed {
		return errRosterInvalid
	}
	if in.ShiftRevisionIDs == nil {
		in.ShiftRevisionIDs = []uuid.UUID{}
	}
	if in.Days == nil {
		in.Days = []rosterDayInput{}
	}
	if in.Kind != "weekly_template" {
		if len(in.ShiftRevisionIDs) == 0 || len(in.Days) != 0 {
			return errRosterInvalid
		}
		sort.Slice(in.ShiftRevisionIDs, func(i, j int) bool { return in.ShiftRevisionIDs[i].String() < in.ShiftRevisionIDs[j].String() })
		for i, id := range in.ShiftRevisionIDs {
			if id == uuid.Nil || (i > 0 && id == in.ShiftRevisionIDs[i-1]) {
				return errRosterInvalid
			}
		}
		return nil
	}
	if len(in.ShiftRevisionIDs) != 0 || len(in.Days) != 7 {
		return errRosterInvalid
	}
	seen := [7]bool{}
	allocations := 0
	for i := range in.Days {
		d := &in.Days[i]
		day := *d.Weekday
		if seen[day] || (*d.WeeklyOff && len(d.Allocations) > 0) || (!*d.WeeklyOff && len(d.Allocations) == 0) {
			return errRosterInvalid
		}
		seen[day] = true
		if d.Allocations == nil {
			d.Allocations = []rosterAllocationInput{}
		}
		ids := map[uuid.UUID]bool{}
		for _, a := range d.Allocations {
			if a.ShiftRevisionID == uuid.Nil || a.AreaRevisionID == uuid.Nil || ids[a.ShiftRevisionID] {
				return errRosterInvalid
			}
			ids[a.ShiftRevisionID] = true
			allocations++
		}
	}
	if allocations == 0 {
		return errRosterInvalid
	}
	sort.Slice(in.Days, func(i, j int) bool { return *in.Days[i].Weekday < *in.Days[j].Weekday })
	return nil
}

type rosterSpan struct{ start, end int }

func spansOverlap(a, b rosterSpan) bool { return a.start < b.end && b.start < a.end }
func validateTemplateIntervals(in rosterConfigInput, shifts map[uuid.UUID]rosterSpan) error {
	spans := []rosterSpan{}
	off := []rosterSpan{}
	for _, d := range in.Days {
		day := *d.Weekday * 1440
		if *d.WeeklyOff {
			off = append(off, rosterSpan{start: day, end: day + 1440})
		}
		for _, a := range d.Allocations {
			s := shifts[a.ShiftRevisionID]
			spans = append(spans, rosterSpan{start: day + s.start, end: day + s.end})
		}
	}
	for i, a := range spans {
		for _, delta := range []int{-10080, 0, 10080} {
			for _, b := range spans[i+1:] {
				if spansOverlap(a, rosterSpan{start: b.start + delta, end: b.end + delta}) {
					return errRosterInvalid
				}
			}
			for _, b := range off {
				if spansOverlap(a, rosterSpan{start: b.start + delta, end: b.end + delta}) {
					return errRosterInvalid
				}
			}
		}
	}
	return nil
}
func sortedRevisionIDs(ids map[uuid.UUID]bool) []uuid.UUID {
	rows := make([]uuid.UUID, 0, len(ids))
	for id := range ids {
		rows = append(rows, id)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].String() < rows[j].String() })
	return rows
}
func validateConfigReferences(tx *gorm.DB, tenant string, hospital uuid.UUID, in rosterConfigInput, old *rosterConfig) error {
	previous := map[uuid.UUID]bool{}
	previousPairs := map[[2]uuid.UUID]bool{}
	if old != nil && old.EffectiveOn == in.EffectiveOn && (old.Active || !*in.Active) {
		for _, s := range old.Shifts {
			previous[s.RevisionID] = true
		}
		for _, d := range old.Days {
			for _, a := range d.Allocations {
				previousPairs[[2]uuid.UUID{a.ShiftRevisionID, a.AreaRevisionID}] = true
			}
		}
	}
	shiftIDs := map[uuid.UUID]bool{}
	areaIDs := map[uuid.UUID]bool{}
	for _, id := range in.ShiftRevisionIDs {
		shiftIDs[id] = true
	}
	for _, d := range in.Days {
		for _, a := range d.Allocations {
			shiftIDs[a.ShiftRevisionID] = true
			areaIDs[a.AreaRevisionID] = true
		}
	}
	var available []struct {
		RevisionID      uuid.UUID
		Current         bool
		StartTime       string
		DurationMinutes int
	}
	if err := tx.Raw(`SELECT r.id AS revision_id,(s.active AND s.revision_id=r.id) AS current,r.start_time,r.duration_minutes
 FROM workforce_shift_revisions r JOIN workforce_shifts s ON (s.tenant_id,s.hospital_id,s.id)=(r.tenant_id,r.hospital_id,r.shift_id)
 WHERE r.tenant_id=? AND r.hospital_id=? AND r.id IN ? ORDER BY s.id FOR SHARE OF s`, tenant, hospital, sortedRevisionIDs(shiftIDs)).Scan(&available).Error; err != nil {
		return err
	}
	current := map[uuid.UUID]bool{}
	spans := map[uuid.UUID]rosterSpan{}
	for _, s := range available {
		current[s.RevisionID] = s.Current
		clock, err := time.Parse("15:04", s.StartTime)
		if err != nil {
			return errRosterReference
		}
		start := clock.Hour()*60 + clock.Minute()
		spans[s.RevisionID] = rosterSpan{start: start, end: start + s.DurationMinutes}
	}
	if in.Kind != "weekly_template" {
		for _, id := range in.ShiftRevisionIDs {
			fresh, exists := current[id]
			if !exists || !fresh && !previous[id] {
				return errRosterReference
			}
		}
		return nil
	}
	var areas []struct {
		RevisionID  uuid.UUID
		Current     bool
		EffectiveOn string
	}
	if err := tx.Raw(`SELECT r.id AS revision_id,(a.active AND a.revision_id=r.id) AS current,r.effective_on::text AS effective_on
 FROM workforce_roster_config_revisions r JOIN workforce_roster_configs a ON (a.tenant_id,a.hospital_id,a.id)=(r.tenant_id,r.hospital_id,r.config_id)
 WHERE r.tenant_id=? AND r.hospital_id=? AND r.kind='area' AND r.id IN ? ORDER BY a.id FOR SHARE OF a`, tenant, hospital, sortedRevisionIDs(areaIDs)).Scan(&areas).Error; err != nil {
		return err
	}
	currentAreas := map[uuid.UUID]bool{}
	areaDates := map[uuid.UUID]string{}
	for _, a := range areas {
		currentAreas[a.RevisionID] = a.Current
		areaDates[a.RevisionID] = a.EffectiveOn
	}
	var members []struct {
		RevisionID      uuid.UUID
		ShiftRevisionID uuid.UUID
	}
	if err := tx.Table("workforce_roster_config_members").Select("revision_id,shift_revision_id").Where("tenant_id=? AND hospital_id=? AND revision_id IN ?", tenant, hospital, sortedRevisionIDs(areaIDs)).Find(&members).Error; err != nil {
		return err
	}
	membership := map[[2]uuid.UUID]bool{}
	for _, m := range members {
		membership[[2]uuid.UUID{m.ShiftRevisionID, m.RevisionID}] = true
	}
	for _, d := range in.Days {
		for _, a := range d.Allocations {
			pair := [2]uuid.UUID{a.ShiftRevisionID, a.AreaRevisionID}
			fresh, exists := current[a.ShiftRevisionID]
			areaFresh, areaExists := currentAreas[a.AreaRevisionID]
			if !exists || !areaExists || !membership[pair] || areaDates[a.AreaRevisionID] > in.EffectiveOn || (!fresh || !areaFresh) && !previousPairs[pair] {
				return errRosterReference
			}
		}
	}
	return validateTemplateIntervals(in, spans)
}
func insertConfigChildren(tx *gorm.DB, tenant string, hospital, revision uuid.UUID, in rosterConfigInput) error {
	type member struct {
		TenantID        string
		HospitalID      uuid.UUID
		RevisionID      uuid.UUID
		Kind            string
		Position        int
		Weekday         *int
		ShiftRevisionID uuid.UUID
		AreaRevisionID  *uuid.UUID
	}
	rows := make([]member, 0, len(in.ShiftRevisionIDs)+14)
	for i, id := range in.ShiftRevisionIDs {
		rows = append(rows, member{TenantID: tenant, HospitalID: hospital, RevisionID: revision, Kind: in.Kind, Position: i, ShiftRevisionID: id})
	}
	if in.Kind == "weekly_template" {
		type day struct {
			TenantID   string
			HospitalID uuid.UUID
			RevisionID uuid.UUID
			Weekday    int
			WeeklyOff  bool
		}
		days := make([]day, 0, 7)
		for _, d := range in.Days {
			days = append(days, day{TenantID: tenant, HospitalID: hospital, RevisionID: revision, Weekday: *d.Weekday, WeeklyOff: *d.WeeklyOff})
			for i, a := range d.Allocations {
				area := a.AreaRevisionID
				rows = append(rows, member{TenantID: tenant, HospitalID: hospital, RevisionID: revision, Kind: in.Kind, Position: *d.Weekday*2 + i, Weekday: d.Weekday, ShiftRevisionID: a.ShiftRevisionID, AreaRevisionID: &area})
			}
		}
		if err := tx.Table("workforce_roster_config_days").Create(&days).Error; err != nil {
			return err
		}
	}
	return tx.Table("workforce_roster_config_members").Create(&rows).Error
}
func (h *handlers) putRosterConfig(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "roster configuration")
		return
	}
	var in rosterConfigInput
	if !decodeSized(c, &in, 16384) {
		return
	}
	if err := normalizeConfig(&in); err != nil {
		respondRosterConfigError(c, err)
		return
	}
	var result rosterConfig
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		var grant struct{ Level string }
		if err := tx.Raw(`SELECT level FROM workforce_hospital_access WHERE tenant_id=? AND hospital_id=? AND subject=? FOR SHARE`, p.TenantID, hospital, p.Subject).Scan(&grant).Error; err != nil {
			return err
		}
		if grant.Level != "manager" {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Exec(`SELECT set_config('app.workforce_roster','authorized',true),pg_advisory_xact_lock(hashtextextended(?,0))`, p.TenantID+":"+hospital.String()+":roster-config:"+id.String()).Error; err != nil {
			return err
		}
		var old rosterConfig
		var previous *rosterConfig
		err := tx.Select(configColumns).Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, id).First(&old).Error
		version := int64(1)
		if err == nil {
			if old.Kind != in.Kind || old.Code != in.Code {
				return errRosterVersion
			}
			parts := []rosterConfig{old}
			if err := loadConfigParts(tx, p.TenantID, hospital, parts); err != nil {
				return err
			}
			old = parts[0]
			previous = &old
			existing := configInputFromRecord(old)
			existing.ExpectedVersion = in.ExpectedVersion
			if reflect.DeepEqual(existing, in) {
				result = old
				return nil
			}
			if old.Version != *in.ExpectedVersion {
				return errRosterVersion
			}
			version = old.Version + 1
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		} else if *in.ExpectedVersion != 0 {
			return gorm.ErrRecordNotFound
		}
		if err := validateConfigReferences(tx, p.TenantID, hospital, in, previous); err != nil {
			return err
		}
		revision := uuid.New()
		if err := tx.Exec(`INSERT INTO workforce_roster_configs(tenant_id,hospital_id,id,kind,code,name,description,active,effective_on,reviewed_by_subject,version,revision_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(tenant_id,hospital_id,id) DO UPDATE SET name=EXCLUDED.name,description=EXCLUDED.description,active=EXCLUDED.active,effective_on=EXCLUDED.effective_on,reviewed_by_subject=EXCLUDED.reviewed_by_subject,version=EXCLUDED.version,revision_id=EXCLUDED.revision_id,updated_at=clock_timestamp()`, p.TenantID, hospital, id, in.Kind, in.Code, in.Name, in.Description, *in.Active, in.EffectiveOn, p.Subject, version, revision).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_roster_config_revisions(tenant_id,hospital_id,id,config_id,kind,code,name,description,active,effective_on,reviewed_by_subject,version) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, p.TenantID, hospital, revision, id, in.Kind, in.Code, in.Name, in.Description, *in.Active, in.EffectiveOn, p.Subject, version).Error; err != nil {
			return err
		}
		if err := insertConfigChildren(tx, p.TenantID, hospital, revision, in); err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.roster_config.changed',?,jsonb_build_object('version',?::bigint,'previous_version',?::bigint))`, p.TenantID, hospital, p.Subject, id, version, version-1).Error; err != nil {
			return err
		}
		data, err := json.Marshal(workforcecontract.RosterConfigChangedData{HospitalID: hospital.String(), ConfigID: id.String(), RevisionID: revision.String(), Version: version})
		if err != nil {
			return err
		}
		if err := h.bus.Publish(tx, RosterConfigChanged, events.Event{Type: RosterConfigChanged, Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
			return err
		}
		if err := tx.Select(configColumns).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, id).First(&result).Error; err != nil {
			return err
		}
		parts := []rosterConfig{result}
		if err := loadConfigParts(tx, p.TenantID, hospital, parts); err != nil {
			return err
		}
		result = parts[0]
		return nil
	})
	if err != nil {
		respondRosterConfigError(c, err)
		return
	}
	respond.OK(c, result)
}
func respondRosterConfigError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, errRosterInvalid):
		respond.Error(c, 400, "invalid_request", "Review the effective date, shift selections and all seven weekdays. Working shifts must not overlap or enter a weekly-off day.")
	case errors.Is(err, errRosterReference):
		respond.Error(c, 400, "invalid_reference", "Select available current shift and area revisions for new assignments.")
	case errors.Is(err, errRosterVersion):
		respond.Conflict(c, "Configuration changed since you opened it, or its code/kind was changed. Reload before saving.")
	case errors.As(err, &pg) && pg.Code == "23505":
		respond.Conflict(c, "Code or name already exists in this hospital and category.")
	default:
		respondError(c, err)
	}
}

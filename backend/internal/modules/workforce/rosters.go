package workforce

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
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

const (
	rosterChangeBodyLimit = 192 << 10
	maxPopulatedRows      = 20000
	maxPopulationRows     = 2 * maxPopulatedRows
	allocationInsertChunk = 2000
)

func (r roster) PageKey() (time.Time, uuid.UUID)          { return r.CreatedAt, r.ID }
func (a datedAllocation) PageKey() (time.Time, uuid.UUID) { return a.CreatedAt, a.ID }
func (r rosterRevision) PageKey() (time.Time, uuid.UUID)  { return r.CreatedAt, r.ID }

// rosterProblem is a client-correctable outcome with a stable error code.
type rosterProblem struct {
	status        int
	code, message string
}

func (e *rosterProblem) Error() string { return e.message }

var (
	errRosterInput            = &rosterProblem{400, "invalid_request", "Use a name, a whole number of weeks (1 to 6) starting on the configured week start, and confirm that you reviewed the roster."}
	errRosterWeekStart        = &rosterProblem{400, "invalid_request", "Start the roster on the week start configured in the hospital's roster settings."}
	errRosterSettingsMissing  = &rosterProblem{400, "roster_settings_missing", "Review roster settings effective on or before the roster's first day before creating it."}
	errRosterReviewRequired   = &rosterProblem{400, "invalid_request", "Confirm that you reviewed this change."}
	errRosterReasonRequired   = &rosterProblem{400, "invalid_request", "Give a reason for this change; it is kept in the roster history."}
	errRosterChangeInvalid    = &rosterProblem{400, "invalid_request", "Each change needs unique allocation IDs and dates inside the roster, and roster-wise changes use one area while employee-wise changes use one person."}
	errRosterAllocationRef    = &rosterProblem{400, "invalid_reference", "Select active staff, the current revision of an active area effective on the date, and a shift from that area. Only current allocations can be removed."}
	errRosterAdditionalOrphan = &rosterProblem{400, "invalid_request", "Remove the additional allocations on that date before, or together with, the primary allocation."}
	errRosterAdditionalNeeds  = &rosterProblem{400, "invalid_request", "An additional allocation needs a primary allocation for the same person and date."}
	errPopulationDisabled     = &rosterProblem{400, "population_disabled", "Automatic population is disabled in the roster settings this roster uses."}
	errPopulationTooLarge     = &rosterProblem{400, "population_too_large", "This population would exceed 20,000 allocations; populate selected staff in smaller batches."}
	errRosterExists           = &rosterProblem{409, "roster_exists", "A roster with this ID already exists with a different name or period."}
	errRosterStale            = &rosterProblem{409, "stale_version", "The roster changed since you opened it. Reload before saving."}
	errRosterState            = &rosterProblem{409, "roster_state", "This action is not available in the roster's current status."}
	errRosterChangeReused     = &rosterProblem{409, "change_reused", "This change ID was already used for a different change."}
	errRosterAllocationReused = &rosterProblem{409, "allocation_reused", "One or more allocation IDs were already used. Generate new IDs and retry."}
	errRosterPeriodOverlap    = &rosterProblem{409, "roster_overlap", "Another draft or frozen roster already covers part of this period."}
)

type allocationOverlap struct{ ids []uuid.UUID }

func (allocationOverlap) Error() string { return "roster allocations overlap" }

// span is one staff member's occupied interval; overlap is half-open so back-to-back shifts may touch.
type span struct {
	staff      uuid.UUID
	start, end time.Time
}

func (s span) overlaps(o span) bool {
	return s.staff == o.staff && s.start.Before(o.end) && o.start.Before(s.end)
}

type plannedAllocation struct {
	ID              uuid.UUID `json:"id"`
	StaffID         uuid.UUID `json:"staff_id"`
	RosterDate      string    `json:"roster_date"`
	ShiftRevisionID uuid.UUID `json:"shift_revision_id"`
	AreaRevisionID  uuid.UUID `json:"area_revision_id"`
	StartsAt        time.Time `json:"starts_at"`
	EndsAt          time.Time `json:"ends_at"`
	Additional      bool      `json:"additional"`
}

func (a plannedAllocation) span() span { return span{a.StaffID, a.StartsAt, a.EndsAt} }

type rosterOutcome struct {
	action, status, reason                        string
	mode                                          *string
	add                                           []plannedAllocation
	remove                                        []uuid.UUID
	removeAll                                     bool
	skippedExisting, skippedUnassigned, conflicts int
}

const rosterView = `(SELECT r.*,(SELECT count(*) FROM workforce_roster_allocations a WHERE (a.tenant_id,a.hospital_id,a.roster_id)=(r.tenant_id,r.hospital_id,r.id) AND a.removed_version IS NULL) AS allocations FROM workforce_rosters r) AS v`
const rosterColumns = "id,revision_id,name,start_date::text AS start_date,end_date::text AS end_date,status,settings_revision_id,timezone,week_start,auto_population,allocations,reviewed_by_subject,version,created_at,updated_at"
const rosterRevisionColumns = "id,id AS revision_id,roster_id,action,mode,status,added,removed,skipped_existing,skipped_unassigned,skipped_conflicts,reason,reviewed_by_subject,version,created_at"
const allocationView = `(SELECT a.tenant_id,a.hospital_id,a.roster_id,a.id,a.staff_id,s.employee_code,s.display_name,a.roster_date::text AS roster_date,
 a.shift_revision_id,sr.code AS shift_code,sr.name AS shift_name,sr.start_time,sr.end_time,a.area_revision_id,ar.code AS area_code,ar.name AS area_name,
 a.starts_at AT TIME ZONE 'UTC' AS starts_at,a.ends_at AT TIME ZONE 'UTC' AS ends_at,a.additional,a.added_version,a.removed_version,a.created_at
 FROM workforce_roster_allocations a JOIN workforce_staff s ON (s.tenant_id,s.hospital_id,s.id)=(a.tenant_id,a.hospital_id,a.staff_id)
 JOIN workforce_shift_revisions sr ON (sr.tenant_id,sr.hospital_id,sr.id)=(a.tenant_id,a.hospital_id,a.shift_revision_id)
 JOIN workforce_roster_config_revisions ar ON (ar.tenant_id,ar.hospital_id,ar.id)=(a.tenant_id,a.hospital_id,a.area_revision_id)) AS v`
const allocationColumns = "id,staff_id,employee_code,display_name,roster_date,shift_revision_id,shift_code,shift_name,start_time,end_time,area_revision_id,area_code,area_name,starts_at,ends_at,additional,added_version,created_at"

// wallInstant resolves a hospital wall-clock time to one instant, refusing DST gaps and folds rather than guessing.
func wallInstant(date time.Time, hhmm string, loc *time.Location) (time.Time, bool) {
	if len(hhmm) != 5 || hhmm[2] != ':' {
		return time.Time{}, false
	}
	hour, errH := strconv.Atoi(hhmm[:2])
	minute, errM := strconv.Atoi(hhmm[3:])
	if errH != nil || errM != nil {
		return time.Time{}, false
	}
	y, m, d := date.Date()
	noon := time.Date(y, m, d, 12, 0, 0, 0, loc)
	var found []time.Time
	for _, probe := range []time.Time{noon.Add(-24 * time.Hour), noon, noon.Add(24 * time.Hour)} {
		_, offset := probe.Zone()
		t := time.Date(y, m, d, hour, minute, 0, 0, time.UTC).Add(-time.Duration(offset) * time.Second)
		local := t.In(loc)
		ly, lm, ld := local.Date()
		if ly == y && lm == m && ld == d && local.Hour() == hour && local.Minute() == minute && !slices.ContainsFunc(found, t.Equal) {
			found = append(found, t)
		}
	}
	if len(found) != 1 {
		return time.Time{}, false
	}
	return found[0], true
}

// shiftInstants places a shift on a roster date; an end at or before the start finishes the next day.
func shiftInstants(day, start, end string, loc *time.Location) (time.Time, time.Time, error) {
	date, err := time.Parse(time.DateOnly, day)
	if err != nil {
		return time.Time{}, time.Time{}, errRosterChangeInvalid
	}
	endDate := date
	if end <= start {
		endDate = date.AddDate(0, 0, 1)
	}
	from, okFrom := wallInstant(date, start, loc)
	to, okTo := wallInstant(endDate, end, loc)
	if !okFrom || !okTo || !to.After(from) {
		return time.Time{}, time.Time{}, &rosterProblem{400, "dst_unresolvable", "The " + start + "–" + end + " shift on " + day + " falls in a daylight-saving gap or repeated hour. Use a shift that avoids the transition."}
	}
	return from, to, nil
}

func validRosterPeriod(in rosterInput) bool {
	start, errS := time.Parse(time.DateOnly, in.StartDate)
	end, errE := time.Parse(time.DateOnly, in.EndDate)
	days := int(end.Sub(start).Hours()/24) + 1
	name := strings.TrimSpace(in.Name)
	return errS == nil && errE == nil && start.Year() >= 1900 && days >= 7 && days <= 42 && days%7 == 0 &&
		name != "" && len([]rune(name)) <= 80 && !strings.ContainsRune(name, 0)
}

func rosterID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "roster")
		return uuid.Nil, false
	}
	return id, true
}

func loadRoster(tx *gorm.DB, p authn.Principal, hospital, id uuid.UUID) (roster, error) {
	var out roster
	err := tx.Table(rosterView).Select(rosterColumns).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, id).Take(&out).Error
	return out, err
}

func (h *handlers) listRosters(c *gin.Context, params pagination.Params) ([]roster, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	rows := []roster{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Table(rosterView).Select(rosterColumns).Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Find(&rows).Error
	})
	if err != nil {
		respondRosterError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) getRoster(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, ok := rosterID(c)
	if !ok {
		return
	}
	var out roster
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var err error
		out, err = loadRoster(tx, p, hospital, id)
		return err
	})
	if err != nil {
		respondRosterError(c, err)
		return
	}
	respond.OK(c, out)
}

func (h *handlers) rosterRevisions(c *gin.Context, params pagination.Params) ([]rosterRevision, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	id, ok := rosterID(c)
	if !ok {
		return nil, nil
	}
	rows := []rosterRevision{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		if _, err := loadRoster(tx, p, hospital, id); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Table("workforce_roster_revisions").Select(rosterRevisionColumns).Where("tenant_id=? AND hospital_id=? AND roster_id=?", p.TenantID, hospital, id).Find(&rows).Error
	})
	if err != nil {
		respondRosterError(c, err)
		return nil, nil
	}
	return rows, nil
}

// rosterAllocations answers "what was rostered at version N"; without ?version it is the current roster.
func (h *handlers) rosterAllocations(c *gin.Context, params pagination.Params) ([]datedAllocation, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	id, ok := rosterID(c)
	if !ok {
		return nil, nil
	}
	version := int64(0)
	if raw := c.Query("version"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 1 {
			respond.Error(c, 400, "invalid_request", "Use a positive roster version.")
			return nil, nil
		}
		version = n
	}
	rows := []datedAllocation{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		head, err := loadRoster(tx, p, hospital, id)
		if err != nil {
			return err
		}
		if version == 0 {
			version = head.Version
		}
		if version > head.Version {
			return gorm.ErrRecordNotFound
		}
		return tenantdb.ApplyKeyset(tx, params).Table(allocationView).Select(allocationColumns).
			Where("tenant_id=? AND hospital_id=? AND roster_id=? AND added_version<=? AND (removed_version IS NULL OR removed_version>?)", p.TenantID, hospital, id, version, version).Find(&rows).Error
	})
	if err != nil {
		respondRosterError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) putRoster(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, ok := rosterID(c)
	if !ok {
		return
	}
	var in rosterInput
	if !decode(c, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	if !validRosterPeriod(in) || !*in.Reviewed {
		respondRosterError(c, errRosterInput)
		return
	}
	var out roster
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_rostering", "roster:"+id.String()); err != nil {
			return err
		}
		existing, err := loadRoster(tx, p, hospital, id)
		if err == nil {
			if existing.Name != in.Name || existing.StartDate != in.StartDate || existing.EndDate != in.EndDate {
				return errRosterExists
			}
			out = existing
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var settings struct {
			ID        uuid.UUID
			WeekStart int
		}
		if err := tx.Raw(`SELECT id,week_start FROM workforce_roster_setting_revisions WHERE tenant_id=? AND hospital_id=? AND effective_on<=? ORDER BY effective_on DESC,version DESC LIMIT 1`, p.TenantID, hospital, in.StartDate).Scan(&settings).Error; err != nil {
			return err
		}
		if settings.ID == uuid.Nil {
			return errRosterSettingsMissing
		}
		start, _ := time.Parse(time.DateOnly, in.StartDate)
		if (int(start.Weekday())+6)%7 != settings.WeekStart {
			return errRosterWeekStart
		}
		revision := uuid.New()
		if err := tx.Exec(`INSERT INTO workforce_rosters(tenant_id,hospital_id,id,name,start_date,end_date,status,settings_revision_id,timezone,week_start,auto_population,reviewed_by_subject,version,revision_id)
 SELECT ?,?,?,?,?,?,'draft',s.id,s.timezone,s.week_start,s.auto_population,?,1,? FROM workforce_roster_setting_revisions s WHERE s.tenant_id=? AND s.hospital_id=? AND s.id=?`,
			p.TenantID, hospital, id, in.Name, in.StartDate, in.EndDate, p.Subject, revision, p.TenantID, hospital, settings.ID).Error; err != nil {
			return err
		}
		if err := insertRosterRevision(tx, p, hospital, id, revision, 1, rosterOutcome{action: "created", status: "draft"}); err != nil {
			return err
		}
		if err := h.recordRosterChange(tx, p, hospital, id, revision, 1, rosterOutcome{action: "created"}); err != nil {
			return err
		}
		out, err = loadRoster(tx, p, hospital, id)
		return err
	})
	if err != nil {
		respondRosterError(c, err)
		return
	}
	respond.OK(c, out)
}

func insertRosterRevision(tx *gorm.DB, p authn.Principal, hospital, id, revision uuid.UUID, version int64, o rosterOutcome) error {
	removed := len(o.remove)
	return tx.Exec(`INSERT INTO workforce_roster_revisions(tenant_id,hospital_id,id,roster_id,action,mode,status,added,removed,skipped_existing,skipped_unassigned,skipped_conflicts,reason,reviewed_by_subject,version)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.TenantID, hospital, revision, id, o.action, o.mode, o.status, len(o.add), removed, o.skippedExisting, o.skippedUnassigned, o.conflicts, o.reason, p.Subject, version).Error
}

// recordRosterChange writes the audit row and the identifier-only event inside the business transaction.
func (h *handlers) recordRosterChange(tx *gorm.DB, p authn.Principal, hospital, id, revision uuid.UUID, version int64, o rosterOutcome) error {
	if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,jsonb_build_object('version',?::bigint,'previous_version',?::bigint,'revision_id',?::uuid,'added',?::int,'removed',?::int))`,
		p.TenantID, hospital, p.Subject, "workforce.roster."+o.action, id, version, version-1, revision, len(o.add), len(o.remove)).Error; err != nil {
		return err
	}
	data, err := json.Marshal(workforcecontract.RosterChangedData{HospitalID: hospital.String(), RosterID: id.String(), RevisionID: revision.String(), Version: version})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, RosterChanged, events.Event{Type: RosterChanged, Version: 1, TenantID: p.TenantID, Data: data})
}

// writeRoster runs one versioned roster transition; a replayed change ID returns the original result.
func (h *handlers) writeRoster(c *gin.Context, changeID uuid.UUID, expected int64, action string, plan func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, head roster) (rosterOutcome, error)) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, ok := rosterID(c)
	if !ok {
		return
	}
	var out rosterChangeResult
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_rostering", "roster:"+id.String()); err != nil {
			return err
		}
		var prior rosterRevision
		err := tx.Table("workforce_roster_revisions").Select(rosterRevisionColumns).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, changeID).Take(&prior).Error
		if err == nil {
			if prior.RosterID != id || prior.Action != action {
				return errRosterChangeReused
			}
			out.Revision = prior
			out.Roster, err = loadRoster(tx, p, hospital, id)
			return err
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Exec(`SELECT 1 FROM workforce_rosters WHERE tenant_id=? AND hospital_id=? AND id=? FOR UPDATE`, p.TenantID, hospital, id).Error; err != nil {
			return err
		}
		head, err := loadRoster(tx, p, hospital, id)
		if err != nil {
			return err
		}
		if head.Version != expected {
			return errRosterStale
		}
		o, err := plan(tx, p, hospital, head)
		if err != nil {
			return err
		}
		o.action = action
		if o.removeAll {
			o.remove = make([]uuid.UUID, head.Allocations)
		}
		version := head.Version + 1
		if err := insertRosterRevision(tx, p, hospital, id, changeID, version, o); err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE workforce_rosters SET status=?,version=?,revision_id=?,updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND id=?`, o.status, version, changeID, p.TenantID, hospital, id).Error; err != nil {
			return err
		}
		removal := tx.Exec(`UPDATE workforce_roster_allocations SET removed_version=?,removed_revision_id=? WHERE tenant_id=? AND hospital_id=? AND roster_id=? AND removed_version IS NULL AND (? OR id IN ?)`,
			version, changeID, p.TenantID, hospital, id, o.removeAll, slices.Concat(o.remove, []uuid.UUID{uuid.Nil}))
		if removal.Error != nil {
			return removal.Error
		}
		if removal.RowsAffected != int64(len(o.remove)) {
			return errRosterAllocationRef
		}
		for chunk := range slices.Chunk(o.add, allocationInsertChunk) {
			rows, err := json.Marshal(chunk)
			if err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO workforce_roster_allocations(tenant_id,hospital_id,id,roster_id,staff_id,roster_date,shift_revision_id,area_revision_id,starts_at,ends_at,additional,added_version,added_revision_id)
 SELECT ?,?,x.id,?,x.staff_id,x.roster_date,x.shift_revision_id,x.area_revision_id,x.starts_at,x.ends_at,x.additional,?,?
 FROM jsonb_to_recordset(?::jsonb) AS x(id uuid,staff_id uuid,roster_date date,shift_revision_id uuid,area_revision_id uuid,starts_at timestamptz,ends_at timestamptz,additional boolean)`,
				p.TenantID, hospital, id, version, changeID, string(rows)).Error; err != nil {
				return err
			}
		}
		if err := h.recordRosterChange(tx, p, hospital, id, changeID, version, o); err != nil {
			return err
		}
		if err := tx.Table("workforce_roster_revisions").Select(rosterRevisionColumns).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, changeID).Take(&out.Revision).Error; err != nil {
			return err
		}
		out.Roster, err = loadRoster(tx, p, hospital, id)
		return err
	})
	if err != nil {
		respondRosterError(c, err)
		return
	}
	respond.OK(c, out)
}

func (h *handlers) changeRoster(c *gin.Context) {
	var in rosterChangeInput
	if !decodeSized(c, &in, rosterChangeBodyLimit) {
		return
	}
	if !*in.Reviewed {
		respondRosterError(c, errRosterReviewRequired)
		return
	}
	if in.ChangeID == uuid.Nil || len(in.Add)+len(in.Remove) == 0 || strings.ContainsRune(in.Reason, 0) || (in.Mode == "additional" && len(in.Remove) > 0) {
		respondRosterError(c, errRosterChangeInvalid)
		return
	}
	h.writeRoster(c, in.ChangeID, *in.ExpectedVersion, "changed", func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, head roster) (rosterOutcome, error) {
		if head.Status != "draft" {
			return rosterOutcome{}, errRosterState
		}
		return planRosterChange(tx, p, hospital, head, in)
	})
}

type activeAllocation struct {
	ID         uuid.UUID
	StaffID    uuid.UUID
	RosterDate string
	StartsAt   time.Time
	EndsAt     time.Time
	Additional bool
}

// activeAllocations returns every current allocation that could overlap the period, across adjacent rosters too.
func activeAllocations(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, head roster, staff []uuid.UUID) ([]activeAllocation, error) {
	var rows []activeAllocation
	q := tx.Table("workforce_roster_allocations").Select("id,staff_id,roster_date::text AS roster_date,starts_at,ends_at,additional").
		Where("tenant_id=? AND hospital_id=? AND removed_version IS NULL AND roster_date BETWEEN ?::date-2 AND ?::date+2", p.TenantID, hospital, head.StartDate, head.EndDate)
	if staff != nil {
		q = q.Where("staff_id IN ?", slices.Concat(staff, []uuid.UUID{uuid.Nil}))
	}
	return rows, q.Find(&rows).Error
}

func planRosterChange(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, head roster, in rosterChangeInput) (rosterOutcome, error) {
	mode := in.Mode
	o := rosterOutcome{status: "draft", mode: &mode, reason: strings.TrimSpace(in.Reason), remove: in.Remove}
	loc, err := time.LoadLocation(head.Timezone)
	if err != nil {
		return o, err
	}
	ids := map[uuid.UUID]bool{}
	var addIDs, staffIDs, areaIDs []uuid.UUID
	for _, a := range in.Add {
		if a.ID == uuid.Nil || ids[a.ID] || a.RosterDate < head.StartDate || a.RosterDate > head.EndDate ||
			(mode == "roster" && a.AreaRevisionID != in.Add[0].AreaRevisionID) || (mode == "employee" && a.StaffID != in.Add[0].StaffID) {
			return o, errRosterChangeInvalid
		}
		ids[a.ID] = true
		addIDs = append(addIDs, a.ID)
		staffIDs = append(staffIDs, a.StaffID)
		areaIDs = append(areaIDs, a.AreaRevisionID)
	}
	for _, id := range in.Remove {
		if ids[id] {
			return o, errRosterChangeInvalid
		}
		ids[id] = true
	}
	if len(in.Add) > 0 {
		var reused int64
		if err := tx.Table("workforce_roster_allocations").Where("tenant_id=? AND hospital_id=? AND id IN ?", p.TenantID, hospital, addIDs).Count(&reused).Error; err != nil {
			return o, err
		}
		if reused > 0 {
			return o, errRosterAllocationReused
		}
	}
	var people []struct {
		ID               uuid.UUID
		EmploymentStatus string
	}
	if len(staffIDs) > 0 {
		if err := tx.Raw(`SELECT id,employment_status FROM workforce_staff WHERE tenant_id=? AND hospital_id=? AND id IN ? FOR SHARE`, p.TenantID, hospital, staffIDs).Scan(&people).Error; err != nil {
			return o, err
		}
	}
	activeStaff := map[uuid.UUID]bool{}
	for _, s := range people {
		activeStaff[s.ID] = s.EmploymentStatus == "active"
	}
	type areaShift struct{ area, shift uuid.UUID }
	var members []struct {
		AreaRevisionID  uuid.UUID
		ShiftRevisionID uuid.UUID
		StartTime       string
		EndTime         string
		EffectiveOn     string
	}
	if len(areaIDs) > 0 {
		if err := tx.Raw(`SELECT r.id AS area_revision_id,m.shift_revision_id,sr.start_time,sr.end_time,r.effective_on::text AS effective_on
 FROM workforce_roster_config_revisions r JOIN workforce_roster_configs c ON (c.tenant_id,c.hospital_id,c.id)=(r.tenant_id,r.hospital_id,r.config_id)
 JOIN workforce_roster_config_members m ON (m.tenant_id,m.hospital_id,m.revision_id)=(r.tenant_id,r.hospital_id,r.id)
 JOIN workforce_shift_revisions sr ON (sr.tenant_id,sr.hospital_id,sr.id)=(m.tenant_id,m.hospital_id,m.shift_revision_id)
 WHERE r.tenant_id=? AND r.hospital_id=? AND r.kind='area' AND r.id IN ? AND c.active AND c.revision_id=r.id FOR SHARE OF c`, p.TenantID, hospital, areaIDs).Scan(&members).Error; err != nil {
			return o, err
		}
	}
	type window struct{ start, end, effective string }
	shifts := map[areaShift]window{}
	for _, m := range members {
		shifts[areaShift{m.AreaRevisionID, m.ShiftRevisionID}] = window{m.StartTime, m.EndTime, m.EffectiveOn}
	}
	for _, a := range in.Add {
		w, found := shifts[areaShift{a.AreaRevisionID, a.ShiftRevisionID}]
		if !activeStaff[a.StaffID] || !found || w.effective > a.RosterDate {
			return o, errRosterAllocationRef
		}
		from, to, err := shiftInstants(a.RosterDate, w.start, w.end, loc)
		if err != nil {
			return o, err
		}
		o.add = append(o.add, plannedAllocation{ID: a.ID, StaffID: a.StaffID, RosterDate: a.RosterDate, ShiftRevisionID: a.ShiftRevisionID, AreaRevisionID: a.AreaRevisionID, StartsAt: from, EndsAt: to, Additional: mode == "additional"})
	}

	var touched []uuid.UUID
	if len(in.Remove) == 0 {
		touched = staffIDs
	}
	current, err := activeAllocations(tx, p, hospital, head, touched)
	if err != nil {
		return o, err
	}
	removing := map[uuid.UUID]bool{}
	for _, id := range in.Remove {
		removing[id] = true
	}
	type staffDay struct {
		staff uuid.UUID
		day   string
	}
	primaries, additionals := map[staffDay]bool{}, map[staffDay]bool{}
	var occupied []span
	found := 0
	for _, a := range current {
		if removing[a.ID] {
			found++
			continue
		}
		key := staffDay{a.StaffID, a.RosterDate}
		if a.Additional {
			additionals[key] = true
		} else {
			primaries[key] = true
		}
		occupied = append(occupied, span{a.StaffID, a.StartsAt, a.EndsAt})
	}
	if found != len(in.Remove) {
		return o, errRosterAllocationRef
	}
	for key := range additionals {
		if !primaries[key] {
			return o, errRosterAdditionalOrphan
		}
	}
	var clashes []uuid.UUID
	for _, a := range o.add {
		key := staffDay{a.StaffID, a.RosterDate}
		if a.Additional && !primaries[key] {
			return o, errRosterAdditionalNeeds
		}
		s := a.span()
		if (!a.Additional && primaries[key]) || slices.ContainsFunc(occupied, s.overlaps) {
			clashes = append(clashes, a.ID)
		}
		if !a.Additional {
			primaries[key] = true
		}
		occupied = append(occupied, s)
	}
	if len(clashes) > 0 {
		return o, allocationOverlap{clashes}
	}
	return o, nil
}

func (h *handlers) populateRoster(c *gin.Context) {
	var in rosterPopulateInput
	if !decodeSized(c, &in, rosterChangeBodyLimit) {
		return
	}
	if !*in.Reviewed {
		respondRosterError(c, errRosterReviewRequired)
		return
	}
	if in.ChangeID == uuid.Nil || strings.ContainsRune(in.Reason, 0) || slices.Contains(in.StaffIDs, uuid.Nil) {
		respondRosterError(c, errRosterChangeInvalid)
		return
	}
	h.writeRoster(c, in.ChangeID, *in.ExpectedVersion, "populated", func(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, head roster) (rosterOutcome, error) {
		if head.Status != "draft" {
			return rosterOutcome{}, errRosterState
		}
		if head.AutoPopulation == "disabled" {
			return rosterOutcome{}, errPopulationDisabled
		}
		return planPopulation(tx, p, hospital, head, in)
	})
}

type populationSlot struct {
	StaffID         uuid.UUID
	RosterDate      string
	Fixed           bool
	Active          bool
	WeeklyOff       bool
	ShiftRevisionID *uuid.UUID
	AreaRevisionID  *uuid.UUID
	StartTime       *string
	EndTime         *string
}

// planPopulation expands each staff member's assignment in force on each date into the pinned template's allocations.
func planPopulation(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, head roster, in rosterPopulateInput) (rosterOutcome, error) {
	o := rosterOutcome{status: "draft", reason: strings.TrimSpace(in.Reason)}
	loc, err := time.LoadLocation(head.Timezone)
	if err != nil {
		return o, err
	}
	filter := ""
	args := []any{head.StartDate, head.EndDate, p.TenantID, hospital}
	if len(in.StaffIDs) > 0 {
		filter = " AND a.staff_id IN ?"
		args = append(args, in.StaffIDs)
	}
	args = append(args, maxPopulationRows+1)
	var slots []populationSlot
	if err := tx.Raw(`WITH days AS (SELECT d::date AS day FROM generate_series(?::date,?::date,interval '1 day') d),
 pinned AS (SELECT DISTINCT ON (a.staff_id,days.day) a.tenant_id,a.hospital_id,a.staff_id,days.day,a.weekly_template_revision_id,a.fixed,a.active
  FROM days JOIN workforce_staff_roster_assignment_revisions a ON a.tenant_id=? AND a.hospital_id=? AND a.effective_on<=days.day`+filter+`
  ORDER BY a.staff_id,days.day,a.effective_on DESC,a.version DESC)
 SELECT p.staff_id,p.day::text AS roster_date,p.fixed,p.active,d.weekly_off,m.shift_revision_id,m.area_revision_id,sr.start_time,sr.end_time
 FROM pinned p JOIN workforce_roster_config_days d ON (d.tenant_id,d.hospital_id,d.revision_id)=(p.tenant_id,p.hospital_id,p.weekly_template_revision_id) AND d.weekday=extract(isodow FROM p.day)::int-1
 LEFT JOIN workforce_roster_config_members m ON (m.tenant_id,m.hospital_id,m.revision_id,m.weekday)=(d.tenant_id,d.hospital_id,d.revision_id,d.weekday)
 LEFT JOIN workforce_shift_revisions sr ON (sr.tenant_id,sr.hospital_id,sr.id)=(m.tenant_id,m.hospital_id,m.shift_revision_id)
 ORDER BY p.staff_id,p.day,m.position LIMIT ?`, args...).Scan(&slots).Error; err != nil {
		return o, err
	}
	if len(slots) > maxPopulationRows {
		return o, errPopulationTooLarge
	}
	staffIDs := []uuid.UUID{}
	for _, s := range slots {
		if len(staffIDs) == 0 || staffIDs[len(staffIDs)-1] != s.StaffID {
			staffIDs = append(staffIDs, s.StaffID)
		}
	}
	activeStaff := map[uuid.UUID]bool{}
	if len(staffIDs) > 0 {
		var people []struct {
			ID               uuid.UUID
			EmploymentStatus string
		}
		if err := tx.Raw(`SELECT id,employment_status FROM workforce_staff WHERE tenant_id=? AND hospital_id=? AND id IN ? FOR SHARE`, p.TenantID, hospital, staffIDs).Scan(&people).Error; err != nil {
			return o, err
		}
		for _, s := range people {
			activeStaff[s.ID] = s.EmploymentStatus == "active"
		}
	}
	current, err := activeAllocations(tx, p, hospital, head, nil)
	if err != nil {
		return o, err
	}
	type staffDay struct {
		staff uuid.UUID
		day   string
	}
	booked := map[staffDay]bool{}
	occupied := map[uuid.UUID][]span{}
	for _, a := range current {
		booked[staffDay{a.StaffID, a.RosterDate}] = true
		occupied[a.StaffID] = append(occupied[a.StaffID], span{a.StaffID, a.StartsAt, a.EndsAt})
	}
	for len(slots) > 0 {
		n := 1
		for n < len(slots) && slots[n].StaffID == slots[0].StaffID && slots[n].RosterDate == slots[0].RosterDate {
			n++
		}
		group := slots[:n]
		slots = slots[n:]
		first := group[0]
		key := staffDay{first.StaffID, first.RosterDate}
		switch {
		case first.WeeklyOff:
			continue
		case !first.Active || !activeStaff[first.StaffID] || (head.AutoPopulation == "fixed_shift" && !first.Fixed):
			o.skippedUnassigned++
			continue
		case booked[key]:
			o.skippedExisting++
			continue
		}
		planned := make([]plannedAllocation, 0, len(group))
		clash := false
		for i, s := range group {
			if s.ShiftRevisionID == nil || s.AreaRevisionID == nil || s.StartTime == nil || s.EndTime == nil {
				return o, errors.New("weekly template working day has no allocation")
			}
			from, to, err := shiftInstants(s.RosterDate, *s.StartTime, *s.EndTime, loc)
			if err != nil {
				return o, err
			}
			a := plannedAllocation{ID: uuid.New(), StaffID: s.StaffID, RosterDate: s.RosterDate, ShiftRevisionID: *s.ShiftRevisionID, AreaRevisionID: *s.AreaRevisionID, StartsAt: from, EndsAt: to, Additional: i > 0}
			clash = clash || slices.ContainsFunc(occupied[s.StaffID], a.span().overlaps)
			planned = append(planned, a)
		}
		if clash {
			o.conflicts++
			continue
		}
		if len(o.add)+len(planned) > maxPopulatedRows {
			return o, errPopulationTooLarge
		}
		for _, a := range planned {
			occupied[a.StaffID] = append(occupied[a.StaffID], a.span())
		}
		o.add = append(o.add, planned...)
	}
	return o, nil
}

type rosterTransition struct {
	from, to     string
	needsReason  bool
	clearsRoster bool
}

func (h *handlers) transitionRoster(t rosterTransition, action string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var in rosterStatusInput
		if !decode(c, &in) {
			return
		}
		reason := strings.TrimSpace(in.Reason)
		switch {
		case !*in.Reviewed:
			respondRosterError(c, errRosterReviewRequired)
			return
		case in.ChangeID == uuid.Nil || strings.ContainsRune(reason, 0):
			respondRosterError(c, errRosterChangeInvalid)
			return
		case t.needsReason && reason == "":
			respondRosterError(c, errRosterReasonRequired)
			return
		}
		h.writeRoster(c, in.ChangeID, *in.ExpectedVersion, action, func(_ *gorm.DB, _ authn.Principal, _ uuid.UUID, head roster) (rosterOutcome, error) {
			if head.Status != t.from {
				return rosterOutcome{}, errRosterState
			}
			return rosterOutcome{status: t.to, reason: reason, removeAll: t.clearsRoster}, nil
		})
	}
}

func respondRosterError(c *gin.Context, err error) {
	var problem *rosterProblem
	var overlap allocationOverlap
	var pg *pgconn.PgError
	switch {
	case errors.As(err, &problem):
		respond.Error(c, problem.status, problem.code, problem.message)
	case errors.As(err, &overlap):
		respond.ConflictWithDetail(c, "These allocations overlap existing shifts or give someone two primary shifts on one date.", gin.H{"reason": "allocation_overlap", "allocation_ids": overlap.ids})
	case errors.As(err, &pg) && pg.ConstraintName == "workforce_roster_period_overlap":
		respond.Error(c, errRosterPeriodOverlap.status, errRosterPeriodOverlap.code, errRosterPeriodOverlap.message)
	case errors.As(err, &pg) && (pg.ConstraintName == "workforce_roster_allocation_overlap" || pg.ConstraintName == "workforce_roster_allocation_primary"):
		// Backstop for a concurrent write to an adjacent roster that the pre-check could not see.
		respond.ConflictWithDetail(c, "Another change booked one of these people at the same time. Reload and retry.", gin.H{"reason": "allocation_overlap"})
	case errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "workforce_roster_allocations_pkey":
		respond.Error(c, errRosterAllocationReused.status, errRosterAllocationReused.code, errRosterAllocationReused.message)
	case errors.As(err, &pg) && pg.Code == "23505":
		respond.Error(c, http.StatusConflict, "conflict", "The roster changed concurrently. Reload before saving.")
	default:
		respondError(c, err)
	}
}

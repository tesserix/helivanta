package workforce

import (
	"encoding/json"
	"errors"
	"regexp"
	"time"
	_ "time/tzdata" // timezone validation must not depend on the runtime image's zoneinfo

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func (s rosterSettings) PageKey() (time.Time, uuid.UUID) { return s.CreatedAt, s.RevisionID }
func (a staffRosterAssignment) PageKey() (time.Time, uuid.UUID) {
	return a.CreatedAt, a.StaffID
}

type staffRosterAssignmentRevision struct{ staffRosterAssignment }

func (a staffRosterAssignmentRevision) PageKey() (time.Time, uuid.UUID) {
	return a.CreatedAt, a.RevisionID
}

var errPolicyInvalid = errors.New("roster policy is invalid")
var errPolicyEffective = errors.New("roster policy effective date precedes current revision")
var errPolicyStaffInactive = errors.New("roster assignment requires active staff")

// Mirrors workforce_roster_timezone_valid(): canonical area names only, never Local or posix/ aliases.
var ianaZone = regexp.MustCompile(`^(UTC|[A-Z][A-Za-z_]+(/[A-Za-z0-9_+-]+){1,2})$`)

const settingsColumns = "revision_id,timezone,week_start,auto_population,effective_on::text AS effective_on,reviewed_by_subject,version,created_at,updated_at"
const settingsRevisionColumns = "id AS revision_id,timezone,week_start,auto_population,effective_on::text AS effective_on,reviewed_by_subject,version,created_at,created_at AS updated_at"
const assignmentColumns = "staff_id,revision_id,employee_code,display_name,weekly_template_revision_id,template_id,template_code,template_name,fixed,active,effective_on,reviewed_by_subject,version,created_at,updated_at"

// Pinned template code/name come from the revision, so history shows what was actually assigned.
const assignmentHeadView = assignmentSelect + `a.staff_id AS id,a.revision_id,a.updated_at FROM workforce_staff_roster_assignments` + assignmentJoins
const assignmentRevisionView = assignmentSelect + `a.id,a.id AS revision_id,a.created_at AS updated_at FROM workforce_staff_roster_assignment_revisions` + assignmentJoins
const assignmentSelect = `(SELECT a.tenant_id,a.hospital_id,a.staff_id,s.employee_code,s.display_name,a.weekly_template_revision_id,t.config_id AS template_id,t.code AS template_code,t.name AS template_name,a.fixed,a.active,a.effective_on::text AS effective_on,a.reviewed_by_subject,a.version,a.created_at,`
const assignmentJoins = ` a JOIN workforce_staff s ON (s.tenant_id,s.hospital_id,s.id)=(a.tenant_id,a.hospital_id,a.staff_id)
 JOIN workforce_roster_config_revisions t ON (t.tenant_id,t.hospital_id,t.id)=(a.tenant_id,a.hospital_id,a.weekly_template_revision_id)) AS v`

func validEffectiveDate(value string) bool {
	date, err := time.Parse("2006-01-02", value)
	return err == nil && date.Year() >= 1
}

func validTimezone(name string) bool {
	if !ianaZone.MatchString(name) {
		return false
	}
	_, err := time.LoadLocation(name)
	return err == nil
}

func lockRosterManager(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, key string) error {
	return lockHospitalManager(tx, p, hospital, "app.workforce_roster_policy", key)
}

// lockHospitalManager holds the manager grant for the whole write so a concurrent revocation cannot interleave.
func lockHospitalManager(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, flag, key string) error {
	var grant struct{ Level string }
	if err := tx.Raw(`SELECT level FROM workforce_hospital_access WHERE tenant_id=? AND hospital_id=? AND subject=? FOR SHARE`, p.TenantID, hospital, p.Subject).Scan(&grant).Error; err != nil {
		return err
	}
	if grant.Level != "manager" {
		return gorm.ErrRecordNotFound
	}
	return tx.Exec(`SELECT set_config(?,'authorized',true),pg_advisory_xact_lock(hashtextextended(?,0))`, flag, p.TenantID+":"+hospital.String()+":"+key).Error
}

func (h *handlers) getRosterSettings(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	var result rosterSettings
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		return tx.Table("workforce_roster_settings").Select(settingsColumns).Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Take(&result).Error
	})
	if err != nil {
		respondRosterPolicyError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) rosterSettingsHistory(c *gin.Context, params pagination.Params) ([]rosterSettings, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, 400, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	rows := []rosterSettings{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Table("workforce_roster_setting_revisions").Select(settingsRevisionColumns).Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Find(&rows).Error
	})
	if err != nil {
		respondRosterPolicyError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) putRosterSettings(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	var in rosterSettingsInput
	if !decode(c, &in) {
		return
	}
	if !validTimezone(in.Timezone) || !validEffectiveDate(in.EffectiveOn) || !*in.Reviewed {
		respondRosterPolicyError(c, errPolicyInvalid)
		return
	}
	var result rosterSettings
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockRosterManager(tx, p, hospital, "roster-settings"); err != nil {
			return err
		}
		var old rosterSettings
		err := tx.Table("workforce_roster_settings").Select(settingsColumns).Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Take(&old).Error
		version := int64(1)
		switch {
		case err == nil:
			if old.Timezone == in.Timezone && old.WeekStart == *in.WeekStart && old.AutoPopulation == in.AutoPopulation && old.EffectiveOn == in.EffectiveOn {
				result = old
				return nil
			}
			if old.Version != *in.ExpectedVersion {
				return errRosterVersion
			}
			if in.EffectiveOn < old.EffectiveOn {
				return errPolicyEffective
			}
			version = old.Version + 1
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		case *in.ExpectedVersion != 0:
			return gorm.ErrRecordNotFound
		}
		revision := uuid.New()
		if err := tx.Exec(`INSERT INTO workforce_roster_settings(tenant_id,hospital_id,timezone,week_start,auto_population,effective_on,reviewed_by_subject,version,revision_id) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(tenant_id,hospital_id) DO UPDATE SET timezone=EXCLUDED.timezone,week_start=EXCLUDED.week_start,auto_population=EXCLUDED.auto_population,effective_on=EXCLUDED.effective_on,reviewed_by_subject=EXCLUDED.reviewed_by_subject,version=EXCLUDED.version,revision_id=EXCLUDED.revision_id,updated_at=clock_timestamp()`, p.TenantID, hospital, in.Timezone, *in.WeekStart, in.AutoPopulation, in.EffectiveOn, p.Subject, version, revision).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_roster_setting_revisions(tenant_id,hospital_id,id,timezone,week_start,auto_population,effective_on,reviewed_by_subject,version) VALUES(?,?,?,?,?,?,?,?,?)`, p.TenantID, hospital, revision, in.Timezone, *in.WeekStart, in.AutoPopulation, in.EffectiveOn, p.Subject, version).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.roster_settings.changed',?,jsonb_build_object('version',?::bigint,'previous_version',?::bigint))`, p.TenantID, hospital, p.Subject, revision, version, version-1).Error; err != nil {
			return err
		}
		data, err := json.Marshal(workforcecontract.RosterSettingsChangedData{HospitalID: hospital.String(), RevisionID: revision.String(), Version: version})
		if err != nil {
			return err
		}
		if err := h.bus.Publish(tx, RosterSettingsChanged, events.Event{Type: RosterSettingsChanged, Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
			return err
		}
		return tx.Table("workforce_roster_settings").Select(settingsColumns).Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Take(&result).Error
	})
	if err != nil {
		respondRosterPolicyError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) listStaffRosterAssignments(c *gin.Context, params pagination.Params) ([]staffRosterAssignment, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, 400, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	rows := []staffRosterAssignment{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		return tenantdb.ApplyKeyset(tx, params).Table(assignmentHeadView).Select(assignmentColumns).Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Find(&rows).Error
	})
	if err != nil {
		respondRosterPolicyError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) staffRosterAssignmentHistory(c *gin.Context, params pagination.Params) ([]staffRosterAssignmentRevision, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "staff roster assignment")
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, 400, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	found := []staffRosterAssignment{}
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var exists bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_staff_roster_assignments WHERE tenant_id=? AND hospital_id=? AND staff_id=?)`, p.TenantID, hospital, id).Scan(&exists).Error; err != nil {
			return err
		}
		if !exists {
			return gorm.ErrRecordNotFound
		}
		return tenantdb.ApplyKeyset(tx, params).Table(assignmentRevisionView).Select(assignmentColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=?", p.TenantID, hospital, id).Find(&found).Error
	})
	if err != nil {
		respondRosterPolicyError(c, err)
		return nil, nil
	}
	rows := make([]staffRosterAssignmentRevision, len(found))
	for i := range found {
		rows[i] = staffRosterAssignmentRevision{found[i]}
	}
	return rows, nil
}

func (h *handlers) putStaffRosterAssignment(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staffID, err := uuid.Parse(c.Param("id"))
	if err != nil || staffID == uuid.Nil {
		respond.NotFound(c, "staff")
		return
	}
	var in staffRosterAssignmentInput
	if !decode(c, &in) {
		return
	}
	if in.WeeklyTemplateRevisionID == uuid.Nil || !validEffectiveDate(in.EffectiveOn) || !*in.Reviewed {
		respondRosterPolicyError(c, errPolicyInvalid)
		return
	}
	var result staffRosterAssignment
	load := func(tx *gorm.DB, into *staffRosterAssignment, lock bool) error {
		q := tx.Table(assignmentHeadView).Select(assignmentColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=?", p.TenantID, hospital, staffID)
		if lock {
			if err := tx.Exec(`SELECT 1 FROM workforce_staff_roster_assignments WHERE tenant_id=? AND hospital_id=? AND staff_id=? FOR UPDATE`, p.TenantID, hospital, staffID).Error; err != nil {
				return err
			}
		}
		return q.Take(into).Error
	}
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockRosterManager(tx, p, hospital, "staff-roster-assignment:"+staffID.String()); err != nil {
			return err
		}
		var person struct{ EmploymentStatus string }
		if err := tx.Raw(`SELECT employment_status FROM workforce_staff WHERE tenant_id=? AND hospital_id=? AND id=? FOR SHARE`, p.TenantID, hospital, staffID).Scan(&person).Error; err != nil {
			return err
		}
		if person.EmploymentStatus == "" {
			return gorm.ErrRecordNotFound
		}
		var old staffRosterAssignment
		err := load(tx, &old, true)
		version := int64(1)
		pinned := false
		switch {
		case err == nil:
			if old.WeeklyTemplateRevisionID == in.WeeklyTemplateRevisionID && old.Fixed == *in.Fixed && old.Active == *in.Active && old.EffectiveOn == in.EffectiveOn {
				result = old
				return nil
			}
			if old.Version != *in.ExpectedVersion {
				return errRosterVersion
			}
			if in.EffectiveOn < old.EffectiveOn {
				return errPolicyEffective
			}
			pinned = old.WeeklyTemplateRevisionID == in.WeeklyTemplateRevisionID
			version = old.Version + 1
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		case *in.ExpectedVersion != 0:
			return gorm.ErrRecordNotFound
		}
		if *in.Active && person.EmploymentStatus != "active" {
			return errPolicyStaffInactive
		}
		var template struct {
			Current     bool
			EffectiveOn string
		}
		if err := tx.Raw(`SELECT (c.active AND c.revision_id=r.id) AS current,r.effective_on::text AS effective_on
 FROM workforce_roster_config_revisions r JOIN workforce_roster_configs c ON (c.tenant_id,c.hospital_id,c.id)=(r.tenant_id,r.hospital_id,r.config_id)
 WHERE r.tenant_id=? AND r.hospital_id=? AND r.id=? AND r.kind='weekly_template' FOR SHARE OF c`, p.TenantID, hospital, in.WeeklyTemplateRevisionID).Scan(&template).Error; err != nil {
			return err
		}
		if template.EffectiveOn == "" || template.EffectiveOn > in.EffectiveOn || !template.Current && !pinned {
			return errRosterReference
		}
		revision := uuid.New()
		if err := tx.Exec(`INSERT INTO workforce_staff_roster_assignments(tenant_id,hospital_id,staff_id,weekly_template_revision_id,fixed,active,effective_on,reviewed_by_subject,version,revision_id) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(tenant_id,hospital_id,staff_id) DO UPDATE SET weekly_template_revision_id=EXCLUDED.weekly_template_revision_id,fixed=EXCLUDED.fixed,active=EXCLUDED.active,effective_on=EXCLUDED.effective_on,reviewed_by_subject=EXCLUDED.reviewed_by_subject,version=EXCLUDED.version,revision_id=EXCLUDED.revision_id,updated_at=clock_timestamp()`, p.TenantID, hospital, staffID, in.WeeklyTemplateRevisionID, *in.Fixed, *in.Active, in.EffectiveOn, p.Subject, version, revision).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_staff_roster_assignment_revisions(tenant_id,hospital_id,id,staff_id,weekly_template_revision_id,fixed,active,effective_on,reviewed_by_subject,version) VALUES(?,?,?,?,?,?,?,?,?,?)`, p.TenantID, hospital, revision, staffID, in.WeeklyTemplateRevisionID, *in.Fixed, *in.Active, in.EffectiveOn, p.Subject, version).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.staff_roster_assignment.changed',?,jsonb_build_object('version',?::bigint,'previous_version',?::bigint))`, p.TenantID, hospital, p.Subject, staffID, version, version-1).Error; err != nil {
			return err
		}
		data, err := json.Marshal(workforcecontract.StaffRosterAssignmentChangedData{HospitalID: hospital.String(), StaffID: staffID.String(), RevisionID: revision.String(), Version: version})
		if err != nil {
			return err
		}
		if err := h.bus.Publish(tx, StaffRosterAssignmentChanged, events.Event{Type: StaffRosterAssignmentChanged, Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
			return err
		}
		return load(tx, &result, false)
	})
	if err != nil {
		respondRosterPolicyError(c, err)
		return
	}
	respond.OK(c, result)
}

func respondRosterPolicyError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errPolicyInvalid):
		respond.Error(c, 400, "invalid_request", "Use a supported IANA timezone, a valid effective date and confirm that you reviewed the policy.")
	case errors.Is(err, errPolicyEffective):
		respond.Error(c, 400, "invalid_request", "The effective date cannot be earlier than the current revision's effective date.")
	case errors.Is(err, errPolicyStaffInactive):
		respond.Error(c, 400, "invalid_request", "Only active staff can receive an active weekly-template assignment.")
	case errors.Is(err, errRosterReference):
		respond.Error(c, 400, "invalid_reference", "Select the current revision of an active weekly template that is effective on or before this date.")
	default:
		respondRosterConfigError(c, err)
	}
}

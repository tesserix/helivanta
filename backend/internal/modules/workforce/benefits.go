package workforce

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
)

const (
	benefitPlanColumns         = "p.id,p.code,p.name,r.kind,r.description,r.covers_dependents,r.covered_relationships::text AS relationships,p.effective_from::text AS effective_from,p.effective_until::text AS effective_until,p.status,p.version,p.revision_id,p.created_at,p.updated_at"
	benefitPlanRevisionColumns = "id,plan_id,version,code,name,kind,description,covers_dependents,covered_relationships::text AS relationships,effective_from::text AS effective_from,effective_until::text AS effective_until,status,changed_by_subject,created_at"
	dependentColumns           = "d.id,d.staff_id,d.full_name,r.relationship_revision_id,d.relationship_master_id,m.name AS relationship_name,d.birth_date::text AS birth_date,r.phone,d.status,d.version,d.revision_id,d.created_at,d.updated_at"
	dependentRevisionColumns   = "r.id,r.dependent_id,r.staff_id,r.version,r.full_name,r.relationship_revision_id,r.relationship_master_id,m.name AS relationship_name,r.birth_date::text AS birth_date,r.phone,r.status,r.reason,r.changed_by_subject,r.created_at"
	benefitProposalColumns     = "id,enrollment_id,staff_id,plan_id,plan_revision_id,dependent_id,dependent_revision_id,base_version,status,approved_version,coverage_from::text AS coverage_from,coverage_until::text AS coverage_until,eligibility_basis,proposed_by_subject,decided_by_subject,decided_at,decision_note,created_at"
	benefitPlanJoin            = "workforce_benefit_plans p JOIN workforce_benefit_plan_revisions r ON (r.tenant_id,r.hospital_id,r.id)=(p.tenant_id,p.hospital_id,p.revision_id)"
	dependentJoin              = "workforce_staff_dependents d JOIN workforce_staff_dependent_revisions r ON (r.tenant_id,r.hospital_id,r.id)=(d.tenant_id,d.hospital_id,d.revision_id) JOIN workforce_hr_master_revisions m ON (m.tenant_id,m.hospital_id,m.id)=(r.tenant_id,r.hospital_id,r.relationship_revision_id)"
	dependentRevisionJoin      = "workforce_staff_dependent_revisions r JOIN workforce_hr_master_revisions m ON (m.tenant_id,m.hospital_id,m.id)=(r.tenant_id,r.hospital_id,r.relationship_revision_id)"
)

var (
	errBenefitInvalid        = errors.New("benefit request invalid")
	errBenefitNoteRequired   = errors.New("benefit rejection needs note")
	errBenefitRelationship   = errors.New("benefit relationship master inactive or unknown")
	errBenefitVersion        = errors.New("benefit record version conflict")
	errBenefitPlanRetired    = errors.New("benefit plan retired")
	errBenefitPending        = errors.New("benefit proposal already pending")
	errBenefitReplay         = errors.New("benefit proposal id reused with different content")
	errBenefitUnchanged      = errors.New("benefit proposal changes nothing")
	errBenefitIdentity       = errors.New("benefit enrollment plan or member changed")
	errBenefitSelf           = errors.New("benefit proposal decided by its proposer")
	errBenefitNotProposer    = errors.New("benefit proposal withdrawn by someone else")
	errBenefitState          = errors.New("benefit proposal already decided")
	benefitPhone             = regexp.MustCompile(`^\+[1-9][0-9]{6,14}$`)
	benefitPlanCode          = regexp.MustCompile(`^[A-Z0-9_-]{1,40}$`)
	benefitIDNames           = map[string]string{"id": "staff member", "plan": "benefit plan", "dependent": "dependent", "enrollment": "benefit enrollment", "proposal": "benefit proposal"}
	benefitConstraintReasons = map[string][2]string{
		"workforce_benefit_enrollment_version":             {"version_conflict", "The enrollment changed since you opened it. Reload before saving."},
		"workforce_benefit_plan_changed":                   {"plan_changed", "The plan changed since this proposal was made. Propose the coverage again."},
		"workforce_benefit_dependent_changed":              {"dependent_changed", "The dependent changed since this proposal was made. Propose the coverage again."},
		"workforce_benefit_plan_retired":                   {"plan_retired", "The plan is retired, so it takes no new or longer coverage."},
		"workforce_benefit_coverage_outside_plan":          {"coverage_outside_plan", "Coverage must lie within the plan's effective dates."},
		"workforce_benefit_plan_code":                      {"duplicate_plan", "Another plan already uses this code."},
		"workforce_benefit_plan_name":                      {"duplicate_plan", "Another plan already uses this name."},
		"workforce_staff_dependent_unique":                 {"duplicate_dependent", "This dependent is already recorded for the staff member."},
		"workforce_staff_dependent_covered":                {"dependent_covered", "End the dependent's coverage before removing them or changing their relationship."},
		"workforce_staff_dependent_staff_inactive":         {"staff_inactive", "The staff member is not active."},
		"workforce_benefit_staff_inactive":                 {"staff_inactive", "The staff member is not active."},
		"workforce_benefit_proposal_one_pending":           {"proposal_pending", "Another proposal for this enrollment is waiting for a decision."},
		"workforce_benefit_coverage_overlap":               {"coverage_overlap", "This person already has coverage under the plan for some of these dates."},
		"workforce_benefit_pending_overlap":                {"coverage_overlap", "Another proposal already covers this person under the plan for some of these dates."},
		"workforce_benefit_relationship_not_covered":       {"relationship_not_covered", "The plan does not cover this dependent's relationship."},
		"workforce_benefit_dependent_removed":              {"dependent_removed", "The dependent has been removed."},
		"workforce_benefit_proposal_separation":            {"self_review", "The person who proposed the coverage must not approve or reject it."},
		"workforce_benefit_proposal_withdrawn_by_proposer": {"not_proposer", "Only the person who proposed the coverage can withdraw it."},
	}
)

func (p benefitPlan) PageKey() (time.Time, uuid.UUID)         { return p.CreatedAt, p.ID }
func (r benefitPlanRevision) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }
func (d dependent) PageKey() (time.Time, uuid.UUID)           { return d.CreatedAt, d.ID }
func (r dependentRevision) PageKey() (time.Time, uuid.UUID)   { return r.CreatedAt, r.ID }
func (e benefitEnrollment) PageKey() (time.Time, uuid.UUID)   { return e.CreatedAt, e.ID }
func (r benefitProposal) PageKey() (time.Time, uuid.UUID)     { return r.CreatedAt, r.ID }

func benefitIDs(c *gin.Context, names ...string) ([]uuid.UUID, bool) {
	ids := make([]uuid.UUID, len(names))
	for i, name := range names {
		id, err := uuid.Parse(c.Param(name))
		if err != nil || id == uuid.Nil {
			respond.NotFound(c, benefitIDNames[name])
			return nil, false
		}
		ids[i] = id
	}
	return ids, true
}

func benefitPage(c *gin.Context, params pagination.Params) bool {
	if params.Limit > 100 {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Use a page limit no greater than 100.")
		return false
	}
	return true
}

// benefitsRead opens the benefits RLS scope for any hospital reader; no other handler sets it.
func benefitsRead(tx *gorm.DB, tenant string, hospital uuid.UUID) error {
	if err := requireStaffAccess(tx, tenant, hospital, false); err != nil {
		return err
	}
	return tx.Exec(`SELECT set_config('app.workforce_benefits','read',true)`).Error
}

// benefitsWrite holds the manager grant for the whole write and serialises writes on one key.
func benefitsWrite(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, key string) error {
	var grant struct{ Level string }
	if err := tx.Raw(`SELECT level FROM workforce_hospital_access WHERE tenant_id=? AND hospital_id=? AND subject=? FOR SHARE`, p.TenantID, hospital, p.Subject).Scan(&grant).Error; err != nil {
		return err
	}
	if grant.Level != "manager" {
		return gorm.ErrRecordNotFound
	}
	return tx.Exec(`SELECT set_config('app.workforce_benefits','write',true),pg_advisory_xact_lock(hashtextextended(?,0))`, p.TenantID+":"+hospital.String()+":benefits:"+key).Error
}

func benefitAudit(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, action string, resource uuid.UUID, changes map[string]any) error {
	body, err := json.Marshal(changes)
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,?::jsonb)`,
		p.TenantID, hospital, p.Subject, "workforce.benefits."+action, resource, string(body)).Error
}

func publishBenefit(h *handlers, tx *gorm.DB, tenant, subject string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, subject, events.Event{Type: subject, Version: 1, TenantID: tenant, Data: data})
}

type benefitPlanRow struct {
	Plan          benefitPlan `gorm:"embedded"`
	Relationships string
}

func loadBenefitPlans(q *gorm.DB) ([]benefitPlan, error) {
	var rows []benefitPlanRow
	if err := q.Select(benefitPlanColumns).Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]benefitPlan, len(rows))
	for i, row := range rows {
		ids, err := parseUUIDArray(row.Relationships)
		if err != nil {
			return nil, err
		}
		result[i] = row.Plan
		result[i].CoveredRelationships = ids
	}
	return result, nil
}

func loadBenefitPlan(tx *gorm.DB, tenant string, hospital, plan uuid.UUID, lock string) (benefitPlan, error) {
	q := tx.Table(benefitPlanJoin).Where("p.tenant_id=? AND p.hospital_id=? AND p.id=?", tenant, hospital, plan)
	if lock != "" {
		q = q.Clauses(clause.Locking{Strength: lock, Table: clause.Table{Name: "p"}})
	}
	rows, err := loadBenefitPlans(q)
	if err != nil {
		return benefitPlan{}, err
	}
	if len(rows) == 0 {
		return benefitPlan{}, gorm.ErrRecordNotFound
	}
	return rows[0], nil
}

type benefitPlanRevisionRow struct {
	Revision      benefitPlanRevision `gorm:"embedded"`
	Relationships string
}

func loadDependent(tx *gorm.DB, tenant string, hospital, staff, id uuid.UUID, lock string) (dependent, error) {
	q := tx.Table(dependentJoin).Select(dependentColumns).Where("d.tenant_id=? AND d.hospital_id=? AND d.staff_id=? AND d.id=?", tenant, hospital, staff, id)
	if lock != "" {
		q = q.Clauses(clause.Locking{Strength: lock, Table: clause.Table{Name: "d"}})
	}
	var rows []dependent
	if err := q.Find(&rows).Error; err != nil {
		return dependent{}, err
	}
	if len(rows) == 0 {
		return dependent{}, gorm.ErrRecordNotFound
	}
	return rows[0], nil
}

func loadBenefitProposal(tx *gorm.DB, tenant string, hospital, staff, enrollment, id uuid.UUID, lock bool) (benefitProposal, error) {
	q := tx.Table("workforce_benefit_enrollment_proposals").Select(benefitProposalColumns).
		Where("tenant_id=? AND hospital_id=? AND staff_id=? AND enrollment_id=? AND id=?", tenant, hospital, staff, enrollment, id)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var row benefitProposal
	if err := q.Take(&row).Error; err != nil {
		return benefitProposal{}, err
	}
	return row, nil
}

type benefitEnrollmentRow struct {
	ID          uuid.UUID
	PlanID      uuid.UUID
	DependentID *uuid.UUID
	Coverage    benefitCoverage `gorm:"embedded"`
}

func loadBenefitCoverage(tx *gorm.DB, tenant string, hospital uuid.UUID, ids []uuid.UUID, lock bool) (map[uuid.UUID]benefitEnrollmentRow, error) {
	q := tx.Table("workforce_benefit_enrollments").Select("id,plan_id,dependent_id,original_plan_revision_id,coverage_from::text AS coverage_from,coverage_until::text AS coverage_until,version,proposal_id,updated_at").
		Where("tenant_id=? AND hospital_id=? AND id IN ?", tenant, hospital, ids)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var rows []benefitEnrollmentRow
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make(map[uuid.UUID]benefitEnrollmentRow, len(rows))
	for _, row := range rows {
		result[row.ID] = row
	}
	return result, nil
}

func validBenefitText(value string, lower, upper int) bool {
	n := utf8.RuneCountInString(value)
	return n >= lower && n <= upper && validStatutoryText(value)
}

// Mirrors table checks so a malformed plan is a 400 before any write; the database still enforces every rule.
func normalizeBenefitPlan(in *benefitPlanInput) error {
	in.Code, in.Name, in.Description = strings.ToUpper(strings.TrimSpace(in.Code)), strings.TrimSpace(in.Name), strings.TrimSpace(in.Description)
	if in.CoveredRelationships == nil {
		in.CoveredRelationships = []uuid.UUID{}
	}
	slices.SortFunc(in.CoveredRelationships, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	until := ""
	if in.EffectiveUntil != nil {
		until = *in.EffectiveUntil
	}
	if !benefitPlanCode.MatchString(in.Code) || !validBenefitText(in.Name, 1, 80) || !validBenefitText(in.Description, 0, 500) ||
		in.EffectiveFrom == "" || !validPeriod(in.EffectiveFrom, until) || in.EffectiveFrom < "2000-01-01" ||
		(in.EffectiveUntil != nil && until == "") || (in.Status == "retired" && until == "") ||
		*in.CoversDependents != (len(in.CoveredRelationships) > 0) {
		return errBenefitInvalid
	}
	for i, id := range in.CoveredRelationships {
		if id == uuid.Nil || (i > 0 && id == in.CoveredRelationships[i-1]) {
			return errBenefitInvalid
		}
	}
	return nil
}

func samePlanContent(cur benefitPlan, in benefitPlanInput) bool {
	return cur.Code == in.Code && cur.Name == in.Name && cur.Kind == in.Kind && cur.Description == in.Description &&
		cur.CoversDependents == *in.CoversDependents && slices.Equal(cur.CoveredRelationships, in.CoveredRelationships) &&
		cur.EffectiveFrom == in.EffectiveFrom && equalOptional(cur.EffectiveUntil, in.EffectiveUntil) && cur.Status == in.Status
}

func equalOptional[T comparable](a, b *T) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}

// requireRelationships refuses unknown masters and any newly cited master that is retired.
func requireRelationships(tx *gorm.DB, tenant string, hospital uuid.UUID, ids, previous []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	var rows []struct {
		ID     uuid.UUID
		Active bool
	}
	if err := tx.Table("workforce_hr_masters").Select("id,active").Where("tenant_id=? AND hospital_id=? AND kind='relationship' AND id IN ?", tenant, hospital, ids).
		Clauses(clause.Locking{Strength: "SHARE"}).Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) != len(ids) {
		return errBenefitRelationship
	}
	for _, row := range rows {
		if !row.Active && !slices.Contains(previous, row.ID) {
			return errBenefitRelationship
		}
	}
	return nil
}

func (h *handlers) listBenefitPlans(c *gin.Context, params pagination.Params) ([]benefitPlan, error) {
	p, hospital, ok := scope(c)
	if !ok || !benefitPage(c, params) {
		return nil, nil
	}
	var rows []benefitPlan
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := benefitsRead(tx, p.TenantID, hospital); err != nil {
			return err
		}
		q := tx.Table(benefitPlanJoin).Where("p.tenant_id=? AND p.hospital_id=?", p.TenantID, hospital).Order("p.created_at DESC, p.id DESC").Limit(params.Limit + 1)
		if params.After != nil {
			q = q.Where("(p.created_at,p.id)<(?,?)", params.After.CreatedAt, params.After.ID)
		}
		var err error
		if rows, err = loadBenefitPlans(q); err != nil {
			return err
		}
		return benefitAudit(tx, p, hospital, "plans.read", hospital, map[string]any{})
	})
	if err != nil {
		respondBenefitError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) benefitPlanHistory(c *gin.Context, params pagination.Params) ([]benefitPlanRevision, error) {
	p, hospital, ok := scope(c)
	if !ok || !benefitPage(c, params) {
		return nil, nil
	}
	ids, ok := benefitIDs(c, "plan")
	if !ok {
		return nil, nil
	}
	var result []benefitPlanRevision
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := benefitsRead(tx, p.TenantID, hospital); err != nil {
			return err
		}
		if _, err := loadBenefitPlan(tx, p.TenantID, hospital, ids[0], ""); err != nil {
			return err
		}
		q := tx.Table("workforce_benefit_plan_revisions").Select(benefitPlanRevisionColumns).
			Where("tenant_id=? AND hospital_id=? AND plan_id=?", p.TenantID, hospital, ids[0]).Order("created_at DESC, id DESC").Limit(params.Limit + 1)
		if params.After != nil {
			q = q.Where("(created_at,id)<(?,?)", params.After.CreatedAt, params.After.ID)
		}
		var rows []benefitPlanRevisionRow
		if err := q.Find(&rows).Error; err != nil {
			return err
		}
		result = make([]benefitPlanRevision, len(rows))
		for i, row := range rows {
			rels, err := parseUUIDArray(row.Relationships)
			if err != nil {
				return err
			}
			result[i] = row.Revision
			result[i].CoveredRelationships = rels
		}
		return benefitAudit(tx, p, hospital, "plan_history.read", ids[0], map[string]any{})
	})
	if err != nil {
		respondBenefitError(c, err)
		return nil, nil
	}
	return result, nil
}

func (h *handlers) putBenefitPlan(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	ids, ok := benefitIDs(c, "plan")
	if !ok {
		return
	}
	plan := ids[0]
	var in benefitPlanInput
	if !decodeSized(c, &in, 4096) {
		return
	}
	if err := normalizeBenefitPlan(&in); err != nil {
		respondBenefitError(c, err)
		return
	}
	var result benefitPlan
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := benefitsWrite(tx, p, hospital, "plan:"+plan.String()); err != nil {
			return err
		}
		cur, err := loadBenefitPlan(tx, p.TenantID, hospital, plan, "UPDATE")
		exists := err == nil
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if exists && samePlanContent(cur, in) {
			result = cur
			return nil
		}
		switch {
		case cur.Version != *in.ExpectedVersion:
			return errBenefitVersion
		case !exists && in.Status != "active", exists && cur.Code != in.Code:
			return errBenefitInvalid
		case exists && cur.Status == "retired":
			return errBenefitPlanRetired
		}
		if err := requireRelationships(tx, p.TenantID, hospital, in.CoveredRelationships, cur.CoveredRelationships); err != nil {
			return err
		}
		revision, version := uuid.New(), cur.Version+1
		if exists {
			update := tx.Exec(`UPDATE workforce_benefit_plans SET name=?,status=?,effective_from=?::date,effective_until=?::date,version=?,revision_id=?,updated_at=clock_timestamp()
 WHERE tenant_id=? AND hospital_id=? AND id=? AND version=?`, in.Name, in.Status, in.EffectiveFrom, in.EffectiveUntil, version, revision, p.TenantID, hospital, plan, cur.Version)
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected != 1 {
				return errBenefitVersion
			}
		} else if err := tx.Exec(`INSERT INTO workforce_benefit_plans(tenant_id,hospital_id,id,code,name,status,effective_from,effective_until,version,revision_id) VALUES(?,?,?,?,?,?,?::date,?::date,1,?)`,
			p.TenantID, hospital, plan, in.Code, in.Name, in.Status, in.EffectiveFrom, in.EffectiveUntil, revision).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_benefit_plan_revisions(tenant_id,hospital_id,id,plan_id,version,code,name,kind,description,covers_dependents,covered_relationships,effective_from,effective_until,status,changed_by_subject)
 VALUES(?,?,?,?,?,?,?,?,?,?,?::uuid[],?::date,?::date,?,?)`, p.TenantID, hospital, revision, plan, version, in.Code, in.Name, in.Kind, in.Description, *in.CoversDependents,
			uuidArrayLiteral(in.CoveredRelationships), in.EffectiveFrom, in.EffectiveUntil, in.Status, p.Subject).Error; err != nil {
			return err
		}
		if err := tx.Exec(`SET CONSTRAINTS ALL IMMEDIATE`).Error; err != nil {
			return err
		}
		if err := benefitAudit(tx, p, hospital, "plan.changed", plan, map[string]any{"revision_id": revision, "version": version, "status": in.Status}); err != nil {
			return err
		}
		if err := publishBenefit(h, tx, p.TenantID, BenefitPlanChanged, workforcecontract.BenefitPlanChangedData{HospitalID: hospital.String(), PlanID: plan.String(), RevisionID: revision.String(), Version: version, Status: in.Status}); err != nil {
			return err
		}
		result, err = loadBenefitPlan(tx, p.TenantID, hospital, plan, "")
		return err
	})
	if err != nil {
		respondBenefitError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) listDependents(c *gin.Context, params pagination.Params) ([]dependent, error) {
	p, hospital, ok := scope(c)
	if !ok || !benefitPage(c, params) {
		return nil, nil
	}
	ids, ok := benefitIDs(c, "id")
	if !ok {
		return nil, nil
	}
	var rows []dependent
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := benefitsRead(tx, p.TenantID, hospital); err != nil {
			return err
		}
		if err := requireHospitalStaff(tx, p.TenantID, hospital, ids[0]); err != nil {
			return err
		}
		q := tx.Table(dependentJoin).Select(dependentColumns).Where("d.tenant_id=? AND d.hospital_id=? AND d.staff_id=?", p.TenantID, hospital, ids[0]).
			Order("d.created_at DESC, d.id DESC").Limit(params.Limit + 1)
		if params.After != nil {
			q = q.Where("(d.created_at,d.id)<(?,?)", params.After.CreatedAt, params.After.ID)
		}
		if err := q.Find(&rows).Error; err != nil {
			return err
		}
		return benefitAudit(tx, p, hospital, "dependents.read", ids[0], map[string]any{})
	})
	if err != nil {
		respondBenefitError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) dependentHistory(c *gin.Context, params pagination.Params) ([]dependentRevision, error) {
	p, hospital, ok := scope(c)
	if !ok || !benefitPage(c, params) {
		return nil, nil
	}
	ids, ok := benefitIDs(c, "id", "dependent")
	if !ok {
		return nil, nil
	}
	var rows []dependentRevision
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := benefitsRead(tx, p.TenantID, hospital); err != nil {
			return err
		}
		if _, err := loadDependent(tx, p.TenantID, hospital, ids[0], ids[1], ""); err != nil {
			return err
		}
		q := tx.Table(dependentRevisionJoin).Select(dependentRevisionColumns).Where("r.tenant_id=? AND r.hospital_id=? AND r.dependent_id=?", p.TenantID, hospital, ids[1]).
			Order("r.created_at DESC, r.id DESC").Limit(params.Limit + 1)
		if params.After != nil {
			q = q.Where("(r.created_at,r.id)<(?,?)", params.After.CreatedAt, params.After.ID)
		}
		if err := q.Find(&rows).Error; err != nil {
			return err
		}
		return benefitAudit(tx, p, hospital, "dependent_history.read", ids[1], map[string]any{"staff_id": ids[0]})
	})
	if err != nil {
		respondBenefitError(c, err)
		return nil, nil
	}
	return rows, nil
}

// Mirrors table checks so a malformed dependent is a 400 before any write.
func normalizeDependent(in *dependentInput) error {
	in.FullName, in.Phone, in.Reason = strings.TrimSpace(in.FullName), strings.TrimSpace(in.Phone), strings.TrimSpace(in.Reason)
	today := time.Now().UTC().AddDate(0, 0, 1).Format(time.DateOnly)
	if !validBenefitText(in.FullName, 1, 150) || !validBenefitText(in.Reason, 1, 500) || in.BirthDate == "" || !validDate(in.BirthDate) ||
		in.BirthDate > today || (in.Phone != "" && !benefitPhone.MatchString(in.Phone)) || in.RelationshipRevisionID == uuid.Nil {
		return errBenefitInvalid
	}
	return nil
}

func (h *handlers) putDependent(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	ids, ok := benefitIDs(c, "id", "dependent")
	if !ok {
		return
	}
	staff, id := ids[0], ids[1]
	var in dependentInput
	if !decodeSized(c, &in, 4096) {
		return
	}
	if err := normalizeDependent(&in); err != nil {
		respondBenefitError(c, err)
		return
	}
	var result dependent
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := benefitsWrite(tx, p, hospital, "staff:"+staff.String()); err != nil {
			return err
		}
		if err := requireHospitalStaff(tx, p.TenantID, hospital, staff); err != nil {
			return err
		}
		cur, err := loadDependent(tx, p.TenantID, hospital, staff, id, "UPDATE")
		exists := err == nil
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if exists && cur.FullName == in.FullName && cur.RelationshipRevisionID == in.RelationshipRevisionID && cur.BirthDate == in.BirthDate && cur.Phone == in.Phone && cur.Status == in.Status {
			result = cur
			return nil
		}
		if cur.Version != *in.ExpectedVersion {
			return errBenefitVersion
		}
		if !exists && in.Status != "active" {
			return errBenefitInvalid
		}
		var rel struct {
			MasterID       uuid.UUID
			Active         bool
			RevisionActive bool
			CurrentID      uuid.UUID
		}
		q := tx.Raw(`SELECT r.master_id,m.active,r.active AS revision_active,m.revision_id AS current_id FROM workforce_hr_master_revisions r
 JOIN workforce_hr_masters m ON (m.tenant_id,m.hospital_id,m.id)=(r.tenant_id,r.hospital_id,r.master_id)
 WHERE r.tenant_id=? AND r.hospital_id=? AND r.id=? AND r.kind='relationship' FOR SHARE OF m`, p.TenantID, hospital, in.RelationshipRevisionID).Scan(&rel)
		if q.Error != nil {
			return q.Error
		}
		if q.RowsAffected == 0 || (in.RelationshipRevisionID != cur.RelationshipRevisionID && (!rel.Active || !rel.RevisionActive || rel.CurrentID != in.RelationshipRevisionID)) {
			return errBenefitRelationship
		}
		revision, version := uuid.New(), cur.Version+1
		if exists {
			update := tx.Exec(`UPDATE workforce_staff_dependents SET full_name=?,relationship_master_id=?,birth_date=?::date,status=?,version=?,revision_id=?,updated_at=clock_timestamp()
 WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND id=? AND version=?`, in.FullName, rel.MasterID, in.BirthDate, in.Status, version, revision, p.TenantID, hospital, staff, id, cur.Version)
			if update.Error != nil {
				return update.Error
			}
			if update.RowsAffected != 1 {
				return errBenefitVersion
			}
		} else if err := tx.Exec(`INSERT INTO workforce_staff_dependents(tenant_id,hospital_id,staff_id,id,full_name,relationship_master_id,birth_date,status,version,revision_id) VALUES(?,?,?,?,?,?,?::date,?,1,?)`,
			p.TenantID, hospital, staff, id, in.FullName, rel.MasterID, in.BirthDate, in.Status, revision).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_staff_dependent_revisions(tenant_id,hospital_id,id,staff_id,dependent_id,version,full_name,relationship_revision_id,relationship_master_id,birth_date,phone,status,reason,changed_by_subject)
 VALUES(?,?,?,?,?,?,?,?,?,?::date,?,?,?,?)`, p.TenantID, hospital, revision, staff, id, version, in.FullName, in.RelationshipRevisionID, rel.MasterID, in.BirthDate, in.Phone, in.Status, in.Reason, p.Subject).Error; err != nil {
			return err
		}
		if err := tx.Exec(`SET CONSTRAINTS ALL IMMEDIATE`).Error; err != nil {
			return err
		}
		if err := benefitAudit(tx, p, hospital, "dependent.changed", id, map[string]any{"staff_id": staff, "revision_id": revision, "version": version, "status": in.Status}); err != nil {
			return err
		}
		if err := publishBenefit(h, tx, p.TenantID, DependentChanged, workforcecontract.DependentChangedData{HospitalID: hospital.String(), StaffID: staff.String(), DependentID: id.String(), RevisionID: revision.String(), Version: version, Status: in.Status}); err != nil {
			return err
		}
		result, err = loadDependent(tx, p.TenantID, hospital, staff, id, "")
		return err
	})
	if err != nil {
		respondBenefitError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) listBenefitEnrollments(c *gin.Context, params pagination.Params) ([]benefitEnrollment, error) {
	p, hospital, ok := scope(c)
	if !ok || !benefitPage(c, params) {
		return nil, nil
	}
	ids, ok := benefitIDs(c, "id")
	if !ok {
		return nil, nil
	}
	staff := ids[0]
	var rows []benefitEnrollment
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := benefitsRead(tx, p.TenantID, hospital); err != nil {
			return err
		}
		if err := requireHospitalStaff(tx, p.TenantID, hospital, staff); err != nil {
			return err
		}
		// An enrollment exists once it holds approved coverage or a pending proposal; rejected or withdrawn first attempts are history only.
		q := tx.Table("workforce_benefit_enrollment_proposals").Select("enrollment_id AS id,staff_id,plan_id,dependent_id,min(created_at) AS created_at").
			Where("tenant_id=? AND hospital_id=? AND staff_id=?", p.TenantID, hospital, staff).Group("enrollment_id,staff_id,plan_id,dependent_id").
			Having("bool_or(status IN ('pending','approved'))").Order("created_at DESC, id DESC").Limit(params.Limit + 1)
		if params.After != nil {
			q = q.Having("(min(created_at),enrollment_id)<(?,?)", params.After.CreatedAt, params.After.ID)
		}
		if err := q.Find(&rows).Error; err != nil {
			return err
		}
		page := make([]uuid.UUID, len(rows))
		for i := range rows {
			page[i] = rows[i].ID
		}
		if len(page) > 0 {
			coverage, err := loadBenefitCoverage(tx, p.TenantID, hospital, page, false)
			if err != nil {
				return err
			}
			var pending []benefitProposal
			if err := tx.Table("workforce_benefit_enrollment_proposals").Select(benefitProposalColumns).
				Where("tenant_id=? AND hospital_id=? AND enrollment_id IN ? AND status='pending'", p.TenantID, hospital, page).Find(&pending).Error; err != nil {
				return err
			}
			for i := range rows {
				if row, ok := coverage[rows[i].ID]; ok {
					rows[i].Coverage = &row.Coverage
				}
				for j := range pending {
					if pending[j].EnrollmentID == rows[i].ID {
						rows[i].Pending = &pending[j]
					}
				}
			}
		}
		return benefitAudit(tx, p, hospital, "enrollments.read", staff, map[string]any{})
	})
	if err != nil {
		respondBenefitError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) benefitEnrollmentHistory(c *gin.Context, params pagination.Params) ([]benefitProposal, error) {
	p, hospital, ok := scope(c)
	if !ok || !benefitPage(c, params) {
		return nil, nil
	}
	ids, ok := benefitIDs(c, "id", "enrollment")
	if !ok {
		return nil, nil
	}
	var rows []benefitProposal
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := benefitsRead(tx, p.TenantID, hospital); err != nil {
			return err
		}
		var known bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_benefit_enrollment_proposals WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND enrollment_id=?)`,
			p.TenantID, hospital, ids[0], ids[1]).Scan(&known).Error; err != nil {
			return err
		}
		if !known {
			return gorm.ErrRecordNotFound
		}
		q := tx.Table("workforce_benefit_enrollment_proposals").Select(benefitProposalColumns).
			Where("tenant_id=? AND hospital_id=? AND staff_id=? AND enrollment_id=?", p.TenantID, hospital, ids[0], ids[1]).Order("created_at DESC, id DESC").Limit(params.Limit + 1)
		if params.After != nil {
			q = q.Where("(created_at,id)<(?,?)", params.After.CreatedAt, params.After.ID)
		}
		if err := q.Find(&rows).Error; err != nil {
			return err
		}
		return benefitAudit(tx, p, hospital, "enrollment_history.read", ids[1], map[string]any{"staff_id": ids[0]})
	})
	if err != nil {
		respondBenefitError(c, err)
		return nil, nil
	}
	return rows, nil
}

func normalizeBenefitProposal(in *benefitProposalInput) error {
	in.EligibilityBasis = strings.TrimSpace(in.EligibilityBasis)
	until := ""
	if in.CoverageUntil != nil {
		until = *in.CoverageUntil
	}
	if !validBenefitText(in.EligibilityBasis, 1, 500) || in.PlanID == uuid.Nil || (in.DependentID != nil && *in.DependentID == uuid.Nil) ||
		in.CoverageFrom == "" || (in.CoverageUntil != nil && until == "") || !validPeriod(in.CoverageFrom, until) || in.CoverageFrom < "2000-01-01" {
		return errBenefitInvalid
	}
	return nil
}

func sameBenefitProposal(r benefitProposal, in benefitProposalInput, staff, enrollment uuid.UUID, subject string) bool {
	return r.StaffID == staff && r.EnrollmentID == enrollment && r.ProposedBySubject == subject && r.BaseVersion == *in.ExpectedVersion &&
		r.PlanID == in.PlanID && equalOptional(r.DependentID, in.DependentID) && r.CoverageFrom == in.CoverageFrom &&
		equalOptional(r.CoverageUntil, in.CoverageUntil) && r.EligibilityBasis == in.EligibilityBasis
}

// lockBenefitSubjects holds the staff member, plan and dependent a proposal cites so no concurrent change slips past its checks.
func lockBenefitSubjects(tx *gorm.DB, tenant string, hospital, staff, plan uuid.UUID, dependentID *uuid.UUID) (planRevision uuid.UUID, dependentRevision *uuid.UUID, err error) {
	if err := tx.Table("workforce_staff").Select("id").Where("tenant_id=? AND hospital_id=? AND id=?", tenant, hospital, staff).
		Clauses(clause.Locking{Strength: "SHARE"}).Take(&struct{ ID uuid.UUID }{}).Error; err != nil {
		return uuid.Nil, nil, err
	}
	current, err := loadBenefitPlan(tx, tenant, hospital, plan, "SHARE")
	if err != nil {
		return uuid.Nil, nil, err
	}
	if dependentID == nil {
		return current.RevisionID, nil, nil
	}
	dep, err := loadDependent(tx, tenant, hospital, staff, *dependentID, "SHARE")
	if err != nil {
		return uuid.Nil, nil, err
	}
	return current.RevisionID, &dep.RevisionID, nil
}

func recordBenefitProposal(h *handlers, tx *gorm.DB, p authn.Principal, hospital uuid.UUID, r benefitProposal, status string, version int64) error {
	if err := benefitAudit(tx, p, hospital, "enrollment."+status, r.EnrollmentID, map[string]any{"staff_id": r.StaffID, "proposal_id": r.ID, "version": version}); err != nil {
		return err
	}
	return publishBenefit(h, tx, p.TenantID, BenefitEnrollmentChanged, workforcecontract.BenefitEnrollmentChangedData{HospitalID: hospital.String(), StaffID: r.StaffID.String(),
		EnrollmentID: r.EnrollmentID.String(), ProposalID: r.ID.String(), Status: status, Version: version})
}

func (h *handlers) putBenefitProposal(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	ids, ok := benefitIDs(c, "id", "enrollment", "proposal")
	if !ok {
		return
	}
	staff, enrollment, proposal := ids[0], ids[1], ids[2]
	var in benefitProposalInput
	if !decodeSized(c, &in, 4096) {
		return
	}
	if err := normalizeBenefitProposal(&in); err != nil {
		respondBenefitError(c, err)
		return
	}
	var result benefitProposal
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := benefitsWrite(tx, p, hospital, "staff:"+staff.String()); err != nil {
			return err
		}
		if err := requireHospitalStaff(tx, p.TenantID, hospital, staff); err != nil {
			return err
		}
		var existing []benefitProposal
		if err := tx.Table("workforce_benefit_enrollment_proposals").Select(benefitProposalColumns).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, proposal).Find(&existing).Error; err != nil {
			return err
		}
		if len(existing) == 1 {
			if !sameBenefitProposal(existing[0], in, staff, enrollment, p.Subject) {
				return errBenefitReplay
			}
			result = existing[0]
			return nil
		}
		coverage, err := loadBenefitCoverage(tx, p.TenantID, hospital, []uuid.UUID{enrollment}, true)
		if err != nil {
			return err
		}
		cur, approved := coverage[enrollment]
		if cur.Coverage.Version != *in.ExpectedVersion {
			return errBenefitVersion
		}
		if approved && (cur.PlanID != in.PlanID || !equalOptional(cur.DependentID, in.DependentID)) {
			return errBenefitIdentity
		}
		if approved && cur.Coverage.CoverageFrom == in.CoverageFrom && equalOptional(cur.Coverage.CoverageUntil, in.CoverageUntil) {
			return errBenefitUnchanged
		}
		var pending bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_benefit_enrollment_proposals WHERE tenant_id=? AND hospital_id=? AND enrollment_id=? AND status='pending')`, p.TenantID, hospital, enrollment).Scan(&pending).Error; err != nil {
			return err
		}
		if pending {
			return errBenefitPending
		}
		planRevision, dependentRevision, err := lockBenefitSubjects(tx, p.TenantID, hospital, staff, in.PlanID, in.DependentID)
		if err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_benefit_enrollment_proposals(tenant_id,hospital_id,id,enrollment_id,staff_id,plan_id,plan_revision_id,dependent_id,dependent_revision_id,base_version,status,coverage_from,coverage_until,eligibility_basis,proposed_by_subject,decision_note)
 VALUES(?,?,?,?,?,?,?,?,?,?,'pending',?::date,?::date,?,?,'')`, p.TenantID, hospital, proposal, enrollment, staff, in.PlanID, planRevision, in.DependentID, dependentRevision,
			cur.Coverage.Version, in.CoverageFrom, in.CoverageUntil, in.EligibilityBasis, p.Subject).Error; err != nil {
			return err
		}
		if result, err = loadBenefitProposal(tx, p.TenantID, hospital, staff, enrollment, proposal, false); err != nil {
			return err
		}
		if err := recordBenefitProposal(h, tx, p, hospital, result, "pending", cur.Coverage.Version); err != nil {
			return err
		}
		return tx.Exec(`SET CONSTRAINTS ALL IMMEDIATE`).Error
	})
	if err != nil {
		respondBenefitError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) decideBenefitProposal(c *gin.Context) {
	var in benefitDecisionInput
	if !decodeSized(c, &in, 2048) {
		return
	}
	to := map[string]string{"approve": "approved", "reject": "rejected"}[in.Decision]
	h.transitionBenefitProposal(c, in.ExpectedVersion, to, in.Note)
}

func (h *handlers) withdrawBenefitProposal(c *gin.Context) {
	var in benefitWithdrawInput
	if !decodeSized(c, &in, 2048) {
		return
	}
	h.transitionBenefitProposal(c, nil, "withdrawn", in.Note)
}

// transitionBenefitProposal decides a pending proposal once; approval rechecks it in the database and writes the coverage in force.
func (h *handlers) transitionBenefitProposal(c *gin.Context, expected *int64, to, note string) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	ids, ok := benefitIDs(c, "id", "enrollment", "proposal")
	if !ok {
		return
	}
	staff, enrollment, proposal := ids[0], ids[1], ids[2]
	note = strings.TrimSpace(note)
	if !validBenefitText(note, 0, 500) {
		respondBenefitError(c, errBenefitInvalid)
		return
	}
	if to == "rejected" && note == "" {
		respondBenefitError(c, errBenefitNoteRequired)
		return
	}
	var result benefitProposal
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := benefitsWrite(tx, p, hospital, "staff:"+staff.String()); err != nil {
			return err
		}
		r, err := loadBenefitProposal(tx, p.TenantID, hospital, staff, enrollment, proposal, true)
		if err != nil {
			return err
		}
		if r.Status == to && r.DecidedBySubject != nil && *r.DecidedBySubject == p.Subject && r.DecisionNote == note {
			result = r
			return nil
		}
		if r.Status != "pending" {
			return errBenefitState
		}
		coverage, err := loadBenefitCoverage(tx, p.TenantID, hospital, []uuid.UUID{enrollment}, true)
		if err != nil {
			return err
		}
		cur, approved := coverage[enrollment]
		if cur.Coverage.Version != r.BaseVersion || (expected != nil && cur.Coverage.Version != *expected) {
			return errBenefitVersion
		}
		if to == "withdrawn" && r.ProposedBySubject != p.Subject {
			return errBenefitNotProposer
		}
		if to != "withdrawn" && r.ProposedBySubject == p.Subject {
			return errBenefitSelf
		}
		version := cur.Coverage.Version
		set := "status=?,decided_by_subject=?,decided_at=clock_timestamp(),decision_note=?"
		if to == "approved" {
			set += ",approved_version=base_version+1"
			version++
			if _, _, err := lockBenefitSubjects(tx, p.TenantID, hospital, staff, r.PlanID, r.DependentID); err != nil {
				return err
			}
			if err := tx.Exec(`SELECT set_config('app.workforce_benefit_approval','authorized',true)`).Error; err != nil {
				return err
			}
		}
		update := tx.Exec(`UPDATE workforce_benefit_enrollment_proposals SET `+set+` WHERE tenant_id=? AND hospital_id=? AND id=? AND status='pending'`, to, p.Subject, note, p.TenantID, hospital, proposal)
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return errBenefitState
		}
		if to == "approved" {
			if err := applyBenefitCoverage(tx, p.TenantID, hospital, r, approved, version); err != nil {
				return err
			}
		}
		if err := recordBenefitProposal(h, tx, p, hospital, r, to, version); err != nil {
			return err
		}
		if err := tx.Exec(`SET CONSTRAINTS ALL IMMEDIATE`).Error; err != nil {
			return err
		}
		result, err = loadBenefitProposal(tx, p.TenantID, hospital, staff, enrollment, proposal, false)
		return err
	})
	if err != nil {
		respondBenefitError(c, err)
		return
	}
	respond.OK(c, result)
}

// applyBenefitCoverage copies the approved proposal into the coverage in force; the first approval pins the plan revision.
func applyBenefitCoverage(tx *gorm.DB, tenant string, hospital uuid.UUID, r benefitProposal, approved bool, version int64) error {
	if !approved {
		return tx.Exec(`INSERT INTO workforce_benefit_enrollments(tenant_id,hospital_id,id,staff_id,plan_id,original_plan_revision_id,dependent_id,coverage_from,coverage_until,version,proposal_id) VALUES(?,?,?,?,?,?,?,?::date,?::date,1,?)`,
			tenant, hospital, r.EnrollmentID, r.StaffID, r.PlanID, r.PlanRevisionID, r.DependentID, r.CoverageFrom, r.CoverageUntil, r.ID).Error
	}
	update := tx.Exec(`UPDATE workforce_benefit_enrollments SET coverage_from=?::date,coverage_until=?::date,version=?,proposal_id=?,updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND id=? AND version=?`,
		r.CoverageFrom, r.CoverageUntil, version, r.ID, tenant, hospital, r.EnrollmentID, version-1)
	if update.Error != nil {
		return update.Error
	}
	if update.RowsAffected != 1 {
		return errBenefitVersion
	}
	return nil
}

func respondBenefitError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	isPG := errors.As(err, &pg)
	if isPG {
		if reason, ok := benefitConstraintReasons[pg.ConstraintName]; ok {
			respond.ConflictWithDetail(c, reason[1], gin.H{"reason": reason[0]})
			return
		}
	}
	switch {
	case errors.Is(err, errBenefitInvalid), isPG && pg.ConstraintName == "workforce_benefit_plan_relationship_unknown":
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Check the highlighted fields.")
	case errors.Is(err, errBenefitIdentity), isPG && pg.ConstraintName == "workforce_benefit_enrollment_identity":
		respond.Error(c, http.StatusBadRequest, "invalid_request", "An enrollment keeps its plan and the person it covers.")
	case errors.Is(err, errBenefitNoteRequired):
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Say why you reject the coverage.")
	case errors.Is(err, errBenefitRelationship):
		respond.Error(c, http.StatusBadRequest, "invalid_request", "Choose a current, active relationship.")
	case errors.Is(err, errBenefitVersion):
		respond.ConflictWithDetail(c, "This record changed since you opened it. Reload before saving.", gin.H{"reason": "version_conflict"})
	case errors.Is(err, errBenefitPlanRetired):
		respond.ConflictWithDetail(c, "A retired plan cannot change.", gin.H{"reason": "plan_retired"})
	case errors.Is(err, errBenefitPending):
		respond.ConflictWithDetail(c, "Another proposal for this enrollment is waiting for a decision.", gin.H{"reason": "proposal_pending"})
	case errors.Is(err, errBenefitReplay):
		respond.ConflictWithDetail(c, "This proposal was already saved with different details.", gin.H{"reason": "proposal_conflict"})
	case errors.Is(err, errBenefitUnchanged):
		respond.ConflictWithDetail(c, "The proposal would leave the coverage as it is.", gin.H{"reason": "nothing_to_change"})
	case errors.Is(err, errBenefitSelf):
		respond.ConflictWithDetail(c, "The person who proposed the coverage must not approve or reject it.", gin.H{"reason": "self_review"})
	case errors.Is(err, errBenefitNotProposer):
		respond.ConflictWithDetail(c, "Only the person who proposed the coverage can withdraw it.", gin.H{"reason": "not_proposer"})
	case errors.Is(err, errBenefitState):
		respond.ConflictWithDetail(c, "This proposal has already been decided.", gin.H{"reason": "not_pending"})
	case isPG && pg.Code == "23505":
		requestid.Logger(c).WarnContext(c.Request.Context(), "benefit uniqueness conflict", "constraint", pg.ConstraintName)
		respond.ConflictWithDetail(c, "This record conflicts with one already saved.", gin.H{"reason": "conflict"})
	default:
		respondError(c, err)
	}
}

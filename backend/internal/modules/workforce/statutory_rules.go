package workforce

import (
	"encoding/json"
	"errors"
	"reflect"
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
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func (r statutoryRule) PageKey() (time.Time, uuid.UUID)     { return r.CreatedAt, r.ID }
func (r statutoryRevision) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

var errStatutoryInvalid = errors.New("statutory rule is invalid")
var errStatutoryVersion = errors.New("statutory rule version conflict")
var errStatutoryPending = errors.New("a statutory proposal is already pending")
var errStatutoryState = errors.New("statutory proposal is not pending")
var errStatutorySelf = errors.New("statutory proposal decided by its author")
var errStatutoryNotProposer = errors.New("statutory proposal withdrawn by someone else")
var errStatutoryNoteRequired = errors.New("statutory rejection needs a note")

// Shares the salary lock so a preview-consistent configuration is always serialised.
const statutoryLock = salaryConfigLock

const statutoryRevisionColumns = "id,rule_id,kind,sequence,base_version,status,approved_version,name,covered,effective_on::text AS effective_on,citation,reference,proposed_by_subject,decided_by_subject,decided_at,decision_note,created_at"

type statutoryParent struct {
	ID          uuid.UUID
	Kind        string
	Version     int64
	RevisionID  *uuid.UUID
	EffectiveOn *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const statutoryParentColumns = "id,kind,version,revision_id,effective_on::text AS effective_on,created_at,updated_at"

func loadStatutoryRevisions(tx *gorm.DB, tenant string, hospital uuid.UUID, q *gorm.DB) ([]statutoryRevision, error) {
	rows := []statutoryRevision{}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(rows))
	index := make(map[uuid.UUID]int, len(rows))
	for i, r := range rows {
		ids[i], index[r.ID] = r.ID, i
	}
	if len(ids) == 0 {
		return rows, nil
	}
	var pf []struct {
		RevisionID uuid.UUID
		Params     statutoryPfParams `gorm:"embedded"`
	}
	if err := tx.Table("workforce_statutory_pf_params").Where("tenant_id=? AND hospital_id=? AND revision_id IN ?", tenant, hospital, ids).Find(&pf).Error; err != nil {
		return nil, err
	}
	for _, p := range pf {
		params := p.Params
		rows[index[p.RevisionID]].Pf = &params
	}
	var esi []struct {
		RevisionID      uuid.UUID
		PeriodStartMask int
		Params          statutoryEsiParams `gorm:"embedded"`
	}
	if err := tx.Table("workforce_statutory_esi_params").Where("tenant_id=? AND hospital_id=? AND revision_id IN ?", tenant, hospital, ids).Find(&esi).Error; err != nil {
		return nil, err
	}
	for _, e := range esi {
		params := e.Params
		params.PeriodStartMonths = monthsFromMask(e.PeriodStartMask)
		rows[index[e.RevisionID]].Esi = &params
	}
	return rows, nil
}

func statutoryRules(tx *gorm.DB, tenant string, hospital uuid.UUID, parents []statutoryParent) ([]statutoryRule, error) {
	out := make([]statutoryRule, len(parents))
	ids := make([]uuid.UUID, len(parents))
	index := make(map[uuid.UUID]int, len(parents))
	for i, p := range parents {
		out[i] = statutoryRule{ID: p.ID, Kind: p.Kind, Version: p.Version, EffectiveOn: p.EffectiveOn, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
		ids[i], index[p.ID] = p.ID, i
	}
	if len(ids) == 0 {
		return out, nil
	}
	revisions, err := loadStatutoryRevisions(tx, tenant, hospital, tx.Table("workforce_statutory_rule_revisions").Select(statutoryRevisionColumns).
		Where("tenant_id=? AND hospital_id=? AND rule_id IN ? AND (status='pending' OR id IN (SELECT revision_id FROM workforce_statutory_rules WHERE tenant_id=? AND hospital_id=? AND id IN ?))", tenant, hospital, ids, tenant, hospital, ids))
	if err != nil {
		return nil, err
	}
	for _, r := range revisions {
		revision, rule := r, &out[index[r.RuleID]]
		if r.Status == "pending" {
			rule.Pending = &revision
		} else {
			rule.Current = &revision
		}
	}
	return out, nil
}

func loadStatutoryRule(tx *gorm.DB, tenant string, hospital, id uuid.UUID, lock bool) (statutoryRule, statutoryParent, error) {
	var parent statutoryParent
	q := tx.Table("workforce_statutory_rules").Select(statutoryParentColumns).Where("tenant_id=? AND hospital_id=? AND id=?", tenant, hospital, id)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.Take(&parent).Error; err != nil {
		return statutoryRule{}, parent, err
	}
	rules, err := statutoryRules(tx, tenant, hospital, []statutoryParent{parent})
	if err != nil {
		return statutoryRule{}, parent, err
	}
	return rules[0], parent, nil
}

func (h *handlers) listStatutoryRules(c *gin.Context, params pagination.Params) ([]statutoryRule, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, 400, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	rows := []statutoryRule{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var parents []statutoryParent
		if err := tenantdb.ApplyKeyset(tx.Table("workforce_statutory_rules"), params).Select(statutoryParentColumns).Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Find(&parents).Error; err != nil {
			return err
		}
		var err error
		rows, err = statutoryRules(tx, p.TenantID, hospital, parents)
		return err
	})
	if err != nil {
		respondStatutoryError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) statutoryRuleHistory(c *gin.Context, params pagination.Params) ([]statutoryRevision, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "statutory rule")
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, 400, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	rows := []statutoryRevision{}
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		if err := tx.Table("workforce_statutory_rules").Select("id").Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, id).Take(&struct{ ID uuid.UUID }{}).Error; err != nil {
			return err
		}
		var err error
		rows, err = loadStatutoryRevisions(tx, p.TenantID, hospital, tenantdb.ApplyKeyset(tx.Table("workforce_statutory_rule_revisions"), params).Select(statutoryRevisionColumns).Where("tenant_id=? AND hospital_id=? AND rule_id=?", p.TenantID, hospital, id))
		return err
	})
	if err != nil {
		respondStatutoryError(c, err)
		return nil, nil
	}
	return rows, nil
}

func validStatutoryText(value string) bool { return !strings.ContainsRune(value, 0) }

// Mirrors the table checks so a malformed proposal is a 400 before any write; the database still enforces every rule.
func normalizeStatutoryRule(in *statutoryRuleInput) error {
	in.Name, in.Citation, in.Reference = collapseSpaces(in.Name), strings.TrimSpace(in.Citation), strings.TrimSpace(in.Reference)
	if !contains(in.Kind, statutoryKinds...) || !validSalaryDate(in.EffectiveOn) || in.Name == "" || in.Citation == "" ||
		!validStatutoryText(in.Name) || !validStatutoryText(in.Citation) || !validStatutoryText(in.Reference) {
		return errStatutoryInvalid
	}
	// Coverage is explicit: a covered revision carries exactly its own kind's parameters, an uncovered one none.
	if (in.Pf != nil) != (*in.Covered && in.Kind == "pf") || (in.Esi != nil) != (*in.Covered && in.Kind == "esi") {
		return errStatutoryInvalid
	}
	if pf := in.Pf; pf != nil && (*pf.EpsRatePpm > *pf.EmployerRatePpm || *pf.EpsWageCeilingPaise > *pf.WageCeilingPaise) {
		return errStatutoryInvalid
	}
	if esi := in.Esi; esi != nil {
		slices.Sort(esi.PeriodStartMonths)
		if *esi.DisabilityWageCeilingPaise < *esi.WageCeilingPaise || len(slices.Compact(slices.Clone(esi.PeriodStartMonths))) != len(esi.PeriodStartMonths) {
			return errStatutoryInvalid
		}
	}
	return nil
}

func statutoryInputFromRevision(r statutoryRevision, expected int64) statutoryRuleInput {
	covered := r.Covered
	return statutoryRuleInput{ExpectedVersion: &expected, Kind: r.Kind, Name: r.Name, Covered: &covered, EffectiveOn: r.EffectiveOn, Citation: r.Citation, Reference: r.Reference, Pf: r.Pf, Esi: r.Esi}
}

func (h *handlers) putStatutoryRule(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "statutory rule")
		return
	}
	var in statutoryRuleInput
	if !decodeSized(c, &in, 8192) {
		return
	}
	if err := normalizeStatutoryRule(&in); err != nil {
		respondStatutoryError(c, err)
		return
	}
	var result statutoryRule
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_payroll", statutoryLock); err != nil {
			return err
		}
		rule, parent, err := loadStatutoryRule(tx, p.TenantID, hospital, id, true)
		switch {
		case err == nil:
			if parent.Kind != in.Kind {
				return errStatutoryVersion
			}
			// Replaying either the pending proposal or the revision now in force returns the rule unchanged.
			for _, r := range []*statutoryRevision{rule.Pending, rule.Current} {
				if r != nil && reflect.DeepEqual(statutoryInputFromRevision(*r, *in.ExpectedVersion), in) {
					result = rule
					return nil
				}
			}
			if rule.Pending != nil {
				return errStatutoryPending
			}
			if parent.Version != *in.ExpectedVersion {
				return errStatutoryVersion
			}
			if parent.EffectiveOn != nil && in.EffectiveOn < *parent.EffectiveOn {
				return errPolicyEffective
			}
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		case *in.ExpectedVersion != 0:
			return gorm.ErrRecordNotFound
		default:
			if err := tx.Exec(`INSERT INTO workforce_statutory_rules(tenant_id,hospital_id,id,kind,version) VALUES(?,?,?,?,0)`, p.TenantID, hospital, id, in.Kind).Error; err != nil {
				return err
			}
		}
		revision := uuid.New()
		if err := tx.Exec(`INSERT INTO workforce_statutory_rule_revisions(tenant_id,hospital_id,id,rule_id,kind,sequence,base_version,status,name,covered,effective_on,citation,reference,proposed_by_subject,decision_note)
 SELECT ?,?,?,?,?,coalesce((SELECT max(sequence) FROM workforce_statutory_rule_revisions WHERE tenant_id=? AND hospital_id=? AND rule_id=?),0)+1,?,'pending',?,?,?,?,?,?,''`,
			p.TenantID, hospital, revision, id, in.Kind, p.TenantID, hospital, id, *in.ExpectedVersion, in.Name, *in.Covered, in.EffectiveOn, in.Citation, in.Reference, p.Subject).Error; err != nil {
			return err
		}
		if pf := in.Pf; pf != nil {
			if err := tx.Exec(`INSERT INTO workforce_statutory_pf_params(tenant_id,hospital_id,revision_id,wage_basis,contribution_basis,wage_ceiling_paise,employee_rate_ppm,employer_rate_ppm,eps_rate_ppm,eps_wage_ceiling_paise,edli_rate_ppm,edli_wage_ceiling_paise,admin_rate_ppm,admin_minimum_paise,rounding_mode,rounding_unit_paise)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.TenantID, hospital, revision, pf.WageBasis, pf.ContributionBasis, *pf.WageCeilingPaise, *pf.EmployeeRatePpm, *pf.EmployerRatePpm, *pf.EpsRatePpm,
				*pf.EpsWageCeilingPaise, *pf.EdliRatePpm, *pf.EdliWageCeilingPaise, *pf.AdminRatePpm, *pf.AdminMinimumPaise, pf.RoundingMode, *pf.RoundingUnitPaise).Error; err != nil {
				return err
			}
		}
		if esi := in.Esi; esi != nil {
			if err := tx.Exec(`INSERT INTO workforce_statutory_esi_params(tenant_id,hospital_id,revision_id,wage_basis,wage_ceiling_paise,disability_wage_ceiling_paise,employee_rate_ppm,employer_rate_ppm,employee_exempt_daily_paise,period_start_mask,rounding_mode,rounding_unit_paise)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, p.TenantID, hospital, revision, esi.WageBasis, *esi.WageCeilingPaise, *esi.DisabilityWageCeilingPaise, *esi.EmployeeRatePpm, *esi.EmployerRatePpm,
				esi.EmployeeExemptDailyPaise, monthMask(esi.PeriodStartMonths), esi.RoundingMode, *esi.RoundingUnitPaise).Error; err != nil {
				return err
			}
		}
		if err := recordStatutoryChange(h, tx, p.TenantID, hospital, p.Subject, in.Kind, "proposed", id, revision, *in.ExpectedVersion); err != nil {
			return err
		}
		result, _, err = loadStatutoryRule(tx, p.TenantID, hospital, id, false)
		return err
	})
	if err != nil {
		respondStatutoryError(c, err)
		return
	}
	respond.OK(c, result)
}

// Audit rows and events carry identifiers, kind, status and version only; rates, ceilings and citations stay behind the API.
func recordStatutoryChange(h *handlers, tx *gorm.DB, tenant string, hospital uuid.UUID, subject, kind, status string, id, revision uuid.UUID, version int64) error {
	if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,jsonb_build_object('revision_id',?::uuid,'kind',?::text,'version',?::bigint))`,
		tenant, hospital, subject, "workforce.statutory_rule."+status, id, revision, kind, version).Error; err != nil {
		return err
	}
	data, err := json.Marshal(workforcecontract.StatutoryRuleChangedData{HospitalID: hospital.String(), RuleID: id.String(), RevisionID: revision.String(), Kind: kind, Status: status, Version: version})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, StatutoryRuleChanged, events.Event{Type: StatutoryRuleChanged, Version: 1, TenantID: tenant, Data: data})
}

func (h *handlers) decideStatutoryRule(c *gin.Context) {
	var in statutoryDecisionInput
	if !decodeSized(c, &in, 2048) {
		return
	}
	to := map[string]string{"approve": "approved", "reject": "rejected"}[in.Decision]
	h.transitionStatutory(c, in.ProposalID, in.ExpectedVersion, to, in.Note)
}

func (h *handlers) withdrawStatutoryRule(c *gin.Context) {
	var in statutoryWithdrawInput
	if !decodeSized(c, &in, 2048) {
		return
	}
	h.transitionStatutory(c, in.ProposalID, nil, "withdrawn", in.Note)
}

// transitionStatutory decides a pending proposal once; the same actor replaying the same decision gets the result back.
func (h *handlers) transitionStatutory(c *gin.Context, proposal uuid.UUID, expected *int64, to, note string) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil || proposal == uuid.Nil {
		respond.NotFound(c, "statutory proposal")
		return
	}
	note = strings.TrimSpace(note)
	if !validStatutoryText(note) {
		respondStatutoryError(c, errStatutoryInvalid)
		return
	}
	if to == "rejected" && note == "" {
		respondStatutoryError(c, errStatutoryNoteRequired)
		return
	}
	var result statutoryRule
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_payroll", statutoryLock); err != nil {
			return err
		}
		_, parent, err := loadStatutoryRule(tx, p.TenantID, hospital, id, true)
		if err != nil {
			return err
		}
		var r struct {
			Status            string
			BaseVersion       int64
			EffectiveOn       string
			ProposedBySubject string
			DecidedBySubject  *string
			DecisionNote      string
		}
		if err := tx.Table("workforce_statutory_rule_revisions").Select("status,base_version,effective_on::text AS effective_on,proposed_by_subject,decided_by_subject,decision_note").
			Where("tenant_id=? AND hospital_id=? AND rule_id=? AND id=?", p.TenantID, hospital, id, proposal).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&r).Error; err != nil {
			return err
		}
		if r.Status == to && r.DecidedBySubject != nil && *r.DecidedBySubject == p.Subject && r.DecisionNote == note {
			result, _, err = loadStatutoryRule(tx, p.TenantID, hospital, id, false)
			return err
		}
		if r.Status != "pending" {
			return errStatutoryState
		}
		if expected != nil && parent.Version != *expected {
			return errStatutoryVersion
		}
		if to == "withdrawn" && r.ProposedBySubject != p.Subject {
			return errStatutoryNotProposer
		}
		if to != "withdrawn" && r.ProposedBySubject == p.Subject {
			return errStatutorySelf
		}
		set := "status=?,decided_by_subject=?,decided_at=clock_timestamp(),decision_note=?"
		if to == "approved" {
			set += ",approved_version=base_version+1"
		}
		update := tx.Exec(`UPDATE workforce_statutory_rule_revisions SET `+set+` WHERE tenant_id=? AND hospital_id=? AND id=? AND status='pending'`, to, p.Subject, note, p.TenantID, hospital, proposal)
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return errStatutoryState
		}
		version := parent.Version
		if to == "approved" {
			version++
			if err := tx.Exec(`UPDATE workforce_statutory_rules SET version=?,revision_id=?,effective_on=?,updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND id=? AND version=?`,
				version, proposal, r.EffectiveOn, p.TenantID, hospital, id, parent.Version).Error; err != nil {
				return err
			}
		}
		if err := recordStatutoryChange(h, tx, p.TenantID, hospital, p.Subject, parent.Kind, to, id, proposal, version); err != nil {
			return err
		}
		result, _, err = loadStatutoryRule(tx, p.TenantID, hospital, id, false)
		return err
	})
	if err != nil {
		respondStatutoryError(c, err)
		return
	}
	respond.OK(c, result)
}

// The preview runs the structure through the salary calculator, then each kind's revision in force; a kind with none is reported, never defaulted.
func (h *handlers) previewStatutory(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	var in statutoryPreviewInput
	if !decodeSized(c, &in, 1024) {
		return
	}
	on, err := time.Parse(time.DateOnly, in.On)
	if err != nil || !validSalaryDate(in.On) || *in.PayableHalfDays > 2*daysInMonth(on) || in.StructureID == uuid.Nil {
		respond.Error(c, 400, "invalid_request", "Choose a structure, a date between 2000 and 2200 and payable days no more than the days in that month.")
		return
	}
	var result statutoryPreview
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		salary, err := salaryPreviewOn(tx, p.TenantID, hospital, in.StructureID, in.On, on, *in.PayableHalfDays)
		if err != nil {
			return err
		}
		result = statutoryPreview{Salary: salary, Results: []statutoryResult{}}
		for _, kind := range statutoryKinds {
			res := statutoryResult{Kind: kind, Status: "not_configured", Components: []statutoryComponent{}}
			revisions, err := loadStatutoryRevisions(tx, p.TenantID, hospital, tx.Table("workforce_statutory_rule_revisions").Select(statutoryRevisionColumns).
				Where("tenant_id=? AND hospital_id=? AND id=workforce_statutory_rule_on(?::uuid,?::uuid,?::text,?::date)", p.TenantID, hospital, p.TenantID, hospital, kind, in.On))
			if err != nil {
				return err
			}
			if len(revisions) == 1 {
				r := revisions[0]
				res.RuleID, res.RevisionID, res.Status = &r.RuleID, &r.ID, "not_covered"
				switch {
				case r.Pf != nil:
					res.WageBasis = &r.Pf.WageBasis
					res.WagesPaise = statutoryWages(r.Pf.WageBasis, salary.WageTest.StatutoryWagesPaise, salary.Totals.GrossPaise)
					res.BasePaise, res.Components = calculatePF(*r.Pf, res.WagesPaise)
					res.Status = "applied"
				case r.Esi != nil:
					res.WageBasis = &r.Esi.WageBasis
					res.WagesPaise = statutoryWages(r.Esi.WageBasis, salary.WageTest.StatutoryWagesPaise, salary.Totals.GrossPaise)
					res.BasePaise = res.WagesPaise
					res.Status, res.Components = calculateESI(*r.Esi, res.WagesPaise, *in.PayableHalfDays, *in.Disability)
				}
			}
			for _, component := range res.Components {
				if component.Party == "employee" {
					result.EmployeePaise += component.AmountPaise
				} else {
					result.EmployerPaise += component.AmountPaise
				}
			}
			result.Results = append(result.Results, res)
		}
		result.NetAfterStatutoryPaise = salary.Totals.NetPaise - result.EmployeePaise
		return nil
	})
	if err != nil {
		respondStatutoryError(c, err)
		return
	}
	respond.OK(c, result)
}

func respondStatutoryError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	isPG := errors.As(err, &pg)
	switch {
	case errors.Is(err, errStatutoryInvalid):
		respond.Error(c, 400, "invalid_request", "Review the rule: a covered rule needs every parameter of its kind and an uncovered one none, EPS cannot exceed the employer share or the wage ceiling, and the disability ceiling cannot be below the standard one.")
	case errors.Is(err, errStatutoryNoteRequired):
		respond.Error(c, 400, "invalid_request", "Give a reason when rejecting a proposal.")
	case errors.Is(err, errPolicyEffective):
		respond.Error(c, 400, "invalid_request", "The effective date cannot be earlier than the revision in force.")
	case errors.Is(err, errStatutorySelf), isPG && pg.ConstraintName == "workforce_statutory_separation":
		respond.Forbidden(c, "Someone other than the person who proposed the change must approve or reject it.")
	case errors.Is(err, errStatutoryNotProposer):
		respond.Forbidden(c, "Only the person who proposed the change can withdraw it.")
	case errors.Is(err, errStatutoryVersion):
		respond.Conflict(c, "The rule changed since you opened it, or its kind was changed. Reload before saving.")
	case errors.Is(err, errStatutoryPending), isPG && pg.ConstraintName == "workforce_statutory_one_pending":
		respond.ConflictWithDetail(c, "Another proposal for this rule is waiting for a decision.", gin.H{"reason": "proposal_pending"})
	case errors.Is(err, errStatutoryState):
		respond.ConflictWithDetail(c, "This proposal has already been decided.", gin.H{"reason": "not_pending"})
	case isPG && pg.ConstraintName == "workforce_statutory_effective":
		respond.ConflictWithDetail(c, "The proposal would take effect before the revision in force.", gin.H{"reason": "effective_before_current"})
	case isPG && pg.Code == "23505":
		respond.Conflict(c, "This hospital already has a rule of that kind.")
	default:
		respondSalaryError(c, err)
	}
}

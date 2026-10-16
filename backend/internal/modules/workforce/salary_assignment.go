package workforce

import (
	"bytes"
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

func (a salaryAssignment) PageKey() (time.Time, uuid.UUID)         { return a.CreatedAt, a.StaffID }
func (r salaryAssignmentRevision) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

var errAssignmentInvalid = errors.New("salary assignment is invalid")
var errAssignmentVersion = errors.New("salary assignment version conflict")
var errAssignmentPending = errors.New("a salary assignment proposal is already pending")
var errAssignmentState = errors.New("salary assignment proposal is not pending")
var errAssignmentSelf = errors.New("salary assignment decided by its author")
var errAssignmentNotProposer = errors.New("salary assignment withdrawn by someone else")
var errAssignmentNoteRequired = errors.New("salary assignment rejection needs a note")
var errNotAssigned = errors.New("no approved salary assignment in force")

const salaryAssignmentRevisionColumns = "id,staff_id,structure_id,sequence,base_version,status,approved_version,effective_on::text AS effective_on,reason,proposed_by_subject,decided_by_subject,decided_at,decision_note,created_at"

const salaryAssignmentParentColumns = "staff_id,version,effective_on::text AS effective_on,created_at,updated_at"

func staffParam(c *gin.Context) (uuid.UUID, bool) {
	staff, err := uuid.Parse(c.Param("staff"))
	if err != nil || staff == uuid.Nil {
		respond.NotFound(c, "staff member")
		return uuid.Nil, false
	}
	return staff, true
}

func requireHospitalStaff(tx *gorm.DB, tenant string, hospital, staff uuid.UUID) error {
	return tx.Table("workforce_staff").Select("id").Where("tenant_id=? AND hospital_id=? AND id=?", tenant, hospital, staff).Take(&struct{ ID uuid.UUID }{}).Error
}

func loadAssignmentRevisions(tx *gorm.DB, tenant string, hospital uuid.UUID, q *gorm.DB) ([]salaryAssignmentRevision, error) {
	rows := []salaryAssignmentRevision{}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, len(rows))
	index := make(map[uuid.UUID]int, len(rows))
	for i, r := range rows {
		ids[i], index[r.ID] = r.ID, i
		rows[i].Amounts = []salaryAssignmentAmount{}
	}
	if len(ids) == 0 {
		return rows, nil
	}
	var amounts []struct {
		RevisionID  uuid.UUID
		HeadID      uuid.UUID
		AmountPaise int64
	}
	if err := tx.Table("workforce_salary_assignment_amounts").Select("revision_id,head_id,amount_paise").Where("tenant_id=? AND hospital_id=? AND revision_id IN ?", tenant, hospital, ids).Order("revision_id,head_id").Find(&amounts).Error; err != nil {
		return nil, err
	}
	for _, a := range amounts {
		r := &rows[index[a.RevisionID]]
		r.Amounts = append(r.Amounts, salaryAssignmentAmount{HeadID: a.HeadID, AmountPaise: &a.AmountPaise})
	}
	return rows, nil
}

func salaryAssignments(tx *gorm.DB, tenant string, hospital uuid.UUID, parents []salaryAssignment) ([]salaryAssignment, error) {
	ids := make([]uuid.UUID, len(parents))
	index := make(map[uuid.UUID]int, len(parents))
	for i, p := range parents {
		ids[i], index[p.StaffID] = p.StaffID, i
	}
	if len(ids) == 0 {
		return parents, nil
	}
	revisions, err := loadAssignmentRevisions(tx, tenant, hospital, tx.Table("workforce_salary_assignment_revisions").Select(salaryAssignmentRevisionColumns).
		Where("tenant_id=? AND hospital_id=? AND staff_id IN ? AND (status='pending' OR id IN (SELECT revision_id FROM workforce_salary_assignments WHERE tenant_id=? AND hospital_id=? AND staff_id IN ?))", tenant, hospital, ids, tenant, hospital, ids))
	if err != nil {
		return nil, err
	}
	for _, r := range revisions {
		revision, parent := r, &parents[index[r.StaffID]]
		if r.Status == "pending" {
			parent.Pending = &revision
		} else {
			parent.Current = &revision
		}
	}
	return parents, nil
}

func loadSalaryAssignment(tx *gorm.DB, tenant string, hospital, staff uuid.UUID, lock bool) (salaryAssignment, error) {
	var parent salaryAssignment
	q := tx.Table("workforce_salary_assignments").Select(salaryAssignmentParentColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=?", tenant, hospital, staff)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.Take(&parent).Error; err != nil {
		return parent, err
	}
	out, err := salaryAssignments(tx, tenant, hospital, []salaryAssignment{parent})
	if err != nil {
		return parent, err
	}
	return out[0], nil
}

func (h *handlers) listSalaryAssignments(c *gin.Context, params pagination.Params) ([]salaryAssignment, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, 400, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	rows := []salaryAssignment{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		// Staff whose proposals were all rejected or withdrawn have nothing to show here; their history stays readable.
		page := tx.Table(`(SELECT staff_id AS id,* FROM workforce_salary_assignments a WHERE tenant_id=? AND hospital_id=? AND (version>0 OR EXISTS(SELECT 1 FROM workforce_salary_assignment_revisions r
 WHERE (r.tenant_id,r.hospital_id,r.staff_id)=(a.tenant_id,a.hospital_id,a.staff_id) AND r.status='pending'))) AS a`, p.TenantID, hospital)
		var parents []salaryAssignment
		if err := tenantdb.ApplyKeyset(page, params).Select(salaryAssignmentParentColumns).Find(&parents).Error; err != nil {
			return err
		}
		var err error
		rows, err = salaryAssignments(tx, p.TenantID, hospital, parents)
		return err
	})
	if err != nil {
		respondAssignmentError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) getSalaryAssignment(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staff, ok := staffParam(c)
	if !ok {
		return
	}
	var result salaryAssignment
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var err error
		if result, err = loadSalaryAssignment(tx, p.TenantID, hospital, staff, false); err != nil {
			return err
		}
		if result.Current == nil && result.Pending == nil {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	if err != nil {
		respondAssignmentError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) salaryAssignmentHistory(c *gin.Context, params pagination.Params) ([]salaryAssignmentRevision, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	staff, ok := staffParam(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, 400, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	rows := []salaryAssignmentRevision{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		if err := requireHospitalStaff(tx, p.TenantID, hospital, staff); err != nil {
			return err
		}
		var err error
		rows, err = loadAssignmentRevisions(tx, p.TenantID, hospital, tenantdb.ApplyKeyset(tx.Table("workforce_salary_assignment_revisions"), params).Select(salaryAssignmentRevisionColumns).
			Where("tenant_id=? AND hospital_id=? AND staff_id=?", p.TenantID, hospital, staff))
		return err
	})
	if err != nil {
		respondAssignmentError(c, err)
		return nil, nil
	}
	return rows, nil
}

// Mirrors the table checks so a malformed proposal is a 400 before any write; amounts are sorted so a replay compares equal.
func normalizeSalaryAssignment(in *salaryAssignmentInput) error {
	in.Reason = collapseSpaces(in.Reason)
	if in.StructureID == uuid.Nil || !validSalaryDate(in.EffectiveOn) || in.Reason == "" || !validStatutoryText(in.Reason) {
		return errAssignmentInvalid
	}
	if in.Amounts == nil {
		in.Amounts = []salaryAssignmentAmount{}
	}
	slices.SortFunc(in.Amounts, func(a, b salaryAssignmentAmount) int { return bytes.Compare(a.HeadID[:], b.HeadID[:]) })
	for i, a := range in.Amounts {
		if a.HeadID == uuid.Nil || (i > 0 && a.HeadID == in.Amounts[i-1].HeadID) {
			return errAssignmentInvalid
		}
	}
	return nil
}

func assignmentInputFromRevision(r salaryAssignmentRevision, expected int64) salaryAssignmentInput {
	return salaryAssignmentInput{ExpectedVersion: &expected, StructureID: r.StructureID, EffectiveOn: r.EffectiveOn, Reason: r.Reason, Amounts: r.Amounts}
}

func (h *handlers) putSalaryAssignment(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staff, ok := staffParam(c)
	if !ok {
		return
	}
	var in salaryAssignmentInput
	if !decodeSized(c, &in, 16384) {
		return
	}
	if err := normalizeSalaryAssignment(&in); err != nil {
		respondAssignmentError(c, err)
		return
	}
	var result salaryAssignment
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		// The salary lock serialises proposals and approvals with head and structure changes, so each deferred check sees the others.
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_payroll", salaryConfigLock); err != nil {
			return err
		}
		if err := requireHospitalStaff(tx, p.TenantID, hospital, staff); err != nil {
			return err
		}
		if err := tx.Table("workforce_salary_structures").Select("id").Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, in.StructureID).Take(&struct{ ID uuid.UUID }{}).Error; err != nil {
			return err
		}
		current, err := loadSalaryAssignment(tx, p.TenantID, hospital, staff, true)
		switch {
		case err == nil:
			// Replaying either the pending proposal or the revision now in force returns the assignment unchanged.
			for _, r := range []*salaryAssignmentRevision{current.Pending, current.Current} {
				if r != nil && reflect.DeepEqual(assignmentInputFromRevision(*r, *in.ExpectedVersion), in) {
					result = current
					return nil
				}
			}
			if current.Pending != nil {
				return errAssignmentPending
			}
			if current.Version != *in.ExpectedVersion {
				return errAssignmentVersion
			}
			if current.EffectiveOn != nil && in.EffectiveOn < *current.EffectiveOn {
				return errPolicyEffective
			}
		case !errors.Is(err, gorm.ErrRecordNotFound):
			return err
		case *in.ExpectedVersion != 0:
			return errAssignmentVersion
		default:
			if err := tx.Exec(`INSERT INTO workforce_salary_assignments(tenant_id,hospital_id,staff_id,version) VALUES(?,?,?,0)`, p.TenantID, hospital, staff).Error; err != nil {
				return err
			}
		}
		revision := uuid.New()
		if err := tx.Exec(`INSERT INTO workforce_salary_assignment_revisions(tenant_id,hospital_id,id,staff_id,structure_id,sequence,base_version,status,effective_on,reason,proposed_by_subject,decision_note)
 SELECT ?,?,?,?,?,coalesce((SELECT max(sequence) FROM workforce_salary_assignment_revisions WHERE tenant_id=? AND hospital_id=? AND staff_id=?),0)+1,?,'pending',?,?,?,''`,
			p.TenantID, hospital, revision, staff, in.StructureID, p.TenantID, hospital, staff, *in.ExpectedVersion, in.EffectiveOn, in.Reason, p.Subject).Error; err != nil {
			return err
		}
		if len(in.Amounts) > 0 {
			values, args := make([]string, len(in.Amounts)), []any{}
			for i, a := range in.Amounts {
				values[i] = "(?,?,?,?,?)"
				args = append(args, p.TenantID, hospital, revision, a.HeadID, *a.AmountPaise)
			}
			if err := tx.Exec(`INSERT INTO workforce_salary_assignment_amounts(tenant_id,hospital_id,revision_id,head_id,amount_paise) VALUES`+strings.Join(values, ","), args...).Error; err != nil {
				return err
			}
		}
		if err := recordAssignmentChange(h, tx, p.TenantID, hospital, p.Subject, "proposed", staff, revision, *in.ExpectedVersion); err != nil {
			return err
		}
		result, err = loadSalaryAssignment(tx, p.TenantID, hospital, staff, false)
		return err
	})
	if err != nil {
		respondAssignmentError(c, err)
		return
	}
	respond.OK(c, result)
}

// Audit rows and events carry identifiers, status and version only; structures and amounts stay behind the API.
func recordAssignmentChange(h *handlers, tx *gorm.DB, tenant string, hospital uuid.UUID, subject, status string, staff, revision uuid.UUID, version int64) error {
	if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,jsonb_build_object('revision_id',?::uuid,'version',?::bigint))`,
		tenant, hospital, subject, "workforce.salary_assignment."+status, staff, revision, version).Error; err != nil {
		return err
	}
	data, err := json.Marshal(workforcecontract.SalaryAssignmentChangedData{HospitalID: hospital.String(), StaffID: staff.String(), RevisionID: revision.String(), Status: status, Version: version})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, SalaryAssignmentChanged, events.Event{Type: SalaryAssignmentChanged, Version: 1, TenantID: tenant, Data: data})
}

func (h *handlers) decideSalaryAssignment(c *gin.Context) {
	var in statutoryDecisionInput
	if !decodeSized(c, &in, 2048) {
		return
	}
	to := map[string]string{"approve": "approved", "reject": "rejected"}[in.Decision]
	h.transitionAssignment(c, in.ProposalID, in.ExpectedVersion, to, in.Note)
}

func (h *handlers) withdrawSalaryAssignment(c *gin.Context) {
	var in statutoryWithdrawInput
	if !decodeSized(c, &in, 2048) {
		return
	}
	h.transitionAssignment(c, in.ProposalID, nil, "withdrawn", in.Note)
}

// transitionAssignment decides a pending proposal once; the same actor replaying the same decision gets the result back.
func (h *handlers) transitionAssignment(c *gin.Context, proposal uuid.UUID, expected *int64, to, note string) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staff, ok := staffParam(c)
	if !ok {
		return
	}
	if proposal == uuid.Nil {
		respond.NotFound(c, "salary assignment proposal")
		return
	}
	note = strings.TrimSpace(note)
	if !validStatutoryText(note) {
		respondAssignmentError(c, errAssignmentInvalid)
		return
	}
	if to == "rejected" && note == "" {
		respondAssignmentError(c, errAssignmentNoteRequired)
		return
	}
	var result salaryAssignment
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_payroll", salaryConfigLock); err != nil {
			return err
		}
		parent, err := loadSalaryAssignment(tx, p.TenantID, hospital, staff, true)
		if err != nil {
			return err
		}
		var r struct {
			Status            string
			EffectiveOn       string
			ProposedBySubject string
			DecidedBySubject  *string
			DecisionNote      string
		}
		if err := tx.Table("workforce_salary_assignment_revisions").Select("status,effective_on::text AS effective_on,proposed_by_subject,decided_by_subject,decision_note").
			Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staff, proposal).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&r).Error; err != nil {
			return err
		}
		if r.Status == to && r.DecidedBySubject != nil && *r.DecidedBySubject == p.Subject && r.DecisionNote == note {
			result, err = loadSalaryAssignment(tx, p.TenantID, hospital, staff, false)
			return err
		}
		if r.Status != "pending" {
			return errAssignmentState
		}
		if expected != nil && parent.Version != *expected {
			return errAssignmentVersion
		}
		if to == "withdrawn" && r.ProposedBySubject != p.Subject {
			return errAssignmentNotProposer
		}
		if to != "withdrawn" && r.ProposedBySubject == p.Subject {
			return errAssignmentSelf
		}
		set := "status=?,decided_by_subject=?,decided_at=clock_timestamp(),decision_note=?"
		if to == "approved" {
			set += ",approved_version=base_version+1"
		}
		update := tx.Exec(`UPDATE workforce_salary_assignment_revisions SET `+set+` WHERE tenant_id=? AND hospital_id=? AND id=? AND status='pending'`, to, p.Subject, note, p.TenantID, hospital, proposal)
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return errAssignmentState
		}
		version := parent.Version
		if to == "approved" {
			version++
			if err := tx.Exec(`UPDATE workforce_salary_assignments SET version=?,revision_id=?,effective_on=?,updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND version=?`,
				version, proposal, r.EffectiveOn, p.TenantID, hospital, staff, parent.Version).Error; err != nil {
				return err
			}
		}
		if err := recordAssignmentChange(h, tx, p.TenantID, hospital, p.Subject, to, staff, proposal, version); err != nil {
			return err
		}
		result, err = loadSalaryAssignment(tx, p.TenantID, hospital, staff, false)
		return err
	})
	if err != nil {
		respondAssignmentError(c, err)
		return
	}
	respond.OK(c, result)
}

// The staff preview runs the approved assignment in force that day, with the person's own amounts, through the same calculator and statutory rules.
func (h *handlers) previewStaffSalary(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staff, ok := staffParam(c)
	if !ok {
		return
	}
	var in staffSalaryPreviewInput
	if !decodeSized(c, &in, 1024) {
		return
	}
	on, err := time.Parse(time.DateOnly, in.On)
	if err != nil || !validSalaryDate(in.On) || *in.PayableHalfDays > 2*daysInMonth(on) {
		respond.Error(c, 400, "invalid_request", "Choose a date between 2000 and 2200 and payable days no more than the days in that month.")
		return
	}
	result := staffSalaryPreview{StaffID: staff, TaxYear: taxYearOf(on), TaxRegimeStatus: "regime_not_elected"}
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		if err := requireHospitalStaff(tx, p.TenantID, hospital, staff); err != nil {
			return err
		}
		revisions, err := loadAssignmentRevisions(tx, p.TenantID, hospital, tx.Table("workforce_salary_assignment_revisions").Select(salaryAssignmentRevisionColumns).
			Where("tenant_id=? AND hospital_id=? AND id=workforce_salary_assignment_on(?::uuid,?::uuid,?::uuid,?::date)", p.TenantID, hospital, p.TenantID, hospital, staff, in.On))
		if err != nil {
			return err
		}
		if len(revisions) != 1 {
			return errNotAssigned
		}
		assignment := revisions[0]
		personal := make(map[uuid.UUID]int64, len(assignment.Amounts))
		for _, a := range assignment.Amounts {
			personal[a.HeadID] = *a.AmountPaise
		}
		salary, err := salaryPreviewOn(tx, p.TenantID, hospital, assignment.StructureID, in.On, on, *in.PayableHalfDays, personal)
		if err != nil {
			return err
		}
		statutory, err := statutoryOn(tx, p.TenantID, hospital, salary, in.On, on, *in.PayableHalfDays, *in.Disability, in.Gender)
		if err != nil {
			return err
		}
		result.Salary, result.Results, result.EmployeePaise, result.EmployerPaise, result.NetAfterStatutoryPaise = statutory.Salary, statutory.Results, statutory.EmployeePaise, statutory.EmployerPaise, statutory.NetAfterStatutoryPaise
		result.AssignmentRevisionID = assignment.ID
		var regime []string
		if err := tx.Table("workforce_staff_tax_regimes").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND tax_year=?", p.TenantID, hospital, staff, result.TaxYear).Pluck("regime", &regime).Error; err != nil {
			return err
		}
		if len(regime) == 1 {
			result.TaxRegimeStatus, result.TaxRegime = "elected", &regime[0]
		}
		return nil
	})
	if err != nil {
		respondAssignmentError(c, err)
		return
	}
	respond.OK(c, result)
}

func respondAssignmentError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	isPG := errors.As(err, &pg)
	switch {
	case errors.Is(err, errAssignmentInvalid):
		respond.Error(c, 400, "invalid_request", "Choose a structure, a date between 2000 and 2200 and a reason, and give each head's amount once.")
	case errors.Is(err, errAssignmentNoteRequired):
		respond.Error(c, 400, "invalid_request", "Give a reason when rejecting a proposal.")
	case errors.Is(err, errPolicyEffective):
		respond.Error(c, 400, "invalid_request", "The effective date cannot be earlier than the assignment in force.")
	case errors.Is(err, errAssignmentSelf), isPG && pg.ConstraintName == "workforce_salary_assignment_separation":
		respond.Forbidden(c, "Someone other than the person who proposed the change must approve or reject it.")
	case errors.Is(err, errAssignmentNotProposer), isPG && pg.ConstraintName == "workforce_salary_assignment_withdrawn_by_proposer":
		respond.Forbidden(c, "Only the person who proposed the change can withdraw it.")
	case errors.Is(err, errAssignmentVersion):
		respond.Conflict(c, "The staff member's pay changed since you opened it. Reload before saving.")
	case errors.Is(err, errAssignmentPending), isPG && pg.ConstraintName == "workforce_salary_assignment_one_pending":
		respond.ConflictWithDetail(c, "Another pay proposal for this staff member is waiting for a decision.", gin.H{"reason": "proposal_pending"})
	case errors.Is(err, errAssignmentState):
		respond.ConflictWithDetail(c, "This proposal has already been decided.", gin.H{"reason": "not_pending"})
	case errors.Is(err, errNotAssigned):
		respond.ConflictWithDetail(c, "The staff member has no approved pay in force on that date.", gin.H{"reason": "not_assigned"})
	case errors.Is(err, errPayrollOverride):
		respond.ConflictWithDetail(c, "The staff member's amounts no longer match the structure's fixed heads on that date.", gin.H{"reason": "assignment_incomplete"})
	case isPG && pg.ConstraintName == "workforce_salary_assignment_staff_inactive":
		respond.ConflictWithDetail(c, "Pay can only be proposed for an active staff member.", gin.H{"reason": "staff_inactive"})
	case isPG && pg.ConstraintName == "workforce_salary_assignment_effective":
		respond.ConflictWithDetail(c, "The proposal would take effect before the assignment in force.", gin.H{"reason": "effective_before_current"})
	case isPG && pg.Code == "23503":
		respondSalaryError(c, errSalaryReference)
	default:
		respondSalaryError(c, err)
	}
}

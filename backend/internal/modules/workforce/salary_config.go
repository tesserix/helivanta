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

func (h salaryHead) PageKey() (time.Time, uuid.UUID)      { return h.CreatedAt, h.ID }
func (s salaryStructure) PageKey() (time.Time, uuid.UUID) { return s.CreatedAt, s.ID }

var errSalaryInvalid = errors.New("salary configuration is invalid")
var errSalaryVersion = errors.New("salary configuration version conflict")
var errSalaryReference = errors.New("salary configuration reference is unavailable")
var errSalaryNotInForce = errors.New("salary structure is not in force on the date")

// One lock key for heads and structures, so the deferred validator always sees a serialised configuration.
const salaryConfigLock = "salary-config"

const salaryHeadRevisionColumns = "id,head_id,id AS revision_id,kind,code,name,active,basis,rate_ppm,proration,proration_days,minimum_payable_half_days,month_mask,rounding_mode,rounding_unit_paise,min_amount_paise,max_amount_paise,in_gross,in_ctc,arrears_eligible,taxability,tax_note,wage_class,payslip_label,payslip_order,show_when_zero,effective_on::text AS effective_on,reviewed_by_subject,version,created_at,created_at AS updated_at"
const salaryStructureRevisionColumns = "id,structure_id,id AS revision_id,code,name,description,active,department_id,designation_id,grade_id,effective_on::text AS effective_on,reviewed_by_subject,version,created_at,created_at AS updated_at"

type salaryHeadRow struct {
	Head      salaryHead `gorm:"embedded"`
	MonthMask int
}

type salaryParent struct {
	ID         uuid.UUID
	RevisionID uuid.UUID
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func monthsFromMask(mask int) []int {
	months := []int{}
	for m := 1; m <= 12; m++ {
		if mask&(1<<(m-1)) != 0 {
			months = append(months, m)
		}
	}
	return months
}

func monthMask(months []int) int {
	mask := 0
	for _, m := range months {
		mask |= 1 << (m - 1)
	}
	return mask
}

func validSalaryDate(value string) bool {
	return validEffectiveDate(value) && value >= "2000-01-01" && value <= "2200-12-31"
}

func collapseSpaces(value string) string { return strings.Join(strings.Fields(value), " ") }

func loadSalaryHeadRevisions(tx *gorm.DB, tenant string, hospital uuid.UUID, q *gorm.DB) ([]salaryHead, error) {
	var rows []salaryHeadRow
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	heads := make([]salaryHead, len(rows))
	ids := make([]uuid.UUID, len(rows))
	index := make(map[uuid.UUID]int, len(rows))
	for i, r := range rows {
		heads[i] = r.Head
		heads[i].Months = monthsFromMask(r.MonthMask)
		heads[i].BaseHeadIDs = []uuid.UUID{}
		ids[i] = r.Head.RevisionID
		index[r.Head.RevisionID] = i
	}
	if len(ids) == 0 {
		return heads, nil
	}
	var bases []struct{ RevisionID, BaseHeadID uuid.UUID }
	if err := tx.Table("workforce_salary_head_bases").Select("revision_id,base_head_id").Where("tenant_id=? AND hospital_id=? AND revision_id IN ?", tenant, hospital, ids).Order("base_head_id").Find(&bases).Error; err != nil {
		return nil, err
	}
	for _, b := range bases {
		i := index[b.RevisionID]
		heads[i].BaseHeadIDs = append(heads[i].BaseHeadIDs, b.BaseHeadID)
	}
	return heads, nil
}

// Current rows carry the parent id and timestamps; history rows keep the revision id and name their parent.
func currentSalaryHeads(tx *gorm.DB, tenant string, hospital uuid.UUID, parents []salaryParent) ([]salaryHead, error) {
	revisions := make([]uuid.UUID, len(parents))
	for i, p := range parents {
		revisions[i] = p.RevisionID
	}
	rows, err := loadSalaryHeadRevisions(tx, tenant, hospital, tx.Table("workforce_salary_head_revisions").Select(salaryHeadRevisionColumns).Where("tenant_id=? AND hospital_id=? AND id IN ?", tenant, hospital, revisions))
	if err != nil {
		return nil, err
	}
	byRevision := make(map[uuid.UUID]salaryHead, len(rows))
	for _, r := range rows {
		byRevision[r.RevisionID] = r
	}
	out := make([]salaryHead, len(parents))
	for i, p := range parents {
		out[i] = byRevision[p.RevisionID]
		out[i].ID, out[i].HeadID, out[i].CreatedAt, out[i].UpdatedAt = p.ID, nil, p.CreatedAt, p.UpdatedAt
	}
	return out, nil
}

func loadSalaryHead(tx *gorm.DB, tenant string, hospital, id uuid.UUID, lock bool) (salaryHead, error) {
	var parent salaryParent
	q := tx.Table("workforce_salary_heads").Select("id,revision_id,created_at,updated_at").Where("tenant_id=? AND hospital_id=? AND id=?", tenant, hospital, id)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.Take(&parent).Error; err != nil {
		return salaryHead{}, err
	}
	rows, err := currentSalaryHeads(tx, tenant, hospital, []salaryParent{parent})
	if err != nil {
		return salaryHead{}, err
	}
	return rows[0], nil
}

func (h *handlers) listSalaryHeads(c *gin.Context, params pagination.Params) ([]salaryHead, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	kind := c.Query("kind")
	if params.Limit > 100 || kind != "" && !contains(kind, salaryHeadKinds...) {
		respond.Error(c, 400, "invalid_request", "Use a supported kind and a page limit no greater than 100.")
		return nil, nil
	}
	rows := []salaryHead{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var parents []salaryParent
		q := tenantdb.ApplyKeyset(tx.Table("workforce_salary_heads"), params).Select("id,revision_id,created_at,updated_at").Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital)
		if kind != "" {
			q = q.Where("kind=?", kind)
		}
		if err := q.Find(&parents).Error; err != nil {
			return err
		}
		var err error
		rows, err = currentSalaryHeads(tx, p.TenantID, hospital, parents)
		return err
	})
	if err != nil {
		respondSalaryError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) salaryHeadHistory(c *gin.Context, params pagination.Params) ([]salaryHead, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "salary head")
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, 400, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	rows := []salaryHead{}
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		if err := tx.Table("workforce_salary_heads").Select("id").Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, id).Take(&struct{ ID uuid.UUID }{}).Error; err != nil {
			return err
		}
		var err error
		rows, err = loadSalaryHeadRevisions(tx, p.TenantID, hospital, tenantdb.ApplyKeyset(tx.Table("workforce_salary_head_revisions"), params).Select(salaryHeadRevisionColumns).Where("tenant_id=? AND hospital_id=? AND head_id=?", p.TenantID, hospital, id))
		return err
	})
	if err != nil {
		respondSalaryError(c, err)
		return nil, nil
	}
	return rows, nil
}

func salaryHeadInputFromRecord(old salaryHead) salaryHeadInput {
	reviewed, version := true, old.Version
	minDays, unit, order := old.MinimumPayableHalfDays, old.RoundingUnitPaise, old.PayslipOrder
	active, gross, ctc, arrears, zero := old.Active, old.InGross, old.InCtc, old.ArrearsEligible, old.ShowWhenZero
	return salaryHeadInput{ExpectedVersion: &version, Kind: old.Kind, Code: old.Code, Name: old.Name, Active: &active, Basis: old.Basis, RatePpm: old.RatePpm,
		BaseHeadIDs: old.BaseHeadIDs, Proration: old.Proration, ProrationDays: old.ProrationDays, MinimumPayableHalfDays: &minDays, Months: old.Months,
		RoundingMode: old.RoundingMode, RoundingUnitPaise: &unit, MinAmountPaise: old.MinAmountPaise, MaxAmountPaise: old.MaxAmountPaise,
		InGross: &gross, InCtc: &ctc, ArrearsEligible: &arrears, Taxability: old.Taxability, TaxNote: old.TaxNote, WageClass: old.WageClass,
		PayslipLabel: old.PayslipLabel, PayslipOrder: &order, ShowWhenZero: &zero, EffectiveOn: old.EffectiveOn, Reviewed: &reviewed}
}

// Mirrors the table checks so a malformed head is a 400 before any write; the database still enforces every rule.
func normalizeSalaryHead(in *salaryHeadInput) error {
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	in.Name, in.PayslipLabel = collapseSpaces(in.Name), collapseSpaces(in.PayslipLabel)
	if in.TaxNote != nil {
		note := strings.TrimSpace(*in.TaxNote)
		in.TaxNote = &note
	}
	if !validSalaryDate(in.EffectiveOn) || !*in.Reviewed || !employeeCode.MatchString(in.Code) || in.Name == "" || in.PayslipLabel == "" ||
		strings.ContainsRune(in.Name, 0) || strings.ContainsRune(in.PayslipLabel, 0) || in.TaxNote != nil && (*in.TaxNote == "" || strings.ContainsRune(*in.TaxNote, 0)) {
		return errSalaryInvalid
	}
	if in.BaseHeadIDs == nil {
		in.BaseHeadIDs = []uuid.UUID{}
	}
	slices.SortFunc(in.BaseHeadIDs, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	slices.Sort(in.Months)
	if len(slices.Compact(slices.Clone(in.BaseHeadIDs))) != len(in.BaseHeadIDs) || len(slices.Compact(slices.Clone(in.Months))) != len(in.Months) {
		return errSalaryInvalid
	}
	percent := in.Basis == "percent"
	unit := int64(*in.RoundingUnitPaise)
	switch {
	case percent != (in.RatePpm != nil), percent != (len(in.BaseHeadIDs) > 0), percent && in.Proration != "none",
		(in.Proration == "fixed_days") != (in.ProrationDays != nil),
		in.MinAmountPaise != nil && *in.MinAmountPaise%unit != 0, in.MaxAmountPaise != nil && *in.MaxAmountPaise%unit != 0,
		in.MinAmountPaise != nil && in.MaxAmountPaise != nil && *in.MinAmountPaise > *in.MaxAmountPaise,
		in.Kind != "earning" && *in.InGross,
		in.Kind == "deduction" && (*in.InCtc || in.Taxability != "not_applicable" || in.WageClass != "outside"),
		in.Kind != "deduction" && in.Taxability == "not_applicable",
		(in.Taxability == "exempt") != (in.TaxNote != nil):
		return errSalaryInvalid
	}
	return nil
}

func (h *handlers) putSalaryHead(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "salary head")
		return
	}
	var in salaryHeadInput
	if !decodeSized(c, &in, 8192) {
		return
	}
	if err := normalizeSalaryHead(&in); err != nil {
		respondSalaryError(c, err)
		return
	}
	var result salaryHead
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_payroll", salaryConfigLock); err != nil {
			return err
		}
		old, err := loadSalaryHead(tx, p.TenantID, hospital, id, true)
		version := int64(1)
		switch {
		case err == nil:
			if old.Kind != in.Kind || old.Code != in.Code {
				return errSalaryVersion
			}
			existing := salaryHeadInputFromRecord(old)
			existing.ExpectedVersion = in.ExpectedVersion
			if reflect.DeepEqual(existing, in) {
				result = old
				return nil
			}
			if old.Version != *in.ExpectedVersion {
				return errSalaryVersion
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
		if len(in.BaseHeadIDs) > 0 {
			// An earning naming itself passes here so the validator refuses it as a cycle.
			want := int64(len(in.BaseHeadIDs))
			if in.Kind == "earning" && slices.Contains(in.BaseHeadIDs, id) {
				want--
			}
			var earnings int64
			if err := tx.Table("workforce_salary_heads").Where("tenant_id=? AND hospital_id=? AND kind='earning' AND id IN ? AND id<>?", p.TenantID, hospital, in.BaseHeadIDs, id).Count(&earnings).Error; err != nil {
				return err
			}
			if earnings != want {
				return errSalaryReference
			}
		}
		revision := uuid.New()
		if err := tx.Exec(`INSERT INTO workforce_salary_heads(tenant_id,hospital_id,id,kind,code,name,active,effective_on,reviewed_by_subject,version,revision_id) VALUES(?,?,?,?,?,?,?,?,?,?,?)
 ON CONFLICT(tenant_id,hospital_id,id) DO UPDATE SET name=EXCLUDED.name,active=EXCLUDED.active,effective_on=EXCLUDED.effective_on,reviewed_by_subject=EXCLUDED.reviewed_by_subject,version=EXCLUDED.version,revision_id=EXCLUDED.revision_id,updated_at=clock_timestamp()`,
			p.TenantID, hospital, id, in.Kind, in.Code, in.Name, *in.Active, in.EffectiveOn, p.Subject, version, revision).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_salary_head_revisions(tenant_id,hospital_id,id,head_id,kind,code,name,active,effective_on,reviewed_by_subject,basis,rate_ppm,proration,proration_days,minimum_payable_half_days,month_mask,rounding_mode,rounding_unit_paise,min_amount_paise,max_amount_paise,in_gross,in_ctc,arrears_eligible,taxability,tax_note,wage_class,payslip_label,payslip_order,show_when_zero,version)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			p.TenantID, hospital, revision, id, in.Kind, in.Code, in.Name, *in.Active, in.EffectiveOn, p.Subject, in.Basis, in.RatePpm, in.Proration, in.ProrationDays, *in.MinimumPayableHalfDays, monthMask(in.Months),
			in.RoundingMode, *in.RoundingUnitPaise, in.MinAmountPaise, in.MaxAmountPaise, *in.InGross, *in.InCtc, *in.ArrearsEligible, in.Taxability, in.TaxNote, in.WageClass, in.PayslipLabel, *in.PayslipOrder, *in.ShowWhenZero, version).Error; err != nil {
			return err
		}
		for _, base := range in.BaseHeadIDs {
			if err := tx.Exec(`INSERT INTO workforce_salary_head_bases(tenant_id,hospital_id,revision_id,base_head_id) VALUES(?,?,?,?)`, p.TenantID, hospital, revision, base).Error; err != nil {
				return err
			}
		}
		if err := recordSalaryChange(h, tx, p.TenantID, hospital, p.Subject, "salary_head", id, revision, version); err != nil {
			return err
		}
		result, err = loadSalaryHead(tx, p.TenantID, hospital, id, false)
		return err
	})
	if err != nil {
		respondSalaryError(c, err)
		return
	}
	respond.OK(c, result)
}

// Audit rows and events carry identifiers, kind and version only; amounts and rates stay behind the API.
func recordSalaryChange(h *handlers, tx *gorm.DB, tenant string, hospital uuid.UUID, subject, kind string, id, revision uuid.UUID, version int64) error {
	if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,jsonb_build_object('revision_id',?::uuid,'version',?::bigint,'previous_version',?::bigint))`,
		tenant, hospital, subject, "workforce."+kind+".changed", id, revision, version, version-1).Error; err != nil {
		return err
	}
	data, err := json.Marshal(workforcecontract.SalaryConfigChangedData{HospitalID: hospital.String(), ConfigID: id.String(), RevisionID: revision.String(), Kind: kind, Version: version})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, SalaryConfigChanged, events.Event{Type: SalaryConfigChanged, Version: 1, TenantID: tenant, Data: data})
}

func loadSalaryStructureLines(tx *gorm.DB, tenant string, hospital uuid.UUID, rows []salaryStructure) error {
	ids := make([]uuid.UUID, len(rows))
	index := make(map[uuid.UUID]int, len(rows))
	for i := range rows {
		ids[i] = rows[i].RevisionID
		index[ids[i]] = i
		rows[i].Lines = []salaryStructureLine{}
	}
	if len(ids) == 0 {
		return nil
	}
	var lines []struct {
		RevisionID uuid.UUID
		Line       salaryStructureLine `gorm:"embedded"`
	}
	if err := tx.Raw(`SELECT l.revision_id,l.position,l.head_id,h.code,h.name,h.kind,r.basis,l.amount_paise FROM workforce_salary_structure_lines l
 JOIN workforce_salary_heads h ON (h.tenant_id,h.hospital_id,h.id)=(l.tenant_id,l.hospital_id,l.head_id)
 JOIN workforce_salary_head_revisions r ON (r.tenant_id,r.hospital_id,r.id)=(h.tenant_id,h.hospital_id,h.revision_id)
 WHERE l.tenant_id=? AND l.hospital_id=? AND l.revision_id IN ? ORDER BY l.position`, tenant, hospital, ids).Scan(&lines).Error; err != nil {
		return err
	}
	for _, l := range lines {
		i := index[l.RevisionID]
		rows[i].Lines = append(rows[i].Lines, l.Line)
	}
	return nil
}

func currentSalaryStructures(tx *gorm.DB, tenant string, hospital uuid.UUID, parents []salaryParent) ([]salaryStructure, error) {
	revisions := make([]uuid.UUID, len(parents))
	for i, p := range parents {
		revisions[i] = p.RevisionID
	}
	var rows []salaryStructure
	if len(revisions) > 0 {
		if err := tx.Table("workforce_salary_structure_revisions").Select(salaryStructureRevisionColumns).Where("tenant_id=? AND hospital_id=? AND id IN ?", tenant, hospital, revisions).Find(&rows).Error; err != nil {
			return nil, err
		}
	}
	byRevision := make(map[uuid.UUID]salaryStructure, len(rows))
	for _, r := range rows {
		byRevision[r.RevisionID] = r
	}
	out := make([]salaryStructure, len(parents))
	for i, p := range parents {
		out[i] = byRevision[p.RevisionID]
		out[i].ID, out[i].StructureID, out[i].CreatedAt, out[i].UpdatedAt = p.ID, nil, p.CreatedAt, p.UpdatedAt
	}
	return out, loadSalaryStructureLines(tx, tenant, hospital, out)
}

func loadSalaryStructure(tx *gorm.DB, tenant string, hospital, id uuid.UUID, lock bool) (salaryStructure, error) {
	var parent salaryParent
	q := tx.Table("workforce_salary_structures").Select("id,revision_id,created_at,updated_at").Where("tenant_id=? AND hospital_id=? AND id=?", tenant, hospital, id)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.Take(&parent).Error; err != nil {
		return salaryStructure{}, err
	}
	rows, err := currentSalaryStructures(tx, tenant, hospital, []salaryParent{parent})
	if err != nil {
		return salaryStructure{}, err
	}
	return rows[0], nil
}

func (h *handlers) listSalaryStructures(c *gin.Context, params pagination.Params) ([]salaryStructure, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, 400, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	rows := []salaryStructure{}
	err := h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var parents []salaryParent
		if err := tenantdb.ApplyKeyset(tx.Table("workforce_salary_structures"), params).Select("id,revision_id,created_at,updated_at").Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Find(&parents).Error; err != nil {
			return err
		}
		var err error
		rows, err = currentSalaryStructures(tx, p.TenantID, hospital, parents)
		return err
	})
	if err != nil {
		respondSalaryError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) salaryStructureHistory(c *gin.Context, params pagination.Params) ([]salaryStructure, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "salary structure")
		return nil, nil
	}
	if params.Limit > 100 {
		respond.Error(c, 400, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	rows := []salaryStructure{}
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		if err := tx.Table("workforce_salary_structures").Select("id").Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, id).Take(&struct{ ID uuid.UUID }{}).Error; err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx.Table("workforce_salary_structure_revisions"), params).Select(salaryStructureRevisionColumns).Where("tenant_id=? AND hospital_id=? AND structure_id=?", p.TenantID, hospital, id).Find(&rows).Error; err != nil {
			return err
		}
		return loadSalaryStructureLines(tx, p.TenantID, hospital, rows)
	})
	if err != nil {
		respondSalaryError(c, err)
		return nil, nil
	}
	return rows, nil
}

func salaryStructureInputFromRecord(old salaryStructure) salaryStructureInput {
	reviewed, version, active := true, old.Version, old.Active
	in := salaryStructureInput{ExpectedVersion: &version, Code: old.Code, Name: old.Name, Description: old.Description, Active: &active,
		DepartmentID: old.DepartmentID, DesignationID: old.DesignationID, GradeID: old.GradeID, Lines: []salaryStructureLineInput{}, EffectiveOn: old.EffectiveOn, Reviewed: &reviewed}
	for _, l := range old.Lines {
		in.Lines = append(in.Lines, salaryStructureLineInput{HeadID: l.HeadID, AmountPaise: l.AmountPaise})
	}
	return in
}

func normalizeSalaryStructure(in *salaryStructureInput) error {
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	in.Name, in.Description = collapseSpaces(in.Name), strings.TrimSpace(in.Description)
	if !validSalaryDate(in.EffectiveOn) || !*in.Reviewed || !employeeCode.MatchString(in.Code) || in.Name == "" || strings.ContainsRune(in.Name, 0) || strings.ContainsRune(in.Description, 0) {
		return errSalaryInvalid
	}
	seen := map[uuid.UUID]bool{}
	for _, l := range in.Lines {
		if l.HeadID == uuid.Nil || seen[l.HeadID] {
			return errSalaryInvalid
		}
		seen[l.HeadID] = true
	}
	for _, scope := range []*uuid.UUID{in.DepartmentID, in.DesignationID, in.GradeID} {
		if scope != nil && *scope == uuid.Nil {
			return errSalaryInvalid
		}
	}
	return nil
}

// Scope masters must exist with the matching kind and be active; head references must exist in the hospital.
func validateSalaryStructureReferences(tx *gorm.DB, tenant string, hospital uuid.UUID, in salaryStructureInput) error {
	heads := make([]uuid.UUID, len(in.Lines))
	for i, l := range in.Lines {
		heads[i] = l.HeadID
	}
	var found int64
	if err := tx.Table("workforce_salary_heads").Where("tenant_id=? AND hospital_id=? AND id IN ?", tenant, hospital, heads).Count(&found).Error; err != nil {
		return err
	}
	if found != int64(len(heads)) {
		return errSalaryReference
	}
	for kind, id := range map[string]*uuid.UUID{"department": in.DepartmentID, "designation": in.DesignationID, "grade": in.GradeID} {
		if id == nil {
			continue
		}
		var master struct{ Active bool }
		if err := tx.Table("workforce_hr_masters").Select("active").Where("tenant_id=? AND hospital_id=? AND id=? AND kind=?", tenant, hospital, *id, kind).Clauses(clause.Locking{Strength: "SHARE"}).Take(&master).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errSalaryReference
			}
			return err
		}
		if !master.Active {
			return errSalaryReference
		}
	}
	return nil
}

func (h *handlers) putSalaryStructure(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "salary structure")
		return
	}
	var in salaryStructureInput
	if !decodeSized(c, &in, 16384) {
		return
	}
	if err := normalizeSalaryStructure(&in); err != nil {
		respondSalaryError(c, err)
		return
	}
	var result salaryStructure
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := lockHospitalManager(tx, p, hospital, "app.workforce_payroll", salaryConfigLock); err != nil {
			return err
		}
		old, err := loadSalaryStructure(tx, p.TenantID, hospital, id, true)
		version := int64(1)
		switch {
		case err == nil:
			if old.Code != in.Code {
				return errSalaryVersion
			}
			existing := salaryStructureInputFromRecord(old)
			existing.ExpectedVersion = in.ExpectedVersion
			if reflect.DeepEqual(existing, in) {
				result = old
				return nil
			}
			if old.Version != *in.ExpectedVersion {
				return errSalaryVersion
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
		if err := validateSalaryStructureReferences(tx, p.TenantID, hospital, in); err != nil {
			return err
		}
		revision := uuid.New()
		if err := tx.Exec(`INSERT INTO workforce_salary_structures(tenant_id,hospital_id,id,code,name,active,effective_on,reviewed_by_subject,version,revision_id) VALUES(?,?,?,?,?,?,?,?,?,?)
 ON CONFLICT(tenant_id,hospital_id,id) DO UPDATE SET name=EXCLUDED.name,active=EXCLUDED.active,effective_on=EXCLUDED.effective_on,reviewed_by_subject=EXCLUDED.reviewed_by_subject,version=EXCLUDED.version,revision_id=EXCLUDED.revision_id,updated_at=clock_timestamp()`,
			p.TenantID, hospital, id, in.Code, in.Name, *in.Active, in.EffectiveOn, p.Subject, version, revision).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_salary_structure_revisions(tenant_id,hospital_id,id,structure_id,code,name,description,active,effective_on,reviewed_by_subject,department_id,designation_id,grade_id,version) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			p.TenantID, hospital, revision, id, in.Code, in.Name, in.Description, *in.Active, in.EffectiveOn, p.Subject, in.DepartmentID, in.DesignationID, in.GradeID, version).Error; err != nil {
			return err
		}
		for i, l := range in.Lines {
			if err := tx.Exec(`INSERT INTO workforce_salary_structure_lines(tenant_id,hospital_id,revision_id,position,head_id,amount_paise) VALUES(?,?,?,?,?,?)`, p.TenantID, hospital, revision, i+1, l.HeadID, l.AmountPaise).Error; err != nil {
				return err
			}
		}
		if err := recordSalaryChange(h, tx, p.TenantID, hospital, p.Subject, "salary_structure", id, revision, version); err != nil {
			return err
		}
		result, err = loadSalaryStructure(tx, p.TenantID, hospital, id, false)
		return err
	})
	if err != nil {
		respondSalaryError(c, err)
		return
	}
	respond.OK(c, result)
}

func daysInMonth(day time.Time) int {
	return time.Date(day.Year(), day.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func (h *handlers) previewSalaryStructure(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		respond.NotFound(c, "salary structure")
		return
	}
	var in salaryPreviewInput
	if !decodeSized(c, &in, 1024) {
		return
	}
	on, err := time.Parse(time.DateOnly, in.On)
	if err != nil || !validSalaryDate(in.On) || *in.PayableHalfDays > 2*daysInMonth(on) {
		respond.Error(c, 400, "invalid_request", "Use a date between 2000 and 2200 and payable days no more than the days in that month.")
		return
	}
	var result salaryPreview
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		var err error
		result, err = salaryPreviewOn(tx, p.TenantID, hospital, id, in.On, on, *in.PayableHalfDays, nil)
		return err
	})
	if err != nil {
		respondSalaryError(c, err)
		return
	}
	respond.OK(c, result)
}

// salaryPreviewOn resolves the structure and its heads in force on the day and runs the calculator a payroll run uses.
func salaryPreviewOn(tx *gorm.DB, tenant string, hospital, id uuid.UUID, day string, on time.Time, payableHalfDays int, personal map[uuid.UUID]int64) (salaryPreview, error) {
	if err := tx.Table("workforce_salary_structures").Select("id").Where("tenant_id=? AND hospital_id=? AND id=?", tenant, hospital, id).Take(&struct{ ID uuid.UUID }{}).Error; err != nil {
		return salaryPreview{}, err
	}
	var structure struct {
		ID     *uuid.UUID
		Active bool
	}
	if err := tx.Raw(`SELECT r.id,r.active FROM workforce_salary_structure_revisions r WHERE r.tenant_id=? AND r.hospital_id=? AND r.id=workforce_salary_structure_on(?::uuid,?::uuid,?::uuid,?::date)`, tenant, hospital, tenant, hospital, id, day).Scan(&structure).Error; err != nil {
		return salaryPreview{}, err
	}
	if structure.ID == nil || !structure.Active {
		return salaryPreview{}, errSalaryNotInForce
	}
	var lines []struct {
		Position    int
		HeadID      uuid.UUID
		AmountPaise *int64
	}
	if err := tx.Table("workforce_salary_structure_lines").Select("position,head_id,amount_paise").Where("tenant_id=? AND hospital_id=? AND revision_id=?", tenant, hospital, *structure.ID).Order("position").Find(&lines).Error; err != nil {
		return salaryPreview{}, err
	}
	heads := make([]uuid.UUID, len(lines))
	for i, l := range lines {
		heads[i] = l.HeadID
	}
	revisions, err := loadSalaryHeadRevisions(tx, tenant, hospital, tx.Raw(`SELECT DISTINCT ON (head_id) `+salaryHeadRevisionColumns+` FROM workforce_salary_head_revisions
 WHERE tenant_id=? AND hospital_id=? AND head_id IN ? AND effective_on<=? ORDER BY head_id, version DESC`, tenant, hospital, heads, day))
	if err != nil {
		return salaryPreview{}, err
	}
	calc := payrollCalcInput{Heads: map[uuid.UUID]payrollHead{}, Month: int(on.Month()), PeriodDays: daysInMonth(on), PayableHalfDays: payableHalfDays}
	meta := map[uuid.UUID]salaryHead{}
	for _, r := range revisions {
		meta[*r.HeadID] = r
		ph := payrollHead{HeadID: *r.HeadID, RevisionID: r.RevisionID, Code: r.Code, Kind: r.Kind, Basis: r.Basis, Bases: r.BaseHeadIDs, Proration: r.Proration,
			MinPayableHalfDays: r.MinimumPayableHalfDays, Months: r.Months, RoundingMode: r.RoundingMode, RoundingUnit: int64(r.RoundingUnitPaise),
			MinAmount: r.MinAmountPaise, MaxAmount: r.MaxAmountPaise, InGross: r.InGross, InCTC: r.InCtc, WageClass: r.WageClass, Active: r.Active}
		if r.RatePpm != nil {
			ph.RatePPM = int64(*r.RatePpm)
		}
		if r.ProrationDays != nil {
			ph.ProrationDays = *r.ProrationDays
		}
		calc.Heads[ph.HeadID] = ph
	}
	position := map[uuid.UUID]int{}
	for _, l := range lines {
		calc.Lines = append(calc.Lines, payrollLineInput{HeadID: l.HeadID, Amount: l.AmountPaise})
		position[l.HeadID] = l.Position
	}
	if calc.Lines, err = withPersonalAmounts(calc.Lines, personal); err != nil {
		return salaryPreview{}, err
	}
	computed, err := calculatePayroll(calc)
	if err != nil {
		return salaryPreview{}, err
	}
	result := salaryPreview{StructureID: id, StructureRevisionID: *structure.ID, On: day, PeriodDays: calc.PeriodDays, PayableHalfDays: calc.PayableHalfDays, Lines: []salaryPreviewLine{},
		Totals:   salaryPreviewTotals{GrossPaise: computed.Totals.Gross, DeductionsPaise: computed.Totals.Deductions, EmployerPaise: computed.Totals.Employer, NetPaise: computed.Totals.Net, CtcPaise: computed.Totals.CTC, NegativeNet: computed.Totals.NegativeNet},
		WageTest: salaryWageTest{WagesPaise: computed.CodeWages.Wages, ExcludedPaise: computed.CodeWages.Excluded, RemunerationPaise: computed.CodeWages.Remuneration, AddBackPaise: computed.CodeWages.AddBack, StatutoryWagesPaise: computed.CodeWages.StatutoryWages, ExcludedSharePpm: computed.CodeWages.ExcludedSharePPM}}
	for _, l := range computed.Lines {
		m := meta[l.HeadID]
		result.Lines = append(result.Lines, salaryPreviewLine{HeadID: l.HeadID, RevisionID: l.RevisionID, Code: m.Code, PayslipLabel: m.PayslipLabel, PayslipOrder: m.PayslipOrder, Kind: m.Kind,
			AmountPaise: l.Amount, Applicable: l.Applicable, Shown: l.Amount != 0 || m.ShowWhenZero})
	}
	slices.SortStableFunc(result.Lines, func(a, b salaryPreviewLine) int {
		if a.PayslipOrder != b.PayslipOrder {
			return a.PayslipOrder - b.PayslipOrder
		}
		return position[a.HeadID] - position[b.HeadID]
	})
	return result, nil
}

func respondSalaryError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	isPG := errors.As(err, &pg)
	conflict := func(reason, field, message string) {
		detail := gin.H{"reason": reason}
		if id, parseErr := uuid.Parse(pg.Detail); parseErr == nil {
			detail[field] = id
		}
		if validSalaryDate(pg.Hint) {
			detail["on"] = pg.Hint
		}
		respond.ConflictWithDetail(c, message, detail)
	}
	switch {
	case errors.Is(err, errSalaryInvalid):
		respond.Error(c, 400, "invalid_request", "Review the head or structure: the basis, rate and bases must agree, amounts must be multiples of the rounding unit, deductions are untaxed and outside wages, and exempt heads need a tax note.")
	case errors.Is(err, errPolicyEffective):
		respond.Error(c, 400, "invalid_request", "The effective date cannot be earlier than the current revision's effective date.")
	case errors.Is(err, errSalaryReference):
		respond.Error(c, 400, "invalid_reference", "Select salary heads from this hospital, earnings as percentage bases, and active masters of the matching kind for the scope.")
	case errors.Is(err, errSalaryVersion):
		respond.Conflict(c, "Configuration changed since you opened it, or its code or kind was changed. Reload before saving.")
	case errors.Is(err, errSalaryNotInForce):
		respond.ConflictWithDetail(c, "The structure is not in force and active on that date.", gin.H{"reason": "not_in_force"})
	case errors.Is(err, errPayrollCycle), errors.Is(err, errPayrollIncomplete), errors.Is(err, errPayrollOverflow):
		respond.ConflictWithDetail(c, "The structure cannot be computed on that date.", gin.H{"reason": "structure_incomplete"})
	case isPG && pg.ConstraintName == "workforce_salary_cycle":
		conflict("cycle", "head_id", "The change would make salary heads depend on each other in a loop.")
	case isPG && pg.ConstraintName == "workforce_salary_base_unavailable":
		conflict("base_unavailable", "head_id", "A percentage head would use a base that is not an active earning on that date.")
	case isPG && pg.ConstraintName == "workforce_salary_structure_incomplete":
		conflict("structure_incomplete", "structure_id", "An active structure would be missing a head, a base or a fixed amount on that date, or exceed the calculation limit.")
	case isPG && pg.ConstraintName == "workforce_salary_assignment_incomplete":
		conflict("assignment_incomplete", "staff_id", "An approved staff pay assignment would lose its active structure or a fixed head it sets an amount for, or exceed the calculation limit.")
	case isPG && pg.ConstraintName == "workforce_salary_scope_inactive":
		respond.Error(c, 400, "invalid_reference", "Select active masters of the matching kind for the scope.")
	case isPG && pg.Code == "23505":
		respond.Conflict(c, "Code or name already exists in this hospital.")
	default:
		respondError(c, err)
	}
}

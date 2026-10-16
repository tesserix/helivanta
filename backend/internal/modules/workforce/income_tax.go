package workforce

import (
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/respond"
)

var errIncomeTaxInvalid = errors.New("income-tax rule is invalid")

var incomeTaxKinds = []string{"income_tax_new", "income_tax_old"}

// Income tax is an annual liability, so the monthly statutory preview leaves it to its own preview.
var monthlyStatutoryKinds = slices.DeleteFunc(slices.Clone(statutoryKinds), func(k string) bool { return slices.Contains(incomeTaxKinds, k) })

func loadIncomeTaxRules(tx *gorm.DB, tenant string, hospital uuid.UUID, ids []uuid.UUID, rows []statutoryRevision, index map[uuid.UUID]int) error {
	var params []struct {
		RevisionID               uuid.UUID
		TaxYear                  string
		StandardDeductionPaise   int64
		TaxableRoundingMode      string
		TaxableRoundingUnitPaise int
		TaxRoundingMode          string
		TaxRoundingUnitPaise     int
		RebateIncomeLimitPaise   *int64
		RebateMaxPaise           *int64
		RebateMarginalRelief     *bool
		CessRatePpm              int
	}
	if err := tx.Table("workforce_income_tax_params").Where("tenant_id=? AND hospital_id=? AND revision_id IN ?", tenant, hospital, ids).Find(&params).Error; err != nil {
		return err
	}
	for _, p := range params {
		it := &incomeTaxParams{TaxYear: p.TaxYear, StandardDeductionPaise: &p.StandardDeductionPaise, TaxableRoundingMode: p.TaxableRoundingMode, TaxableRoundingUnitPaise: &p.TaxableRoundingUnitPaise,
			TaxRoundingMode: p.TaxRoundingMode, TaxRoundingUnitPaise: &p.TaxRoundingUnitPaise, AgeBands: []incomeTaxAgeBand{}, SurchargeBands: []incomeTaxSurcharge{}, CessRatePpm: &p.CessRatePpm}
		if p.RebateIncomeLimitPaise != nil {
			it.Rebate = &incomeTaxRebate{IncomeLimitPaise: p.RebateIncomeLimitPaise, MaxPaise: p.RebateMaxPaise, MarginalRelief: p.RebateMarginalRelief}
		}
		rows[index[p.RevisionID]].IncomeTax = it
	}
	if len(params) == 0 {
		return nil
	}
	var slabs []struct {
		RevisionID uuid.UUID
		MinAge     int
		FromPaise  int64
		ToPaise    *int64
		RatePpm    int
	}
	if err := tx.Table("workforce_income_tax_slabs").Where("tenant_id=? AND hospital_id=? AND revision_id IN ?", tenant, hospital, ids).Order("revision_id,min_age,position").Find(&slabs).Error; err != nil {
		return err
	}
	for _, s := range slabs {
		it := rows[index[s.RevisionID]].IncomeTax
		if n := len(it.AgeBands); n == 0 || *it.AgeBands[n-1].MinAge != s.MinAge {
			it.AgeBands = append(it.AgeBands, incomeTaxAgeBand{MinAge: &s.MinAge, Slabs: []incomeTaxSlab{}})
		}
		band := &it.AgeBands[len(it.AgeBands)-1]
		band.Slabs = append(band.Slabs, incomeTaxSlab{FromPaise: &s.FromPaise, ToPaise: s.ToPaise, RatePpm: &s.RatePpm})
	}
	var surcharges []struct {
		RevisionID uuid.UUID
		AbovePaise int64
		RatePpm    int
	}
	if err := tx.Table("workforce_income_tax_surcharges").Where("tenant_id=? AND hospital_id=? AND revision_id IN ?", tenant, hospital, ids).Order("revision_id,position").Find(&surcharges).Error; err != nil {
		return err
	}
	for _, s := range surcharges {
		it := rows[index[s.RevisionID]].IncomeTax
		it.SurchargeBands = append(it.SurchargeBands, incomeTaxSurcharge{AbovePaise: &s.AbovePaise, RatePpm: &s.RatePpm})
	}
	return nil
}

func insertIncomeTaxRule(tx *gorm.DB, tenant string, hospital, revision uuid.UUID, kind string, p *incomeTaxParams) error {
	if p == nil {
		return nil
	}
	var limit, most *int64
	var relief *bool
	if r := p.Rebate; r != nil {
		limit, most, relief = r.IncomeLimitPaise, r.MaxPaise, r.MarginalRelief
	}
	if err := tx.Exec(`INSERT INTO workforce_income_tax_params(tenant_id,hospital_id,revision_id,kind,tax_year,standard_deduction_paise,taxable_rounding_mode,taxable_rounding_unit_paise,tax_rounding_mode,tax_rounding_unit_paise,rebate_income_limit_paise,rebate_max_paise,rebate_marginal_relief,cess_rate_ppm)
 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, tenant, hospital, revision, kind, p.TaxYear, *p.StandardDeductionPaise, p.TaxableRoundingMode, *p.TaxableRoundingUnitPaise, p.TaxRoundingMode, *p.TaxRoundingUnitPaise,
		limit, most, relief, *p.CessRatePpm).Error; err != nil {
		return err
	}
	bands, slabs, slabArgs := make([]string, len(p.AgeBands)), []string{}, []any{}
	bandArgs := []any{}
	for i, b := range p.AgeBands {
		bands[i] = "(?,?,?,?)"
		bandArgs = append(bandArgs, tenant, hospital, revision, *b.MinAge)
		for j, s := range b.Slabs {
			slabs = append(slabs, "(?,?,?,?,?,?,?,?)")
			slabArgs = append(slabArgs, tenant, hospital, revision, *b.MinAge, j+1, *s.FromPaise, s.ToPaise, *s.RatePpm)
		}
	}
	if err := tx.Exec(`INSERT INTO workforce_income_tax_age_bands(tenant_id,hospital_id,revision_id,min_age) VALUES`+strings.Join(bands, ","), bandArgs...).Error; err != nil {
		return err
	}
	if err := tx.Exec(`INSERT INTO workforce_income_tax_slabs(tenant_id,hospital_id,revision_id,min_age,position,from_paise,to_paise,rate_ppm) VALUES`+strings.Join(slabs, ","), slabArgs...).Error; err != nil {
		return err
	}
	if len(p.SurchargeBands) == 0 {
		return nil
	}
	values, args := make([]string, len(p.SurchargeBands)), []any{}
	for i, s := range p.SurchargeBands {
		values[i] = "(?,?,?,?,?,?)"
		args = append(args, tenant, hospital, revision, i+1, *s.AbovePaise, *s.RatePpm)
	}
	return tx.Exec(`INSERT INTO workforce_income_tax_surcharges(tenant_id,hospital_id,revision_id,position,above_paise,rate_ppm) VALUES`+strings.Join(values, ","), args...).Error
}

// The preview reports each regime's annual liability under the revision in force; last year's rates are never rolled into a new tax year.
func (h *handlers) previewIncomeTax(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	var in incomeTaxPreviewInput
	if !decodeSized(c, &in, 1024) {
		return
	}
	on, err := time.Parse(time.DateOnly, in.On)
	if err != nil || !validSalaryDate(in.On) {
		respond.Error(c, 400, "invalid_request", "Choose a date between 2000 and 2200.")
		return
	}
	result := incomeTaxPreview{On: in.On, Results: []incomeTaxResult{}}
	err = h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, false); err != nil {
			return err
		}
		for _, kind := range incomeTaxKinds {
			res := incomeTaxResult{Kind: kind, Status: "not_configured", IncomePaise: *in.AnnualIncomePaise}
			revisions, err := loadStatutoryRevisions(tx, p.TenantID, hospital, tx.Table("workforce_statutory_rule_revisions").Select(statutoryRevisionColumns).
				Where("tenant_id=? AND hospital_id=? AND id=workforce_statutory_rule_on(?::uuid,?::uuid,?::text,?::date)", p.TenantID, hospital, p.TenantID, hospital, kind, in.On))
			if err != nil {
				return err
			}
			if len(revisions) == 1 {
				r := revisions[0]
				res.RuleID, res.RevisionID, res.Status = &r.RuleID, &r.ID, "not_covered"
				if it := r.IncomeTax; it != nil {
					res.TaxYear = &it.TaxYear
					if it.TaxYear != taxYearOf(on) {
						res.Status = "tax_year_missing"
					} else {
						calculateIncomeTax(*it, *in.AnnualIncomePaise, in.Age, &res)
					}
				}
			}
			result.Results = append(result.Results, res)
		}
		return nil
	})
	if err != nil {
		respondStatutoryError(c, err)
		return
	}
	respond.OK(c, result)
}

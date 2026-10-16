package workforce

import (
	"fmt"
	"slices"
	"strconv"
	"time"
)

// taxYearOf names the Act's tax year (1 April to 31 March) containing day.
func taxYearOf(day time.Time) string {
	y := day.Year()
	if day.Month() < time.April {
		y--
	}
	return fmt.Sprintf("%04d-%02d", y, (y+1)%100)
}

func validTaxYear(year string) bool {
	if len(year) != 7 || year[4] != '-' {
		return false
	}
	start, err1 := strconv.Atoi(year[:4])
	end, err2 := strconv.Atoi(year[5:])
	return err1 == nil && err2 == nil && start >= 2000 && start <= 2199 && end == (start+1)%100
}

// incomeTaxProduct rounds rate × amount half-up to the paisa before any stated rounding.
func incomeTaxProduct(rate int, amount int64) int64 {
	return roundPaise(int64(rate)*amount, payrollRateScale, 1, "nearest")
}

func incomeTaxSlabTax(slabs []incomeTaxSlab, taxable int64) int64 {
	var tax int64
	for _, s := range slabs {
		if taxable <= *s.FromPaise {
			continue
		}
		top := taxable
		if s.ToPaise != nil {
			top = min(top, *s.ToPaise)
		}
		tax += incomeTaxProduct(*s.RatePpm, top-*s.FromPaise)
	}
	return tax
}

func incomeTaxRebateOn(r *incomeTaxRebate, taxable, slabTax int64) int64 {
	switch {
	case r == nil:
		return 0
	case taxable <= *r.IncomeLimitPaise:
		return min(slabTax, *r.MaxPaise)
	case *r.MarginalRelief:
		return min(*r.MaxPaise, max(0, slabTax-(taxable-*r.IncomeLimitPaise)))
	}
	return 0
}

// incomeTaxAfterRebate is slab tax less rebate on an already rounded taxable income.
func incomeTaxAfterRebate(p incomeTaxParams, slabs []incomeTaxSlab, taxable int64) int64 {
	slab := incomeTaxSlabTax(slabs, taxable)
	return slab - incomeTaxRebateOn(p.Rebate, taxable, slab)
}

// incomeTaxSurcharge applies the highest band the income exceeds, limited so crossing its threshold never costs more than the income above it.
func incomeTaxSurchargeOn(p incomeTaxParams, slabs []incomeTaxSlab, taxable, tax int64) int64 {
	band := -1
	for i, s := range p.SurchargeBands {
		if taxable > *s.AbovePaise {
			band = i
		}
	}
	if band < 0 {
		return 0
	}
	surcharge := incomeTaxProduct(*p.SurchargeBands[band].RatePpm, tax)
	threshold := *p.SurchargeBands[band].AbovePaise
	atThreshold := incomeTaxAfterRebate(p, slabs, threshold)
	if band > 0 {
		atThreshold += incomeTaxProduct(*p.SurchargeBands[band-1].RatePpm, atThreshold)
	}
	if excess := tax + surcharge - (atThreshold + taxable - threshold); excess > 0 {
		surcharge = max(0, surcharge-excess)
	}
	return surcharge
}

// incomeTaxBand picks the band with the greatest minimum age at or below age; with several bands an unknown age is never guessed.
func incomeTaxBand(bands []incomeTaxAgeBand, age *int) (incomeTaxAgeBand, bool) {
	if len(bands) > 1 && age == nil {
		return incomeTaxAgeBand{}, false
	}
	chosen := -1
	for i, b := range bands {
		if *b.MinAge == 0 && chosen < 0 || age != nil && *b.MinAge <= *age && (chosen < 0 || *b.MinAge > *bands[chosen].MinAge) {
			chosen = i
		}
	}
	if chosen < 0 {
		return incomeTaxAgeBand{}, false
	}
	return bands[chosen], true
}

// calculateIncomeTax follows the Act's order: deduction, rounding, slabs, rebate, surcharge with relief, cess, final rounding.
func calculateIncomeTax(p incomeTaxParams, income int64, age *int, res *incomeTaxResult) {
	res.IncomePaise = income
	band, ok := incomeTaxBand(p.AgeBands, age)
	if !ok {
		res.Status = "age_required"
		return
	}
	res.AgeBandMinAge = band.MinAge
	res.StandardDeductionPaise = min(*p.StandardDeductionPaise, income)
	res.TaxablePaise = roundPaise(income-res.StandardDeductionPaise, 1, int64(*p.TaxableRoundingUnitPaise), p.TaxableRoundingMode)
	res.SlabTaxPaise = incomeTaxSlabTax(band.Slabs, res.TaxablePaise)
	res.RebatePaise = incomeTaxRebateOn(p.Rebate, res.TaxablePaise, res.SlabTaxPaise)
	tax := res.SlabTaxPaise - res.RebatePaise
	res.SurchargePaise = incomeTaxSurchargeOn(p, band.Slabs, res.TaxablePaise, tax)
	res.CessPaise = incomeTaxProduct(*p.CessRatePpm, tax+res.SurchargePaise)
	res.TotalPaise = roundPaise(tax+res.SurchargePaise+res.CessPaise, 1, int64(*p.TaxRoundingUnitPaise), p.TaxRoundingMode)
	res.Status = "applied"
}

// normalizeIncomeTax sorts bands and slabs into stored order and refuses anything the completeness trigger would.
func normalizeIncomeTax(p *incomeTaxParams, effectiveOn string) bool {
	on, err := time.Parse(time.DateOnly, effectiveOn)
	if err != nil || !validTaxYear(p.TaxYear) || taxYearOf(on) != p.TaxYear {
		return false
	}
	slices.SortFunc(p.AgeBands, func(a, b incomeTaxAgeBand) int { return *a.MinAge - *b.MinAge })
	for i, b := range p.AgeBands {
		if (i == 0) != (*b.MinAge == 0) || i > 0 && *b.MinAge == *p.AgeBands[i-1].MinAge {
			return false
		}
		bands := make([]statutoryBand, len(b.Slabs))
		for j, s := range b.Slabs {
			bands[j] = statutoryBand{*s.FromPaise, s.ToPaise}
		}
		if !statutoryPartition(bands) {
			return false
		}
		slices.SortFunc(b.Slabs, func(x, y incomeTaxSlab) int { return int(*x.FromPaise - *y.FromPaise) })
	}
	for i := 1; i < len(p.SurchargeBands); i++ {
		if *p.SurchargeBands[i].AbovePaise <= *p.SurchargeBands[i-1].AbovePaise {
			return false
		}
	}
	return true
}

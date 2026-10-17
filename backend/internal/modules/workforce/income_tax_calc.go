package workforce

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
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

// incomeTaxClaimsIn is what an employee claims against one tax year's income.
type incomeTaxClaimsIn struct {
	Lines []incomeTaxClaimInput
	Hra   *incomeTaxHraInput
}

func claimLineResult(c incomeTaxClaim, claimed, allowed int64) incomeTaxClaimResult {
	status := "allowed"
	if allowed < claimed {
		status = "capped"
	}
	return incomeTaxClaimResult{Code: c.Code, Kind: &c.Kind, ClaimedPaise: claimed, AllowedPaise: allowed, Status: status}
}

// hraExemption is the least of the allowance, rent above a share of salary, and the city class's share of salary.
func hraExemption(c incomeTaxClaim, in incomeTaxHraInput) int64 {
	rate := *c.HraOtherRatePpm
	if *in.Metro {
		rate = *c.HraMetroRatePpm
	}
	return min(*in.ReceivedPaise, max(0, *in.RentPaidPaise-incomeTaxProduct(*c.HraRentExcessRatePpm, *in.SalaryPaise)), incomeTaxProduct(rate, *in.SalaryPaise))
}

// limitFromLast takes excess off the last lines first, so a shared limit is applied the same way every time.
func limitFromLast(lines []*incomeTaxClaimResult, limit int64) {
	var total int64
	for _, l := range lines {
		total += l.AllowedPaise
	}
	for i := len(lines) - 1; i >= 0 && total > limit; i-- {
		cut := min(lines[i].AllowedPaise, total-limit)
		lines[i].AllowedPaise -= cut
		total -= cut
	}
}

// applyIncomeTaxClaims sets the exemption, standard deduction, house property loss and deductions in the Act's order, and returns gross total income less deductions; a claim the revision lacks never reduces tax.
func applyIncomeTaxClaims(p incomeTaxParams, income int64, in incomeTaxClaimsIn, res *incomeTaxResult) int64 {
	claimed := map[string]int64{}
	for _, l := range in.Lines {
		claimed[l.Code] = *l.AmountPaise
	}
	lines := make([]incomeTaxClaimResult, 0, len(p.Claims)+len(in.Lines)+1)
	var hra, interest *incomeTaxClaimResult
	var deductions []*incomeTaxClaimResult
	grouped := map[string][]*incomeTaxClaimResult{}
	recognised := map[string]bool{}
	for _, c := range p.Claims {
		amount, ok := claimed[c.Code]
		switch {
		case c.Kind == "hra" && in.Hra != nil:
			lines = append(lines, claimLineResult(c, *in.Hra.ReceivedPaise, min(hraExemption(c, *in.Hra), income)))
			hra = &lines[len(lines)-1]
		case c.Kind != "hra" && ok:
			recognised[c.Code] = true
			allowed := amount
			if c.CapPaise != nil {
				allowed = min(amount, *c.CapPaise)
			}
			lines = append(lines, claimLineResult(c, amount, allowed))
			if l := &lines[len(lines)-1]; c.Kind == "house_property_interest" {
				interest = l
			} else if deductions = append(deductions, l); c.CapGroup != nil {
				grouped[*c.CapGroup] = append(grouped[*c.CapGroup], l)
			}
		}
	}
	if hra != nil {
		res.ExemptionPaise = hra.AllowedPaise
	}
	salary := income - res.ExemptionPaise
	res.StandardDeductionPaise = min(*p.StandardDeductionPaise, salary)
	left := salary - res.StandardDeductionPaise
	if interest != nil {
		interest.AllowedPaise = min(interest.AllowedPaise, left)
		res.HousePropertyLossPaise = interest.AllowedPaise
	}
	left -= res.HousePropertyLossPaise
	for _, g := range p.ClaimGroups {
		limitFromLast(grouped[g.Code], *g.CapPaise)
	}
	limitFromLast(deductions, left)
	for i := range lines {
		if lines[i].AllowedPaise < lines[i].ClaimedPaise {
			lines[i].Status = "capped"
		}
		if *lines[i].Kind == "deduction" {
			res.DeductionsPaise += lines[i].AllowedPaise
		}
	}
	if in.Hra != nil && hra == nil {
		kind := "hra"
		lines = append(lines, incomeTaxClaimResult{Kind: &kind, ClaimedPaise: *in.Hra.ReceivedPaise, Status: "not_allowed"})
	}
	for _, l := range in.Lines {
		if !recognised[l.Code] {
			lines = append(lines, incomeTaxClaimResult{Code: l.Code, ClaimedPaise: *l.AmountPaise, Status: "not_allowed"})
		}
	}
	res.Claims = lines
	return left - res.DeductionsPaise
}

// calculateIncomeTax follows the Act's order: exemption and deductions, rounding, slabs, rebate, surcharge with relief, cess, final rounding.
func calculateIncomeTax(p incomeTaxParams, income int64, age *int, claims incomeTaxClaimsIn, res *incomeTaxResult) {
	res.IncomePaise = income
	res.Claims = []incomeTaxClaimResult{}
	band, ok := incomeTaxBand(p.AgeBands, age)
	if !ok {
		res.Status = "age_required"
		return
	}
	res.AgeBandMinAge = band.MinAge
	res.TaxablePaise = roundPaise(applyIncomeTaxClaims(p, income, claims, res), 1, int64(*p.TaxableRoundingUnitPaise), p.TaxableRoundingMode)
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
	return normalizeIncomeTaxClaims(p)
}

// incomeTaxClaimFields lists which optional fields each claim kind carries; the table's kind check mirrors it.
func incomeTaxClaimFields(c incomeTaxClaim) bool {
	hra := []bool{c.HraRentExcessRatePpm != nil, c.HraMetroRatePpm != nil, c.HraOtherRatePpm != nil, c.LandlordPanRentAbovePaise != nil}
	switch c.Kind {
	case "hra":
		return c.CapPaise == nil && c.CapGroup == nil && !slices.Contains(hra, false)
	case "house_property_interest":
		return c.CapPaise != nil && c.CapGroup == nil && !slices.Contains(hra, true)
	}
	return !slices.Contains(hra, true)
}

// normalizeIncomeTaxClaims refuses what the claim tables and trigger would, and stores no claims as none.
func normalizeIncomeTaxClaims(p *incomeTaxParams) bool {
	if len(p.Claims) == 0 {
		p.Claims = nil
	}
	if len(p.ClaimGroups) == 0 {
		p.ClaimGroups = nil
	}
	groups, used := map[string]bool{}, map[string]bool{}
	for i := range p.ClaimGroups {
		g := &p.ClaimGroups[i]
		g.Code, g.Label = strings.ToUpper(strings.TrimSpace(g.Code)), collapseSpaces(g.Label)
		if !employeeCode.MatchString(g.Code) || g.Label == "" || !validStatutoryText(g.Label) || groups[g.Code] {
			return false
		}
		groups[g.Code] = true
	}
	codes, kinds := map[string]bool{}, map[string]int{}
	for i := range p.Claims {
		c := &p.Claims[i]
		c.Code, c.Label = strings.ToUpper(strings.TrimSpace(c.Code)), collapseSpaces(c.Label)
		if c.CapGroup != nil {
			group := strings.ToUpper(strings.TrimSpace(*c.CapGroup))
			c.CapGroup = &group
			if !groups[group] {
				return false
			}
			used[group] = true
		}
		kinds[c.Kind]++
		if !employeeCode.MatchString(c.Code) || c.Label == "" || !validStatutoryText(c.Label) || codes[c.Code] || !incomeTaxClaimFields(*c) {
			return false
		}
		codes[c.Code] = true
	}
	return len(used) == len(groups) && kinds["hra"] <= 1 && kinds["house_property_interest"] <= 1
}

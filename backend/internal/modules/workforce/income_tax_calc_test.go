package workforce

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func taxSlab(from int64, to *int64, rate int) incomeTaxSlab {
	return incomeTaxSlab{FromPaise: ptr(from), ToPaise: to, RatePpm: ptr(rate)}
}

// Synthetic ladder only: ₹1 lakh nil, 10% to ₹2 lakh, 20% above; rebate to ₹1.5 lakh; surcharges above ₹5 and ₹10 lakh; 5% cess.
func syntheticIncomeTax() incomeTaxParams {
	return incomeTaxParams{
		TaxYear: "2026-27", StandardDeductionPaise: ptr[int64](1000000),
		TaxableRoundingMode: "nearest", TaxableRoundingUnitPaise: ptr(1), TaxRoundingMode: "nearest", TaxRoundingUnitPaise: ptr(1),
		AgeBands:       []incomeTaxAgeBand{{MinAge: ptr(0), Slabs: []incomeTaxSlab{taxSlab(0, ptr[int64](10000000), 0), taxSlab(10000000, ptr[int64](20000000), 100000), taxSlab(20000000, nil, 200000)}}},
		Rebate:         &incomeTaxRebate{IncomeLimitPaise: ptr[int64](15000000), MaxPaise: ptr[int64](1000000), MarginalRelief: ptr(true)},
		SurchargeBands: []incomeTaxSurcharge{{AbovePaise: ptr[int64](50000000), RatePpm: ptr(100000)}, {AbovePaise: ptr[int64](100000000), RatePpm: ptr(200000)}},
		CessRatePpm:    ptr(50000),
	}
}

func taxOn(p incomeTaxParams, taxable int64, age *int) incomeTaxResult {
	res := incomeTaxResult{}
	calculateIncomeTax(p, taxable+*p.StandardDeductionPaise, age, incomeTaxClaimsIn{}, &res)
	return res
}

func TestIncomeTaxSlabsRebateAndCess(t *testing.T) {
	p := syntheticIncomeTax()
	edge := taxOn(p, 10000000, nil)
	require.Equal(t, "applied", edge.Status)
	require.EqualValues(t, 10000000, edge.TaxablePaise)
	require.EqualValues(t, 0, edge.SlabTaxPaise, "income at a slab's start pays nothing in it")
	require.EqualValues(t, 0, *edge.AgeBandMinAge)

	limit := taxOn(p, 15000000, nil)
	require.EqualValues(t, 500000, limit.SlabTaxPaise)
	require.EqualValues(t, 500000, limit.RebatePaise, "the whole tax is rebated at the limit")
	require.EqualValues(t, 0, limit.TotalPaise)

	above := taxOn(p, 15000100, nil)
	require.EqualValues(t, 500010, above.SlabTaxPaise)
	require.EqualValues(t, 499910, above.RebatePaise, "relief leaves only the ₹1 above the limit")
	require.EqualValues(t, 5, above.CessPaise)
	require.EqualValues(t, 105, above.TotalPaise)

	p.Rebate.MarginalRelief = ptr(false)
	cliff := taxOn(p, 15000100, nil)
	require.EqualValues(t, 0, cliff.RebatePaise, "without relief the rebate stops at the limit")
	require.EqualValues(t, 500000, taxOn(p, 15000000, nil).RebatePaise, "income equal to the limit still earns the whole rebate")
	require.EqualValues(t, 25001, cliff.CessPaise, "cess on 5,000.10 rounds half-up to the paisa")
	require.EqualValues(t, 525011, cliff.TotalPaise)

	p = syntheticIncomeTax()
	p.Rebate.MaxPaise = ptr[int64](400000)
	capped := taxOn(p, 15000000, nil)
	require.EqualValues(t, 400000, capped.RebatePaise, "the rebate never exceeds its maximum")
	require.EqualValues(t, 105000, capped.TotalPaise)
	require.EqualValues(t, 400000, taxOn(p, 15000100, nil).RebatePaise, "nor does marginal relief")

	p.Rebate = nil
	require.EqualValues(t, 0, taxOn(p, 15000000, nil).RebatePaise)
}

func TestIncomeTaxSurchargeMarginalRelief(t *testing.T) {
	p := syntheticIncomeTax()
	at := taxOn(p, 50000000, nil)
	require.EqualValues(t, 7000000, at.SlabTaxPaise)
	require.EqualValues(t, 0, at.SurchargePaise, "surcharge applies only above the threshold")
	require.EqualValues(t, 7350000, at.TotalPaise)

	above := taxOn(p, 50000100, nil)
	require.EqualValues(t, 7000020, above.SlabTaxPaise)
	require.EqualValues(t, 80, above.SurchargePaise, "tax and surcharge exceed the threshold's by no more than the ₹1 earned above it")
	require.EqualValues(t, 7350105, above.TotalPaise)

	second := taxOn(p, 100000000, nil)
	require.EqualValues(t, 1700000, second.SurchargePaise, "at the second threshold the first band still applies, without relief")
	beyond := taxOn(p, 100000100, nil)
	require.EqualValues(t, 1700080, beyond.SurchargePaise, "relief at the second threshold counts the first band's surcharge")
	require.EqualValues(t, 18700100, beyond.SlabTaxPaise+beyond.SurchargePaise)
	far := taxOn(p, 200000000, nil)
	require.EqualValues(t, 7400000, far.SurchargePaise, "relief fades once the income is well above the threshold")
}

func TestIncomeTaxRoundingAndDeduction(t *testing.T) {
	p := syntheticIncomeTax()
	p.Rebate = nil
	p.TaxableRoundingUnitPaise, p.TaxRoundingMode, p.TaxRoundingUnitPaise = ptr(1000), "up", ptr(1000)
	res := taxOn(p, 10100499, nil)
	require.EqualValues(t, 10100000, res.TaxablePaise, "taxable income rounds to the nearest ₹10")
	require.EqualValues(t, 10000, res.SlabTaxPaise)
	require.EqualValues(t, 500, res.CessPaise)
	require.EqualValues(t, 11000, res.TotalPaise, "the total rounds up to the next ₹10")
	p.TaxRoundingMode = "down"
	require.EqualValues(t, 10000, taxOn(p, 10100499, nil).TotalPaise)
	require.EqualValues(t, 10101000, taxOn(p, 10100500, nil).TaxablePaise, "half a unit rounds up")

	low := incomeTaxResult{}
	calculateIncomeTax(syntheticIncomeTax(), 500000, nil, incomeTaxClaimsIn{}, &low)
	require.EqualValues(t, 500000, low.StandardDeductionPaise, "the deduction is limited to the income")
	require.EqualValues(t, 0, low.TaxablePaise)
	require.EqualValues(t, 0, low.TotalPaise)
}

func TestIncomeTaxAgeBands(t *testing.T) {
	p := syntheticIncomeTax()
	p.Rebate = nil
	p.AgeBands = append(p.AgeBands, incomeTaxAgeBand{MinAge: ptr(60), Slabs: []incomeTaxSlab{taxSlab(0, ptr[int64](20000000), 0), taxSlab(20000000, nil, 100000)}})
	res := taxOn(p, 30000000, nil)
	require.Equal(t, "age_required", res.Status, "an unknown age is never guessed when the bands differ")
	require.Nil(t, res.AgeBandMinAge)
	require.EqualValues(t, 0, res.TotalPaise)
	for age, want := range map[int]struct{ band, tax int64 }{59: {0, 3000000}, 60: {60, 1000000}, 90: {60, 1000000}} {
		res := taxOn(p, 30000000, ptr(age))
		require.Equal(t, "applied", res.Status)
		require.EqualValues(t, want.band, *res.AgeBandMinAge, age)
		require.EqualValues(t, want.tax, res.SlabTaxPaise, age)
	}
	require.Equal(t, "applied", taxOn(syntheticIncomeTax(), 30000000, nil).Status, "a single band needs no age")
}

func TestTaxYearsAndNormalisation(t *testing.T) {
	require.Equal(t, "2026-27", taxYearOf(time.Date(2027, 3, 31, 0, 0, 0, 0, time.UTC)))
	require.Equal(t, "2027-28", taxYearOf(time.Date(2027, 4, 1, 0, 0, 0, 0, time.UTC)))
	require.Equal(t, "2099-00", taxYearOf(time.Date(2099, 4, 1, 0, 0, 0, 0, time.UTC)))
	for year, ok := range map[string]bool{"2026-27": true, "2099-00": true, "2026-28": false, "1999-00": false, "2200-01": false, "2026/27": false, "202-027": false, "2026-2x": false} {
		require.Equal(t, ok, validTaxYear(year), year)
	}

	p := syntheticIncomeTax()
	p.AgeBands = []incomeTaxAgeBand{{MinAge: ptr(60), Slabs: []incomeTaxSlab{taxSlab(0, nil, 0)}}, {MinAge: ptr(0), Slabs: []incomeTaxSlab{taxSlab(10000000, nil, 100000), taxSlab(0, ptr[int64](10000000), 0)}}}
	require.True(t, normalizeIncomeTax(&p, "2027-03-31"))
	require.Equal(t, 0, *p.AgeBands[0].MinAge, "bands are stored by age")
	require.EqualValues(t, 0, *p.AgeBands[0].Slabs[0].FromPaise, "slabs are stored by income")

	for name, change := range map[string]func(*incomeTaxParams) string{
		"effective before the year": func(*incomeTaxParams) string { return "2026-03-31" },
		"effective after the year":  func(*incomeTaxParams) string { return "2027-04-01" },
		"malformed year":            func(p *incomeTaxParams) string { p.TaxYear = "2026-28"; return "2026-04-01" },
		"no age 0 band":             func(p *incomeTaxParams) string { *p.AgeBands[0].MinAge = 1; return "2026-04-01" },
		"repeated age": func(p *incomeTaxParams) string {
			senior := incomeTaxAgeBand{MinAge: ptr(60), Slabs: []incomeTaxSlab{taxSlab(0, nil, 0)}}
			p.AgeBands = append(p.AgeBands, senior, senior)
			return "2026-04-01"
		},
		"slab gap": func(p *incomeTaxParams) string { *p.AgeBands[0].Slabs[1].FromPaise = 10000001; return "2026-04-01" },
		"bounded last": func(p *incomeTaxParams) string {
			p.AgeBands[0].Slabs[2].ToPaise = ptr[int64](30000000)
			return "2026-04-01"
		},
		"equal surcharge": func(p *incomeTaxParams) string {
			*p.SurchargeBands[1].AbovePaise = 50000000
			return "2026-04-01"
		},
		"falling surcharge": func(p *incomeTaxParams) string {
			*p.SurchargeBands[1].AbovePaise = 40000000
			return "2026-04-01"
		},
	} {
		p := syntheticIncomeTax()
		on := change(&p)
		require.False(t, normalizeIncomeTax(&p, on), name)
	}
	p = syntheticIncomeTax()
	require.False(t, normalizeIncomeTax(&p, "2026-02-30"), "an impossible date")
}

package workforce

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Synthetic claim heads only: HRA at 10%/50%/40%, a ₹15,000 group over two investments, a ₹5,000 medical cap and ₹20,000 interest.
func syntheticClaims() ([]incomeTaxClaimGroup, []incomeTaxClaim) {
	return []incomeTaxClaimGroup{{Code: "INVEST", Label: "Investments", CapPaise: ptr[int64](1500000)}},
		[]incomeTaxClaim{
			{Code: "HRA", Label: "House rent", Kind: "hra", HraRentExcessRatePpm: ptr(100000), HraMetroRatePpm: ptr(500000), HraOtherRatePpm: ptr(400000), LandlordPanRentAbovePaise: ptr[int64](10000000)},
			{Code: "SAVINGS", Label: "Savings", Kind: "deduction", CapPaise: ptr[int64](1200000), CapGroup: ptr("INVEST")},
			{Code: "PENSION", Label: "Pension", Kind: "deduction", CapGroup: ptr("INVEST")},
			{Code: "MEDICAL", Label: "Medical", Kind: "deduction", CapPaise: ptr[int64](500000)},
			{Code: "HOMELOAN", Label: "Home loan interest", Kind: "house_property_interest", CapPaise: ptr[int64](2000000)},
		}
}

func claimsTax() incomeTaxParams {
	p := syntheticIncomeTax()
	p.Rebate, p.SurchargeBands = nil, []incomeTaxSurcharge{}
	p.ClaimGroups, p.Claims = syntheticClaims()
	return p
}

func claimLine(code string, amount int64) incomeTaxClaimInput {
	return incomeTaxClaimInput{Code: code, AmountPaise: ptr(amount)}
}

func hraIn(received, salary, rent int64, metro bool) *incomeTaxHraInput {
	return &incomeTaxHraInput{ReceivedPaise: ptr(received), SalaryPaise: ptr(salary), RentPaidPaise: ptr(rent), Metro: ptr(metro)}
}

func taxWith(p incomeTaxParams, income int64, claims incomeTaxClaimsIn) incomeTaxResult {
	res := incomeTaxResult{}
	calculateIncomeTax(p, income, nil, claims, &res)
	return res
}

func TestHRAExemptionTakesTheLeastOfItsThreeLimits(t *testing.T) {
	p := claimsTax()
	for name, tc := range map[string]struct {
		hra  *incomeTaxHraInput
		want int64
	}{
		"allowance received": {hraIn(300000, 1000000, 600000, true), 300000},
		"metro salary share": {hraIn(900000, 1000000, 800000, true), 500000},
		"other salary share": {hraIn(900000, 1000000, 800000, false), 400000},
		"rent above a share": {hraIn(900000, 1000000, 150000, true), 50000},
		"rent below a share": {hraIn(900000, 1000000, 90000, true), 0},
	} {
		res := taxWith(p, 50000000, incomeTaxClaimsIn{Hra: tc.hra})
		require.EqualValues(t, tc.want, res.ExemptionPaise, name)
		require.Equal(t, []incomeTaxClaimResult{{Code: "HRA", Kind: ptr("hra"), ClaimedPaise: *tc.hra.ReceivedPaise, AllowedPaise: tc.want, Status: map[bool]string{true: "allowed", false: "capped"}[tc.want == *tc.hra.ReceivedPaise]}}, res.Claims, name)
	}
	res := taxWith(p, 200000, incomeTaxClaimsIn{Hra: hraIn(300000, 1000000, 600000, true)})
	require.EqualValues(t, 200000, res.ExemptionPaise, "the exemption never exceeds the income")
	require.EqualValues(t, 0, res.StandardDeductionPaise)
	require.Equal(t, "capped", res.Claims[0].Status)
}

func TestClaimsReduceTaxableIncomeInTheActsOrder(t *testing.T) {
	p := claimsTax()
	res := taxWith(p, 30000000, incomeTaxClaimsIn{
		Lines: []incomeTaxClaimInput{claimLine("MEDICAL", 300000), claimLine("PENSION", 800000), claimLine("UNKNOWN", 100000), claimLine("HOMELOAN", 2500000), claimLine("SAVINGS", 1500000)},
		Hra:   hraIn(300000, 1000000, 600000, true),
	})
	require.Equal(t, "applied", res.Status)
	require.EqualValues(t, 300000, res.ExemptionPaise)
	require.EqualValues(t, 1000000, res.StandardDeductionPaise)
	require.EqualValues(t, 2000000, res.HousePropertyLossPaise, "interest stops at its cap")
	require.EqualValues(t, 1500000+300000, res.DeductionsPaise, "the group cap holds two investments to ₹15,000")
	require.EqualValues(t, 30000000-300000-1000000-2000000-1800000, res.TaxablePaise)
	require.Equal(t, []incomeTaxClaimResult{
		{Code: "HRA", Kind: ptr("hra"), ClaimedPaise: 300000, AllowedPaise: 300000, Status: "allowed"},
		{Code: "SAVINGS", Kind: ptr("deduction"), ClaimedPaise: 1500000, AllowedPaise: 1200000, Status: "capped"},
		{Code: "PENSION", Kind: ptr("deduction"), ClaimedPaise: 800000, AllowedPaise: 300000, Status: "capped"},
		{Code: "MEDICAL", Kind: ptr("deduction"), ClaimedPaise: 300000, AllowedPaise: 300000, Status: "allowed"},
		{Code: "HOMELOAN", Kind: ptr("house_property_interest"), ClaimedPaise: 2500000, AllowedPaise: 2000000, Status: "capped"},
		{Code: "UNKNOWN", ClaimedPaise: 100000, AllowedPaise: 0, Status: "not_allowed"},
	}, res.Claims, "heads in entry order, then claims the revision does not allow")
	plain := taxWith(p, 30000000-300000-2000000-1800000, incomeTaxClaimsIn{})
	require.Equal(t, plain.TotalPaise, res.TotalPaise, "slabs, rebate and cess run on the reduced income")
}

func TestClaimsNeverTakeIncomeBelowZero(t *testing.T) {
	p := claimsTax()
	res := taxWith(p, 2500000, incomeTaxClaimsIn{Lines: []incomeTaxClaimInput{claimLine("HOMELOAN", 1000000), claimLine("MEDICAL", 500000), claimLine("SAVINGS", 1000000)}})
	require.EqualValues(t, 1000000, res.StandardDeductionPaise)
	require.EqualValues(t, 1000000, res.HousePropertyLossPaise)
	require.EqualValues(t, 500000, res.DeductionsPaise, "deductions stop at gross total income")
	require.EqualValues(t, 0, res.TaxablePaise)
	require.Equal(t, []incomeTaxClaimResult{
		{Code: "SAVINGS", Kind: ptr("deduction"), ClaimedPaise: 1000000, AllowedPaise: 500000, Status: "capped"},
		{Code: "MEDICAL", Kind: ptr("deduction"), ClaimedPaise: 500000, AllowedPaise: 0, Status: "capped"},
		{Code: "HOMELOAN", Kind: ptr("house_property_interest"), ClaimedPaise: 1000000, AllowedPaise: 1000000, Status: "allowed"},
	}, res.Claims, "the income limit is taken off the last heads first")

	short := taxWith(p, 1500000, incomeTaxClaimsIn{Lines: []incomeTaxClaimInput{claimLine("HOMELOAN", 1000000)}})
	require.EqualValues(t, 500000, short.HousePropertyLossPaise, "interest is set off only against income left")
	require.Equal(t, "capped", short.Claims[0].Status)
}

func TestARevisionWithoutClaimsAllowsNone(t *testing.T) {
	p := syntheticIncomeTax()
	claims := incomeTaxClaimsIn{Lines: []incomeTaxClaimInput{claimLine("SAVINGS", 1000000)}, Hra: hraIn(300000, 1000000, 600000, true)}
	res := taxWith(p, 30000000, claims)
	require.Equal(t, taxWith(p, 30000000, incomeTaxClaimsIn{}).TotalPaise, res.TotalPaise, "unknown claims never reduce tax")
	require.Equal(t, []incomeTaxClaimResult{
		{Code: "", Kind: ptr("hra"), ClaimedPaise: 300000, AllowedPaise: 0, Status: "not_allowed"},
		{Code: "SAVINGS", ClaimedPaise: 1000000, AllowedPaise: 0, Status: "not_allowed"},
	}, res.Claims)
	require.Equal(t, []incomeTaxClaimResult{}, taxWith(p, 30000000, incomeTaxClaimsIn{}).Claims)

	hra := claimsTax()
	got := taxWith(hra, 30000000, incomeTaxClaimsIn{Lines: []incomeTaxClaimInput{claimLine("HRA", 500000)}})
	require.Equal(t, "not_allowed", got.Claims[0].Status, "HRA is claimed through its own inputs, never as an amount")
	require.EqualValues(t, 0, got.ExemptionPaise)
}

func TestNormalizeIncomeTaxClaims(t *testing.T) {
	p := claimsTax()
	require.True(t, normalizeIncomeTax(&p, "2026-04-01"))
	empty := syntheticIncomeTax()
	empty.Claims, empty.ClaimGroups = []incomeTaxClaim{}, []incomeTaxClaimGroup{}
	require.True(t, normalizeIncomeTax(&empty, "2026-04-01"))
	require.Nil(t, empty.Claims, "no claims read back as none")
	require.Nil(t, empty.ClaimGroups)

	for name, change := range map[string]func(*incomeTaxParams){
		"duplicate claim code":     func(p *incomeTaxParams) { p.Claims[2].Code = "SAVINGS" },
		"duplicate group code":     func(p *incomeTaxParams) { p.ClaimGroups = append(p.ClaimGroups, p.ClaimGroups[0]) },
		"unknown group":            func(p *incomeTaxParams) { p.Claims[1].CapGroup = ptr("OTHER") },
		"unused group":             func(p *incomeTaxParams) { p.Claims[1].CapGroup, p.Claims[2].CapGroup = nil, nil },
		"second HRA":               func(p *incomeTaxParams) { h := p.Claims[0]; h.Code = "HRA2"; p.Claims = append(p.Claims, h) },
		"second interest":          func(p *incomeTaxParams) { h := p.Claims[4]; h.Code = "LOAN2"; p.Claims = append(p.Claims, h) },
		"interest without cap":     func(p *incomeTaxParams) { p.Claims[4].CapPaise = nil },
		"interest in a group":      func(p *incomeTaxParams) { p.Claims[4].CapGroup = ptr("INVEST") },
		"HRA without a rate":       func(p *incomeTaxParams) { p.Claims[0].HraMetroRatePpm = nil },
		"HRA without PAN limit":    func(p *incomeTaxParams) { p.Claims[0].LandlordPanRentAbovePaise = nil },
		"HRA with a cap":           func(p *incomeTaxParams) { p.Claims[0].CapPaise = ptr[int64](1) },
		"deduction with HRA field": func(p *incomeTaxParams) { p.Claims[3].HraOtherRatePpm = ptr(1) },
		"code with a space":        func(p *incomeTaxParams) { p.Claims[3].Code = "MED ICAL" },
		"blank label":              func(p *incomeTaxParams) { p.Claims[3].Label = "  " },
		"blank group label":        func(p *incomeTaxParams) { p.ClaimGroups[0].Label = "" },
	} {
		p := claimsTax()
		change(&p)
		require.False(t, normalizeIncomeTax(&p, "2026-04-01"), name)
	}
}

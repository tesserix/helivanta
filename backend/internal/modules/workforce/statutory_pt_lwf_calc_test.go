package workforce

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func ptSlab(months []int, genders []string, from int64, to *int64, amount int64) statutoryPtSlab {
	return statutoryPtSlab{Months: months, Genders: genders, FromPaise: ptr(from), ToPaise: to, AmountPaise: ptr(amount)}
}

func lwfSlab(from int64, to *int64, employee, employer int64) statutoryLwfSlab {
	return statutoryLwfSlab{FromPaise: ptr(from), ToPaise: to, EmployeeFixedPaise: ptr(employee), EmployeeRatePpm: ptr(0), EmployerFixedPaise: ptr(employer), EmployerRatePpm: ptr(0)}
}

func TestStatutoryPartitionCoversEveryWageOnce(t *testing.T) {
	band := func(from int64, to *int64) statutoryBand { return statutoryBand{from, to} }
	require.True(t, statutoryPartition([]statutoryBand{band(1000, nil), band(0, ptr[int64](1000))}), "order of entry does not matter")
	require.True(t, statutoryPartition([]statutoryBand{band(0, nil)}))
	for name, bands := range map[string][]statutoryBand{
		"none":          {},
		"gap":           {band(0, ptr[int64](1000)), band(1001, nil)},
		"overlap":       {band(0, ptr[int64](1000)), band(999, nil)},
		"same start":    {band(0, ptr[int64](1000)), band(0, nil)},
		"bounded last":  {band(0, ptr[int64](1000)), band(1000, ptr[int64](2000))},
		"two open":      {band(0, nil), band(1000, nil)},
		"nonzero start": {band(1, nil)},
		"empty band":    {band(0, ptr[int64](0)), band(0, nil)},
	} {
		require.False(t, statutoryPartition(bands), name)
	}
}

func TestPTCompletenessIsPerMonthAndGender(t *testing.T) {
	split := []statutoryPtSlab{
		ptSlab(allMonths(), []string{"female"}, 0, ptr[int64](2500000), 0),
		ptSlab(allMonths(), []string{"female", "male", "other"}, 2500000, nil, 20000),
		ptSlab(allMonths(), []string{"male", "other"}, 0, ptr[int64](1000000), 0),
		ptSlab(allMonths(), []string{"male", "other"}, 1000000, ptr[int64](2500000), 17500),
	}
	require.True(t, ptComplete(split))
	require.True(t, ptComplete([]statutoryPtSlab{ptSlab([]int{3}, statutoryGenders, 0, nil, 100)}), "months no slab names carry no PT")
	require.False(t, ptComplete(split[1:]), "a gender left without its lower band")
	require.False(t, ptComplete([]statutoryPtSlab{ptSlab([]int{3}, []string{"female", "male"}, 0, nil, 100)}), "a named month must cover every gender")
	require.False(t, ptComplete([]statutoryPtSlab{ptSlab(allMonths(), statutoryGenders, 0, nil, 0), ptSlab([]int{2}, statutoryGenders, 0, nil, 100)}), "two slabs for February")
	require.True(t, lwfComplete([]statutoryLwfSlab{lwfSlab(0, ptr[int64](300000), 0, 0), lwfSlab(300000, nil, 1200, 3600)}))
	require.False(t, lwfComplete([]statutoryLwfSlab{lwfSlab(0, ptr[int64](300000), 0, 0)}))
}

func TestCalculatePTByMonthGenderAndBand(t *testing.T) {
	p := statutoryPtParams{WageBasis: "gross", Slabs: []statutoryPtSlab{
		ptSlab([]int{1, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, statutoryGenders, 0, ptr[int64](1000000), 0),
		ptSlab([]int{1, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}, statutoryGenders, 1000000, nil, 20000),
		ptSlab([]int{2}, statutoryGenders, 0, ptr[int64](1000000), 0),
		ptSlab([]int{2}, statutoryGenders, 1000000, nil, 30000),
	}}
	for name, tc := range map[string]struct {
		wages  int64
		month  int
		amount int64
	}{
		"below the band":          {999999, 1, 0},
		"at the band edge":        {1000000, 1, 20000},
		"February at the edge":    {1000000, 2, 30000},
		"February below the band": {999999, 2, 0},
	} {
		status, components, err := calculatePT(p, tc.wages, tc.month, nil)
		require.NoError(t, err, name)
		require.Equal(t, "applied", status, name)
		require.Equal(t, map[string]int64{"pt_employee": tc.amount}, amounts(components), name)
	}

	split := statutoryPtParams{WageBasis: "gross", Slabs: []statutoryPtSlab{
		ptSlab(allMonths()[:6], []string{"female"}, 0, ptr[int64](2500000), 0),
		ptSlab(allMonths()[:6], []string{"male", "other"}, 0, ptr[int64](2500000), 20000),
		ptSlab(allMonths()[:6], statutoryGenders, 2500000, nil, 20000),
	}}
	status, components, err := calculatePT(split, 2000000, 3, nil)
	require.NoError(t, err)
	require.Equal(t, "gender_required", status, "the amount depends on gender, which is never guessed")
	require.Empty(t, components)
	status, components, err = calculatePT(split, 3000000, 3, nil)
	require.NoError(t, err)
	require.Equal(t, "applied", status, "above the split every gender pays the same")
	require.Equal(t, map[string]int64{"pt_employee": 20000}, amounts(components))
	for gender, amount := range map[string]int64{"female": 0, "male": 20000, "other": 20000} {
		_, components, err = calculatePT(split, 2000000, 3, ptr(gender))
		require.NoError(t, err)
		require.Equal(t, map[string]int64{"pt_employee": amount}, amounts(components), gender)
	}
	status, components, err = calculatePT(split, 3000000, 7, ptr("female"))
	require.NoError(t, err)
	require.Equal(t, "not_due", status, "no slab names July")
	require.Empty(t, components)
	_, _, err = calculatePT(statutoryPtParams{Slabs: []statutoryPtSlab{ptSlab([]int{3}, statutoryGenders, 100, nil, 1)}}, 50, 3, ptr("male"))
	require.ErrorIs(t, err, errStatutorySlabMissing, "a corrupt rule is an error, never a zero")
}

func TestCalculateLWFInDeductionMonths(t *testing.T) {
	p := statutoryLwfParams{WageBasis: "gross", DeductionMonths: []int{6, 12}, RoundingMode: "nearest", RoundingUnitPaise: ptr(1), Slabs: []statutoryLwfSlab{
		lwfSlab(0, ptr[int64](300000), 0, 0),
		{FromPaise: ptr[int64](300000), EmployeeFixedPaise: ptr[int64](0), EmployeeRatePpm: ptr(2000), EmployeeCapPaise: ptr[int64](3400), EmployerFixedPaise: ptr[int64](500), EmployerRatePpm: ptr(4000)},
	}}
	status, components, err := calculateLWF(p, 1000000, 5)
	require.NoError(t, err)
	require.Equal(t, "not_due", status)
	require.Empty(t, components)
	for name, tc := range map[string]struct {
		wages              int64
		employee, employer int64
	}{
		"exempt band":        {299999, 0, 0},
		"rate below the cap": {1000000, 2000, 500 + 4000},
		"rate at the cap":    {1700000, 3400, 500 + 6800},
		"rate above the cap": {2000000, 3400, 500 + 8000},
		"rounded rate share": {300125, 600, 500 + 1201},
	} {
		status, components, err = calculateLWF(p, tc.wages, 12)
		require.NoError(t, err, name)
		require.Equal(t, "applied", status, name)
		require.Equal(t, map[string]int64{"lwf_employee": tc.employee, "lwf_employer": tc.employer}, amounts(components), name)
	}
	_, _, err = calculateLWF(statutoryLwfParams{DeductionMonths: []int{6}, RoundingUnitPaise: ptr(1), Slabs: []statutoryLwfSlab{lwfSlab(100, nil, 1, 1)}}, 50, 6)
	require.ErrorIs(t, err, errStatutorySlabMissing)
}

// The trigger shadows these answers through the API, so the handler's own refusal is proven directly.
func TestNormalizeRefusesIncompleteSlabsBeforeWriting(t *testing.T) {
	rule := func(kind string, pt *statutoryPtParams, lwf *statutoryLwfParams) *statutoryRuleInput {
		return &statutoryRuleInput{ExpectedVersion: ptr[int64](0), Kind: kind, Name: "Synthetic", Covered: ptr(true), EffectiveOn: "2026-04-01", Citation: "Synthetic", Pt: pt, Lwf: lwf}
	}
	pt := &statutoryPtParams{StateCode: "IN-ZZ", WageBasis: "gross", Cadence: "monthly", Slabs: []statutoryPtSlab{ptSlab(allMonths(), []string{"other", "female", "male"}, 0, nil, 0)}}
	require.NoError(t, normalizeStatutoryRule(rule("pt", pt, nil)))
	require.Equal(t, statutoryGenders, pt.Slabs[0].Genders, "genders are put in canonical order")
	require.ErrorIs(t, normalizeStatutoryRule(rule("pt", &statutoryPtParams{StateCode: "IN-ZZ", WageBasis: "gross", Cadence: "monthly", Slabs: []statutoryPtSlab{ptSlab(allMonths(), statutoryGenders, 0, ptr[int64](5), 0)}}, nil)), errStatutorySlabs)
	lwf := func(slabs ...statutoryLwfSlab) *statutoryLwfParams {
		return &statutoryLwfParams{StateCode: "IN-ZZ", WageBasis: "gross", DeductionMonths: []int{12, 6}, RoundingMode: "nearest", RoundingUnitPaise: ptr(1), Slabs: slabs}
	}
	require.NoError(t, normalizeStatutoryRule(rule("lwf", nil, lwf(lwfSlab(0, nil, 0, 0)))))
	require.ErrorIs(t, normalizeStatutoryRule(rule("lwf", nil, lwf(lwfSlab(1, nil, 0, 0)))), errStatutorySlabs)
	for code, valid := range map[string]bool{"IN-MH": true, "IN-KA": true, "IN-M": false, "IN-mh": false, "XX-MH": false, "IN-M1": false, "IN-MHX": false} {
		require.Equal(t, valid, validStateCode(code), code)
	}
}

func TestPTPeriodContainsTheMonth(t *testing.T) {
	day := func(s string) time.Time { v, _ := time.Parse(time.DateOnly, s); return v }
	half := statutoryPtParams{Cadence: "half_yearly", PeriodStartMonths: []int{4, 10}}
	for on, want := range map[string][2]string{
		"2026-04-01": {"2026-04-01", "2026-09-30"},
		"2026-09-30": {"2026-04-01", "2026-09-30"},
		"2026-10-15": {"2026-10-01", "2027-03-31"},
		"2027-02-28": {"2026-10-01", "2027-03-31"},
		"2027-03-31": {"2026-10-01", "2027-03-31"},
	} {
		got, ok := ptPeriodOn(half, day(on))
		require.True(t, ok, on)
		require.Equal(t, ptPeriodSpan{Start: day(want[0]), End: day(want[1]), Months: 6}, got, on)
	}
	annual := statutoryPtParams{Cadence: "annual", PeriodStartMonths: []int{4}}
	got, ok := ptPeriodOn(annual, day("2027-01-10"))
	require.True(t, ok)
	require.Equal(t, ptPeriodSpan{Start: day("2026-04-01"), End: day("2027-03-31"), Months: 12}, got, "an annual period wraps the year end")
	got, _ = ptPeriodOn(statutoryPtParams{Cadence: "annual", PeriodStartMonths: []int{1}}, day("2026-12-31"))
	require.Equal(t, ptPeriodSpan{Start: day("2026-01-01"), End: day("2026-12-31"), Months: 12}, got)
	for start := 1; start <= 6; start++ {
		p := statutoryPtParams{Cadence: "half_yearly", PeriodStartMonths: []int{start, start + 6}}
		for m := 1; m <= 12; m++ {
			got, ok := ptPeriodOn(p, time.Date(2026, time.Month(m), 1, 0, 0, 0, 0, time.UTC))
			require.True(t, ok)
			on := time.Date(2026, time.Month(m), 1, 0, 0, 0, 0, time.UTC)
			require.False(t, on.Before(got.Start) || on.After(got.End), "start %d month %d", start, m)
			require.Equal(t, got.Start.AddDate(0, 6, -1), got.End)
		}
	}
	_, ok = ptPeriodOn(statutoryPtParams{Cadence: "monthly"}, day("2026-04-01"))
	require.False(t, ok, "monthly PT has no period")
}

func TestPTPeriodsChargeOncePerPeriod(t *testing.T) {
	slabs := func(months ...int) []statutoryPtSlab {
		return []statutoryPtSlab{ptSlab(months, statutoryGenders, 0, nil, 100)}
	}
	for name, tc := range map[string]struct {
		p    statutoryPtParams
		want bool
	}{
		"monthly":                        {statutoryPtParams{Cadence: "monthly", Slabs: slabs(1, 2, 3)}, true},
		"monthly with starts":            {statutoryPtParams{Cadence: "monthly", PeriodStartMonths: []int{4}, Slabs: slabs(1)}, false},
		"half-yearly":                    {statutoryPtParams{Cadence: "half_yearly", PeriodStartMonths: []int{4, 10}, Slabs: slabs(8, 2)}, true},
		"half-yearly at period ends":     {statutoryPtParams{Cadence: "half_yearly", PeriodStartMonths: []int{4, 10}, Slabs: slabs(9, 3)}, true},
		"half-yearly one start":          {statutoryPtParams{Cadence: "half_yearly", PeriodStartMonths: []int{4}, Slabs: slabs(8)}, false},
		"half-yearly starts not 6 apart": {statutoryPtParams{Cadence: "half_yearly", PeriodStartMonths: []int{4, 9}, Slabs: slabs(8, 2)}, false},
		"half-yearly period uncharged":   {statutoryPtParams{Cadence: "half_yearly", PeriodStartMonths: []int{4, 10}, Slabs: slabs(8)}, false},
		"half-yearly charged twice":      {statutoryPtParams{Cadence: "half_yearly", PeriodStartMonths: []int{4, 10}, Slabs: []statutoryPtSlab{ptSlab([]int{8, 2}, statutoryGenders, 0, nil, 1), ptSlab([]int{9}, statutoryGenders, 0, nil, 1)}}, false},
		"annual":                         {statutoryPtParams{Cadence: "annual", PeriodStartMonths: []int{4}, Slabs: slabs(3)}, true},
		"annual two starts":              {statutoryPtParams{Cadence: "annual", PeriodStartMonths: []int{4, 10}, Slabs: slabs(3)}, false},
		"annual charged twice":           {statutoryPtParams{Cadence: "annual", PeriodStartMonths: []int{4}, Slabs: slabs(3, 9)}, false},
		"unknown cadence":                {statutoryPtParams{Cadence: "weekly", Slabs: slabs(1)}, false},
	} {
		require.Equal(t, tc.want, ptPeriodsValid(tc.p), name)
	}
}

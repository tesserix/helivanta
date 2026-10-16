package workforce

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func allMonths() []int { return []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12} }

func fixedHead(kind string, mutate ...func(*payrollHead)) payrollHead {
	h := payrollHead{HeadID: uuid.New(), RevisionID: uuid.New(), Code: "H", Kind: kind, Basis: "fixed", Proration: "none", Months: allMonths(),
		RoundingMode: "nearest", RoundingUnit: 1, InGross: kind == "earning", InCTC: kind != "deduction", WageClass: "outside", Active: true}
	for _, m := range mutate {
		m(&h)
	}
	return h
}

func percentHead(kind string, ratePPM int64, bases []uuid.UUID, mutate ...func(*payrollHead)) payrollHead {
	h := fixedHead(kind, mutate...)
	h.Basis, h.RatePPM, h.Bases = "percent", ratePPM, bases
	return h
}

func paise(v int64) *int64 { return &v }

type calcFixture struct {
	heads []payrollHead
	lines []payrollLineInput
}

func (f *calcFixture) fixed(h payrollHead, amount int64) uuid.UUID {
	f.heads = append(f.heads, h)
	f.lines = append(f.lines, payrollLineInput{HeadID: h.HeadID, Amount: paise(amount)})
	return h.HeadID
}

func (f *calcFixture) percent(h payrollHead) uuid.UUID {
	f.heads = append(f.heads, h)
	f.lines = append(f.lines, payrollLineInput{HeadID: h.HeadID})
	return h.HeadID
}

func (f *calcFixture) run(t *testing.T, month, periodDays, payableHalfDays int) (payrollCalcResult, error) {
	t.Helper()
	heads := make(map[uuid.UUID]payrollHead, len(f.heads))
	for _, h := range f.heads {
		heads[h.HeadID] = h
	}
	return calculatePayroll(payrollCalcInput{Heads: heads, Lines: f.lines, Month: month, PeriodDays: periodDays, PayableHalfDays: payableHalfDays})
}

func singleFixed(t *testing.T, h payrollHead, amount int64, month, periodDays, payableHalfDays int) payrollCalcLine {
	t.Helper()
	var f calcFixture
	f.fixed(h, amount)
	res, err := f.run(t, month, periodDays, payableHalfDays)
	require.NoError(t, err)
	return res.Lines[0]
}

func TestCalculatePayrollProratesFixedHeads(t *testing.T) {
	calendar := func(h *payrollHead) { h.Proration = "calendar_days" }
	fixed26 := func(h *payrollHead) { h.Proration, h.ProrationDays = "fixed_days", 26 }
	for name, tc := range map[string]struct {
		mutate  func(*payrollHead)
		period  int
		halves  int
		expects int64
	}{
		"no proration ignores absence": {func(*payrollHead) {}, 30, 0, 3_000_000},
		"calendar full month":          {calendar, 30, 60, 3_000_000},
		"calendar half month":          {calendar, 30, 30, 1_500_000},
		"calendar half day":            {calendar, 30, 29, 1_450_000},
		"calendar 31-day month":        {calendar, 31, 31, 1_500_000},
		"fixed days below divisor":     {fixed26, 30, 26, 1_500_000},
		"fixed days one half short":    {fixed26, 30, 51, 2_942_308},
		"fixed days at divisor":        {fixed26, 30, 52, 3_000_000},
		"fixed days ratio capped":      {fixed26, 30, 60, 3_000_000},
	} {
		t.Run(name, func(t *testing.T) {
			line := singleFixed(t, fixedHead("earning", tc.mutate), 3_000_000, 10, tc.period, tc.halves)
			require.Equal(t, tc.expects, line.Amount)
			require.True(t, line.Applicable)
		})
	}
}

func TestCalculatePayrollApplicability(t *testing.T) {
	minDays := func(h *payrollHead) { h.MinPayableHalfDays, h.MinAmount = 40, paise(500) }
	march := func(h *payrollHead) { h.Months, h.MinAmount = []int{3}, paise(500) }
	for name, tc := range map[string]struct {
		mutate     func(*payrollHead)
		month      int
		halves     int
		amount     int64
		applicable bool
	}{
		"below minimum days":  {minDays, 10, 39, 0, false},
		"at minimum days":     {minDays, 10, 40, 1_000, true},
		"outside month":       {march, 2, 60, 0, false},
		"inside month":        {march, 3, 60, 1_000, true},
		"floor skipped at 0":  {func(h *payrollHead) { h.MinAmount, h.Months = paise(500), []int{1} }, 2, 60, 0, false},
		"zero payable passes": {func(*payrollHead) {}, 10, 0, 1_000, true},
	} {
		t.Run(name, func(t *testing.T) {
			line := singleFixed(t, fixedHead("earning", tc.mutate), 1_000, tc.month, 30, tc.halves)
			require.Equal(t, tc.amount, line.Amount)
			require.Equal(t, tc.applicable, line.Applicable)
		})
	}
}

func TestCalculatePayrollRoundsPercentagesAtTheHalf(t *testing.T) {
	for name, tc := range map[string]struct {
		mode    string
		base    int64
		ratePPM int64
		unit    int64
		expects int64
	}{
		"nearest at half rounds up":     {"nearest", 1_000, 50_000, 100, 100},
		"nearest below half rounds 0":   {"nearest", 1_000, 49_999, 100, 0},
		"up just above a multiple":      {"up", 1_000, 1, 100, 100},
		"up on an exact multiple":       {"up", 1_000, 100_000, 100, 100},
		"down just below a multiple":    {"down", 1_000, 199_999, 100, 100},
		"down on an exact multiple":     {"down", 1_000, 200_000, 100, 200},
		"nearest just above a multiple": {"nearest", 1_000, 100_001, 100, 100},
		"up on the smallest remainder":  {"up", 1, 1, 1, 1},
	} {
		t.Run(name, func(t *testing.T) {
			var f calcFixture
			base := f.fixed(fixedHead("earning"), tc.base)
			pct := f.percent(percentHead("earning", tc.ratePPM, []uuid.UUID{base}, func(h *payrollHead) { h.RoundingMode, h.RoundingUnit = tc.mode, tc.unit }))
			res, err := f.run(t, 10, 30, 60)
			require.NoError(t, err)
			require.Equal(t, pct, res.Lines[1].HeadID)
			require.Equal(t, tc.expects, res.Lines[1].Amount)
		})
	}
}

func TestCalculatePayrollClampsAfterRounding(t *testing.T) {
	clamp := func(h *payrollHead) { h.MinAmount, h.MaxAmount = paise(2_000), paise(5_000) }
	for name, tc := range map[string]struct {
		amount, expects int64
	}{
		"raised to floor": {1_999, 2_000},
		"at floor":        {2_000, 2_000},
		"between":         {3_000, 3_000},
		"at cap":          {5_000, 5_000},
		"cut to cap":      {5_001, 5_000},
	} {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, tc.expects, singleFixed(t, fixedHead("earning", clamp), tc.amount, 10, 30, 60).Amount)
		})
	}
}

func TestCalculatePayrollPercentagesUseProratedRoundedBases(t *testing.T) {
	var f calcFixture
	basic := f.fixed(fixedHead("earning", func(h *payrollHead) { h.Proration = "calendar_days" }), 3_000_001)
	da := f.fixed(fixedHead("earning", func(h *payrollHead) { h.Proration = "calendar_days"; h.RoundingMode, h.RoundingUnit = "down", 100 }), 1_000_099)
	hra := f.percent(percentHead("earning", 400_000, []uuid.UUID{basic, da}))
	special := f.percent(percentHead("earning", 100_000, []uuid.UUID{hra}))
	res, err := f.run(t, 10, 30, 30)
	require.NoError(t, err)
	got := map[uuid.UUID]int64{}
	for _, l := range res.Lines {
		got[l.HeadID] = l.Amount
	}
	require.Equal(t, int64(1_500_001), got[basic])
	require.Equal(t, int64(500_000), got[da])
	require.Equal(t, int64(800_000), got[hra])
	require.Equal(t, int64(80_000), got[special])
}

func TestCalculatePayrollEvaluatesDependentsDeclaredFirst(t *testing.T) {
	var f calcFixture
	basic := fixedHead("earning")
	hra := f.percent(percentHead("earning", 500_000, []uuid.UUID{basic.HeadID}))
	f.fixed(basic, 10_000)
	res, err := f.run(t, 10, 30, 60)
	require.NoError(t, err)
	require.Equal(t, hra, res.Lines[0].HeadID)
	require.Equal(t, int64(5_000), res.Lines[0].Amount)
	require.Equal(t, int64(15_000), res.Totals.Gross)
}

func TestCalculatePayrollTotals(t *testing.T) {
	var f calcFixture
	f.fixed(fixedHead("earning", func(h *payrollHead) { h.InCTC = false }), 10_000)
	f.fixed(fixedHead("earning", func(h *payrollHead) { h.InGross = false }), 3_000)
	f.fixed(fixedHead("deduction"), 2_500)
	f.fixed(fixedHead("employer_contribution"), 1_200)
	f.fixed(fixedHead("employer_contribution", func(h *payrollHead) { h.InCTC = false }), 700)
	res, err := f.run(t, 10, 30, 60)
	require.NoError(t, err)
	require.Equal(t, payrollTotals{Gross: 10_000, Deductions: 2_500, Employer: 1_900, Net: 7_500, CTC: 4_200}, res.Totals)
}

func TestCalculatePayrollReportsNegativeNet(t *testing.T) {
	for name, tc := range map[string]struct {
		deduction int64
		negative  bool
	}{
		"net zero":     {1_000, false},
		"net negative": {1_001, true},
	} {
		t.Run(name, func(t *testing.T) {
			var f calcFixture
			f.fixed(fixedHead("earning"), 1_000)
			f.fixed(fixedHead("deduction"), tc.deduction)
			res, err := f.run(t, 10, 30, 60)
			require.NoError(t, err)
			require.Equal(t, 1_000-tc.deduction, res.Totals.Net)
			require.Equal(t, tc.negative, res.Totals.NegativeNet)
		})
	}
}

func TestCalculatePayrollLabourCodeWageTest(t *testing.T) {
	wages := func(h *payrollHead) { h.WageClass = "wages" }
	excluded := func(h *payrollHead) { h.WageClass = "excluded" }
	for name, tc := range map[string]struct {
		wages, excludedEarning, excludedEmployer, outside int64
		expects                                           payrollCodeWages
	}{
		"excluded under half": {6_000, 4_000, 0, 9_000, payrollCodeWages{Wages: 6_000, Excluded: 4_000, Remuneration: 10_000, StatutoryWages: 6_000, ExcludedSharePPM: 400_000}},
		"excluded at half":    {5_000, 5_000, 0, 0, payrollCodeWages{Wages: 5_000, Excluded: 5_000, Remuneration: 10_000, StatutoryWages: 5_000, ExcludedSharePPM: 500_000}},
		"excluded over half":  {4_000, 4_000, 2_000, 0, payrollCodeWages{Wages: 4_000, Excluded: 6_000, Remuneration: 10_000, AddBack: 1_000, StatutoryWages: 5_000, ExcludedSharePPM: 600_000}},
		"odd remuneration":    {4_999, 5_000, 0, 0, payrollCodeWages{Wages: 4_999, Excluded: 5_000, Remuneration: 9_999, AddBack: 1, StatutoryWages: 5_000, ExcludedSharePPM: 500_050}},
		"no remuneration":     {0, 0, 0, 5_000, payrollCodeWages{}},
	} {
		t.Run(name, func(t *testing.T) {
			var f calcFixture
			f.fixed(fixedHead("earning", wages), tc.wages)
			f.fixed(fixedHead("earning", excluded), tc.excludedEarning)
			f.fixed(fixedHead("employer_contribution", excluded), tc.excludedEmployer)
			f.fixed(fixedHead("earning"), tc.outside)
			f.fixed(fixedHead("deduction", wages), 777)
			res, err := f.run(t, 10, 30, 60)
			require.NoError(t, err)
			require.Equal(t, tc.expects, res.CodeWages)
		})
	}
}

func TestCalculatePayrollRefusesUncomputableStructures(t *testing.T) {
	for name, tc := range map[string]struct {
		build func(*calcFixture)
		err   error
	}{
		"unknown head": {func(f *calcFixture) {
			f.lines = append(f.lines, payrollLineInput{HeadID: uuid.New(), Amount: paise(1)})
		}, errPayrollIncomplete},
		"inactive head":             {func(f *calcFixture) { f.fixed(fixedHead("earning", func(h *payrollHead) { h.Active = false }), 1) }, errPayrollIncomplete},
		"fixed head without amount": {func(f *calcFixture) { f.percent(fixedHead("earning")) }, errPayrollIncomplete},
		"percent head with amount": {func(f *calcFixture) {
			base := f.fixed(fixedHead("earning"), 1)
			f.fixed(percentHead("earning", 1, []uuid.UUID{base}), 1)
		}, errPayrollIncomplete},
		"base outside structure": {func(f *calcFixture) {
			outside := fixedHead("earning")
			f.heads = append(f.heads, outside)
			f.percent(percentHead("earning", 1, []uuid.UUID{outside.HeadID}))
		}, errPayrollIncomplete},
		"deduction as base": {func(f *calcFixture) {
			f.percent(percentHead("earning", 1, []uuid.UUID{f.fixed(fixedHead("deduction"), 1)}))
		}, errPayrollIncomplete},
		"employer contribution as base": {func(f *calcFixture) {
			f.percent(percentHead("deduction", 1, []uuid.UUID{f.fixed(fixedHead("employer_contribution"), 1)}))
		}, errPayrollIncomplete},
		"self reference": {func(f *calcFixture) {
			h := percentHead("earning", 1, nil)
			h.Bases = []uuid.UUID{h.HeadID}
			f.percent(h)
		}, errPayrollCycle},
		"two-head cycle": {func(f *calcFixture) {
			a, b := percentHead("earning", 1, nil), percentHead("earning", 1, nil)
			a.Bases, b.Bases = []uuid.UUID{b.HeadID}, []uuid.UUID{a.HeadID}
			f.percent(a)
			f.percent(b)
		}, errPayrollCycle},
		"line over the limit": {func(f *calcFixture) { f.fixed(fixedHead("earning"), maxPayrollLinePaise+1) }, errPayrollOverflow},
		"cap does not hide overflow at the limit": {func(f *calcFixture) {
			f.fixed(fixedHead("earning", func(h *payrollHead) { h.MinAmount = paise(maxPayrollLinePaise + 1) }), 1)
		}, errPayrollOverflow},
	} {
		t.Run(name, func(t *testing.T) {
			var f calcFixture
			tc.build(&f)
			_, err := f.run(t, 10, 30, 60)
			require.ErrorIs(t, err, tc.err)
		})
	}
}

func TestCalculatePayrollAcceptsTheLineLimit(t *testing.T) {
	var f calcFixture
	base := f.fixed(fixedHead("earning"), maxPayrollLinePaise)
	f.percent(percentHead("earning", payrollRateScale, []uuid.UUID{base}))
	res, err := f.run(t, 10, 30, 60)
	require.NoError(t, err)
	require.Equal(t, 2*maxPayrollLinePaise, res.Totals.Gross)
}

func TestWithPersonalAmountsReplacesFixedLines(t *testing.T) {
	var f calcFixture
	basic := f.fixed(fixedHead("earning"), 3_000_000)
	hra := f.fixed(fixedHead("earning"), 1_000_000)
	pf := f.percent(percentHead("deduction", 120_000, []uuid.UUID{basic}))
	lines, err := withPersonalAmounts(f.lines, map[uuid.UUID]int64{basic: 4_000_000})
	require.NoError(t, err)
	require.Equal(t, int64(3_000_000), *f.lines[0].Amount, "the structure's lines are not modified")
	f.lines = lines
	res, err := f.run(t, 10, 30, 60)
	require.NoError(t, err)
	got := map[uuid.UUID]int64{}
	for _, l := range res.Lines {
		got[l.HeadID] = l.Amount
	}
	require.Equal(t, map[uuid.UUID]int64{basic: 4_000_000, hra: 1_000_000, pf: 480_000}, got)
}

func TestWithPersonalAmountsRefusesOtherHeads(t *testing.T) {
	var f calcFixture
	basic := f.fixed(fixedHead("earning"), 3_000_000)
	pf := f.percent(percentHead("deduction", 120_000, []uuid.UUID{basic}))
	for name, amounts := range map[string]map[uuid.UUID]int64{
		"percentage head":   {pf: 100},
		"head not in lines": {uuid.New(): 100},
		"negative amount":   {basic: -1},
		"above the limit":   {basic: maxPayrollAmountPaise + 1},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := withPersonalAmounts(f.lines, amounts)
			require.ErrorIs(t, err, errPayrollOverride)
		})
	}
	lines, err := withPersonalAmounts(f.lines, map[uuid.UUID]int64{basic: maxPayrollAmountPaise})
	require.NoError(t, err)
	require.Equal(t, maxPayrollAmountPaise, *lines[0].Amount)
}

package workforce

import (
	"cmp"
	"errors"
	"slices"
	"time"
)

// The tables guarantee one slab per wage, so a miss means the stored rule is corrupt and nothing is calculated.
var errStatutorySlabMissing = errors.New("statutory rule has no slab for these wages")

// statutoryWages picks the wage figure a rule declares; nothing defaults to basic pay.
func statutoryWages(basis string, codeWages, gross int64) int64 {
	if basis == "gross" {
		return gross
	}
	return codeWages
}

func statutoryRound(rate, base int64, unit int, mode string) int64 {
	return roundPaise(rate*base, payrollRateScale, int64(unit), mode)
}

// calculatePF applies the PF order of operations; EPS rounds from a smaller product than the employer total, so EPF never goes negative.
func calculatePF(p statutoryPfParams, wages int64) (base int64, components []statutoryComponent) {
	base = wages
	if p.ContributionBasis == "capped" {
		base = min(wages, *p.WageCeilingPaise)
	}
	unit, mode := *p.RoundingUnitPaise, p.RoundingMode
	employee := statutoryRound(int64(*p.EmployeeRatePpm), base, unit, mode)
	employer := statutoryRound(int64(*p.EmployerRatePpm), base, unit, mode)
	eps := statutoryRound(int64(*p.EpsRatePpm), min(wages, *p.EpsWageCeilingPaise), unit, mode)
	edli := statutoryRound(int64(*p.EdliRatePpm), min(wages, *p.EdliWageCeilingPaise), unit, mode)
	admin := statutoryRound(int64(*p.AdminRatePpm), base, unit, mode)
	return base, []statutoryComponent{
		{Component: "pf_employee", Party: "employee", AmountPaise: employee},
		{Component: "pf_employer_epf", Party: "employer", AmountPaise: employer - eps},
		{Component: "pf_employer_eps", Party: "employer", AmountPaise: eps},
		{Component: "pf_edli", Party: "employer", AmountPaise: edli},
		{Component: "pf_admin", Party: "employer", AmountPaise: admin},
	}
}

// calculateESI reports above_ceiling rather than contributing; coverage that persists through a contribution period is the run's decision.
func calculateESI(p statutoryEsiParams, wages int64, payableHalfDays int, disability bool) (status string, components []statutoryComponent) {
	ceiling := *p.WageCeilingPaise
	if disability {
		ceiling = *p.DisabilityWageCeilingPaise
	}
	if wages > ceiling {
		return "above_ceiling", []statutoryComponent{}
	}
	unit, mode := *p.RoundingUnitPaise, p.RoundingMode
	var employee, employer int64
	if payableHalfDays > 0 {
		employer = statutoryRound(int64(*p.EmployerRatePpm), wages, unit, mode)
		// Average daily wage is wages ÷ (half-days ÷ 2); compared cross-multiplied to stay exact.
		if p.EmployeeExemptDailyPaise == nil || 2*wages > *p.EmployeeExemptDailyPaise*int64(payableHalfDays) {
			employee = statutoryRound(int64(*p.EmployeeRatePpm), wages, unit, mode)
		}
	}
	return "applied", []statutoryComponent{
		{Component: "esi_employee", Party: "employee", AmountPaise: employee},
		{Component: "esi_employer", Party: "employer", AmountPaise: employer},
	}
}

var statutoryGenders = []string{"female", "male", "other"}

type statutoryBand struct {
	From int64
	To   *int64
}

func (b statutoryBand) holds(wages int64) bool {
	return wages >= b.From && (b.To == nil || wages < *b.To)
}

// statutoryPartition holds when the bands cover every wage from zero exactly once.
func statutoryPartition(bands []statutoryBand) bool {
	bands = slices.Clone(bands)
	slices.SortStableFunc(bands, func(a, b statutoryBand) int { return cmp.Compare(a.From, b.From) })
	next := int64(0)
	for i, b := range bands {
		if b.From != next || (b.To == nil) != (i == len(bands)-1) || (b.To != nil && *b.To <= b.From) {
			return false
		}
		if b.To != nil {
			next = *b.To
		}
	}
	return len(bands) > 0
}

func ptApplies(s statutoryPtSlab, month int, gender string) bool {
	return slices.Contains(s.Months, month) && slices.Contains(s.Genders, gender)
}

// ptComplete requires every gender to be partitioned in every month any slab names; an unnamed month has no PT.
func ptComplete(slabs []statutoryPtSlab) bool {
	for month := 1; month <= 12; month++ {
		if !slices.ContainsFunc(slabs, func(s statutoryPtSlab) bool { return slices.Contains(s.Months, month) }) {
			continue
		}
		for _, gender := range statutoryGenders {
			var bands []statutoryBand
			for _, s := range slabs {
				if ptApplies(s, month, gender) {
					bands = append(bands, statutoryBand{*s.FromPaise, s.ToPaise})
				}
			}
			if !statutoryPartition(bands) {
				return false
			}
		}
	}
	return true
}

func lwfComplete(slabs []statutoryLwfSlab) bool {
	bands := make([]statutoryBand, len(slabs))
	for i, s := range slabs {
		bands[i] = statutoryBand{*s.FromPaise, s.ToPaise}
	}
	return statutoryPartition(bands)
}

var ptPeriodMonths = map[string]int{"monthly": 1, "half_yearly": 6, "annual": 12}

type ptPeriodSpan struct {
	Start, End time.Time
	Months     int
}

// ptPeriodOn is the period holding the day's month: the latest start at or before it, reaching into the previous year if needed.
func ptPeriodOn(p statutoryPtParams, on time.Time) (ptPeriodSpan, bool) {
	length := ptPeriodMonths[p.Cadence]
	month := int(on.Month())
	for _, start := range p.PeriodStartMonths {
		if (month-start+12)%12 < length {
			year := on.Year()
			if start > month {
				year--
			}
			first := time.Date(year, time.Month(start), 1, 0, 0, 0, 0, time.UTC)
			return ptPeriodSpan{Start: first, End: first.AddDate(0, length, -1), Months: length}, true
		}
	}
	return ptPeriodSpan{}, false
}

// ptPeriodsValid sorts the starts and needs the ones the cadence implies plus, when periodic, exactly one charged month per period, so no period's tax is skipped or doubled.
func ptPeriodsValid(p statutoryPtParams) bool {
	length, ok := ptPeriodMonths[p.Cadence]
	if !ok || !uniqueMonths(p.PeriodStartMonths) {
		return false
	}
	starts := p.PeriodStartMonths
	switch p.Cadence {
	case "monthly":
		return len(starts) == 0
	case "half_yearly":
		if len(starts) != 2 || starts[1]-starts[0] != 6 {
			return false
		}
	case "annual":
		if len(starts) != 1 {
			return false
		}
	}
	charged := map[int]bool{}
	for _, s := range p.Slabs {
		for _, m := range s.Months {
			charged[m] = true
		}
	}
	for _, start := range starts {
		n := 0
		for i := range length {
			if charged[(start-1+i)%12+1] {
				n++
			}
		}
		if n != 1 {
			return false
		}
	}
	return true
}

func ptAmount(p statutoryPtParams, wages int64, month int, gender string) (int64, bool) {
	for _, s := range p.Slabs {
		if ptApplies(s, month, gender) && (statutoryBand{*s.FromPaise, s.ToPaise}).holds(wages) {
			return *s.AmountPaise, true
		}
	}
	return 0, false
}

// calculatePT needs a gender only when the amount at these wages depends on it, and never guesses one.
func calculatePT(p statutoryPtParams, wages int64, month int, gender *string) (string, []statutoryComponent, error) {
	if !slices.ContainsFunc(p.Slabs, func(s statutoryPtSlab) bool { return slices.Contains(s.Months, month) }) {
		return "not_due", []statutoryComponent{}, nil
	}
	genders := statutoryGenders
	if gender != nil {
		genders = []string{*gender}
	}
	var amount int64
	for i, g := range genders {
		a, ok := ptAmount(p, wages, month, g)
		if !ok {
			return "", nil, errStatutorySlabMissing
		}
		if i > 0 && a != amount {
			return "gender_required", []statutoryComponent{}, nil
		}
		amount = a
	}
	return "applied", []statutoryComponent{{Component: "pt_employee", Party: "employee", AmountPaise: amount}}, nil
}

func lwfShare(fixed int64, rate int, cap *int64, wages int64, unit int, mode string) int64 {
	amount := fixed + statutoryRound(int64(rate), wages, unit, mode)
	if cap != nil {
		amount = min(amount, *cap)
	}
	return amount
}

func calculateLWF(p statutoryLwfParams, wages int64, month int) (string, []statutoryComponent, error) {
	if !slices.Contains(p.DeductionMonths, month) {
		return "not_due", []statutoryComponent{}, nil
	}
	for _, s := range p.Slabs {
		if (statutoryBand{*s.FromPaise, s.ToPaise}).holds(wages) {
			unit := *p.RoundingUnitPaise
			return "applied", []statutoryComponent{
				{Component: "lwf_employee", Party: "employee", AmountPaise: lwfShare(*s.EmployeeFixedPaise, *s.EmployeeRatePpm, s.EmployeeCapPaise, wages, unit, p.RoundingMode)},
				{Component: "lwf_employer", Party: "employer", AmountPaise: lwfShare(*s.EmployerFixedPaise, *s.EmployerRatePpm, s.EmployerCapPaise, wages, unit, p.RoundingMode)},
			}, nil
		}
	}
	return "", nil, errStatutorySlabMissing
}

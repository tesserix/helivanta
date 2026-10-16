package workforce

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

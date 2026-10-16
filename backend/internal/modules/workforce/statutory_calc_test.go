package workforce

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func ptr[T any](v T) *T { return &v }

// Synthetic parameters: tests prove arithmetic, never statutory values.
func synthPF(mutate ...func(*statutoryPfParams)) statutoryPfParams {
	p := statutoryPfParams{WageBasis: "statutory_wages", ContributionBasis: "capped", WageCeilingPaise: ptr[int64](1000000), EmployeeRatePpm: ptr(100000), EmployerRatePpm: ptr(100000),
		EpsRatePpm: ptr(60000), EpsWageCeilingPaise: ptr[int64](1000000), EdliRatePpm: ptr(4000), EdliWageCeilingPaise: ptr[int64](800000), AdminRatePpm: ptr(3000),
		AdminMinimumPaise: ptr[int64](0), RoundingMode: "nearest", RoundingUnitPaise: ptr(1)}
	for _, m := range mutate {
		m(&p)
	}
	return p
}

func synthESI(mutate ...func(*statutoryEsiParams)) statutoryEsiParams {
	p := statutoryEsiParams{WageBasis: "statutory_wages", WageCeilingPaise: ptr[int64](1000000), DisabilityWageCeilingPaise: ptr[int64](1500000), EmployeeRatePpm: ptr(10000),
		EmployerRatePpm: ptr(30000), PeriodStartMonths: []int{4, 10}, RoundingMode: "nearest", RoundingUnitPaise: ptr(1)}
	for _, m := range mutate {
		m(&p)
	}
	return p
}

func amounts(components []statutoryComponent) map[string]int64 {
	out := map[string]int64{}
	for _, c := range components {
		out[c.Component] = c.AmountPaise
	}
	return out
}

func TestStatutoryWagesFollowTheDeclaredBasis(t *testing.T) {
	require.EqualValues(t, 700, statutoryWages("statutory_wages", 700, 900))
	require.EqualValues(t, 900, statutoryWages("gross", 700, 900))
}

func TestCalculatePFCeilingsAndCarveOut(t *testing.T) {
	for name, tc := range map[string]struct {
		params statutoryPfParams
		wages  int64
		base   int64
		want   map[string]int64
	}{
		"below every ceiling": {synthPF(), 700000, 700000, map[string]int64{"pf_employee": 70000, "pf_employer_epf": 28000, "pf_employer_eps": 42000, "pf_edli": 2800, "pf_admin": 2100}},
		"at the wage ceiling": {synthPF(), 1000000, 1000000, map[string]int64{"pf_employee": 100000, "pf_employer_epf": 40000, "pf_employer_eps": 60000, "pf_edli": 3200, "pf_admin": 3000}},
		"above, capped":       {synthPF(), 1200000, 1000000, map[string]int64{"pf_employee": 100000, "pf_employer_epf": 40000, "pf_employer_eps": 60000, "pf_edli": 3200, "pf_admin": 3000}},
		"above, actual": {synthPF(func(p *statutoryPfParams) { p.ContributionBasis = "actual" }), 1200000, 1200000,
			map[string]int64{"pf_employee": 120000, "pf_employer_epf": 60000, "pf_employer_eps": 60000, "pf_edli": 3200, "pf_admin": 3600}},
		"lower EPS ceiling": {synthPF(func(p *statutoryPfParams) { p.EpsWageCeilingPaise = ptr[int64](500000) }), 1000000, 1000000,
			map[string]int64{"pf_employee": 100000, "pf_employer_epf": 70000, "pf_employer_eps": 30000, "pf_edli": 3200, "pf_admin": 3000}},
		"EPS equals employer": {synthPF(func(p *statutoryPfParams) { p.EpsRatePpm = ptr(100000) }), 1000000, 1000000,
			map[string]int64{"pf_employee": 100000, "pf_employer_epf": 0, "pf_employer_eps": 100000, "pf_edli": 3200, "pf_admin": 3000}},
	} {
		base, components := calculatePF(tc.params, tc.wages)
		require.Equal(t, tc.base, base, name)
		require.Equal(t, tc.want, amounts(components), name)
		require.Len(t, components, 5, name)
		for _, c := range components {
			require.Equal(t, map[bool]string{true: "employee", false: "employer"}[c.Component == "pf_employee"], c.Party, name)
		}
	}
}

func TestCalculatePFRoundsEveryComponent(t *testing.T) {
	// 10% of 12,345 paise is 1,234.5; EPS at 6% is 740.7.
	for mode, want := range map[string]map[string]int64{
		"nearest": {"pf_employee": 1235, "pf_employer_epf": 494, "pf_employer_eps": 741},
		"up":      {"pf_employee": 1235, "pf_employer_epf": 494, "pf_employer_eps": 741},
		"down":    {"pf_employee": 1234, "pf_employer_epf": 494, "pf_employer_eps": 740},
	} {
		_, components := calculatePF(synthPF(func(p *statutoryPfParams) { p.RoundingMode = mode }), 12345)
		got := amounts(components)
		for k, v := range want {
			require.Equal(t, v, got[k], "%s %s", mode, k)
		}
	}
	_, components := calculatePF(synthPF(func(p *statutoryPfParams) { p.RoundingUnitPaise = ptr(100) }), 12345)
	require.EqualValues(t, 1200, amounts(components)["pf_employee"], "rounds to the configured unit")
	for _, wages := range []int64{0, 1, 49, 50, 99, 12345, 999999} {
		_, components := calculatePF(synthPF(func(p *statutoryPfParams) { p.EpsRatePpm, p.RoundingMode = ptr(99999), "up" }), wages)
		require.GreaterOrEqual(t, amounts(components)["pf_employer_epf"], int64(0), "EPF never goes negative at %d", wages)
	}
}

func TestCalculateESICoverageAndExemption(t *testing.T) {
	exempt := synthESI(func(p *statutoryEsiParams) { p.EmployeeExemptDailyPaise = ptr[int64](20000) })
	for name, tc := range map[string]struct {
		params     statutoryEsiParams
		wages      int64
		halfDays   int
		disability bool
		status     string
		want       map[string]int64
	}{
		"below ceiling":            {synthESI(), 900000, 60, false, "applied", map[string]int64{"esi_employee": 9000, "esi_employer": 27000}},
		"at ceiling":               {synthESI(), 1000000, 60, false, "applied", map[string]int64{"esi_employee": 10000, "esi_employer": 30000}},
		"above ceiling":            {synthESI(), 1000001, 60, false, "above_ceiling", map[string]int64{}},
		"disability lifts ceiling": {synthESI(), 1200000, 60, true, "applied", map[string]int64{"esi_employee": 12000, "esi_employer": 36000}},
		"above disability ceiling": {synthESI(), 1500001, 60, true, "above_ceiling", map[string]int64{}},
		"zero payable days":        {synthESI(), 900000, 0, false, "applied", map[string]int64{"esi_employee": 0, "esi_employer": 0}},
		"exempt at equality":       {exempt, 600000, 60, false, "applied", map[string]int64{"esi_employee": 0, "esi_employer": 18000}},
		"charged one paisa above":  {exempt, 600001, 60, false, "applied", map[string]int64{"esi_employee": 6000, "esi_employer": 18000}},
		"exemption uses paid days": {exempt, 600000, 59, false, "applied", map[string]int64{"esi_employee": 6000, "esi_employer": 18000}},
		"no exemption configured":  {synthESI(), 100, 60, false, "applied", map[string]int64{"esi_employee": 1, "esi_employer": 3}},
		"zero wages":               {synthESI(), 0, 60, false, "applied", map[string]int64{"esi_employee": 0, "esi_employer": 0}},
	} {
		status, components := calculateESI(tc.params, tc.wages, tc.halfDays, tc.disability)
		require.Equal(t, tc.status, status, name)
		require.Equal(t, tc.want, amounts(components), name)
	}
	_, components := calculateESI(synthESI(func(p *statutoryEsiParams) { p.RoundingMode = "up"; p.RoundingUnitPaise = ptr(100) }), 12345, 60, false)
	require.Equal(t, map[string]int64{"esi_employee": 200, "esi_employer": 400}, amounts(components))
}

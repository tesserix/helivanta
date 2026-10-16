package workforce

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

const deriveDay = "2026-10-12"

func at(clock string) time.Time {
	t, err := time.Parse(time.RFC3339, clock)
	if err != nil {
		panic(err)
	}
	return t
}

func onDay(clock string) time.Time { return at(deriveDay + "T" + clock + ":00Z") }

func shift(date, start, end string) attendanceAllocation {
	from, to := at(date+"T"+start+":00Z"), at(date+"T"+end+":00Z")
	if !to.After(from) {
		to = to.AddDate(0, 0, 1)
	}
	return attendanceAllocation{ID: uuid.New(), RosterID: uuid.New(), ShiftRevisionID: uuid.New(), StaffID: uuid.New(), RosterDate: date, RosterStatus: "frozen",
		ShiftCode: "GEN", ShiftName: "General", StartsAt: from, EndsAt: to, BreakMinutes: 30, GraceMinutes: 10}
}

func punch(direction string, when time.Time) attendanceObservation {
	return attendanceObservation{ID: uuid.New(), Direction: direction, PunchedAt: when, LocalDate: when.Format(time.DateOnly)}
}

func policy() *attendancePolicy {
	return &attendancePolicy{ID: uuid.New(), EarlyArrivalMinutes: 60, LateDepartureMinutes: 120, EarlyDepartureGraceMinutes: 10, ShortDutyMinutes: 30, DuplicateWindowMinutes: 5}
}

func inputs(allocations []attendanceAllocation, punches []attendanceObservation) attendanceInputs {
	return attendanceInputs{allocations: allocations, punches: punches, policies: map[string]*attendancePolicy{}, leave: map[string]leaveCover{}, overtime: map[string]int{}}
}

func TestEvaluateDerivesStatusFiguresAndExceptions(t *testing.T) {
	late := onDay("20:00")
	for name, tc := range map[string]struct {
		punches         []attendanceObservation
		cover           leaveCover
		status          string
		exceptions      []string
		worked, payable int
		expected        int
		lateMin, early  int
		unpaid          int
	}{
		"on time":              {[]attendanceObservation{punch("in", onDay("08:55")), punch("out", onDay("17:05"))}, leaveCover{}, "present", []string{}, 480, 450, 450, 0, 0, 0},
		"late inside grace":    {[]attendanceObservation{punch("in", onDay("09:10")), punch("out", onDay("17:00"))}, leaveCover{}, "present", []string{}, 470, 440, 450, 0, 0, 0},
		"late past grace":      {[]attendanceObservation{punch("in", onDay("09:20")), punch("out", onDay("17:00"))}, leaveCover{}, "present", []string{"late"}, 460, 430, 450, 20, 0, 0},
		"early departure":      {[]attendanceObservation{punch("in", onDay("09:00")), punch("out", onDay("16:40"))}, leaveCover{}, "present", []string{"early_departure"}, 460, 430, 450, 0, 20, 0},
		"short duty":           {[]attendanceObservation{punch("in", onDay("09:00")), punch("out", onDay("16:00"))}, leaveCover{}, "present", []string{"early_departure", "short_duty"}, 420, 390, 450, 0, 60, 0},
		"absent":               {nil, leaveCover{}, "absent", []string{"absent"}, 0, 0, 450, 0, 0, 2},
		"absent on half day":   {nil, leaveCover{firstHalf: true}, "absent", []string{"absent"}, 0, 0, 240, 0, 0, 1},
		"missing out":          {[]attendanceObservation{punch("in", onDay("09:30"))}, leaveCover{}, "missing_out", []string{"missing_punch", "late"}, 0, 0, 450, 30, 0, 0},
		"missing in":           {[]attendanceObservation{punch("out", onDay("17:00"))}, leaveCover{}, "missing_in", []string{"missing_punch"}, 0, 0, 450, 0, 0, 0},
		"duplicate in":         {[]attendanceObservation{punch("in", onDay("08:55")), punch("in", onDay("08:58")), punch("out", onDay("17:00"))}, leaveCover{}, "present", []string{"duplicate_punch"}, 480, 450, 450, 0, 0, 0},
		"full day leave":       {nil, leaveCover{firstHalf: true, secondHalf: true}, "on_leave", []string{}, 0, 0, 0, 0, 0, 0},
		"punched on leave":     {[]attendanceObservation{punch("in", onDay("09:00"))}, leaveCover{firstHalf: true, secondHalf: true}, "on_leave", []string{"punched_on_leave"}, 0, 0, 0, 0, 0, 0},
		"first half leave":     {[]attendanceObservation{punch("in", onDay("13:00")), punch("out", onDay("17:00"))}, leaveCover{firstHalf: true}, "present", []string{}, 240, 240, 240, 0, 0, 0},
		"second half leave":    {[]attendanceObservation{punch("in", onDay("09:00")), punch("out", onDay("13:00"))}, leaveCover{secondHalf: true}, "present", []string{}, 240, 240, 240, 0, 0, 0},
		"short leave excuses":  {[]attendanceObservation{punch("in", onDay("10:00")), punch("out", onDay("17:00"))}, leaveCover{shortMinutes: 60}, "present", []string{}, 420, 390, 390, 0, 0, 0},
		"short leave exceeded": {[]attendanceObservation{punch("in", onDay("10:30")), punch("out", onDay("17:00"))}, leaveCover{shortMinutes: 60}, "present", []string{"late"}, 390, 360, 390, 90, 0, 0},
	} {
		t.Run(name, func(t *testing.T) {
			s := evaluate(shift(deriveDay, "09:00", "17:00"), tc.punches, policy(), tc.cover, late)
			require.Equal(t, tc.status, s.Status)
			require.ElementsMatch(t, tc.exceptions, s.Exceptions)
			require.Equal(t, []int{tc.worked, tc.payable, tc.expected, tc.lateMin, tc.early, tc.unpaid},
				[]int{s.WorkedMinutes, s.PayableMinutes, s.ExpectedMinutes, s.LateMinutes, s.EarlyDepartureMinutes, s.UnpaidHalfDays})
		})
	}
}

func TestEvaluateWithholdsFiguresUntilTheShiftCanBeJudged(t *testing.T) {
	a := shift(deriveDay, "09:00", "17:00")
	require.Equal(t, "upcoming", evaluate(a, nil, policy(), leaveCover{}, onDay("08:00")).Status)
	open := evaluate(a, []attendanceObservation{punch("in", onDay("09:30"))}, policy(), leaveCover{}, onDay("12:00"))
	require.Equal(t, "in_progress", open.Status)
	require.Equal(t, 30, open.LateMinutes)
	require.Zero(t, open.PayableMinutes)
	require.Equal(t, "in_progress", evaluate(a, nil, policy(), leaveCover{}, onDay("18:00")).Status, "absence waits for the late-departure window")
	require.Equal(t, "absent", evaluate(a, nil, policy(), leaveCover{}, onDay("19:00")).Status)

	draft := a
	draft.RosterStatus = "draft"
	require.Equal(t, "unpublished", evaluate(draft, nil, policy(), leaveCover{}, onDay("20:00")).Status)
	none := evaluate(a, []attendanceObservation{punch("in", onDay("09:00"))}, nil, leaveCover{}, onDay("20:00"))
	require.Equal(t, "no_policy", none.Status)
	require.Nil(t, none.PolicyRevisionID)
	require.Len(t, none.PunchIDs, 1)
}

func TestEvaluateIgnoresAnOutBeforeTheFirstIn(t *testing.T) {
	stray, in, out := punch("out", onDay("08:30")), punch("in", onDay("09:00")), punch("out", onDay("17:00"))
	s := evaluate(shift(deriveDay, "09:00", "17:00"), []attendanceObservation{stray, in, out}, policy(), leaveCover{}, onDay("20:00"))
	require.Equal(t, "present", s.Status)
	require.Equal(t, []uuid.UUID{stray.ID}, s.IgnoredPunchIDs)
	require.Equal(t, in.ID, *s.FirstInPunchID)
	require.Equal(t, out.ID, *s.LastOutPunchID)
}

func TestCollapseKeepsEarliestInAndLatestOutOfEachRun(t *testing.T) {
	a, b, c := punch("in", onDay("08:50")), punch("in", onDay("08:53")), punch("in", onDay("09:10"))
	x, y := punch("out", onDay("17:00")), punch("out", onDay("17:04"))
	kept, dupes := collapse([]attendanceObservation{a, b, c, x, y}, 5*time.Minute)
	require.Equal(t, []uuid.UUID{a.ID, c.ID, y.ID}, ids(kept))
	require.Equal(t, []uuid.UUID{b.ID, x.ID}, ids(dupes))
}

func ids(list []attendanceObservation) []uuid.UUID {
	out := []uuid.UUID{}
	for _, p := range list {
		out = append(out, p.ID)
	}
	return out
}

func TestDeriveDaysAssignsOvernightAndStrayPunchesToTheirDates(t *testing.T) {
	night := shift(deriveDay, "22:00", "06:00")
	in, out := punch("in", onDay("21:55")), punch("out", at("2026-10-13T06:05:00Z"))
	stray := punch("in", at("2026-10-13T13:00:00Z"))
	data := inputs([]attendanceAllocation{night}, []attendanceObservation{in, out, stray})
	data.policies[deriveDay] = policy()
	data.overtime["2026-10-13"] = 45
	days := deriveDays(data, deriveDay, "2026-10-13", at("2026-10-14T00:00:00Z"))

	first := days[deriveDay]
	require.Len(t, first.Shifts, 1)
	require.Equal(t, "present", first.Shifts[0].Status)
	require.Equal(t, []uuid.UUID{in.ID, out.ID}, first.Shifts[0].PunchIDs)
	require.Equal(t, 450, first.PayableMinutes)
	require.Empty(t, first.StrayPunchIDs)

	second := days["2026-10-13"]
	require.Empty(t, second.Shifts)
	require.Equal(t, []uuid.UUID{stray.ID}, second.StrayPunchIDs)
	require.Equal(t, []string{"stray_punch"}, second.Exceptions)
	require.Equal(t, 45, second.ApprovedOvertimeMinutes)
	require.Equal(t, 45, second.PayableMinutes)

	require.NotContains(t, deriveDays(data, "2026-10-13", "2026-10-13", at("2026-10-14T00:00:00Z")), deriveDay, "dates outside the range are not reported")
}

func TestAssignGivesEachPunchToTheNearestAdjacentShift(t *testing.T) {
	morning, evening := shift(deriveDay, "08:00", "12:00"), shift(deriveDay, "12:30", "16:30")
	out, in := punch("out", onDay("12:10")), punch("in", onDay("12:20"))
	data := inputs([]attendanceAllocation{morning, evening}, []attendanceObservation{out, in})
	data.policies[deriveDay] = policy()
	owned, stray := data.assign()
	require.Empty(t, stray)
	require.Equal(t, []uuid.UUID{out.ID}, ids(owned[morning.ID]))
	require.Equal(t, []uuid.UUID{in.ID}, ids(owned[evening.ID]))

	data.policies = map[string]*attendancePolicy{}
	_, stray = data.assign()
	require.Len(t, stray, 2, "without a policy only the shift itself claims punches")
}

func TestDeriveDaysOrdersExceptionsByContract(t *testing.T) {
	a := shift(deriveDay, "09:00", "17:00")
	data := inputs([]attendanceAllocation{a}, []attendanceObservation{punch("in", onDay("09:30")), punch("in", onDay("09:31")), punch("in", onDay("23:30"))})
	data.policies[deriveDay] = policy()
	d := deriveDays(data, deriveDay, deriveDay, onDay("23:59"))[deriveDay]
	require.Equal(t, []string{"late", "missing_punch", "duplicate_punch", "stray_punch"}, d.Exceptions)
}

func TestLeaveCoverKind(t *testing.T) {
	for want, cover := range map[string]leaveCover{
		"full_day": {firstHalf: true, secondHalf: true}, "first_half": {firstHalf: true}, "second_half": {secondHalf: true}, "short": {shortMinutes: 30}, "none": {},
	} {
		require.Equal(t, want, cover.kind())
	}
}

func TestTallyCountsEveryCodeInContractOrder(t *testing.T) {
	tl := newTally()
	tl.add(attendanceDay{Shifts: []attendanceShift{{Status: "present", Exceptions: []string{"late"}}, {Status: "absent", Exceptions: []string{"absent"}, UnpaidHalfDays: 2}}, StrayPunchIDs: []uuid.UUID{uuid.New()}})
	statuses := counts(attendanceStatuses, tl.statuses)
	require.Len(t, statuses, len(attendanceStatuses))
	require.Equal(t, attendanceCount{Code: "present", Count: 1}, statuses[0])
	require.Equal(t, attendanceCount{Code: "absent", Count: 1}, statuses[1])
	require.Equal(t, attendanceCount{Code: "on_leave", Count: 0}, statuses[2])
	exceptions := counts(attendanceExceptions, tl.exceptions)
	require.Equal(t, attendanceCount{Code: "late", Count: 1}, exceptions[0])
	require.Equal(t, attendanceCount{Code: "stray_punch", Count: 1}, exceptions[len(exceptions)-1])
	require.Equal(t, 2, unpaid(attendanceDay{Shifts: []attendanceShift{{UnpaidHalfDays: 2}}}))
}

func TestPersonDayFillsAnEmptyDayForSomeoneOnlyOnAdjacentDates(t *testing.T) {
	person := attendancePerson{ID: uuid.New(), EmployeeCode: "E1", DisplayName: "Asha"}
	data := inputs(nil, nil)
	data.leave[deriveDay] = leaveCover{shortMinutes: 30}
	d := personDay(&data, person, deriveDay, "Asia/Kolkata", onDay("12:00"))
	require.Equal(t, person.ID, d.StaffID)
	require.Equal(t, "Asia/Kolkata", d.Timezone)
	require.Equal(t, "short", d.Leave)
	require.Equal(t, 30, d.ShortLeaveMinutes)
	require.NotNil(t, d.Shifts)
}

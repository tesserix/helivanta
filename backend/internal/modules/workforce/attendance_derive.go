package workforce

import (
	"slices"
	"time"

	"github.com/google/uuid"
)

// attendancePolicy is the revision of the attendance settings in force on a roster date.
type attendancePolicy struct {
	ID                         uuid.UUID
	EarlyArrivalMinutes        int
	LateDepartureMinutes       int
	EarlyDepartureGraceMinutes int
	ShortDutyMinutes           int
	DuplicateWindowMinutes     int
}

// attendanceAllocation is one active roster allocation with the shift revision it pins.
type attendanceAllocation struct {
	ID, RosterID, ShiftRevisionID, StaffID uuid.UUID
	RosterDate                             string
	RosterStatus                           string
	ShiftCode, ShiftName                   string
	StartsAt, EndsAt                       time.Time
	BreakMinutes, GraceMinutes             int
}

// attendanceObservation is a live (unvoided) punch.
type attendanceObservation struct {
	ID, StaffID uuid.UUID
	Direction   string
	PunchedAt   time.Time
	LocalDate   string
}

// leaveCover is the approved leave on one person-day.
type leaveCover struct {
	firstHalf, secondHalf bool
	shortMinutes          int
}

func (l leaveCover) kind() string {
	switch {
	case l.firstHalf && l.secondHalf:
		return "full_day"
	case l.firstHalf:
		return "first_half"
	case l.secondHalf:
		return "second_half"
	case l.shortMinutes > 0:
		return "short"
	}
	return "none"
}

// attendanceInputs holds everything derivation reads for one person; policies are keyed by roster date.
type attendanceInputs struct {
	allocations []attendanceAllocation
	punches     []attendanceObservation
	policies    map[string]*attendancePolicy
	leave       map[string]leaveCover
	overtime    map[string]int
}

// window is the span in which a punch belongs to an allocation; with no policy only the shift itself counts.
func (in attendanceInputs) window(a attendanceAllocation) (time.Time, time.Time) {
	p := in.policies[a.RosterDate]
	if p == nil {
		return a.StartsAt, a.EndsAt
	}
	return a.StartsAt.Add(-time.Duration(p.EarlyArrivalMinutes) * time.Minute), a.EndsAt.Add(time.Duration(p.LateDepartureMinutes) * time.Minute)
}

// assign gives each punch to the allocation whose window contains it, nearest start for in and nearest end for out; the rest are stray.
func (in attendanceInputs) assign() (map[uuid.UUID][]attendanceObservation, []attendanceObservation) {
	owned := map[uuid.UUID][]attendanceObservation{}
	var stray []attendanceObservation
	for _, p := range in.punches {
		best, bestGap := -1, time.Duration(0)
		for i, a := range in.allocations {
			from, to := in.window(a)
			if p.PunchedAt.Before(from) || p.PunchedAt.After(to) {
				continue
			}
			anchor := a.StartsAt
			if p.Direction == "out" {
				anchor = a.EndsAt
			}
			gap := p.PunchedAt.Sub(anchor).Abs()
			if best < 0 || gap < bestGap {
				best, bestGap = i, gap
			}
		}
		if best < 0 {
			stray = append(stray, p)
			continue
		}
		owned[in.allocations[best].ID] = append(owned[in.allocations[best].ID], p)
	}
	return owned, stray
}

// collapse keeps the earliest in and the latest out of each run of same-direction punches closer than the duplicate window.
func collapse(punches []attendanceObservation, window time.Duration) (kept, duplicates []attendanceObservation) {
	for _, dir := range []string{"in", "out"} {
		var run []attendanceObservation
		flush := func() {
			if len(run) == 0 {
				return
			}
			keep := 0
			if dir == "out" {
				keep = len(run) - 1
			}
			for i, p := range run {
				if i == keep {
					kept = append(kept, p)
				} else {
					duplicates = append(duplicates, p)
				}
			}
			run = nil
		}
		for _, p := range punches {
			if p.Direction != dir {
				continue
			}
			if len(run) > 0 && p.PunchedAt.Sub(run[len(run)-1].PunchedAt) >= window {
				flush()
			}
			run = append(run, p)
		}
		flush()
	}
	byTime := func(a, b attendanceObservation) int { return a.PunchedAt.Compare(b.PunchedAt) }
	slices.SortStableFunc(kept, byTime)
	slices.SortStableFunc(duplicates, byTime)
	return kept, duplicates
}

func minutes(d time.Duration) int { return int(d / time.Minute) }

func addException(list []string, code string) []string {
	if slices.Contains(list, code) {
		return list
	}
	return append(list, code)
}

// evaluate derives one shift from its punches; figures are only produced once the shift's window has closed or both punches exist.
func evaluate(a attendanceAllocation, punches []attendanceObservation, policy *attendancePolicy, cover leaveCover, now time.Time) attendanceShift {
	s := attendanceShift{AllocationID: a.ID, RosterID: a.RosterID, ShiftRevisionID: a.ShiftRevisionID, ShiftCode: a.ShiftCode, ShiftName: a.ShiftName,
		StartsAt: a.StartsAt.UTC(), EndsAt: a.EndsAt.UTC(), BreakMinutes: a.BreakMinutes, GraceMinutes: a.GraceMinutes,
		Exceptions: []string{}, PunchIDs: []uuid.UUID{}, IgnoredPunchIDs: []uuid.UUID{}}
	for _, p := range punches {
		s.PunchIDs = append(s.PunchIDs, p.ID)
	}
	if a.RosterStatus != "frozen" {
		s.Status = "unpublished"
		return s
	}
	if policy == nil {
		s.Status = "no_policy"
		return s
	}
	s.PolicyRevisionID = &policy.ID
	duration := minutes(a.EndsAt.Sub(a.StartsAt))
	kept, duplicates := collapse(punches, time.Duration(policy.DuplicateWindowMinutes)*time.Minute)
	for _, p := range duplicates {
		s.IgnoredPunchIDs = append(s.IgnoredPunchIDs, p.ID)
		s.Exceptions = addException(s.Exceptions, "duplicate_punch")
	}
	var in, out *attendanceObservation
	for i := range kept {
		if kept[i].Direction == "in" && in == nil {
			in = &kept[i]
		}
	}
	for i := range kept {
		p := &kept[i]
		if p.Direction != "out" {
			continue
		}
		// An out at or before the first in closes nothing.
		if in != nil && !p.PunchedAt.After(in.PunchedAt) {
			s.IgnoredPunchIDs = append(s.IgnoredPunchIDs, p.ID)
			continue
		}
		out = p
	}
	if in != nil {
		at := in.PunchedAt.UTC()
		s.FirstIn, s.FirstInPunchID = &at, &in.ID
	}
	if out != nil {
		at := out.PunchedAt.UTC()
		s.LastOut, s.LastOutPunchID = &at, &out.ID
	}

	switch cover.kind() {
	case "full_day":
		s.Status = "on_leave"
		if len(punches) > 0 {
			s.Exceptions = addException(s.Exceptions, "punched_on_leave")
		}
		return s
	case "first_half", "second_half":
		// A half day is half the shift with no break; the other half is paid as leave.
		s.ExpectedMinutes = duration / 2
	default:
		s.ExpectedMinutes = max(0, duration-a.BreakMinutes-cover.shortMinutes)
	}

	closes := a.EndsAt.Add(time.Duration(policy.LateDepartureMinutes) * time.Minute)
	switch {
	case in != nil && out != nil:
		s.Status = "present"
	case now.Before(a.StartsAt) && len(kept) == 0:
		s.Status = "upcoming"
		return s
	case now.Before(closes):
		s.Status = "in_progress"
	case in == nil && out == nil:
		s.Status = "absent"
		s.Exceptions = addException(s.Exceptions, "absent")
		s.UnpaidHalfDays = 2
		if cover.firstHalf || cover.secondHalf {
			s.UnpaidHalfDays = 1
		}
		return s
	case in == nil:
		s.Status = "missing_in"
		s.Exceptions = addException(s.Exceptions, "missing_punch")
		return s
	default:
		s.Status = "missing_out"
		s.Exceptions = addException(s.Exceptions, "missing_punch")
	}

	excused := cover.shortMinutes
	if in != nil {
		if late := minutes(in.PunchedAt.Sub(a.StartsAt)); late > a.GraceMinutes && !cover.firstHalf {
			if late <= excused {
				excused -= late
			} else {
				s.LateMinutes = late
				s.Exceptions = addException(s.Exceptions, "late")
			}
		}
	}
	if s.Status != "present" {
		return s
	}
	if early := minutes(a.EndsAt.Sub(out.PunchedAt)); early > policy.EarlyDepartureGraceMinutes && !cover.secondHalf && early > excused {
		s.EarlyDepartureMinutes = early
		s.Exceptions = addException(s.Exceptions, "early_departure")
	}
	from, to := in.PunchedAt, out.PunchedAt
	if from.Before(a.StartsAt) {
		from = a.StartsAt
	}
	if to.After(a.EndsAt) {
		to = a.EndsAt
	}
	s.WorkedMinutes = max(0, minutes(to.Sub(from)))
	payable := s.WorkedMinutes
	if !cover.firstHalf && !cover.secondHalf {
		payable = max(0, payable-a.BreakMinutes)
	}
	s.PayableMinutes = min(payable, s.ExpectedMinutes)
	if s.ExpectedMinutes-s.PayableMinutes > policy.ShortDutyMinutes {
		s.Exceptions = addException(s.Exceptions, "short_duty")
	}
	return s
}

// deriveDays turns one person's inputs into a day per roster date, stray-punch date or overtime date inside [from, to].
func deriveDays(in attendanceInputs, from, to string, now time.Time) map[string]*attendanceDay {
	owned, stray := in.assign()
	days := map[string]*attendanceDay{}
	day := func(date string) *attendanceDay {
		if d, ok := days[date]; ok {
			return d
		}
		cover := in.leave[date]
		d := &attendanceDay{Date: date, Leave: cover.kind(), ShortLeaveMinutes: cover.shortMinutes, Shifts: []attendanceShift{}, StrayPunchIDs: []uuid.UUID{}, Exceptions: []string{}}
		days[date] = d
		return d
	}
	inside := func(date string) bool { return date >= from && date <= to }
	for _, a := range in.allocations {
		if !inside(a.RosterDate) {
			continue
		}
		d := day(a.RosterDate)
		s := evaluate(a, owned[a.ID], in.policies[a.RosterDate], in.leave[a.RosterDate], now)
		d.Shifts = append(d.Shifts, s)
		d.PayableMinutes += s.PayableMinutes
		for _, e := range s.Exceptions {
			d.Exceptions = addException(d.Exceptions, e)
		}
	}
	for _, p := range stray {
		if !inside(p.LocalDate) {
			continue
		}
		d := day(p.LocalDate)
		d.StrayPunchIDs = append(d.StrayPunchIDs, p.ID)
		d.Exceptions = addException(d.Exceptions, "stray_punch")
	}
	for date, minutes := range in.overtime {
		if inside(date) && minutes > 0 {
			d := day(date)
			d.ApprovedOvertimeMinutes = minutes
			d.PayableMinutes += minutes
		}
	}
	for _, d := range days {
		slices.SortFunc(d.Shifts, func(a, b attendanceShift) int { return a.StartsAt.Compare(b.StartsAt) })
		// Exceptions follow the contract order so responses are stable.
		slices.SortFunc(d.Exceptions, func(a, b string) int {
			return slices.Index(attendanceExceptions, a) - slices.Index(attendanceExceptions, b)
		})
	}
	return days
}

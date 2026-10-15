package workforce

import (
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
)

// leavePolicyEntry is one balance-tracked leave type pinned by an assignment's structure revision.
type leavePolicyEntry struct {
	TypeRevisionID uuid.UUID
	Annual         int
	Carry          int
	Accrual        string
	Proration      string
}

type leaveAssignmentRevision struct {
	ID          uuid.UUID
	Version     int64
	EffectiveOn time.Time
	Active      bool
	Entries     map[uuid.UUID]leavePolicyEntry
}

type leaveCredit struct {
	TypeID               uuid.UUID
	TypeRevisionID       uuid.UUID
	AssignmentRevisionID uuid.UUID
	Kind                 string
	HalfDays             int
	EffectiveOn          time.Time
	Key                  string
}

type leavePeriod struct {
	Year  int
	Start time.Time
}

func newLeavePeriod(year, startMonth int) leavePeriod {
	return leavePeriod{Year: year, Start: time.Date(year, time.Month(startMonth), 1, 0, 0, 0, 0, time.UTC)}
}

func (p leavePeriod) monthStart(k int) time.Time { return p.Start.AddDate(0, k-1, 0) }
func (p leavePeriod) monthEnd(k int) time.Time   { return p.Start.AddDate(0, k, -1) }
func (p leavePeriod) end() time.Time             { return p.Start.AddDate(1, 0, -1) }

// leaveInForce returns the latest revision effective on day; revisions are ordered by version and never move earlier.
func leaveInForce(revs []leaveAssignmentRevision, day time.Time) *leaveAssignmentRevision {
	var found *leaveAssignmentRevision
	for i := range revs {
		if !revs[i].EffectiveOn.After(day) {
			found = &revs[i]
		}
	}
	return found
}

// leaveCandidates are the days in [from,to] on which the assignment in force can change.
func leaveCandidates(revs []leaveAssignmentRevision, from, to time.Time) []time.Time {
	days := []time.Time{from}
	for _, r := range revs {
		if r.EffectiveOn.After(from) && !r.EffectiveOn.After(to) && !slices.ContainsFunc(days, r.EffectiveOn.Equal) {
			days = append(days, r.EffectiveOn)
		}
	}
	slices.SortFunc(days, time.Time.Compare)
	return days
}

// leaveCredits lists every allocation and monthly credit owed through month `through`; keys make reposting a no-op.
// A month is credited under the assignment in force on its first day, or on the day an assignment starts mid-month.
// Upfront allowances are granted once per year; after monthly credits or with join-month proration they cover the remaining months only.
func leaveCredits(staff uuid.UUID, period leavePeriod, through int, revs []leaveAssignmentRevision) []leaveCredit {
	var types []uuid.UUID
	for _, r := range revs {
		for t := range r.Entries {
			if !slices.Contains(types, t) {
				types = append(types, t)
			}
		}
	}
	slices.SortFunc(types, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	var out []leaveCredit
	for _, t := range types {
		allocated, credited := false, false
		for k := 1; k <= through; k++ {
			days := leaveCandidates(revs, period.monthStart(k), period.monthEnd(k))
			if !allocated {
				for _, day := range days {
					r := leaveInForce(revs, day)
					if r == nil || !r.Active {
						continue
					}
					e, ok := r.Entries[t]
					if !ok || e.Accrual != "upfront" || e.Annual == 0 {
						continue
					}
					allocated = true
					amount := e.Annual
					if credited || e.Proration == "join_month" {
						amount = e.Annual * (13 - k) / 12
					}
					if amount > 0 {
						out = append(out, leaveCredit{TypeID: t, TypeRevisionID: e.TypeRevisionID, AssignmentRevisionID: r.ID, Kind: "allocation", HalfDays: amount, EffectiveOn: day, Key: fmt.Sprintf("allocation:%s:%s:%d", staff, t, period.Year)})
					}
					break
				}
			}
			if allocated {
				continue
			}
			for _, day := range days {
				r := leaveInForce(revs, day)
				if r == nil || !r.Active {
					continue
				}
				if e, ok := r.Entries[t]; ok && e.Accrual == "monthly" {
					if amount := e.Annual*k/12 - e.Annual*(k-1)/12; amount > 0 {
						credited = true
						out = append(out, leaveCredit{TypeID: t, TypeRevisionID: e.TypeRevisionID, AssignmentRevisionID: r.ID, Kind: "accrual", HalfDays: amount, EffectiveOn: day, Key: fmt.Sprintf("accrual:%s:%s:%d:%d", staff, t, period.Year, k)})
					}
				}
				break
			}
		}
	}
	return out
}

// leaveCarry splits a closing balance into the carried part and the part that expires.
func leaveCarry(balance, limit int) (carried, expired int) {
	if balance <= 0 {
		return 0, 0
	}
	carried = min(balance, limit)
	return carried, balance - carried
}

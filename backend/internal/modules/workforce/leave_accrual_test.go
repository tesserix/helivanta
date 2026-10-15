package workforce

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

var (
	accrualStaff = uuid.MustParse("a1111111-1111-4111-8111-111111111111")
	accrualType  = uuid.MustParse("b1111111-1111-4111-8111-111111111111")
	accrualRev   = uuid.MustParse("c1111111-1111-4111-8111-111111111111")
)

func day(value string) time.Time {
	d, err := time.Parse("2006-01-02", value)
	if err != nil {
		panic(err)
	}
	return d
}

func revision(version int64, effective string, active bool, accrual, proration string, annual int) leaveAssignmentRevision {
	return leaveAssignmentRevision{ID: uuid.New(), Version: version, EffectiveOn: day(effective), Active: active, Entries: map[uuid.UUID]leavePolicyEntry{
		accrualType: {TypeRevisionID: accrualRev, Annual: annual, Carry: 10, Accrual: accrual, Proration: proration},
	}}
}

func total(credits []leaveCredit) (sum int) {
	for _, c := range credits {
		sum += c.HalfDays
	}
	return sum
}

func TestMonthlyCreditsSumExactlyToTheAllowance(t *testing.T) {
	period := newLeavePeriod(2026, 4)
	for _, annual := range []int{1, 5, 24, 25, 730} {
		credits := leaveCredits(accrualStaff, period, 12, []leaveAssignmentRevision{revision(1, "2026-01-01", true, "monthly", "none", annual)})
		require.Equal(t, annual, total(credits), annual)
		for _, c := range credits {
			require.Equal(t, "accrual", c.Kind)
			require.Positive(t, c.HalfDays)
		}
	}
	credits := leaveCredits(accrualStaff, period, 12, []leaveAssignmentRevision{revision(1, "2026-01-01", true, "monthly", "none", 24)})
	require.Len(t, credits, 12)
	require.Equal(t, day("2026-04-01"), credits[0].EffectiveOn)
	require.Equal(t, day("2027-03-01"), credits[11].EffectiveOn)
	require.Equal(t, "accrual:"+accrualStaff.String()+":"+accrualType.String()+":2026:1", credits[0].Key)
}

func TestPartialPostingIsAPrefixOfTheFullYear(t *testing.T) {
	period := newLeavePeriod(2026, 1)
	revs := []leaveAssignmentRevision{revision(1, "2026-02-10", true, "monthly", "none", 25), revision(2, "2026-07-01", true, "monthly", "none", 13)}
	full := leaveCredits(accrualStaff, period, 12, revs)
	for through := 1; through <= 12; through++ {
		part := leaveCredits(accrualStaff, period, through, revs)
		require.Equal(t, append([]leaveCredit{}, full[:len(part)]...), append([]leaveCredit{}, part...), through)
	}
}

func TestJoinMonthAndInactivity(t *testing.T) {
	period := newLeavePeriod(2026, 1)
	joined := leaveCredits(accrualStaff, period, 12, []leaveAssignmentRevision{revision(1, "2026-03-15", true, "monthly", "join_month", 24)})
	require.Equal(t, day("2026-03-15"), joined[0].EffectiveOn)
	require.Equal(t, 24-4, total(joined))

	paused := leaveCredits(accrualStaff, period, 12, []leaveAssignmentRevision{
		revision(1, "2026-01-01", true, "monthly", "none", 24),
		revision(2, "2026-05-01", false, "monthly", "none", 24),
		revision(3, "2026-08-20", true, "monthly", "none", 24),
	})
	var months []string
	for _, c := range paused {
		months = append(months, c.EffectiveOn.Format("2006-01-02"))
	}
	require.Equal(t, []string{"2026-01-01", "2026-02-01", "2026-03-01", "2026-04-01", "2026-08-20", "2026-09-01", "2026-10-01", "2026-11-01", "2026-12-01"}, months)

	require.Empty(t, leaveCredits(accrualStaff, period, 12, []leaveAssignmentRevision{revision(1, "2027-01-01", true, "monthly", "none", 24)}))
	require.Empty(t, leaveCredits(accrualStaff, period, 12, []leaveAssignmentRevision{revision(1, "2026-01-01", false, "monthly", "none", 24)}))
}

func TestUpfrontAllocationAndProration(t *testing.T) {
	period := newLeavePeriod(2026, 1)
	full := leaveCredits(accrualStaff, period, 1, []leaveAssignmentRevision{revision(1, "2025-06-01", true, "upfront", "join_month", 24)})
	require.Equal(t, []leaveCredit{{TypeID: accrualType, TypeRevisionID: accrualRev, AssignmentRevisionID: full[0].AssignmentRevisionID, Kind: "allocation", HalfDays: 24, EffectiveOn: day("2026-01-01"), Key: "allocation:" + accrualStaff.String() + ":" + accrualType.String() + ":2026"}}, full)

	require.Empty(t, leaveCredits(accrualStaff, period, 9, []leaveAssignmentRevision{revision(1, "2026-10-05", true, "upfront", "join_month", 24)}))
	prorated := leaveCredits(accrualStaff, period, 12, []leaveAssignmentRevision{revision(1, "2026-10-05", true, "upfront", "join_month", 24)})
	require.Equal(t, 6, total(prorated))
	require.Equal(t, day("2026-10-05"), prorated[0].EffectiveOn)
	unprorated := leaveCredits(accrualStaff, period, 12, []leaveAssignmentRevision{revision(1, "2026-10-05", true, "upfront", "none", 24)})
	require.Equal(t, 24, total(unprorated))
	require.Len(t, leaveCredits(accrualStaff, period, 12, []leaveAssignmentRevision{revision(1, "2026-01-01", true, "upfront", "none", 24), revision(2, "2026-06-01", true, "upfront", "none", 30)}), 1, "one allowance per year")
}

func TestModeChangesNeverDoubleCredit(t *testing.T) {
	period := newLeavePeriod(2026, 1)
	toUpfront := leaveCredits(accrualStaff, period, 12, []leaveAssignmentRevision{revision(1, "2026-01-01", true, "monthly", "none", 24), revision(2, "2026-04-01", true, "upfront", "none", 24)})
	require.Equal(t, 24, total(toUpfront), "three monthly credits then nine months upfront")
	require.Equal(t, "allocation", toUpfront[len(toUpfront)-1].Kind)
	toMonthly := leaveCredits(accrualStaff, period, 12, []leaveAssignmentRevision{revision(1, "2026-01-01", true, "upfront", "none", 24), revision(2, "2026-04-01", true, "monthly", "none", 24)})
	require.Equal(t, 24, total(toMonthly), "the year's allowance was already granted")
}

func TestCarrySplitsTheClosingBalance(t *testing.T) {
	for _, tc := range []struct{ balance, limit, carried, expired int }{{30, 10, 10, 20}, {6, 10, 6, 0}, {0, 10, 0, 0}, {-2, 10, 0, 0}, {8, 0, 0, 8}} {
		carried, expired := leaveCarry(tc.balance, tc.limit)
		require.Equal(t, [2]int{tc.carried, tc.expired}, [2]int{carried, expired}, tc)
	}
}

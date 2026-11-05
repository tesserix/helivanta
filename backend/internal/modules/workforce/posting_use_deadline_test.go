package workforce

import (
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestCurrentPostingUseTruncatesScheduledEmploymentEnd(t *testing.T) {
	checked := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	generationID := uuid.Must(uuid.NewV7())
	row := StaffPostingProposal{Status: "approved", GenerationID: generationID, StaffVersion: 4, EmploymentEpoch: 3, StartsAt: checked.Add(-time.Hour), EligibilityUntil: checked.Add(24 * time.Hour)}
	source := EmploymentPeriodSource{StaffVersion: 4, EmploymentEpoch: 3}
	generation := employmentGenerationRecord{EmploymentGeneration: EmploymentGeneration{ID: generationID, EmploymentEpoch: 3, StartsAt: checked.Add(-2 * time.Hour)}}
	scheduledEnd := checked.Add(3 * time.Hour)
	until, err := currentPostingUseDeadline(row, source, generation, "effective", scheduledEnd, checked)
	require.NoError(t, err)
	require.Equal(t, scheduledEnd, until, "a future source ending caps use without pretending the original posting remains valid beyond it")
	_, err = currentPostingUseDeadline(row, source, generation, "effective", scheduledEnd, scheduledEnd)
	require.ErrorIs(t, err, authz.ErrScopedCommandDenied, "no command may begin at the effective end boundary")
}

func TestCurrentPostingUseKeepsNominalBoundsAndRejectsInactiveSources(t *testing.T) {
	checked := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	generationID := uuid.Must(uuid.NewV7())
	row := StaffPostingProposal{Status: "approved", GenerationID: generationID, StaffVersion: 4, EmploymentEpoch: 3, StartsAt: checked.Add(-time.Hour), EligibilityUntil: checked.Add(4 * time.Hour)}
	source := EmploymentPeriodSource{StaffVersion: 4, EmploymentEpoch: 3}
	generation := employmentGenerationRecord{EmploymentGeneration: EmploymentGeneration{ID: generationID, EmploymentEpoch: 3, StartsAt: checked.Add(-2 * time.Hour)}}
	until, err := currentPostingUseDeadline(row, source, generation, "effective", checked.Add(12*time.Hour), checked)
	require.NoError(t, err)
	require.Equal(t, row.EligibilityUntil, until, "a renewed term cannot extend an already approved posting")
	end := checked.Add(time.Hour)
	row.EndsAt = &end
	until, err = currentPostingUseDeadline(row, source, generation, "effective", checked.Add(12*time.Hour), checked)
	require.NoError(t, err)
	require.Equal(t, end, until)
	for _, state := range []string{"not_started", "ended", "suspended", "withdrawn", "unknown"} {
		t.Run(state, func(t *testing.T) {
			_, err := currentPostingUseDeadline(row, source, generation, state, checked.Add(time.Hour), checked)
			require.ErrorIs(t, err, authz.ErrScopedCommandDenied)
		})
	}
	changed := source
	changed.EmploymentEpoch++
	_, err = currentPostingUseDeadline(row, changed, generation, "effective", checked.Add(time.Hour), checked)
	require.ErrorIs(t, err, authz.ErrScopedCommandDenied)
	future := row
	future.StartsAt = checked.Add(time.Minute)
	_, err = currentPostingUseDeadline(future, source, generation, "effective", checked.Add(time.Hour), checked)
	require.ErrorIs(t, err, authz.ErrScopedCommandDenied)
}

func TestPostingSourcePlanPinsSupervisorAndStructuralPath(t *testing.T) {
	tenant := "11111111-1111-4111-8111-111111111111"
	hospital := uuid.New()
	checked := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	parent := StaffPostingProposal{Status: "approved", HospitalID: hospital, ID: uuid.Must(uuid.NewV7()), RevisionID: uuid.Must(uuid.NewV7()), StaffID: uuid.New(), DepartmentID: uuid.Must(uuid.NewV7()), DepartmentRevisionID: uuid.Must(uuid.NewV7()), StartsAt: checked.Add(-2 * time.Hour), EligibilityUntil: checked.Add(2 * time.Hour)}
	child := parent
	child.ID = uuid.Must(uuid.NewV7())
	child.RevisionID = uuid.Must(uuid.NewV7())
	child.StaffID = uuid.New()
	child.DepartmentID = uuid.Must(uuid.NewV7())
	child.DepartmentRevisionID = uuid.Must(uuid.NewV7())
	child.StartsAt = checked.Add(-time.Hour)
	child.EligibilityUntil = checked.Add(3 * time.Hour)
	intent := StaffPostingSupervisionIntent{Kind: "department_member", SupervisorStaffID: &parent.StaffID, SupervisorProposalID: &parent.ID, SupervisorRevisionID: &parent.RevisionID}
	plan, err := collectPostingSourcePlan(tenant, hospital, child, intent, func(scope postingDraftScope, revision uuid.UUID) (postingChainSource, error) {
		require.Equal(t, parent.StaffID, scope.staff)
		require.Equal(t, parent.ID, scope.draft)
		require.Equal(t, parent.RevisionID, revision)
		return postingChainSource{Posting: parent, Intent: StaffPostingSupervisionIntent{Kind: "hospital_management"}}, nil
	})
	require.NoError(t, err)
	require.Len(t, plan.Sources, 1)
	require.Len(t, plan.References, 2)
	require.Equal(t, parent.EligibilityUntil, plan.EligibilityUntil)
	require.Equal(t, child.DepartmentID, plan.References[0].DepartmentID)
	require.Equal(t, parent.DepartmentID, plan.References[1].DepartmentID)
}

func TestPostingSourcePlanBoundsAndPinsEveryAncestor(t *testing.T) {
	tenant := "11111111-1111-4111-8111-111111111111"
	hospital := uuid.New()
	checked := time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	build := func(count int) (StaffPostingProposal, StaffPostingSupervisionIntent, map[uuid.UUID]postingChainSource) {
		rows := make([]postingChainSource, count)
		for i := range rows {
			rows[i].Posting = StaffPostingProposal{Status: "approved", HospitalID: hospital, StaffID: uuid.New(), ID: uuid.Must(uuid.NewV7()), RevisionID: uuid.Must(uuid.NewV7()), DepartmentID: uuid.Must(uuid.NewV7()), DepartmentRevisionID: uuid.Must(uuid.NewV7()), StartsAt: checked.Add(-time.Hour), EligibilityUntil: checked.Add(time.Hour)}
		}
		nodes := map[uuid.UUID]postingChainSource{}
		for i := len(rows) - 1; i >= 0; i-- {
			if i == len(rows)-1 {
				rows[i].Intent = StaffPostingSupervisionIntent{Kind: "hospital_management"}
			} else {
				next := rows[i+1].Posting
				rows[i].Intent = StaffPostingSupervisionIntent{Kind: "department_supervisor", SupervisorStaffID: &next.StaffID, SupervisorProposalID: &next.ID, SupervisorRevisionID: &next.RevisionID}
			}
			nodes[rows[i].Posting.ID] = rows[i]
		}
		return rows[0].Posting, rows[0].Intent, nodes
	}
	for _, count := range []int{1, 2, 32, 33} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			row, intent, nodes := build(count)
			reads := 0
			plan, err := collectPostingSourcePlan(tenant, hospital, row, intent, func(scope postingDraftScope, _ uuid.UUID) (postingChainSource, error) {
				reads++
				return nodes[scope.draft], nil
			})
			if count <= 32 {
				require.NoError(t, err)
				require.Len(t, plan.Sources, count-1)
				require.Len(t, plan.References, count)
			} else {
				require.ErrorIs(t, err, errPostingProposalConflict)
			}
			require.LessOrEqual(t, reads, 31)
		})
	}
	for _, bad := range []string{"cycle", "wrong hospital", "wrong revision", "member supervisor"} {
		t.Run(bad, func(t *testing.T) {
			row, intent, nodes := build(2)
			parent := nodes[*intent.SupervisorProposalID]
			switch bad {
			case "cycle":
				intent.SupervisorStaffID = &row.StaffID
			case "wrong hospital":
				parent.Posting.HospitalID = uuid.New()
			case "wrong revision":
				parent.Posting.RevisionID = uuid.Must(uuid.NewV7())
			case "member supervisor":
				parent.Intent.Kind = "department_member"
			}
			_, err := collectPostingSourcePlan(tenant, hospital, row, intent, func(postingDraftScope, uuid.UUID) (postingChainSource, error) { return parent, nil })
			require.ErrorIs(t, err, errPostingProposalConflict)
		})
	}
}

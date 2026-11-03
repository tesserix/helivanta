package workforce

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestPostingCommandsRejectControlCharactersBeforePersistence(t *testing.T) {
	zero, one, examined := int64(0), int64(1), true
	id := uuid.Must(uuid.NewV7())
	for _, reason := range []string{"Synthetic\ninvalid reason", "Synthetic\tinvalid reason", "Synthetic\x00invalid reason", "Synthetic\u0085invalid reason", "संरचना बदलने का कारण"} {
		t.Run(reason, func(t *testing.T) {
			expected := reason == "संरचना बदलने का कारण"
			draft := StaffPostingDraftInput{ExpectedVersion: &zero, StaffVersion: 1, EmploymentEpoch: 1, DepartmentID: id, DepartmentRevisionID: id, StartsAt: time.Now().UTC(), Reason: reason}
			proposal := StaffPostingProposalInput{ExpectedVersion: &zero, DraftID: id, DraftRevisionID: id, DraftVersion: &one, GenerationID: id, StaffVersion: &one, EmploymentEpoch: &one, Reason: reason, SourcesExamined: &examined}
			decision := StaffPostingDecisionInput{ExpectedVersion: &one, ExpectedRevisionID: id, Decision: "reject", Reason: reason, SourcesExamined: &examined}
			require.Equal(t, expected, validPostingDraftInput(&draft), "draft validation must agree with its SQL reason constraint")
			require.Equal(t, expected, validPostingProposalInput(&proposal), "submission must refuse malformed protected reasons before writing")
			require.Equal(t, expected, validPostingDecisionInput(&decision), "decision must return 400 rather than fail a SQL constraint")
		})
	}
}

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

func TestPostingSupervisionIntentValidation(t *testing.T) {
	zero, one, examined := int64(0), int64(1), true
	proposal, revision, oldID := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.New()
	for _, tc := range []struct {
		name   string
		intent *StaffPostingSupervisionIntent
		valid  bool
	}{
		{"legacy omitted", nil, true},
		{"explicit root", &StaffPostingSupervisionIntent{Kind: "hospital_management"}, true},
		{"supervisor", &StaffPostingSupervisionIntent{Kind: "department_supervisor", SupervisorStaffID: &oldID, SupervisorProposalID: &proposal, SupervisorRevisionID: &revision}, true},
		{"member", &StaffPostingSupervisionIntent{Kind: "department_member", SupervisorStaffID: &oldID, SupervisorProposalID: &proposal, SupervisorRevisionID: &revision}, true},
		{"missing member supervisor", &StaffPostingSupervisionIntent{Kind: "department_member"}, false},
		{"partial supervisor", &StaffPostingSupervisionIntent{Kind: "department_supervisor", SupervisorStaffID: &oldID, SupervisorProposalID: &proposal}, false},
		{"partial revision", &StaffPostingSupervisionIntent{Kind: "department_member", SupervisorStaffID: &oldID, SupervisorRevisionID: &revision}, false},
		{"root with supervisor", &StaffPostingSupervisionIntent{Kind: "hospital_management", SupervisorProposalID: &proposal, SupervisorRevisionID: &revision}, false},
		{"wrong proposal version", &StaffPostingSupervisionIntent{Kind: "department_member", SupervisorStaffID: &oldID, SupervisorProposalID: &oldID, SupervisorRevisionID: &revision}, false},
		{"wrong revision version", &StaffPostingSupervisionIntent{Kind: "department_member", SupervisorStaffID: &oldID, SupervisorProposalID: &proposal, SupervisorRevisionID: &oldID}, false},
		{"unknown kind", &StaffPostingSupervisionIntent{Kind: "clinical"}, false},
		{"empty kind", &StaffPostingSupervisionIntent{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := StaffPostingProposalInput{ExpectedVersion: &zero, DraftID: proposal, DraftRevisionID: revision, DraftVersion: &one, GenerationID: proposal, StaffVersion: &one, EmploymentEpoch: &one, Reason: "Synthetic reviewed supervision intent", SourcesExamined: &examined, Supervision: tc.intent}
			require.Equal(t, tc.valid, validPostingProposalInput(&in))
		})
	}
}

func TestPostingManagementAttestationIsOnlyForApproval(t *testing.T) {
	one, examined := int64(1), true
	for _, action := range []string{"approve", "reject", "withdraw"} {
		for _, tc := range []struct {
			name     string
			reviewed *bool
		}{{"omitted", nil}, {"false", new(false)}, {"true", new(true)}} {
			t.Run(action+"/"+tc.name, func(t *testing.T) {
				in := StaffPostingDecisionInput{ExpectedVersion: &one, ExpectedRevisionID: uuid.Must(uuid.NewV7()), Decision: action, Reason: "Synthetic hospital management review", SourcesExamined: &examined, HospitalManagementReviewed: tc.reviewed}
				valid := tc.reviewed == nil || !*tc.reviewed || action == "approve"
				require.Equal(t, valid, validPostingDecisionInput(&in))
				if tc.reviewed != nil && !*tc.reviewed {
					require.Nil(t, in.HospitalManagementReviewed, "false and omitted have the same normalized command intent")
				}
			})
		}
	}
}

func TestPostingSupervisionRequiresCanonicalStaffReference(t *testing.T) {
	zero, one, examined := int64(0), int64(1), true
	proposal, revision, staff, nilID := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.New(), uuid.Nil
	for _, tc := range []struct {
		name   string
		intent StaffPostingSupervisionIntent
		valid  bool
	}{
		{"missing Staff", StaffPostingSupervisionIntent{Kind: "department_member", SupervisorProposalID: &proposal, SupervisorRevisionID: &revision}, false},
		{"legacy Staff format", StaffPostingSupervisionIntent{Kind: "department_member", SupervisorStaffID: &staff, SupervisorProposalID: &proposal, SupervisorRevisionID: &revision}, true},
		{"nil Staff", StaffPostingSupervisionIntent{Kind: "department_member", SupervisorStaffID: &nilID, SupervisorProposalID: &proposal, SupervisorRevisionID: &revision}, false},
		{"root cannot retain Staff", StaffPostingSupervisionIntent{Kind: "hospital_management", SupervisorStaffID: &staff}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := StaffPostingProposalInput{ExpectedVersion: &zero, DraftID: proposal, DraftRevisionID: revision, DraftVersion: &one, GenerationID: proposal, StaffVersion: &one, EmploymentEpoch: &one, Reason: "Synthetic canonical supervisor reference", SourcesExamined: &examined, Supervision: &tc.intent}
			require.Equal(t, tc.valid, validPostingProposalInput(&in))
		})
	}
}

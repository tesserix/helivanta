package workforce

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

// UnmarshalJSON distinguishes an omitted legacy intent from an explicitly null intent.
func (in *StaffPostingProposalInput) UnmarshalJSON(data []byte) error {
	type input StaffPostingProposalInput
	var decoded input
	if err := decodePostingNonNullObject(data, &decoded, "supervision"); err != nil {
		return err
	}
	*in = StaffPostingProposalInput(decoded)
	return nil
}

// UnmarshalJSON preserves the non-null supervisor reference contract.
func (in *StaffPostingSupervisionIntent) UnmarshalJSON(data []byte) error {
	type input StaffPostingSupervisionIntent
	var decoded input
	if err := decodePostingNonNullObject(data, &decoded, "kind", "supervisor_staff_id", "supervisor_proposal_id", "supervisor_revision_id"); err != nil {
		return err
	}
	*in = StaffPostingSupervisionIntent(decoded)
	return nil
}

// UnmarshalJSON refuses an explicitly null management attestation.
func (in *StaffPostingDecisionInput) UnmarshalJSON(data []byte) error {
	type input StaffPostingDecisionInput
	var decoded input
	if err := decodePostingNonNullObject(data, &decoded, "hospital_management_reviewed"); err != nil {
		return err
	}
	*in = StaffPostingDecisionInput(decoded)
	return nil
}

func decodePostingNonNullObject(data []byte, into any, fields ...string) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw == nil {
		return errors.New("posting input must be an object")
	}
	for _, field := range fields {
		if bytes.Equal(bytes.TrimSpace(raw[field]), []byte("null")) {
			return errors.New("posting intent fields cannot be null")
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(into)
}

func validPostingSupervisionIntent(in *StaffPostingSupervisionIntent) bool {
	if in == nil {
		return true
	}
	switch in.Kind {
	case "hospital_management":
		return in.SupervisorStaffID == nil && in.SupervisorProposalID == nil && in.SupervisorRevisionID == nil
	case "department_supervisor", "department_member":
		return in.SupervisorStaffID != nil && *in.SupervisorStaffID != uuid.Nil &&
			in.SupervisorProposalID != nil && in.SupervisorRevisionID != nil &&
			postingV7(*in.SupervisorProposalID) && postingV7(*in.SupervisorRevisionID)
	default:
		return false
	}
}

package workforce

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

type bindingPreparationOperation string

const (
	bindingPreparationList     bindingPreparationOperation = "list"
	bindingPreparationRead     bindingPreparationOperation = "read"
	bindingPreparationHistory  bindingPreparationOperation = "history"
	bindingPreparationAttach   bindingPreparationOperation = "attach"
	bindingPreparationWithdraw bindingPreparationOperation = "withdraw"
	bindingPreparationDownload bindingPreparationOperation = "download"
)

type employmentBindingSelection struct {
	Staff, Period, Binding                       uuid.UUID
	PeriodVersion, StaffVersion, EmploymentEpoch int64
	Operation                                    bindingPreparationOperation
}
type employmentBindingCommand struct {
	ID, Document    uuid.UUID
	ExpectedVersion int64
	Digest          string
}

func employmentBindingProof(p authn.Principal, hospital uuid.UUID, s employmentBindingSelection, command *employmentBindingCommand, source authz.GovernanceSourceEvidence) (map[string]any, error) {
	safe := func(v int64) bool { return v >= 1 && v <= 9007199254740990 }
	if s.Staff == uuid.Nil || s.Period.Version() != 7 || !safe(s.PeriodVersion) || !safe(s.StaffVersion) || !safe(s.EmploymentEpoch) {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	switch s.Operation {
	case bindingPreparationList:
		if s.Binding != uuid.Nil || command != nil {
			return nil, authz.ErrScopedAuthorizationUnavailable
		}
	case bindingPreparationRead, bindingPreparationHistory, bindingPreparationDownload:
		if s.Binding.Version() != 7 || command != nil {
			return nil, authz.ErrScopedAuthorizationUnavailable
		}
	case bindingPreparationAttach, bindingPreparationWithdraw:
		if s.Binding.Version() != 7 || command == nil || command.ID.Version() != 7 || !documentDigest.MatchString(command.Digest) {
			return nil, authz.ErrScopedAuthorizationUnavailable
		}
		if s.Operation == bindingPreparationAttach && (command.Document == uuid.Nil || command.ExpectedVersion != 0) || s.Operation == bindingPreparationWithdraw && (command.Document != uuid.Nil || command.ExpectedVersion != 1) {
			return nil, authz.ErrScopedAuthorizationUnavailable
		}
	default:
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	proof, err := employmentDiscoveryProof(p, hospital, PermEmploymentPrepare, source)
	if err != nil {
		return nil, err
	}
	proof["purpose"], proof["operation"], proof["staff_id"], proof["period_id"], proof["period_version"], proof["staff_version"], proof["employment_epoch"] = "employment_binding_preparation", s.Operation, s.Staff, s.Period, s.PeriodVersion, s.StaffVersion, s.EmploymentEpoch
	if s.Binding != uuid.Nil {
		proof["binding_id"] = s.Binding
	}
	if command != nil {
		proof["command_id"], proof["input_hash"], proof["expected_version"] = command.ID, command.Digest, command.ExpectedVersion
		if command.Document != uuid.Nil {
			proof["file_id"] = command.Document
		}
	}
	return proof, nil
}
func sameEmploymentBindingSource(original, current map[string]any) bool {
	if !sameEmploymentPreparationSource(original, current) {
		return false
	}
	for _, key := range []string{"binding_id", "period_version", "file_id"} {
		a, aErr := json.Marshal(original[key])
		b, bErr := json.Marshal(current[key])
		if aErr != nil || bErr != nil || !bytes.Equal(a, b) {
			return false
		}
	}
	return true
}

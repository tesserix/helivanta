package workforce

import (
	"errors"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"maps"
)

var errEmploymentDocumentBasis = errors.New("employment document source changed")

type documentPreparationOperation string

const (
	documentPreparationMetadata documentPreparationOperation = "metadata"
	documentPreparationList     documentPreparationOperation = "list"
	documentPreparationRegister documentPreparationOperation = "register"
	documentPreparationUpload   documentPreparationOperation = "upload"
	documentPreparationDownload documentPreparationOperation = "download"
	documentPreparationRevoke   documentPreparationOperation = "revoke"
	documentPreparationRetry    documentPreparationOperation = "retry"
)

func employmentDocumentProof(p authn.Principal, hospital, staff, file uuid.UUID, version, epoch int64, operation documentPreparationOperation, source authz.GovernanceSourceEvidence) (map[string]any, error) {
	if staff == uuid.Nil || version < 1 || version > 9007199254740990 || epoch < 1 || epoch > 9007199254740990 {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	switch operation {
	case documentPreparationList:
		if file != uuid.Nil {
			return nil, authz.ErrScopedAuthorizationUnavailable
		}
	case documentPreparationMetadata, documentPreparationRegister, documentPreparationUpload, documentPreparationDownload, documentPreparationRevoke, documentPreparationRetry:
		if file == uuid.Nil {
			return nil, authz.ErrScopedAuthorizationUnavailable
		}
	default:
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	proof, err := employmentDiscoveryProof(p, hospital, PermEmploymentPrepare, source)
	if err != nil {
		return nil, err
	}
	proof["phase"] = int64(1)
	proof["purpose"], proof["staff_id"], proof["staff_version"], proof["employment_epoch"], proof["operation"], proof["stage"] = "employment_document_preparation", staff, version, epoch, operation, "read"
	if file != uuid.Nil {
		proof["file_id"] = file
	}
	return proof, nil
}

func employmentDocumentMutationProof(proof map[string]any, mutation documentMutation, digest string) (map[string]any, error) {
	staff, staffOK := proof["staff_id"].(uuid.UUID)
	file, fileOK := proof["file_id"].(uuid.UUID)
	hospital, hospitalOK := proof["hospital_id"].(uuid.UUID)
	version, versionOK := proof["staff_version"].(int64)
	if !staffOK || !fileOK || !hospitalOK || !versionOK || proof["stage"] != "read" || !documentDigest.MatchString(digest) {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	operation, ok := proof["operation"].(documentPreparationOperation)
	if !ok {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	phase, phaseOK := proof["phase"].(int64)
	expectedPhase := map[documentMutationStage]int64{documentMutationIntent: 1, documentMutationStamp: 2, documentMutationQueue: 3, documentMutationRevoke: 1, documentMutationRetry: 1}[mutation.Stage]
	if !phaseOK || expectedPhase == 0 || phase != expectedPhase {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	result := maps.Clone(proof)
	result["stage"], result["intent_hash"] = string(mutation.Stage), digest
	if mutation.Stage == documentMutationIntent {
		if operation != documentPreparationRegister || mutation.Input == nil || mutation.Saved != nil {
			return nil, authz.ErrScopedAuthorizationUnavailable
		}
		normalized := *mutation.Input
		if normalized.RecordKind != "enrollment" || normalized.Category != "employment_evidence" || !normalized.valid(staff) || normalized.Filename != mutation.Input.Filename {
			return nil, authz.ErrScopedAuthorizationUnavailable
		}
		result["scan_attempt"] = int64(1)
		return result, nil
	}
	row := mutation.Saved
	if mutation.Input != nil || row == nil || row.ID != file || row.HospitalID != hospital || row.StaffID != staff || row.RecordID != staff || row.RecordKind != "enrollment" || row.Category != "employment_evidence" || row.RecordVersion != version || row.ScanAttempt < 1 || row.ScanAttempt >= 9007199254740990 {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	result["scan_attempt"] = row.ScanAttempt
	switch mutation.Stage {
	case documentMutationStamp, documentMutationQueue:
		if operation != documentPreparationUpload || row.Status != "upload_pending" || row.ScanAttempt != 1 || row.SourceGeneration != nil || row.ReleasedGeneration != nil {
			return nil, authz.ErrScopedAuthorizationUnavailable
		}
		if mutation.Stage == documentMutationQueue {
			if mutation.Generation < 1 {
				return nil, authz.ErrScopedAuthorizationUnavailable
			}
			result["source_generation"] = mutation.Generation
		}
	case documentMutationRevoke:
		if operation != documentPreparationRevoke || row.Status != "available" || row.SourceGeneration == nil || *row.SourceGeneration < 1 || row.ReleasedGeneration == nil || *row.ReleasedGeneration < 1 {
			return nil, authz.ErrScopedAuthorizationUnavailable
		}
		result["source_generation"], result["released_generation"] = *row.SourceGeneration, *row.ReleasedGeneration
	case documentMutationRetry:
		if operation != documentPreparationRetry || !contains(row.Status, "scan_error", "scan_pending") || row.SourceGeneration == nil || *row.SourceGeneration < 1 || row.ReleasedGeneration != nil || mutation.Attempt != row.ScanAttempt+1 {
			return nil, authz.ErrScopedAuthorizationUnavailable
		}
		result["source_generation"], result["scan_attempt"] = *row.SourceGeneration, mutation.Attempt
	default:
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	return result, nil
}

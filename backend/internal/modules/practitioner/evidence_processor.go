package practitioner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	practitionercontract "github.com/tesserix/helivanta/internal/modules/practitioner/contract"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

const evidenceWorkerActor = "service:practitioner-evidence-worker"

var ErrEvidenceNotReady = errors.New("professional evidence is not ready for processing")
var ErrEvidenceIntegrity = errors.New("professional evidence integrity mismatch")

// EvidenceProcessor owns exact-generation professional credential file processing.
type EvidenceProcessor struct {
	db      *tenantdb.DB
	files   *attachments.GCS
	scanner *attachments.ClamAV
}

func NewEvidenceProcessor(ctx context.Context, db *tenantdb.DB, files *attachments.GCS, scanner *attachments.ClamAV) (*EvidenceProcessor, error) {
	if db == nil || files == nil || scanner == nil {
		return nil, errors.New("professional evidence processing dependencies unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var allowed bool
	err := db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Raw("SELECT current_user='helivanta_practitioner_evidence_worker' AND NOT rolsuper AND NOT rolbypassrls AND NOT rolcreaterole AND NOT rolcreatedb AND NOT rolinherit AND NOT EXISTS(SELECT 1 FROM pg_auth_members m WHERE m.member=pg_roles.oid) FROM pg_roles WHERE rolname=current_user").Scan(&allowed).Error
	})
	if err != nil || !allowed {
		return nil, errors.New("professional evidence database identity is not authorized")
	}
	return &EvidenceProcessor{db, files, scanner}, nil
}
func validEvidenceScanInput(in practitionercontract.EvidenceScanInput) bool {
	if in.ScanAttempt < 1 || in.ScanAttempt > 9007199254740991 {
		return false
	}
	_, credential, valid := scanCredential(in)
	if !valid {
		return false
	}
	for i, value := range []string{in.TenantID, in.PractitionerID, credential, in.RevisionID, in.FileID} {
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil || id.String() != value || (i > 0 && (id.Variant() != uuid.RFC4122 || id.Version() != 7)) {
			return false
		}
	}
	return true
}
func (p *EvidenceProcessor) transaction(ctx context.Context, in practitionercontract.EvidenceScanInput, fn func(*gorm.DB) error) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	kind, credential, _ := scanCredential(in)
	return p.db.WithTenant(tenantdb.WithActor(ctx, evidenceWorkerActor), in.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT set_config('app.practitioner_evidence_owner',?,true),set_config('app.practitioner_evidence_credential',?,true),set_config('app.practitioner_evidence_kind',?,true),set_config('app.practitioner_evidence_revision',?,true),set_config('app.practitioner_evidence_file',?,true),set_config('lock_timeout','2s',true),set_config('statement_timeout','5s',true),set_config('app.practitioner_evidence_attempt',?,true)", in.PractitionerID, credential, kind, in.RevisionID, in.FileID, fmt.Sprint(in.ScanAttempt)).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}
func processingEvidence(tx *gorm.DB, in practitionercontract.EvidenceScanInput, row *credentialEvidence) error {
	kind, credential, _ := scanCredential(in)
	return lockedEvidence(tx, authn.Principal{TenantID: in.TenantID, Subject: evidenceWorkerActor}, evidenceParent{uuid.MustParse(in.PractitionerID), uuid.MustParse(credential), uuid.MustParse(in.RevisionID), kind}, uuid.MustParse(in.FileID), row)
}
func evidenceStateEvent(tx *gorm.DB, in practitionercontract.EvidenceScanInput, state string) error {
	data, err := json.Marshal(practitionercontract.EvidenceChangedData{PractitionerID: in.PractitionerID, RegistrationID: in.RegistrationID, QualificationID: in.QualificationID, RevisionID: in.RevisionID, FileID: in.FileID, ScanAttempt: in.ScanAttempt, Status: state})
	if err != nil {
		return err
	}
	return events.Enqueue(tx, practitionercontract.EvidenceChanged, events.Event{Type: practitionercontract.EvidenceChanged, Version: 1, TenantID: in.TenantID, Data: data})
}
func processableEvidence(state string) bool { return state == "scan_pending" || state == "scan_error" }

// Process returns only a bounded lifecycle outcome; scan success confers no professional authority.
func (p *EvidenceProcessor) Process(ctx context.Context, in practitionercontract.EvidenceScanInput) (string, error) {
	if !validEvidenceScanInput(in) {
		return "", ErrEvidenceNotReady
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	var row credentialEvidence
	err := p.transaction(ctx, in, func(tx *gorm.DB) error {
		if err := processingEvidence(tx, in, &row); err != nil {
			return err
		}
		return evidenceAudit(tx, authn.Principal{TenantID: in.TenantID, Subject: evidenceWorkerActor}, row.ID)
	})
	if err != nil {
		return "", fmt.Errorf("load professional evidence: %w", err)
	}
	if row.ScanAttempt != in.ScanAttempt {
		return "superseded", nil
	}
	if row.Status == "available" || row.Status == "rejected" || row.Status == "revoked" {
		return row.Status, nil
	}
	if !processableEvidence(row.Status) || row.UploadedAt == nil || row.SourceGeneration == nil {
		return "", ErrEvidenceNotReady
	}
	scope := attachments.ObjectScope{TenantID: uuid.MustParse(in.TenantID), OwnerKind: "practitioner", OwnerID: row.PractitionerID, RecordID: row.RevisionID, FileID: row.ID, Version: 1, UploadedAt: *row.UploadedAt}
	scan, err := p.files.ScanQuarantine(ctx, scope, *row.SourceGeneration, row.ContentType, p.scanner)
	if err != nil {
		switch {
		case errors.Is(err, attachments.ErrInfected):
			return p.failure(ctx, in, "rejected", "malware_detected", nil)
		case errors.Is(err, attachments.ErrUnsupportedContent):
			return p.failure(ctx, in, "rejected", "unsupported_content", nil)
		case errors.Is(err, attachments.ErrStorageUnavailable):
			return p.failure(ctx, in, "scan_error", "storage_unavailable", err)
		default:
			return p.failure(ctx, in, "scan_error", "scanner_unavailable", err)
		}
	}
	if !scan.Matches(*row.SourceGeneration, row.Size, row.SHA256) {
		return p.failure(ctx, in, "rejected", "integrity_mismatch", nil)
	}
	release, err := p.files.Promote(ctx, scan)
	if err != nil {
		return p.failure(ctx, in, "scan_error", "storage_unavailable", err)
	}
	status := ""
	err = p.transaction(ctx, in, func(tx *gorm.DB) error {
		var current credentialEvidence
		if err := processingEvidence(tx, in, &current); err != nil {
			return err
		}
		if current.ScanAttempt != in.ScanAttempt {
			status = "superseded"
			return nil
		}
		if !processableEvidence(current.Status) {
			status = current.Status
			return nil
		}
		if current.SourceGeneration == nil || *current.SourceGeneration != release.SourceGeneration || current.Size != release.Object.Size || current.SHA256 != release.Object.SHA256 || current.ContentType != release.ContentType {
			return ErrEvidenceIntegrity
		}
		result := tx.Exec("UPDATE practitioner_credential_evidence SET status='available',released_generation=?,engine_version=?,definition_version=?,definitions_updated_at=?,scanned_at=?,failure_code=NULL WHERE tenant_id=? AND id=? AND status IN ('scan_pending','scan_error') AND scan_attempt=? AND source_generation=?", release.Object.Generation, release.Scanner.EngineVersion, release.Scanner.DefinitionVersion, release.Scanner.DefinitionsUpdatedAt, release.ScannedAt, in.TenantID, in.FileID, in.ScanAttempt, release.SourceGeneration)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrEvidenceIntegrity
		}
		if err := evidenceStateEvent(tx, in, "available"); err != nil {
			return err
		}
		status = "available"
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("commit professional evidence release: %w", err)
	}
	return status, nil
}
func (p *EvidenceProcessor) failure(ctx context.Context, in practitionercontract.EvidenceScanInput, state, code string, cause error) (string, error) {
	status := ""
	err := p.transaction(ctx, in, func(tx *gorm.DB) error {
		var current credentialEvidence
		if err := processingEvidence(tx, in, &current); err != nil {
			return err
		}
		status = current.Status
		if current.ScanAttempt != in.ScanAttempt {
			status = "superseded"
			return nil
		}
		if !processableEvidence(current.Status) {
			return nil
		}
		if current.Status == state && current.FailureCode != nil && *current.FailureCode == code {
			return nil
		}
		result := tx.Exec("UPDATE practitioner_credential_evidence SET status=?,failure_code=? WHERE tenant_id=? AND id=? AND status IN ('scan_pending','scan_error') AND scan_attempt=?", state, code, in.TenantID, in.FileID, in.ScanAttempt)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrEvidenceIntegrity
		}
		if err := evidenceStateEvent(tx, in, state); err != nil {
			return err
		}
		status = state
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("commit professional evidence failure: %w", err)
	}
	if cause != nil && status == "scan_error" {
		return status, fmt.Errorf("professional evidence processing unavailable: %w", cause)
	}
	return status, nil
}

package supply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract" //nolint:depguard // own module and data-only contract
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const evidenceWorkerActor = "service:supply-evidence-worker"
const workerEvidenceColumns = "tenant_id,id,party_id,license_id,license_version,content_type,size,sha256,status,scan_attempt,uploaded_at,source_generation,released_generation,engine_version,definition_version,definitions_updated_at,scanned_at,failure_code,expires_at,created_at,updated_at"

var ErrEvidenceIntegrity = errors.New("document integrity mismatch")
var ErrEvidenceNotReady = errors.New("document is not ready for scanning")

// EvidenceProcessor owns Supply licence-evidence scanning and matching release commits.
type EvidenceProcessor struct {
	db      *tenantdb.DB
	files   *attachments.GCS
	scanner *attachments.ClamAV
}

func NewEvidenceProcessor(ctx context.Context, db *tenantdb.DB, files *attachments.GCS, scanner *attachments.ClamAV) (*EvidenceProcessor, error) {
	if db == nil || files == nil || scanner == nil {
		return nil, errors.New("document processing dependencies are unavailable")
	}
	var authorized bool
	err := db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT current_user='helivanta_supply_evidence_worker' AND NOT rolsuper AND NOT rolbypassrls AND NOT rolcreaterole AND NOT rolcreatedb AND NOT rolinherit AND NOT EXISTS(SELECT 1 FROM pg_auth_members m WHERE m.member=pg_roles.oid) FROM pg_roles WHERE rolname=current_user`).Scan(&authorized).Error
	})
	if err != nil || !authorized {
		return nil, errors.New("document processor database identity is not authorized")
	}
	return &EvidenceProcessor{db: db, files: files, scanner: scanner}, nil
}

func validEvidenceScanInput(in supplycontract.EvidenceScanInput) bool {
	if in.ScanAttempt < 1 || in.ScanAttempt > 9007199254740991 {
		return false
	}
	for _, value := range []string{in.TenantID, in.PartyID, in.LicenseID, in.FileID} {
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil || id.String() != value {
			return false
		}
	}
	return true
}

func (p *EvidenceProcessor) transaction(ctx context.Context, in supplycontract.EvidenceScanInput, fn func(*gorm.DB) error) error {
	return p.db.WithTenant(tenantdb.WithActor(ctx, evidenceWorkerActor), in.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.supply_party',?,true),set_config('app.supply_license',?,true),set_config('app.supply_file',?,true)`, in.PartyID, in.LicenseID, in.FileID).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}

func workerEvidence(tx *gorm.DB, in supplycontract.EvidenceScanInput) (evidence, error) {
	var saved evidence
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select(workerEvidenceColumns).Where("tenant_id=? AND party_id=? AND license_id=? AND id=?", in.TenantID, in.PartyID, in.LicenseID, in.FileID).First(&saved).Error
	return saved, err
}

// Process returns only a safe lifecycle status; content and protected facts never enter history.
func (p *EvidenceProcessor) Process(ctx context.Context, in supplycontract.EvidenceScanInput) (string, error) {
	if !validEvidenceScanInput(in) {
		return "", ErrEvidenceNotReady
	}
	var saved evidence
	err := p.transaction(ctx, in, func(tx *gorm.DB) error {
		var err error
		saved, err = workerEvidence(tx, in)
		return err
	})
	if err != nil {
		return "", fmt.Errorf("load scoped document: %w", err)
	}
	if saved.ScanAttempt != in.ScanAttempt {
		return "superseded", nil
	}
	if saved.Status == "available" || saved.Status == "revoked" || saved.Status == "rejected" {
		return saved.Status, nil
	}
	if saved.Status != "scan_pending" && saved.Status != "scan_error" {
		return "", ErrEvidenceNotReady
	}
	if saved.SourceGeneration == nil || saved.UploadedAt == nil {
		return "", ErrEvidenceNotReady
	}
	scope := attachments.ObjectScope{TenantID: uuid.MustParse(in.TenantID), OwnerKind: "organisation", OwnerID: saved.PartyID, RecordID: saved.LicenseID, FileID: saved.ID, Version: 1, UploadedAt: *saved.UploadedAt}
	scan, err := p.files.ScanQuarantine(ctx, scope, *saved.SourceGeneration, saved.ContentType, p.scanner)
	if err != nil {
		if errors.Is(err, attachments.ErrInfected) {
			return p.failure(ctx, in, "rejected", "malware_detected")
		}
		if errors.Is(err, attachments.ErrUnsupportedContent) {
			return p.failure(ctx, in, "rejected", "unsupported_content")
		}
		code := "scanner_unavailable"
		if errors.Is(err, attachments.ErrStorageUnavailable) {
			code = "storage_unavailable"
		}
		return p.retryableFailure(ctx, in, code, err)
	}
	if !scan.Matches(*saved.SourceGeneration, saved.Size, saved.SHA256) {
		return p.failure(ctx, in, "rejected", "integrity_mismatch")
	}
	release, err := p.files.Promote(ctx, scan)
	if err != nil {
		return p.retryableFailure(ctx, in, "storage_unavailable", err)
	}
	status := ""
	err = p.transaction(ctx, in, func(tx *gorm.DB) error {
		current, err := workerEvidence(tx, in)
		if err != nil {
			return err
		}
		if current.ScanAttempt != in.ScanAttempt {
			status = "superseded"
			return nil
		}
		if current.Status != "scan_pending" && current.Status != "scan_error" {
			status = current.Status
			return nil
		}
		if current.SourceGeneration == nil || *current.SourceGeneration != release.SourceGeneration || current.Size != release.Object.Size || current.SHA256 != release.Object.SHA256 || current.ContentType != release.ContentType {
			return ErrEvidenceIntegrity
		}
		if err := resetEvidenceError(tx, in, current); err != nil {
			return err
		}
		result := tx.Exec(`UPDATE supply_license_evidence SET status='available',released_generation=?,engine_version=?,definition_version=?,definitions_updated_at=?,scanned_at=?,failure_code=NULL,updated_at=clock_timestamp()
		 WHERE tenant_id=? AND party_id=? AND license_id=? AND id=? AND status='scan_pending' AND scan_attempt=? AND source_generation=?`, release.Object.Generation, release.Scanner.EngineVersion, release.Scanner.DefinitionVersion, release.Scanner.DefinitionsUpdatedAt, release.ScannedAt, in.TenantID, in.PartyID, in.LicenseID, in.FileID, in.ScanAttempt, release.SourceGeneration)
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
		return "", fmt.Errorf("commit document release: %w", err)
	}
	return status, nil
}

func resetEvidenceError(tx *gorm.DB, in supplycontract.EvidenceScanInput, current evidence) error {
	if current.Status != "scan_error" {
		return nil
	}
	return tx.Exec(`UPDATE supply_license_evidence SET status='scan_pending',updated_at=clock_timestamp() WHERE tenant_id=? AND party_id=? AND license_id=? AND id=? AND status='scan_error' AND scan_attempt=?`, in.TenantID, in.PartyID, in.LicenseID, in.FileID, in.ScanAttempt).Error
}

func (p *EvidenceProcessor) retryableFailure(ctx context.Context, in supplycontract.EvidenceScanInput, code string, cause error) (string, error) {
	status, err := p.failure(ctx, in, "scan_error", code)
	if err != nil {
		return "", err
	}
	if status != "scan_error" {
		return status, nil
	}
	return status, fmt.Errorf("document processing temporarily unavailable: %w", cause)
}

func (p *EvidenceProcessor) failure(ctx context.Context, in supplycontract.EvidenceScanInput, state, code string) (string, error) {
	status := ""
	err := p.transaction(ctx, in, func(tx *gorm.DB) error {
		current, err := workerEvidence(tx, in)
		if err != nil {
			return err
		}
		if current.ScanAttempt != in.ScanAttempt {
			status = "superseded"
			return nil
		}
		if current.Status != "scan_pending" && current.Status != "scan_error" {
			status = current.Status
			return nil
		}
		if current.Status == state && current.FailureCode != nil && *current.FailureCode == code {
			status = state
			return nil
		}
		if err := resetEvidenceError(tx, in, current); err != nil {
			return err
		}
		result := tx.Exec(`UPDATE supply_license_evidence SET status=?,failure_code=?,updated_at=clock_timestamp() WHERE tenant_id=? AND party_id=? AND license_id=? AND id=? AND status='scan_pending' AND scan_attempt=?`, state, code, in.TenantID, in.PartyID, in.LicenseID, in.FileID, in.ScanAttempt)
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
		return "", fmt.Errorf("commit document processing failure: %w", err)
	}
	return status, nil
}

func evidenceStateEvent(tx *gorm.DB, in supplycontract.EvidenceScanInput, status string) error {
	data, err := json.Marshal(supplycontract.EvidenceChangedData{EvidenceScanRequestedData: supplycontract.EvidenceScanRequestedData{PartyID: in.PartyID, LicenseID: in.LicenseID, FileID: in.FileID, ScanAttempt: in.ScanAttempt}, Status: status})
	if err != nil {
		return err
	}
	return events.Enqueue(tx, supplycontract.SubjectEvidenceChanged, events.Event{Type: supplycontract.SubjectEvidenceChanged, Version: 1, TenantID: in.TenantID, Data: data})
}

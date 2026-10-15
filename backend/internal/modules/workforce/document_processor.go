package workforce

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const documentWorkerActor = "service:workforce-document-worker"
const workerDocumentColumns = "tenant_id,id,hospital_id,staff_id,record_id,record_kind,record_version,content_type,size,sha256,status,scan_attempt,uploaded_at,source_generation,released_generation,engine_version,definition_version,definitions_updated_at,scanned_at,failure_code,expires_at,created_at,updated_at"

var ErrDocumentIntegrity = errors.New("document integrity mismatch")
var ErrDocumentNotReady = errors.New("document is not ready for scanning")

// DocumentProcessor owns workforce scanning and matching release commits.
type DocumentProcessor struct {
	db      *tenantdb.DB
	files   *attachments.GCS
	scanner *attachments.ClamAV
}

func NewDocumentProcessor(ctx context.Context, db *tenantdb.DB, files *attachments.GCS, scanner *attachments.ClamAV) (*DocumentProcessor, error) {
	if db == nil || files == nil || scanner == nil {
		return nil, errors.New("document processing dependencies are unavailable")
	}
	var authorized bool
	err := db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT current_user='helivanta_document_worker' AND NOT rolsuper AND NOT rolbypassrls AND NOT rolcreaterole AND NOT rolcreatedb FROM pg_roles WHERE rolname=current_user`).Scan(&authorized).Error
	})
	if err != nil || !authorized {
		return nil, errors.New("document processor database identity is not authorized")
	}
	return &DocumentProcessor{db: db, files: files, scanner: scanner}, nil
}

func validScanInput(in workforcecontract.DocumentScanInput) bool {
	if in.ScanAttempt < 1 {
		return false
	}
	for _, value := range []string{in.TenantID, in.HospitalID, in.StaffID, in.FileID} {
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil || id.String() != value {
			return false
		}
	}
	return true
}

func (p *DocumentProcessor) transaction(ctx context.Context, in workforcecontract.DocumentScanInput, fn func(*gorm.DB) error) error {
	return p.db.WithTenant(tenantdb.WithActor(ctx, documentWorkerActor), in.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_hospital',?,true),set_config('app.workforce_document_staff',?,true),set_config('app.workforce_document_file',?,true)`, in.HospitalID, in.StaffID, in.FileID).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}

func workerDocument(tx *gorm.DB, in workforcecontract.DocumentScanInput) (document, error) {
	var saved document
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select(workerDocumentColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", in.TenantID, in.HospitalID, in.StaffID, in.FileID).First(&saved).Error
	return saved, err
}

// Process returns only a safe lifecycle status; content and protected facts never enter history.
func (p *DocumentProcessor) Process(ctx context.Context, in workforcecontract.DocumentScanInput) (string, error) {
	if !validScanInput(in) {
		return "", ErrDocumentNotReady
	}
	var saved document
	err := p.transaction(ctx, in, func(tx *gorm.DB) error {
		var err error
		saved, err = workerDocument(tx, in)
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
		return "", ErrDocumentNotReady
	}
	if saved.SourceGeneration == nil || saved.UploadedAt == nil {
		return "", ErrDocumentNotReady
	}
	scope := attachments.ObjectScope{TenantID: uuid.MustParse(in.TenantID), HospitalID: saved.HospitalID, OwnerKind: "staff", OwnerID: saved.StaffID, RecordID: saved.RecordID, FileID: saved.ID, Version: 1, UploadedAt: *saved.UploadedAt}
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
		current, err := workerDocument(tx, in)
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
			return ErrDocumentIntegrity
		}
		if err := resetDocumentError(tx, in, current); err != nil {
			return err
		}
		result := tx.Exec(`UPDATE workforce_documents SET status='available',released_generation=?,engine_version=?,definition_version=?,definitions_updated_at=?,scanned_at=?,failure_code=NULL,updated_at=clock_timestamp()
		 WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND id=? AND status='scan_pending' AND scan_attempt=? AND source_generation=?`, release.Object.Generation, release.Scanner.EngineVersion, release.Scanner.DefinitionVersion, release.Scanner.DefinitionsUpdatedAt, release.ScannedAt, in.TenantID, in.HospitalID, in.StaffID, in.FileID, in.ScanAttempt, release.SourceGeneration)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrDocumentIntegrity
		}
		if err := documentStateEvent(tx, in, "available"); err != nil {
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

func resetDocumentError(tx *gorm.DB, in workforcecontract.DocumentScanInput, current document) error {
	if current.Status != "scan_error" {
		return nil
	}
	return tx.Exec(`UPDATE workforce_documents SET status='scan_pending',updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND id=? AND status='scan_error' AND scan_attempt=?`, in.TenantID, in.HospitalID, in.StaffID, in.FileID, in.ScanAttempt).Error
}

func (p *DocumentProcessor) retryableFailure(ctx context.Context, in workforcecontract.DocumentScanInput, code string, cause error) (string, error) {
	status, err := p.failure(ctx, in, "scan_error", code)
	if err != nil {
		return "", err
	}
	if status != "scan_error" {
		return status, nil
	}
	return status, fmt.Errorf("document processing temporarily unavailable: %w", cause)
}

func (p *DocumentProcessor) failure(ctx context.Context, in workforcecontract.DocumentScanInput, state, code string) (string, error) {
	status := ""
	err := p.transaction(ctx, in, func(tx *gorm.DB) error {
		current, err := workerDocument(tx, in)
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
		if err := resetDocumentError(tx, in, current); err != nil {
			return err
		}
		result := tx.Exec(`UPDATE workforce_documents SET status=?,failure_code=?,updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND id=? AND status='scan_pending' AND scan_attempt=?`, state, code, in.TenantID, in.HospitalID, in.StaffID, in.FileID, in.ScanAttempt)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrDocumentIntegrity
		}
		if err := documentStateEvent(tx, in, state); err != nil {
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

func documentStateEvent(tx *gorm.DB, in workforcecontract.DocumentScanInput, status string) error {
	data, err := json.Marshal(workforcecontract.DocumentChangedData{HospitalID: in.HospitalID, StaffID: in.StaffID, FileID: in.FileID, ScanAttempt: in.ScanAttempt, Status: status})
	if err != nil {
		return err
	}
	return events.Enqueue(tx, DocumentChanged, events.Event{Type: DocumentChanged, Version: 1, TenantID: in.TenantID, Data: data})
}

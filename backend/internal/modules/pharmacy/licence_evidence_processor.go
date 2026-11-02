package pharmacy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract" //nolint:depguard // own data-only contract
	"github.com/tesserix/helivanta/pkg/attachments"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const workerLicenceEvidenceColumns = "tenant_id,id,outlet_id,licence_id,content_type,size,sha256,status,scan_attempt,uploaded_at,source_generation,released_generation,engine_version,definition_version,definitions_updated_at,scanned_at,failure_code,expires_at,created_at,updated_at"

var ErrLicenceEvidenceIntegrity = errors.New("licence evidence integrity mismatch")
var ErrLicenceEvidenceNotReady = errors.New("licence evidence is not ready for scanning")

// LicenceEvidenceProcessor scans one licence file and commits the
// matching release, as the pharmacy evidence worker identity only.
type LicenceEvidenceProcessor struct {
	db      *tenantdb.DB
	files   *attachments.GCS
	scanner *attachments.ClamAV
}

func NewLicenceEvidenceProcessor(ctx context.Context, db *tenantdb.DB, files *attachments.GCS, scanner *attachments.ClamAV) (*LicenceEvidenceProcessor, error) {
	if db == nil || files == nil || scanner == nil {
		return nil, errors.New("licence evidence processing dependencies are unavailable")
	}
	var authorized bool
	err := db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT current_user='helivanta_pharmacy_evidence_worker' AND NOT rolsuper AND NOT rolbypassrls AND NOT rolcreaterole AND NOT rolcreatedb AND NOT rolinherit AND NOT EXISTS(SELECT 1 FROM pg_auth_members m WHERE m.member=pg_roles.oid) FROM pg_roles WHERE rolname=current_user`).Scan(&authorized).Error
	})
	if err != nil || !authorized {
		return nil, errors.New("licence evidence processor database identity is not authorized")
	}
	return &LicenceEvidenceProcessor{db: db, files: files, scanner: scanner}, nil
}

func validLicenceEvidenceScanInput(in pharmacycontract.LicenceEvidenceScanInput) bool {
	if in.ScanAttempt < 1 || in.ScanAttempt > 9007199254740991 {
		return false
	}
	for _, value := range []string{in.TenantID, in.OutletID, in.LicenceID, in.FileID} {
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil || id.String() != value {
			return false
		}
	}
	return true
}

func (p *LicenceEvidenceProcessor) transaction(ctx context.Context, in pharmacycontract.LicenceEvidenceScanInput, fn func(*gorm.DB) error) error {
	return p.db.WithTenant(tenantdb.WithActor(ctx, evidenceWorkerActor), in.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_licence',?,true),set_config('app.pharmacy_file',?,true)`, in.OutletID, in.LicenceID, in.FileID).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}

func workerLicenceEvidence(tx *gorm.DB, in pharmacycontract.LicenceEvidenceScanInput) (licenceEvidence, error) {
	var saved licenceEvidence
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select(workerLicenceEvidenceColumns).Where("tenant_id=? AND outlet_id=? AND licence_id=? AND id=?", in.TenantID, in.OutletID, in.LicenceID, in.FileID).First(&saved).Error
	return saved, err
}

// Process returns only a safe lifecycle status; content and protected facts never enter history.
func (p *LicenceEvidenceProcessor) Process(ctx context.Context, in pharmacycontract.LicenceEvidenceScanInput) (string, error) {
	if !validLicenceEvidenceScanInput(in) {
		return "", ErrLicenceEvidenceNotReady
	}
	var saved licenceEvidence
	if err := p.transaction(ctx, in, func(tx *gorm.DB) (err error) {
		saved, err = workerLicenceEvidence(tx, in)
		return err
	}); err != nil {
		return "", fmt.Errorf("load scoped licence evidence: %w", err)
	}
	if saved.ScanAttempt != in.ScanAttempt {
		return "superseded", nil
	}
	if saved.Status == "available" || saved.Status == "rejected" {
		return saved.Status, nil
	}
	if saved.Status != "scan_pending" && saved.Status != "scan_error" {
		return "", ErrLicenceEvidenceNotReady
	}
	if saved.SourceGeneration == nil || saved.UploadedAt == nil {
		return "", ErrLicenceEvidenceNotReady
	}
	saved.TenantID = uuid.MustParse(in.TenantID)
	scan, err := p.files.ScanQuarantine(ctx, saved.objectScope(), *saved.SourceGeneration, saved.ContentType, p.scanner)
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
		current, err := workerLicenceEvidence(tx, in)
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
			return ErrLicenceEvidenceIntegrity
		}
		if err := resetLicenceEvidenceError(tx, in, current); err != nil {
			return err
		}
		result := tx.Exec(`UPDATE pharmacy_licence_evidence SET status='available',released_generation=?,engine_version=?,definition_version=?,definitions_updated_at=?,scanned_at=?,failure_code=NULL,updated_at=clock_timestamp()
		 WHERE tenant_id=? AND outlet_id=? AND licence_id=? AND id=? AND status='scan_pending' AND scan_attempt=? AND source_generation=?`, release.Object.Generation, release.Scanner.EngineVersion, release.Scanner.DefinitionVersion, release.Scanner.DefinitionsUpdatedAt, release.ScannedAt, in.TenantID, in.OutletID, in.LicenceID, in.FileID, in.ScanAttempt, release.SourceGeneration)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrLicenceEvidenceIntegrity
		}
		if err := licenceEvidenceStateEvent(tx, in, "available"); err != nil {
			return err
		}
		status = "available"
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("commit licence evidence release: %w", err)
	}
	return status, nil
}

func resetLicenceEvidenceError(tx *gorm.DB, in pharmacycontract.LicenceEvidenceScanInput, current licenceEvidence) error {
	if current.Status != "scan_error" {
		return nil
	}
	return tx.Exec(`UPDATE pharmacy_licence_evidence SET status='scan_pending',updated_at=clock_timestamp() WHERE tenant_id=? AND outlet_id=? AND licence_id=? AND id=? AND status='scan_error' AND scan_attempt=?`, in.TenantID, in.OutletID, in.LicenceID, in.FileID, in.ScanAttempt).Error
}

func (p *LicenceEvidenceProcessor) retryableFailure(ctx context.Context, in pharmacycontract.LicenceEvidenceScanInput, code string, cause error) (string, error) {
	status, err := p.failure(ctx, in, "scan_error", code)
	if err != nil {
		return "", err
	}
	if status != "scan_error" {
		return status, nil
	}
	return status, fmt.Errorf("licence evidence processing temporarily unavailable: %w", cause)
}

func (p *LicenceEvidenceProcessor) failure(ctx context.Context, in pharmacycontract.LicenceEvidenceScanInput, state, code string) (string, error) {
	status := ""
	err := p.transaction(ctx, in, func(tx *gorm.DB) error {
		current, err := workerLicenceEvidence(tx, in)
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
		if err := resetLicenceEvidenceError(tx, in, current); err != nil {
			return err
		}
		result := tx.Exec(`UPDATE pharmacy_licence_evidence SET status=?,failure_code=?,updated_at=clock_timestamp() WHERE tenant_id=? AND outlet_id=? AND licence_id=? AND id=? AND status='scan_pending' AND scan_attempt=?`, state, code, in.TenantID, in.OutletID, in.LicenceID, in.FileID, in.ScanAttempt)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrLicenceEvidenceIntegrity
		}
		if err := licenceEvidenceStateEvent(tx, in, state); err != nil {
			return err
		}
		status = state
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("commit licence evidence failure: %w", err)
	}
	return status, nil
}

func licenceEvidenceStateEvent(tx *gorm.DB, in pharmacycontract.LicenceEvidenceScanInput, status string) error {
	data, err := json.Marshal(pharmacycontract.LicenceEvidenceChangedData{LicenceEvidenceScanRequestedData: pharmacycontract.LicenceEvidenceScanRequestedData{OutletID: in.OutletID, LicenceID: in.LicenceID, FileID: in.FileID, ScanAttempt: in.ScanAttempt}, Status: status})
	if err != nil {
		return err
	}
	return events.Enqueue(tx, pharmacycontract.SubjectLicenceEvidenceChanged, events.Event{Type: pharmacycontract.SubjectLicenceEvidenceChanged, Version: 1, TenantID: in.TenantID, Data: data})
}

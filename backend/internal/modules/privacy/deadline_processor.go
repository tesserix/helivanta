package privacy

import (
	"context"
	"errors"
	privacycontract "github.com/tesserix/helivanta/internal/modules/privacy/contract"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

const deadlineWorkerActor = "service:privacy-deadline-worker"

// DeadlineProcessor holds only the restricted metadata-worker identity.
type DeadlineProcessor struct{ db *tenantdb.DB }

// NewDeadlineProcessor rejects owner, application, inherited and bypass identities.
func NewDeadlineProcessor(ctx context.Context, db *tenantdb.DB) (*DeadlineProcessor, error) {
	if db == nil {
		return nil, errors.New("privacy deadline database unavailable")
	}
	var allowed bool
	err := db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT current_user='helivanta_privacy_worker' AND NOT rolsuper AND NOT rolbypassrls AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolinherit AND NOT EXISTS(SELECT 1 FROM pg_auth_members WHERE member=pg_roles.oid) FROM pg_roles WHERE rolname=current_user`).Scan(&allowed).Error
	})
	if err != nil || !allowed {
		return nil, errors.New("privacy deadline database identity is not authorized")
	}
	return &DeadlineProcessor{db: db}, nil
}
func (p *DeadlineProcessor) transaction(ctx context.Context, in privacycontract.DeadlineWorkflowInput, fn func(*gorm.DB) error) error {
	if !validDeadlineInput(in) {
		return errors.New("invalid privacy deadline scope")
	}
	return p.db.WithTenant(tenantdb.WithActor(ctx, deadlineWorkerActor), in.TenantID, func(tx *gorm.DB) error {
		if err := scope(tx, authn.Principal{TenantID: in.TenantID, Subject: deadlineWorkerActor}, false); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.privacy_request',?,true),set_config('statement_timeout','3s',true)`, in.RequestID).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}
func readDeadlineSnapshot(tx *gorm.DB, in privacycontract.DeadlineWorkflowInput) (DeadlineSnapshot, error) {
	var out DeadlineSnapshot
	result := tx.Raw(`SELECT version,privacy_deadline_clock() AS observed_at,acknowledge_by,resolve_by,acknowledged_at IS NOT NULL AS acknowledged,paused_at IS NOT NULL AS paused,terminal,acknowledgement_escalated_at IS NOT NULL AS acknowledgement_escalated,resolution_escalated_at IS NOT NULL AS resolution_escalated FROM privacy_case_deadlines WHERE tenant_id=? AND id=? FOR UPDATE`, in.TenantID, in.RequestID).Scan(&out)
	if result.Error != nil {
		return out, result.Error
	}
	if result.RowsAffected != 1 {
		return out, gorm.ErrRecordNotFound
	}
	return out, nil
}

// Refresh records a committed observation without exposing private case content.
func (p *DeadlineProcessor) Refresh(ctx context.Context, in privacycontract.DeadlineWorkflowInput) (DeadlineSnapshot, error) {
	var out DeadlineSnapshot
	err := p.transaction(ctx, in, func(tx *gorm.DB) error {
		var err error
		out, err = readDeadlineSnapshot(tx, in)
		if err != nil {
			return err
		}
		return tx.Exec(`UPDATE privacy_case_deadlines SET worker_observed_at=? WHERE tenant_id=? AND id=?`, out.ObservedAt, in.TenantID, in.RequestID).Error
	})
	return out, err
}

// Expire rechecks committed database time and retains at most one receipt per kind.
func (p *DeadlineProcessor) Expire(ctx context.Context, in DeadlineExpiryInput) (DeadlineSnapshot, error) {
	if in.Kind != "acknowledgement" && in.Kind != "resolution" {
		return DeadlineSnapshot{}, errors.New("invalid privacy deadline kind")
	}
	var out DeadlineSnapshot
	err := p.transaction(ctx, in.DeadlineWorkflowInput, func(tx *gorm.DB) error {
		var err error
		out, err = readDeadlineSnapshot(tx, in.DeadlineWorkflowInput)
		if err != nil {
			return err
		}
		if out.Terminal {
			return nil
		}
		if in.Kind == "acknowledgement" && (out.Acknowledged || out.AcknowledgementEscalated || out.ObservedAt.Before(out.AcknowledgeBy)) {
			return nil
		}
		if in.Kind == "resolution" && (out.Paused || out.ResolutionEscalated || out.ObservedAt.Before(out.ResolveBy)) {
			return nil
		}
		if err := tx.Exec(`INSERT INTO privacy_deadline_escalations(tenant_id,request_id,kind) VALUES(?,?,?) ON CONFLICT(tenant_id,request_id,kind) DO NOTHING`, in.TenantID, in.RequestID, in.Kind).Error; err != nil {
			return err
		}
		out, err = readDeadlineSnapshot(tx, in.DeadlineWorkflowInput)
		return err
	})
	return out, err
}

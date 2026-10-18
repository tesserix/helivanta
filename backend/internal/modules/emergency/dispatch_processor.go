package emergency

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	emergencycontract "github.com/tesserix/helivanta/internal/modules/emergency/contract"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"time"
)

const dispatchWorkerActor = "service:emergency-dispatch-worker"

// DispatchSnapshot is operational state safe to retain in workflow history.
type DispatchSnapshot struct {
	State                   string     `json:"state"`
	Version                 int64      `json:"version"`
	AcknowledgementDeadline *time.Time `json:"acknowledgement_deadline"`
}

// DispatchProcessor uses only the dedicated, non-bypass workflow identity.
type DispatchProcessor struct{ db *tenantdb.DB }

// NewDispatchProcessor rejects owner, application and bypass database identities.
func NewDispatchProcessor(ctx context.Context, db *tenantdb.DB) (*DispatchProcessor, error) {
	if db == nil {
		return nil, errors.New("dispatch database unavailable")
	}
	var allowed bool
	err := db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT current_user='helivanta_emergency_worker' AND NOT rolsuper AND NOT rolbypassrls AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolinherit FROM pg_roles WHERE rolname=current_user`).Scan(&allowed).Error
	})
	if err != nil || !allowed {
		return nil, errors.New("dispatch workflow database identity is not authorized")
	}
	return &DispatchProcessor{db: db}, nil
}
func validDispatchWorkflowInput(in emergencycontract.DispatchWorkflowInput) bool {
	for i, s := range []string{in.TenantID, in.BaseID, in.DispatchID} {
		id, err := uuid.Parse(s)
		if err != nil || id == uuid.Nil || id.String() != s || (i > 0 && id.Version() != 7) {
			return false
		}
	}
	return true
}
func (p *DispatchProcessor) transaction(ctx context.Context, in emergencycontract.DispatchWorkflowInput, fn func(*gorm.DB) error) error {
	if !validDispatchWorkflowInput(in) {
		return errors.New("invalid dispatch workflow identifiers")
	}
	return p.db.WithTenant(tenantdb.WithActor(ctx, dispatchWorkerActor), in.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.emergency_base',?,true),set_config('app.emergency_dispatch',?,true),set_config('app.emergency_subject',?,true),set_config('statement_timeout','3s',true)`, in.BaseID, in.DispatchID, dispatchWorkerActor).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}
func dispatchSnapshot(tx *gorm.DB, in emergencycontract.DispatchWorkflowInput) (DispatchSnapshot, error) {
	var out DispatchSnapshot
	err := tx.Table("emergency_dispatches").Select("state,version,acknowledgement_deadline").Where("tenant_id=? AND base_id=? AND id=?", in.TenantID, in.BaseID, in.DispatchID).Take(&out).Error
	return out, err
}

// Snapshot reads only the dispatch identified by its committed workflow key.
func (p *DispatchProcessor) Snapshot(ctx context.Context, in emergencycontract.DispatchWorkflowInput) (DispatchSnapshot, error) {
	var out DispatchSnapshot
	err := p.transaction(ctx, in, func(tx *gorm.DB) error { var err error; out, err = dispatchSnapshot(tx, in); return err })
	return out, err
}
func workerDispatchCommand(tx *gorm.DB, in emergencycontract.DispatchWorkflowInput, id uuid.UUID, version int64, operation string) error {
	if err := tx.Exec(`INSERT INTO emergency_dispatch_commands(tenant_id,base_id,id,dispatch_id,expected_version,operation,evidence_reference,actor_subject) VALUES(?,?,?,?,?,?,'WORKFLOW/EMERGENCY-V1',?)`, in.TenantID, in.BaseID, id, in.DispatchID, version, operation, dispatchWorkerActor).Error; err != nil {
		return err
	}
	principal := authn.Principal{TenantID: in.TenantID, Subject: dispatchWorkerActor}
	base, dispatch := uuid.MustParse(in.BaseID), uuid.MustParse(in.DispatchID)
	if err := audit(tx, principal, base, dispatch, "dispatch."+operation, nil); err != nil {
		return err
	}
	data, err := json.Marshal(emergencycontract.DispatchChangedData{BaseID: in.BaseID, DispatchID: in.DispatchID, Version: version + 1})
	if err != nil {
		return err
	}
	return transportEvent(tx, principal, emergencycontract.SubjectDispatchChanged, data)
}

// Offer retries safely and revalidates the current clinical and fleet evidence.
func (p *DispatchProcessor) Offer(ctx context.Context, in emergencycontract.DispatchWorkflowInput) (DispatchSnapshot, error) {
	var out DispatchSnapshot
	err := p.transaction(ctx, in, func(tx *gorm.DB) error {
		current, err := dispatchSnapshot(tx, in)
		if err != nil {
			return err
		}
		if current.State == "workflow_pending" {
			if err := workerDispatchCommand(tx, in, uuid.MustParse(in.DispatchID), current.Version, "offer"); err != nil {
				return err
			}
		}
		out, err = dispatchSnapshot(tx, in)
		return err
	})
	return out, err
}

// Expire escalates unanswered offers without releasing uncertain capacity.
func (p *DispatchProcessor) Expire(ctx context.Context, in emergencycontract.DispatchWorkflowInput, command uuid.UUID) (DispatchSnapshot, error) {
	if command.Version() != 7 {
		return DispatchSnapshot{}, errors.New("invalid dispatch timeout command")
	}
	var out DispatchSnapshot
	err := p.transaction(ctx, in, func(tx *gorm.DB) error {
		current, err := dispatchSnapshot(tx, in)
		if err != nil {
			return err
		}
		if current.State == "offered" {
			if err := workerDispatchCommand(tx, in, command, current.Version, "acknowledgement_timeout"); err != nil {
				return err
			}
		}
		out, err = dispatchSnapshot(tx, in)
		return err
	})
	return out, err
}

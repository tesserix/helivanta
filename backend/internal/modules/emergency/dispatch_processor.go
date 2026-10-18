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
	"gorm.io/gorm/clause"
	"time"
)

const dispatchWorkerActor = "service:emergency-dispatch-worker"

// DispatchSnapshot is operational state safe to retain in workflow history.
type DispatchSnapshot struct {
	ReceivingRequestID      *uuid.UUID `json:"receiving_request_id"`
	ReceivingDeadline       *time.Time `json:"receiving_deadline"`
	ReceivingDeadlineKind   string     `json:"receiving_deadline_kind"`
	ReceivingTimedOut       bool       `json:"receiving_timed_out"`
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
		return tx.Raw(`SELECT current_user='helivanta_emergency_worker' AND NOT rolsuper AND NOT rolbypassrls AND NOT rolcreatedb AND NOT rolcreaterole AND NOT rolinherit AND NOT EXISTS(SELECT 1 FROM pg_auth_members WHERE member=pg_roles.oid) FROM pg_roles WHERE rolname=current_user`).Scan(&allowed).Error
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
	if err != nil || out.State == "cleared" || out.State == "cancelled" {
		return out, err
	}
	var receiving struct {
		ID       uuid.UUID
		Deadline *time.Time
		Kind     string
		TimedOut bool
	}
	err = tx.Raw(`SELECT r.id,r.deadline,r.kind,
 EXISTS(SELECT 1 FROM emergency_receiving_timeouts e WHERE e.tenant_id=r.tenant_id AND e.base_id=r.base_id AND e.request_id=r.id AND e.kind=r.kind)AS timed_out
 FROM emergency_receiving_workflow_state r WHERE r.tenant_id=? AND r.base_id=? AND r.dispatch_id=? ORDER BY r.sequence DESC LIMIT 1`, in.TenantID, in.BaseID, in.DispatchID).Scan(&receiving).Error
	if err != nil {
		return out, err
	}
	if receiving.ID != uuid.Nil {
		out.ReceivingRequestID = &receiving.ID
		out.ReceivingDeadline = receiving.Deadline
		out.ReceivingDeadlineKind = receiving.Kind
		out.ReceivingTimedOut = receiving.TimedOut
	}
	return out, nil
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

// DispatchReceivingExpiryInput identifies one receiving deadline without private payloads.
type DispatchReceivingExpiryInput struct {
	emergencycontract.DispatchWorkflowInput
	RequestID string `json:"request_id"`
	Kind      string `json:"kind"`
}

// ExpireReceiving escalates the selected receiving wait without releasing capacity.
func (p *DispatchProcessor) ExpireReceiving(ctx context.Context, in DispatchReceivingExpiryInput) (DispatchSnapshot, error) {
	request, err := uuid.Parse(in.RequestID)
	if !validDispatchWorkflowInput(in.DispatchWorkflowInput) || err != nil || request.Version() != 7 || (in.Kind != "response" && in.Kind != "acceptance") {
		return DispatchSnapshot{}, errors.New("invalid receiving deadline identifiers")
	}
	var out DispatchSnapshot
	err = p.transaction(ctx, in.DispatchWorkflowInput, func(tx *gorm.DB) error {
		var locked struct{ ID uuid.UUID }
		if err := tx.Table("emergency_bases").Select("id").Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).Where("tenant_id=? AND id=?", in.TenantID, in.BaseID).Take(&locked).Error; err != nil {
			return err
		}
		locked.ID = uuid.Nil
		if err := tx.Table("emergency_dispatches").Select("id").Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND base_id=? AND id=?", in.TenantID, in.BaseID, in.DispatchID).Take(&locked).Error; err != nil {
			return err
		}
		current, err := dispatchSnapshot(tx, in.DispatchWorkflowInput)
		if err != nil {
			return err
		}
		if current.ReceivingRequestID == nil || *current.ReceivingRequestID != request || current.ReceivingDeadlineKind != in.Kind || current.ReceivingDeadline == nil || current.ReceivingTimedOut {
			out = current
			return nil
		}
		result := tx.Exec(`INSERT INTO emergency_receiving_timeouts(tenant_id,base_id,dispatch_id,request_id,kind,deadline)VALUES(?,?,?,?,?,?)ON CONFLICT DO NOTHING`, in.TenantID, in.BaseID, in.DispatchID, request, in.Kind, current.ReceivingDeadline)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 1 {
			principal := authn.Principal{TenantID: in.TenantID, Subject: dispatchWorkerActor}
			if err := audit(tx, principal, uuid.MustParse(in.BaseID), uuid.MustParse(in.DispatchID), "dispatch.receiving_timeout", nil); err != nil {
				return err
			}
			data, err := json.Marshal(emergencycontract.DispatchChangedData{BaseID: in.BaseID, DispatchID: in.DispatchID, Version: current.Version})
			if err != nil {
				return err
			}
			if err := transportEvent(tx, principal, emergencycontract.SubjectDispatchChanged, data); err != nil {
				return err
			}
		}
		out, err = dispatchSnapshot(tx, in.DispatchWorkflowInput)
		return err
	})
	return out, err
}

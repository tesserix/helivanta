package iam

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

//go:embed assignment_projection.sql
var assignmentProjectionSchema string

type assignmentProjector interface {
	ProjectScopedAssignment(context.Context, authz.ScopedAssignment) error
}

type assignmentSnapshot struct {
	ID          uuid.UUID
	Version     int64
	HospitalID  uuid.UUID
	Subject     string
	Permissions []authz.Permission `gorm:"serializer:json"`
}

func (assignmentSnapshot) TableName() string { return "iam_assignment_snapshots" }

type assignmentProjection struct {
	db     *tenantdb.DB
	writer assignmentProjector
}

func queueAssignmentProjection(tx *gorm.DB, bus *events.Bus, tenant string, id uuid.UUID) error {
	if bus == nil {
		return errors.New("assignment projection transport unavailable")
	}
	data, err := json.Marshal(iamcontract.AssignmentProjectionRequestedData{ProposalID: id})
	if err != nil {
		return err
	}
	eventID, err := uuid.NewV7()
	if err != nil {
		return err
	}
	return bus.Publish(tx, iamcontract.SubjectAssignmentProjectionRequested, events.Event{ID: eventID.String(), Type: iamcontract.SubjectAssignmentProjectionRequested, Version: 1, TenantID: tenant, OccurredAt: time.Now(), Data: data})
}
func assignmentProjectionRequest(evt events.Event) (uuid.UUID, error) {
	var data iamcontract.AssignmentProjectionRequestedData
	tenant, err := uuid.Parse(evt.TenantID)
	if err != nil || tenant == uuid.Nil || tenant.String() != evt.TenantID || evt.Version != 1 || evt.DestinationTenantID != "" || json.Unmarshal(evt.Data, &data) != nil || data.ProposalID.Version() != 7 {
		return uuid.Nil, errors.New("invalid assignment projection request")
	}
	return data.ProposalID, nil
}
func assignmentProjectionConsumer(deps platform.Deps) events.Consumer {
	writer, _ := deps.Authz.(assignmentProjector)
	h := &assignmentProjection{db: deps.DB, writer: writer}
	return events.Consumer{Name: "iam-assignment-projection", Subject: iamcontract.SubjectAssignmentProjectionRequested, Handle: func(_ context.Context, _ *gorm.DB, evt events.Event) error {
		_, err := assignmentProjectionRequest(evt)
		return err
	}, AfterCommit: h.project}
}
func (h *assignmentProjection) project(ctx context.Context, evt events.Event) error {
	id, err := assignmentProjectionRequest(evt)
	if err != nil {
		return err
	}
	if h.db == nil || h.writer == nil {
		return errors.New("assignment projection unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var proposal assignmentProposal
	var epoch int64
	err = h.db.WithTenant(ctx, evt.TenantID, func(tx *gorm.DB) error {
		if err := assignmentAccess(tx); err != nil {
			return err
		}
		if err := tx.Select("id,hospital_id,subject").Where("tenant_id=? AND id=?", evt.TenantID, id).Take(&proposal).Error; err != nil {
			return err
		}
		return tx.Table("iam_assignment_scope_epochs").Select("epoch").Where("tenant_id=? AND hospital_id=? AND subject=?", evt.TenantID, proposal.HospitalID, proposal.Subject).Take(&epoch).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	after := uuid.Nil
	for {
		var snapshots []assignmentSnapshot
		err = h.db.WithTenant(ctx, evt.TenantID, func(tx *gorm.DB) error {
			if err := assignmentAccess(tx); err != nil {
				return err
			}
			return tx.Table("iam_assignment_snapshots AS s").Select("s.id,s.version,s.hospital_id,s.subject,s.permissions").
				Joins("JOIN iam_assignment_proposals p ON p.tenant_id=s.tenant_id AND p.id=s.id").
				Joins("JOIN iam_subject_membership_epochs e ON e.tenant_id=s.tenant_id AND e.subject=s.subject AND e.epoch=s.membership_epoch AND e.member_count>0").
				Where("s.tenant_id=? AND s.hospital_id=? AND s.subject=? AND s.id>? AND p.status IN ('approved_pending_activation','active') AND s.expires_at>CURRENT_TIMESTAMP", evt.TenantID, proposal.HospitalID, proposal.Subject, after).
				Order("s.id").Limit(200).Find(&snapshots).Error
		})
		if err != nil {
			return err
		}
		for _, s := range snapshots {
			if err := h.writer.ProjectScopedAssignment(ctx, authz.ScopedAssignment{TenantID: evt.TenantID, HospitalID: s.HospitalID.String(), ScopeKind: "hospital", ScopeID: s.HospitalID.String(), ID: s.ID.String(), Version: s.Version, Subject: s.Subject, Permissions: s.Permissions}); err != nil {
				return errors.New("assignment graph projection failed")
			}
			if err := h.activate(ctx, evt.TenantID, s, epoch); err != nil {
				return err
			}
			after = s.ID
		}
		if len(snapshots) < 200 {
			break
		}
	}
	return h.db.WithTenant(ctx, evt.TenantID, func(tx *gorm.DB) error {
		if err := assignmentAccess(tx); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.assignment_projection_ack','authorized',true)`).Error; err != nil {
			return err
		}
		result := tx.Table("iam_assignment_scope_epochs").Where("tenant_id=? AND hospital_id=? AND subject=? AND epoch=?", evt.TenantID, proposal.HospitalID, proposal.Subject, epoch).Update("acknowledged_epoch", epoch)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("assignment scope changed during projection")
		}
		return nil
	})
}

package workforce

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

const assignmentScopeRequested = iamcontract.SubjectAssignmentScopeRequested

func assignmentScopeConsumer(bus *events.Bus) events.Consumer {
	return events.Consumer{Name: "workforce-assignment-scope", Subject: assignmentScopeRequested, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
		var request iamcontract.AssignmentScopeRequestedData
		if evt.Version != 1 || evt.DestinationTenantID != "" {
			return errors.New("invalid assignment scope request envelope")
		}
		if err := json.Unmarshal(evt.Data, &request); err != nil {
			return errors.New("invalid assignment scope request")
		}
		if request.ProposalID.Version() != 7 || request.RequestID.Version() != 7 || request.HospitalID == uuid.Nil || !validLoginSubject(request.ActorSubject) {
			return errors.New("invalid assignment scope fields")
		}
		var eligible bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_hospitals h JOIN workforce_hospital_access a ON a.tenant_id=h.tenant_id AND a.hospital_id=h.id WHERE h.tenant_id=? AND h.id=? AND a.subject=? AND a.level='manager')`, evt.TenantID, request.HospitalID, request.ActorSubject).Scan(&eligible).Error; err != nil {
			return err
		}
		data, err := json.Marshal(workforcecontract.AssignmentScopeVerifiedData{ProposalID: request.ProposalID, RequestID: request.RequestID, HospitalID: request.HospitalID, Eligible: eligible})
		if err != nil {
			return err
		}
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		return bus.Publish(tx, workforcecontract.SubjectAssignmentScopeVerified, events.Event{ID: id.String(), Type: workforcecontract.SubjectAssignmentScopeVerified, Version: 1, TenantID: evt.TenantID, OccurredAt: time.Now(), Data: data})
	}}
}

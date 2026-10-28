package emergency

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/uuid"
	emergencycontract "github.com/tesserix/helivanta/internal/modules/emergency/contract"
	"github.com/tesserix/helivanta/pkg/events"
	"go.temporal.io/sdk/client"
	"gorm.io/gorm"
)

func receivingEventInput(evt events.Event) (emergencycontract.DispatchWorkflowInput, emergencycontract.ReceivingChangedData, error) {
	var data emergencycontract.ReceivingChangedData
	in := emergencycontract.DispatchWorkflowInput{TenantID: evt.DestinationTenantID}
	invalid := errors.New("invalid receiving notification scope")
	if evt.Type != emergencycontract.SubjectReceivingChanged || evt.Version != 1 || len(evt.Data) > 2048 {
		return in, data, invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(evt.Data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&data); err != nil {
		return in, data, invalid
	}
	var extra any
	if !errors.Is(decoder.Decode(&extra), io.EOF) {
		return in, data, invalid
	}
	in.BaseID = data.BaseID
	in.DispatchID = data.DispatchID
	origin, err := uuid.Parse(evt.TenantID)
	if err != nil || origin == uuid.Nil || origin.String() != evt.TenantID {
		return in, data, invalid
	}
	request, err := uuid.Parse(data.RequestID)
	if err != nil || request.Version() != 7 || request.String() != data.RequestID || data.Version < 1 || !validDispatchWorkflowInput(in) {
		return in, data, invalid
	}
	return in, data, nil
}

// ReceivingConsumer verifies directed receipt lineage before waking the provider workflow.
func ReceivingConsumer(c client.Client) events.Consumer {
	return events.Consumer{Name: "emergency-receiving", Subject: emergencycontract.SubjectReceivingChanged,
		Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
			in, data, err := receivingEventInput(evt)
			if err != nil {
				return err
			}
			var previous struct {
				Subject      string
				Base         string
				Notification string
			}
			if err := tx.Raw(`SELECT COALESCE(current_setting('app.emergency_subject',true),'')AS subject,COALESCE(current_setting('app.emergency_base',true),'')AS base,COALESCE(current_setting('app.emergency_notification',true),'')AS notification`).Scan(&previous).Error; err != nil {
				return err
			}
			if err := tx.Exec(`SELECT set_config('app.emergency_subject','service:emergency-receiving-notifications',true),set_config('app.emergency_base',?,true),set_config('app.emergency_notification',?,true)`, in.BaseID, data.RequestID).Error; err != nil {
				return err
			}
			var found struct{ ID uuid.UUID }
			if err := tx.Table("emergency_receiving_request_status").Select("id").Where("tenant_id=? AND base_id=? AND dispatch_id=? AND id=? AND destination_tenant_id=? AND version>=?", in.TenantID, in.BaseID, in.DispatchID, data.RequestID, evt.TenantID, data.Version).Take(&found).Error; err != nil {
				return errors.New("receiving notification receipt unavailable")
			}
			return tx.Exec(`SELECT set_config('app.emergency_subject',?,true),set_config('app.emergency_base',?,true),set_config('app.emergency_notification',?,true)`, previous.Subject, previous.Base, previous.Notification).Error
		}, AfterCommit: func(ctx context.Context, evt events.Event) error {
			in, data, err := receivingEventInput(evt)
			if err != nil {
				return err
			}
			payload, err := json.Marshal(emergencycontract.DispatchChangedData{BaseID: in.BaseID, DispatchID: in.DispatchID, Version: data.Version})
			if err != nil {
				return err
			}
			return DispatchConsumer(c).AfterCommit(ctx, events.Event{ID: evt.ID, Type: emergencycontract.SubjectDispatchChanged, Version: 1, TenantID: in.TenantID, OccurredAt: evt.OccurredAt, Data: payload})
		}}
}

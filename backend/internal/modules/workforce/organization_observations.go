package workforce

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
	organizationcontract "github.com/tesserix/helivanta/internal/modules/organization/contract"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

//go:embed organization_observations.sql
var organizationObservationsSchema string

type organizationObservation struct {
	TenantID       uuid.UUID
	RequestID      uuid.UUID
	HospitalID     uuid.UUID
	ObservedExists bool
	CreatedAt      time.Time `gorm:"autoCreateTime:false;default:now()"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime:false;default:now()"`
}

func (organizationObservation) TableName() string { return "workforce_organization_observations" }

func organizationReferenceConsumer(bus *events.Bus) events.Consumer {
	return events.Consumer{Name: "workforce-organization-reference", Subject: organizationcontract.SubjectHospitalReferenceRequested, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
		tenant, err := uuid.Parse(evt.TenantID)
		if err != nil || tenant == uuid.Nil || evt.Type != organizationcontract.SubjectHospitalReferenceRequested || len(evt.Data) > 1024 {
			return errors.New("invalid organization reference envelope")
		}
		evt.TenantID = tenant.String()
		var in organizationcontract.HospitalReferenceRequestedData
		decoder := json.NewDecoder(bytes.NewReader(evt.Data))
		decoder.DisallowUnknownFields()
		if evt.Version != 1 || evt.DestinationTenantID != "" || decoder.Decode(&in) != nil {
			return errors.New("invalid organization hospital observation request")
		}
		var extra any
		if !errors.Is(decoder.Decode(&extra), io.EOF) || in.RequestID.Version() != 7 || in.RequestID.Variant() != uuid.RFC4122 || in.HospitalID == uuid.Nil {
			return errors.New("invalid organization hospital reference")
		}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, evt.TenantID+":organization-observation:"+in.RequestID.String()).Error; err != nil {
			return err
		}
		var retained organizationObservation
		err = tx.Where("tenant_id=? AND request_id=?", evt.TenantID, in.RequestID).Take(&retained).Error
		if err == nil {
			if retained.HospitalID != in.HospitalID {
				return errors.New("organization hospital observation identity conflicts")
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var exists bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_hospitals WHERE tenant_id=? AND id=?)`, evt.TenantID, in.HospitalID).Scan(&exists).Error; err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.workforce_organization_observation','authorized',true)`).Error; err != nil {
			return err
		}
		retained = organizationObservation{TenantID: uuid.MustParse(evt.TenantID), RequestID: in.RequestID, HospitalID: in.HospitalID, ObservedExists: exists}
		if err := tx.Create(&retained).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,'system:organization-reference','hospital_reference_observed','workforce_organization_observations',?)`, evt.TenantID, in.RequestID).Error; err != nil {
			return err
		}
		raw, err := json.Marshal(workforcecontract.OrganizationHospitalObservedData{RequestID: in.RequestID, HospitalID: in.HospitalID, Exists: exists})
		if err != nil {
			return err
		}
		return bus.Publish(tx, workforcecontract.SubjectOrganizationHospitalObserved, events.Event{Type: workforcecontract.SubjectOrganizationHospitalObserved, Version: 1, TenantID: evt.TenantID, Data: raw})
	}}
}

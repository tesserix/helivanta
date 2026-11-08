package workforce

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/uuid"
	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

//go:embed partner_group_scope.sql
var partnerGroupScopeSchema string

func partnerGroupHospitalScopeConsumer(bus *events.Bus) events.Consumer {
	return events.Consumer{Name: "workforce-partner-group-hospital-scope", Subject: iamcontract.SubjectPartnerGroupHospitalScopeRequested, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
		tenant, err := uuid.Parse(evt.TenantID)
		if err != nil || tenant == uuid.Nil || tenant.String() != evt.TenantID || evt.Version != 1 || evt.DestinationTenantID != "" || evt.Type != iamcontract.SubjectPartnerGroupHospitalScopeRequested || len(evt.Data) > 1024 || bus == nil {
			return errors.New("invalid partner group hospital scope request envelope")
		}
		var in iamcontract.PartnerGroupHospitalScopeRequestedData
		decoder := json.NewDecoder(bytes.NewReader(evt.Data))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&in) != nil {
			return errors.New("invalid partner group hospital scope request")
		}
		var extra any
		if !errors.Is(decoder.Decode(&extra), io.EOF) || in.GroupID.Version() != 7 || in.GroupID.Variant() != uuid.RFC4122 || in.HospitalID == uuid.Nil {
			return errors.New("partner group hospital scope requires exact identifiers")
		}
		eventID, err := uuid.Parse(evt.ID)
		if err != nil || eventID == uuid.Nil {
			return errors.New("partner group hospital scope requires delivery identity")
		}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, evt.TenantID+":partner-group-observation:"+in.GroupID.String()).Error; err != nil {
			return err
		}
		var retained struct {
			HospitalID     uuid.UUID
			ObservedExists bool
		}
		err = tx.Table("workforce_partner_group_observations").Select("hospital_id,observed_exists").Where("tenant_id=? AND group_id=?", evt.TenantID, in.GroupID).Take(&retained).Error
		if err == nil {
			if retained.HospitalID != in.HospitalID {
				return errors.New("partner group hospital scope identity conflicts")
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var exists bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_hospitals WHERE tenant_id=? AND id=? AND establishment_kind='hospital')`, evt.TenantID, in.HospitalID).Scan(&exists).Error; err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.workforce_partner_group_observation','authorized',true)`).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO workforce_partner_group_observations(tenant_id,group_id,hospital_id,observed_exists,source_event_id)VALUES(?,?,?,?,?)`, evt.TenantID, in.GroupID, in.HospitalID, exists, eventID).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,'system:partner-group-hospital-scope','partner_group_hospital_observed','workforce_partner_group_observations',?)`, evt.TenantID, in.GroupID).Error; err != nil {
			return err
		}
		payload, err := json.Marshal(workforcecontract.PartnerGroupHospitalScopeObservedData{GroupID: in.GroupID, HospitalID: in.HospitalID, Exists: &exists})
		if err != nil {
			return err
		}
		return bus.Publish(tx, workforcecontract.SubjectPartnerGroupHospitalScopeObserved, events.Event{Type: workforcecontract.SubjectPartnerGroupHospitalScopeObserved, Version: 1, TenantID: evt.TenantID, Data: payload})
	}}
}

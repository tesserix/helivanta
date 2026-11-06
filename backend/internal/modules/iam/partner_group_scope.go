package iam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/uuid"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

func partnerGroupHospitalObservationConsumer() events.Consumer {
	return events.Consumer{Name: "iam-partner-group-hospital-scope", Subject: workforcecontract.SubjectPartnerGroupHospitalScopeObserved, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
		tenant, err := uuid.Parse(evt.TenantID)
		if err != nil || tenant == uuid.Nil || tenant.String() != evt.TenantID || evt.Version != 1 || evt.DestinationTenantID != "" || evt.Type != workforcecontract.SubjectPartnerGroupHospitalScopeObserved || len(evt.Data) > 1024 {
			return errors.New("invalid partner group scope observation envelope")
		}
		var in workforcecontract.PartnerGroupHospitalScopeObservedData
		decoder := json.NewDecoder(bytes.NewReader(evt.Data))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&in) != nil {
			return errors.New("invalid partner group scope observation")
		}
		var extra any
		if !errors.Is(decoder.Decode(&extra), io.EOF) || in.GroupID.Version() != 7 || in.GroupID.Variant() != uuid.RFC4122 || in.HospitalID == uuid.Nil || in.Exists == nil {
			return errors.New("partner group scope requires exact identifiers and explicit eligibility")
		}
		eventID, err := uuid.Parse(evt.ID)
		if err != nil || eventID == uuid.Nil {
			return errors.New("partner group scope observation requires delivery identity")
		}
		if err := tx.Exec(`SELECT set_config('app.iam_partner_groups','observe',true),pg_advisory_xact_lock(hashtextextended(?,0))`, evt.TenantID+":partner-group:"+in.GroupID.String()).Error; err != nil {
			return err
		}
		group, err := readPartnerGroup(tx, evt.TenantID, in.GroupID)
		if err != nil {
			return err
		}
		if group.HospitalID != in.HospitalID {
			return errors.New("partner group scope observation does not match the proposal")
		}
		var retained struct {
			HospitalID     uuid.UUID
			ObservedExists bool
		}
		err = tx.Table("iam_partner_group_scope_observations").Select("hospital_id,observed_exists").Where("tenant_id=? AND group_id=?", evt.TenantID, in.GroupID).Take(&retained).Error
		if err == nil {
			if retained.HospitalID != in.HospitalID || retained.ObservedExists != *in.Exists {
				return errors.New("conflicting partner group scope observation")
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Exec(`INSERT INTO iam_partner_group_scope_observations(tenant_id,group_id,hospital_id,observed_exists,source_event_id) VALUES(?,?,?,?,?)`, evt.TenantID, in.GroupID, in.HospitalID, *in.Exists, eventID).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,'system:partner-group-hospital-scope','partner_group_hospital_observed','iam_partner_groups',?)`, evt.TenantID, in.GroupID).Error
	}}
}

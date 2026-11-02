package organization

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
	"gorm.io/gorm/clause"
)

func hospitalObservationConsumer() events.Consumer {
	return events.Consumer{Name: "organization-hospital-observation", Subject: workforcecontract.SubjectOrganizationHospitalObserved, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
		tenant, err := uuid.Parse(evt.TenantID)
		if err != nil || tenant == uuid.Nil || evt.Type != workforcecontract.SubjectOrganizationHospitalObserved || len(evt.Data) > 1024 {
			return errors.New("invalid organization observation envelope")
		}
		evt.TenantID = tenant.String()
		var fields map[string]json.RawMessage
		if json.NewDecoder(bytes.NewReader(evt.Data)).Decode(&fields) != nil {
			return errors.New("invalid organization observation payload")
		}
		observed, present := fields["exists"]
		observed = bytes.TrimSpace(observed)
		if !present || (!bytes.Equal(observed, []byte("true")) && !bytes.Equal(observed, []byte("false"))) {
			return errors.New("organization observation requires an explicit boolean")
		}
		var in workforcecontract.OrganizationHospitalObservedData
		decoder := json.NewDecoder(bytes.NewReader(evt.Data))
		decoder.DisallowUnknownFields()
		if evt.Version != 1 || evt.DestinationTenantID != "" || decoder.Decode(&in) != nil {
			return errors.New("invalid organization hospital observation")
		}
		var extra any
		if !errors.Is(decoder.Decode(&extra), io.EOF) || in.RequestID.Version() != 7 || in.RequestID.Variant() != uuid.RFC4122 || in.HospitalID == uuid.Nil {
			return errors.New("invalid observed organization reference")
		}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, evt.TenantID+":organization-registration:"+in.RequestID.String()).Error; err != nil {
			return err
		}
		var request registrationRequest
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND id=?", evt.TenantID, in.RequestID).Take(&request).Error; err != nil {
			return err
		}
		if request.HospitalID != in.HospitalID {
			return errConflict
		}
		if request.Status != "pending" {
			if request.ObservedExists == nil || *request.ObservedExists != in.Exists {
				return errConflict
			}
			return nil
		}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, evt.TenantID+":organization-hospital:"+in.HospitalID.String()).Error; err != nil {
			return err
		}
		var head registration
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND id=?", evt.TenantID, in.HospitalID).Take(&head).Error; err != nil {
			return err
		}
		if head.RequestID != in.RequestID || head.Status != "pending" {
			return errConflict
		}
		if err := tx.Exec(`SELECT set_config('app.organization_write','authorized',true),set_config('app.organization_observation','authorized',true)`).Error; err != nil {
			return err
		}
		status := "rejected"
		if in.Exists {
			status = "verified"
		}
		if err := tx.Model(&request).Where("tenant_id=? AND hospital_id=? AND id=?", evt.TenantID, in.HospitalID, in.RequestID).Updates(map[string]any{"status": status, "observed_exists": in.Exists, "updated_at": gorm.Expr("clock_timestamp()")}).Error; err != nil {
			return err
		}
		if err := tx.Model(&head).Where("tenant_id=? AND id=?", evt.TenantID, in.HospitalID).Updates(map[string]any{"status": status, "version": head.Version + 1, "updated_at": gorm.Expr("clock_timestamp()")}).Error; err != nil {
			return err
		}
		return audit(tx, evt.TenantID, "system:workforce-observation", "hospital_reference_decided", "organization_hospital_requests", in.RequestID)
	}}
}

package workforce

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"github.com/google/uuid"
	organizationcontract "github.com/tesserix/helivanta/internal/modules/organization/contract"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func departmentLinkObservationConsumer() events.Consumer {
	return events.Consumer{Name: "workforce-department-link-observation", Subject: organizationcontract.SubjectDepartmentLinkObserved, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
		tenant, err := uuid.Parse(evt.TenantID)
		if err != nil || tenant == uuid.Nil || evt.Type != organizationcontract.SubjectDepartmentLinkObserved || evt.Version != 1 || evt.DestinationTenantID != "" || len(evt.Data) > 2048 {
			return errors.New("invalid department observation envelope")
		}
		var in organizationcontract.DepartmentLinkObservedData
		d := json.NewDecoder(bytes.NewReader(evt.Data))
		d.DisallowUnknownFields()
		if d.Decode(&in) != nil {
			return errors.New("invalid department observation")
		}
		var extra any
		if !errors.Is(d.Decode(&extra), io.EOF) || in.RequestID.Version() != 7 || in.RequestID.Variant() != uuid.RFC4122 || in.HospitalID == uuid.Nil || in.MasterID == uuid.Nil || in.MasterRevisionID == uuid.Nil || in.DepartmentID.Version() != 7 || in.DepartmentID.Variant() != uuid.RFC4122 || in.DepartmentRevisionID.Version() != 7 || in.DepartmentRevisionID.Variant() != uuid.RFC4122 || in.ObservedAt.IsZero() || in.MatchesCurrent == nil {
			return errors.New("invalid department observation identity")
		}
		if err := tx.Exec(`SELECT set_config('app.workforce_department_observation','authorized',true),pg_advisory_xact_lock(hashtextextended(?,0))`, tenant.String()+":department-link:"+in.HospitalID.String()+":"+in.MasterID.String()).Error; err != nil {
			return err
		}
		var row departmentLink
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND id=?", tenant, in.RequestID).Take(&row).Error; err != nil {
			return err
		}
		if row.HospitalID != in.HospitalID || row.MasterID != in.MasterID || row.MasterRevisionID != in.MasterRevisionID || row.DepartmentID != in.DepartmentID || row.DepartmentRevisionID != in.DepartmentRevisionID {
			return errDepartmentLinkConflict
		}
		status := "rejected"
		if *in.MatchesCurrent {
			status = "matched"
		}
		if row.Status != "pending" {
			if row.Status != status || row.ObservedAt == nil || !row.ObservedAt.Equal(in.ObservedAt) {
				return errDepartmentLinkConflict
			}
			return nil
		}
		if err := tx.Model(&row).Where("tenant_id=? AND id=? AND status='pending'", tenant, row.ID).Updates(map[string]any{"status": status, "observed_at": in.ObservedAt, "updated_at": gorm.Expr("clock_timestamp()")}).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,'system:organization-department','workforce.department_link.observed',?,jsonb_build_object('version',?::bigint,'status',?::text))`, tenant, row.HospitalID, row.ID, row.Version, status).Error
	}}
}

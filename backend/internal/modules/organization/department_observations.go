package organization

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

//go:embed department_observations.sql
var departmentObservationsSchema string

type departmentObservation struct {
	TenantID             uuid.UUID
	ID                   uuid.UUID
	HospitalID           uuid.UUID
	MasterID             uuid.UUID
	MasterRevisionID     uuid.UUID
	DepartmentID         uuid.UUID
	DepartmentRevisionID uuid.UUID
	MatchesCurrent       bool
	CreatedAt            time.Time `gorm:"autoCreateTime:false;default:now()"`
	UpdatedAt            time.Time `gorm:"autoUpdateTime:false;default:now()"`
}

func (departmentObservation) TableName() string { return "organization_department_observations" }

func departmentLinkRequestConsumer(bus *events.Bus) events.Consumer {
	return events.Consumer{Name: "organization-department-link-request", Subject: workforcecontract.SubjectDepartmentLinkRequested, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
		tenant, err := uuid.Parse(evt.TenantID)
		if err != nil || tenant == uuid.Nil || evt.Type != workforcecontract.SubjectDepartmentLinkRequested || evt.Version != 1 || evt.DestinationTenantID != "" || len(evt.Data) > 2048 {
			return errors.New("invalid department handoff envelope")
		}
		var in workforcecontract.DepartmentLinkRequestedData
		d := json.NewDecoder(bytes.NewReader(evt.Data))
		d.DisallowUnknownFields()
		if d.Decode(&in) != nil {
			return errors.New("invalid department handoff request")
		}
		var extra any
		if !errors.Is(d.Decode(&extra), io.EOF) || in.RequestID.Version() != 7 || in.RequestID.Variant() != uuid.RFC4122 || in.HospitalID == uuid.Nil || in.MasterID == uuid.Nil || in.MasterRevisionID == uuid.Nil || in.DepartmentID.Version() != 7 || in.DepartmentID.Variant() != uuid.RFC4122 || in.DepartmentRevisionID.Version() != 7 || in.DepartmentRevisionID.Variant() != uuid.RFC4122 {
			return errors.New("invalid department handoff identity")
		}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, tenant.String()+":department-observation:"+in.RequestID.String()).Error; err != nil {
			return err
		}
		var row departmentObservation
		err = tx.Where("tenant_id=? AND id=?", tenant, in.RequestID).Take(&row).Error
		if err == nil {
			if row.HospitalID != in.HospitalID || row.MasterID != in.MasterID || row.MasterRevisionID != in.MasterRevisionID || row.DepartmentID != in.DepartmentID || row.DepartmentRevisionID != in.DepartmentRevisionID {
				return errors.New("department handoff request identity conflicts")
			}
		} else {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			var matches bool
			if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM organization_departments d JOIN organization_hospital_registrations h ON (h.tenant_id,h.id)=(d.tenant_id,d.hospital_id) WHERE d.tenant_id=? AND d.hospital_id=? AND d.id=? AND d.revision_id=? AND d.status='active' AND h.status='verified')`, tenant, in.HospitalID, in.DepartmentID, in.DepartmentRevisionID).Scan(&matches).Error; err != nil {
				return err
			}
			if err := tx.Exec(`SELECT set_config('app.organization_department_observation','authorized',true)`).Error; err != nil {
				return err
			}
			row = departmentObservation{TenantID: tenant, ID: in.RequestID, HospitalID: in.HospitalID, MasterID: in.MasterID, MasterRevisionID: in.MasterRevisionID, DepartmentID: in.DepartmentID, DepartmentRevisionID: in.DepartmentRevisionID, MatchesCurrent: matches}
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			if err := audit(tx, tenant.String(), "system:workforce-department-link", "department_link_observed", "organization_department_observations", row.ID); err != nil {
				return err
			}
		}
		// Re-publish the retained answer so a pending requester can repair lost replies.
		raw, err := json.Marshal(organizationcontract.DepartmentLinkObservedData{RequestID: row.ID, HospitalID: row.HospitalID, MasterID: row.MasterID, MasterRevisionID: row.MasterRevisionID, DepartmentID: row.DepartmentID, DepartmentRevisionID: row.DepartmentRevisionID, MatchesCurrent: &row.MatchesCurrent, ObservedAt: row.CreatedAt})
		if err != nil {
			return err
		}
		return bus.Publish(tx, organizationcontract.SubjectDepartmentLinkObserved, events.Event{Type: organizationcontract.SubjectDepartmentLinkObserved, Version: 1, TenantID: tenant.String(), Data: raw})
	}}
}

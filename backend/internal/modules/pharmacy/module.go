// Package pharmacy owns medications and dispense tasks. It consumes
// medicore's visit_created to open a pending dispense per visit and
// publishes dispense_recorded when the pharmacist dispenses.
package pharmacy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/internal/platform/respond"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
)

const (
	// SubjectVisitCreated is medicore's subject, repeated by value —
	// modules must not import each other (spec D6 / phase 1).
	subjectVisitCreated     = "hms.in.medicore.visit_created.v1"
	SubjectDispenseRecorded = "hms.in.pharmacy.dispense_recorded.v1"
)

const (
	PermDispenseRead    authz.Permission = "pharmacy.dispense.read"
	PermDispenseFulfil  authz.Permission = "pharmacy.dispense.fulfil"
	PermMedicationRead  authz.Permission = "pharmacy.medication.read"
	PermMedicationWrite authz.Permission = "pharmacy.medication.write"
)

type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Name() string { return "pharmacy" }

func (m *Module) Permissions() []authz.Grant {
	return []authz.Grant{
		{Permission: PermDispenseRead, Roles: []authz.Role{authz.RolePharmacist, authz.RoleDoctor}},
		{Permission: PermDispenseFulfil, Roles: []authz.Role{authz.RolePharmacist}},
		{Permission: PermMedicationRead, Roles: []authz.Role{authz.RolePharmacist}},
		{Permission: PermMedicationWrite, Roles: []authz.Role{authz.RolePharmacist}},
	}
}

func (m *Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{
		ID: "0001_pharmacy",
		SQL: `
			CREATE TABLE pharmacy_medications (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  name text NOT NULL,
			  strength text NOT NULL DEFAULT '',
			  created_at timestamptz NOT NULL DEFAULT now()
			);
			ALTER TABLE pharmacy_medications ENABLE ROW LEVEL SECURITY;
			ALTER TABLE pharmacy_medications FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON pharmacy_medications
			  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE INDEX ON pharmacy_medications (tenant_id, created_at DESC);

			CREATE TABLE pharmacy_dispenses (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  visit_id uuid NOT NULL,
			  patient_name text NOT NULL,
			  medication text NOT NULL DEFAULT '',
			  status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','dispensed')),
			  dispensed_at timestamptz,
			  created_at timestamptz NOT NULL DEFAULT now()
			);
			ALTER TABLE pharmacy_dispenses ENABLE ROW LEVEL SECURITY;
			ALTER TABLE pharmacy_dispenses FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON pharmacy_dispenses
			  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE INDEX ON pharmacy_dispenses (tenant_id, created_at DESC);`,
	}}
}

type medication struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	TenantID  uuid.UUID `json:"-"`
	Name      string    `json:"name"`
	Strength  string    `json:"strength"`
	CreatedAt time.Time `json:"created_at"`
}

func (medication) TableName() string { return "pharmacy_medications" }

type dispense struct {
	ID          uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	TenantID    uuid.UUID  `json:"-"`
	VisitID     uuid.UUID  `json:"visit_id"`
	PatientName string     `json:"patient_name"`
	Medication  string     `json:"medication"`
	Status      string     `gorm:"default:pending" json:"status"`
	DispensedAt *time.Time `json:"dispensed_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (dispense) TableName() string { return "pharmacy_dispenses" }

type createMedicationRequest struct {
	Name     string `json:"name" binding:"required,max=200"`
	Strength string `json:"strength" binding:"max=100"`
}

type dispenseRequest struct {
	Medication string `json:"medication" binding:"required,max=200"`
}

type visitCreatedData struct {
	VisitID     string `json:"visit_id"`
	PatientName string `json:"patient_name"`
}

type dispenseRecordedData struct {
	DispenseID string `json:"dispense_id"`
	VisitID    string `json:"visit_id"`
}

func (m *Module) Routes(r *platform.Router, deps platform.Deps) {
	g := r.Group("/pharmacy")

	g.POST("/medications", PermMedicationWrite, func(c *gin.Context) {
		p, tenantUUID, ok := authn.TenantPrincipal(c)
		if !ok {
			return
		}
		var req createMedicationRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respond.BadRequest(c, err)
			return
		}
		row := medication{TenantID: tenantUUID, Name: req.Name, Strength: req.Strength}
		err := deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			return tx.Create(&row).Error
		})
		if err != nil {
			respond.Internal(c, "could not create medication")
			return
		}
		respond.Created(c, gin.H{"id": row.ID.String()})
	})

	g.GET("/medications", PermMedicationRead, func(c *gin.Context) {
		p, _, ok := authn.TenantPrincipal(c)
		if !ok {
			return
		}
		var rows []medication
		err := deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			return tx.Order("created_at DESC").Limit(100).Find(&rows).Error
		})
		if err != nil {
			respond.Internal(c, "could not list medications")
			return
		}
		respond.OK(c, gin.H{"data": rows})
	})

	g.GET("/dispenses", PermDispenseRead, func(c *gin.Context) {
		p, _, ok := authn.TenantPrincipal(c)
		if !ok {
			return
		}
		var rows []dispense
		err := deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			return tx.Order("created_at DESC").Limit(100).Find(&rows).Error
		})
		if err != nil {
			respond.Internal(c, "could not list dispenses")
			return
		}
		respond.OK(c, gin.H{"data": rows})
	})

	g.POST("/dispenses/:id/dispense", PermDispenseFulfil, func(c *gin.Context) {
		p, _, ok := authn.TenantPrincipal(c)
		if !ok {
			return
		}
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			respond.NotFound(c, "dispense")
			return
		}
		var req dispenseRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respond.BadRequest(c, err)
			return
		}
		var status int
		err = deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			var row dispense
			if err := tx.First(&row, "id = ?", id).Error; err != nil {
				return err
			}
			if row.Status != "pending" {
				status = http.StatusConflict
				return nil
			}
			now := time.Now().UTC()
			result := tx.Model(&dispense{}).Where("id = ? AND status = 'pending'", id).
				Updates(map[string]any{"status": "dispensed", "medication": req.Medication, "dispensed_at": now})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				// Lost the race to a concurrent dispense between the
				// pre-check above and this guarded UPDATE.
				status = http.StatusConflict
				return nil
			}
			data, err := json.Marshal(dispenseRecordedData{DispenseID: row.ID.String(), VisitID: row.VisitID.String()})
			if err != nil {
				return err
			}
			return deps.Bus.Publish(tx, SubjectDispenseRecorded, events.Event{
				Type: "DispenseRecorded", Version: 1, TenantID: p.TenantID, Data: data,
			})
		})
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respond.NotFound(c, "dispense")
			return
		}
		if err != nil {
			respond.Internal(c, "could not dispense")
			return
		}
		if status == http.StatusConflict {
			respond.Conflict(c, "already dispensed")
			return
		}
		respond.OK(c, gin.H{"id": id.String(), "status": "dispensed"})
	})
}

func (m *Module) Consumers(deps platform.Deps) []events.Consumer {
	return []events.Consumer{{
		Name:    "pharmacy-visit-intake",
		Subject: subjectVisitCreated,
		Handle: func(ctx context.Context, tx *gorm.DB, evt events.Event) error {
			var d visitCreatedData
			if err := json.Unmarshal(evt.Data, &d); err != nil {
				return err
			}
			// The bus scoped this tx to evt.TenantID (Task 1), so this
			// RLS-forced insert lands under the visit's tenant.
			return tx.Exec(`INSERT INTO pharmacy_dispenses (tenant_id, visit_id, patient_name)
				VALUES (?, ?, ?)`, evt.TenantID, d.VisitID, d.PatientName).Error
		},
	}}
}

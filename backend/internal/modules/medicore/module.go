// Package medicore owns clinical visits (OPD/IPD). Creating a visit
// publishes visit_created, which pharmacy and lab consume to open
// pending work — the phase 2 cross-zone journey.
package medicore

import (
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/internal/platform/respond"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
)

const SubjectVisitCreated = "hms.in.medicore.visit_created.v1"

type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Name() string { return "medicore" }

func (m *Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{
		ID: "0001_medicore",
		SQL: `
			CREATE TABLE medicore_visits (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  patient_name text NOT NULL,
			  department text NOT NULL CHECK (department IN ('OPD','IPD')),
			  status text NOT NULL DEFAULT 'open',
			  created_at timestamptz NOT NULL DEFAULT now()
			);
			ALTER TABLE medicore_visits ENABLE ROW LEVEL SECURITY;
			ALTER TABLE medicore_visits FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON medicore_visits
			  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE INDEX ON medicore_visits (tenant_id, created_at DESC);`,
	}}
}

type visit struct {
	ID          uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	TenantID    uuid.UUID `json:"-"`
	PatientName string    `json:"patient_name"`
	Department  string    `json:"department"`
	Status      string    `gorm:"default:open" json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

func (visit) TableName() string { return "medicore_visits" }

type createVisitRequest struct {
	PatientName string `json:"patient_name" binding:"required,max=200"`
	Department  string `json:"department" binding:"required,oneof=OPD IPD"`
}

// VisitCreatedData is the v1 payload of visit_created.
type VisitCreatedData struct {
	VisitID     string `json:"visit_id"`
	PatientName string `json:"patient_name"`
	Department  string `json:"department"`
}

func (m *Module) Routes(r *gin.RouterGroup, deps platform.Deps) {
	g := r.Group("/medicore")

	g.POST("/visits", func(c *gin.Context) {
		p, tenantUUID, ok := authn.TenantPrincipal(c)
		if !ok {
			return
		}
		var req createVisitRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respond.BadRequest(c, err)
			return
		}
		row := visit{TenantID: tenantUUID, PatientName: req.PatientName, Department: req.Department}
		err := deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			data, err := json.Marshal(VisitCreatedData{
				VisitID: row.ID.String(), PatientName: row.PatientName, Department: row.Department,
			})
			if err != nil {
				return err
			}
			return deps.Bus.Publish(tx, SubjectVisitCreated, events.Event{
				Type: "VisitCreated", Version: 1, TenantID: p.TenantID, Data: data,
			})
		})
		if err != nil {
			respond.Internal(c, "could not create visit")
			return
		}
		respond.Accepted(c, gin.H{"id": row.ID.String()})
	})

	g.GET("/visits", func(c *gin.Context) {
		p, _, ok := authn.TenantPrincipal(c)
		if !ok {
			return
		}
		var rows []visit
		err := deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			return tx.Order("created_at DESC").Limit(100).Find(&rows).Error
		})
		if err != nil {
			respond.Internal(c, "could not list visits")
			return
		}
		respond.OK(c, gin.H{"data": rows})
	})
}

func (m *Module) Consumers(deps platform.Deps) []events.Consumer { return nil }

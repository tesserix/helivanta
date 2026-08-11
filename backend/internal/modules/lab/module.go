// Package lab owns test orders and results. It consumes medicore's
// visit_created to open a pending order and publishes result_ready
// when a result is recorded.
package lab

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
	subjectVisitCreated = "hms.in.medicore.visit_created.v1"
	SubjectResultReady  = "hms.in.lab.result_ready.v1"
)

const (
	PermOrderRead   authz.Permission = "lab.order.read"
	PermOrderFulfil authz.Permission = "lab.order.fulfil"
)

type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Name() string { return "lab" }

func (m *Module) Permissions() []authz.Grant {
	return []authz.Grant{
		{Permission: PermOrderRead, Roles: []authz.Role{authz.RoleLabTech, authz.RoleDoctor}},
		{Permission: PermOrderFulfil, Roles: []authz.Role{authz.RoleLabTech}},
	}
}

func (m *Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{
		ID: "0001_lab",
		SQL: `
			CREATE TABLE lab_orders (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  visit_id uuid NOT NULL,
			  patient_name text NOT NULL,
			  test_name text NOT NULL DEFAULT 'CBC',
			  status text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','completed')),
			  result_value text,
			  resulted_at timestamptz,
			  created_at timestamptz NOT NULL DEFAULT now()
			);
			ALTER TABLE lab_orders ENABLE ROW LEVEL SECURITY;
			ALTER TABLE lab_orders FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON lab_orders
			  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE INDEX ON lab_orders (tenant_id, created_at DESC);`,
	}}
}

type order struct {
	ID          uuid.UUID  `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	TenantID    uuid.UUID  `json:"-"`
	VisitID     uuid.UUID  `json:"visit_id"`
	PatientName string     `json:"patient_name"`
	TestName    string     `gorm:"default:CBC" json:"test_name"`
	Status      string     `gorm:"default:pending" json:"status"`
	ResultValue *string    `json:"result_value"`
	ResultedAt  *time.Time `json:"resulted_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (order) TableName() string { return "lab_orders" }

type resultRequest struct {
	ResultValue string `json:"result_value" binding:"required,max=500"`
}

type visitCreatedData struct {
	VisitID     string `json:"visit_id"`
	PatientName string `json:"patient_name"`
}

type resultReadyData struct {
	OrderID string `json:"order_id"`
	VisitID string `json:"visit_id"`
}

func (m *Module) Routes(r *platform.Router, deps platform.Deps) {
	g := r.Group("/lab")

	g.GET("/orders", PermOrderRead, func(c *gin.Context) {
		p, _, ok := authn.TenantPrincipal(c)
		if !ok {
			return
		}
		var rows []order
		err := deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			return tx.Order("created_at DESC").Limit(100).Find(&rows).Error
		})
		if err != nil {
			respond.Internal(c, "could not list orders")
			return
		}
		respond.OK(c, gin.H{"data": rows})
	})

	g.POST("/orders/:id/result", PermOrderFulfil, func(c *gin.Context) {
		p, _, ok := authn.TenantPrincipal(c)
		if !ok {
			return
		}
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			respond.NotFound(c, "order")
			return
		}
		var req resultRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respond.BadRequest(c, err)
			return
		}
		var status int
		err = deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			var row order
			if err := tx.First(&row, "id = ?", id).Error; err != nil {
				return err
			}
			if row.Status != "pending" {
				status = http.StatusConflict
				return nil
			}
			now := time.Now().UTC()
			result := tx.Model(&order{}).Where("id = ? AND status = 'pending'", id).
				Updates(map[string]any{"status": "completed", "result_value": req.ResultValue, "resulted_at": now})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				// Lost the race to a concurrent result submission between
				// the pre-check above and this guarded UPDATE.
				status = http.StatusConflict
				return nil
			}
			data, err := json.Marshal(resultReadyData{OrderID: row.ID.String(), VisitID: row.VisitID.String()})
			if err != nil {
				return err
			}
			return deps.Bus.Publish(tx, SubjectResultReady, events.Event{
				Type: "ResultReady", Version: 1, TenantID: p.TenantID, Data: data,
			})
		})
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respond.NotFound(c, "order")
			return
		}
		if err != nil {
			respond.Internal(c, "could not record result")
			return
		}
		if status == http.StatusConflict {
			respond.Conflict(c, "result already recorded")
			return
		}
		respond.OK(c, gin.H{"id": id.String(), "status": "completed"})
	})
}

func (m *Module) Consumers(deps platform.Deps) []events.Consumer {
	return []events.Consumer{{
		Name:    "lab-visit-intake",
		Subject: subjectVisitCreated,
		Handle: func(ctx context.Context, tx *gorm.DB, evt events.Event) error {
			var d visitCreatedData
			if err := json.Unmarshal(evt.Data, &d); err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO lab_orders (tenant_id, visit_id, patient_name)
				VALUES (?, ?, ?)`, evt.TenantID, d.VisitID, d.PatientName).Error
		},
	}}
}

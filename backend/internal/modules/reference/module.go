// Package reference is the trivial module proving the platform wiring:
// authn → tenantdb (RLS) → outbox → JetStream → consumer (issue #2).
package reference

import (
	"context"
	"encoding/json"
	"errors"
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

const SubjectPinged = "hms.in.reference.pinged.v1"

type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Name() string { return "reference" }

// Permissions is empty: reference exposes no tenant data, so every
// route is deliberately public (authz.Public).
func (m *Module) Permissions() []authz.Grant { return nil }

func (m *Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{
		ID: "0001_reference",
		SQL: `
			CREATE TABLE reference_pings (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  message text NOT NULL,
			  created_at timestamptz NOT NULL DEFAULT now()
			);
			ALTER TABLE reference_pings ENABLE ROW LEVEL SECURITY;
			ALTER TABLE reference_pings FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON reference_pings
			  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE INDEX ON reference_pings (tenant_id, created_at DESC);

			CREATE TABLE reference_ping_receipts (
			  event_id uuid PRIMARY KEY,
			  ping_id uuid NOT NULL,
			  processed_at timestamptz NOT NULL DEFAULT now()
			);`,
	}}
}

type ping struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	TenantID  uuid.UUID `json:"-"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

func (ping) TableName() string { return "reference_pings" }

type pingRequest struct {
	Message string `json:"message" binding:"required,max=500"`
}

type pingedData struct {
	PingID string `json:"ping_id"`
}

func (m *Module) Routes(r *platform.Router, deps platform.Deps) {
	g := r.Group("/reference")

	g.POST("/ping", authz.Public, func(c *gin.Context) {
		p, tenantUUID, ok := authn.TenantPrincipal(c)
		if !ok {
			return
		}
		var req pingRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respond.BadRequest(c, err)
			return
		}
		row := ping{TenantID: tenantUUID, Message: req.Message}
		err := deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			if err := tx.Create(&row).Error; err != nil {
				return err
			}
			data, err := json.Marshal(pingedData{PingID: row.ID.String()})
			if err != nil {
				return err
			}
			return deps.Bus.Publish(tx, SubjectPinged, events.Event{
				Type: "ReferencePinged", Version: 1, TenantID: p.TenantID, Data: data,
			})
		})
		if err != nil {
			respond.Internal(c, "could not record ping")
			return
		}
		respond.Accepted(c, gin.H{"id": row.ID.String()})
	})

	g.GET("/pings", authz.Public, func(c *gin.Context) {
		p, _, ok := authn.TenantPrincipal(c)
		if !ok {
			return
		}
		var rows []ping
		err := deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			return tx.Order("created_at DESC").Limit(100).Find(&rows).Error
		})
		if err != nil {
			respond.Internal(c, "could not list pings")
			return
		}
		respond.OK(c, gin.H{"data": rows})
	})

	g.GET("/pings/:id", authz.Public, func(c *gin.Context) {
		p, _, ok := authn.TenantPrincipal(c)
		if !ok {
			return
		}
		id, err := uuid.Parse(c.Param("id"))
		if err != nil {
			respond.NotFound(c, "ping")
			return
		}
		var row ping
		err = deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			return tx.First(&row, "id = ?", id).Error
		})
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// RLS filters cross-tenant rows → identical 404 (issue #2).
			respond.NotFound(c, "ping")
			return
		}
		if err != nil {
			respond.Internal(c, "could not load ping")
			return
		}
		respond.OK(c, row)
	})
}

func (m *Module) Consumers(deps platform.Deps) []events.Consumer {
	return []events.Consumer{{
		Name:    "reference-receipts",
		Subject: SubjectPinged,
		Handle: func(ctx_ context.Context, tx *gorm.DB, evt events.Event) error {
			var d pingedData
			if err := json.Unmarshal(evt.Data, &d); err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO reference_ping_receipts (event_id, ping_id) VALUES (?, ?)
				ON CONFLICT DO NOTHING`, evt.ID, d.PingID).Error
		},
	}}
}

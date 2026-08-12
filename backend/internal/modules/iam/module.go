// Package iam owns tenant membership and roles. Postgres is the system
// of record; OpenFGA is the decision point, and is fully rebuildable
// from these tables by the reconciler.
package iam

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/internal/platform/respond"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
)

const PermMemberManage authz.Permission = "iam.member.manage"

const (
	SubjectMemberGranted = "hms.in.iam.member_granted.v1"
	SubjectMemberRevoked = "hms.in.iam.member_revoked.v1"
)

type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Name() string { return "iam" }

// Permissions declares only iam.member.manage. It lists no roles because
// tenant_admin is implicit — the reconciler grants it every declared
// permission — and no other system role may manage membership.
func (m *Module) Permissions() []authz.Grant {
	return []authz.Grant{{Permission: PermMemberManage}}
}

func (m *Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{
		ID: "0001_iam",
		SQL: `
			CREATE TABLE iam_roles (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  key text NOT NULL,
			  label text NOT NULL,
			  is_system boolean NOT NULL DEFAULT false,
			  created_at timestamptz NOT NULL DEFAULT now(),
			  UNIQUE (tenant_id, key)
			);
			ALTER TABLE iam_roles ENABLE ROW LEVEL SECURITY;
			ALTER TABLE iam_roles FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON iam_roles
			  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);

			CREATE TABLE iam_members (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  subject text NOT NULL,
			  role_key text NOT NULL,
			  created_at timestamptz NOT NULL DEFAULT now(),
			  UNIQUE (tenant_id, subject, role_key)
			);
			ALTER TABLE iam_members ENABLE ROW LEVEL SECURITY;
			ALTER TABLE iam_members FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON iam_members
			  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE INDEX ON iam_members (tenant_id, subject);
			CREATE INDEX ON iam_members (subject);`,
	}, {
		ID: "0002_iam",
		// USING moves to the shared predicate so group-tenant
		// visibility can later be enabled in one place. WITH CHECK
		// deliberately stays pinned to strict equality: reads may
		// widen, writes must always land in exactly one tenant.
		SQL: `
			ALTER POLICY tenant_isolation ON iam_roles
			  USING (hms_tenant_visible(tenant_id))
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			ALTER POLICY tenant_isolation ON iam_members
			  USING (hms_tenant_visible(tenant_id))
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);`,
	}}
}

type role struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	TenantID  uuid.UUID `json:"-"`
	Key       string    `json:"key"`
	Label     string    `json:"label"`
	IsSystem  bool      `json:"is_system"`
	CreatedAt time.Time `json:"created_at"`
}

func (role) TableName() string { return "iam_roles" }

type member struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	TenantID  uuid.UUID `json:"-"`
	Subject   string    `json:"subject"`
	RoleKey   string    `json:"role_key"`
	CreatedAt time.Time `json:"created_at"`
}

func (member) TableName() string { return "iam_members" }

// MemberChangedData is the v1 payload of member_granted and
// member_revoked.
type MemberChangedData struct {
	Subject string `json:"subject"`
	RoleKey string `json:"role_key"`
}

type grantRequest struct {
	Subject string `json:"subject" binding:"required,max=200"`
	RoleKey string `json:"role_key" binding:"required,max=100"`
}

// knownRole reports whether key is a system role. Custom roles are
// supported by the model but not yet creatable, so anything else is a
// client error rather than a silently-dead grant. Delegates to
// authz.KnownRole, the single source of truth for the system role list,
// so this HTTP-path check and Reconcile's raw-SQL-path check
// (internal/platform/reconcile.go) can never drift apart.
func knownRole(key string) bool {
	return authz.KnownRole(authz.Role(key))
}

func (m *Module) Routes(r *platform.Router, deps platform.Deps) {
	g := r.Group("/iam")

	g.POST("/members", PermMemberManage, func(c *gin.Context) {
		p, tenantUUID, ok := authn.TenantPrincipal(c)
		if !ok {
			return
		}
		var req grantRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respond.BadRequest(c, err)
			return
		}
		if !knownRole(req.RoleKey) {
			respond.BadRequest(c, fmt.Errorf("unknown role %q", req.RoleKey))
			return
		}
		row := member{TenantID: tenantUUID, Subject: req.Subject, RoleKey: req.RoleKey}
		err := deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			// Re-granting an existing role is a no-op at the row level,
			// not a conflict: the caller's intent is already satisfied.
			// An explicit ON CONFLICT DO NOTHING (rather than GORM's
			// FirstOrCreate, which has sharp edges around which fields
			// populate the created row) both sets tenant_id on the
			// inserted row and makes "already granted" unambiguous:
			// row.ID stays uuid.Nil because Postgres returns no row for
			// a skipped insert.
			if err := tx.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "subject"}, {Name: "role_key"}},
				DoNothing: true,
			}).Create(&row).Error; err != nil {
				return err
			}
			if row.ID == uuid.Nil {
				// Already granted at the row level, but the outbox
				// event still publishes unconditionally below (fetch
				// the existing row first so the response carries its
				// real id). Re-granting is a request-time repair
				// mechanism for FGA drift, not the only one:
				// platform.Reconcile (internal/platform/reconcile.go)
				// re-applies every iam_members row's role tuple at
				// boot, so a lost grant event also self-heals the next
				// time any replica boots. This re-grant path stays
				// useful because it repairs immediately, on request,
				// without waiting for a boot. Tuple writes are
				// idempotent, so the redundant write on a normal
				// re-grant is harmless.
				if err := tx.Where("subject = ? AND role_key = ?", req.Subject, req.RoleKey).First(&row).Error; err != nil {
					return err
				}
			}
			data, err := json.Marshal(MemberChangedData(req))
			if err != nil {
				return err
			}
			return deps.Bus.Publish(tx, SubjectMemberGranted, events.Event{
				Type: "MemberGranted", Version: 1, TenantID: p.TenantID, Data: data,
			})
		})
		if err != nil {
			respond.Internal(c, "could not grant role")
			return
		}
		respond.Accepted(c, gin.H{"id": row.ID.String()})
	})

	g.DELETE("/members/:subject/:role", PermMemberManage, func(c *gin.Context) {
		p, _, ok := authn.TenantPrincipal(c)
		if !ok {
			return
		}
		subject, roleKey := c.Param("subject"), c.Param("role")
		err := deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			if err := tx.Where("subject = ? AND role_key = ?", subject, roleKey).
				Delete(&member{}).Error; err != nil {
				return err
			}
			// Publishes unconditionally, even if no row matched, for the
			// same repair-mechanism reason as the grant path above: a
			// re-issued revoke must still be able to clear a stuck FGA
			// tuple.
			data, err := json.Marshal(MemberChangedData{Subject: subject, RoleKey: roleKey})
			if err != nil {
				return err
			}
			return deps.Bus.Publish(tx, SubjectMemberRevoked, events.Event{
				Type: "MemberRevoked", Version: 1, TenantID: p.TenantID, Data: data,
			})
		})
		if err != nil {
			respond.Internal(c, "could not revoke role")
			return
		}
		respond.Accepted(c, gin.H{"subject": subject, "role_key": roleKey})
	})

	g.GET("/members", PermMemberManage, func(c *gin.Context) {
		p, _, ok := authn.TenantPrincipal(c)
		if !ok {
			return
		}
		var rows []member
		err := deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			return tx.Order("created_at DESC").Limit(500).Find(&rows).Error
		})
		if err != nil {
			respond.Internal(c, "could not list members")
			return
		}
		respond.OK(c, gin.H{"data": rows})
	})

	g.GET("/roles", PermMemberManage, func(c *gin.Context) {
		respond.OK(c, gin.H{"data": SystemRoles()})
	})

	m.registerMe(g, deps)
}

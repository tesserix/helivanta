// Package iam owns tenant membership and roles. Postgres is the system
// of record; OpenFGA is the decision point, and is fully rebuildable
// from these tables by the reconciler.
package iam

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

// PermCredentialRevoke gates POST /subjects/:subject/revoke. It lists no
// system roles, so tenant_admin is the only role that holds it — the
// reconciler's implicit grant to tenant_admin, and nothing else. Revoking
// a person's credentials platform-wide (see revocationHandlers.adminRevoke
// in signout.go) is not an authority any clinical role should carry.
const PermCredentialRevoke authz.Permission = "iam.credential.revoke" //nolint:gosec // permission name, not a credential value

const (
	SubjectMemberGranted = "hms.in.iam.member_granted.v1"
	SubjectMemberRevoked = "hms.in.iam.member_revoked.v1"
)

// Module owns tenant membership and the credential-revocation watermark.
//
// checker is the same *RevocationChecker instance the authentication
// middleware (pkg/authn.Middleware) consults on every request. It MUST be
// constructed once by the caller (cmd/api/main.go, via
// internal/bootstrap) and threaded into both New and the middleware — two
// separately constructed checkers would compile and pass most tests while
// the module's sign-out/revoke handlers silently invalidate a cache
// nothing on the request path ever reads (#781).
type Module struct {
	checker *RevocationChecker
}

func New(checker *RevocationChecker) *Module { return &Module{checker: checker} }

func (m *Module) Name() string { return "iam" }

// CheckerForTest exposes the module's revocation checker for tests that
// need to assert it is the SAME instance the caller constructed and
// handed to authn.Middleware — see TestNewRegistryGivesIAMTheExactCheckerPassedIn
// in internal/bootstrap.
func (m *Module) CheckerForTest() *RevocationChecker { return m.checker }

// Permissions declares iam.member.manage and iam.credential.revoke.
// Neither lists a role: tenant_admin is implicit, and revoking a
// person's credentials platform-wide is not an authority any clinical
// role should carry.
func (m *Module) Permissions() []authz.Grant {
	return []authz.Grant{
		{Permission: PermMemberManage},
		{Permission: PermCredentialRevoke},
	}
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
	}, {
		ID: "0003_iam",
		// Not tenant-scoped, and therefore an explicit LintRLS allowlist
		// entry rather than a table that quietly has no tenant_id: a GIP
		// subject is global, so a revocation is global. It holds no
		// tenant data and no PHI.
		//
		// The watermark only ever moves forward (see the GREATEST upsert
		// in revocation.go): a retried or late write must never be able
		// to resurrect a revoked credential.
		SQL: `
			CREATE TABLE iam_credential_revocations (
			  subject    text PRIMARY KEY,
			  revoked_at timestamptz NOT NULL,
			  reason     text NOT NULL CHECK (reason IN ('sign_out','admin_revoke')),
			  actor      text NOT NULL,
			  updated_at timestamptz NOT NULL DEFAULT now()
			);`,
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

type memberHandlers struct {
	db  *tenantdb.DB
	bus *events.Bus
}

// grant adds a role to a subject, or repairs a stuck FGA tuple by
// re-publishing member_granted for a grant that already exists at the
// row level.
func (h *memberHandlers) grant(c *gin.Context) {
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
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
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
		return h.bus.Publish(tx, SubjectMemberGranted, events.Event{
			Type: "MemberGranted", Version: 1, TenantID: p.TenantID, Data: data,
		})
	})
	if err != nil {
		respond.InternalErr(c, err, "could not grant role")
		return
	}
	respond.Accepted(c, gin.H{"id": row.ID.String()})
}

// revoke removes a subject's role, publishing member_revoked
// unconditionally so a re-issued revoke can still clear a stuck FGA
// tuple even when no row matched.
func (h *memberHandlers) revoke(c *gin.Context) {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	subject, roleKey := c.Param("subject"), c.Param("role")
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
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
		return h.bus.Publish(tx, SubjectMemberRevoked, events.Event{
			Type: "MemberRevoked", Version: 1, TenantID: p.TenantID, Data: data,
		})
	})
	if err != nil {
		respond.InternalErr(c, err, "could not revoke role")
		return
	}
	respond.Accepted(c, gin.H{"subject": subject, "role_key": roleKey})
}

// list returns this tenant's members, newest first.
func (h *memberHandlers) list(c *gin.Context) {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	var rows []member
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		return tx.Order("created_at DESC").Limit(500).Find(&rows).Error
	})
	if err != nil {
		respond.InternalErr(c, err, "could not list members")
		return
	}
	respond.OK(c, gin.H{"data": rows})
}

func (m *Module) Routes(r *platform.Router, deps platform.Deps) {
	g := r.Group("/iam")
	members := &memberHandlers{db: deps.DB, bus: deps.Bus}

	g.POST("/members", PermMemberManage, members.grant)
	g.DELETE("/members/:subject/:role", PermMemberManage, members.revoke)
	g.GET("/members", PermMemberManage, members.list)
	g.GET("/roles", PermMemberManage, listRoles)

	m.registerMe(g, deps)

	rev := &revocationHandlers{db: deps.DB, bus: deps.Bus, checker: m.checker, revoker: deps.TokenRevoker, roles: deps.Roles}
	// NoTenantMembership: a member whose membership was just revoked must
	// still be able to end their own session (#781). adminRevoke is the
	// opposite — it requires both PermCredentialRevoke (membership
	// examined as usual) and, inside the handler, that the target is a
	// member of the ACTING admin's own tenant.
	g.POST("/me/sign-out", authz.NoTenantMembership, rev.signOut)
	g.POST("/subjects/:subject/revoke", PermCredentialRevoke, rev.adminRevoke)
}

// Broadcasts invalidates this replica's revocation cache the instant
// another replica revokes a credential (#781). The durable truth is
// Postgres — see RevocationChecker.RevokedAfter's read-through and its
// 5-minute TTL backstop — so a dropped or duplicated broadcast degrades
// to that TTL, never to incorrectness.
func (m *Module) Broadcasts(platform.Deps) []events.Broadcast {
	return []events.Broadcast{{
		Subject: SubjectCredentialRevoked,
		Handle: func(ctx context.Context, evt events.Event) {
			var data CredentialRevokedData
			if err := json.Unmarshal(evt.Data, &data); err != nil {
				slog.ErrorContext(ctx, "credential_revoked: undecodable payload", "err", err)
				return
			}
			m.checker.Invalidate(data.Subject)
		},
	}}
}

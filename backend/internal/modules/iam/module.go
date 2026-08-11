// Package iam owns tenant membership and roles. Postgres is the system
// of record; OpenFGA is the decision point, and is fully rebuildable
// from these tables by the reconciler.
package iam

import (
	"time"

	"github.com/google/uuid"

	"github.com/tesserix/hms/internal/platform"
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

// Routes and Consumers are filled in by the next tasks.
func (m *Module) Routes(r *platform.Router, deps platform.Deps) {}

func (m *Module) Consumers(deps platform.Deps) []events.Consumer { return nil }

// Package medicore owns clinical visits (OPD/IPD). Creating a visit
// publishes visit_created, which pharmacy and lab consume to open
// pending work — the phase 2 cross-zone journey.
package medicore

import (
	medicorecontract "github.com/tesserix/hms/internal/modules/medicore/contract"
	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
)

const (
	PermVisitCreate authz.Permission = "medicore.visit.create"
	PermVisitRead   authz.Permission = "medicore.visit.read"
	PermVisitUpdate authz.Permission = "medicore.visit.update"
)

type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Name() string { return "medicore" }

func (m *Module) Permissions() []authz.Grant {
	return []authz.Grant{
		{Permission: PermVisitCreate, Roles: []authz.Role{authz.RoleDoctor}},
		{Permission: PermVisitRead, Roles: []authz.Role{authz.RoleDoctor, authz.RoleNurse}},
		{Permission: PermVisitUpdate, Roles: []authz.Role{authz.RoleNurse}},
	}
}

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
	}, {
		ID: "0002_medicore",
		// USING moves to the shared predicate so group-tenant
		// visibility can later be enabled in one place. WITH CHECK
		// deliberately stays pinned to strict equality: reads may
		// widen, writes must always land in exactly one tenant.
		SQL: `
			ALTER POLICY tenant_isolation ON medicore_visits
			  USING (hms_tenant_visible(tenant_id))
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);`,
	}}
}

func (m *Module) Routes(r *platform.Router, deps platform.Deps) {
	g := r.Group("/medicore")
	visits := &visitHandlers{db: deps.DB, bus: deps.Bus}

	g.POST("/visits", PermVisitCreate, visits.create)
	platform.ListRoute(g, "/visits", PermVisitRead, visits.list)
}

// Publishes declares visit_created, the one event medicore emits.
// pharmacy and lab each consume it via medicorecontract.
func (m *Module) Publishes() []string {
	return []string{medicorecontract.SubjectVisitCreated}
}

func (m *Module) Consumers(deps platform.Deps) []events.Consumer { return nil }

// Broadcasts declares none: medicore has no per-replica cache to
// invalidate.
func (m *Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }

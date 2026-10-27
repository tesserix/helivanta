// Package medicore owns clinical visits (OPD/IPD). Creating a visit
// publishes visit_created, which pharmacy and lab consume to open
// pending work — the phase 2 cross-zone journey.
package medicore

import (
	_ "embed"

	medicorecontract "github.com/tesserix/helivanta/internal/modules/medicore/contract"
	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/insights"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const (
	PermVisitCreate authz.Permission = "medicore.visit.create"
	PermVisitRead   authz.Permission = "medicore.visit.read"
	PermVisitUpdate authz.Permission = "medicore.visit.update"
	// PermAccessManage administers who may see a hospital's encounters
	// (#80 E3); tenant admins only, so no one grants themselves access.
	PermAccessManage authz.Permission = "medicore.access.manage"
)

//go:embed encounters.sql
var encountersSchema string

type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Name() string { return "medicore" }

func (m *Module) Permissions() []authz.Grant {
	return []authz.Grant{
		{Permission: PermVisitCreate, Roles: []authz.Role{authz.RoleDoctor}},
		{Permission: PermVisitRead, Roles: []authz.Role{authz.RoleDoctor, authz.RoleNurse}},
		{Permission: PermVisitUpdate, Roles: []authz.Role{authz.RoleDoctor, authz.RoleNurse}},
		{Permission: PermAccessManage},
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
	}, {
		ID: "0003_medicore",
		// Nullable, not NOT NULL: visits opened before patientmaster
		// exists (or before a caller supplies one) must keep working.
		// This is the transition spec D7 describes, not a hard cutover.
		SQL: `ALTER TABLE medicore_visits ADD COLUMN patient_id uuid;`,
	}, {
		ID: "0004_medicore",
		// Every row change lands in audit_events with its actor (#54).
		SQL: `
			CREATE TRIGGER medicore_visits_audit AFTER INSERT OR UPDATE OR DELETE ON medicore_visits
			  FOR EACH ROW EXECUTE FUNCTION audit_record_change();`,
	}, {
		// OPD encounters, their patient projection and hospital grants (#80).
		ID:  "0005_medicore",
		SQL: encountersSchema,
	}}
}

func (m *Module) Routes(r *platform.Router, deps platform.Deps) {
	g := r.Group("/medicore")
	g.GET("/insights", PermVisitRead, insights.Counts(deps.DB, "medicore_visits", "department", []string{"OPD", "IPD"}))
	visits := &visitHandlers{db: deps.DB, bus: deps.Bus}

	g.POST("/visits", PermVisitCreate, visits.create)
	platform.ListRoute(g, "/visits", PermVisitRead, visits.list)

	encounters := &encounterHandlers{db: deps.DB, bus: deps.Bus}
	g.PUT("/hospitals/:hospital/access/:subject", PermAccessManage, encounters.grantAccess)
	g.DELETE("/hospitals/:hospital/access/:subject", PermAccessManage, encounters.revokeAccess)
	g.GET("/hospitals/:hospital/access", PermVisitRead, encounters.myAccess)
	g.PUT("/hospitals/:hospital/encounters/:id", PermVisitCreate, encounters.open)
	platform.ListRoute(g, "/hospitals/:hospital/encounters", PermVisitRead, encounters.list)
	g.GET("/hospitals/:hospital/encounters/:id", PermVisitRead, encounters.get)
	g.POST("/hospitals/:hospital/encounters/:id/transitions", PermVisitUpdate, encounters.transition)
}

// Publishes declares visit_created, which pharmacy and lab consume, and
// the encounter events (#80 E6).
func (m *Module) Publishes() []string {
	return []string{medicorecontract.SubjectVisitCreated, medicorecontract.SubjectEncounterOpened, medicorecontract.SubjectEncounterStatusChanged}
}

// DirectedSubjects declares none: this module publishes nothing that
// creates data in another tenant.
func (m *Module) DirectedSubjects() []string { return nil }

// DirectedWriteTables declares none: this module writes no table on
// behalf of another tenant.
func (m *Module) DirectedWriteTables() []string { return nil }

// Consumers keeps medicore's record of which hospital each patient is
// registered at (#80 E2).
func (m *Module) Consumers(deps platform.Deps) []events.Consumer {
	return []events.Consumer{{Name: "medicore-patient-registered", Subject: patientmastercontract.SubjectPatientRegistered, Handle: patientRegistered}}
}

// Broadcasts declares none: medicore has no per-replica cache to
// invalidate.
func (m *Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }

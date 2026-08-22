// Package lab owns test orders and results. It consumes medicore's
// visit_created to open a pending order and publishes result_ready
// when a result is recorded.
package lab

import (
	labcontract "github.com/tesserix/helivanta/internal/modules/lab/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
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
	}, {
		ID: "0002_lab",
		// USING moves to the shared predicate so group-tenant
		// visibility can later be enabled in one place. WITH CHECK
		// deliberately stays pinned to strict equality: reads may
		// widen, writes must always land in exactly one tenant.
		SQL: `
			ALTER POLICY tenant_isolation ON lab_orders
			  USING (hms_tenant_visible(tenant_id))
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);`,
	}}
}

func (m *Module) Routes(r *platform.Router, deps platform.Deps) {
	g := r.Group("/lab")
	orders := &orderHandlers{db: deps.DB, bus: deps.Bus}

	platform.ListRoute(g, "/orders", PermOrderRead, orders.list)
	g.POST("/orders/:id/result", PermOrderFulfil, orders.result)
}

// Publishes declares result_ready, the one event lab emits. No module
// consumes it today; declaring it anyway is what makes it available to
// anything added later without changing lab.
func (m *Module) Publishes() []string {
	return []string{labcontract.SubjectResultReady}
}

// DirectedSubjects declares none: this module publishes nothing that
// creates data in another tenant.
func (m *Module) DirectedSubjects() []string { return nil }

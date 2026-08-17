// Package pharmacy owns medications and dispense tasks. It consumes
// medicore's visit_created to open a pending dispense per visit and
// publishes dispense_recorded when the pharmacist dispenses.
package pharmacy

import (
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
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
	}, {
		ID: "0002_pharmacy",
		// USING moves to the shared predicate so group-tenant
		// visibility can later be enabled in one place. WITH CHECK
		// deliberately stays pinned to strict equality: reads may
		// widen, writes must always land in exactly one tenant.
		SQL: `
			ALTER POLICY tenant_isolation ON pharmacy_medications
			  USING (hms_tenant_visible(tenant_id))
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			ALTER POLICY tenant_isolation ON pharmacy_dispenses
			  USING (hms_tenant_visible(tenant_id))
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);`,
	}}
}

func (m *Module) Routes(r *platform.Router, deps platform.Deps) {
	g := r.Group("/pharmacy")
	meds := &medicationHandlers{db: deps.DB}
	disp := &dispenseHandlers{db: deps.DB, bus: deps.Bus}

	g.POST("/medications", PermMedicationWrite, meds.create)
	platform.ListRoute(g, "/medications", PermMedicationRead, meds.list)
	platform.ListRoute(g, "/dispenses", PermDispenseRead, disp.list)
	g.POST("/dispenses/:id/dispense", PermDispenseFulfil, disp.fulfil)
}

// Publishes declares dispense_recorded, the one event pharmacy emits.
// No module consumes it today; declaring it anyway is what makes it
// available to anything added later without changing pharmacy.
func (m *Module) Publishes() []string {
	return []string{pharmacycontract.SubjectDispenseRecorded}
}

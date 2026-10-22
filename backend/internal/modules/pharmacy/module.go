// Package pharmacy owns pharmacy directories, catalogue, stock, operational payables
// and dispense tasks. Clinical integrations use published contracts.
package pharmacy

import (
	_ "embed"
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/insights"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

//go:embed catalogue.sql
var catalogueSchema string

//go:embed inventory.sql
var inventorySchema string

//go:embed payables.sql
var payablesSchema string

//go:embed batch_safety.sql
var batchSafetySchema string

//go:embed tax.sql
var taxSchema string

//go:embed storage_address.sql
var storageAddressSchema string

const (
	PermAccountsRead    authz.Permission = "pharmacy.accounts.read"
	PermAccountsWrite   authz.Permission = "pharmacy.accounts.write"
	PermAccountsPayment authz.Permission = "pharmacy.accounts.payment"
	PermAccountsAdjust  authz.Permission = "pharmacy.accounts.adjust"
	PermStockRead       authz.Permission = "pharmacy.stock.read"
	PermStockReceive    authz.Permission = "pharmacy.stock.receive"
	PermStockCorrect    authz.Permission = "pharmacy.stock.correct"
	PermStockQuarantine authz.Permission = "pharmacy.stock.quarantine"
	PermCatalogueRead   authz.Permission = "pharmacy.catalogue.read"
	PermCatalogueWrite  authz.Permission = "pharmacy.catalogue.write"
	PermAccessManage    authz.Permission = "pharmacy.access.manage"
	// PermTaxManage sets outlet registrations and product rates. It is apart
	// from catalogue.write: a wrong rate taxes every sale of the product.
	PermTaxManage       authz.Permission = "pharmacy.tax.manage"
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
		{Permission: PermAccountsRead, Roles: []authz.Role{authz.RolePharmacyOwner, authz.RolePharmacyPurchasing}},
		{Permission: PermAccountsWrite, Roles: []authz.Role{authz.RolePharmacyOwner, authz.RolePharmacyPurchasing}},
		{Permission: PermAccountsPayment, Roles: []authz.Role{authz.RolePharmacyOwner}},
		{Permission: PermAccountsAdjust, Roles: []authz.Role{authz.RolePharmacyOwner}},
		{Permission: PermStockRead, Roles: []authz.Role{authz.RolePharmacist, authz.RolePharmacyOwner, authz.RolePharmacyPurchasing, authz.RolePharmacyInventory}},
		{Permission: PermStockReceive, Roles: []authz.Role{authz.RolePharmacist, authz.RolePharmacyOwner, authz.RolePharmacyInventory}},
		{Permission: PermStockCorrect, Roles: []authz.Role{authz.RolePharmacyOwner, authz.RolePharmacyInventory}},
		{Permission: PermStockQuarantine, Roles: []authz.Role{authz.RolePharmacist, authz.RolePharmacyOwner, authz.RolePharmacyInventory}},
		{Permission: PermCatalogueRead, Roles: []authz.Role{authz.RolePharmacist, authz.RolePharmacyOwner, authz.RolePharmacyPurchasing, authz.RolePharmacyInventory}},
		{Permission: PermCatalogueWrite, Roles: []authz.Role{authz.RolePharmacist, authz.RolePharmacyOwner, authz.RolePharmacyPurchasing}},
		{Permission: PermAccessManage, Roles: []authz.Role{authz.RolePharmacyOwner}},
		{Permission: PermTaxManage, Roles: []authz.Role{authz.RolePharmacyOwner}},
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
	}, {
		ID: "0003_pharmacy",
		// Every row change lands in audit_events with its actor (#54).
		SQL: `
			CREATE TRIGGER pharmacy_medications_audit AFTER INSERT OR UPDATE OR DELETE ON pharmacy_medications
			  FOR EACH ROW EXECUTE FUNCTION audit_record_change();
			CREATE TRIGGER pharmacy_dispenses_audit AFTER INSERT OR UPDATE OR DELETE ON pharmacy_dispenses
			  FOR EACH ROW EXECUTE FUNCTION audit_record_change();`,
	}, {ID: "0004_pharmacy", SQL: catalogueSchema}, {ID: "0005_pharmacy", SQL: inventorySchema}, {ID: "0006_pharmacy", SQL: payablesSchema}, {ID: "0007_pharmacy", SQL: batchSafetySchema}, {ID: "0008_pharmacy", SQL: storageAddressSchema}, {ID: "0009_pharmacy", SQL: taxSchema}}
}

func (m *Module) Routes(r *platform.Router, deps platform.Deps) {
	g := r.Group("/pharmacy")
	g.GET("/insights", PermDispenseRead, requirePharmacyTenant, insights.Counts(deps.DB, "pharmacy_dispenses", "status", []string{"pending", "dispensed"}))
	registryRoutes(g, deps)
	catalogueRoutes(g, deps)
	inventoryRoutes(g, deps)
	payableRoutes(g, deps)
	meds := &medicationHandlers{db: deps.DB}
	disp := &dispenseHandlers{db: deps.DB, bus: deps.Bus}

	g.POST("/medications", PermMedicationWrite, meds.create)
	platform.ListRoute(g, "/medications", PermMedicationRead, meds.list)
	platform.ListRoute(g, "/dispenses", PermDispenseRead, disp.list)
	g.POST("/dispenses/:id/dispense", PermDispenseFulfil, disp.fulfil)
}

// Publishes declares the module's clinical, catalogue, inventory and financial events.
func (m *Module) Publishes() []string {
	return []string{pharmacycontract.SubjectDispenseRecorded, pharmacycontract.SubjectCatalogueUpdated, pharmacycontract.SubjectStockMovementRecorded, pharmacycontract.SubjectPayableRecorded, pharmacycontract.SubjectSettlementRecorded, pharmacycontract.SubjectBatchSafetyRecorded}
}

// DirectedSubjects declares none: this module publishes nothing that
// creates data in another tenant.
func (m *Module) DirectedSubjects() []string { return nil }

// DirectedWriteTables declares none: this module writes no table on
// behalf of another tenant.
func (m *Module) DirectedWriteTables() []string { return nil }

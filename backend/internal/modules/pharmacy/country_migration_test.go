package pharmacy_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const pakistanMigration = "0011_pharmacy"

// seedLegacyOutlet writes an outlet straight to the database, as data that
// predates a country's removal would be, bypassing the API's country check.
func seedLegacyOutlet(tx *gorm.DB, outlet, country, zone string) error {
	if err := tx.Exec(`INSERT INTO pharmacy_organizations (id,tenant_id,code,name,ownership_type) VALUES (?,?,'TEST-LEGACY','TEST Legacy Pharmacy','independent') ON CONFLICT DO NOTHING`, pharmaOrganization, testutil.TenantA).Error; err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO pharmacy_outlets (id,tenant_id,pharmacy_id,code,name,country_code,timezone) VALUES (?,?,?,?,'TEST Legacy Outlet',?,?)`,
		outlet, testutil.TenantA, pharmaOrganization, "TEST-"+outlet[24:], country, zone).Error
}

func legacyTx(t *testing.T, db *tenantdb.DB, fn func(*gorm.DB) error) {
	t.Helper()
	ctx := tenantdb.WithActor(t.Context(), "user-editor")
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, fn))
}

// #1287 D2: one tenant's Pakistan outlet refuses the migration, which
// changes nothing, though the owner role cannot see tenant rows under
// forced RLS.
func TestPharmacyMigrationRefusesPakistanOutlets(t *testing.T) {
	db, ctx, rest := testutil.MigratedBefore(t, pakistanMigration, pharmacy.New())
	legacyTx(t, db, func(tx *gorm.DB) error { return seedLegacyOutlet(tx, pharmaOutlet, "PK", "Asia/Karachi") })
	err := db.Migrate(ctx, rest)
	require.Error(t, err)
	require.Contains(t, err.Error(), "#1287: Pakistan rows: pharmacy_outlets 1")
	testutil.RequireForcedRLS(t, ctx, db, "pharmacy_outlets", "pharmacy_catalogue_products", "pharmacy_payables", "pharmacy_settlements")
}

// A rupee amount at an Indian outlet is refused too: it is Pakistan data.
func TestPharmacyMigrationRefusesRupeeAmounts(t *testing.T) {
	db, ctx, rest := testutil.MigratedBefore(t, pakistanMigration, pharmacy.New())
	legacyTx(t, db, func(tx *gorm.DB) error {
		if err := seedLegacyOutlet(tx, pharmaOutlet, "IN", "Asia/Kolkata"); err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO pharmacy_outlet_access (id,tenant_id,outlet_id,subject,level) VALUES (?,?,?,'user-editor','editor')`, uuid.Must(uuid.NewV7()).String(), testutil.TenantA, pharmaOutlet).Error; err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.pharmacy_subject','user-editor',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)`, pharmaOutlet, pharmaOrganization).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO pharmacy_catalogue_products (id,tenant_id,outlet_id,sku,brand_name,generic_name,dosage_form,route,manufacturer,marketing_company,ingredients,pack_size,unit_of_issue,currency,mrp_minor,status,version)
			VALUES (?,?,?,'TEST-PKR','TEST Rupee','TEST generic','tablet','oral','TEST Maker','','[{"name":"TEST","strength":"1","unit":"mg"}]',10,'tablet','PKR',100,'active',1)`,
			uuid.Must(uuid.NewV7()).String(), testutil.TenantA, pharmaOutlet).Error
	})
	err := db.Migrate(ctx, rest)
	require.Error(t, err)
	require.Contains(t, err.Error(), "pharmacy_catalogue_products 1")
	testutil.RequireForcedRLS(t, ctx, db, "pharmacy_outlets", "pharmacy_catalogue_products")
}

func TestPharmacyMigrationPassesWithoutPakistanRows(t *testing.T) {
	db, ctx, rest := testutil.MigratedBefore(t, pakistanMigration, pharmacy.New())
	legacyTx(t, db, func(tx *gorm.DB) error { return seedLegacyOutlet(tx, pharmaOutlet, "IN", "Asia/Kolkata") })
	require.NoError(t, db.Migrate(ctx, rest))
	testutil.RequireForcedRLS(t, ctx, db, "pharmacy_outlets", "pharmacy_catalogue_products", "pharmacy_payables", "pharmacy_settlements")
}

// #1287: the API refuses new rupee amounts too, not only the migration the
// old ones: an amount is in a supported country's currency.
func TestPharmacyRefusesRupeeAmounts(t *testing.T) {
	r, _ := overviewHarness(t)
	w := do(r, "PUT", productPath(uuid.Must(uuid.NewV7()).String()), "editor", strings.Replace(productBody(0, "TEST Rupee"), `"INR"`, `"PKR"`, 1))
	require.Equal(t, 400, w.Code, w.Body.String())
	w = do(r, "PUT", payableRoot()+"/payables/"+uuid.Must(uuid.NewV7()).String(), "editor", strings.Replace(expenseBody("TEST-RUPEE"), `"INR"`, `"PKR"`, 1))
	require.Equal(t, 400, w.Code, w.Body.String())
	w = do(r, "PUT", productPath(uuid.Must(uuid.NewV7()).String()), "editor", productBody(0, "TEST Rupaiya"))
	require.Equal(t, 200, w.Code, w.Body.String())
}

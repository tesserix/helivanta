package pharmacy_test

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// seedTaxProduct creates the shared outlet and one product, with the editor
// granted outlet access, and returns the product id.
func seedTaxProduct(t *testing.T) (*gin.Engine, *tenantdb.DB, string) {
	t.Helper()
	r, db := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	id := uuid.Must(uuid.NewV7()).String()
	w := do(r, "PUT", productPath(id), "editor", productBody(0, "TEST Amoxil"))
	require.Equal(t, 200, w.Code, w.Body.String())
	return r, db, id
}

func insertRegistration(tx *gorm.DB, effective, reason string) error {
	return tx.Exec(`INSERT INTO pharmacy_outlet_tax_registrations
 (id,tenant_id,outlet_id,jurisdiction_code,identifier_kind,identifier,effective_from,reason,recorded_by)
 VALUES (?,?,?,'07','gstin','07ABCDE1234F1Z2',?,NULLIF(?,''),'user-editor')`,
		uuid.Must(uuid.NewV7()), testutil.TenantA, pharmaOutlet, effective, reason).Error
}

func insertClassification(tx *gorm.DB, product, nature string, rate int64, basis, effective, reason string) error {
	return tx.Exec(`INSERT INTO pharmacy_product_tax_classifications
 (id,tenant_id,outlet_id,product_id,classification_code,supply_nature,rate_ppm,exemption_basis,effective_from,reason,recorded_by)
 VALUES (?,?,?,?,'3004',?,?,NULLIF(?,''),?,NULLIF(?,''),'user-editor')`,
		uuid.Must(uuid.NewV7()), testutil.TenantA, pharmaOutlet, product, nature, rate, basis, effective, reason).Error
}

func TestTaxSchemaIsAppendOnlyAndExplainsCorrections(t *testing.T) {
	_, db, product := seedTaxProduct(t)
	tx := func(fn func(*gorm.DB) error) error { return catalogueTx(t, db, fn) }

	require.NoError(t, tx(func(tx *gorm.DB) error { return insertRegistration(tx, "2026-11-01", "") }))
	require.Error(t, tx(func(tx *gorm.DB) error { return insertRegistration(tx, "2026-11-01", "") }), "same date without a reason")
	require.NoError(t, tx(func(tx *gorm.DB) error { return insertRegistration(tx, "2026-11-01", "Typo in GSTIN") }))

	require.NoError(t, tx(func(tx *gorm.DB) error {
		return insertClassification(tx, product, "taxable", 50000, "", "2026-11-01", "")
	}))
	require.Error(t, tx(func(tx *gorm.DB) error {
		return insertClassification(tx, product, "taxable", 120000, "", "2026-11-01", "")
	}))
	require.NoError(t, tx(func(tx *gorm.DB) error {
		return insertClassification(tx, product, "taxable", 120000, "", "2026-11-01", "Wrong slab")
	}))

	for name, row := range map[string]struct {
		nature string
		rate   int64
		basis  string
	}{
		"taxable at zero":         {"taxable", 0, ""},
		"taxable above 100%":      {"taxable", 1000100, ""},
		"rate not in 0.01% steps": {"taxable", 50050, ""},
		"nil-rated with a rate":   {"nil_rated", 50000, ""},
		"exempt without a basis":  {"exempt", 0, ""},
		"basis on a taxable row":  {"taxable", 50000, "TEST notification"},
		"unknown nature":          {"zero_rated", 0, ""},
	} {
		t.Run(name, func(t *testing.T) {
			require.Error(t, tx(func(tx *gorm.DB) error {
				return insertClassification(tx, product, row.nature, row.rate, row.basis, "2027-01-01", "")
			}))
		})
	}
	require.NoError(t, tx(func(tx *gorm.DB) error {
		return insertClassification(tx, product, "exempt", 0, "TEST notification entry", "2027-02-01", "")
	}))

	for _, table := range []string{"pharmacy_outlet_tax_registrations", "pharmacy_product_tax_classifications"} {
		for _, stmt := range []string{"UPDATE " + table + " SET reason='x'", "DELETE FROM " + table, "TRUNCATE " + table} {
			require.Error(t, tx(func(tx *gorm.DB) error { return tx.Exec(stmt).Error }), stmt)
		}
		// Tenant B sees nothing of tenant A's tax setup.
		require.NoError(t, db.WithTenant(t.Context(), testutil.TenantB, func(tx *gorm.DB) error {
			var n int64
			require.NoError(t, tx.Table(table).Count(&n).Error)
			require.Zero(t, n, table)
			return nil
		}))
	}

	// Classifications follow the catalogue: no outlet access row, no rows.
	asStranger := func(fn func(*gorm.DB) error) error {
		return db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.pharmacy_subject','user-nobody',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)`, pharmaOutlet, pharmaOrganization).Error; err != nil {
				return err
			}
			return fn(tx)
		})
	}
	require.NoError(t, asStranger(func(tx *gorm.DB) error {
		var n int64
		require.NoError(t, tx.Table("pharmacy_product_tax_classifications").Count(&n).Error)
		require.Zero(t, n)
		return nil
	}))
	require.Error(t, asStranger(func(tx *gorm.DB) error {
		return insertClassification(tx, product, "taxable", 50000, "", "2027-03-01", "")
	}))
}

package pharmacy_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // own module's external test
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestOrderProductFactsKeepPrivateOwnerVersionUnitsAndPurpose(t *testing.T) {
	r, db := catalogueHarness(t)
	grantCatalogue(t, r, "user-editor", "editor")
	id := uuid.Must(uuid.NewV7())
	require.Equal(t, 200, do(r, "PUT", productPath(id.String()), "editor", productBody(0, "TEST order product")).Code)
	version := int64(1)
	lines := []supplierorder.SupplierOrderLine{{ID: uuid.Must(uuid.NewV7()), Product: supplierorder.SupplierOrderProduct{Owner: "pharmacy", ProductID: id, Version: &version}, QuantityUnits: 10, UnitCode: "capsule"}}
	endpoint := suppliertrade.Endpoint{TenantID: uuid.MustParse(testutil.TenantA), Product: "pharmacy", OrganizationID: uuid.MustParse(pharmaOrganization), LocationID: uuid.MustParse(pharmaOutlet), OrganizationVersion: 1, LocationVersion: 1}
	guard := pharmacy.New().SupplierOrderProducts(db, nil)
	check := func(lines []supplierorder.SupplierOrderLine) error {
		return db.WithGuardedTenantCommand(tenantdb.WithActor(t.Context(), "TEST-product-fact-reader"), testutil.TenantA, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(ctx context.Context) error {
			return guard.WithCurrentOrderProducts(ctx, endpoint, lines, func(current context.Context) error {
				return db.WithTenantCommand(current, testutil.TenantA, func(tx *gorm.DB) error {
					var purpose string
					if err := tx.Raw(`SELECT COALESCE(current_setting('app.pharmacy_order_catalogue',true),'')`).Scan(&purpose).Error; err != nil {
						return err
					}
					require.Empty(t, purpose, "private catalogue purpose must be gone before order writes")
					return nil
				})
			})
		})
	}
	require.NoError(t, check(lines))
	version = 2
	require.ErrorIs(t, check(lines), supplierorder.ErrIneligible)
	version = 1
	lines[0].UnitCode = "TEST-wrong-unit"
	require.ErrorIs(t, check(lines), supplierorder.ErrIneligible)
	lines[0].UnitCode = "capsule"
	lines[0].Product.ProductID = uuid.Must(uuid.NewV7())
	require.ErrorIs(t, check(lines), supplierorder.ErrIneligible)
}

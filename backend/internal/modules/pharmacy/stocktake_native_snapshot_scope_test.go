package pharmacy_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestStocktakeHospitalPreservesNativeSnapshotIsolation(t *testing.T) {
	r, db := stocktakeHarness(t)
	_, location, batch := stockFixture(t, r)
	response := do(r, "PUT", outletPath("/stock/movements/")+uuid.Must(uuid.NewV7()).String(), "editor", stockBody(location, batch, "receipt", "TEST-native-snapshot", 10))
	require.Equal(t, 200, response.Code, response.Body.String())
	for _, test := range []struct {
		name, actor, tenant, outlet string
		expected                    int64
	}{
		{"current editor", "user-editor", testutil.TenantA, pharmaOutlet, 1},
		{"current reader", "user-reader", testutil.TenantA, pharmaOutlet, 1},
		{"ungranted actor", "user-denied", testutil.TenantA, pharmaOutlet, 0},
		{"wrong tenant", "user-editor", testutil.TenantB, pharmaOutlet, 0},
		{"wrong outlet", "user-editor", testutil.TenantA, uuid.Must(uuid.NewV7()).String(), 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), test.actor), test.tenant, func(tx *gorm.DB) error {
				if err := tx.Exec(`SELECT set_config('app.pharmacy_subject',?,true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true),set_config('app.pharmacy_stocktake_native_snapshot','1',true)`, test.actor, test.outlet, pharmaOrganization).Error; err != nil {
					return err
				}
				var total int64
				if err := tx.Table("pharmacy_stock_movements").Where("tenant_id=? AND outlet_id=?", testutil.TenantA, pharmaOutlet).Count(&total).Error; err != nil {
					return err
				}
				require.Equal(t, test.expected, total)
				return nil
			}))
		})
	}
	require.Error(t, db.WithTenant(tenantdb.WithActor(t.Context(), "user-reader"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_subject','user-reader',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true),set_config('app.pharmacy_stocktake_native_snapshot','1',true)`, pharmaOutlet, pharmaOrganization).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO pharmacy_stock_movements(tenant_id,outlet_id,id,location_id,batch_id,kind,quantity_units,reason,source_reference,actor_subject)VALUES(?,?,?,?,?,'receipt',1,'TEST marker cannot confer write access','TEST-NATIVE-MARKER','user-reader')`, testutil.TenantA, pharmaOutlet, uuid.Must(uuid.NewV7()), location, batch).Error
	}))
}

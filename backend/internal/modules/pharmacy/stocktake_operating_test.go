package pharmacy_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestStocktakeOperatingPurposeCannotInheritReceiptWrites(t *testing.T) {
	r, db := stocktakeHarness(t)
	_, location, batch := stockFixture(t, r)
	for _, permission := range []string{"", "pharmacy.stocktake.count"} {
		err := db.WithTenant(tenantdb.WithActor(t.Context(), "user-editor"), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.pharmacy_subject','user-editor',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true),
 set_config('app.pharmacy_stock_access','authorized',true),set_config('app.pharmacy_stock_pharmacy',?,true),set_config('app.pharmacy_stock_outlet',?,true),set_config('app.pharmacy_stock_permission',?,true)`,
				pharmaOutlet, pharmaOrganization, pharmaOrganization, pharmaOutlet, permission).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO pharmacy_stock_movements(tenant_id,outlet_id,id,batch_id,location_id,kind,quantity_units,reason,source_reference,actor_subject)
 VALUES(?,?,?,?,?,'receipt',1,'TEST purpose separation',?,'user-editor')`,
				testutil.TenantA, pharmaOutlet, uuid.Must(uuid.NewV7()), batch, location, "TEST-RECEIPT-"+permission).Error
		})
		if permission == "" {
			require.NoError(t, err, "the existing editor control must remain valid")
		} else {
			requireSQLState(t, err, "42501")
		}
	}
}

func TestStocktakeOperatingDeferredGuardsRequireHeldPurpose(t *testing.T) {
	r, db := stocktakeHarness(t)
	_, quarantine, _ := stockFixture(t, r)
	take := uuid.Must(uuid.NewV7())
	for _, validate := range []bool{false, true} {
		err := operatingStockTx(t, db, receiverSettings("pharmacy.stocktake.count"), func(tx *gorm.DB) error {
			if err := tx.Exec(`INSERT INTO pharmacy_stocktake_operating_evidence(tenant_id,outlet_id,id,pharmacy_id,stocktake_id,kind,permission,duty_id,snapshot_version,authority_epoch,staff_id,staff_version,link_id,link_version,actor_subject)
 VALUES(?,?,?,?,?,'start','pharmacy.stocktake.count',?,1,1,?,1,?,2,?)`,
				testutil.TenantA, pharmaOutlet, take, pharmaOrganization, take, uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), receiverSubject).Error; err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO pharmacy_stocktakes(tenant_id,outlet_id,id,number,selection,product_ids,quarantine_location_id,reason,actor_subject,intent_sha256,snapshot_rows)
 VALUES(?,?,?,0,'all','{}'::uuid[],?,'TEST held source purpose',?,?,0)`,
				testutil.TenantA, pharmaOutlet, take, quarantine, receiverSubject, strings.Repeat("a", 64)).Error; err != nil {
				return err
			}
			if err := tx.Exec("UPDATE pharmacy_stocktakes SET sealed=true WHERE tenant_id=? AND outlet_id=? AND id=?", testutil.TenantA, pharmaOutlet, take).Error; err != nil {
				return err
			}
			if validate {
				if err := tx.Exec("SET CONSTRAINTS pharmacy_stocktake_commit_guard IMMEDIATE").Error; err != nil {
					return err
				}
			}
			return tx.Exec("SELECT set_config('app.pharmacy_stock_permission','',true)").Error
		})
		if !validate {
			requireSQLState(t, err, "23514")
			continue
		}
		require.NoError(t, err)
	}
	require.NoError(t, operatingStockTx(t, db, receiverSettings("pharmacy.stocktake.read"), func(tx *gorm.DB) error {
		var facts int64
		if err := tx.Table("pharmacy_stocktake_operating_evidence").Where("tenant_id=? AND outlet_id=? AND stocktake_id=?", testutil.TenantA, pharmaOutlet, take).Count(&facts).Error; err != nil {
			return err
		}
		require.EqualValues(t, 1, facts, "failed validation must roll back its source evidence")
		return nil
	}))
}

package pharmacy_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestStocktakeSnapshotInsertGuardsRollBackMixedStatements(t *testing.T) {
	r, db := stocktakeHarness(t)
	product, location, _ := stockFixture(t, r)
	secondBatch := uuid.Must(uuid.NewV7()).String()
	response := do(r, "PUT", outletPath("/stock/batches/")+secondBatch, "editor", fmt.Sprintf(`{"product_id":%q,"lot_number":"TEST-SECOND-LOT","expires_on":"2030-10-31","mrp_minor":12500}`, product))
	require.Equal(t, 200, response.Code, response.Body.String())
	for _, tc := range []struct{ name, constraint string }{
		{"batch identity", "pharmacy_stocktake_batch"},
		{"actor identity", "pharmacy_stocktake_sealed"},
		{"snapshot time", "pharmacy_stocktake_sealed"},
		{"sealed parent", "pharmacy_stocktake_sealed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			take := uuid.Must(uuid.NewV7()).String()
			line1, line2 := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
			err := db.WithTenant(tenantdb.WithActor(t.Context(), "user-editor"), testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec(`SELECT set_config('app.pharmacy_subject','user-editor',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)`, pharmaOutlet, pharmaOrganization).Error; err != nil {
					return err
				}
				rows := 2
				if tc.name == "sealed parent" {
					rows = 0
				}
				if err := tx.Exec(`INSERT INTO pharmacy_stocktakes(tenant_id,outlet_id,id,number,selection,product_ids,quarantine_location_id,reason,actor_subject,intent_sha256,snapshot_rows)
 VALUES(?,?,?,0,'all','{}'::uuid[],?,'TEST malformed snapshot','user-editor',?,?)`, testutil.TenantA, pharmaOutlet, take, location, strings.Repeat("a", 64), rows).Error; err != nil {
					return err
				}
				if tc.name == "actor identity" {
					if err := tx.Exec(`SELECT set_config('app.pharmacy_subject','user-reviewer',true)`).Error; err != nil {
						return err
					}
				}
				if tc.name == "sealed parent" {
					if err := tx.Exec(`UPDATE pharmacy_stocktakes SET sealed=true WHERE tenant_id=? AND outlet_id=? AND id=?`, testutil.TenantA, pharmaOutlet, take).Error; err != nil {
						return err
					}
				}
				return tx.Exec(`INSERT INTO pharmacy_stocktake_lines(tenant_id,outlet_id,id,stocktake_id,product_id,product_version,sku,brand_name,unit_of_issue,batch_id,lot_number,expires_on,location_id,location_code,location_name,stock_status,book_units,movement_count,ndps_status,created_at)
 SELECT s.tenant_id,s.outlet_id,CASE WHEN b.id=?::uuid THEN ?::uuid ELSE ?::uuid END,s.id,p.id,p.version,p.sku,p.brand_name,p.unit_of_issue,b.id,
 CASE WHEN ? AND b.id=?::uuid THEN 'TEST wrong lot' ELSE b.lot_number END,b.expires_on,l.id,l.code,l.name,l.stock_status,0,0,'unknown',
 s.created_at+CASE WHEN ? AND b.id=?::uuid THEN interval '1 second' ELSE interval '0 seconds' END
 FROM pharmacy_stocktakes s JOIN pharmacy_catalogue_products p ON p.tenant_id=s.tenant_id AND p.outlet_id=s.outlet_id
 JOIN pharmacy_stock_batches b ON b.tenant_id=p.tenant_id AND b.outlet_id=p.outlet_id AND b.product_id=p.id
 JOIN pharmacy_stock_locations l ON l.tenant_id=s.tenant_id AND l.outlet_id=s.outlet_id AND l.id=s.quarantine_location_id
 WHERE s.tenant_id=? AND s.outlet_id=? AND s.id=? ORDER BY b.id`, secondBatch, line2, line1, tc.name == "batch identity", secondBatch, tc.name == "snapshot time", secondBatch, testutil.TenantA, pharmaOutlet, take).Error
			})
			var pg *pgconn.PgError
			require.ErrorAs(t, err, &pg)
			require.Equal(t, "23514", pg.Code)
			require.Equal(t, tc.constraint, pg.ConstraintName)
			require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				var remaining int64
				if err := tx.Table("audit_events").Where("tenant_id=? AND resource_id IN(?::uuid,?::uuid,?::uuid)", testutil.TenantA, take, line1, line2).Count(&remaining).Error; err != nil {
					return err
				}
				require.Zero(t, remaining, "a refused statement must retain no partial audit rows")
				return nil
			}))
		})
	}
}

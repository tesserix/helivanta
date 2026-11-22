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
	for _, tc := range []struct{ name, code, constraint string }{
		{"batch identity", "23514", "pharmacy_stocktake_batch"},
		{"actor identity", "23514", "pharmacy_stocktake_sealed"},
		{"snapshot time", "23514", "pharmacy_stocktake_sealed"},
		{"sealed parent", "23514", "pharmacy_stocktake_sealed"},
		{"missing audit actor", "42501", ""},
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
				if tc.name == "missing audit actor" {
					if err := tx.Exec(`SELECT set_config('app.actor_subject','',true)`).Error; err != nil {
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
			require.Equal(t, tc.code, pg.Code)
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

func TestStocktakeSnapshotAuditAndStatementScopedAuthority(t *testing.T) {
	r, db := stocktakeHarness(t)
	_, location, batch := stockFixture(t, r)
	response := do(r, "PUT", outletPath("/stock/movements/")+uuid.Must(uuid.NewV7()).String(), "editor", stockBody(location, batch, "receipt", "TEST audit scope", 10))
	require.Equal(t, 200, response.Code, response.Body.String())
	take := uuid.Must(uuid.NewV7()).String()
	response = do(r, "PUT", outletPath("/stock/stocktakes/")+take, "editor", fmt.Sprintf(`{"selection":"all","product_ids":[],"quarantine_location_id":%q,"reason":"TEST audit scope"}`, location))
	require.Equal(t, 200, response.Code, response.Body.String())
	otherOutlet := uuid.Must(uuid.NewV7()).String()
	root := "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/outlets/" + otherOutlet
	put := func(path, body string) {
		t.Helper()
		response := do(r, "PUT", path, "editor", body)
		require.Equal(t, 200, response.Code, response.Body.String())
	}
	put(root, `{"code":"TEST-SECOND","name":"TEST Second Outlet","country_code":"IN","timezone":"Asia/Kolkata"}`)
	put(root+"/access/user-editor", `{"level":"editor"}`)
	product2, location2, batch2, take2 := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	put(root+"/catalogue/products/"+product2, productBody(0, "TEST Second product"))
	put(root+"/stock/locations/"+location2, `{"code":"TEST-Q","name":"TEST Second quarantine"}`)
	put(root+"/stock/batches/"+batch2, fmt.Sprintf(`{"product_id":%q,"lot_number":"TEST-LOT","expires_on":"2030-10-31","mrp_minor":12500}`, product2))
	put(root+"/stock/movements/"+uuid.Must(uuid.NewV7()).String(), stockBody(location2, batch2, "receipt", "TEST second scope", 10))
	put(root+"/stock/stocktakes/"+take2, fmt.Sprintf(`{"selection":"all","product_ids":[],"quarantine_location_id":%q,"reason":"TEST second count"}`, location2))

	for _, tc := range []struct {
		name, tenant, subject, outlet string
		want                          int64
	}{
		{"first outlet only", testutil.TenantA, "user-editor", pharmaOutlet, 1},
		{"second outlet only", testutil.TenantA, "user-editor", otherOutlet, 1},
		{"reader first outlet", testutil.TenantA, "user-reader", pharmaOutlet, 1},
		{"reader ungranted outlet", testutil.TenantA, "user-reader", otherOutlet, 0},
		{"missing selected outlet", testutil.TenantA, "user-editor", "", 0},
		{"ungranted subject", testutil.TenantA, "user-sibling", pharmaOutlet, 0},
		{"foreign tenant", testutil.TenantB, "user-editor", pharmaOutlet, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, db.WithTenant(t.Context(), tc.tenant, func(tx *gorm.DB) error {
				if err := tx.Exec(`SELECT set_config('app.pharmacy_subject',?,true),set_config('app.pharmacy_outlet',?,true)`, tc.subject, tc.outlet).Error; err != nil {
					return err
				}
				var count int64
				if err := tx.Table("pharmacy_stocktake_lines").Count(&count).Error; err != nil {
					return err
				}
				require.Equal(t, tc.want, count, "unfiltered table reads must retain row scope")
				return nil
			}))
		})
	}
	err := db.WithTenant(tenantdb.WithActor(t.Context(), "user-editor"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_subject','user-editor',true),set_config('app.pharmacy_outlet',?,true)`, pharmaOutlet).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO pharmacy_stocktake_lines SELECT proposed.* FROM pharmacy_stocktake_lines l CROSS JOIN LATERAL jsonb_populate_record(NULL::pharmacy_stocktake_lines,to_jsonb(l)||jsonb_build_object('id',?::uuid,'outlet_id',?::uuid))proposed`, uuid.Must(uuid.NewV7()).String(), otherOutlet).Error
	})
	var crossOutlet *pgconn.PgError
	require.ErrorAs(t, err, &crossOutlet)
	require.Equal(t, "42501", crossOutlet.Code, "even a writer granted both outlets must retain the selected row scope")
	for _, subject := range []string{"user-reader", "user-editor"} {
		if subject == "user-editor" {
			response := do(r, "PUT", outletPath("/access/user-editor"), "reviewer", `{"level":"reader"}`)
			require.Equal(t, 200, response.Code, response.Body.String())
		}
		err := db.WithTenant(tenantdb.WithActor(t.Context(), subject), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.pharmacy_subject',?,true),set_config('app.pharmacy_outlet',?,true)`, subject, pharmaOutlet).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO pharmacy_stocktake_lines SELECT proposed.* FROM pharmacy_stocktake_lines l CROSS JOIN LATERAL jsonb_populate_record(NULL::pharmacy_stocktake_lines,to_jsonb(l)||jsonb_build_object('id',?::uuid))proposed`, uuid.Must(uuid.NewV7()).String()).Error
		})
		var pg *pgconn.PgError
		require.ErrorAs(t, err, &pg)
		require.Equal(t, "42501", pg.Code, "reader and subsequently downgraded editor must fail the write policy")
	}
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("audit_events").Where("tenant_id=? AND resource_type='pharmacy_stocktake_lines' AND action='insert'", testutil.TenantA).Count(&count).Error; err != nil {
			return err
		}
		require.EqualValues(t, 2, count, "each frozen line retains exactly one audit event")
		if err := tx.Table("audit_events").Where("tenant_id=? AND resource_type='pharmacy_stocktake_lines' AND (actor_subject<>'user-editor' OR changed_fields<>'{}'::text[])", testutil.TenantA).Count(&count).Error; err != nil {
			return err
		}
		require.Zero(t, count, "audit actor and insert-field semantics remain unchanged")
		return nil
	}))
}

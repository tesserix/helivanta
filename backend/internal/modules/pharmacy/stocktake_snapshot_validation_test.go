package pharmacy_test

import (
	"encoding/json"
	"errors"
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

func TestStocktakeAdjustmentBatchValidationIsAtomic(t *testing.T) {
	r, db := stocktakeHarness(t)
	product, location, batch := stockFixture(t, r)
	secondBatch, otherLocation := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	put := func(path, body string) {
		t.Helper()
		response := do(r, "PUT", outletPath(path), "editor", body)
		require.Equal(t, 200, response.Code, response.Body.String())
	}
	put("/stock/batches/"+secondBatch.String(), fmt.Sprintf(`{"product_id":%q,"lot_number":"TEST-SECOND","expires_on":"2030-10-31","mrp_minor":12500}`, product))
	put("/stock/locations/"+otherLocation.String(), `{"code":"TEST-OTHER-Q","name":"TEST other quarantine"}`)
	for _, id := range []string{batch, secondBatch.String()} {
		put("/stock/movements/"+uuid.Must(uuid.NewV7()).String(), stockBody(location, id, "receipt", "TEST adjustment validation", 10))
	}
	take := uuid.Must(uuid.NewV7()).String()
	path := outletPath("/stock/stocktakes/") + take
	put("/stock/stocktakes/"+take, fmt.Sprintf(`{"selection":"all","product_ids":[],"quarantine_location_id":%q,"reason":"TEST adjustment validation"}`, location))
	response := do(r, "GET", path+"/lines", "editor", "")
	require.Equal(t, 200, response.Code, response.Body.String())
	var page struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
	require.Len(t, page.Data, 2)
	var counts []map[string]any
	for _, line := range page.Data {
		counts = append(counts, map[string]any{"line_id": line.ID, "counted_units": 9})
	}
	body, err := json.Marshal(map[string]any{"expected_version": 0, "counts": counts, "reason": "TEST two shortages"})
	require.NoError(t, err)
	response = do(r, "PUT", path+"/counts/"+uuid.Must(uuid.NewV7()).String(), "editor", string(body))
	require.Equal(t, 200, response.Code, response.Body.String())
	response = do(r, "PUT", path+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "editor", `{"decision":"submit","expected_version":1,"reason":"TEST mixed adjustment validation"}`)
	require.Equal(t, 200, response.Code, response.Body.String())
	var fault, observedConstraint string
	var callbackDB *gorm.DB
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		callbackDB = tx
		if err := tx.Callback().Create().Before("gorm:create").Register("test:stocktake-adjustment-corruption", func(tx *gorm.DB) {
			if tx.Statement.Table != "pharmacy_stocktake_adjustments" || fault == "" {
				return
			}
			rows := tx.Statement.ReflectValue
			if rows.Len() != 2 {
				tx.AddError(fmt.Errorf("expected two adjustment rows"))
				return
			}
			field := tx.Statement.Schema.LookUpField(fault)
			if field == nil {
				tx.AddError(fmt.Errorf("missing adjustment field"))
				return
			}
			var value any = int64(-2)
			if fault == "batch_id" {
				var zero bool
				value, zero = field.ValueOf(tx.Statement.Context, rows.Index(0))
				if zero {
					tx.AddError(fmt.Errorf("expected a real alternate batch"))
					return
				}
			}
			if fault == "location_id" {
				value = otherLocation
			}
			tx.AddError(field.Set(tx.Statement.Context, rows.Index(1), value))
		}); err != nil {
			return err
		}
		return tx.Callback().Create().After("gorm:create").Register("test:stocktake-adjustment-constraint", func(tx *gorm.DB) {
			if tx.Statement.Table != "pharmacy_stocktake_adjustments" {
				return
			}
			var pg *pgconn.PgError
			if errors.As(tx.Error, &pg) {
				observedConstraint = pg.ConstraintName
			}
		})
	}))
	t.Cleanup(func() {
		require.NoError(t, callbackDB.Callback().Create().Remove("test:stocktake-adjustment-corruption"))
		require.NoError(t, callbackDB.Callback().Create().Remove("test:stocktake-adjustment-constraint"))
	})
	for _, column := range []string{"quantity_units", "batch_id", "location_id"} {
		t.Run(column, func(t *testing.T) {
			fault, observedConstraint = column, ""
			response := do(r, "PUT", path+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "reviewer", `{"decision":"approve","expected_version":2,"reason":"TEST malformed mixed adjustments","extra_units_disposition":"no_extra_units"}`)
			require.Equal(t, 409, response.Code, response.Body.String())
			require.Equal(t, "pharmacy_stocktake_adjustment", observedConstraint, "the adjustment insert itself must reject the malformed row")
			header := do(r, "GET", path, "editor", "")
			require.Equal(t, 200, header.Code, header.Body.String())
			var state struct {
				State   string `json:"state"`
				Version int    `json:"version"`
			}
			require.NoError(t, json.Unmarshal(header.Body.Bytes(), &state))
			require.Equal(t, "submitted", state.State)
			require.Equal(t, 2, state.Version)
			require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec(`SELECT set_config('app.pharmacy_subject','user-editor',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)`, pharmaOutlet, pharmaOrganization).Error; err != nil {
					return err
				}
				for _, table := range []string{"pharmacy_stocktake_adjustments", "audit_events"} {
					query := tx.Table(table)
					if table == "audit_events" {
						query = query.Where("resource_type='pharmacy_stocktake_adjustments'")
					}
					var count int64
					if err := query.Count(&count).Error; err != nil {
						return err
					}
					require.Zero(t, count, "mixed invalid adjustment inserts must roll back their rows and audit")
				}
				var ledger struct{ Corrections, Units int64 }
				if err := tx.Raw(`SELECT count(*)FILTER(WHERE kind='correction')corrections,COALESCE(sum(quantity_units),0)units FROM pharmacy_stock_movements WHERE tenant_id=? AND outlet_id=?`, testutil.TenantA, pharmaOutlet).Scan(&ledger).Error; err != nil {
					return err
				}
				require.Zero(t, ledger.Corrections)
				require.EqualValues(t, 20, ledger.Units, "rejected approval must not change physical stock")
				return nil
			}))
		})
	}
	fault = ""
	response = do(r, "PUT", path+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "reviewer", `{"decision":"approve","expected_version":2,"reason":"TEST valid retry after refusals","extra_units_disposition":"no_extra_units"}`)
	require.Equal(t, 200, response.Code, response.Body.String())
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_subject','user-reviewer',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)`, pharmaOutlet, pharmaOrganization).Error; err != nil {
			return err
		}
		var totals struct{ Rows, Mismatched int64 }
		if err := tx.Raw(`SELECT count(*)rows,count(*)FILTER(WHERE a.created_at<>c.created_at)mismatched FROM pharmacy_stocktake_adjustments a JOIN pharmacy_stocktake_commands c ON(c.tenant_id,c.outlet_id,c.id)=(a.tenant_id,a.outlet_id,a.command_id) WHERE a.tenant_id=? AND a.outlet_id=?`, testutil.TenantA, pharmaOutlet).Scan(&totals).Error; err != nil {
			return err
		}
		require.EqualValues(t, 2, totals.Rows)
		require.Zero(t, totals.Mismatched, "older API inserts retain the command timestamp")
		return nil
	}))
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
	put(root+"/access/user-reviewer", `{"level":"editor"}`)
	product2, location2, batch2, take2 := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	put(root+"/catalogue/products/"+product2, productBody(0, "TEST Second product"))
	put(root+"/stock/locations/"+location2, `{"code":"TEST-Q","name":"TEST Second quarantine"}`)
	put(root+"/stock/batches/"+batch2, fmt.Sprintf(`{"product_id":%q,"lot_number":"TEST-LOT","expires_on":"2030-10-31","mrp_minor":12500}`, product2))
	put(root+"/stock/movements/"+uuid.Must(uuid.NewV7()).String(), stockBody(location2, batch2, "receipt", "TEST second scope", 10))
	put(root+"/stock/stocktakes/"+take2, fmt.Sprintf(`{"selection":"all","product_ids":[],"quarantine_location_id":%q,"reason":"TEST second count"}`, location2))

	for _, path := range []string{outletPath("/stock/stocktakes/") + take, root + "/stock/stocktakes/" + take2} {
		lines := do(r, "GET", path+"/lines", "editor", "")
		require.Equal(t, 200, lines.Code, lines.Body.String())
		var page struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(lines.Body.Bytes(), &page))
		require.Len(t, page.Data, 1)
		response := do(r, "PUT", path+"/counts/"+uuid.Must(uuid.NewV7()).String(), "editor", fmt.Sprintf(`{"expected_version":0,"counts":[{"line_id":%q,"counted_units":9}],"reason":"TEST scoped observation"}`, page.Data[0].ID))
		require.Equal(t, 200, response.Code, response.Body.String())
		response = do(r, "PUT", path+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "editor", `{"decision":"submit","expected_version":1,"reason":"TEST scoped submission"}`)
		require.Equal(t, 200, response.Code, response.Body.String())
		response = do(r, "PUT", path+"/decisions/"+uuid.Must(uuid.NewV7()).String(), "reviewer", `{"decision":"approve","expected_version":2,"reason":"TEST independent scoped approval","extra_units_disposition":"no_extra_units"}`)
		require.Equal(t, 200, response.Code, response.Body.String())
	}
	rowsPerOutlet := map[string]int64{"pharmacy_stocktakes": 1, "pharmacy_stocktake_lines": 1, "pharmacy_stocktake_commands": 3, "pharmacy_stocktake_counts": 1, "pharmacy_stocktake_adjustments": 1}
	for _, tc := range []struct {
		name, tenant, subject, outlet, pharmacy string
		want                                    int64
	}{
		{"first outlet only", testutil.TenantA, "user-editor", pharmaOutlet, pharmaOrganization, 1},
		{"second outlet only", testutil.TenantA, "user-editor", otherOutlet, pharmaOrganization, 1},
		{"reader first outlet", testutil.TenantA, "user-reader", pharmaOutlet, pharmaOrganization, 1},
		{"reader ungranted outlet", testutil.TenantA, "user-reader", otherOutlet, pharmaOrganization, 0},
		{"missing selected outlet", testutil.TenantA, "user-editor", "", pharmaOrganization, 0},
		{"ungranted subject", testutil.TenantA, "user-sibling", pharmaOutlet, pharmaOrganization, 0},
		{"foreign tenant", testutil.TenantB, "user-editor", pharmaOutlet, pharmaOrganization, 0},
		{"missing selected pharmacy", testutil.TenantA, "user-editor", pharmaOutlet, "", 0},
		{"wrong selected pharmacy", testutil.TenantA, "user-editor", pharmaOutlet, uuid.Must(uuid.NewV7()).String(), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.NoError(t, db.WithTenant(t.Context(), tc.tenant, func(tx *gorm.DB) error {
				if err := tx.Exec(`SELECT set_config('app.pharmacy_subject',?,true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)`, tc.subject, tc.outlet, tc.pharmacy).Error; err != nil {
					return err
				}
				for table, rows := range rowsPerOutlet {
					var count int64
					if err := tx.Table(table).Count(&count).Error; err != nil {
						return err
					}
					require.Equal(t, tc.want*rows, count, "%s unfiltered reads must retain row scope", table)
				}
				return nil
			}))
		})
	}
	err := db.WithTenant(tenantdb.WithActor(t.Context(), "user-editor"), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_subject','user-editor',true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)`, pharmaOutlet, pharmaOrganization).Error; err != nil {
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
			if err := tx.Exec(`SELECT set_config('app.pharmacy_subject',?,true),set_config('app.pharmacy_outlet',?,true),set_config('app.pharmacy_id',?,true)`, subject, pharmaOutlet, pharmaOrganization).Error; err != nil {
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

package pharmacy_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const receiverSubject = "user-receiver"

type operatingStockSettings struct{ subject, access, pharmacy, outlet, permission string }

func receiverSettings(permission string) operatingStockSettings {
	return operatingStockSettings{subject: receiverSubject, access: "authorized", pharmacy: pharmaOrganization, outlet: pharmaOutlet, permission: permission}
}

func operatingStockTx(t *testing.T, db *tenantdb.DB, s operatingStockSettings, fn func(*gorm.DB) error) error {
	t.Helper()
	return db.WithTenant(tenantdb.WithActor(t.Context(), receiverSubject), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_subject',?,true),set_config('app.pharmacy_stock_access',?,true),set_config('app.pharmacy_stock_pharmacy',?,true),set_config('app.pharmacy_stock_outlet',?,true),set_config('app.pharmacy_stock_permission',?,true)`, s.subject, s.access, s.pharmacy, s.outlet, s.permission).Error; err != nil {
			return err
		}
		return fn(tx)
	})
}

func visibleOperatingStock(t *testing.T, db *tenantdb.DB, s operatingStockSettings) [4]int64 {
	t.Helper()
	var counts [4]int64
	require.NoError(t, operatingStockTx(t, db, s, func(tx *gorm.DB) error {
		for i, table := range []string{"pharmacy_stock_locations", "pharmacy_stock_batches", "pharmacy_stock_movements", "pharmacy_catalogue_products"} {
			if err := tx.Table(table).Count(&counts[i]).Error; err != nil {
				return err
			}
		}
		return nil
	}))
	return counts
}

func requireSQLState(t *testing.T, err error, code string) {
	t.Helper()
	var pg *pgconn.PgError
	require.True(t, errors.As(err, &pg), "expected a PostgreSQL error, got %v", err)
	require.Equal(t, code, pg.Code, pg.Message)
}

type operatingStockRows struct{ product, quarantine, sellable, batch string }

func operatingStockFixture(t *testing.T, r *gin.Engine) operatingStockRows {
	t.Helper()
	grantCatalogue(t, r, "user-editor", "editor")
	product, quarantine, batch := stockFixture(t, r)
	root := "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/outlets/" + pharmaOutlet + "/stock"
	sellable := uuid.Must(uuid.NewV7()).String()
	w := do(r, "PUT", root+"/locations/"+sellable, "editor", `{"code":"TEST-SELLABLE","name":"TEST Sellable","stock_status":"sellable"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = do(r, "PUT", root+"/movements/"+uuid.Must(uuid.NewV7()).String(), "editor", stockBody(quarantine, batch, "receipt", "TEST-GRN-EDITOR", 10))
	require.Equal(t, 200, w.Code, w.Body.String())
	return operatingStockRows{product: product, quarantine: quarantine, sellable: sellable, batch: batch}
}

func insertOperatingMovement(tx *gorm.DB, id, location, batch, kind, actor string) error {
	return tx.Exec(`INSERT INTO pharmacy_stock_movements(id,tenant_id,outlet_id,location_id,batch_id,kind,quantity_units,reason,source_reference,actor_subject)VALUES(?,?,?,?,?,?,5,'TEST duty receipt',?,?)`,
		id, testutil.TenantA, pharmaOutlet, location, batch, kind, "TEST-DUTY-"+id, actor).Error
}

func insertOperatingAudit(tx *gorm.DB, resource, permission, actor string) error {
	return tx.Exec(`INSERT INTO pharmacy_operating_stock_audit(id,tenant_id,pharmacy_id,outlet_id,action,resource_kind,resource_id,permission,duty_id,snapshot_version,authority_epoch,staff_id,staff_version,link_id,link_version,actor_subject)VALUES(?,?,?,?,'pharmacy.operating_stock.receipt_recorded','receipt',?,?,?,3,1,?,1,?,2,?)`,
		uuid.Must(uuid.NewV7()), testutil.TenantA, pharmaOrganization, pharmaOutlet, resource, permission, uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), actor).Error
}

func TestOperatingStockScopeAdmitsOnlyTheExactAuthorizedOutletCommand(t *testing.T) {
	r, db := catalogueHarness(t)
	operatingStockFixture(t, r)
	visible := [4]int64{2, 1, 1, 1}
	require.Equal(t, visible, visibleOperatingStock(t, db, receiverSettings("pharmacy.stock.receive")))
	require.Equal(t, visible, visibleOperatingStock(t, db, receiverSettings("pharmacy.stock.read")))
	other := uuid.Must(uuid.NewV7()).String()
	for name, mutate := range map[string]func(*operatingStockSettings){
		"no settings":         func(s *operatingStockSettings) { *s = operatingStockSettings{} },
		"no access marker":    func(s *operatingStockSettings) { s.access = "" },
		"other access marker": func(s *operatingStockSettings) { s.access = "pending" },
		"other subject":       func(s *operatingStockSettings) { s.subject = "user-other" },
		"no subject":          func(s *operatingStockSettings) { s.subject = "" },
		"other pharmacy":      func(s *operatingStockSettings) { s.pharmacy = other },
		"other outlet":        func(s *operatingStockSettings) { s.outlet = other },
		"release permission":  func(s *operatingStockSettings) { s.permission = "pharmacy.stock.release" },
		"no permission":       func(s *operatingStockSettings) { s.permission = "" },
	} {
		t.Run(name, func(t *testing.T) {
			s := receiverSettings("pharmacy.stock.receive")
			mutate(&s)
			require.Equal(t, [4]int64{}, visibleOperatingStock(t, db, s))
		})
	}
}

func TestOperatingStockScopeWritesOnlyQuarantineReceiptsAttributedToTheVerifiedSubject(t *testing.T) {
	r, db := catalogueHarness(t)
	rows := operatingStockFixture(t, r)
	receive := receiverSettings("pharmacy.stock.receive")
	batch := uuid.Must(uuid.NewV7()).String()
	require.NoError(t, operatingStockTx(t, db, receive, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_stock_batches(id,tenant_id,outlet_id,product_id,product_version,lot_number,expires_on,mrp_minor)VALUES(?,?,?,?,1,'TEST-DUTY-LOT','2030-10-31',12500)`, batch, testutil.TenantA, pharmaOutlet, rows.product).Error
	}))
	receipt := uuid.Must(uuid.NewV7()).String()
	require.NoError(t, operatingStockTx(t, db, receive, func(tx *gorm.DB) error {
		return insertOperatingMovement(tx, receipt, rows.quarantine, batch, "receipt", receiverSubject)
	}))
	var stored struct{ Kind, ActorSubject string }
	require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT kind,actor_subject FROM pharmacy_stock_movements WHERE id=?`, receipt).Scan(&stored).Error
	}))
	require.Equal(t, "receipt", stored.Kind)
	require.Equal(t, receiverSubject, stored.ActorSubject)

	refused := map[string]struct {
		settings operatingStockSettings
		write    func(*gorm.DB) error
		code     string
	}{
		"receipt under read permission": {receiverSettings("pharmacy.stock.read"), func(tx *gorm.DB) error {
			return insertOperatingMovement(tx, uuid.Must(uuid.NewV7()).String(), rows.quarantine, rows.batch, "receipt", receiverSubject)
		}, "42501"},
		"batch under read permission": {receiverSettings("pharmacy.stock.read"), func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO pharmacy_stock_batches(id,tenant_id,outlet_id,product_id,product_version,lot_number,expires_on)VALUES(?,?,?,?,1,'TEST-READ-LOT','2030-10-31')`, uuid.Must(uuid.NewV7()), testutil.TenantA, pharmaOutlet, rows.product).Error
		}, "42501"},
		"correction": {receive, func(tx *gorm.DB) error {
			return insertOperatingMovement(tx, uuid.Must(uuid.NewV7()).String(), rows.quarantine, rows.batch, "correction", receiverSubject)
		}, "42501"},
		"release": {receive, func(tx *gorm.DB) error {
			return insertOperatingMovement(tx, uuid.Must(uuid.NewV7()).String(), rows.sellable, rows.batch, "release", receiverSubject)
		}, "23514"},
		"receipt attributed to another subject": {receive, func(tx *gorm.DB) error {
			return insertOperatingMovement(tx, uuid.Must(uuid.NewV7()).String(), rows.quarantine, rows.batch, "receipt", "user-editor")
		}, "42501"},
		"receipt into sellable stock": {receive, func(tx *gorm.DB) error {
			return insertOperatingMovement(tx, uuid.Must(uuid.NewV7()).String(), rows.sellable, rows.batch, "receipt", receiverSubject)
		}, "23514"},
		"stock location": {receive, func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO pharmacy_stock_locations(id,tenant_id,outlet_id,code,name)VALUES(?,?,?,'TEST-DUTY-LOCATION','TEST duty location')`, uuid.Must(uuid.NewV7()), testutil.TenantA, pharmaOutlet).Error
		}, "42501"},
		"catalogue product": {receive, func(tx *gorm.DB) error {
			return tx.Exec(`UPDATE pharmacy_catalogue_products SET marketing_company='TEST duty change',version=version+1 WHERE id=?`, rows.product).Error
		}, "42501"},
	}
	for name, tc := range refused {
		t.Run(name, func(t *testing.T) {
			requireSQLState(t, operatingStockTx(t, db, tc.settings, tc.write), tc.code)
		})
	}
	require.Equal(t, [4]int64{2, 2, 2, 1}, visibleOperatingStock(t, db, receive))
}

func TestOperatingStockAuditIsAppendOnlyAndRollsBackWithTheWrite(t *testing.T) {
	r, db := catalogueHarness(t)
	rows := operatingStockFixture(t, r)
	receive := receiverSettings("pharmacy.stock.receive")
	rolledBack := uuid.Must(uuid.NewV7()).String()
	errAbort := errors.New("abort after audit")
	require.ErrorIs(t, operatingStockTx(t, db, receive, func(tx *gorm.DB) error {
		if err := insertOperatingMovement(tx, rolledBack, rows.quarantine, rows.batch, "receipt", receiverSubject); err != nil {
			return err
		}
		if err := insertOperatingAudit(tx, rolledBack, "pharmacy.stock.receive", receiverSubject); err != nil {
			return err
		}
		return errAbort
	}), errAbort)
	committed := uuid.Must(uuid.NewV7()).String()
	require.NoError(t, operatingStockTx(t, db, receive, func(tx *gorm.DB) error {
		if err := insertOperatingMovement(tx, committed, rows.quarantine, rows.batch, "receipt", receiverSubject); err != nil {
			return err
		}
		return insertOperatingAudit(tx, committed, "pharmacy.stock.receive", receiverSubject)
	}))
	auditFor := func(resource string) (movements, audits int64) {
		require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
			if err := tx.Table("pharmacy_stock_movements").Where("id=?", resource).Count(&movements).Error; err != nil {
				return err
			}
			return tx.Table("pharmacy_operating_stock_audit").Where("resource_id=? AND actor_subject=? AND permission='pharmacy.stock.receive' AND snapshot_version=3", resource, receiverSubject).Count(&audits).Error
		}))
		return movements, audits
	}
	movements, audits := auditFor(rolledBack)
	require.Zero(t, movements)
	require.Zero(t, audits)
	movements, audits = auditFor(committed)
	require.EqualValues(t, 1, movements)
	require.EqualValues(t, 1, audits)

	var receiverAudits int64
	require.NoError(t, operatingStockTx(t, db, receive, func(tx *gorm.DB) error {
		return tx.Table("pharmacy_operating_stock_audit").Count(&receiverAudits).Error
	}))
	require.Zero(t, receiverAudits, "the receiver records evidence without reading the outlet's audit trail")

	for name, write := range map[string]func(*gorm.DB) error{
		"other actor": func(tx *gorm.DB) error {
			return insertOperatingAudit(tx, committed, "pharmacy.stock.receive", "user-editor")
		},
		"other permission": func(tx *gorm.DB) error {
			return insertOperatingAudit(tx, committed, "pharmacy.stock.read", receiverSubject)
		},
	} {
		t.Run(name, func(t *testing.T) { requireSQLState(t, operatingStockTx(t, db, receive, write), "42501") })
	}
	requireSQLState(t, operatingStockTx(t, db, operatingStockSettings{}, func(tx *gorm.DB) error {
		return insertOperatingAudit(tx, committed, "pharmacy.stock.receive", receiverSubject)
	}), "42501")
	for _, statement := range []string{
		`UPDATE pharmacy_operating_stock_audit SET snapshot_version=4`,
		`DELETE FROM pharmacy_operating_stock_audit`,
		`TRUNCATE pharmacy_operating_stock_audit`,
	} {
		require.Error(t, catalogueTx(t, db, func(tx *gorm.DB) error { return tx.Exec(statement).Error }), statement)
		require.Error(t, operatingStockTx(t, db, receive, func(tx *gorm.DB) error { return tx.Exec(statement).Error }), statement)
	}
	_, audits = auditFor(committed)
	require.EqualValues(t, 1, audits, fmt.Sprintf("audit for %s must survive mutation attempts", committed))
}

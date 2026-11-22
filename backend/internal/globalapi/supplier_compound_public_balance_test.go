package globalapi

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/supply"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/supplierstock"
	fulfilment "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"gorm.io/gorm"
)

func (f supplierCompoundFixture) assertWarehouseDerivedBalanceIsProtected(t *testing.T, operator operatingDutyFixture, receipt fulfilment.AllocationCommandReceipt) {
	t.Helper()
	p := receipt.Allocation.Plan
	modules := f.registry.All()
	adapter := supply.SupplierStock(platform.Deps{DB: f.db, OperatingAuthorizer: iam.NewOperatingAuthorizer(f.db, f.graph, platform.IndependentStaffIdentities(modules, f.db), platform.OperatingLocations(modules, f.db))})
	source := fulfilment.SourceScope{TenantID: p.OwnerTenantID, Product: "supply", OrganizationID: p.SourceOrganizationID, LocationID: p.SourceLocationID, OrderID: p.OrderID}
	run := func(action func(*gorm.DB) error) error {
		return adapter.Admit(t.Context(), operator.principal, source, func(ctx context.Context, actor supplierorder.Actor) error {
			return f.db.WithTenantCommand(ctx, operator.principal.TenantID, func(tx *gorm.DB) error {
				return (supplierstock.Store{Owner: supplierstock.Warehouse}).Frame(tx, supplierstock.Scope{TenantID: p.OwnerTenantID, OrganizationID: p.SourceOrganizationID, LocationID: p.SourceLocationID, AllocationID: p.ID, OrderID: p.OrderID, StaffID: actor.StaffID, Subject: actor.Subject, Mode: "operator"}, func() error { return action(tx) })
			})
		})
	}
	predicate := "tenant_id=? AND party_id=? AND premise_id=? AND receipt_id=?"
	args := []any{p.OwnerTenantID, p.SourceOrganizationID, p.SourceLocationID, p.Input.StockID}
	assertBalance := func() {
		t.Helper()
		require.NoError(t, run(func(tx *gorm.DB) error {
			var row struct{ DispatchedUnits int64 }
			if err := tx.Table("supply_stock_fulfilment_balances").Select("dispatched_units").Where(predicate, args...).Take(&row).Error; err != nil {
				return err
			}
			require.Equal(t, receipt.Allocation.DispatchedUnits, row.DispatchedUnits)
			var prior string
			if err := tx.Raw("SELECT COALESCE(current_setting('app.supply_balance_dispatch',true),'')").Scan(&prior).Error; err != nil {
				return err
			}
			require.Empty(t, prior, "derived dispatch context must not escape its trigger")
			return nil
		}))
	}
	assertBalance()
	for _, operation := range []string{"insert", "update", "delete", "truncate", "indirect update with old journal"} {
		require.True(t, t.Run(operation, func(t *testing.T) {
			err := run(func(tx *gorm.DB) error {
				switch operation {
				case "insert":
					return tx.Exec("INSERT INTO supply_stock_fulfilment_balances(tenant_id,party_id,premise_id,receipt_id,dispatched_units)VALUES(?,?,?,?,1)", args...).Error
				case "update":
					return tx.Exec("UPDATE supply_stock_fulfilment_balances SET dispatched_units=dispatched_units+1 WHERE "+predicate, args...).Error
				case "delete":
					return tx.Exec("DELETE FROM supply_stock_fulfilment_balances WHERE "+predicate, args...).Error
				case "truncate":
					return tx.Exec("TRUNCATE supply_stock_fulfilment_balances").Error
				default:
					for _, query := range []string{
						"CREATE TEMP TABLE supplier_balance_tamper_attempt(id integer) ON COMMIT DROP",
						"CREATE FUNCTION pg_temp.supplier_balance_tamper() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN UPDATE supply_stock_fulfilment_balances SET dispatched_units=dispatched_units+1; RETURN NEW; END $$",
						"CREATE TRIGGER supplier_balance_tamper AFTER INSERT ON supplier_balance_tamper_attempt FOR EACH ROW EXECUTE FUNCTION pg_temp.supplier_balance_tamper()",
					} {
						if err := tx.Exec(query).Error; err != nil {
							return err
						}
					}
					if err := tx.Exec("SELECT set_config('app.supply_balance_dispatch',?,true)", receipt.CommandID.String()).Error; err != nil {
						return err
					}
					return tx.Exec("INSERT INTO supplier_balance_tamper_attempt VALUES(1)").Error
				}
			})
			var denied *pgconn.PgError
			require.ErrorAs(t, err, &denied)
			require.Equal(t, "42501", denied.Code, "a current scoped operator cannot mutate the derived balance")
		}))
		assertBalance()
	}
}

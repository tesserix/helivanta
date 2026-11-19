package globalapi

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/supplierstock"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// #1634 slice 0a: the supplier paths ask the pharmacy block rule, in the
// fulfilment frame they actually run in.

var blockReasons = []string{"expired", "mrp_missing", "safety_incident"}

type outletWorld struct {
	r       *gin.Engine
	db      *tenantdb.DB
	fixture supplierStockFixture
}

func newOutletWorld(t *testing.T) outletWorld {
	t.Helper()
	perms := []authz.Permission{"pharmacy.catalogue.read", "pharmacy.catalogue.write", "pharmacy.access.manage", "pharmacy.tax.manage", "pharmacy.stock.read", "pharmacy.stock.receive", "pharmacy.stock.quarantine", "pharmacy.stock.release", "pharmacy.schedule.declare", "pharmacy.schedule.review"}
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"owner": testutil.TenantA, "reviewer": testutil.TenantA}, Perms: map[string][]authz.Permission{"owner": perms, "reviewer": perms}, Modules: []platform.Module{pharmacy.New()}})
	return outletWorld{r: r, db: db, fixture: seedSupplierStock(t, r, db, "pharmacy")}
}

func (w outletWorld) stockPath(suffix string) string {
	return "/v1/pharmacy/pharmacies/" + w.fixture.organization.String() + "/outlets/" + w.fixture.location.String() + "/stock/" + suffix
}

// lot shelves another 100 units of the fixture's product, received and released.
func (w outletWorld) lot(t *testing.T) supplierStockFixture {
	t.Helper()
	lot := w.fixture
	lot.stock = uuid.Must(uuid.NewV7())
	for _, c := range []struct{ path, token, body string }{
		{"batches/" + lot.stock.String(), "owner", fmt.Sprintf(`{"product_id":%q,"lot_number":"TEST-%s","expires_on":"2030-12-31","mrp_minor":12500}`, lot.product.ProductID, lot.stock.String()[24:])},
		{"movements/" + uuid.Must(uuid.NewV7()).String(), "owner", fmt.Sprintf(`{"location_id":%q,"batch_id":%q,"kind":"receipt","quantity_units":100,"reason":"TEST receipt","source_reference":"TEST-GRN:%s"}`, lot.quarantine, lot.stock, lot.stock)},
		{"releases/" + uuid.Must(uuid.NewV7()).String(), "reviewer", fmt.Sprintf(`{"batch_id":%q,"from_location_id":%q,"to_location_id":%q,"quantity_units":100,"reason":"TEST checked physical release"}`, lot.stock, lot.quarantine, *lot.bucket)},
	} {
		res := testutil.Do(w.r, "PUT", w.stockPath(c.path), c.token, c.body)
		require.Equal(t, 200, res.Code, res.Body.String())
	}
	return lot
}

// block applies a reason to a shelved lot. Batches are immutable, so expiry
// and a missing MRP are fixtures for the calendar moving on and legacy rows.
func (w outletWorld) block(t *testing.T, lot supplierStockFixture, reason string) {
	t.Helper()
	set := map[string]string{"expired": "expires_on=(clock_timestamp()AT TIME ZONE o.timezone)::date", "mrp_missing": "mrp_minor=NULL"}[reason]
	switch reason {
	case "clean":
		return
	case "safety_incident":
		res := testutil.Do(w.r, "PUT", w.stockPath("safety-incidents/"+uuid.Must(uuid.NewV7()).String()), "owner", fmt.Sprintf(`{"batch_id":%q,"location_id":%q,"kind":"temperature_excursion","reason":"TEST refrigerator temperature excursion","occurred_at":"2026-01-01T10:00:00Z","measured_temperature_millicelsius":12000}`, lot.stock, *lot.bucket))
		require.Equal(t, 200, res.Code, res.Body.String())
		return
	}
	require.NotEmpty(t, set, reason)
	require.NoError(t, w.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_block_lot_" + uuid.Must(uuid.NewV7()).String(), SQL: `
 SELECT set_config('app.tenant_id','` + testutil.TenantA + `',true),set_config('app.pharmacy_subject','user-owner',true),set_config('app.pharmacy_outlet','` + lot.location.String() + `',true),set_config('app.pharmacy_id','` + lot.organization.String() + `',true);
 ALTER TABLE pharmacy_stock_batches DISABLE TRIGGER pharmacy_stock_batch_immutable;
 DO $$ DECLARE changed bigint; BEGIN
 UPDATE pharmacy_stock_batches b SET ` + set + ` FROM pharmacy_outlets o WHERE o.tenant_id=b.tenant_id AND o.id=b.outlet_id AND b.id='` + lot.stock.String() + `';
 GET DIAGNOSTICS changed=ROW_COUNT;
 IF changed<>1 THEN RAISE EXCEPTION 'fixture must change exactly one batch'; END IF;
 END $$;
 ALTER TABLE pharmacy_stock_batches ENABLE TRIGGER pharmacy_stock_batch_immutable;`}}))
}

// run opens the outlet's supplier fulfilment frame, as Admit does.
func (w outletWorld) run(t *testing.T, actor supplierorder.Actor, fn func(*gorm.DB) error) error {
	t.Helper()
	scope := supplierstock.Scope{TenantID: uuid.MustParse(testutil.TenantA), OrganizationID: w.fixture.organization, LocationID: w.fixture.location, StaffID: actor.StaffID, Subject: actor.Subject, Mode: "operator"}
	return w.db.WithTenant(tenantdb.WithActor(t.Context(), actor.Subject), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.pharmacy_subject',?,true),set_config('app.pharmacy_stock_pharmacy',?,true),set_config('app.pharmacy_stock_outlet',?,true),set_config('app.pharmacy_stock_access','authorized',true),set_config('app.pharmacy_stock_permission','pharmacy.stock.fulfil',true)`, actor.Subject, w.fixture.organization.String(), w.fixture.location.String()).Error; err != nil {
			return err
		}
		return supplierstock.Store{Owner: supplierstock.Outlet}.Frame(tx, scope, func() error { return fn(tx) })
	})
}

// evidence stands in for Snapshot, so only the database guard decides.
func evidence(lot supplierStockFixture) func(*gorm.DB, f.AllocationPlan) (f.StockEvidence, error) {
	return func(*gorm.DB, f.AllocationPlan) (f.StockEvidence, error) {
		return f.StockEvidence{LotNumber: "TEST-" + lot.stock.String()[24:], ExpiresOn: "2030-12-31", AvailableUnits: 100, Description: "TEST repository fact fixture", PackSize: 10}, nil
	}
}

func refusedBy(t *testing.T, err error, message, reason string) {
	t.Helper()
	var pg *pgconn.PgError
	require.True(t, errors.As(err, &pg), "%s: %v", reason, err)
	require.Equal(t, "23514", pg.Code, reason)
	require.Equal(t, message, pg.Message, reason)
}

// rawDispatch writes the dispatch movement without the adapter's Snapshot
// pre-check, so the dispatch guard alone decides.
type rawDispatch struct{}

func (rawDispatch) Lock(*gorm.DB, f.Allocation) error { return nil }
func (rawDispatch) Apply(tx *gorm.DB, r f.AllocationCommandReceipt) error {
	p := r.Allocation.Plan
	return tx.Exec(`INSERT INTO pharmacy_stock_movements(tenant_id,outlet_id,id,location_id,batch_id,kind,quantity_units,reason,source_reference,actor_subject,created_at)VALUES(?,?,?,?,?,'supplier_dispatch',?,?,?,?,?)`,
		p.OwnerTenantID, p.SourceLocationID, r.CommandID, *p.Input.StockLocationID, p.Input.StockID, -r.Input.QuantityUnits, r.Input.Reason, "supplier-dispatch:"+r.CommandID.String(), r.ActorSubject, r.RecordedAt).Error
}

// AC-0a-07
func TestSupplierStockNeverOffersABlockedLot(t *testing.T) {
	w := newOutletWorld(t)
	adapter := pharmacy.SupplierStock(platform.Deps{})
	lots := map[string]supplierStockFixture{"clean": w.fixture}
	names := map[uuid.UUID]string{w.fixture.stock: "clean"}
	for _, reason := range blockReasons {
		lots[reason] = w.lot(t)
		names[lots[reason].stock] = reason
		w.block(t, lots[reason], reason)
	}
	order, in, actor := sourceOrder(t, "pharmacy", w.fixture)
	source := f.SourceScope{TenantID: order.OwnerTenantID, OrderID: order.ID, OrganizationID: w.fixture.organization, LocationID: w.fixture.location, Product: "pharmacy"}
	type seen struct {
		Locations int
		Available int64
	}
	offered, read := map[string]int64{}, map[string]seen{}
	require.NoError(t, w.run(t, actor, func(tx *gorm.DB) error {
		choices, err := adapter.Choices(tx, source, order.Input.Lines[0], pagination.Params{Limit: 10})
		if err != nil {
			return err
		}
		for _, c := range choices {
			offered[names[c.ID]] = c.AvailableUnits
		}
		for reason, lot := range lots {
			locations, err := adapter.Locations(tx, source, order.Input.Lines[0], lot.stock, pagination.Params{Limit: 10})
			if err != nil {
				return err
			}
			planned := in
			planned.StockID = lot.stock
			plan, err := f.Plan(uuid.Must(uuid.NewV7()), order, planned, actor, f.Budget{}, time.Now())
			if err != nil {
				return err
			}
			snapshot, err := adapter.Snapshot(tx, plan)
			if err != nil {
				return fmt.Errorf("%s snapshot: %w", reason, err)
			}
			read[reason] = seen{len(locations), snapshot.AvailableUnits}
		}
		return nil
	}))
	require.Equal(t, map[string]int64{"clean": 100}, offered, "choices offer only the clean lot")
	require.Equal(t, map[string]seen{"clean": {1, 100}, "expired": {}, "mrp_missing": {}, "safety_incident": {}}, read, "locations and snapshot")
}

// AC-0a-08
func TestSupplierHoldRefusesEveryBlockReason(t *testing.T) {
	w := newOutletWorld(t)
	store := supplierstock.Store{Owner: supplierstock.Outlet}
	for _, reason := range append([]string{"clean"}, blockReasons...) {
		t.Run(reason, func(t *testing.T) {
			lot := w.lot(t)
			w.block(t, lot, reason)
			order, in, actor := sourceOrder(t, "pharmacy", lot)
			err := w.run(t, actor, func(tx *gorm.DB) error {
				_, _, err := store.Create(tx, uuid.Must(uuid.NewV7()), order, in, actor, evidence(lot))
				return err
			})
			if reason == "clean" {
				require.NoError(t, err)
			} else {
				refusedBy(t, err, "stock safety changed", reason)
			}
			require.NoError(t, w.run(t, actor, func(tx *gorm.DB) error {
				held, err := store.Held(tx, order.OwnerTenantID, lot.location, lot.stock, *lot.bucket)
				require.Equal(t, map[bool]int64{true: 60, false: 0}[reason == "clean"], held)
				return err
			}))
		})
	}
}

// AC-0a-09
func TestSupplierDispatchGuardRefusesEveryBlockReason(t *testing.T) {
	w := newOutletWorld(t)
	store := supplierstock.Store{Owner: supplierstock.Outlet}
	for _, reason := range append([]string{"clean"}, blockReasons...) {
		t.Run(reason, func(t *testing.T) {
			lot := w.lot(t)
			order, in, actor := sourceOrder(t, "pharmacy", lot)
			allocation := uuid.Must(uuid.NewV7())
			require.NoError(t, w.run(t, actor, func(tx *gorm.DB) error {
				_, _, err := store.Create(tx, allocation, order, in, actor, evidence(lot))
				return err
			}))
			w.block(t, lot, reason)
			err := w.run(t, actor, func(tx *gorm.DB) error {
				_, _, err := store.Command(tx, order.OwnerTenantID, allocation, uuid.Must(uuid.NewV7()), f.AllocationDecisionInput{ExpectedVersion: 1, QuantityUnits: 10, Decision: "dispatch", HandoffReference: "TEST-HANDOFF", Reason: "TEST courier handoff"}, actor, rawDispatch{})
				return err
			})
			if reason == "clean" {
				require.NoError(t, err)
			} else {
				refusedBy(t, err, "physical dispatch evidence changed", reason)
			}
			require.NoError(t, w.run(t, actor, func(tx *gorm.DB) error {
				var dispatched int64
				err := tx.Raw(`SELECT count(*) FROM pharmacy_stock_movements WHERE tenant_id=? AND outlet_id=? AND batch_id=? AND kind='supplier_dispatch'`, order.OwnerTenantID, lot.location, lot.stock).Scan(&dispatched).Error
				require.EqualValues(t, map[bool]int64{true: 1, false: 0}[reason == "clean"], dispatched)
				return err
			}))
		})
	}
}

// AC-0a-15: the receiving pre-check runs in the outlet's fulfil frame too.
func TestShipmentSafetyRefusesEveryBlockReason(t *testing.T) {
	w := newOutletWorld(t)
	store := supplierstock.Store{Owner: supplierstock.Outlet}
	safety := pharmacy.New().ShipmentSafety(platform.Deps{DB: w.db})
	for _, reason := range append([]string{"clean"}, blockReasons...) {
		t.Run(reason, func(t *testing.T) {
			lot := w.lot(t)
			order, in, actor := sourceOrder(t, "pharmacy", lot)
			allocation := uuid.Must(uuid.NewV7())
			var receipt f.AllocationCommandReceipt
			require.NoError(t, w.run(t, actor, func(tx *gorm.DB) error {
				if _, _, err := store.Create(tx, allocation, order, in, actor, evidence(lot)); err != nil {
					return err
				}
				var err error
				receipt, _, err = store.Command(tx, order.OwnerTenantID, allocation, uuid.Must(uuid.NewV7()), f.AllocationDecisionInput{ExpectedVersion: 1, QuantityUnits: 10, Decision: "dispatch", HandoffReference: "TEST-HANDOFF", Reason: "TEST courier handoff"}, actor, rawDispatch{})
				return err
			}))
			mrp := int64(12500)
			shipment, err := f.ShipmentFor(receipt, &mrp)
			require.NoError(t, err)
			w.block(t, lot, reason)
			received := false
			err = safety.WithSafeShipment(tenantdb.WithActor(t.Context(), actor.Subject), shipment, func(context.Context) error {
				received = true
				return nil
			})
			if reason == "clean" {
				require.NoError(t, err)
				require.True(t, received)
				return
			}
			require.ErrorIs(t, err, f.ErrConflict)
			require.False(t, received, "a blocked lot is never received")
		})
	}
}

package globalapi

import (
	"encoding/csv"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/supplierstock"
	"github.com/tesserix/helivanta/internal/testutil"
	fulfilment "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type reviewedStocktakeReservationFixture struct {
	f                  pharmacyReceiverFixture
	path, approvalPath string
	approval           map[string]any
	reserve            func() (int64, error)
	release            func() error
	held               func() (int64, error)
}

func newReviewedStocktakeReservationFixture(t *testing.T) reviewedStocktakeReservationFixture {
	t.Helper()
	f := newPharmacyReceiverFixture(t)
	f.activate(t)
	legacy := "/v1/pharmacy/pharmacies/" + f.pharmacy + "/outlets/" + f.outlet
	product := legacy + "/catalogue/products/" + f.product
	f.call("PUT", legacy+"/access/user-reviewer", "owner", map[string]any{"level": "editor"}, 200)
	classify := func(class string) {
		declaration := product + "/schedule/" + v7()
		f.call("PUT", declaration, "owner", map[string]any{"regulatory_class": class, "ndps": false, "source": "TEST synthetic schedule label"}, 201)
		f.call("POST", declaration+"/review", "reviewer", map[string]any{"decision": "approved", "reason": "TEST independent schedule evidence"}, 200)
	}
	classify("H1")
	f.call("PUT", product+"/tax-classifications/"+v7(), "owner", map[string]any{"classification_code": "30041010", "supply_nature": "taxable", "rate_percent": "5", "effective_from": time.Now().In(time.FixedZone("Asia/Kolkata", 19800)).Format(time.DateOnly)}, 201)
	batch := v7()
	f.call("PUT", f.stock(f.pharmacy, f.outlet, "/batches/"+batch), "receiver", f.batch("TEST-RESERVED-COUNT"), 200)
	f.call("PUT", f.stock(f.pharmacy, f.outlet, "/receipts/"+v7()), "receiver", f.receipt(batch, f.quarantine, "TEST-RESERVED-COUNT-RECEIPT"), 200)
	f.call("PUT", legacy+"/stock/releases/"+v7(), "reviewer", map[string]any{"batch_id": batch, "from_location_id": f.quarantine, "to_location_id": f.shelf, "quantity_units": 30, "reason": "TEST checked synthetic stock release"}, 200)
	receiving := f.duty
	f.duty = "/v1/iam/operating-duties/" + v7()
	f.input.TemplateKey = "pharmacy.stocktake_counter.v1"
	f.input.Reason = "TEST independent physical count"
	f.activate(t)
	f.call("PUT", receiving+"/commands/"+v7(), "owner", map[string]any{"decision": "revoke", "expected_version": 3, "reason": "TEST counting without receiving authority"}, 201)
	root := "/v1/pharmacy/pharmacies/" + f.pharmacy + "/operating-locations/" + f.outlet + "/stocktakes"
	path := root + "/" + v7()
	f.call("PUT", path, "receiver", map[string]any{"selection": "all", "product_ids": []string{}, "quarantine_location_id": f.quarantine, "reason": "TEST freeze scheduled physical stock"}, 200)
	var lines struct {
		Data []struct {
			ID         string `json:"id"`
			LocationID string `json:"location_id"`
			BookUnits  int64  `json:"book_units"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal([]byte(f.call("GET", path+"/lines", "receiver", nil, 200)), &lines))
	require.Len(t, lines.Data, 2)
	entries := make([]map[string]any, 0, 2)
	for _, line := range lines.Data {
		units := 0
		if line.LocationID == f.shelf {
			require.EqualValues(t, 30, line.BookUnits)
			units = 15
		} else {
			require.Equal(t, f.quarantine, line.LocationID)
			require.Zero(t, line.BookUnits)
		}
		entries = append(entries, map[string]any{"line_id": line.ID, "counted_units": units})
	}
	f.call("PUT", path+"/counts/"+v7(), "receiver", map[string]any{"expected_version": 0, "counts": entries, "reason": "TEST observed synthetic shortage"}, 200)
	f.call("PUT", path+"/decisions/"+v7(), "receiver", map[string]any{"decision": "submit", "expected_version": 1, "reason": "TEST ready for independent review"}, 200)
	classify("X")
	shelf := uuid.MustParse(f.shelf)
	version := int64(1)
	fixture := supplierStockFixture{organization: uuid.MustParse(f.pharmacy), location: uuid.MustParse(f.outlet), stock: uuid.MustParse(batch), quarantine: uuid.MustParse(f.quarantine), bucket: &shelf, product: supplierorder.SupplierOrderProduct{Owner: "pharmacy", ProductID: uuid.MustParse(f.product), Version: &version}}
	order, input, actor := sourceOrder(t, "pharmacy", fixture)
	input.QuantityUnits = 20
	store := supplierstock.Store{Owner: supplierstock.Outlet}
	adapter := pharmacy.SupplierStock(platform.Deps{DB: f.db, Bus: f.bus})
	allocation := uuid.Must(uuid.NewV7())
	scope := supplierstock.Scope{TenantID: order.OwnerTenantID, OrganizationID: fixture.organization, LocationID: fixture.location, StaffID: actor.StaffID, Subject: actor.Subject, Mode: "operator"}
	// The native supplier repository supplies real hold evidence; its caller's Staff admission is not under test here.
	supplier := func(action func(*gorm.DB) error) error {
		return f.db.WithTenant(tenantdb.WithActor(t.Context(), actor.Subject), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.pharmacy_subject',?,true),set_config('app.pharmacy_stock_pharmacy',?,true),set_config('app.pharmacy_stock_outlet',?,true),set_config('app.pharmacy_stock_access','authorized',true),set_config('app.pharmacy_stock_permission','pharmacy.stock.fulfil',true)`, actor.Subject, f.pharmacy, f.outlet).Error; err != nil {
				return err
			}
			return store.Frame(tx, scope, func() error { return action(tx) })
		})
	}
	reserve := func() (int64, error) {
		var available int64
		err := supplier(func(tx *gorm.DB) error {
			_, _, err := store.Create(tx, allocation, order, input, actor, func(tx *gorm.DB, plan fulfilment.AllocationPlan) (fulfilment.StockEvidence, error) {
				evidence, err := adapter.Snapshot(tx, plan)
				available = evidence.AvailableUnits
				return evidence, err
			})
			return err
		})
		return available, err
	}
	release := func() error {
		return supplier(func(tx *gorm.DB) error {
			_, _, err := store.Command(tx, order.OwnerTenantID, allocation, uuid.Must(uuid.NewV7()), fulfilment.AllocationDecisionInput{ExpectedVersion: 1, Decision: "release", QuantityUnits: 20, Reason: "TEST source released held units"}, actor, adapter)
			return err
		})
	}
	held := func() (int64, error) {
		var held int64
		err := supplier(func(tx *gorm.DB) error {
			var err error
			held, err = store.Held(tx, order.OwnerTenantID, fixture.location, fixture.stock, shelf)
			return err
		})
		return held, err
	}
	activateReviewedStocktakeReviewer(t, &f)
	f.call("GET", legacy+"/stock/balance?location_id="+f.shelf+"&batch_id="+batch, "stock-reviewer", nil, 403)
	return reviewedStocktakeReservationFixture{f: f, path: path, approvalPath: path + "/decisions/" + v7(), approval: map[string]any{"decision": "approve", "expected_version": 2, "extra_units_disposition": "no_extra_units", "reason": "TEST independent shortage review with live holds"}, reserve: reserve, release: release, held: held}
}

func (fixture reviewedStocktakeReservationFixture) assertLedger(t *testing.T, corrections, units int64) {
	t.Helper()
	fixture.f.ownerOutletTx(t, func(tx *gorm.DB) error {
		var row struct{ Corrections, Units int64 }
		if err := tx.Raw(`SELECT count(*) FILTER(WHERE kind='correction') corrections,COALESCE(sum(quantity_units),0) units FROM pharmacy_stock_movements WHERE tenant_id=? AND outlet_id=? AND location_id=?`, testutil.TenantA, fixture.f.outlet, fixture.f.shelf).Scan(&row).Error; err != nil {
			return err
		}
		require.Equal(t, corrections, row.Corrections)
		require.Equal(t, units, row.Units)
		return nil
	})
}

func TestStocktakeReviewedReadsPreserveModernReservationAndFrozenScheduledRegister(t *testing.T) {
	fixture := newReviewedStocktakeReservationFixture(t)
	available, err := fixture.reserve()
	require.NoError(t, err)
	require.EqualValues(t, 30, available)
	fixture.f.call("PUT", fixture.approvalPath, "stock-reviewer", fixture.approval, 409)
	fixture.f.call("GET", fixture.approvalPath, "stock-reviewer", nil, 404)
	fixture.assertLedger(t, 0, 30)
	held, err := fixture.held()
	require.NoError(t, err)
	require.EqualValues(t, 20, held)
	require.NoError(t, fixture.release())
	posted := fixture.f.call("PUT", fixture.approvalPath, "stock-reviewer", fixture.approval, 200)
	require.JSONEq(t, posted, fixture.f.call("PUT", fixture.approvalPath, "stock-reviewer", fixture.approval, 200))
	fixture.assertLedger(t, 1, 15)
	records, err := csv.NewReader(strings.NewReader(fixture.f.call("GET", fixture.path+"/register.csv", "stock-reviewer", nil, 200))).ReadAll()
	require.NoError(t, err)
	require.Greater(t, len(records), 1)
	section := -1
	for i, name := range records[0] {
		if name == "register_section" {
			section = i
		}
	}
	require.NotEqual(t, -1, section)
	for _, row := range records[1:] {
		require.Equal(t, "H1", row[section], "the count's frozen schedule must survive a later X classification")
	}
}

func TestStocktakeReviewedReadsPostedStockRejectsIncompatibleSupplierReservation(t *testing.T) {
	fixture := newReviewedStocktakeReservationFixture(t)
	posted := fixture.f.call("PUT", fixture.approvalPath, "stock-reviewer", fixture.approval, 200)
	available, err := fixture.reserve()
	require.ErrorIs(t, err, fulfilment.ErrConflict)
	require.EqualValues(t, 15, available)
	held, err := fixture.held()
	require.NoError(t, err)
	require.Zero(t, held)
	fixture.assertLedger(t, 1, 15)
	require.JSONEq(t, posted, fixture.f.call("PUT", fixture.approvalPath, "stock-reviewer", fixture.approval, 200))
}

func TestStocktakeReviewedReadsReservationRaceNeverOvercommitsPhysicalStock(t *testing.T) {
	fixture := newReviewedStocktakeReservationFixture(t)
	body, err := json.Marshal(fixture.approval)
	require.NoError(t, err)
	request := httptest.NewRequest("PUT", fixture.approvalPath, strings.NewReader(string(body)))
	request.Header.Set("Authorization", "Bearer stock-reviewer")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
	request.Header.Set("X-Helivanta-Expected-Subject", "user-stock-reviewer")
	type reservationResult struct {
		available int64
		err       error
	}
	start := make(chan struct{})
	reservations := make(chan reservationResult, 1)
	approvals := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		<-start
		available, err := fixture.reserve()
		reservations <- reservationResult{available, err}
	}()
	go func() {
		<-start
		response := httptest.NewRecorder()
		fixture.f.handler.ServeHTTP(response, request)
		approvals <- response
	}()
	close(start)
	reserved := <-reservations
	response := <-approvals
	if reserved.err == nil {
		require.Equal(t, 409, response.Code, response.Body.String())
		require.EqualValues(t, 30, reserved.available)
		fixture.f.call("GET", fixture.approvalPath, "stock-reviewer", nil, 404)
		fixture.assertLedger(t, 0, 30)
		held, err := fixture.held()
		require.NoError(t, err)
		require.EqualValues(t, 20, held)
		require.NoError(t, fixture.release())
	} else {
		require.ErrorIs(t, reserved.err, fulfilment.ErrConflict)
		require.Equal(t, 200, response.Code, response.Body.String())
		require.EqualValues(t, 15, reserved.available)
		fixture.assertLedger(t, 1, 15)
	}
	posted := fixture.f.call("PUT", fixture.approvalPath, "stock-reviewer", fixture.approval, 200)
	require.JSONEq(t, posted, fixture.f.call("PUT", fixture.approvalPath, "stock-reviewer", fixture.approval, 200))
	fixture.assertLedger(t, 1, 15)
	held, err := fixture.held()
	require.NoError(t, err)
	require.Zero(t, held)
}

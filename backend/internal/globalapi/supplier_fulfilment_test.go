package globalapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/drugmaster"
	"github.com/tesserix/helivanta/internal/modules/pharmacy"
	"github.com/tesserix/helivanta/internal/modules/supply"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/supplierstock"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/stockproduct"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"sync"
	"testing"
	"time"
)

func ptr[T any](v T) *T { return &v }
func sourceOrder(t *testing.T, owner string, stock ...supplierStockFixture) (supplierorder.SupplierOrder, f.ReserveInput, supplierorder.Actor) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	tenant := uuid.MustParse(testutil.TenantA)
	terms := suppliertrade.Terms{Seller: suppliertrade.Endpoint{Product: owner, TenantID: tenant, OrganizationID: uuid.Must(uuid.NewV7()), LocationID: uuid.Must(uuid.NewV7()), OrganizationVersion: 1, LocationVersion: 1, LicenceID: uuid.Must(uuid.NewV7()), LicenceVersion: 1, Jurisdiction: "IN-MH"}, Buyer: suppliertrade.Endpoint{Product: "pharmacy", TenantID: uuid.MustParse(testutil.TenantB), OrganizationID: uuid.Must(uuid.NewV7()), LocationID: uuid.Must(uuid.NewV7()), OrganizationVersion: 1, LocationVersion: 1, LicenceID: uuid.Must(uuid.NewV7()), LicenceVersion: 1, Jurisdiction: "IN-OD", HospitalID: ptr(uuid.New()), BindingID: ptr(uuid.Must(uuid.NewV7())), BindingVersion: ptr(int64(1)), RegistrationID: ptr(uuid.Must(uuid.NewV7())), RegistrationVersion: ptr(int64(1))}, Category: "prescription", Currency: "INR", ValidFrom: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), TermsReference: "TEST repository protocol only"}
	if owner == "pharmacy" {
		terms.Seller.RegistrationID = ptr(uuid.Must(uuid.NewV7()))
		terms.Seller.RegistrationVersion = ptr(int64(1))
	}
	if len(stock) > 0 {
		terms.Seller.OrganizationID = stock[0].organization
		terms.Seller.LocationID = stock[0].location
	}
	digest, err := suppliertrade.TermsDigest(terms)
	require.NoError(t, err)
	relationship := suppliertrade.Relationship{ID: uuid.Must(uuid.NewV7()), OwnerTenantID: terms.Buyer.TenantID, Side: "buyer", Version: 3, Terms: terms, TermsSha256: digest, Status: "active", AuthorityEpoch: 2, RemoteEpoch: 2, SignedBy: ptr("TEST-relationship-buyer"), RemoteSignedBy: ptr("TEST-relationship-source"), CreatedAt: now, UpdatedAt: now}
	product := supplierorder.SupplierOrderProduct{Owner: owner, ProductID: uuid.New(), VersionID: ptr(uuid.New())}
	if owner == "pharmacy" {
		product.ProductID = uuid.Must(uuid.NewV7())
		product.VersionID = nil
		product.Version = ptr(int64(1))
	}
	if len(stock) > 0 {
		product = stock[0].product
	}
	in := supplierorder.SupplierOrderInput{RelationshipID: relationship.ID, TermsSha256: digest, PurchaseOrderReference: "TEST repository PO", QuoteReference: "TEST reference only", ExpiresAt: now.Add(30 * time.Minute), Reason: "TEST immutable order", Lines: []supplierorder.SupplierOrderLine{{ID: uuid.Must(uuid.NewV7()), Product: product, QuantityUnits: 100, UnitCode: "capsule"}}}
	order, err := supplierorder.Prepare(uuid.Must(uuid.NewV7()), relationship, in, supplierorder.Actor{Subject: "TEST-hospital-maker", StaffID: uuid.New()}, now)
	require.NoError(t, err)
	_, event, err := supplierorder.Decide(order, supplierorder.SupplierOrderDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST hospital review"}, supplierorder.Actor{Subject: "TEST-hospital-reviewer", StaffID: uuid.New()}, now)
	require.NoError(t, err)
	order, err = supplierorder.Apply(nil, *event, tenant, now)
	require.NoError(t, err)
	order, _, err = supplierorder.Decide(order, supplierorder.SupplierOrderDecisionInput{ExpectedVersion: 1, Decision: "approve", Reason: "TEST source review"}, supplierorder.Actor{Subject: "TEST-source-reviewer", StaffID: uuid.New()}, now)
	require.NoError(t, err)
	input := f.ReserveInput{ExpectedOrderVersion: order.Version, LineID: in.Lines[0].ID, StockID: uuid.Must(uuid.NewV7()), QuantityUnits: 60, ExpiresAt: now.Add(15 * time.Minute), Reason: "TEST bounded allocation"}
	if owner == "pharmacy" {
		input.StockLocationID = ptr(uuid.Must(uuid.NewV7()))
	}
	if len(stock) > 0 {
		input.StockID = stock[0].stock
		input.StockLocationID = stock[0].bucket
	}
	return order, input, supplierorder.Actor{Subject: "TEST-stock-operator", StaffID: uuid.New()}
}

// This is real owner-namespace repository evidence, not Staff admission or physical stock eligibility.
func TestOwnerAllocationBudgetsSerializeAndRecoverOriginalIntent(t *testing.T) {
	perms := []authz.Permission{supply.PermPartyWrite, supply.PermOrganizationManage, supply.PermPremiseWrite, supply.PermOperatingWrite, supply.PermOperatingRead, supply.PermAccessManage, supply.PermStockReview, drugmaster.PermCatalogueRead, "pharmacy.catalogue.read", "pharmacy.catalogue.write", "pharmacy.access.manage", "pharmacy.tax.manage", "pharmacy.stock.read", "pharmacy.stock.receive", "pharmacy.stock.quarantine", "pharmacy.stock.release", "pharmacy.schedule.declare", "pharmacy.schedule.review"}
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"owner": testutil.TenantA, "reviewer": testutil.TenantA}, Roles: stockQualityMembership{}, Perms: map[string][]authz.Permission{"owner": perms, "reviewer": perms}, Modules: []platform.Module{drugmaster.New(), supply.New(), pharmacy.New()}})
	publishStockProduct(t, db, "active")
	for _, owner := range []supplierstock.Owner{supplierstock.Warehouse, supplierstock.Outlet} {
		t.Run(string(owner), func(t *testing.T) {
			store := supplierstock.Store{Owner: owner}
			fixture := seedSupplierStock(t, r, db, string(owner))
			order, in, actor := sourceOrder(t, string(owner), fixture)
			tenant := order.OwnerTenantID
			scope := supplierstock.Scope{TenantID: tenant, OrganizationID: order.Terms.Seller.OrganizationID, LocationID: order.Terms.Seller.LocationID, StaffID: actor.StaffID, Subject: actor.Subject, Mode: "operator"}
			run := func(ctx context.Context, fn func(*gorm.DB) error) error {
				return db.WithTenant(tenantdb.WithActor(ctx, actor.Subject), tenant.String(), func(tx *gorm.DB) error {
					if owner == supplierstock.Warehouse {
						if err := tx.Exec(`SELECT set_config('app.supply_subject',?,true),set_config('app.supply_stock_party',?,true),set_config('app.supply_stock_premise',?,true),set_config('app.supply_stock_access','authorized',true),set_config('app.supply_stock_permission','supply.stock.fulfil',true)`, actor.Subject, fixture.organization.String(), fixture.location.String()).Error; err != nil {
							return err
						}
					} else {
						if err := tx.Exec(`SELECT set_config('app.pharmacy_subject',?,true),set_config('app.pharmacy_stock_pharmacy',?,true),set_config('app.pharmacy_stock_outlet',?,true),set_config('app.pharmacy_stock_access','authorized',true),set_config('app.pharmacy_stock_permission','pharmacy.stock.fulfil',true)`, actor.Subject, fixture.organization.String(), fixture.location.String()).Error; err != nil {
							return err
						}
					}
					return store.Frame(tx, scope, func() error { return fn(tx) })
				})
			}
			stock := func(*gorm.DB, f.AllocationPlan) (f.StockEvidence, error) {
				return f.StockEvidence{LotNumber: "TEST-LOT", ExpiresOn: "2030-12-31", AvailableUnits: 100, Description: "TEST repository fact fixture", PackSize: 10}, nil
			}
			var wg sync.WaitGroup
			results := make(chan error, 2)
			ids := []uuid.UUID{uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())}
			for _, id := range ids {
				wg.Add(1)
				go func() {
					defer wg.Done()
					results <- run(t.Context(), func(tx *gorm.DB) error { _, _, err := store.Create(tx, id, order, in, actor, stock); return err })
				}()
			}
			wg.Wait()
			close(results)
			success, conflicts := 0, 0
			for err := range results {
				if err == nil {
					success++
				} else if errors.Is(err, f.ErrConflict) {
					conflicts++
				} else {
					require.NoError(t, err)
				}
			}
			require.Equal(t, 1, success)
			require.Equal(t, 1, conflicts)
			var created f.Allocation
			require.NoError(t, run(t.Context(), func(tx *gorm.DB) error {
				for _, id := range ids {
					a, err := store.Current(tx, tenant, id)
					if err == nil {
						created = a
						break
					}
					require.ErrorIs(t, err, gorm.ErrRecordNotFound)
				}
				require.NotEqual(t, uuid.Nil, created.Plan.ID)
				original, err := store.Original(tx, tenant, created.Plan.ID, in, actor)
				require.NoError(t, err)
				require.Equal(t, int64(1), original.Version)
				changed := in
				changed.QuantityUnits = 59
				_, err = store.Original(tx, tenant, created.Plan.ID, changed, actor)
				require.ErrorIs(t, err, f.ErrConflict)
				_, err = store.Original(tx, tenant, created.Plan.ID, in, supplierorder.Actor{Subject: actor.Subject, StaffID: uuid.New()})
				require.ErrorIs(t, err, f.ErrConflict)
				return nil
			}))

			require.NoError(t, run(t.Context(), func(tx *gorm.DB) error {
				scope := f.SourceScope{TenantID: tenant, OrderID: order.ID, OrganizationID: order.Terms.Seller.OrganizationID, LocationID: order.Terms.Seller.LocationID, Product: string(owner)}
				rows, err := store.List(tx, scope, pagination.Params{Limit: 1})
				if err != nil {
					return err
				}
				require.Len(t, rows, 1)
				require.Equal(t, created.Plan.ID, rows[0].Plan.ID)
				next, err := store.List(tx, scope, pagination.Params{Limit: 1, After: &pagination.Cursor{TenantID: tenant.String(), ID: rows[0].Plan.ID, CreatedAt: rows[0].Plan.CreatedAt}})
				if err != nil {
					return err
				}
				require.Empty(t, next)
				scope.OrderID = uuid.Must(uuid.NewV7())
				rows, err = store.List(tx, scope, pagination.Params{Limit: 1})
				require.Empty(t, rows)
				return err
			}))
			rollback := errors.New("TEST dependency failed before commit")
			next := in
			next.QuantityUnits = 40
			id := uuid.Must(uuid.NewV7())
			err := run(t.Context(), func(tx *gorm.DB) error {
				_, _, err := store.Create(tx, id, order, next, actor, stock)
				if err != nil {
					return err
				}
				return rollback
			})
			require.ErrorIs(t, err, rollback)
			require.NoError(t, run(t.Context(), func(tx *gorm.DB) error {
				_, err := store.Current(tx, tenant, id)
				require.ErrorIs(t, err, gorm.ErrRecordNotFound)
				return nil
			}))
			pending, err := supplierorder.Apply(&order, supplierorder.SupplierOrderExchange{OrderID: order.ID, Terms: order.Terms, Input: order.Input, InputSha256: order.InputSha256, SourceTenantID: order.Terms.Buyer.TenantID, SourceEpoch: order.RemoteEpoch + 1, Phase: "cancel_requested", SignatorySubject: "TEST-buyer-canceller"}, tenant, time.Now())
			require.NoError(t, err)
			canceller := platform.SupplierStockCancellation([]platform.Module{supply.New(), pharmacy.New()}, platform.Deps{DB: db})
			cancel := func(fail bool) error {
				ctx := tenantdb.WithActor(t.Context(), "system:supplier-fulfilment")
				return db.WithTenant(ctx, tenant.String(), func(tx *gorm.DB) error {
					if err := canceller.ReleaseOrder(ctx, tx, pending); err != nil {
						return err
					}
					if fail {
						return rollback
					}
					return nil
				})
			}
			require.ErrorIs(t, cancel(true), rollback)
			require.NoError(t, run(t.Context(), func(tx *gorm.DB) error {
				current, err := store.Current(tx, tenant, created.Plan.ID)
				if err != nil {
					return err
				}
				require.Equal(t, int64(60), current.ReservedUnits)
				return nil
			}))
			require.NoError(t, cancel(false))
			require.NoError(t, cancel(false))
			require.NoError(t, run(t.Context(), func(tx *gorm.DB) error {
				current, err := store.Current(tx, tenant, created.Plan.ID)
				if err != nil {
					return err
				}
				require.Zero(t, current.ReservedUnits)
				require.Equal(t, int64(60), current.ReleasedUnits)
				require.Equal(t, int64(2), current.Version)
				original, err := store.Original(tx, tenant, created.Plan.ID, in, actor)
				if err != nil {
					return err
				}
				require.Equal(t, int64(60), original.ReservedUnits)
				require.Equal(t, int64(1), original.Version)
				return nil
			}))
		})
	}
}

func TestSourceOrderFixtureMatchesBothPublishedOwnerContracts(t *testing.T) {
	for _, owner := range []string{"supply", "pharmacy"} {
		t.Run(owner, func(t *testing.T) {
			order, in, a := sourceOrder(t, owner)
			require.NoError(t, supplierorder.ValidateRecord(order))
			_, err := f.Plan(uuid.Must(uuid.NewV7()), order, in, a, f.Budget{}, time.Now())
			require.NoError(t, err)
		})
	}
}

type supplierStockFixture struct {
	organization, location, stock uuid.UUID
	bucket                        *uuid.UUID
	product                       supplierorder.SupplierOrderProduct
}

func seedSupplierStock(t *testing.T, r *gin.Engine, db *tenantdb.DB, owner string) supplierStockFixture {
	t.Helper()
	v := supplierStockFixture{organization: uuid.Must(uuid.NewV7()), location: uuid.Must(uuid.NewV7()), stock: uuid.Must(uuid.NewV7())}
	call := func(method, path, token, body string, status int) {
		t.Helper()
		response := testutil.Do(r, method, path, token, body)
		require.Equal(t, status, response.Code, response.Body.String())
	}
	if owner == "supply" {
		var product stockproduct.Reference
		// Query the published master through its own public port, never its tables.
		response := testutil.Do(r, "GET", "/v1/drugmaster/products/by-gtin/8901234567890", "owner", "")
		require.Equal(t, 200, response.Code, response.Body.String())
		var found struct {
			VersionID uuid.UUID `json:"version_id"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &found))
		require.NoError(t, drugmaster.New().StockProducts(db).WithCurrentStockProduct(t.Context(), found.VersionID, func(_ context.Context, ref stockproduct.Reference) error { product = ref; return nil }))
		v.product = supplierorder.SupplierOrderProduct{Owner: owner, ProductID: product.ProductID, VersionID: &found.VersionID}
		root := "/v1/supply/parties/" + v.organization.String()
		call("PUT", root, "owner", `{"expected_version":0,"code":"TEST-FULFILMENT","record_kind":"owned_organisation","legal_name":"TEST Distributor","trading_name":"TEST Trade","business_kind":"distributor","country_code":"IN","jurisdiction":"IN-MH","registration_number":"TEST-REG","corporate_group":"","website":"","default_credit_days":30,"contacts":[],"addresses":[],"tax_registrations":[]}`, 200)
		call("PUT", root+"/premises/"+v.location.String(), "owner", `{"expected_version":0,"code":"TEST-WAREHOUSE","name":"TEST Warehouse","kind":"warehouse","line1":"TEST Building","line2":"","city":"Pune","region":"IN-MH","postal_code":"411001","country_code":"IN","time_zone":"Asia/Kolkata","status":"active"}`, 200)
		stock := root + "/operating-locations/" + v.location.String() + "/stock/receipts/" + v.stock.String()
		call("PUT", stock, "owner", fmt.Sprintf(`{"product_version_id":%q,"lot_number":"TEST-LOT","expires_on":"2030-12-31","unit_code":"pack","quantity_units":10,"source_reference":"TEST-GRN:1","reason":"TEST counted packs"}`, found.VersionID), 201)
		identity := uuid.Must(uuid.NewV7())
		call("PUT", stock+"/identity-checks/"+identity.String(), "owner", fmt.Sprintf(`{"expected_version":0,"product_version_id":%q,"unit_kind":"pack","expected_pack_size":10,"expected_base_units":100,"reason":"TEST verified physical pack"}`, found.VersionID), 201)
		call("PUT", root+"/grants/"+uuid.Must(uuid.NewV7()).String(), "owner", fmt.Sprintf(`{"expected_version":0,"subject":"user-reviewer","role":"inventory","premise_id":%q,"valid_from":%q,"expires_at":%q,"status":"active","reason":"TEST warehouse quality reviewer"}`, v.location, time.Now().Add(-time.Hour).UTC().Format(time.RFC3339), time.Now().Add(time.Hour).UTC().Format(time.RFC3339)), 200)
		call("PUT", stock+"/quality-decisions/"+uuid.Must(uuid.NewV7()).String(), "reviewer", fmt.Sprintf(`{"expected_version":0,"identity_check_id":%q,"identity_version":1,"decision":"release","reason":"TEST independent physical quality review"}`, identity), 201)
		return v
	}
	root := "/v1/pharmacy/pharmacies/" + v.organization.String()
	call("PUT", root, "owner", `{"code":"TEST-FULFILMENT","name":"TEST Independent Pharmacy","ownership_type":"independent"}`, 200)
	call("PUT", root+"/outlets/"+v.location.String(), "owner", `{"code":"TEST-MAIN","name":"TEST Main Outlet","country_code":"IN","timezone":"Asia/Kolkata"}`, 200)
	root += "/outlets/" + v.location.String()
	for _, subject := range []string{"user-owner", "user-reviewer"} {
		call("PUT", root+"/access/"+subject, "owner", `{"level":"editor"}`, 200)
	}
	product := uuid.Must(uuid.NewV7())
	version := int64(1)
	v.product = supplierorder.SupplierOrderProduct{Owner: owner, ProductID: product, Version: &version}
	productPath := root + "/catalogue/products/" + product.String()
	call("PUT", productPath, "owner", `{"expected_version":0,"sku":"TEST-CAPSULE","brand_name":"TEST capsule","generic_name":"TEST molecule","dosage_form":"capsule","route":"oral","manufacturer":"TEST Manufacturer","marketing_company":"TEST Marketing","ingredients":[{"name":"TEST molecule","strength":"500","unit":"mg"}],"pack_size":10,"unit_of_issue":"capsule","currency":"INR","mrp_minor":12500,"status":"active"}`, 200)
	class := uuid.Must(uuid.NewV7()).String()
	call("PUT", productPath+"/schedule/"+class, "owner", `{"regulatory_class":"H","ndps":false,"source":"TEST pack label"}`, 201)
	call("POST", productPath+"/schedule/"+class+"/review", "reviewer", `{"decision":"approved","reason":"TEST checked"}`, 200)
	call("POST", productPath+"/tax-classifications", "owner", fmt.Sprintf(`{"classification_code":"30041010","supply_nature":"taxable","rate_percent":"5","effective_from":%q}`, time.Now().In(time.FixedZone("Asia/Kolkata", 19800)).Format(time.DateOnly)), 201)
	quarantine, sellable := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	v.bucket = &sellable
	call("PUT", root+"/stock/locations/"+quarantine.String(), "owner", `{"code":"TEST-Q","name":"TEST Quarantine"}`, 200)
	call("PUT", root+"/stock/locations/"+sellable.String(), "owner", `{"code":"TEST-SHELF","name":"TEST Shelf","stock_status":"sellable"}`, 200)
	call("PUT", root+"/stock/batches/"+v.stock.String(), "owner", fmt.Sprintf(`{"product_id":%q,"lot_number":"TEST-LOT","expires_on":"2030-12-31","mrp_minor":12500}`, product), 200)
	call("PUT", root+"/stock/movements/"+uuid.Must(uuid.NewV7()).String(), "owner", fmt.Sprintf(`{"location_id":%q,"batch_id":%q,"kind":"receipt","quantity_units":100,"reason":"TEST receipt","source_reference":"TEST-GRN:1"}`, quarantine, v.stock), 200)
	call("PUT", root+"/stock/releases/"+uuid.Must(uuid.NewV7()).String(), "reviewer", fmt.Sprintf(`{"batch_id":%q,"from_location_id":%q,"to_location_id":%q,"quantity_units":100,"reason":"TEST checked physical release"}`, v.stock, quarantine, sellable), 200)
	return v
}

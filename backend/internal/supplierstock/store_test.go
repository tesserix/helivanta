package supplierstock_test

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/pharmacy"
	"github.com/tesserix/helivanta/internal/modules/supply"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/supplierstock"
	"github.com/tesserix/helivanta/internal/testutil"
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
func sourceOrder(t *testing.T, owner string) (supplierorder.SupplierOrder, f.ReserveInput, supplierorder.Actor) {
	t.Helper()
	now := time.Now().UTC().Truncate(time.Microsecond)
	tenant := uuid.MustParse(testutil.TenantA)
	terms := suppliertrade.Terms{Seller: suppliertrade.Endpoint{Product: owner, TenantID: tenant, OrganizationID: uuid.Must(uuid.NewV7()), LocationID: uuid.Must(uuid.NewV7()), OrganizationVersion: 1, LocationVersion: 1, LicenceID: uuid.Must(uuid.NewV7()), LicenceVersion: 1, Jurisdiction: "IN-MH"}, Buyer: suppliertrade.Endpoint{Product: "pharmacy", TenantID: uuid.MustParse(testutil.TenantB), OrganizationID: uuid.Must(uuid.NewV7()), LocationID: uuid.Must(uuid.NewV7()), OrganizationVersion: 1, LocationVersion: 1, LicenceID: uuid.Must(uuid.NewV7()), LicenceVersion: 1, Jurisdiction: "IN-OD", HospitalID: ptr(uuid.New()), BindingID: ptr(uuid.Must(uuid.NewV7())), BindingVersion: ptr(int64(1)), RegistrationID: ptr(uuid.Must(uuid.NewV7())), RegistrationVersion: ptr(int64(1))}, Category: "prescription", Currency: "INR", ValidFrom: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), TermsReference: "TEST repository protocol only"}
	if owner == "pharmacy" {
		terms.Seller.RegistrationID = ptr(uuid.Must(uuid.NewV7()))
		terms.Seller.RegistrationVersion = ptr(int64(1))
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
	return order, input, supplierorder.Actor{Subject: "TEST-stock-operator", StaffID: uuid.New()}
}

// This is real owner-namespace repository evidence, not Staff admission or physical stock eligibility.
func TestOwnerAllocationBudgetsSerializeAndRecoverOriginalIntent(t *testing.T) {
	_, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{supply.New(), pharmacy.New()}})
	for _, owner := range []supplierstock.Owner{supplierstock.Warehouse, supplierstock.Outlet} {
		t.Run(string(owner), func(t *testing.T) {
			store := supplierstock.Store{Owner: owner}
			order, in, actor := sourceOrder(t, string(owner))
			tenant := order.OwnerTenantID
			scope := supplierstock.Scope{TenantID: tenant, OrganizationID: order.Terms.Seller.OrganizationID, LocationID: order.Terms.Seller.LocationID, StaffID: actor.StaffID, Subject: actor.Subject, Mode: "operator"}
			run := func(ctx context.Context, fn func(*gorm.DB) error) error {
				return db.WithTenant(tenantdb.WithActor(ctx, actor.Subject), tenant.String(), func(tx *gorm.DB) error { return store.Frame(tx, scope, func() error { return fn(tx) }) })
			}
			stock := func(*gorm.DB, f.AllocationPlan) (f.StockEvidence, error) {
				return f.StockEvidence{LotNumber: "TEST-LOT", ExpiresOn: time.Now().AddDate(1, 0, 0).UTC().Format(time.DateOnly), AvailableUnits: 100, Description: "TEST repository fact fixture", PackSize: 10}, nil
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

package supply_test

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/supply" //nolint:depguard // external test exercises its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"testing"
	"time"
)

func TestDirectedRelationshipProtocolPersistsBothConsentsAndNeverReactivatesFromReplay(t *testing.T) {
	_, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{supply.New()}})
	verifyRelationshipProofBoundaries(t, db)
	sellerID, buyerID := uuid.MustParse(testutil.TenantA), uuid.MustParse(testutil.TenantB)
	hospital, binding, registration := uuid.New(), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	two := int64(2)
	terms := suppliertrade.Terms{Seller: suppliertrade.Endpoint{TenantID: sellerID, Product: "supply", OrganizationID: uuid.Must(uuid.NewV7()), LocationID: uuid.Must(uuid.NewV7()), OrganizationVersion: 1, LocationVersion: 1, LicenceID: uuid.Must(uuid.NewV7()), LicenceVersion: 1, Jurisdiction: "IN-MH"}, Buyer: suppliertrade.Endpoint{TenantID: buyerID, Product: "pharmacy", OrganizationID: uuid.Must(uuid.NewV7()), LocationID: uuid.Must(uuid.NewV7()), OrganizationVersion: 1, LocationVersion: 1, LicenceID: uuid.Must(uuid.NewV7()), LicenceVersion: 1, Jurisdiction: "IN-OD", HospitalID: &hospital, BindingID: &binding, BindingVersion: &two, RegistrationID: &registration, RegistrationVersion: &two}, Category: "prescription", Currency: "INR", ValidFrom: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour), TermsReference: "TEST ONLY directed consent protocol; no trade activation"}
	input := suppliertrade.SupplierRelationshipInput{Terms: terms, Reason: "TEST protocol fixture"}
	id := uuid.Must(uuid.NewV7())
	state, err := suppliertrade.PrepareRelationship(id, terms, "TEST-maker", time.Now())
	require.NoError(t, err)
	input.Terms = state.Terms
	require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), "TEST-maker"), sellerID.String(), func(tx *gorm.DB) error {
		if err := supply.TestRelationshipLock(tx, sellerID, id); err != nil {
			return err
		}
		return supply.TestSaveTradeRelationship(tx, state, &input, "TEST-maker", nil)
	}))
	change := func(tenant uuid.UUID, decision, actor string) {
		t.Helper()
		require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), actor), tenant.String(), func(tx *gorm.DB) error {
			if err := supply.TestRelationshipLock(tx, tenant, id); err != nil {
				return err
			}
			current, err := supply.TestLoadTradeRelationship(tx, tenant, id)
			if err != nil {
				return err
			}
			next, event, err := suppliertrade.DecideRelationship(current, current.Version, decision, actor, time.Now())
			if err != nil {
				return err
			}
			if err := supply.TestSaveTradeRelationship(tx, next, nil, actor, nil); err != nil {
				return err
			}
			return supply.TestEnqueueRelationshipExchange(tx, event)
		}))
	}
	wait := func(tenant uuid.UUID, status string) suppliertrade.Relationship {
		t.Helper()
		var value suppliertrade.Relationship
		require.Eventually(t, func() bool {
			err := db.WithTenant(t.Context(), tenant.String(), func(tx *gorm.DB) error {
				var err error
				value, err = supply.TestLoadTradeRelationship(tx, tenant, id)
				return err
			})
			return err == nil && value.Status == status
		}, 20*time.Second, 100*time.Millisecond)
		return value
	}
	change(sellerID, "approve", "TEST-supplier-signatory")
	incoming := wait(buyerID, "incoming")
	require.False(t, suppliertrade.RelationshipCurrent(incoming, time.Now()))
	change(buyerID, "approve", "TEST-hospital-signatory")
	seller := wait(sellerID, "active")
	buyer := wait(buyerID, "active")
	require.Equal(t, seller.AuthorityEpoch, buyer.RemoteEpoch)
	require.Equal(t, buyer.AuthorityEpoch, seller.RemoteEpoch)
	verifyPurchaseOrderProtocol(t, db, seller, buyer)
	verifyRelationshipProofConsumption(t, db, seller)
	checker := relationshipCheckerForTest(t, db)
	for _, state := range []suppliertrade.Relationship{seller, buyer} {
		result, err := checker.CheckRelationship(t.Context(), relationshipCheckForTest(state, "new_business"))
		require.NoError(t, err)
		require.Equal(t, state.AuthorityEpoch, result.AuthorityEpoch)
		require.Equal(t, state.RemoteEpoch, result.RemoteEpoch)
	}
	wrongEpoch := relationshipCheckForTest(seller, "new_business")
	wrongEpoch.ExpectedAuthorityEpoch++
	_, err = checker.CheckRelationship(t.Context(), wrongEpoch)
	require.ErrorIs(t, err, suppliertrade.ErrIneligible)
	wrongTerms := relationshipCheckForTest(seller, "new_business")
	wrongTerms.TermsSha256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	_, err = checker.CheckRelationship(t.Context(), wrongTerms)
	require.ErrorIs(t, err, suppliertrade.ErrIneligible)
	foreign := relationshipCheckForTest(seller, "new_business")
	foreign.OwnerTenantID = uuid.New()
	_, err = checker.CheckRelationship(t.Context(), foreign)
	require.ErrorIs(t, err, suppliertrade.ErrIneligible)
	change(sellerID, "suspend", "TEST-supplier-signatory")
	buyer = wait(buyerID, "suspended")
	require.False(t, suppliertrade.RelationshipCurrent(buyer, time.Now()))
	for _, state := range []suppliertrade.Relationship{wait(sellerID, "suspended"), buyer} {
		_, err = checker.CheckRelationship(t.Context(), relationshipCheckForTest(state, "new_business"))
		require.ErrorIs(t, err, suppliertrade.ErrIneligible)
		_, err = checker.CheckRelationship(t.Context(), relationshipCheckForTest(state, "retained_transaction"))
		require.NoError(t, err)
	}
	// Redelivery with a different transport id must still be a business no-op.
	old := suppliertrade.RelationshipExchange{RelationshipID: id, Terms: seller.Terms, TermsSha256: seller.TermsSha256, SourceTenantID: sellerID, SourceEpoch: seller.AuthorityEpoch, Phase: "activated", SignatorySubject: "TEST-supplier-signatory"}
	payload, err := json.Marshal(old)
	require.NoError(t, err)
	replayID := uuid.Must(uuid.NewV7())
	require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), "TEST-supplier-signatory"), sellerID.String(), func(tx *gorm.DB) error {
		return events.Enqueue(tx, supply.TestRelationshipExchangeSubject, events.Event{ID: replayID.String(), Type: supply.TestRelationshipExchangeSubject, Version: 1, TenantID: sellerID.String(), DestinationTenantID: buyerID.String(), Data: payload})
	}))
	require.Eventually(t, func() bool {
		var count int64
		err := db.WithSystem(t.Context(), func(tx *gorm.DB) error {
			return tx.Table("processed_events").Where("consumer=? AND event_id=?", "supply-relationship-exchange", replayID).Count(&count).Error
		})
		return err == nil && count == 1
	}, 20*time.Second, 100*time.Millisecond)

	require.Eventually(t, func() bool {
		var count int64
		err := db.WithTenant(t.Context(), buyerID.String(), func(tx *gorm.DB) error {
			return tx.Table("supply_trade_relationship_history").Where("tenant_id=? AND relationship_id=?", buyerID, id).Count(&count).Error
		})
		return err == nil && count == 4
	}, 20*time.Second, 100*time.Millisecond)
	current := wait(buyerID, "suspended")
	require.Equal(t, buyer.Version, current.Version)
	// Explicitly verify row-level isolation even when a consumer forgets a tenant predicate.
	require.NoError(t, db.WithTenant(context.Background(), uuid.NewString(), func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("supply_trade_relationships").Where("id=?", id).Count(&count).Error; err != nil {
			return err
		}
		require.Zero(t, count)
		return nil
	}))
}

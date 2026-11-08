package supply_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/supply" //nolint:depguard // own module's external test
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

// Protocol/storage evidence only: actor admission and source product eligibility remain owner API gates.
func verifyPurchaseOrderProtocol(t *testing.T, db *tenantdb.DB, seller, buyer suppliertrade.Relationship) {
	t.Helper()
	dept := uuid.Must(uuid.NewV7())
	actor := supplierorder.Actor{Subject: "TEST-order-maker", StaffID: uuid.New()}
	reviewer := supplierorder.Actor{Subject: "TEST-order-reviewer", StaffID: uuid.New()}
	supplier := supplierorder.Actor{Subject: "TEST-order-source", StaffID: uuid.New()}
	version := uuid.New()
	input := supplierorder.SupplierOrderInput{RelationshipID: buyer.ID, TermsSha256: buyer.TermsSha256, PurchaseOrderReference: "TEST-PO-001", QuoteReference: "TEST agreed quote reference", ExpiresAt: buyer.Terms.ExpiresAt.Add(-time.Minute), Reason: "TEST shared order instructions", Lines: []supplierorder.SupplierOrderLine{{ID: uuid.Must(uuid.NewV7()), Product: supplierorder.SupplierOrderProduct{Owner: "supply", ProductID: uuid.New(), VersionID: &version}, QuantityUnits: 100, UnitCode: "capsule"}}}
	order, err := supplierorder.Prepare(uuid.Must(uuid.NewV7()), buyer, input, actor, time.Now())
	require.NoError(t, err)
	order.BuyerDepartmentID = &dept
	originalInput, err := json.Marshal(order.Input)
	require.NoError(t, err)
	originalReceipt, err := json.Marshal(order)
	require.NoError(t, err)
	buyerFrame := func(mode string, a supplierorder.Actor) map[string]string {
		return map[string]string{"app.supply_order_mode": mode, "app.supply_order_hospital": buyer.Terms.Buyer.HospitalID.String(), "app.supply_order_department": dept.String(), "app.supply_order_outlet": buyer.Terms.Buyer.LocationID.String(), "app.supply_order_actor_staff": a.StaffID.String()}
	}
	sourceFrame := func(mode string, a supplierorder.Actor) map[string]string {
		return map[string]string{"app.supply_order_mode": mode, "app.supply_order_product": seller.Terms.Seller.Product, "app.supply_order_organization": seller.Terms.Seller.OrganizationID.String(), "app.supply_order_location": seller.Terms.Seller.LocationID.String(), "app.supply_order_actor_staff": a.StaffID.String()}
	}
	local := supply.TestLocalPurchaseOrder{Order: order, DepartmentID: &dept, ProposalInput: originalInput, ProposalReceipt: originalReceipt}
	require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), actor.Subject), buyer.OwnerTenantID.String(), func(tx *gorm.DB) error {
		return supply.TestPurchaseOrderFrame(tx, buyerFrame("buyer_write", actor), func() error { return supply.TestSavePurchaseOrder(tx, local, actor.Subject, &actor.StaffID, nil) })
	}))
	apply := func(tenant uuid.UUID, a supplierorder.Actor, frame map[string]string, decision string) {
		t.Helper()
		require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), a.Subject), tenant.String(), func(tx *gorm.DB) error {
			return supply.TestPurchaseOrderFrame(tx, frame, func() error {
				if err := supply.TestPurchaseOrderLock(tx, tenant.String(), order.ID); err != nil {
					return err
				}
				current, err := supply.TestLoadPurchaseOrder(tx, tenant.String(), order.ID)
				if err != nil {
					return err
				}
				input := supplierorder.SupplierOrderDecisionInput{ExpectedVersion: current.Order.Version, Decision: decision, Reason: "TEST independently reviewed protocol"}
				next, event, err := supplierorder.Decide(current.Order, input, a, time.Now())
				if err != nil {
					return err
				}
				current.Order = next
				if err = supply.TestSavePurchaseOrder(tx, current, a.Subject, &a.StaffID, nil); err != nil {
					return err
				}
				receipt := supplierorder.SupplierOrderCommandReceipt{CommandID: uuid.Must(uuid.NewV7()), Input: input, Order: next, ActorSubject: a.Subject, ActorStaffID: a.StaffID, RecordedAt: time.Now().UTC().Truncate(time.Microsecond)}
				if err = supply.TestSavePurchaseOrderCommand(tx, receipt); err != nil {
					return err
				}
				for _, original := range []*supplierorder.SupplierOrderDecisionInput{nil, &input} {
					recovered, err := supply.TestRecoverPurchaseOrderCommand(tx, tenant.String(), order.ID, receipt.CommandID, a, original)
					require.NoError(t, err)
					require.Equal(t, receipt, recovered)
				}
				changed := input
				changed.Reason = "TEST changed retry intent"
				_, err = supply.TestRecoverPurchaseOrderCommand(tx, tenant.String(), order.ID, receipt.CommandID, a, &changed)
				require.ErrorIs(t, err, supplierorder.ErrConflict)
				for _, wrong := range []supplierorder.Actor{{Subject: a.Subject, StaffID: uuid.New()}, {Subject: "TEST-other-account", StaffID: a.StaffID}} {
					_, err = supply.TestRecoverPurchaseOrderCommand(tx, tenant.String(), order.ID, receipt.CommandID, wrong, nil)
					require.ErrorIs(t, err, supplierorder.ErrConflict)
				}
				_, err = supply.TestRecoverPurchaseOrderCommand(tx, tenant.String(), uuid.Must(uuid.NewV7()), receipt.CommandID, a, nil)
				require.ErrorIs(t, err, gorm.ErrRecordNotFound)
				if event != nil {
					return supply.TestEnqueueOrderExchange(tx, *event)
				}
				return nil
			})
		}))
	}
	read := func(tenant uuid.UUID, frame map[string]string) (supplierorder.SupplierOrder, error) {
		var current supplierorder.SupplierOrder
		err := db.WithTenant(tenantdb.WithActor(t.Context(), "TEST-order-reader"), tenant.String(), func(tx *gorm.DB) error {
			return supply.TestPurchaseOrderFrame(tx, frame, func() error {
				row, err := supply.TestLoadPurchaseOrder(tx, tenant.String(), order.ID)
				current = row.Order
				return err
			})
		})
		return current, err
	}
	wait := func(tenant uuid.UUID, frame map[string]string, status string) supplierorder.SupplierOrder {
		t.Helper()
		var current supplierorder.SupplierOrder
		require.Eventually(t, func() bool {
			value, err := read(tenant, frame)
			if err != nil {
				return false
			}
			current = value
			return value.Status == status
		}, 20*time.Second, 100*time.Millisecond)
		return current
	}
	apply(buyer.OwnerTenantID, reviewer, buyerFrame("buyer_write", reviewer), "approve")
	incoming := wait(seller.OwnerTenantID, sourceFrame("seller_read", supplier), "incoming")
	require.Equal(t, order.InputSha256, incoming.InputSha256)
	require.Nil(t, incoming.PreparedStaffID)
	apply(seller.OwnerTenantID, supplier, sourceFrame("seller_write", supplier), "approve")
	accepted := wait(buyer.OwnerTenantID, buyerFrame("buyer_read", reviewer), "accepted")
	require.Equal(t, order.InputSha256, accepted.InputSha256)
	require.Equal(t, reviewer.StaffID, *accepted.SignedStaffID)
	verifyReceivingResponseProjection(t, db, accepted, dept)
	// Narrow scope is enforced even when the application connection knows the identifier.
	wrong := buyerFrame("buyer_read", reviewer)
	wrong["app.supply_order_department"] = uuid.Must(uuid.NewV7()).String()
	_, err = read(buyer.OwnerTenantID, wrong)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	require.NoError(t, db.WithTenant(t.Context(), uuid.NewString(), func(tx *gorm.DB) error {
		var n int64
		if err := tx.Table("supply_purchase_orders").Where("id=?", order.ID).Count(&n).Error; err != nil {
			return err
		}
		require.Zero(t, n)
		return nil
	}))
	// With no stock commitments, the source owner can release its empty budget and acknowledge cancellation.
	apply(buyer.OwnerTenantID, actor, buyerFrame("buyer_write", actor), "cancel")
	cancelled := wait(seller.OwnerTenantID, sourceFrame("seller_read", supplier), "cancelled")
	require.Equal(t, supplier.StaffID, *cancelled.SignedStaffID)
	confirmed := wait(buyer.OwnerTenantID, buyerFrame("buyer_read", reviewer), "cancelled")
	require.Equal(t, reviewer.StaffID, *confirmed.SignedStaffID)
	require.Equal(t, cancelled.AuthorityEpoch, confirmed.RemoteEpoch)
	// A current authorized read keeps the original approval receipt after the later state.
	require.NoError(t, db.WithTenant(tenantdb.WithActor(context.WithoutCancel(t.Context()), reviewer.Subject), buyer.OwnerTenantID.String(), func(tx *gorm.DB) error {
		return supply.TestPurchaseOrderFrame(tx, buyerFrame("buyer_read", reviewer), func() error {
			row, err := supply.TestLoadPurchaseOrder(tx, buyer.OwnerTenantID.String(), order.ID)
			if err != nil {
				return err
			}
			require.JSONEq(t, string(originalReceipt), string(row.ProposalReceipt))
			require.Equal(t, "cancelled", row.Order.Status)
			recovered, err := supply.TestSamePurchaseOrderProposal(row, input, actor)
			require.NoError(t, err)
			require.Equal(t, "pending_review", recovered.Status)
			changed := input
			changed.QuoteReference = "TEST different quote"
			_, err = supply.TestSamePurchaseOrderProposal(row, changed, actor)
			require.ErrorIs(t, err, supplierorder.ErrConflict)
			_, err = supply.TestSamePurchaseOrderProposal(row, input, supplierorder.Actor{Subject: actor.Subject, StaffID: uuid.New()})
			require.ErrorIs(t, err, supplierorder.ErrConflict)
			row.ProposalReceipt = json.RawMessage(`{"not_a_receipt":true}`)
			_, err = supply.TestSamePurchaseOrderProposal(row, input, actor)
			require.ErrorIs(t, err, supplierorder.ErrUnavailable)
			return nil
		})
	}))
}

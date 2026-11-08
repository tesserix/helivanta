package supply_test

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/supply" //nolint:depguard // own package external integration tests
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"testing"
	"time"
)

func verifyReceivingResponseProjection(t *testing.T, db *tenantdb.DB, order supplierorder.SupplierOrder, department uuid.UUID) {
	t.Helper()
	id := func() uuid.UUID { return uuid.Must(uuid.NewV7()) }
	at := time.Now().UTC().Truncate(time.Microsecond)
	shipment := f.Shipment{ID: id(), AllocationID: id(), OrderID: order.ID, LineID: order.Input.Lines[0].ID, OrderInputSha256: order.InputSha256, SourceTenantID: order.Terms.Seller.TenantID, SourceOrganizationID: order.Terms.Seller.OrganizationID, SourceLocationID: order.Terms.Seller.LocationID, BuyerTenantID: order.OwnerTenantID, HospitalID: *order.Terms.Buyer.HospitalID, BuyerOrganizationID: order.Terms.Buyer.OrganizationID, BuyerLocationID: order.Terms.Buyer.LocationID, Product: order.Input.Lines[0].Product, UnitCode: order.Input.Lines[0].UnitCode, QuantityUnits: order.Input.Lines[0].QuantityUnits, LotNumber: "TEST-RESPONSE-LOT", ExpiresOn: "2030-12-31", HandoffReference: "TEST response delivery", ActorSubject: "TEST source picker", ProductDescription: "TEST source response product", PackSize: 1, DispatchedAt: at}
	body, err := json.Marshal(shipment)
	require.NoError(t, err)
	require.NoError(t, db.WithTenantCommand(tenantdb.WithActor(t.Context(), "system:supplier-shipments"), order.OwnerTenantID.String(), func(tx *gorm.DB) error {
		return supply.TestPurchaseOrderFrame(tx, map[string]string{"app.supply_order_mode": "exchange", "app.supply_order_id": order.ID.String(), "app.supply_order_origin": shipment.SourceTenantID.String()}, func() error {
			return tx.Exec(`INSERT INTO supply_supplier_shipments(tenant_id,id,order_id,hospital_id,department_id,pharmacy_id,outlet_id,source_tenant_id,source_product,body,actor_subject,created_at)VALUES(?,?,?,?,?,?,?,?,?,?::jsonb,?,?)`, order.OwnerTenantID, shipment.ID, order.ID, shipment.HospitalID, department, shipment.BuyerOrganizationID, shipment.BuyerLocationID, shipment.SourceTenantID, shipment.Product.Owner, string(body), "system:supplier-shipments", at).Error
		})
	}))
	record := f.ReceivingResponseRecord{ID: id(), OwnerTenantID: shipment.SourceTenantID, BuyerTenantID: shipment.BuyerTenantID, OrderID: order.ID, ShipmentID: shipment.ID, SourceProduct: shipment.Product.Owner, SourceOrganizationID: shipment.SourceOrganizationID, SourceLocationID: shipment.SourceLocationID, SummaryVersion: 2, Response: "disputed", Reason: "TEST ask to review shortage evidence", ActorSubject: "TEST source responder", ActorStaffID: id(), CreatedAt: at.Add(time.Second)}
	data, err := json.Marshal(f.ShareReceivingResponse(record))
	require.NoError(t, err)
	const topic = "helivanta.in.supply.supplier_receiving_response_exchange.v1"
	event := events.Event{ID: id().String(), Type: topic, Version: 1, TenantID: record.OwnerTenantID.String(), DestinationTenantID: record.BuyerTenantID.String(), Data: data}
	var consumer events.Consumer
	for _, candidate := range supply.New().Consumers(platform.Deps{}) {
		if candidate.Subject == topic {
			consumer = candidate
		}
	}
	require.NotNil(t, consumer.Handle)
	deliver := func(value events.Event) error {
		return db.WithTenantCommand(tenantdb.WithActor(t.Context(), "system:counterparty-response"), order.OwnerTenantID.String(), func(tx *gorm.DB) error { return consumer.Handle(t.Context(), tx, value) })
	}
	require.NoError(t, deliver(event))
	require.NoError(t, deliver(event))
	duplicate := event
	duplicate.ID = id().String()
	require.NoError(t, deliver(duplicate))
	forged := event
	forged.ID = id().String()
	forged.TenantID = uuid.NewString()
	require.Error(t, deliver(forged))
	forged = event
	altered := record
	altered.Reason = "TEST different immutable response"
	forged.Data, err = json.Marshal(f.ShareReceivingResponse(altered))
	require.NoError(t, err)
	require.Error(t, deliver(forged))
	read := func(frame map[string]string, tenant uuid.UUID, check func(*gorm.DB) error) error {
		return db.WithTenantCommand(tenantdb.WithActor(context.WithoutCancel(t.Context()), "TEST hospital reader"), tenant.String(), func(tx *gorm.DB) error {
			return supply.TestPurchaseOrderFrame(tx, frame, func() error { return check(tx) })
		})
	}
	frame := map[string]string{"app.supply_order_mode": "buyer_read", "app.supply_order_hospital": shipment.HospitalID.String(), "app.supply_order_department": department.String(), "app.supply_order_outlet": shipment.BuyerLocationID.String()}
	require.NoError(t, read(frame, order.OwnerTenantID, func(tx *gorm.DB) error {
		rows, err := supply.TestBuyerResponsePage(tx, order, shipment.ID, pagination.Params{Limit: 1})
		if err != nil {
			return err
		}
		require.Len(t, rows, 1)
		require.Equal(t, record.ID, rows[0].ID)
		require.Equal(t, int64(2), rows[0].SummaryVersion)
		next, err := supply.TestBuyerResponsePage(tx, order, shipment.ID, pagination.Params{Limit: 1, After: &pagination.Cursor{TenantID: order.OwnerTenantID.String(), ID: rows[0].ID, CreatedAt: rows[0].CreatedAt}})
		if err != nil {
			return err
		}
		require.Empty(t, next)
		var n int64
		if err = tx.Table("supply_supplier_received_responses").Count(&n).Error; err != nil {
			return err
		}
		require.Equal(t, int64(1), n)
		return nil
	}))
	frame["app.supply_order_department"] = id().String()
	require.NoError(t, read(frame, order.OwnerTenantID, func(tx *gorm.DB) error {
		var n int64
		err := tx.Table("supply_supplier_received_responses").Count(&n).Error
		require.Zero(t, n)
		return err
	}))
	require.NoError(t, read(frame, uuid.New(), func(tx *gorm.DB) error {
		var n int64
		err := tx.Table("supply_supplier_received_responses").Count(&n).Error
		require.Zero(t, n)
		return err
	}))
}

package supply

import (
	"context"
	"encoding/json"
	"errors"
	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract" //nolint:depguard // own data-only event contract
	"github.com/tesserix/helivanta/pkg/events"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"gorm.io/gorm"
	"reflect"
)

func receivingResponseEnvelope(event events.Event) (f.ReceivingSharedResponse, error) {
	var record f.ReceivingSharedResponse
	if supplierorder.DecodeObject(event.Data, &record) != nil || f.ValidateSharedReceivingResponse(record) != nil || event.Type != supplycontract.SubjectSupplierReceivingResponseExchange || event.Version != 1 || event.TenantID != record.OwnerTenantID.String() || event.DestinationTenantID != record.BuyerTenantID.String() {
		return record, errors.New("invalid supplier receiving response provenance")
	}
	return record, nil
}

func supplierReceivingResponseConsumer() events.Consumer {
	return events.Consumer{Name: "supply-supplier-receiving-responses", Subject: supplycontract.SubjectSupplierReceivingResponseExchange, Handle: func(_ context.Context, tx *gorm.DB, event events.Event) error {
		record, err := receivingResponseEnvelope(event)
		if err != nil {
			return err
		}
		return purchaseOrderFrame(tx, map[string]string{"app.supply_order_mode": "exchange", "app.supply_order_id": record.OrderID.String(), "app.supply_order_origin": record.OwnerTenantID.String(), "app.actor_subject": "system:supplier-responses"}, func() error {
			if err := purchaseOrderLock(tx, record.BuyerTenantID.String(), record.OrderID); err != nil {
				return err
			}
			order, err := loadPurchaseOrder(tx, record.BuyerTenantID.String(), record.OrderID)
			if err != nil {
				return err
			}
			if order.Order.Side != "buyer" || !receivingResponseMatchesOrder(record, order.Order) {
				return f.ErrInvalid
			}
			var row struct{ Body json.RawMessage }
			if err = tx.Table("supply_supplier_shipments").Select("body").Where("tenant_id=? AND id=? AND order_id=?", record.BuyerTenantID, record.ShipmentID, record.OrderID).Take(&row).Error; err != nil {
				return err
			}
			var shipment f.Shipment
			if supplierorder.DecodeObject(row.Body, &shipment) != nil || !shipmentMatchesBuyerOrder(shipment, order.Order) {
				return f.ErrInvalid
			}
			var prior struct{ Body json.RawMessage }
			err = tx.Table("supply_supplier_received_responses").Select("body").Where("tenant_id=? AND id=?", record.BuyerTenantID, record.ID).Take(&prior).Error
			if err == nil {
				var old f.ReceivingSharedResponse
				if supplierorder.DecodeObject(prior.Body, &old) != nil {
					return f.ErrUnavailable
				}
				if !reflect.DeepEqual(old, record) {
					return f.ErrConflict
				}
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			return tx.Exec(`INSERT INTO supply_supplier_received_responses(tenant_id,id,order_id,shipment_id,source_tenant_id,summary_version,body,source_event_id,created_at)VALUES(?,?,?,?,?,?,?::jsonb,?,?)`, record.BuyerTenantID, record.ID, record.OrderID, record.ShipmentID, record.OwnerTenantID, record.SummaryVersion, string(event.Data), event.ID, record.CreatedAt).Error
		})
	}}
}
func receivingResponseMatchesOrder(v f.ReceivingSharedResponse, order supplierorder.SupplierOrder) bool {
	seller, buyer := order.Terms.Seller, order.Terms.Buyer
	return v.OrderID == order.ID && v.OwnerTenantID == seller.TenantID && v.BuyerTenantID == buyer.TenantID && v.SourceProduct == seller.Product && v.SourceOrganizationID == seller.OrganizationID && v.SourceLocationID == seller.LocationID
}

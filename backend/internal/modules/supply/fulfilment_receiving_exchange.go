package supply

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract" //nolint:depguard // own module event contracts
	"github.com/tesserix/helivanta/pkg/events"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"gorm.io/gorm"
	"reflect"
	"time"
)

func receivingForwarder() events.Consumer {
	return events.Consumer{Name: "supply-supplier-receiving-forward", Subject: pharmacycontract.SubjectSupplierReceivingRecorded, Handle: func(_ context.Context, tx *gorm.DB, event events.Event) error {
		var data pharmacycontract.SupplierReceivingRecordedData
		if supplierorder.DecodeObject(event.Data, &data) != nil || data.Summary.OwnerTenantID.String() != event.TenantID || event.DestinationTenantID != "" || data.ShipmentID != data.Summary.ShipmentID || data.SourceTenantID == data.Summary.OwnerTenantID || event.Type != pharmacycontract.SubjectSupplierReceivingRecorded || event.Version != 1 {
			return f.ErrInvalid
		}
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		return events.Enqueue(tx, supplycontract.SubjectSupplierReceivingExchange, events.Event{ID: id.String(), Type: supplycontract.SubjectSupplierReceivingExchange, Version: 1, OccurredAt: time.Now().UTC(), TenantID: event.TenantID, DestinationTenantID: data.SourceTenantID.String(), Data: event.Data})
	}}
}
func receivingExchangeConsumer() events.Consumer {
	return events.Consumer{Name: "supply-supplier-receiving-exchange", Subject: supplycontract.SubjectSupplierReceivingExchange, Handle: func(_ context.Context, tx *gorm.DB, event events.Event) error {
		var data pharmacycontract.SupplierReceivingRecordedData
		if supplierorder.DecodeObject(event.Data, &data) != nil || data.Summary.OwnerTenantID.String() != event.TenantID || data.SourceTenantID.String() != event.DestinationTenantID || data.ShipmentID != data.Summary.ShipmentID || event.Type != supplycontract.SubjectSupplierReceivingExchange || event.Version != 1 {
			return f.ErrInvalid
		}
		v := f.ReceivingSummary(data.Summary)
		if v.Version < 1 || v.CountedUnits < 0 || v.AcceptedUnits < 0 || v.RejectedUnits < 0 || v.AcceptedUnits+v.RejectedUnits != v.CountedUnits || v.ShortUnits < 0 || !v.Closed && v.ShortUnits != 0 {
			return f.ErrInvalid
		}
		return purchaseOrderFrame(tx, map[string]string{"app.supply_order_mode": "exchange", "app.supply_order_id": v.OrderID.String(), "app.supply_order_origin": v.OwnerTenantID.String(), "app.actor_subject": "system:supplier-receiving"}, func() error {
			if err := purchaseOrderLock(tx, data.SourceTenantID.String(), v.OrderID); err != nil {
				return err
			}
			order, err := loadPurchaseOrder(tx, data.SourceTenantID.String(), v.OrderID)
			if err != nil {
				return err
			}
			if order.Order.Side != "seller" || order.Order.Terms.Buyer.TenantID != v.OwnerTenantID {
				return f.ErrInvalid
			}
			var dispatched struct{ Body json.RawMessage }
			if err = tx.Table("supply_supplier_dispatched_shipments").Select("body").Where("tenant_id=? AND id=? AND order_id=? AND buyer_tenant_id=?", data.SourceTenantID, v.ShipmentID, v.OrderID, v.OwnerTenantID).Take(&dispatched).Error; err != nil {
				return err
			}
			var shipment f.Shipment
			if supplierorder.DecodeObject(dispatched.Body, &shipment) != nil {
				return f.ErrUnavailable
			}
			if f.ValidateReceivingSummary(v, shipment) != nil {
				return f.ErrInvalid
			}
			var old struct{ Body json.RawMessage }
			err = tx.Table("supply_supplier_receiving_updates").Select("body").Where("tenant_id=? AND shipment_id=? AND version=?", data.SourceTenantID, v.ShipmentID, v.Version).Take(&old).Error
			if err == nil {
				var prior f.ReceivingSummary
				if supplierorder.DecodeObject(old.Body, &prior) != nil {
					return f.ErrUnavailable
				}
				if !reflect.DeepEqual(prior, v) {
					return f.ErrConflict
				}
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			raw, err := json.Marshal(v)
			if err != nil {
				return err
			}
			source := order.Order.Terms.Seller
			return tx.Exec(`INSERT INTO supply_supplier_receiving_updates(tenant_id,shipment_id,version,order_id,source_product,source_organization_id,source_location_id,buyer_tenant_id,body,source_event_id,actor_subject,created_at)VALUES(?,?,?,?,?,?,?,?,?::jsonb,?,?,?)`, data.SourceTenantID, v.ShipmentID, v.Version, v.OrderID, source.Product, source.OrganizationID, source.LocationID, v.OwnerTenantID, string(raw), event.ID, "system:supplier-receiving", v.RecordedAt).Error
		})
	}}
}

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

// dispatchForwarder publishes only after the stock owner's committed dispatch fact.
func dispatchForwarder(subject, owner string) events.Consumer {
	return events.Consumer{Name: "supply-" + owner + "-supplier-dispatch", Subject: subject, Handle: func(_ context.Context, tx *gorm.DB, event events.Event) error {
		var s f.Shipment
		if supplierorder.DecodeObject(event.Data, &s) != nil || f.ValidateShipment(s) != nil || s.Product.Owner != owner || s.SourceTenantID.String() != event.TenantID || event.DestinationTenantID != "" || event.Type != subject || event.Version != 1 {
			return errors.New("invalid owner dispatch provenance")
		}
		if err := retainSourceShipment(tx, s, event.Data); err != nil {
			return err
		}
		id, err := uuid.NewV7()
		if err != nil {
			return err
		}
		return events.Enqueue(tx, supplycontract.SubjectSupplierShipmentExchange, events.Event{ID: id.String(), Type: supplycontract.SubjectSupplierShipmentExchange, Version: 1, OccurredAt: time.Now().UTC(), TenantID: s.SourceTenantID.String(), DestinationTenantID: s.BuyerTenantID.String(), Data: event.Data})
	}}
}
func supplierShipmentConsumer() events.Consumer {
	return events.Consumer{Name: "supply-supplier-shipment-exchange", Subject: supplycontract.SubjectSupplierShipmentExchange, Handle: func(_ context.Context, tx *gorm.DB, event events.Event) error {
		var s f.Shipment
		if supplierorder.DecodeObject(event.Data, &s) != nil || f.ValidateShipment(s) != nil || s.SourceTenantID.String() != event.TenantID || s.BuyerTenantID.String() != event.DestinationTenantID || event.Type != supplycontract.SubjectSupplierShipmentExchange || event.Version != 1 {
			return errors.New("invalid supplier shipment provenance")
		}
		return purchaseOrderFrame(tx, map[string]string{"app.supply_order_mode": "exchange", "app.supply_order_id": s.OrderID.String(), "app.supply_order_origin": s.SourceTenantID.String(), "app.actor_subject": "system:supplier-shipments"}, func() error {
			if err := purchaseOrderLock(tx, s.BuyerTenantID.String(), s.OrderID); err != nil {
				return err
			}
			order, err := loadPurchaseOrder(tx, s.BuyerTenantID.String(), s.OrderID)
			if err != nil {
				return err
			}
			if !shipmentMatchesBuyerOrder(s, order.Order) {
				return f.ErrInvalid
			}
			var offered bool
			if err = tx.Raw(`SELECT EXISTS(SELECT 1 FROM supply_purchase_order_history WHERE tenant_id=? AND order_id=? AND body->>'status'='offered')`, s.BuyerTenantID, s.OrderID).Scan(&offered).Error; err != nil {
				return err
			}
			if !offered {
				return f.ErrConflict
			}
			var prior struct{ Body json.RawMessage }
			err = tx.Table("supply_supplier_shipments").Select("body").Where("tenant_id=? AND id=?", s.BuyerTenantID, s.ID).Take(&prior).Error
			if err == nil {
				var old f.Shipment
				if json.Unmarshal(prior.Body, &old) != nil {
					return f.ErrUnavailable
				}
				if !reflect.DeepEqual(old, s) {
					return f.ErrConflict
				}
				return nil
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			var used int64
			if err = tx.Table("supply_supplier_shipments").Select("COALESCE(SUM((body->>'quantity_units')::bigint),0)").Where("tenant_id=? AND order_id=? AND body->>'line_id'=?", s.BuyerTenantID, s.OrderID, s.LineID.String()).Scan(&used).Error; err != nil {
				return err
			}
			var limit int64
			for _, line := range order.Order.Input.Lines {
				if line.ID == s.LineID {
					limit = line.QuantityUnits
				}
			}
			if used < 0 || used > limit || s.QuantityUnits > limit-used {
				return f.ErrConflict
			}
			return tx.Exec(`INSERT INTO supply_supplier_shipments(tenant_id,id,order_id,hospital_id,department_id,pharmacy_id,outlet_id,source_tenant_id,source_product,body,actor_subject,created_at)VALUES(?,?,?,?,?,?,?,?,?,?::jsonb,?,?)`, s.BuyerTenantID, s.ID, s.OrderID, s.HospitalID, order.DepartmentID, s.BuyerOrganizationID, s.BuyerLocationID, s.SourceTenantID, s.Product.Owner, string(event.Data), "system:supplier-shipments", s.DispatchedAt).Error
		})
	}}
}
func shipmentMatchesBuyerOrder(s f.Shipment, o supplierorder.SupplierOrder) bool {
	if o.Side != "buyer" || o.ID != s.OrderID || o.OwnerTenantID != s.BuyerTenantID || o.InputSha256 != s.OrderInputSha256 || o.Terms.Buyer.HospitalID == nil || *o.Terms.Buyer.HospitalID != s.HospitalID || o.Terms.Buyer.OrganizationID != s.BuyerOrganizationID || o.Terms.Buyer.LocationID != s.BuyerLocationID || o.Terms.Seller.TenantID != s.SourceTenantID || o.Terms.Seller.OrganizationID != s.SourceOrganizationID || o.Terms.Seller.LocationID != s.SourceLocationID {
		return false
	}
	for _, line := range o.Input.Lines {
		if line.ID == s.LineID {
			return line.UnitCode == s.UnitCode && reflect.DeepEqual(line.Product, s.Product) && s.QuantityUnits <= line.QuantityUnits
		}
	}
	return false
}
func fulfilmentConsumers() []events.Consumer {
	return []events.Consumer{receivingForwarder(), receivingExchangeConsumer(), dispatchForwarder(supplycontract.SubjectSupplierDispatchRecorded, "supply"), dispatchForwarder(pharmacycontract.SubjectSupplierDispatchRecorded, "pharmacy"), supplierShipmentConsumer()}
}

func retainSourceShipment(tx *gorm.DB, s f.Shipment, raw json.RawMessage) error {
	return purchaseOrderFrame(tx, map[string]string{"app.supply_order_mode": "exchange", "app.supply_order_id": s.OrderID.String(), "app.supply_order_origin": s.BuyerTenantID.String(), "app.actor_subject": "system:supplier-shipments"}, func() error {
		if err := purchaseOrderLock(tx, s.SourceTenantID.String(), s.OrderID); err != nil {
			return err
		}
		local, err := loadPurchaseOrder(tx, s.SourceTenantID.String(), s.OrderID)
		if err != nil {
			return err
		}
		o := local.Order
		if o.Side != "seller" || o.OwnerTenantID != s.SourceTenantID {
			return f.ErrInvalid
		}
		o.Side = "buyer"
		o.OwnerTenantID = s.BuyerTenantID
		if !shipmentMatchesBuyerOrder(s, o) {
			return f.ErrInvalid
		}
		var old struct{ Body json.RawMessage }
		err = tx.Table("supply_supplier_dispatched_shipments").Select("body").Where("tenant_id=? AND id=?", s.SourceTenantID, s.ID).Take(&old).Error
		if err == nil {
			var prior f.Shipment
			if supplierorder.DecodeObject(old.Body, &prior) != nil {
				return f.ErrUnavailable
			}
			if !reflect.DeepEqual(prior, s) {
				return f.ErrConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return tx.Exec(`INSERT INTO supply_supplier_dispatched_shipments(tenant_id,id,order_id,buyer_tenant_id,body,created_at)VALUES(?,?,?,?,?::jsonb,?)`, s.SourceTenantID, s.ID, s.OrderID, s.BuyerTenantID, string(raw), s.DispatchedAt).Error
	})
}

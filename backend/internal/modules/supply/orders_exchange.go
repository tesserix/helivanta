package supply

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract" //nolint:depguard // own data-only event contract
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"gorm.io/gorm"
)

func (h *purchaseOrderHandlers) publishOrderExchange(tx *gorm.DB, input supplierorder.SupplierOrderExchange) error {
	if h.bus == nil {
		return supplierorder.ErrUnavailable
	}
	return enqueueOrderExchange(tx, input)
}
func enqueueOrderExchange(tx *gorm.DB, input supplierorder.SupplierOrderExchange) error {
	destination := input.Terms.Seller.TenantID
	if input.SourceTenantID == destination {
		destination = input.Terms.Buyer.TenantID
	} else if input.SourceTenantID != input.Terms.Buyer.TenantID {
		return supplierorder.ErrInvalid
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	return events.Enqueue(tx, supplycontract.SubjectOrderExchange, events.Event{ID: id.String(), Type: supplycontract.SubjectOrderExchange, Version: 1, OccurredAt: time.Now().UTC(), TenantID: input.SourceTenantID.String(), DestinationTenantID: destination.String(), Data: raw})
}
func purchaseOrderConsumer() events.Consumer {
	return events.Consumer{Name: "supply-purchase-order-exchange", Subject: supplycontract.SubjectOrderExchange, Handle: func(_ context.Context, tx *gorm.DB, event events.Event) error {
		var input supplierorder.SupplierOrderExchange
		source, sourceErr := uuid.Parse(event.TenantID)
		destination, destErr := uuid.Parse(event.DestinationTenantID)
		eventID, eventErr := uuid.Parse(event.ID)
		if sourceErr != nil || destErr != nil || eventErr != nil || eventID.Version() != 7 || eventID.Variant() != uuid.RFC4122 || source == uuid.Nil || destination == uuid.Nil || source == destination || event.Type != supplycontract.SubjectOrderExchange || event.Version != 1 || suppliertrade.DecodeObject(event.Data, &input) != nil || input.SourceTenantID != source {
			return errors.New("invalid purchase order exchange provenance")
		}
		if err := purchaseOrderLock(tx, destination.String(), input.OrderID); err != nil {
			return err
		}
		return purchaseOrderFrame(tx, map[string]string{"app.supply_order_mode": "exchange", "app.supply_order_id": input.OrderID.String(), "app.supply_order_origin": source.String(), "app.actor_subject": "system:supplier-orders"}, func() error {
			relationship, err := loadTradeRelationship(tx, destination, input.Input.RelationshipID)
			if err != nil {
				return err
			}
			digest, err := suppliertrade.TermsDigest(input.Terms)
			if err != nil || relationship.TermsSha256 != digest || input.Input.TermsSha256 != digest {
				return supplierorder.ErrInvalid
			}
			var established bool
			if err = tx.Raw(`SELECT EXISTS(SELECT 1 FROM supply_trade_relationship_history WHERE tenant_id=? AND relationship_id=? AND body->>'status'='active')`, destination, relationship.ID).Scan(&established).Error; err != nil {
				return err
			}
			if !established {
				return supplierorder.ErrIneligible
			}
			previous, err := loadPurchaseOrder(tx, destination.String(), input.OrderID)
			var current *supplierorder.SupplierOrder
			if err == nil {
				current = &previous.Order
			} else if !purchaseOrderMissing(err) {
				return err
			}
			next, err := supplierorder.Apply(current, input, destination, purchaseOrderNow())
			if err != nil {
				return err
			}
			if current != nil && next.Version == current.Version {
				return nil
			}
			previous.Order = next
			return savePurchaseOrder(tx, previous, "system:supplier-orders", nil, &eventID)
		})
	}}
}

package platform

import (
	"context"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
)

type shipmentSafetyProvider interface{ ShipmentSafety(Deps) f.ShipmentSafety }
type shipmentSafety struct{ owners map[string]f.ShipmentSafety }

func (s shipmentSafety) WithSafeShipment(ctx context.Context, shipment f.Shipment, action func(context.Context) error) error {
	owner := s.owners[shipment.Product.Owner]
	if owner == nil {
		return f.ErrUnavailable
	}
	return owner.WithSafeShipment(ctx, shipment, action)
}
func SupplierShipmentSafety(modules []Module, deps Deps) f.ShipmentSafety {
	result := shipmentSafety{owners: map[string]f.ShipmentSafety{}}
	for _, name := range []string{"supply", "pharmacy"} {
		if owner, ok := soleOwner(modules, deps.DB, name).(shipmentSafetyProvider); ok {
			result.owners[name] = owner.ShipmentSafety(deps)
		}
	}
	return result
}

package supply

import (
	"context"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	f "github.com/tesserix/helivanta/pkg/supplierfulfilment"
	"github.com/tesserix/helivanta/pkg/supplierorder"
	"gorm.io/gorm"
)

const PharmacyHospitalFulfilmentUse authz.Permission = "pharmacy.hospital_fulfilment.use"

type hospitalSourceAuthority struct{ h *purchaseOrderHandlers }

func (*Module) HospitalSourceAuthority(deps platform.Deps) f.HospitalSourceAuthority {
	return hospitalSourceAuthority{h: &purchaseOrderHandlers{db: deps.DB, authority: deps.ScopedAuthorizer, actors: deps.PurchaseOrderActors, departments: deps.DepartmentScopes, endpoints: deps.TradingEndpoints}}
}
func (g hospitalSourceAuthority) WithOutlet(ctx context.Context, p authn.Principal, hospital, department, outlet uuid.UUID, action func(context.Context, supplierorder.Actor) error) error {
	if action == nil {
		return f.ErrUnavailable
	}
	return g.h.buyer(ctx, p, buyerOrderScope{hospital: hospital, department: department, outlet: outlet}, PharmacyHospitalFulfilmentUse, func(ctx context.Context, _ *gorm.DB, a supplierorder.Actor) error {
		return action(ctx, a)
	})
}

package platform

import f "github.com/tesserix/helivanta/pkg/supplierfulfilment"

type fulfilmentOrderProvider interface {
	FulfilmentOrders(Deps) f.CurrentOrderGuard
}

// FulfilmentOrders resolves only SupplyConnect's authoritative bilateral order publication.
func FulfilmentOrders(modules []Module, deps Deps) f.CurrentOrderGuard {
	owner, ok := soleOwner(modules, deps.DB, "supply").(fulfilmentOrderProvider)
	if !ok {
		return nil
	}
	return owner.FulfilmentOrders(deps)
}

type receivingAuthorityProvider interface {
	ReceivingAuthority(Deps) f.ReceivingAuthority
}

func ReceivingAuthority(modules []Module, deps Deps) f.ReceivingAuthority {
	owner, ok := soleOwner(modules, deps.DB, "supply").(receivingAuthorityProvider)
	if !ok {
		return nil
	}
	return owner.ReceivingAuthority(deps)
}

type hospitalSourceProvider interface {
	HospitalSourceAuthority(Deps) f.HospitalSourceAuthority
}

func HospitalSourceAuthority(modules []Module, deps Deps) f.HospitalSourceAuthority {
	owner, ok := soleOwner(modules, deps.DB, "supply").(hospitalSourceProvider)
	if !ok {
		return nil
	}
	return owner.HospitalSourceAuthority(deps)
}

package platform

import (
	"context"

	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/onboarding-service/api/customercontract"
)

// StaffMFATargetReader returns after Workforce's scoped transaction has closed.
type StaffMFATargetReader func(context.Context, customercontract.StaffMFAResetInput) (customercontract.StaffMFARecoveryTarget, error)

// StaffMFARecoveryService owns administrator authorization and application revocation.
type StaffMFARecoveryService interface {
	Reset(context.Context, authn.Principal, customercontract.StaffMFAResetInput, StaffMFATargetReader) (customercontract.StaffMFARecoveryReceipt, error)
	Cancel(context.Context, authn.Principal, string, func(customercontract.StaffMFARecoveryTarget) bool) (customercontract.StaffMFARecoveryReceipt, error)
	Read(context.Context, authn.Principal, string, StaffMFATargetReader) (customercontract.StaffMFARecoveryReceipt, error)
}

type staffMFARecoveryProvider interface {
	StaffMFARecovery(Deps) StaffMFARecoveryService
}

// StaffMFARecovery resolves the IAM owner without sharing its repositories.
func StaffMFARecovery(modules []Module, deps Deps) StaffMFARecoveryService {
	for _, module := range modules {
		if module.Name() == "iam" {
			if owner, ok := module.(staffMFARecoveryProvider); ok {
				return owner.StaffMFARecovery(deps)
			}
		}
	}
	return nil
}

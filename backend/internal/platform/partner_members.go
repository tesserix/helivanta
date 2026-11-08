package platform

import (
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type partnerMemberProvider interface {
	PartnerMembers(*tenantdb.DB, authz.CurrentStaffActorGuard, authz.CurrentStaffPostingSourceGuard) authz.CurrentPartnerMemberGuard
}

func PartnerMembers(modules []Module, db *tenantdb.DB) authz.CurrentPartnerMemberGuard {
	for _, module := range modules {
		if module.Name() != "workforce" {
			continue
		}
		if owner, ok := module.(partnerMemberProvider); ok {
			return owner.PartnerMembers(db, StaffActors(modules, db), StaffPostingSources(modules, db))
		}
	}
	return nil
}

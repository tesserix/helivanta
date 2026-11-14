package authz

import (
	"context"
	"github.com/tesserix/helivanta/pkg/authn"
)

// SourceAdministrationGuard holds the requested current tenant-level administrative capability and session authority.
// It does not authorize operational Staff work, a foreign source or regulated activity.
type SourceAdministrationGuard interface {
	WithSourceAdministration(context.Context, authn.Principal, Permission, func(context.Context) error) error
}

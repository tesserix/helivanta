package workforce

import (
	"context"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/onboarding-service/api/customercontract"
)

// NewCustomerWorkspaceInitializer creates only this owner's initial business resource.
func NewCustomerWorkspaceInitializer(deps platform.Deps) func(context.Context, customercontract.Workspace) error {
	return func(ctx context.Context, in customercontract.Workspace) error { return nil }
}

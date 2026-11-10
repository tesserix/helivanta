package iam

import (
	"context"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/onboarding-service/api/customercontract"
)

// NewCustomerWorkspaceAuthority holds the existing initial owner's current IAM authority.
func NewCustomerWorkspaceAuthority(deps platform.Deps) func(context.Context, customercontract.Workspace, func(context.Context) error) error {
	return func(ctx context.Context, in customercontract.Workspace, action func(context.Context) error) error {
		return nil
	}
}

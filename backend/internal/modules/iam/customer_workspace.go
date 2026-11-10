package iam

import (
	"context"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"github.com/tesserix/onboarding-service/api/customercontract"
	"gorm.io/gorm"
	"time"
)

// NewCustomerWorkspaceAuthority holds the existing initial owner's current IAM authority.
func NewCustomerWorkspaceAuthority(deps platform.Deps) func(context.Context, customercontract.Workspace, func(context.Context) error) error {
	return func(ctx context.Context, in customercontract.Workspace, action func(context.Context) error) error {
		if deps.DB == nil || action == nil {
			return authz.ErrScopedAuthorizationUnavailable
		}
		if in.Validate() != nil {
			return customercontract.ErrInvalid
		}
		ctx = tenantdb.WithActor(ctx, hospitalProvisioningActor)
		return deps.DB.WithGuardedTenantCommand(ctx, in.TenantID, time.Now().Add(10*time.Second), func(tx *gorm.DB) error {
			current, err := assignmentAdministrator(tx, in.TenantID, in.OwnerSubject)
			if err != nil {
				return err
			}
			if !current {
				return authz.ErrScopedCommandDenied
			}
			var verified bool
			if err = tx.Raw(`SELECT EXISTS(SELECT 1 FROM audit_events WHERE tenant_id=? AND resource_type='hospital_onboarding' AND resource_id=? AND action='membership_verified')`, in.TenantID, in.TenantID).Scan(&verified).Error; err != nil {
				return err
			}
			if !verified {
				return authz.ErrScopedCommandDenied
			}
			return nil
		}, action)
	}
}

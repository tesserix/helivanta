package customeronboarding

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/onboarding-service/api/customercontract"
	"gorm.io/gorm"
	"time"
)

// WorkloadVerifier verifies the provisioner's purpose-bound Kubernetes identity.
type WorkloadVerifier interface {
	Verify(context.Context, string) error
}

// NewWorkspaceHandler composes current IAM authority with the two business owners.
func NewWorkspaceHandler(caller WorkloadVerifier, authority func(context.Context, customercontract.Workspace, func(context.Context) error) error, workforce, pharmacy func(context.Context, customercontract.Workspace) error) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		credential, authorization := c.GetHeader("X-Onboarding-Token"), c.GetHeader("Authorization")
		if len(c.Request.Header.Values("X-Onboarding-Token")) > 1 || len(c.Request.Header.Values("Authorization")) > 1 || (credential != "" && authorization != "") {
			respond.Unauthenticated(c, "ambiguous provisioning credentials")
			return
		}
		if credential == "" {
			credential = authorization
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
		defer cancel()
		if caller == nil || caller.Verify(ctx, credential) != nil {
			respond.Unauthenticated(c, "provisioning credential required")
			return
		}
		var in customercontract.Workspace
		if !decode(c, &in) {
			return
		}
		if in.Validate() != nil {
			respond.Error(c, 400, "invalid_request", "invalid initial workspace binding")
			return
		}
		if authority == nil || workforce == nil || pharmacy == nil {
			unavailable(c)
			return
		}
		err := authority(ctx, in, func(current context.Context) error {
			if in.Kind == customercontract.Pharmacy {
				if err := pharmacy(current, in); err != nil {
					return err
				}
			}
			return workforce(current, in)
		})
		switch {
		case errors.Is(err, authz.ErrScopedCommandDenied):
			respond.Forbidden(c, "current tenant owner required")
		case errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, authz.ErrPharmacyEmployerHidden):
			respond.NotFound(c, "workspace")
		case err != nil:
			unavailable(c)
		default:
			respond.OK(c, customercontract.Receipt{State: customercontract.Ready, Name: in.Name, Kind: in.Kind, TenantID: in.TenantID, ResourceID: in.ResourceID})
		}
	}
}

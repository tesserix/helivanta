package authz

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/pkg/authn"
)

// WithHospitalScopeCommand runs synchronous product work under current authority.
func WithHospitalScopeCommand(c *gin.Context, permission Permission, hospital string, action func(context.Context) error) (err error) {
	const failureKey = "helivanta.hospital_scope_command_failure"
	if previous, ok := c.Get(failureKey); ok {
		return previous.(error)
	}
	defer func() {
		if err != nil {
			c.Set(failureKey, err)
		}
	}()
	if action == nil {
		return ErrScopedAuthorizationUnavailable
	}
	grant, scoped := HospitalScopeGrant(c, permission, hospital)
	if !scoped {
		permissions, ok := PermissionsFrom(c)
		if !ok || !permissions.Has(permission) {
			return ErrScopedCommandDenied
		}
		return action(c.Request.Context())
	}
	if grant.AssignmentID == "" {
		return ErrScopedCommandDenied
	}
	value, _ := c.Get(hospitalScopeKey)
	proof := value.(hospitalScopeAuthorization)
	guard, ok := proof.authority.(ScopedCommandGuard)
	if !ok {
		c.Set(hospitalScopeKey, hospitalScopeAuthorization{})
		c.Set(hospitalScopeFailureKey, true)
		return ErrScopedAuthorizationUnavailable
	}
	p, _ := authn.PrincipalFrom(c)
	err = guard.WithScopeGrant(c.Request.Context(), proof.request, grant, p.AuthTime, action)
	if err != nil {
		c.Set(hospitalScopeKey, hospitalScopeAuthorization{})
		c.Set(hospitalScopeFailureKey, errors.Is(err, ErrScopedAuthorizationUnavailable))
	}
	return err
}

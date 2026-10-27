package authz

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
)

const hospitalScopeKey = "helivanta.hospital_scope_authorization"
const hospitalScopeFailureKey = "helivanta.hospital_scope_unavailable"

type hospitalScopeAuthorization struct {
	request ScopeRequest
	grant   ScopeGrant
}

// RequireHospitalScope accepts either the legacy route permission or current scoped authority.
// The product handler must still check the corresponding live owner scope and object state.
func RequireHospitalScope(permission Permission, hospitalParam string, authority ScopedAuthorizer) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, _, ok := authn.TenantPrincipal(c)
		if !ok {
			return
		}
		hospital := c.Param(hospitalParam)
		id, err := uuid.Parse(hospital)
		if err != nil || id == uuid.Nil || id.String() != hospital {
			respond.Error(c, http.StatusBadRequest, "invalid_scope", "A valid hospital scope is required.")
			c.Abort()
			return
		}
		permissions, initialized := PermissionsFrom(c)
		if !initialized {
			respond.InternalErr(c, errors.New("permission set missing from context"), "authorization not initialized")
			return
		}
		legacy := permissions.Has(permission)
		request := ScopeRequest{TenantID: p.TenantID, Subject: p.Subject, HospitalID: hospital, Permission: permission}
		var grant ScopeGrant
		if authority == nil {
			err = ErrScopedAuthorizationUnavailable
		} else {
			grant, err = authority.AuthorizeScope(c.Request.Context(), request)
		}
		if err == nil && grant.AssignmentID != "" {
			assignment, e := uuid.Parse(grant.AssignmentID)
			if e != nil || assignment.Version() != 7 || assignment.String() != grant.AssignmentID || grant.Version <= 0 || grant.ScopeEpoch <= 0 || grant.MembershipEpoch <= 0 {
				err = ErrScopedAuthorizationUnavailable
			}
		}
		if err != nil {
			c.Set(hospitalScopeFailureKey, true)
			if !legacy {
				respond.Error(c, http.StatusServiceUnavailable, "scoped_authorization_unavailable", "Scoped authorization is unavailable.")
				c.Abort()
				return
			}
		} else if grant.AssignmentID != "" {
			c.Set(hospitalScopeKey, hospitalScopeAuthorization{request: request, grant: grant})
		} else if !legacy {
			respond.Error(c, http.StatusForbidden, "forbidden", "You do not have permission to perform this action.")
			c.Abort()
			return
		}
		c.Next()
	}
}

// HospitalScopeGrant returns request-local evidence bound to the verified principal and action.
func HospitalScopeGrant(c *gin.Context, permission Permission, hospital string) (ScopeGrant, bool) {
	value, ok := c.Get(hospitalScopeKey)
	if !ok {
		return ScopeGrant{}, false
	}
	proof, ok := value.(hospitalScopeAuthorization)
	if !ok {
		return ScopeGrant{}, false
	}
	p, ok := authn.PrincipalFrom(c)
	if !ok || p.TenantID != proof.request.TenantID || p.Subject != proof.request.Subject || hospital != proof.request.HospitalID || permission != proof.request.Permission {
		return ScopeGrant{}, false
	}
	return proof.grant, true
}

// HospitalScopeUnavailable distinguishes a failed scoped lookup from a scoped denial.
// A legacy-permission caller may proceed only if its independent product scope check succeeds.
func HospitalScopeUnavailable(c *gin.Context) bool { return c.GetBool(hospitalScopeFailureKey) }

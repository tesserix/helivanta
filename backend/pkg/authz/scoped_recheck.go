package authz

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RecheckHospitalScope refreshes scoped authority after command input is decoded.
func RecheckHospitalScope(c *gin.Context, permission Permission, hospital string) (ScopeGrant, error) {
	if _, ok := HospitalScopeGrant(c, permission, hospital); !ok {
		if HospitalScopeUnavailable(c) {
			return ScopeGrant{}, ErrScopedAuthorizationUnavailable
		}
		return ScopeGrant{}, nil
	}
	value, _ := c.Get(hospitalScopeKey)
	proof := value.(hospitalScopeAuthorization)
	// An unsuccessful refresh must also invalidate the earlier request evidence.
	c.Set(hospitalScopeKey, hospitalScopeAuthorization{})
	if proof.authority == nil {
		c.Set(hospitalScopeFailureKey, true)
		return ScopeGrant{}, ErrScopedAuthorizationUnavailable
	}
	grant, err := proof.authority.AuthorizeScope(c.Request.Context(), proof.request)
	if err == nil {
		err = validateScopeGrant(grant)
	}
	if err != nil {
		c.Set(hospitalScopeFailureKey, true)
		return ScopeGrant{}, err
	}
	c.Set(hospitalScopeFailureKey, false)
	if grant.AssignmentID != "" {
		proof.grant = grant
		c.Set(hospitalScopeKey, proof)
	}
	return grant, nil
}

func validateScopeGrant(grant ScopeGrant) error {
	if grant.AssignmentID == "" {
		return nil
	}
	id, err := uuid.Parse(grant.AssignmentID)
	if err != nil || id.Version() != 7 || id.String() != grant.AssignmentID || grant.Version <= 0 || grant.ScopeEpoch <= 0 || grant.MembershipEpoch <= 0 {
		return ErrScopedAuthorizationUnavailable
	}
	return nil
}

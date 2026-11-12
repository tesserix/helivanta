package loginclient

import (
	"context"
	"regexp"

	"github.com/google/uuid"
)

type IdentityReadPurpose uint8

const (
	IdentityReadStatus IdentityReadPurpose = iota + 1
	IdentityReadMembership
	IdentityReadProfile
)

// IdentitySnapshot is an internal view of a scoped identity-owner projection.
type IdentitySnapshot struct {
	State              UserState
	MembershipEligible bool
	Profile            *UserProfile
}

type TenantIdentityResolver interface {
	TenantIdentitySnapshot(context.Context, string, string, IdentityReadPurpose) (IdentitySnapshot, error)
}

func (c *Client) SetTenantIdentityResolver(resolver TenantIdentityResolver) {
	c.tenantIdentity = resolver
}

var identitySubject = regexp.MustCompile(`^[A-Za-z0-9_-]{1,200}$`)

func (c *Client) identitySnapshot(ctx context.Context, tenant, subject string, purpose IdentityReadPurpose) (IdentitySnapshot, error) {
	var empty IdentitySnapshot
	tenantID, err := uuid.Parse(tenant)
	if c == nil || c.tenantIdentity == nil || ctx.Err() != nil || err != nil || tenantID == uuid.Nil || tenantID.String() != tenant || !identitySubject.MatchString(subject) {
		return empty, ErrUnavailable
	}
	if purpose != IdentityReadStatus && purpose != IdentityReadMembership && purpose != IdentityReadProfile {
		return empty, ErrUnavailable
	}
	snapshot, err := c.tenantIdentity.TenantIdentitySnapshot(ctx, tenant, subject, purpose)
	if err != nil || ctx.Err() != nil {
		return empty, ErrUnavailable
	}
	if snapshot.State != UserStateActive && snapshot.State != UserStateInactive {
		return empty, ErrUnavailable
	}
	if snapshot.MembershipEligible && (purpose != IdentityReadMembership || snapshot.State != UserStateActive) {
		return empty, ErrUnavailable
	}
	if snapshot.Profile != nil && (purpose != IdentityReadProfile || snapshot.State != UserStateActive) {
		return empty, ErrUnavailable
	}
	return snapshot, nil
}

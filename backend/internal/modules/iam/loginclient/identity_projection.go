package loginclient

import (
	"context"
	"regexp"

	"github.com/google/uuid"
	"github.com/tesserix/onboarding-service/api/customercontract"
)

type IdentityReadPurpose uint8

const (
	IdentityReadStatus IdentityReadPurpose = iota + 1
	IdentityReadMembership
	IdentityReadProfile
	IdentityReadAssignmentCandidate
)

// IdentitySnapshot is an internal view of a scoped identity-owner projection.
type IdentitySnapshot struct {
	State              UserState
	MembershipEligible bool
	Profile            *UserProfile
	Candidate          *customercontract.AssignmentCandidateIdentity
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
	if purpose != IdentityReadStatus && purpose != IdentityReadMembership && purpose != IdentityReadProfile && purpose != IdentityReadAssignmentCandidate {
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
	if snapshot.Candidate != nil && (purpose != IdentityReadAssignmentCandidate || snapshot.State != UserStateActive || snapshot.Candidate.Validate() != nil) {
		return empty, ErrUnavailable
	}
	return snapshot, nil
}

// AssignmentCandidateIdentity reads the separate owned-account selection purpose.
func (c *Client) AssignmentCandidateIdentity(ctx context.Context, tenant, subject string) (*customercontract.AssignmentCandidateIdentity, error) {
	snapshot, err := c.identitySnapshot(ctx, tenant, subject, IdentityReadAssignmentCandidate)
	if err != nil {
		return nil, err
	}
	return snapshot.Candidate, nil
}

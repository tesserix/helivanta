package loginclient

import (
	"context"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/onboarding-service/api/customercontract"
)

type IdentityReadPurpose uint8

const (
	IdentityReadStatus IdentityReadPurpose = iota + 1
	IdentityReadMembership
	IdentityReadProfile
	IdentityReadAssignmentCandidate
	IdentityReadCredentials
)

// IdentitySnapshot is an internal view of a scoped identity-owner projection.
type IdentitySnapshot struct {
	State              UserState
	MembershipEligible bool
	Profile            *UserProfile
	Candidate          *customercontract.AssignmentCandidateIdentity
	Credentials        *customercontract.IdentityCredentialState
	MFARecovery        *customercontract.StaffMFARecoveryRequirement
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
	if purpose != IdentityReadStatus && purpose != IdentityReadMembership && purpose != IdentityReadProfile && purpose != IdentityReadAssignmentCandidate && purpose != IdentityReadCredentials {
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
	if snapshot.Credentials != nil && (purpose != IdentityReadCredentials || snapshot.State != UserStateActive || snapshot.Credentials.Validate() != nil) {
		return empty, ErrUnavailable
	}
	if purpose == IdentityReadCredentials && snapshot.State == UserStateActive && snapshot.Credentials == nil {
		return empty, ErrUnavailable
	}
	if snapshot.MFARecovery != nil && (purpose != IdentityReadCredentials || snapshot.State != UserStateActive || snapshot.MFARecovery.Validate() != nil) {
		return empty, ErrUnavailable
	}
	if purpose == IdentityReadCredentials && snapshot.State == UserStateActive && snapshot.MFARecovery == nil {
		return empty, ErrUnavailable
	}
	return snapshot, nil
}

// CredentialStatus contains current native evidence, never a password or factor.
type CredentialStatus struct {
	State             UserState
	PasswordChangedAt time.Time
	MFARecovery       *customercontract.StaffMFARecoveryRequirement
}

func (c *Client) UserCredentialState(ctx context.Context, tenant, subject string) (CredentialStatus, error) {
	snapshot, err := c.identitySnapshot(ctx, tenant, subject, IdentityReadCredentials)
	if err != nil {
		return CredentialStatus{}, err
	}
	out := CredentialStatus{State: snapshot.State, MFARecovery: snapshot.MFARecovery}
	if snapshot.Credentials != nil && snapshot.Credentials.PasswordConfigured {
		out.PasswordChangedAt, err = time.Parse(time.RFC3339Nano, snapshot.Credentials.PasswordChangedAt)
		if err != nil {
			return CredentialStatus{}, ErrUnavailable
		}
	}
	return out, nil
}

// AssignmentCandidateIdentity reads the separate owned-account selection purpose.
func (c *Client) AssignmentCandidateIdentity(ctx context.Context, tenant, subject string) (*customercontract.AssignmentCandidateIdentity, error) {
	snapshot, err := c.identitySnapshot(ctx, tenant, subject, IdentityReadAssignmentCandidate)
	if err != nil {
		return nil, err
	}
	return snapshot.Candidate, nil
}

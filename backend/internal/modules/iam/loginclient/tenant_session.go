package loginclient

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

const organizationScopePrefix = "urn:zitadel:iam:org:id:"

// TenantUserResolver is the private identity owner's organisation-scoped lookup.
type TenantUserResolver interface {
	TenantLoginSubject(context.Context, string, string) (string, error)
}

// SetTenantUserResolver wires the broker without expanding the native login PAT.
func (c *Client) SetTenantUserResolver(resolver TenantUserResolver) { c.tenantUsers = resolver }

// CreatePasswordSessionForRequest resolves email within the native request's org.
func (c *Client) CreatePasswordSessionForRequest(ctx context.Context, requestID, clientID, loginName, password string) (Session, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()
	ar, err := c.AuthRequest(ctx, requestID)
	if err != nil {
		return Session{}, err
	}
	if clientID == "" || ar.ID != requestID || ar.ClientID != clientID {
		return Session{}, ErrAuthRequestInvalid
	}
	org := ""
	for _, scope := range ar.Scope {
		if strings.HasPrefix(scope, "urn:zitadel:iam:org:domain:") {
			return Session{}, ErrAuthRequestInvalid
		}
		if candidate, ok := strings.CutPrefix(scope, organizationScopePrefix); ok {
			tenant, err := uuid.Parse(strings.TrimPrefix(candidate, "helivanta-"))
			if err != nil || tenant == uuid.Nil || candidate != "helivanta-"+tenant.String() || org != "" {
				return Session{}, ErrAuthRequestInvalid
			}
			org = candidate
		}
	}
	if org == "" {
		return c.CreatePasswordSession(ctx, loginName, password)
	}
	if c.tenantUsers == nil {
		return Session{}, ErrUnavailable
	}
	userID, err := c.tenantUsers.TenantLoginSubject(ctx, org, loginName)
	if err != nil {
		return Session{}, err
	}
	session, err := c.createPasswordSession(ctx, map[string]any{"userId": userID}, password)
	if err != nil {
		return Session{}, err
	}
	subject, err := c.sessionSubject(ctx, session.ID)
	if err != nil {
		return Session{}, err
	}
	if subject.OrgID != org || subject.UserID != userID {
		return Session{}, ErrAuthRequestInvalid
	}
	return session, nil
}

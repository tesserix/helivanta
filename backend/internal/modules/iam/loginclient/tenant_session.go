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

// CreatePasswordSessionForRequest resolves loginName within the native
// request's org. On an unscoped request an email is resolved through the
// identity owner and may yield one accepted session per account (#1751 spec
// D2); a scoped request yields at most one.
func (c *Client) CreatePasswordSessionForRequest(ctx context.Context, requestID, clientID, loginName, password string) ([]TenantSession, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()
	org, err := c.AuthRequestOrganization(ctx, requestID, clientID)
	if err != nil {
		return nil, err
	}
	if org == "" && strings.Contains(loginName, "@") {
		return c.emailSessions(ctx, loginName, password)
	}
	if org == "" {
		return oneSession(c.CreatePasswordSession(ctx, loginName, password))
	}
	if c.tenantUsers == nil {
		return nil, ErrUnavailable
	}
	userID, err := c.tenantUsers.TenantLoginSubject(ctx, org, loginName)
	if err != nil {
		return nil, err
	}
	session, err := c.createPasswordSession(ctx, map[string]any{"userId": userID}, password)
	if err != nil {
		return nil, err
	}
	subject, err := c.sessionSubject(ctx, session.ID)
	if err != nil {
		return nil, err
	}
	if subject.OrgID != org || subject.UserID != userID {
		return nil, ErrAuthRequestInvalid
	}
	return []TenantSession{{Session: session}}, nil
}

func oneSession(session Session, err error) ([]TenantSession, error) {
	if err != nil {
		return nil, err
	}
	return []TenantSession{{Session: session}}, nil
}

// AuthRequestOrganization validates this deployment's native request before selecting its organization.
func (c *Client) AuthRequestOrganization(ctx context.Context, requestID, clientID string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()
	ar, err := c.AuthRequest(ctx, requestID)
	if err != nil {
		return "", err
	}
	if clientID == "" || ar.ID != requestID || ar.ClientID != clientID {
		return "", ErrAuthRequestInvalid
	}
	org := ""
	for _, scope := range ar.Scope {
		if strings.HasPrefix(scope, "urn:zitadel:iam:org:domain:") {
			return "", ErrAuthRequestInvalid
		}
		if candidate, ok := strings.CutPrefix(scope, organizationScopePrefix); ok {
			tenant, err := uuid.Parse(strings.TrimPrefix(candidate, "helivanta-"))
			if err != nil || tenant == uuid.Nil || candidate != "helivanta-"+tenant.String() || org != "" {
				return "", ErrAuthRequestInvalid
			}
			org = candidate
		}
	}
	return org, nil
}

// CreateUserPasswordSession checks password for the exact native user and
// organisation a setup step has just proven (#1778): a session for any other
// account is refused before it is used.
func (c *Client) CreateUserPasswordSession(ctx context.Context, userID, orgID, password string) (Session, error) {
	if userID == "" || orgID == "" {
		return Session{}, ErrAuthRequestInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()
	session, err := c.createPasswordSession(ctx, map[string]any{"userId": userID}, password)
	if err != nil {
		return Session{}, err
	}
	if err = c.SessionBelongsTo(ctx, session.ID, userID, orgID); err != nil {
		return Session{}, err
	}
	return session, nil
}

// SessionBelongsTo re-reads the session and requires its user, and its
// organisation unless orgID is empty (an unscoped auth request).
func (c *Client) SessionBelongsTo(ctx context.Context, sessionID, userID, orgID string) error {
	subject, err := c.sessionSubject(ctx, sessionID)
	if err != nil {
		return err
	}
	if subject.UserID != userID || orgID != "" && subject.OrgID != orgID {
		return ErrAuthRequestInvalid
	}
	return nil
}

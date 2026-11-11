package loginclient

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

const organizationScopePrefix = "urn:zitadel:iam:org:id:"

// CreatePasswordSessionForRequest resolves email within the native request's org.
func (c *Client) CreatePasswordSessionForRequest(ctx context.Context, requestID, clientID, loginName, password string) (Session, error) {
	ar, err := c.AuthRequest(ctx, requestID)
	if err != nil {
		return Session{}, err
	}
	if clientID == "" || ar.ID != requestID || ar.ClientID != clientID {
		return Session{}, ErrAuthRequestInvalid
	}
	org := ""
	for _, scope := range ar.Scope {
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
	userID, err := c.tenantUserByEmail(ctx, org, loginName)
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

func (c *Client) tenantUserByEmail(ctx context.Context, org, email string) (string, error) {
	var wire struct {
		Result []struct {
			ID      string `json:"userId"`
			State   string `json:"state"`
			Details struct {
				ResourceOwner string `json:"resourceOwner"`
			} `json:"details"`
			Human *struct {
				Email struct {
					Address  string `json:"email"`
					Verified bool   `json:"isVerified"`
				} `json:"email"`
			} `json:"human"`
		} `json:"result"`
	}
	input := map[string]any{"query": map[string]string{"limit": "2"}, "queries": []map[string]any{
		{"organizationIdQuery": map[string]string{"organizationId": org}},
		{"emailQuery": map[string]string{"emailAddress": email, "method": "TEXT_QUERY_METHOD_EQUALS_IGNORE_CASE"}},
	}}
	if err := c.do(ctx, "POST", "/v2/users", input, &wire, ErrUserNotFound); err != nil {
		return "", err
	}
	if len(wire.Result) != 1 {
		return "", ErrUserNotFound
	}
	u := wire.Result[0]
	if u.ID == "" || u.State != "USER_STATE_ACTIVE" || u.Details.ResourceOwner != org || u.Human == nil || !u.Human.Email.Verified || !strings.EqualFold(u.Human.Email.Address, email) {
		return "", ErrUserNotFound
	}
	return u.ID, nil
}

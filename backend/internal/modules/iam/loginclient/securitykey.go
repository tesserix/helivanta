package loginclient

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// SecurityKeyLoginURL continues the same request on its existing relying-party
// origin (ADR-0069). It neither finalizes nor exports the password-only session.
func (c *Client) SecurityKeyLoginURL(ctx context.Context, requestID string, s Session) (string, error) {
	issuer, err := url.Parse(c.baseURL)
	if err != nil || issuer.Scheme != "https" || issuer.Host == "" || issuer.User != nil || (issuer.Path != "" && issuer.Path != "/") || issuer.RawQuery != "" || issuer.Fragment != "" || s.ID == "" || s.Token == "" {
		return "", fmt.Errorf("security key issuer or session unavailable: %w", ErrUnavailable)
	}
	var wire struct {
		Session struct {
			Factors struct {
				User struct {
					ID        string `json:"id"`
					LoginName string `json:"loginName"`
				} `json:"user"`
				Password *struct {
					VerifiedAt string `json:"verifiedAt"`
				} `json:"password"`
			} `json:"factors"`
		} `json:"session"`
	}
	if err = c.do(ctx, http.MethodGet, "/v2/sessions/"+url.PathEscape(s.ID), nil, &wire, ErrUnavailable); err != nil {
		return "", err
	}
	if wire.Session.Factors.Password == nil || wire.Session.Factors.User.ID == "" || wire.Session.Factors.User.LoginName == "" || len(wire.Session.Factors.User.LoginName) > 200 {
		return "", ErrUnavailable
	}
	verified, err := time.Parse(time.RFC3339Nano, wire.Session.Factors.Password.VerifiedAt)
	if err != nil || verified.IsZero() {
		return "", ErrUnavailable
	}
	ar, err := c.AuthRequest(ctx, requestID)
	if err != nil {
		return "", err
	}
	if ar.ID != requestID || ar.ClientID == "" || ar.RedirectURI == "" {
		return "", ErrUnavailable
	}
	issuer.Path = "/ui/v2/login/password"
	issuer.RawQuery = url.Values{"requestId": {"oidc_" + requestID}, "loginName": {wire.Session.Factors.User.LoginName}}.Encode()
	return issuer.String(), nil
}

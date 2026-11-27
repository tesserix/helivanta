package loginclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// SecurityKeyRegistration is a registered-but-unverified security key: Zitadel's
// u2fId and its WebAuthn creation options, relayed verbatim. Helivanta never
// parses or verifies WebAuthn data; the browser and Zitadel do.
type SecurityKeyRegistration struct {
	ID              string
	CreationOptions json.RawMessage
}

// RegisterSecurityKey starts a security-key registration for the session's user
// (POST /v2/users/{id}/u2f, verified live against Zitadel v4.19.5). rpID is the
// boot-validated relying-party ID; callers must never derive it from request
// input. A 200 without both the id and the options cannot drive the browser
// ceremony, so it fails closed as retryable. Nothing returned is logged.
func (c *Client) RegisterSecurityKey(ctx context.Context, sessionID, rpID string) (SecurityKeyRegistration, error) {
	subject, err := c.sessionSubject(ctx, sessionID)
	if err != nil {
		return SecurityKeyRegistration{}, err
	}
	var wire struct {
		ID      string          `json:"u2fId"`
		Options json.RawMessage `json:"publicKeyCredentialCreationOptions"`
	}
	if err := c.do(ctx, http.MethodPost, "/v2/users/"+url.PathEscape(subject.UserID)+"/u2f", map[string]any{"domain": rpID}, &wire, ErrUnavailable); err != nil {
		return SecurityKeyRegistration{}, err
	}
	if wire.ID == "" || len(wire.Options) == 0 || string(wire.Options) == "null" {
		return SecurityKeyRegistration{}, fmt.Errorf("POST /v2/users/%s/u2f: response missing id or options: %w", subject.UserID, ErrUnavailable)
	}
	return SecurityKeyRegistration{ID: wire.ID, CreationOptions: wire.Options}, nil
}

// VerifySecurityKey completes a registration with the browser's credential
// (POST /v2/users/{id}/u2f/{keyID}). A rejected credential is a 400, which the
// shared classification turns into a credential refusal (IsCredentialRefusal).
func (c *Client) VerifySecurityKey(ctx context.Context, sessionID, keyID string, credential json.RawMessage, name string) error {
	subject, err := c.sessionSubject(ctx, sessionID)
	if err != nil {
		return err
	}
	body := map[string]any{"publicKeyCredential": credential, "tokenName": name}
	return c.do(ctx, http.MethodPost, "/v2/users/"+url.PathEscape(subject.UserID)+"/u2f/"+url.PathEscape(keyID), body, nil, ErrUnavailable)
}

// ChallengeSecurityKey asks Zitadel for a WebAuthn request challenge on the
// session (PATCH /v2/sessions/{id}) and returns the rotated session plus the
// request options verbatim. rpID is the boot-validated relying-party ID, never
// request input. The token rotates on every PATCH, as in VerifyTOTP.
func (c *Client) ChallengeSecurityKey(ctx context.Context, s Session, rpID string) (Session, json.RawMessage, error) {
	body := map[string]any{
		"sessionToken": s.Token,
		"challenges": map[string]any{
			"webAuthN": map[string]any{
				"domain":                      rpID,
				"userVerificationRequirement": "USER_VERIFICATION_REQUIREMENT_DISCOURAGED",
			},
		},
	}
	var wire struct {
		SessionToken string `json:"sessionToken"`
		Challenges   struct {
			WebAuthN struct {
				Options json.RawMessage `json:"publicKeyCredentialRequestOptions"`
			} `json:"webAuthN"`
		} `json:"challenges"`
	}
	if err := c.do(ctx, http.MethodPatch, "/v2/sessions/"+url.PathEscape(s.ID), body, &wire, ErrUnavailable); err != nil {
		return Session{}, nil, err
	}
	opts := wire.Challenges.WebAuthN.Options
	if wire.SessionToken == "" || len(opts) == 0 || string(opts) == "null" {
		return Session{}, nil, fmt.Errorf("PATCH /v2/sessions/%s: challenge response missing token or options: %w", s.ID, ErrUnavailable)
	}
	return Session{ID: s.ID, Token: wire.SessionToken}, opts, nil
}

// CheckSecurityKey submits the browser's assertion against the session (PATCH
// /v2/sessions/{id}) and returns the session with its rotated token. A wrong
// assertion is a 400, mapped to a credential refusal like a wrong TOTP code.
func (c *Client) CheckSecurityKey(ctx context.Context, s Session, assertion json.RawMessage) (Session, error) {
	body := map[string]any{
		"sessionToken": s.Token,
		"checks": map[string]any{
			"webAuthN": map[string]any{"credentialAssertionData": assertion},
		},
	}
	var wire struct {
		SessionToken string `json:"sessionToken"`
	}
	if err := c.do(ctx, http.MethodPatch, "/v2/sessions/"+url.PathEscape(s.ID), body, &wire, ErrBadCredentials); err != nil {
		return Session{}, err
	}
	return Session{ID: s.ID, Token: wire.SessionToken}, nil
}

// SessionUserID returns the Zitadel user id the session authenticated (GET
// /v2/sessions/{id}): the subject a verified security key is recorded under.
// It is read from Zitadel, never taken from the request.
func (c *Client) SessionUserID(ctx context.Context, sessionID string) (string, error) {
	subject, err := c.sessionSubject(ctx, sessionID)
	return subject.UserID, err
}

// ReadySecurityKeyIDs lists the u2fIds Zitadel holds as verified for the
// session's user (POST /management/v1/users/{id}/auth_factors/_search, scoped
// to the user's organisation; shape verified live against v4.19.5). Only u2f
// entries in AUTH_FACTOR_STATE_READY count; other factor kinds and states are
// ignored. A body without a result list is ErrUnavailable: fail closed.
func (c *Client) ReadySecurityKeyIDs(ctx context.Context, sessionID string) ([]string, error) {
	subject, err := c.sessionSubject(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	path := "/management/v1/users/" + url.PathEscape(subject.UserID) + "/auth_factors/_search"
	if subject.OrgID == "" {
		return nil, fmt.Errorf("POST %s: session has no organisation: %w", path, ErrUnavailable)
	}
	var wire struct {
		Result *[]struct {
			State string `json:"state"`
			U2F   *struct {
				ID string `json:"id"`
			} `json:"u2f"`
		} `json:"result"`
	}
	if err := c.do(ctx, http.MethodPost, path, map[string]any{}, &wire, ErrUnavailable, withOrgID(subject.OrgID)); err != nil {
		return nil, err
	}
	if wire.Result == nil {
		return nil, fmt.Errorf("POST %s: response missing result: %w", path, ErrUnavailable)
	}
	ids := []string{}
	for _, factor := range *wire.Result {
		if factor.U2F != nil && factor.U2F.ID != "" && factor.State == "AUTH_FACTOR_STATE_READY" {
			ids = append(ids, factor.U2F.ID)
		}
	}
	return ids, nil
}

package organizationidentity

import (
	"context"
	"crypto/rsa"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// KeyTokens exchanges a dedicated product machine key for short-lived OAuth credentials.
type KeyTokens struct {
	keyID, userID, issuer, project string
	key                            *rsa.PrivateKey
	http                           http.Client
	gate                           chan struct{}
	token                          string
	expires                        time.Time
}

// String prevents credential disclosure through ordinary diagnostic formatting.
func (KeyTokens) String() string { return "[redacted organisation identity credential]" }

// GoString also protects Go-syntax diagnostic formatting.
func (k KeyTokens) GoString() string { return k.String() }

// LogValue keeps key material and cached tokens out of structured logs.
func (k KeyTokens) LogValue() slog.Value { return slog.StringValue(k.String()) }

// NewKeyTokens accepts a Zitadel machine key without exposing it to browser sessions.
func NewKeyTokens(raw []byte, issuer, project string, httpClient *http.Client) (*KeyTokens, error) {
	u, err := url.Parse(issuer)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || (u.Path != "" && u.Path != "/") || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || httpClient == nil || !sourceID.MatchString(project) || len(raw) > 16<<10 {
		return nil, ErrUnavailable
	}
	var file struct {
		Type   string `json:"type"`
		KeyID  string `json:"keyId"`
		UserID string `json:"userId"`
		Key    string `json:"key"`
	}
	if json.Unmarshal(raw, &file) != nil || file.Type != "serviceaccount" || !sourceID.MatchString(file.KeyID) || !sourceID.MatchString(file.UserID) {
		return nil, ErrUnavailable
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(file.Key))
	if err != nil || key.N.BitLen() < 2048 || key.N.BitLen() > 4096 {
		return nil, ErrUnavailable
	}
	client := *httpClient
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &KeyTokens{keyID: file.KeyID, userID: file.UserID, issuer: strings.TrimSuffix(issuer, "/"), project: project, http: client, gate: make(chan struct{}, 1), key: key}, nil
}

// Token retains only an expiring access credential; every identity observation stays uncached.
func (k *KeyTokens) Token(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	select {
	case <-ctx.Done():
		return "", ErrUnavailable
	case k.gate <- struct{}{}:
	}
	defer func() { <-k.gate }()
	if ctx.Err() != nil {
		return "", ErrUnavailable
	}
	now := time.Now().UTC()
	if k.token != "" && now.Before(k.expires.Add(-30*time.Second)) {
		return k.token, nil
	}
	assertion := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{Issuer: k.userID, Subject: k.userID, Audience: jwt.ClaimStrings{k.issuer}, IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute))})
	assertion.Header["kid"] = k.keyID
	signed, err := assertion.SignedString(k.key)
	if err != nil {
		return "", ErrUnavailable
	}
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {signed}, "scope": {"openid urn:zitadel:iam:org:project:id:" + k.project + ":aud"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, k.issuer+"/oauth/v2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", ErrUnavailable
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	response, err := k.http.Do(req)
	if err != nil {
		return "", ErrUnavailable
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", ErrUnavailable
	}
	const limit = 8 << 10
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(body) > limit || ctx.Err() != nil {
		return "", ErrUnavailable
	}
	var credential struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if json.Unmarshal(body, &credential) != nil || !strings.EqualFold(credential.TokenType, "Bearer") || strings.TrimSpace(credential.AccessToken) != credential.AccessToken || credential.AccessToken == "" || strings.ContainsAny(credential.AccessToken, "\r\n\x00") || credential.ExpiresIn < 5 || credential.ExpiresIn > 3600 {
		return "", ErrUnavailable
	}
	expires := now.Add(time.Duration(credential.ExpiresIn) * time.Second)
	if !expires.After(time.Now().Add(2 * time.Second)) {
		return "", ErrUnavailable
	}
	k.token, k.expires = credential.AccessToken, expires
	return k.token, nil
}

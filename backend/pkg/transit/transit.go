// Package transit encrypts, decrypts and fingerprints values through OpenBao Transit; key material never leaves OpenBao.
package transit

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ErrUnavailable covers an unreachable, sealed, misconfigured or refusing vault; callers fail closed on it.
var ErrUnavailable = errors.New("transit unavailable")

// ErrIntegrity means OpenBao rejected a ciphertext for this key and context: tampered, moved or from another tenant.
var ErrIntegrity = errors.New("transit ciphertext rejected")

var keyName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,127}$`)

const maxResponse = 64 << 10

// Login exchanges a credential for a token and how long OpenBao says it lasts.
type Login func(ctx context.Context, c *http.Client, addr string) (token string, ttl time.Duration, err error)

// Client caches one token and logs in again before it expires or after OpenBao refuses it.
type Client struct {
	addr  string
	mount string
	http  *http.Client
	login Login
	now   func() time.Time

	mu      sync.Mutex
	token   string
	expires time.Time
}

// New builds a client; addr must be an absolute http(s) URL and mount a single path segment.
func New(addr, mount string, login Login, httpClient *http.Client) (*Client, error) {
	u, err := url.Parse(addr)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" || u.RawPath != "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery {
		return nil, errors.New("transit address must be an http(s) origin")
	}
	if !keyName.MatchString(mount) || login == nil {
		return nil, errors.New("transit mount and login are required")
	}
	client := http.Client{Timeout: 5 * time.Second}
	if httpClient != nil {
		client = *httpClient
		if client.Timeout == 0 {
			client.Timeout = 5 * time.Second
		}
	}
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{addr: strings.TrimSuffix(addr, "/"), mount: mount, http: &client, login: login, now: time.Now}, nil
}

// KubernetesLogin authenticates with the projected service-account token at tokenPath, re-read on every login.
func KubernetesLogin(role, tokenPath string) Login {
	return func(ctx context.Context, c *http.Client, addr string) (string, time.Duration, error) {
		jwt, err := os.ReadFile(tokenPath) //nolint:gosec // path is operator configuration, not request input
		if err != nil || len(bytes.TrimSpace(jwt)) == 0 {
			return "", 0, fmt.Errorf("%w: service account token unreadable", ErrUnavailable)
		}
		return loginAt(ctx, c, addr+"/v1/auth/kubernetes/login", map[string]string{"role": role, "jwt": string(bytes.TrimSpace(jwt))})
	}
}

func loginAt(ctx context.Context, c *http.Client, endpoint string, body any) (string, time.Duration, error) {
	var out struct {
		Auth struct {
			ClientToken   string `json:"client_token"`
			LeaseDuration int64  `json:"lease_duration"`
		} `json:"auth"`
	}
	status, err := send(ctx, c, endpoint, "", body, &out)
	if err != nil {
		return "", 0, err
	}
	if status != http.StatusOK || out.Auth.ClientToken == "" || out.Auth.LeaseDuration <= 0 {
		return "", 0, fmt.Errorf("%w: login refused (%d)", ErrUnavailable, status)
	}
	return out.Auth.ClientToken, time.Duration(out.Auth.LeaseDuration) * time.Second, nil
}

// UserpassLogin is for tests against a dev server; production uses KubernetesLogin.
func UserpassLogin(user, password string) Login {
	return func(ctx context.Context, c *http.Client, addr string) (string, time.Duration, error) {
		return loginAt(ctx, c, addr+"/v1/auth/userpass/login/"+url.PathEscape(user), map[string]string{"password": password})
	}
}

// Encrypt upserts key as a derived aes256-gcm96 key on first use and binds the ciphertext to context.
func (c *Client) Encrypt(ctx context.Context, key string, derivation, plaintext []byte) (string, error) {
	var out struct {
		Data struct {
			Ciphertext string `json:"ciphertext"`
		} `json:"data"`
	}
	body := map[string]string{"plaintext": b64(plaintext), "context": b64(derivation), "type": "aes256-gcm96"}
	if err := c.call(ctx, "encrypt", key, body, &out); err != nil {
		return "", err
	}
	if !strings.HasPrefix(out.Data.Ciphertext, "vault:v") {
		return "", fmt.Errorf("%w: malformed ciphertext", ErrUnavailable)
	}
	return out.Data.Ciphertext, nil
}

// Decrypt returns ErrIntegrity when the ciphertext does not authenticate under key and context.
func (c *Client) Decrypt(ctx context.Context, key string, derivation []byte, ciphertext string) ([]byte, error) {
	if !strings.HasPrefix(ciphertext, "vault:v") {
		return nil, ErrIntegrity
	}
	var out struct {
		Data struct {
			Plaintext string `json:"plaintext"`
		} `json:"data"`
	}
	if err := c.call(ctx, "decrypt", key, map[string]string{"ciphertext": ciphertext, "context": b64(derivation)}, &out); err != nil {
		return nil, err
	}
	plain, err := base64.StdEncoding.DecodeString(out.Data.Plaintext)
	if err != nil {
		return nil, fmt.Errorf("%w: malformed plaintext", ErrUnavailable)
	}
	return plain, nil
}

// HMAC fingerprints input with key version 1, so later rotation never changes an existing fingerprint.
func (c *Client) HMAC(ctx context.Context, key string, input []byte) (string, error) {
	var out struct {
		Data struct {
			HMAC string `json:"hmac"`
		} `json:"data"`
	}
	if err := c.call(ctx, "hmac", key, map[string]any{"input": b64(input), "key_version": 1, "algorithm": "sha2-256"}, &out); err != nil {
		return "", err
	}
	if _, valid := receiptMACBytes(out.Data.HMAC); !valid {
		return "", fmt.Errorf("%w: malformed hmac", ErrUnavailable)
	}
	return out.Data.HMAC, nil
}

// EnsureHMACKey idempotently provisions a non-exportable 256-bit HMAC key allowed by policy.
func (c *Client) EnsureHMACKey(ctx context.Context, key string) error {
	var out map[string]any
	// Transit uses bytes here; a decimal string also matches the policy's exact raw parameter type.
	return c.call(ctx, "keys", key, map[string]any{"type": "hmac", "key_size": "32", "exportable": false, "allow_plaintext_backup": false}, &out)
}

// VerifyHMAC separates a modified receipt from an unavailable cryptographic dependency.
func (c *Client) VerifyHMAC(ctx context.Context, key string, payload []byte, signature string) (bool, error) {
	provided, valid := receiptMACBytes(signature)
	if !valid {
		return false, nil
	}
	expected, err := c.HMAC(ctx, key, payload)
	if err != nil {
		return false, err
	}
	computed, valid := receiptMACBytes(expected)
	if !valid {
		return false, ErrUnavailable
	}
	return subtle.ConstantTimeCompare(computed, provided) == 1, nil
}

func receiptMACBytes(signature string) ([]byte, bool) {
	if !strings.HasPrefix(signature, "vault:v1:") {
		return nil, false
	}
	encoded := strings.TrimPrefix(signature, "vault:v1:")
	decoded, err := base64.StdEncoding.Strict().DecodeString(encoded)
	return decoded, err == nil && len(decoded) == 32 && base64.StdEncoding.EncodeToString(decoded) == encoded
}

// call retries exactly once with a fresh token when OpenBao answers 403, which is how an expired or revoked token presents.
func (c *Client) call(ctx context.Context, op, key string, body, out any) error {
	if !keyName.MatchString(key) {
		return fmt.Errorf("%w: invalid key name", ErrUnavailable)
	}
	endpoint := c.addr + "/v1/" + c.mount + "/" + op + "/" + key
	for attempt := 0; ; attempt++ {
		token, err := c.currentToken(ctx, attempt > 0)
		if err != nil {
			return err
		}
		status, err := send(ctx, c.http, endpoint, token, body, out)
		switch {
		case err != nil:
			return err
		case status == http.StatusOK || (status == http.StatusNoContent && op == "keys"):
			return nil
		case status == http.StatusForbidden && attempt == 0:
			continue
		case status == http.StatusBadRequest && op == "decrypt":
			return ErrIntegrity
		default:
			return fmt.Errorf("%w: %s refused (%d)", ErrUnavailable, op, status)
		}
	}
}

func (c *Client) currentToken(ctx context.Context, refresh bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !refresh && c.token != "" && c.now().Before(c.expires) {
		return c.token, nil
	}
	token, ttl, err := c.login(ctx, c.http, c.addr)
	if err != nil {
		c.token = ""
		if errors.Is(err, ErrUnavailable) {
			return "", err
		}
		return "", fmt.Errorf("%w: login failed", ErrUnavailable)
	}
	// Renew at 80% of the lease so a request never carries a token in its last moments.
	c.token, c.expires = token, c.now().Add(ttl*4/5)
	return token, nil
}

func send(ctx context.Context, c *http.Client, endpoint, token string, body, out any) (int, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, fmt.Errorf("%w: encode request", ErrUnavailable)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return 0, fmt.Errorf("%w: build request", ErrUnavailable)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Vault-Token", token)
	}
	resp, err := c.Do(req)
	if err != nil {
		// The error text can carry the request URL but never the body; it is still reduced to the sentinel.
		return 0, fmt.Errorf("%w: unreachable", ErrUnavailable)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil || len(data) > maxResponse {
		return 0, fmt.Errorf("%w: unreadable response", ErrUnavailable)
	}
	if resp.StatusCode == http.StatusOK && json.Unmarshal(data, out) != nil {
		return 0, fmt.Errorf("%w: malformed response", ErrUnavailable)
	}
	return resp.StatusCode, nil
}

func b64(v []byte) string { return base64.StdEncoding.EncodeToString(v) }

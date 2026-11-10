// Package customeronboarding owns the public signup receipt boundary.
package customeronboarding

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/onboarding-service/api/customercontract"
)

const cookieName = "__Host-helivanta-signup"

type Handlers struct {
	origin    string
	base      string
	tokenFile string
	client    *http.Client
}

// New accepts only the owned private onboarding origin and public browser origin.
func New(base, tokenFile, origin string) (*Handlers, error) {
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "http" || u.Host != "onboarding-api.onboarding.svc.cluster.local:8080" || (u.Path != "" && u.Path != "/") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || origin != "https://helivanta.app" || tokenFile != "/var/run/customer-onboarding/token" {
		return nil, errors.New("invalid customer onboarding destination")
	}
	return &Handlers{origin: origin, base: strings.TrimSuffix(base, "/"), tokenFile: tokenFile, client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

type capability struct {
	id    uuid.UUID
	token string
}

func newCapability() (capability, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return capability{}, err
	}
	var secret [32]byte
	if _, err = rand.Read(secret[:]); err != nil {
		return capability{}, err
	}
	return capability{id, base64.RawURLEncoding.EncodeToString(secret[:])}, nil
}
func readCapability(r *http.Request) (capability, bool, error) {
	rawCount := 0
	for _, header := range r.Header.Values("Cookie") {
		for _, part := range strings.Split(header, ";") {
			name, _, _ := strings.Cut(strings.TrimSpace(part), "=")
			if name == cookieName {
				rawCount++
			}
		}
	}
	if rawCount > 1 {
		return capability{}, false, errors.New("duplicate receipt")
	}
	var found *http.Cookie
	for _, cookie := range r.Cookies() {
		if cookie.Name == cookieName {
			if found != nil {
				return capability{}, false, errors.New("duplicate receipt")
			}
			found = cookie
		}
	}
	if found == nil {
		if rawCount != 0 {
			return capability{}, false, errors.New("invalid receipt")
		}
		return capability{}, false, nil
	}
	if len(found.Value) != 80 {
		return capability{}, false, errors.New("invalid receipt")
	}
	parts := strings.Split(found.Value, ".")
	if len(parts) != 2 {
		return capability{}, false, errors.New("invalid receipt")
	}
	id, err := uuid.Parse(parts[0])
	if err != nil || id == uuid.Nil || id.String() != parts[0] {
		return capability{}, false, errors.New("invalid receipt")
	}
	secret, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(secret) != 32 || base64.RawURLEncoding.EncodeToString(secret) != parts[1] {
		return capability{}, false, errors.New("invalid receipt")
	}
	return capability{id, parts[1]}, true, nil
}
func (c capability) hash() string {
	sum := sha256.Sum256([]byte(c.token))
	return hex.EncodeToString(sum[:])
}
func setCapability(c *gin.Context, value capability) {
	http.SetCookie(c.Writer, &http.Cookie{Name: cookieName, Value: value.id.String() + "." + value.token, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 7 * 24 * 60 * 60})
}
func (h *Handlers) browser(c *gin.Context) bool {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	if c.Request.Method != http.MethodGet {
		if len(c.Request.Header.Values("Origin")) != 1 || c.GetHeader("Origin") != h.origin || c.GetHeader("Sec-Fetch-Site") == "cross-site" {
			respond.Forbidden(c, "signup requires a same-origin request")
			return false
		}
		media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if err != nil || media != "application/json" {
			respond.Error(c, 415, "invalid_request", "JSON is required")
			return false
		}
	}
	if _, _, err := readCapability(c.Request); err != nil {
		respond.Error(c, 400, "invalid_request", "invalid signup receipt")
		return false
	}
	return true
}
func decode(c *gin.Context, in any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
	decoder.DisallowUnknownFields()
	if decoder.Decode(in) != nil || decoder.Decode(new(any)) != io.EOF {
		respond.Error(c, 400, "invalid_request", "invalid signup request")
		return false
	}
	return true
}
func unavailable(c *gin.Context) {
	respond.Error(c, 503, "onboarding_unavailable", "Workspace setup is temporarily unavailable. Please try again.")
}

// Start records a new owner request using the browser's private receipt.
func (h *Handlers) Start(c *gin.Context) {
	if !h.browser(c) {
		return
	}
	var in customercontract.Signup
	if !decode(c, &in) {
		return
	}
	cap, exists, _ := readCapability(c.Request)
	if !exists {
		var err error
		cap, err = newCapability()
		if err != nil {
			unavailable(c)
			return
		}
	}
	if in.Validate(cap.id) != nil {
		respond.Error(c, 400, "invalid_request", "check your organisation and owner details")
		return
	}
	setCapability(c, cap)
	receipt, _, ok := h.request(c, http.MethodPost, "", cap, in)
	if ok {
		respond.Accepted(c, receipt)
	}
}

// Status discloses progress only to the request's opaque receipt.
func (h *Handlers) Status(c *gin.Context) {
	if !h.browser(c) {
		return
	}
	cap, exists, _ := readCapability(c.Request)
	if !exists {
		var err error
		cap, err = newCapability()
		if err != nil {
			unavailable(c)
			return
		}
		setCapability(c, cap)
		respond.OK(c, customercontract.Receipt{State: customercontract.NotStarted})
		return
	}
	receipt, _, ok := h.request(c, http.MethodGet, "/"+cap.id.String(), cap, nil)
	if ok {
		respond.OK(c, receipt)
	}
}

// VerifyEmail consumes only a native email proof and rotates the browser receipt.
func (h *Handlers) VerifyEmail(c *gin.Context) { h.prove(c, false) }

// Password consumes only a native password-setup proof.
func (h *Handlers) Password(c *gin.Context) { h.prove(c, true) }
func (h *Handlers) prove(c *gin.Context, password bool) {
	if !h.browser(c) {
		return
	}
	var in customercontract.Proof
	if !decode(c, &in) {
		return
	}
	next, err := newCapability()
	if err != nil {
		unavailable(c)
		return
	}
	path := "/verify-email"
	if password {
		path = "/password"
	}
	receipt, id, ok := h.request(c, http.MethodPost, path, next, in)
	if !ok {
		return
	}
	if id == uuid.Nil {
		unavailable(c)
		return
	}
	next.id = id
	setCapability(c, next)
	respond.OK(c, receipt)
}

// EmailLink requests bounded native verification delivery.
func (h *Handlers) EmailLink(c *gin.Context) { h.deliver(c, false) }

// PasswordLink requests bounded native password setup or recovery delivery.
func (h *Handlers) PasswordLink(c *gin.Context) { h.deliver(c, true) }
func (h *Handlers) deliver(c *gin.Context, password bool) {
	if !h.browser(c) {
		return
	}
	var in customercontract.Delivery
	if !decode(c, &in) {
		return
	}
	cap, exists, _ := readCapability(c.Request)
	if !exists && in.UserID == "" {
		respond.Accepted(c, customercontract.Receipt{State: customercontract.EmailActionRequired})
		return
	}
	path := "/email-link"
	if password {
		path = "/password-link"
	}
	receipt, _, ok := h.request(c, http.MethodPost, path, cap, in)
	if ok {
		respond.Accepted(c, receipt)
	}
}
func (h *Handlers) request(c *gin.Context, method, path string, cap capability, input any) (customercontract.Receipt, uuid.UUID, bool) {
	var empty customercontract.Receipt
	token, err := os.ReadFile(h.tokenFile)
	if err != nil || len(token) > 8192 || strings.TrimSpace(string(token)) == "" {
		unavailable(c)
		return empty, uuid.Nil, false
	}
	var body []byte
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			respond.Error(c, 400, "invalid_request", "invalid signup request")
			return empty, uuid.Nil, false
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, method, h.base+"/internal/v1/customer-signups"+path, bytes.NewReader(body))
	if err != nil {
		unavailable(c)
		return empty, uuid.Nil, false
	}
	request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", cap.id.String())
	request.Header.Set("X-Signup-Receipt-Hash", cap.hash())
	response, err := h.client.Do(request)
	if err != nil {
		unavailable(c)
		return empty, uuid.Nil, false
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == 404 && method == http.MethodGet {
		return customercontract.Receipt{State: customercontract.NotStarted}, uuid.Nil, true
	}
	if response.StatusCode == 400 || response.StatusCode == 409 || response.StatusCode == 404 {
		respond.Error(c, 400, "signup_refused", "Check your details or request a fresh email link.")
		return empty, uuid.Nil, false
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		unavailable(c)
		return empty, uuid.Nil, false
	}
	var receipt customercontract.Receipt
	decoder := json.NewDecoder(io.LimitReader(response.Body, 8192))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&receipt) != nil || decoder.Decode(new(any)) != io.EOF || receipt.Validate() != nil {
		unavailable(c)
		return empty, uuid.Nil, false
	}
	var id uuid.UUID
	if raw := response.Header.Get("X-Signup-Command"); raw != "" {
		id, err = uuid.Parse(raw)
		if err != nil || id == uuid.Nil || id.String() != raw {
			unavailable(c)
			return empty, uuid.Nil, false
		}
	}
	return receipt, id, true
}

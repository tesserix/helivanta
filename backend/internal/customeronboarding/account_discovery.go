package customeronboarding

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/onboarding-service/api/customercontract"
	"io"
	"net/http"
	"strings"
)

const discoveryCookieName = authn.AccountDiscoveryCookie

// DiscoverAccounts starts mailbox proof without disclosing matching accounts.
func (h *Handlers) DiscoverAccounts(c *gin.Context) {
	if !h.browser(c) {
		return
	}
	var in customercontract.DiscoveryRequest
	if !decode(c, &in) {
		return
	}
	if in.Validate() != nil {
		respond.Error(c, 400, "invalid_request", "Enter a valid email address.")
		return
	}
	cap, err := newCapability()
	if err != nil {
		discoveryUnavailable(c)
		return
	}
	source := sha256.Sum256([]byte("helivanta-account-discovery-v1:" + c.ClientIP()))
	input := customercontract.DiscoveryStart{ID: cap.id.String(), BrowserHash: cap.hash(), SourceHash: hex.EncodeToString(source[:]), Email: in.Email}
	raw, ok := h.discoveryRequest(c, "start", input)
	if !ok {
		return
	}
	var result customercontract.DiscoveryAccepted
	if json.Unmarshal(raw, &result) != nil || result.State != "check_email" {
		discoveryUnavailable(c)
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: discoveryCookieName, Value: cap.id.String() + "." + cap.token, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: 600})
	c.JSON(202, result)
}

// VerifyAccounts consumes a mailbox proof bound to this browser, never a signup proof.
func (h *Handlers) VerifyAccounts(c *gin.Context) {
	if !h.browser(c) {
		return
	}
	var in customercontract.DiscoveryCode
	if !decode(c, &in) {
		return
	}
	cap, present, err := readCapabilityNamed(c.Request, discoveryCookieName)
	if err != nil || !present {
		discoveryRefused(c)
		return
	}
	proof := customercontract.DiscoveryProof{ID: cap.id.String(), BrowserHash: cap.hash(), Code: in.Code}
	if proof.Validate() != nil {
		discoveryRefused(c)
		return
	}
	raw, ok := h.discoveryRequest(c, "verify", proof)
	if !ok {
		return
	}
	var result customercontract.DiscoveryVerified
	if json.Unmarshal(raw, &result) != nil || result.State != "verified" {
		discoveryUnavailable(c)
		return
	}
	c.JSON(200, result)
}

// AccountChoices returns display-only choices after the exact browser's mailbox proof.
func (h *Handlers) AccountChoices(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	if c.GetHeader("Sec-Fetch-Site") == "cross-site" {
		respond.Forbidden(c, "same-origin request required")
		return
	}
	cap, present, err := readCapabilityNamed(c.Request, discoveryCookieName)
	if err != nil || !present {
		discoveryRefused(c)
		return
	}
	raw, ok := h.discoveryRequest(c, "choices", customercontract.DiscoveryAccess{ID: cap.id.String(), BrowserHash: cap.hash()})
	if !ok {
		return
	}
	var result customercontract.DiscoveryChoices
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || decoder.Decode(new(any)) != io.EOF || result.Validate() != nil {
		discoveryUnavailable(c)
		return
	}
	c.JSON(200, result)
}
func (h *Handlers) discoveryRequest(c *gin.Context, operation string, input any) ([]byte, bool) {
	token, err := h.identityCredential()
	if err != nil || len(token) > 8192 || strings.TrimSpace(string(token)) == "" {
		discoveryUnavailable(c)
		return nil, false
	}
	body, err := json.Marshal(input)
	if err != nil {
		discoveryUnavailable(c)
		return nil, false
	}
	req, err := http.NewRequestWithContext(c.Request.Context(), "POST", h.base+"/internal/v1/customer-signups/account-discovery/"+operation, bytes.NewReader(body))
	if err != nil {
		discoveryUnavailable(c)
		return nil, false
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	req.Header.Set("Content-Type", "application/json")
	res, err := h.client.Do(req)
	if err != nil {
		discoveryUnavailable(c)
		return nil, false
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == 429 {
		c.Header("Retry-After", "3600")
		respond.Error(c, 429, "rate_limited", "Too many sign-in codes requested. Please try again later.")
		return nil, false
	}
	if res.StatusCode == 400 || res.StatusCode == 404 {
		discoveryRefused(c)
		return nil, false
	}
	if res.StatusCode != 200 && res.StatusCode != 202 {
		discoveryUnavailable(c)
		return nil, false
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, 32769))
	if err != nil || len(raw) > 32768 {
		discoveryUnavailable(c)
		return nil, false
	}
	return raw, true
}
func discoveryRefused(c *gin.Context) {
	respond.Error(c, 404, "account_discovery_expired", "The code is invalid, expired or already used. Request a new code, or retry account selection if you already verified it.")
}
func discoveryUnavailable(c *gin.Context) {
	respond.Error(c, 503, "identity_unavailable", "Sign-in is temporarily unavailable. Please try again.")
}

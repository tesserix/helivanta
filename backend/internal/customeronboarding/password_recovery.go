package customeronboarding

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/ratelimit"
	"github.com/tesserix/onboarding-service/api/customercontract"
)

const recoveryCookieName = authn.PasswordRecoveryCookie
const recoveryCommandCookieName = authn.PasswordRecoveryCommandCookie

// PasswordRecoveryDeps binds tenant selection to this deployment's native authentication requests.
type PasswordRecoveryDeps struct {
	Requests *loginclient.Client
	ClientID string
	Limiter  ratelimit.Limiter
}

// PasswordRecoveryHandlers brokers dedicated mailbox recovery without minting an application session.
type PasswordRecoveryHandlers struct {
	owner *Handlers
	deps  PasswordRecoveryDeps
}

// NewPasswordRecoveryHandlers requires explicit native request and capacity-control wiring.
func NewPasswordRecoveryHandlers(owner *Handlers, deps PasswordRecoveryDeps) (*PasswordRecoveryHandlers, error) {
	if owner == nil || owner.client == nil || deps.Requests == nil || deps.ClientID == "" || strings.TrimSpace(deps.ClientID) != deps.ClientID || deps.Limiter == nil {
		return nil, errors.New("password recovery dependencies unavailable")
	}
	return &PasswordRecoveryHandlers{owner: owner, deps: deps}, nil
}

func (h *PasswordRecoveryHandlers) browser(c *gin.Context) bool {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		respond.Error(c, 400, "invalid_request", "Invalid password recovery request.")
		return false
	}
	if c.Request.Method == http.MethodGet && (c.Request.ContentLength > 0 || len(c.Request.TransferEncoding) > 0) {
		respond.Error(c, 400, "invalid_request", "Recovery status does not accept a request body.")
		return false
	}
	if len(c.Request.Header.Values("Sec-Fetch-Site")) > 1 || c.GetHeader("Sec-Fetch-Site") == "cross-site" {
		respond.Forbidden(c, "Password recovery requires the same browser origin.")
		return false
	}
	if c.Request.Method != http.MethodGet {
		if len(c.Request.Header.Values("Origin")) != 1 || c.GetHeader("Origin") != h.owner.origin {
			respond.Forbidden(c, "Password recovery requires the same browser origin.")
			return false
		}
		media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if len(c.Request.Header.Values("Content-Type")) != 1 || err != nil || media != "application/json" {
			respond.Error(c, 415, "invalid_request", "JSON is required.")
			return false
		}
	}
	decision := h.deps.Limiter.Allow("password_recovery:"+c.ClientIP(), ratelimit.Rule{Rate: 60, Burst: 10, Per: time.Minute}, time.Now())
	if !decision.Allowed {
		respond.TooManyRequests(c, "Too many recovery requests. Try again later.", decision.RetryAfter, decision.Limit, decision.Remaining)
		return false
	}
	return true
}
func (h *PasswordRecoveryHandlers) capability(c *gin.Context) (capability, bool) {
	if !h.browser(c) {
		return capability{}, false
	}
	cap, present, err := readCapabilityNamed(c.Request, recoveryCookieName)
	if err != nil || !present {
		recoveryExpired(c)
		return capability{}, false
	}
	return cap, true
}
func recoveryCookie(c *gin.Context, cap capability) {
	recoveryNamedCookie(c, recoveryCookieName, cap, 600)
}
func recoveryNamedCookie(c *gin.Context, name string, cap capability, age int) {
	saved := c.Writer.Header().Values("Set-Cookie")
	c.Writer.Header().Del("Set-Cookie")
	for _, value := range saved {
		if !strings.HasPrefix(value, name+"=") {
			c.Writer.Header().Add("Set-Cookie", value)
		}
	}
	value := cap.id.String() + "." + cap.token
	if age < 0 {
		value = ""
	}
	http.SetCookie(c.Writer, &http.Cookie{Name: name, Value: value, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: age})
}

// Start derives the tenant from native proof and retains the browser's explicit delivery command.
func (h *PasswordRecoveryHandlers) Start(c *gin.Context) {
	if !h.browser(c) {
		return
	}
	var in customercontract.PasswordRecoveryRequest
	if !decodeRecovery(c, &in) {
		return
	}
	org, err := h.deps.Requests.AuthRequestOrganization(c.Request.Context(), in.AuthRequestID, h.deps.ClientID)
	if errors.Is(err, loginclient.ErrAuthRequestInvalid) {
		respond.Error(c, 400, "recovery_request_expired", "This sign-in request is invalid or expired. Start sign-in again.")
		return
	}
	if err != nil {
		recoveryUnavailable(c)
		return
	}
	if org == "" {
		respond.Error(c, 409, "recovery_account_selection_required", "Choose your organization from sign-in before requesting password recovery.")
		return
	}
	tenant, err := uuid.Parse(strings.TrimPrefix(org, "helivanta-"))
	if err != nil || tenant == uuid.Nil || org != "helivanta-"+tenant.String() {
		respond.Error(c, 400, "recovery_request_expired", "This sign-in request could not be verified.")
		return
	}
	cap, present, err := readCapabilityNamed(c.Request, recoveryCookieName)
	if err != nil {
		recoveryExpired(c)
		return
	}
	if !present {
		cap, err = newCapability()
		if err != nil {
			recoveryUnavailable(c)
			return
		}
	}
	if _, active, valid := recoveryCommand(c, cap); !valid {
		return
	} else if active {
		recoveryConflict(c)
		return
	}
	cap.id, _ = uuid.Parse(in.RequestID)
	// Persist the browser binding before private I/O; a network retry keeps the same request ID.
	recoveryCookie(c, cap)
	source := sha256.Sum256([]byte("helivanta-password-recovery-v1:" + c.ClientIP()))
	input := customercontract.PasswordRecoveryStart{ID: cap.id.String(), TenantID: tenant.String(), BrowserHash: cap.hash(), SourceHash: hex.EncodeToString(source[:]), Email: in.Email}
	raw, ok := h.request(c, "start", input)
	if !ok {
		return
	}
	var result customercontract.PasswordRecoveryAccepted
	if json.Unmarshal(raw, &result) != nil || result.Validate() != nil || result.ChallengeID == "" {
		recoveryUnavailable(c)
		return
	}
	cap.id, _ = uuid.Parse(result.ChallengeID)
	// A verified resend may retain an earlier challenge; the browser never receives its internal ID.
	recoveryCookie(c, cap)
	result.ChallengeID = ""
	c.JSON(202, result)
}

// Verify consumes only this browser's dedicated recovery mailbox proof.
func (h *PasswordRecoveryHandlers) Verify(c *gin.Context) {
	cap, ok := h.capability(c)
	if !ok {
		return
	}
	var in customercontract.PasswordRecoveryCode
	if !decodeRecovery(c, &in) {
		return
	}
	raw, ok := h.request(c, "verify", customercontract.PasswordRecoveryProof{ID: cap.id.String(), BrowserHash: cap.hash(), Code: in.Code})
	if !ok {
		return
	}
	var compact bytes.Buffer
	if json.Compact(&compact, raw) != nil || compact.String() != `{"state":"verified"}` {
		recoveryUnavailable(c)
		return
	}
	c.JSON(200, customercontract.PasswordRecoveryVerified{State: "verified"})
}

// Status restores a retained verified outcome without resending mail or repeating a native write.
func (h *PasswordRecoveryHandlers) Status(c *gin.Context) {
	cap, ok := h.capability(c)
	if !ok {
		return
	}
	command, present, valid := recoveryCommand(c, cap)
	if !valid {
		return
	}
	selected := ""
	if present {
		selected = command.id.String()
	}
	raw, ok := h.request(c, "status", customercontract.PasswordRecoveryRead{ID: cap.id.String(), BrowserHash: cap.hash(), CommandID: selected})
	if !ok {
		return
	}
	var result customercontract.PasswordRecoveryStatus
	if json.Unmarshal(raw, &result) != nil || result.Validate() != nil {
		recoveryUnavailable(c)
		return
	}
	if !recoveryReceipt(c, cap, result.State, result.CommandID, selected) {
		return
	}
	result.CommandID = ""
	c.JSON(200, result)
}

// Password submits one immutable native command; its receipt never grants a session.
func (h *PasswordRecoveryHandlers) Password(c *gin.Context) {
	cap, ok := h.capability(c)
	if !ok {
		return
	}
	var in customercontract.PasswordRecoverySubmit
	if !decodeRecovery(c, &in) {
		return
	}
	command, present, valid := recoveryCommand(c, cap)
	if !valid {
		return
	}
	if !present || command.id.String() != in.CommandID {
		recoveryConflict(c)
		return
	}
	raw, ok := h.request(c, "password", customercontract.PasswordRecoveryPassword{ID: cap.id.String(), BrowserHash: cap.hash(), CommandID: in.CommandID, Password: in.Password})
	if !ok {
		return
	}
	var result customercontract.PasswordRecoveryResult
	if json.Unmarshal(raw, &result) != nil || result.Validate() != nil {
		recoveryUnavailable(c)
		return
	}
	if result.State == "prepared" || result.State == "cancelled" {
		recoveryUnavailable(c)
		return
	}
	if !recoveryReceipt(c, cap, result.State, result.CommandID, in.CommandID) {
		return
	}
	result.CommandID = ""
	c.JSON(200, result)
}

// Prepare retains the exact command marker before any password may be submitted.
func (h *PasswordRecoveryHandlers) Prepare(c *gin.Context) { h.intent(c, "prepare") }

// Cancel fences only a prepared command and never replays a password.
func (h *PasswordRecoveryHandlers) Cancel(c *gin.Context) { h.intent(c, "cancel") }

func (h *PasswordRecoveryHandlers) intent(c *gin.Context, action string) {
	cap, ok := h.capability(c)
	if !ok {
		return
	}
	var commandID string
	if action == "cancel" {
		var in customercontract.PasswordRecoveryCancellation
		if !decodeRecovery(c, &in) {
			return
		}
		commandID = in.CommandID
	} else {
		var in customercontract.PasswordRecoveryCommand
		if !decodeRecovery(c, &in) {
			return
		}
		commandID = in.CommandID
	}
	active, present, valid := recoveryCommand(c, cap)
	if !valid {
		return
	}
	if commandID == "" && present {
		commandID = active.id.String()
	}
	if commandID == "" || (present && active.id.String() != commandID) {
		recoveryConflict(c)
		return
	}
	marker := cap
	marker.id, _ = uuid.Parse(commandID)
	recoveryNamedCookie(c, recoveryCommandCookieName, marker, 600)
	raw, ok := h.request(c, action, customercontract.PasswordRecoveryIntent{ID: cap.id.String(), BrowserHash: cap.hash(), CommandID: commandID})
	if !ok {
		return
	}
	var result customercontract.PasswordRecoveryResult
	if json.Unmarshal(raw, &result) != nil || result.Validate() != nil || (action == "cancel" && result.State != "cancelled") {
		recoveryUnavailable(c)
		return
	}
	if !recoveryReceipt(c, cap, result.State, result.CommandID, commandID) {
		return
	}
	result.CommandID = ""
	c.JSON(200, result)
}

func recoveryCommand(c *gin.Context, base capability) (capability, bool, bool) {
	command, present, err := readCapabilityNamed(c.Request, recoveryCommandCookieName)
	if err != nil || (present && command.token != base.token) {
		recoveryConflict(c)
		return capability{}, false, false
	}
	return command, present, true
}
func recoveryReceipt(c *gin.Context, base capability, state, command, selected string) bool {
	if command == "" {
		if selected != "" || (state != "ready" && state != "locked") {
			recoveryUnavailable(c)
			return false
		}
		return true
	}
	if selected != "" && command != selected {
		recoveryUnavailable(c)
		return false
	}
	marker := base
	marker.id, _ = uuid.Parse(command)
	age := 600
	if state == "completed" || state == "refused" || state == "cancelled" {
		age = -1
	}
	recoveryNamedCookie(c, recoveryCommandCookieName, marker, age)
	return true
}
func recoveryConflict(c *gin.Context) {
	respond.Error(c, 409, "password_recovery_conflict", "Check the current recovery status before preparing, cancelling or submitting a password.")
}

func (h *PasswordRecoveryHandlers) request(c *gin.Context, action string, input any) ([]byte, bool) {
	token, err := h.owner.identityCredential()
	if err != nil || len(token) > 8192 || strings.TrimSpace(string(token)) == "" {
		recoveryUnavailable(c)
		return nil, false
	}
	raw, err := json.Marshal(input) // #nosec G117 -- Transient proof/password handoff to the fixed private owner; never logged or persisted.
	if err != nil {
		recoveryUnavailable(c)
		return nil, false
	}
	deadline := 10 * time.Second
	if action == "password" {
		deadline = 32 * time.Second
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), deadline)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.owner.base+"/internal/v1/customer-signups/password-recovery/"+action, bytes.NewReader(raw))
	if err != nil {
		recoveryUnavailable(c)
		return nil, false
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
	req.Header.Set("Content-Type", "application/json")
	client := *h.owner.client
	client.Timeout = deadline
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := client.Do(req)
	if err != nil {
		recoveryUnavailable(c)
		return nil, false
	}
	defer func() { _ = res.Body.Close() }()
	switch res.StatusCode {
	case 404:
		if read, selected := input.(customercontract.PasswordRecoveryRead); action == "status" && selected && read.CommandID != "" {
			respond.Error(c, 409, "password_recovery_command_unknown", "The retained attempt could not be confirmed. Check its status or cancel the prepared attempt before continuing.")
			return nil, false
		}
		recoveryExpired(c)
		return nil, false
	case 400:
		recoveryExpired(c)
		return nil, false
	case 409:
		respond.Error(c, 409, "password_recovery_conflict", "This recovery cannot proceed. Check its status or contact your organization administrator.")
		return nil, false
	case 429:
		seconds, err := strconv.Atoi(res.Header.Get("Retry-After"))
		if err != nil || seconds < 1 || seconds > 86400 {
			recoveryUnavailable(c)
			return nil, false
		}
		c.Header("Retry-After", strconv.Itoa(seconds))
		respond.Error(c, 429, "password_recovery_limited", "Too many recovery codes requested. Up to three per hour and ten per day are allowed for an email address.")
		return nil, false
	}
	expected := 200
	if action == "start" {
		expected = 202
	}
	if res.StatusCode != expected {
		recoveryUnavailable(c)
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 8193))
	if err != nil || len(body) > 8192 {
		recoveryUnavailable(c)
		return nil, false
	}
	return body, true
}
func recoveryExpired(c *gin.Context) {
	respond.Error(c, 404, "password_recovery_expired", "This recovery is unavailable or expired. Request a fresh code, or check status if you already verified it.")
}
func recoveryUnavailable(c *gin.Context) {
	respond.Error(c, 503, "password_recovery_unavailable", "Password recovery is temporarily unavailable. Check its status before retrying a password update.")
}

func decodeRecovery(c *gin.Context, in any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
	decoder.DisallowUnknownFields()
	if decoder.Decode(in) != nil || decoder.Decode(new(any)) != io.EOF {
		respond.Error(c, 400, "invalid_request", "Invalid password recovery request.")
		return false
	}
	return true
}

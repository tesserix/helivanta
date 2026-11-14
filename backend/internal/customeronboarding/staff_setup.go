package customeronboarding

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/ratelimit"
	"github.com/tesserix/onboarding-service/api/staffcontract"
)

type StaffSetupDeps struct {
	Limiter ratelimit.Limiter
	Limit   ratelimit.Rule
}

// StaffSetup forwards only an opaque recipient capability and native proof.
func (h *Handlers) StaffSetup(deps StaffSetupDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !h.browser(c) {
			return
		}
		if deps.Limiter == nil || deps.Limit.Rate < 1 || deps.Limit.Burst < 1 || deps.Limit.Per <= 0 {
			unavailable(c)
			return
		}
		decision := deps.Limiter.Allow("staff_setup:"+c.ClientIP(), deps.Limit, time.Now())
		if !decision.Allowed {
			respond.TooManyRequests(c, "Too many setup attempts. Try again later.", decision.RetryAfter, decision.Limit, decision.Remaining)
			return
		}
		action := c.Param("action")
		if action != "status" && action != "email" && action != "password" && action != "resend" {
			respond.NotFound(c, "Invitation action not found.")
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
		if err != nil || c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
			respond.Error(c, 400, "invalid_request", "Invalid invitation request.")
			return
		}
		in, err := staffcontract.DecodeRecipient(body, action)
		if err != nil {
			respond.Error(c, 400, "invalid_request", "Invalid invitation request.")
			return
		}
		token, err := os.ReadFile(h.tokenFile)
		if err != nil || len(token) > 8192 || strings.TrimSpace(string(token)) == "" {
			unavailable(c)
			return
		}
		raw, err := json.Marshal(in) // #nosec G117 -- Private credential handoff to the fixed identity owner; never logged or persisted.
		if err != nil {
			unavailable(c)
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		defer cancel()
		request, err := http.NewRequestWithContext(ctx, "POST", h.base+"/internal/v1/customer-signups/staff-invitations/"+action, bytes.NewReader(raw))
		if err != nil || h.client == nil {
			unavailable(c)
			return
		}
		request.Header.Set("Authorization", "Bearer "+strings.TrimSpace(string(token)))
		request.Header.Set("Content-Type", "application/json")
		client := *h.client
		client.Timeout = 30 * time.Second
		client.Jar = nil
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		response, err := client.Do(request)
		if err != nil {
			unavailable(c)
			return
		}
		defer func() { _ = response.Body.Close() }()
		switch response.StatusCode {
		case 400, 404, 409:
			respond.Error(c, response.StatusCode, "staff_setup_refused", "Check this invitation or request a fresh link from your administrator.")
			return
		case 429:
			seconds, err := strconv.Atoi(response.Header.Get("Retry-After"))
			if err != nil || seconds < 1 || seconds > 86400 {
				unavailable(c)
				return
			}
			c.Header("Retry-After", strconv.Itoa(seconds))
			respond.Error(c, 429, "staff_setup_limited", "Please wait before requesting another link. Five links are allowed per day.")
			return
		}
		if response.StatusCode != 200 {
			unavailable(c)
			return
		}
		var receipt staffcontract.StaffInvitationReceipt
		data, readErr := io.ReadAll(io.LimitReader(response.Body, 8193))
		if readErr != nil || len(data) > 8192 {
			unavailable(c)
			return
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&receipt) != nil || decoder.Decode(new(any)) != io.EOF || receipt.Validate() != nil {
			unavailable(c)
			return
		}
		expected := in.InvitationID
		if action == "resend" {
			expected = *in.DeliveryID
		}
		if receipt.InvitationID != nil && *receipt.InvitationID != expected {
			unavailable(c)
			return
		}
		c.JSON(200, receipt)
	}
}

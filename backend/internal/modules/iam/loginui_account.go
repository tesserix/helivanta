package iam

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/audit"
)

// tooManyAccountsMessage answers loginclient.ErrTooManyAccounts (#1751 spec
// D9): no password was checked, and the two routes that name one workspace
// still work.
const tooManyAccountsMessage = "this email has more accounts than can be checked at once; use your workspace link or find your accounts by email"

// accountChoiceWire is one account the person may pick. Names come from the
// identity owner, never from the browser, and only for accounts whose
// password matched.
type accountChoiceWire struct {
	TenantID string `json:"tenant_id"`
	Name     string `json:"name"`
	Account  string `json:"account"`
}

// accountChoiceResponse is Password's answer when one email and password
// matched several accounts. Like factor_required it is a 200 whose only key
// names the next step.
type accountChoiceResponse struct {
	Choices []accountChoiceWire `json:"account_choice_required"`
}

// accountRequest is the body POST /v1/auth/login/account accepts.
type accountRequest struct {
	AuthRequestID string `json:"auth_request_id" binding:"required"`
	TenantID      string `json:"tenant_id" binding:"required,uuid"`
}

// offerAccountChoice parks every matched session and asks which account to
// continue with (#1751 spec D3). Nothing is completed until Account runs.
func (h *LoginUIHandlers) offerAccountChoice(c *gin.Context, req passwordRequest, sessions []loginclient.TenantSession) {
	expires := time.Now().Add(loginAttemptTTL)
	choices := make([]accountChoice, 0, len(sessions))
	wire := make([]accountChoiceWire, 0, len(sessions))
	for _, s := range sessions {
		choices = append(choices, accountChoice{
			AuthRequestID: req.AuthRequestID,
			TenantID:      s.Target.TenantID,
			SessionID:     s.Session.ID,
			SessionToken:  s.Session.Token,
			Subject:       s.Target.Subject,
			Name:          s.Target.Name,
			Account:       s.Target.Account,
			ExpiresAt:     expires,
		})
		wire = append(wire, accountChoiceWire{TenantID: s.Target.TenantID, Name: s.Target.Name, Account: s.Target.Account})
	}
	if err := h.store.PutChoices(c.Request.Context(), req.AuthRequestID, choices); err != nil {
		h.deleteSessions(c, sessions)
		respond.InternalErr(c, err, "sign-in could not be completed")
		return
	}
	requestid.Logger(c).InfoContext(c.Request.Context(), "login password succeeded, account choice required",
		"auth_request_id", req.AuthRequestID, "outcome", "account_choice_required", "accounts", len(choices))
	respond.OK(c, accountChoiceResponse{Choices: wire})
}

// Account backs POST /v1/auth/login/account: the person picked one of the
// accounts Password parked. The chosen session continues exactly as a single
// match would (continueWithSession); every sibling session is terminated.
// An unknown, expired or already-taken pick is attempt-expired at the
// failure floor, like an unknown attempt on Factor, so the route says nothing
// about which auth requests have choices.
func (h *LoginUIHandlers) Account(c *gin.Context) {
	start := time.Now()
	var req accountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, err)
		return
	}
	if !h.allowedByLimiter(c, passwordRateBucket, h.limit) {
		return
	}
	chosen, siblings, err := h.store.TakeChoice(c.Request.Context(), req.AuthRequestID, req.TenantID)
	if errors.Is(err, errAttemptNotFound) {
		h.respondAttemptExpired(c, start)
		return
	}
	if err != nil {
		respond.InternalErr(c, err, "sign-in could not be completed")
		return
	}
	for _, s := range siblings {
		h.deleteSession(c, loginclient.Session{ID: s.SessionID, Token: s.SessionToken})
	}
	audit.Log(c.Request.Context(), requestid.Logger(c), slog.LevelInfo, "auth.login.account_chosen", "login account chosen",
		"auth_request_id", req.AuthRequestID, "tenant_id", chosen.TenantID)
	h.continueWithSession(c, req.AuthRequestID, chosen.Account, loginclient.Session{ID: chosen.SessionID, Token: chosen.SessionToken})
}

func (h *LoginUIHandlers) deleteSessions(c *gin.Context, sessions []loginclient.TenantSession) {
	for _, s := range sessions {
		h.deleteSession(c, s.Session)
	}
}

// deleteSession terminates a session nobody will use. Failure is logged, not
// returned: the session is unusable without a token only Helivanta held, and
// the person's sign-in must not fail over cleanup.
func (h *LoginUIHandlers) deleteSession(c *gin.Context, s loginclient.Session) {
	if err := h.client.DeleteSession(context.WithoutCancel(c.Request.Context()), s); err != nil {
		requestid.Logger(c).WarnContext(c.Request.Context(), "login: unused session not deleted", "err", err)
	}
}

// respondTooManyAccounts answers loginclient.ErrTooManyAccounts.
func respondTooManyAccounts(c *gin.Context) {
	respond.Error(c, http.StatusBadRequest, "too_many_accounts", tooManyAccountsMessage)
}

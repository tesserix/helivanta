package iam

import (
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
)

// This file is #856's HTTP half: a password that must change is changed in
// Helivanta's own form, never Zitadel's, before the login completes. See
// docs/superpowers/specs/2026-10-08-password-change-required-design.md.

// passwordChangeRateBucket is POST /v1/auth/login/password-change's own
// bucket on the shared login rule (h.limit) — see allowedByLimiter for why
// every route keys its own prefix. It is not a guessing surface: a row at
// stagePasswordChange exists only after every factor was proven, and a wrong
// current password ends the attempt (PasswordChange).
const passwordChangeRateBucket = "login_password_change:"

// Answers for a new password that cannot be accepted (spec D4). Each is a
// 422 the change step shows inline, keeping the row so the user can try
// again; none is a credential refusal — the user has already proven every
// factor, so naming the failed rule discloses nothing.
const (
	passwordRejectedCode     = "password_rejected"
	passwordUnchangedCode    = "password_unchanged"
	passwordUnchangedMessage = "the new password must be different from your current password"
	passwordPolicyMessage    = "the new password does not meet the password policy"
)

// passwordRuleMessages words each loginclient.PasswordRule* for the change
// step. A rule missing here (Zitadel added one) falls back to
// passwordPolicyMessage rather than to nothing.
var passwordRuleMessages = map[string]string{
	loginclient.PasswordRuleMinLength: "the new password is too short",
	loginclient.PasswordRuleMaxLength: "the new password must be at most 200 characters",
	loginclient.PasswordRuleLowercase: "the new password must contain a lowercase letter",
	loginclient.PasswordRuleUppercase: "the new password must contain an uppercase letter",
	loginclient.PasswordRuleNumber:    "the new password must contain a number",
	loginclient.PasswordRuleSymbol:    "the new password must contain a symbol",
}

// refusalCodePasswordChangedRestart answers a failure AFTER Zitadel accepted
// the new password (spec D4 step 5). The attempt cannot be resumed — a retry
// would send the old password as current_password and be refused — so the
// row is deleted and the user is told the one thing they need: the change
// took effect, and they sign in again with the new password.
const (
	refusalCodePasswordChangedRestart    = "password_changed_sign_in_again"
	refusalMessagePasswordChangedRestart = "your password was changed, but this sign-in could not be completed; " +
		"sign in again with your new password"
)

// passwordPolicyResponse is loginclient.PasswordComplexity on the wire,
// snake_case like every other login-UI body. The browser renders it as a
// requirements checklist; Zitadel enforces it.
type passwordPolicyResponse struct {
	MinLength         uint64 `json:"min_length"`
	RequiresUppercase bool   `json:"requires_uppercase"`
	RequiresLowercase bool   `json:"requires_lowercase"`
	RequiresNumber    bool   `json:"requires_number"`
	RequiresSymbol    bool   `json:"requires_symbol"`
}

// passwordChangeRequiredResponse is OutcomePasswordChangeRequired's shape:
// why the password must change ("required" or "expired") and the policy a
// new one must meet. Like factorRequiredResponse it carries no callback_url,
// so it cannot be mistaken for a completed login.
type passwordChangeRequiredResponse struct {
	PasswordChange passwordChangeInfo `json:"password_change_required"`
}

type passwordChangeInfo struct {
	Reason string                 `json:"reason"`
	Policy passwordPolicyResponse `json:"policy"`
}

// respondPasswordChangeRequired answers OutcomePasswordChangeRequired from
// Password or Factor (stage names which, for the log only), once the row is
// already held at stagePasswordChange.
func (h *LoginUIHandlers) respondPasswordChangeRequired(c *gin.Context, authRequestID, stage string, change loginclient.PasswordChange) {
	requestid.Logger(c).InfoContext(c.Request.Context(), "login: password change required",
		"auth_request_id", authRequestID, "stage", stage, "outcome", "password_change_required",
		"password_change_reason", change.Reason.String())
	respond.OK(c, passwordChangeRequiredResponse{PasswordChange: passwordChangeInfo{
		Reason: change.Reason.String(),
		Policy: passwordPolicyResponse{
			MinLength:         change.Policy.MinLength,
			RequiresUppercase: change.Policy.RequiresUppercase,
			RequiresLowercase: change.Policy.RequiresLowercase,
			RequiresNumber:    change.Policy.RequiresNumber,
			RequiresSymbol:    change.Policy.RequiresSymbol,
		},
	}})
}

// passwordChangeRequest is the body POST /v1/auth/login/password-change
// accepts. current_password is what the user typed at the credential step;
// the browser holds it in memory only (spec D6) and Zitadel verifies it
// (loginclient.ChangePassword).
type passwordChangeRequest struct {
	AuthRequestID   string `json:"auth_request_id" binding:"required"`
	CurrentPassword string `json:"current_password" binding:"required"`
	NewPassword     string `json:"new_password" binding:"required"`
}

// PasswordChange backs POST /v1/auth/login/password-change (#856 spec D4):
// it resumes a session held at stagePasswordChange, changes the password in
// Zitadel, and completes the login only through
// loginclient.CompleteAfterPasswordChange — which re-runs every sufficiency
// check before it finalizes.
//
// It cannot change a password for a session that has not proven every
// factor: store.Get takes stagePasswordChange, and only a full sufficiency
// decision writes a row at that stage (Password or Factor, on
// OutcomePasswordChangeRequired). A row at the factor stage — a TOTP still
// unproven — answers exactly like a missing one.
//
// Like Factor, a missing, expired or wrong-stage attempt answers
// attempt-expired after the same MinFailedLoginDuration floor, so the answer
// and its timing say nothing about which auth_request_ids exist.
func (h *LoginUIHandlers) PasswordChange(c *gin.Context) {
	start := time.Now()

	var req passwordChangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, err)
		return
	}
	if !h.allowedByLimiter(c, passwordChangeRateBucket, h.limit) {
		return
	}

	attempt, err := h.store.Get(c.Request.Context(), req.AuthRequestID, stagePasswordChange)
	if err != nil {
		requestid.Logger(c).WarnContext(c.Request.Context(), "login password change: no pending attempt",
			"auth_request_id", req.AuthRequestID)
		h.respondAttemptExpired(c, start)
		return
	}
	session := loginclient.Session{ID: attempt.SessionID, Token: attempt.SessionToken}

	if err := h.client.ChangePassword(c.Request.Context(), session, req.CurrentPassword, req.NewPassword); err != nil {
		h.respondPasswordChangeFailure(c, req.AuthRequestID, err)
		return
	}
	requestid.Logger(c).InfoContext(c.Request.Context(), "login: password changed",
		"auth_request_id", req.AuthRequestID)

	// From here the password HAS changed. Nothing below may answer in a way
	// that invites the browser to resubmit the change (it would send the old
	// password as current_password and be refused): every failure ends the
	// attempt and says the change took effect.
	result, err := h.client.CompleteAfterPasswordChange(c.Request.Context(), req.AuthRequestID, session)
	if err != nil {
		h.deleteAttempt(c, req.AuthRequestID)
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, loginclient.ErrUnavailable):
			status = http.StatusServiceUnavailable
		case errors.Is(err, loginclient.ErrAuthRequestInvalid):
			status = http.StatusBadRequest
		}
		requestid.Logger(c).ErrorContext(c.Request.Context(), "login: password changed but the sign-in could not complete",
			"auth_request_id", req.AuthRequestID, "stage", "complete_after_password_change", "err", err)
		respond.Error(c, status, refusalCodePasswordChangedRestart, refusalMessagePasswordChangedRestart)
		return
	}

	h.deleteAttempt(c, req.AuthRequestID)
	switch result.Outcome {
	case loginclient.OutcomeComplete:
		requestid.Logger(c).InfoContext(c.Request.Context(), "login password change succeeded",
			"auth_request_id", req.AuthRequestID, "outcome", "complete")
		respond.OK(c, passwordSuccessResponse{CallbackURL: result.CallbackURL})
	default:
		// A refusal (the enrolment or policy changed under the login, or
		// the change did not register — RefusalPasswordChangeUnconfirmed),
		// or an outcome this switch does not know, failing closed the same
		// way. Answered like every other refusal.
		h.respondRefusal(c, req.AuthRequestID, "password_change", result)
	}
}

// respondPasswordChangeFailure answers a ChangePassword that did not change
// the password (spec D4 step 4).
func (h *LoginUIHandlers) respondPasswordChangeFailure(c *gin.Context, authRequestID string, err error) {
	log := requestid.Logger(c)
	ctx := c.Request.Context()
	switch {
	case errors.Is(err, loginclient.ErrPasswordUnchanged):
		log.InfoContext(ctx, "login password change rejected",
			"auth_request_id", authRequestID, "outcome", "password_unchanged")
		respond.Error(c, http.StatusUnprocessableEntity, passwordUnchangedCode, passwordUnchangedMessage)

	case errors.Is(err, loginclient.ErrPasswordPolicy):
		rule := loginclient.PasswordPolicyRule(err)
		log.InfoContext(ctx, "login password change rejected",
			"auth_request_id", authRequestID, "outcome", "password_rejected", "password_rule", rule,
			"zitadel_error_id", loginclient.ZitadelErrorID(err))
		message, ok := passwordRuleMessages[rule]
		if !ok {
			message = passwordPolicyMessage
		}
		respond.Error(c, http.StatusUnprocessableEntity, passwordRejectedCode, message)

	case loginclient.IsCredentialRefusal(err):
		// The browser sent the password the user typed moments ago at the
		// credential step, which Zitadel accepted then. A mismatch now is
		// anomalous; ending the attempt keeps this endpoint from becoming a
		// way to guess the current password one held session at a time.
		h.deleteAttempt(c, authRequestID)
		log.WarnContext(ctx, "login password change refused", failureLogAttrs(authRequestID, err)...)
		respond.Error(c, http.StatusForbidden, refusalCodeIncomplete, refusalMessageIncomplete)

	default:
		// Zitadel unavailable (retryable — the row is kept), or an
		// unexpected error.
		h.respondLoginClientError(c, err, "change_password")
	}
}

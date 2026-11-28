package iam

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/audit"
)

// stageParked is a password-verified session a setup step parked for /login
// to resume (#1778). Only takeParked reads it.
const stageParked attemptStage = "parked"

// SetupResumeCookie carries the parked session's handle. Only its hash is stored.
const SetupResumeCookie = "helivanta_setup_handle"

const resumeRateBucket = "login_resume:"

func parkedKey(handle string) string {
	sum := sha256.Sum256([]byte(handle))
	return "parked:" + hex.EncodeToString(sum[:])
}

// ParkSetupSession is called by a password-setup step right after the
// identity owner accepted password for userID in orgID (#1778 spec D1). It
// checks the password once more against Zitadel for exactly that account,
// parks the session and returns the cookie that alone can resume it. The
// password is used for that one call and kept nowhere.
func (h *LoginUIHandlers) ParkSetupSession(ctx context.Context, userID, orgID, password string) (*http.Cookie, error) {
	session, err := h.client.CreateUserPasswordSession(ctx, userID, orgID, password)
	if err != nil {
		return nil, fmt.Errorf("park setup session: %w", err)
	}
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return nil, fmt.Errorf("park setup session: %w", err)
	}
	handle := base64.RawURLEncoding.EncodeToString(raw)
	expires := time.Now().Add(loginAttemptTTL)
	if err = h.store.Put(ctx, loginAttempt{AuthRequestID: parkedKey(handle), SessionID: session.ID, SessionToken: session.Token,
		Subject: userID, ExpiresAt: expires, Stage: stageParked}); err != nil {
		return nil, fmt.Errorf("park setup session: %w", err)
	}
	return &http.Cookie{Name: SetupResumeCookie, Value: handle, Path: "/", Secure: true, HttpOnly: true,
		SameSite: http.SameSiteStrictMode, MaxAge: int(loginAttemptTTL.Seconds())}, nil
}

// takeParked deletes and returns the live parked row in one statement, so a
// handle resumes at most once even under concurrent requests.
func (s *loginAttemptStore) takeParked(ctx context.Context, handle string) (loginAttempt, error) {
	var rows []loginAttemptRow
	err := s.db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`DELETE FROM login_attempt WHERE auth_request_id = ? AND stage = ? RETURNING *`, parkedKey(handle), stageParked).Scan(&rows).Error
	})
	if err != nil {
		return loginAttempt{}, fmt.Errorf("take parked session: %w", err)
	}
	if len(rows) != 1 || !rows[0].ExpiresAt.After(time.Now()) {
		return loginAttempt{}, errAttemptNotFound
	}
	return rows[0].toDomain(), nil
}

type resumeRequest struct {
	AuthRequestID string `json:"auth_request_id" binding:"required,max=200"`
}

// Resume continues a sign-in with the session a setup step parked (#1778
// spec D3). It answers exactly as the password step does, so finalising
// still happens only through CompleteIfSufficient (D4). Anything it cannot
// use — no cookie, an expired, replayed or unknown handle, another account's
// organisation — is a 404 and the page shows the ordinary email card.
func (h *LoginUIHandlers) Resume(c *gin.Context) {
	var req resumeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.BadRequest(c, err)
		return
	}
	if !h.allowedByLimiter(c, resumeRateBucket, h.limit) {
		return
	}
	handle, err := c.Cookie(SetupResumeCookie)
	// Single use whatever happens next: the browser forgets the handle.
	http.SetCookie(c.Writer, &http.Cookie{Name: SetupResumeCookie, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	// Without this deployment's client id the auth request cannot be bound,
	// so the shortcut is off and the ordinary card answers (fail closed).
	if err != nil || handle == "" || len(handle) > 64 || h.nativeWebClientID == "" {
		respond.NotFound(c, "Nothing to continue.")
		return
	}
	attempt, err := h.store.takeParked(c.Request.Context(), handle)
	if errors.Is(err, errAttemptNotFound) {
		respond.NotFound(c, "Nothing to continue.")
		return
	}
	if err != nil {
		respond.InternalErr(c, err, "sign-in could not be continued")
		return
	}
	org, err := h.client.AuthRequestOrganization(c.Request.Context(), req.AuthRequestID, h.nativeWebClientID)
	if err != nil {
		h.respondLoginClientError(c, err, "resume")
		return
	}
	if err = h.client.SessionBelongsTo(c.Request.Context(), attempt.SessionID, attempt.Subject, org); err != nil {
		if errors.Is(err, loginclient.ErrAuthRequestInvalid) {
			audit.Log(c.Request.Context(), requestid.Logger(c), slog.LevelWarn, "auth.login.resume_refused", "parked session does not match the auth request",
				"auth_request_id", req.AuthRequestID)
			respond.NotFound(c, "Nothing to continue.")
			return
		}
		h.respondLoginClientError(c, err, "resume")
		return
	}
	h.continueWithSession(c, req.AuthRequestID, attempt.Subject, loginclient.Session{ID: attempt.SessionID, Token: attempt.SessionToken})
}

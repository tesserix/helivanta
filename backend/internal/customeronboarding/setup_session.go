package customeronboarding

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// SessionParker parks a password-verified sign-in session for the account a
// setup step has just given a password (#1778). IAM implements it; this
// package only hands over the account and sets the returned cookie.
type SessionParker interface {
	ParkSetupSession(ctx context.Context, userID, orgID, password string) (*http.Cookie, error)
}

// SetSessionParker wires the parker after IAM's login handlers exist.
func (h *Handlers) SetSessionParker(p SessionParker) { h.parker = p }

// parkSetupSession is a shortcut, never a requirement (#1778 spec D5): any
// failure leaves setup successful, sets no cookie, and the page signs in the
// ordinary way. Only the stage is logged.
func (h *Handlers) parkSetupSession(c *gin.Context, userID, orgID, password string) {
	if h.parker == nil || password == "" || userID == "" || orgID == "" {
		return
	}
	cookie, err := h.parker.ParkSetupSession(c.Request.Context(), userID, orgID, password)
	if err != nil || cookie == nil {
		slog.WarnContext(c.Request.Context(), "setup sign-in continuation unavailable", "stage", "park")
		return
	}
	http.SetCookie(c.Writer, cookie)
}

// ownerOrganization is the native organisation of a signup owner, whose
// subject is always owner-<tenant> in helivanta-<tenant>.
func ownerOrganization(userID string) string {
	tenant, ok := strings.CutPrefix(userID, "owner-")
	if !ok || uuid.Validate(tenant) != nil {
		return ""
	}
	return "helivanta-" + tenant
}

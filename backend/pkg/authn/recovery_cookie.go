package authn

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

// PasswordRecoveryCookie carries only a short-lived, browser-bound recovery capability.
// #nosec G101 -- This is a public cookie name, not credential material.
const PasswordRecoveryCookie = "__Host-helivanta-password-recovery"

// PasswordRecoveryCommandCookie selects the retained intent after a lost password response.
// #nosec G101 -- This is a public cookie name, not credential material.
const PasswordRecoveryCommandCookie = "__Host-helivanta-recovery-command"

// ClearPasswordRecovery prevents reuse by the next shared-terminal user.
func ClearPasswordRecovery(c *gin.Context) {
	for _, name := range []string{PasswordRecoveryCookie, PasswordRecoveryCommandCookie} {
		http.SetCookie(c.Writer, &http.Cookie{Name: name, Value: "", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	}
}

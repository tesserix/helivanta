package authn

import (
	"github.com/gin-gonic/gin"
	"net/http"
)

// AccountDiscoveryCookie carries only a short-lived, host-bound mailbox capability.
const AccountDiscoveryCookie = "__Host-helivanta-accounts"

// ClearAccountDiscovery prevents account-list reuse by the next shared-terminal user.
func ClearAccountDiscovery(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{Name: AccountDiscoveryCookie, Value: "", Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode, MaxAge: -1})
}

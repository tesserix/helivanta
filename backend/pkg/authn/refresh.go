package authn

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

const entryRefreshKey = "authn.entry_refresh"
const identityRefreshKey = "authn.identity_refresh"

// BindIdentityRefresh retains the entry identity guard for later private work.
func BindIdentityRefresh(c *gin.Context, check func() bool) { c.Set(identityRefreshKey, check) }

// Refresh rechecks the original credential and any current identity guard at entry.
func Refresh(c *gin.Context) bool {
	c.Header("Cache-Control", "no-store")
	raw, ok := c.Get(entryRefreshKey)
	check, valid := raw.(func() bool)
	if !ok || !valid || check == nil {
		refreshUnavailable(c)
		return false
	}
	if !check() {
		return false
	}
	raw, ok = c.Get(identityRefreshKey)
	if !ok {
		return true
	}
	check, valid = raw.(func() bool)
	if !valid || check == nil {
		refreshUnavailable(c)
		return false
	}
	if !check() {
		if !c.IsAborted() {
			refreshUnavailable(c)
		}
		return false
	}
	return true
}

func samePrincipal(a, b Principal) bool {
	return a.Subject == b.Subject && a.TenantID == b.TenantID && a.AuthTime.Equal(b.AuthTime) && a.IdleDeadline.Equal(b.IdleDeadline)
}

func rejectChangedCredential(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthenticated", "message": "credential identity changed"})
}

func refreshUnavailable(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"error": "authz_unavailable", "message": "authorization is temporarily unavailable"})
}

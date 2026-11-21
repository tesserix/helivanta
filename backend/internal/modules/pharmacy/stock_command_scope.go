package pharmacy

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
)

func stockCommandScope(c *gin.Context) (authn.Principal, uuid.UUID, bool) {
	p, outlet, ok := catalogueScope(c)
	if !ok {
		return p, uuid.Nil, false
	}
	if expected := c.Request.Header.Values("X-Helivanta-Expected-Subject"); len(expected) > 0 && (len(expected) != 1 || expected[0] != p.Subject) {
		respond.Error(c, http.StatusConflict, "session_changed", "Your account changed. Refresh the page before continuing.")
		return p, uuid.Nil, false
	}
	return p, outlet, true
}

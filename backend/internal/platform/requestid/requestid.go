// Package requestid tags every request with an id for log correlation.
package requestid

import (
	"log/slog"
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/tesserix/helivanta/pkg/authn"
)

const Key = "request_id"

const loggerKey = "request_logger"

// validRequestID bounds inbound X-Request-ID values to a safe charset and
// length before they are echoed back or used in log correlation.
var validRequestID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" || !validRequestID.MatchString(id) {
			id = uuid.NewString()
		}
		c.Set(Key, id)
		c.Set(loggerKey, slog.Default().With("request_id", id))
		c.Header("X-Request-ID", id)
		c.Next()
	}
}

// Logger returns the request-scoped logger (falls back to the default).
func Logger(c *gin.Context) *slog.Logger {
	if v, ok := c.Get(loggerKey); ok {
		if l, ok := v.(*slog.Logger); ok {
			return l
		}
	}
	return slog.Default()
}

// Enrich binds additional fields to the request-scoped logger for the rest
// of this request. slog.Logger.With returns a new logger rather than
// mutating the receiver, so this cannot leak fields into another request.
func Enrich(c *gin.Context, args ...any) {
	if len(args) == 0 {
		return
	}
	c.Set(loggerKey, Logger(c).With(args...))
}

// PrincipalMiddleware adds tenant_id and subject to the request logger once
// authn has populated the context. It must be registered after
// authn.Middleware and before the handlers.
//
// This lives in the platform layer rather than in pkg/authn deliberately.
// internal/ may import pkg/; the reverse is the dependency inversion the
// foundation audit flagged, and having pkg/authn reach into
// internal/platform/requestid would deepen it for no benefit.
//
// Neither field is patient data: subject is a Zitadel user id, pseudonymous by
// construction, and tenant_id is a UUID. With request_id they answer which
// hospital, which user, which request — without naming anyone.
func PrincipalMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Unauthenticated requests still log (a 401 is worth a line). An
		// absent principal leaves the logger untouched rather than binding
		// empty strings, which would read in a query as a real tenant of "".
		if p, ok := authn.PrincipalFrom(c); ok {
			Enrich(c, "tenant_id", p.TenantID, "subject", p.Subject)
		}
		c.Next()
	}
}

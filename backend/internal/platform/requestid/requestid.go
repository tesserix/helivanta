// Package requestid tags every request with an id for log correlation.
package requestid

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const Key = "request_id"

const loggerKey = "request_logger"

func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if id == "" {
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

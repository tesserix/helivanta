// Package respond centralizes the HTTP envelopes. Success helpers pass
// the body through unchanged (shapes are frozen by the frontend/E2E);
// error helpers own the {"error","message"} envelope.
package respond

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/hms/internal/platform/requestid"
)

func OK(c *gin.Context, data any)       { c.JSON(http.StatusOK, data) }
func Created(c *gin.Context, data any)  { c.JSON(http.StatusCreated, data) }
func Accepted(c *gin.Context, data any) { c.JSON(http.StatusAccepted, data) }

func Error(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": code, "message": message})
}

func NotFound(c *gin.Context, resource string) {
	Error(c, http.StatusNotFound, "not_found", resource+" not found")
}

func Conflict(c *gin.Context, message string) {
	Error(c, http.StatusConflict, "conflict", message)
}

func BadRequest(c *gin.Context, err error) {
	Error(c, http.StatusBadRequest, "invalid_request", err.Error())
}

// InternalErr logs the underlying cause against the request id and returns
// the client-safe message.
//
// It replaces a plain Internal(c, msg): 15 call sites discarded their
// error, so a production 500 gave the client a generic string and the
// operator nothing — no error text, no SQLSTATE, no request id. The
// discarding form is deliberately not offered, so it cannot come back.
func InternalErr(c *gin.Context, err error, message string) {
	requestid.Logger(c).ErrorContext(c.Request.Context(), message, "err", err)
	Error(c, http.StatusInternalServerError, "internal", message)
}

func Unauthenticated(c *gin.Context, message string) {
	Error(c, http.StatusUnauthorized, "unauthenticated", message)
}

// Forbidden means the caller is a member of the tenant but lacks the
// permission. Cross-tenant access returns NotFound instead — the record
// does not exist for that caller.
func Forbidden(c *gin.Context, message string) {
	Error(c, http.StatusForbidden, "forbidden", message)
}

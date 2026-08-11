// Package respond centralizes the HTTP envelopes. Success helpers pass
// the body through unchanged (shapes are frozen by the frontend/E2E);
// error helpers own the {"error","message"} envelope.
package respond

import (
	"net/http"

	"github.com/gin-gonic/gin"
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

func Internal(c *gin.Context, message string) {
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

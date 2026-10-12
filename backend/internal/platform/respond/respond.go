// Package respond centralizes the HTTP envelopes. Success helpers pass
// the body through unchanged (shapes are frozen by the frontend/E2E);
// error helpers own the {"error","message"} envelope.
package respond

import (
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/platform/requestid"
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

// ConflictWithDetail is Conflict plus caller-supplied fields merged into
// the same envelope, for a 409 the client must act on programmatically
// rather than just display — e.g. patient registration's confident
// duplicate carries the matched patient (id, MRN, name and score), which
// is what a clerk needs to recognise the existing record and decide
// whether it is the same person. It deliberately carries a description,
// not a full record: anything beyond identification is a read under the
// caller's own permission.
func ConflictWithDetail(c *gin.Context, message string, detail gin.H) {
	body := gin.H{"error": "conflict", "message": message}
	for k, v := range detail {
		body[k] = v
	}
	c.AbortWithStatusJSON(http.StatusConflict, body)
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

// TooManyRequests refuses a request that exceeded its rate budget.
//
// Retry-After is seconds (RFC 7231) and is rounded UP: a sub-second wait
// must never render as "0", because a client told to retry after zero
// seconds retries immediately and turns a limiter into an amplifier.
// The offline-first mobile clients in the backlog back off on this
// header, so it is load-bearing rather than informational.
//
// Takes primitives rather than a ratelimit.Decision on purpose: this
// package is under internal/platform, and importing pkg/ratelimit here
// would deepen the pkg/ -> internal/ inversion the foundation audit
// flagged rather than leaving it where it is.
func TooManyRequests(c *gin.Context, message string, retryAfter time.Duration, limit, remaining int) {
	secs := int(math.Ceil(retryAfter.Seconds()))
	if secs < 1 {
		secs = 1
	}
	c.Header("Retry-After", strconv.Itoa(secs))
	c.Header("RateLimit-Limit", strconv.Itoa(limit))
	c.Header("RateLimit-Remaining", strconv.Itoa(remaining))
	c.Header("RateLimit-Reset", strconv.Itoa(secs))
	Error(c, http.StatusTooManyRequests, "rate_limited", message)
}

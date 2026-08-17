package respond_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/platform/respond"
)

func run(h gin.HandlerFunc) *httptest.ResponseRecorder {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/t", h)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/t", nil))
	return w
}

func TestSuccessHelpersPreserveShapes(t *testing.T) {
	w := run(func(c *gin.Context) { respond.OK(c, gin.H{"data": []string{"a"}}) })
	require.Equal(t, http.StatusOK, w.Code)
	require.JSONEq(t, `{"data":["a"]}`, w.Body.String())

	w = run(func(c *gin.Context) { respond.Accepted(c, gin.H{"id": "x"}) })
	require.Equal(t, http.StatusAccepted, w.Code)
	require.JSONEq(t, `{"id":"x"}`, w.Body.String())
}

func TestErrorHelpers(t *testing.T) {
	w := run(func(c *gin.Context) { respond.NotFound(c, "ping") })
	require.Equal(t, http.StatusNotFound, w.Code)
	require.JSONEq(t, `{"error":"not_found","message":"ping not found"}`, w.Body.String())

	w = run(func(c *gin.Context) { respond.Conflict(c, "already dispensed") })
	require.Equal(t, http.StatusConflict, w.Code)
	require.JSONEq(t, `{"error":"conflict","message":"already dispensed"}`, w.Body.String())

	w = run(func(c *gin.Context) { respond.BadRequest(c, errors.New("bad field")) })
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.JSONEq(t, `{"error":"invalid_request","message":"bad field"}`, w.Body.String())

	w = run(func(c *gin.Context) { respond.InternalErr(c, errors.New("insert failed"), "could not record ping") })
	require.Equal(t, http.StatusInternalServerError, w.Code)
	require.JSONEq(t, `{"error":"internal","message":"could not record ping"}`, w.Body.String())
}

func TestForbiddenEnvelope(t *testing.T) {
	w := run(func(c *gin.Context) { respond.Forbidden(c, "missing permission") })

	require.Equal(t, http.StatusForbidden, w.Code)
	require.JSONEq(t, `{"error":"forbidden","message":"missing permission"}`, w.Body.String())
}

func TestTooManyRequestsCarriesTheBackoffHeaders(t *testing.T) {
	w := run(func(c *gin.Context) {
		respond.TooManyRequests(c, "too many requests for this principal; retry in 12s",
			12*time.Second, 120, 0)
	})

	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.JSONEq(t, `{"error":"rate_limited","message":"too many requests for this principal; retry in 12s"}`, w.Body.String())

	// Retry-After is seconds per RFC 7231, and it is the header the
	// offline-first mobile clients back off on. A client that retries
	// immediately turns a limiter into an amplifier.
	require.Equal(t, "12", w.Header().Get("Retry-After"))
	require.Equal(t, "120", w.Header().Get("RateLimit-Limit"))
	require.Equal(t, "0", w.Header().Get("RateLimit-Remaining"))
	require.Equal(t, "12", w.Header().Get("RateLimit-Reset"))
}

// TestTooManyRequestsRoundsSubSecondRetryUp: Retry-After has
// second granularity, so a 400ms wait must render as 1, not 0. A client
// told to retry after 0 seconds retries immediately, which is the
// amplification this header exists to prevent.
func TestTooManyRequestsRoundsSubSecondRetryUp(t *testing.T) {
	w := run(func(c *gin.Context) {
		respond.TooManyRequests(c, "slow down", 400*time.Millisecond, 120, 0)
	})
	require.Equal(t, "1", w.Header().Get("Retry-After"))
}

// TestTooManyRequestsRoundsAMultiSecondWaitUp is the case that actually
// discriminates the rounding, and it exists because the obvious test did
// not.
//
// TestTooManyRequestsRoundsSubSecondRetryUp looks like it pins Ceil, but
// the `secs < 1` clamp rescues every sub-second value: at 400ms, Floor
// gives 0, the clamp bumps it to 1, and the header is identical either
// way. Swapping Ceil for Floor passed the whole suite.
//
// 1.5s is above the clamp, so the two disagree: Ceil gives 2, Floor
// gives 1. Rounding down tells a client to come back before a token
// exists, so it is refused again — the retry storm Retry-After is
// supposed to prevent, arriving through the header meant to prevent it.
func TestTooManyRequestsRoundsAMultiSecondWaitUp(t *testing.T) {
	w := run(func(c *gin.Context) {
		respond.TooManyRequests(c, "slow down", 1500*time.Millisecond, 120, 0)
	})

	require.Equal(t, "2", w.Header().Get("Retry-After"),
		"a 1.5s wait must round up to 2: telling a client to retry after 1s sends it back before a token exists")
	require.Equal(t, "2", w.Header().Get("RateLimit-Reset"))
}

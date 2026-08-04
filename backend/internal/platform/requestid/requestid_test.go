package requestid_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/platform/requestid"
)

func TestMiddlewareGeneratesAndEchoesIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(requestid.Middleware())
	r.GET("/t", func(c *gin.Context) {
		require.NotNil(t, requestid.Logger(c))
		id := c.GetString(requestid.Key)
		require.NotEmpty(t, id)
		c.String(http.StatusOK, id)
	})

	// Generated when absent.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/t", nil))
	require.Equal(t, w.Body.String(), w.Header().Get("X-Request-ID"))
	require.NotEmpty(t, w.Body.String())

	// Propagated when present.
	w = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.Header.Set("X-Request-ID", "req-123")
	r.ServeHTTP(w, req)
	require.Equal(t, "req-123", w.Body.String())
	require.Equal(t, "req-123", w.Header().Get("X-Request-ID"))
}

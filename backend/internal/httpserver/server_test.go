package httpserver_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/httpserver"
)

func TestEveryResponseCarriesSecurityHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := httpserver.New(nil, nil)
	srv.Engine.GET("/cached", func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=60")
		c.Status(http.StatusOK)
	})

	for _, path := range []string{"/healthz", "/no-such-route", "/cached"} {
		w := httptest.NewRecorder()
		srv.Engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		h := w.Header()
		require.Equal(t, "max-age=31536000; includeSubDomains", h.Get("Strict-Transport-Security"), path)
		require.Equal(t, "nosniff", h.Get("X-Content-Type-Options"), path)
		require.Equal(t, "DENY", h.Get("X-Frame-Options"), path)
		require.Equal(t, "no-referrer", h.Get("Referrer-Policy"), path)
		require.Equal(t, "default-src 'none'; frame-ancestors 'none'", h.Get("Content-Security-Policy"), path)
	}
}

func TestResponsesAreUncachedUnlessAHandlerSaysOtherwise(t *testing.T) {
	gin.SetMode(gin.TestMode)
	srv := httpserver.New(nil, nil)
	srv.Engine.GET("/cached", func(c *gin.Context) {
		c.Header("Cache-Control", "public, max-age=60")
		c.Status(http.StatusOK)
	})

	for path, want := range map[string]string{"/healthz": "no-store", "/cached": "public, max-age=60"} {
		w := httptest.NewRecorder()
		srv.Engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, want, w.Header().Get("Cache-Control"), path)
	}
}

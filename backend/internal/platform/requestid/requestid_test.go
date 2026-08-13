package requestid_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/platform/requestid"
	"github.com/tesserix/hms/pkg/authn"
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

func TestMiddlewareRejectsInvalidInboundIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(requestid.Middleware())
	r.GET("/t", func(c *gin.Context) {
		c.String(http.StatusOK, c.GetString(requestid.Key))
	})

	overlong := ""
	for i := 0; i < 129; i++ {
		overlong += "a"
	}

	for name, id := range map[string]string{
		"over-long":       overlong,
		"invalid-charset": "req 123!",
	} {
		t.Run(name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/t", nil)
			req.Header.Set("X-Request-ID", id)
			r.ServeHTTP(w, req)

			got := w.Body.String()
			require.NotEmpty(t, got)
			require.NotEqual(t, id, got)
			require.Equal(t, got, w.Header().Get("X-Request-ID"))
			_, err := uuid.Parse(got)
			require.NoError(t, err)
		})
	}
}

// captureLogger swaps slog.Default for one writing JSON to buf, restoring
// the original when the test ends.
func captureLogger(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestPrincipalMiddlewareAddsTenantAndSubject(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buf := captureLogger(t)

	r := gin.New()
	r.Use(requestid.Middleware())
	r.Use(func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{Subject: "gip-uid-42", TenantID: "11111111-1111-1111-1111-111111111111"})
		c.Next()
	})
	r.Use(requestid.PrincipalMiddleware())
	r.GET("/t", func(c *gin.Context) {
		requestid.Logger(c).Info("handled")
		c.Status(http.StatusOK)
	})

	req := httptest.NewRequest("GET", "/t", nil)
	req.Header.Set("X-Request-ID", "req-abc")
	r.ServeHTTP(httptest.NewRecorder(), req)

	var line map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &line))
	require.Equal(t, "handled", line["msg"])
	require.Equal(t, "req-abc", line["request_id"])
	require.Equal(t, "11111111-1111-1111-1111-111111111111", line["tenant_id"])
	require.Equal(t, "gip-uid-42", line["subject"])
}

// The middleware runs on unauthenticated paths too (a 401 still logs). With
// no principal it must leave the logger exactly as requestid.Middleware left
// it, not attach empty fields that would read as a real tenant of "".
func TestPrincipalMiddlewareIsANoOpWithoutAPrincipal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buf := captureLogger(t)

	r := gin.New()
	r.Use(requestid.Middleware(), requestid.PrincipalMiddleware())
	r.GET("/t", func(c *gin.Context) {
		requestid.Logger(c).Info("handled")
		c.Status(http.StatusOK)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/t", nil))

	var line map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &line))
	require.NotEmpty(t, line["request_id"])
	_, hasTenant := line["tenant_id"]
	require.False(t, hasTenant, "an absent principal must not produce an empty tenant_id field")
	_, hasSubject := line["subject"]
	require.False(t, hasSubject)
}

func TestEnrichIsScopedToTheRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	buf := captureLogger(t)

	r := gin.New()
	r.Use(requestid.Middleware())
	r.GET("/t", func(c *gin.Context) {
		requestid.Enrich(c, "module", "medicore")
		requestid.Logger(c).Info("handled")
		c.Status(http.StatusOK)
	})
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/t", nil))

	var line map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &line))
	require.Equal(t, "medicore", line["module"])

	// A second request must not inherit the first request's field.
	buf.Reset()
	r2 := gin.New()
	r2.Use(requestid.Middleware())
	r2.GET("/t", func(c *gin.Context) {
		requestid.Logger(c).Info("handled")
		c.Status(http.StatusOK)
	})
	r2.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/t", nil))
	line = map[string]any{}
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &line))
	_, leaked := line["module"]
	require.False(t, leaked, "Enrich must not mutate a logger shared across requests")
}

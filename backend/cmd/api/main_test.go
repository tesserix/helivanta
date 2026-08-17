package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/httpserver"
	"github.com/tesserix/helivanta/pkg/logging"
)

// TestGinRecoveryOutputIsRedacted proves the fix for the gin.Recovery() gap:
// gin builds its own log.New(gin.DefaultErrorWriter, ...) rather than using
// log.Default(), so slog.SetDefault's redirection of the standard library
// log package never reaches a handler panic's recovery output. Without
// wrapping gin.DefaultErrorWriter in run() (backend/cmd/api/main.go), a
// panic carrying a PHI-shaped value — a mobile number, here — reaches
// os.Stderr completely unscreened.
//
// This wires an httpserver.Server exactly as run() does (gin.New() +
// gin.Recovery(), see internal/httpserver/server.go), points
// gin.DefaultErrorWriter at logging.NewRedactingWriter the same way run()
// does, triggers a real panic through the engine, and asserts the captured
// output carries no raw PHI. It restores the prior gin.DefaultErrorWriter
// so it cannot leak into other tests in this package or others that run in
// the same process.
func TestGinRecoveryOutputIsRedacted(t *testing.T) {
	prev := gin.DefaultErrorWriter
	t.Cleanup(func() { gin.DefaultErrorWriter = prev })

	var buf bytes.Buffer
	gin.DefaultErrorWriter = logging.NewRedactingWriter(&buf)

	gin.SetMode(gin.TestMode)
	srv := httpserver.New(nil)
	srv.Engine.GET("/panic", func(c *gin.Context) {
		panic("patient 9876543210 not found")
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/panic", nil)

	// gin.Recovery() catches the panic itself and responds 500; it must not
	// propagate out of ServeHTTP.
	require.NotPanics(t, func() { srv.Engine.ServeHTTP(w, req) })
	require.Equal(t, http.StatusInternalServerError, w.Code)

	out := buf.String()
	require.NotEmpty(t, out, "gin.Recovery() must have written something to gin.DefaultErrorWriter")
	require.NotContains(t, out, "9876543210",
		"raw PHI reached gin's recovery output unredacted: %s", out)
	require.Contains(t, out, "[REDACTED:mobile]",
		"expected the mobile-number marker in the redacted recovery output: %s", out)
}

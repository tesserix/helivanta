package testutil_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/internal/platform/requestid"
	"github.com/tesserix/hms/internal/testutil"
	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
)

// logModule is a minimal platform.Module whose only route emits a log line
// through requestid.Logger(c), so tests can observe exactly what a real
// module handler's request-scoped logger carries. It exists only to prove
// the NewHarness middleware chain — production modules should not need
// to grow test-only routes just to exercise this.
type logModule struct{}

func (logModule) Name() string                              { return "logprobe" }
func (logModule) Migrations() []tenantdb.Migration          { return nil }
func (logModule) Permissions() []authz.Grant                { return nil }
func (logModule) Consumers(platform.Deps) []events.Consumer   { return nil }
func (logModule) Broadcasts(platform.Deps) []events.Broadcast { return nil }

func (logModule) Routes(r *platform.Router, _ platform.Deps) {
	r.Group("/logprobe").GET("/ping", authz.Public, func(c *gin.Context) {
		requestid.Logger(c).Info("handled")
		c.Status(http.StatusOK)
	})
}

// TestNewHarnessRequestScopedLoggerCarriesCorrelationFields proves
// NewHarness wires the same middleware chain, in the same order, as
// cmd/api/main.go: requestid.Middleware() at the engine level, then authn,
// then requestid.PrincipalMiddleware(), then authz. Before this test (and
// the harness fix it accompanies), the harness built its engine without
// requestid.Middleware() or requestid.PrincipalMiddleware() at all, so every
// module integration test ran with requestid.Logger(c) silently falling
// back to a bare slog.Default() — no request_id, no tenant_id, no subject —
// and nothing in the suite noticed.
func TestNewHarnessRequestScopedLoggerCarriesCorrelationFields(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"tokA": testutil.TenantA},
		Modules: []platform.Module{logModule{}},
	})

	w := testutil.Do(r, http.MethodGet, "/v1/logprobe/ping", "tokA", "")
	require.Equal(t, http.StatusOK, w.Code)

	var line map[string]any
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(lastLine(buf.String()))), &line),
		"raw log output: %s", buf.String())
	require.Equal(t, "handled", line["msg"])
	require.NotEmpty(t, line["request_id"], "harness must wire requestid.Middleware()")
	require.Equal(t, testutil.TenantA, line["tenant_id"], "harness must wire requestid.PrincipalMiddleware() after authn")
	require.Equal(t, "user-tokA", line["subject"])
}

// lastLine returns the final non-empty line of s, in case boot or other
// incidental logging shares the buffer.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

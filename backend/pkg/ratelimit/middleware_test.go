package ratelimit_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/ratelimit"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// principalStub stands in for authn.Middleware, which is tested elsewhere:
// it reads the test-chosen subject/tenant from headers so each call site
// can drive its own principal without a real token.
func principalStub() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{
			Subject:  c.GetHeader("X-Test-Subject"),
			TenantID: c.GetHeader("X-Test-Tenant"),
		})
		c.Next()
	}
}

func harness(t *testing.T, cfg ratelimit.Config) *gin.Engine {
	t.Helper()
	e := gin.New()
	e.Use(principalStub(), ratelimit.Middleware(ratelimit.NewMemory(1000), cfg))
	e.GET("/v1/things", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	e.POST("/v1/mint", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	e.POST("/v1/escape", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	return e
}

// harnessCounting is TestThrottledRequestNeverReachesTheHandler's harness:
// it counts how many times the handler behind the limiter actually runs, so
// a refusal that nonetheless did the work it was refused for is caught.
func harnessCounting(t *testing.T, cfg ratelimit.Config, reached *int) *gin.Engine {
	t.Helper()
	e := gin.New()
	e.Use(principalStub(), ratelimit.Middleware(ratelimit.NewMemory(1000), cfg))
	e.GET("/v1/things", func(c *gin.Context) {
		*reached++
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return e
}

// countingHandler counts every log record with the given message and
// otherwise discards it. It exists so TestExemptRouteIsStillRecorded can
// assert on an actual emitted record rather than assume one was written.
type countingHandler struct {
	want string
	n    *int
}

func (h countingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h countingHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Message == h.want {
		*h.n++
	}
	return nil
}

func (h countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h countingHandler) WithGroup(string) slog.Handler      { return h }

// harnessCapturingLogs swaps the default slog logger for one that counts
// the exempt-route log line, restoring the previous default on cleanup so
// no other test observes the swap.
func harnessCapturingLogs(t *testing.T, cfg ratelimit.Config, logged *int) *gin.Engine {
	t.Helper()
	prev := slog.Default()
	slog.SetDefault(slog.New(countingHandler{want: "rate limit exempt route", n: logged}))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return harness(t, cfg)
}

func request(e *gin.Engine, method, path, subject, tenant string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("X-Test-Subject", subject)
	req.Header.Set("X-Test-Tenant", tenant)
	e.ServeHTTP(w, req)
	return w
}

func do(e *gin.Engine, path, subject, tenant string) *httptest.ResponseRecorder {
	return request(e, http.MethodGet, path, subject, tenant)
}

func doPost(e *gin.Engine, path, subject, tenant string) *httptest.ResponseRecorder {
	return request(e, http.MethodPost, path, subject, tenant)
}

// TestBothBucketsAreEnforced: a tenant within budget whose principal is
// exhausted must still be refused, and vice versa. Checking only whichever
// comes first would leave one of the two protections decorative.
func TestBothBucketsAreEnforced(t *testing.T) {
	t.Run("principal exhausted, tenant fine", func(t *testing.T) {
		r := harness(t, ratelimit.Config{
			Tenant:    ratelimit.Rule{Rate: 600, Burst: 100, Per: time.Minute},
			Principal: ratelimit.Rule{Rate: 120, Burst: 2, Per: time.Minute},
		})
		require.Equal(t, http.StatusOK, do(r, "/v1/things", "alice", "tenant-a").Code)
		require.Equal(t, http.StatusOK, do(r, "/v1/things", "alice", "tenant-a").Code)
		w := do(r, "/v1/things", "alice", "tenant-a")
		require.Equal(t, http.StatusTooManyRequests, w.Code)
		require.Contains(t, w.Body.String(), "principal",
			"the denial must name which bucket was exhausted")
	})

	t.Run("tenant exhausted, principal fine", func(t *testing.T) {
		r := harness(t, ratelimit.Config{
			Tenant:    ratelimit.Rule{Rate: 600, Burst: 2, Per: time.Minute},
			Principal: ratelimit.Rule{Rate: 120, Burst: 100, Per: time.Minute},
		})
		// Two different principals in the same tenant drain the tenant bucket.
		require.Equal(t, http.StatusOK, do(r, "/v1/things", "alice", "tenant-a").Code)
		require.Equal(t, http.StatusOK, do(r, "/v1/things", "bob", "tenant-a").Code)
		w := do(r, "/v1/things", "carol", "tenant-a")
		require.Equal(t, http.StatusTooManyRequests, w.Code)
		require.Contains(t, w.Body.String(), "tenant",
			"a tenant-level denial must say so — 'you' and 'your whole hospital' are different answers")
	})
}

// TestTightRuleAppliesToItsRoute proves the tight budget is applied and not
// merely declared. Without this, the map could be ignored entirely and
// every other test would still pass.
func TestTightRuleAppliesToItsRoute(t *testing.T) {
	r := harness(t, ratelimit.Config{
		Tenant:    ratelimit.Rule{Rate: 600, Burst: 100, Per: time.Minute},
		Principal: ratelimit.Rule{Rate: 120, Burst: 100, Per: time.Minute},
		Tight:     map[string]ratelimit.Rule{"POST /v1/mint": {Rate: 10, Burst: 1, Per: time.Minute}},
	})
	require.Equal(t, http.StatusOK, doPost(r, "/v1/mint", "alice", "tenant-a").Code)
	require.Equal(t, http.StatusTooManyRequests, doPost(r, "/v1/mint", "alice", "tenant-a").Code,
		"the tight rule's burst of 1 must govern, not the default burst of 100")

	// The default route is untouched by the tight rule.
	require.Equal(t, http.StatusOK, do(r, "/v1/things", "alice", "tenant-a").Code)
}

// TestExemptRouteIsNeverThrottled: sign-out and admin revoke must work
// during the incident that trips the limiter. An attacker looping requests
// is exactly what exhausts a bucket, at the moment an administrator most
// needs to cut the credential off.
func TestExemptRouteIsNeverThrottled(t *testing.T) {
	r := harness(t, ratelimit.Config{
		Tenant:    ratelimit.Rule{Rate: 600, Burst: 1, Per: time.Minute},
		Principal: ratelimit.Rule{Rate: 120, Burst: 1, Per: time.Minute},
		Exempt:    map[string]string{"POST /v1/escape": "test exemption"},
	})
	for i := range 100 {
		require.Equal(t, http.StatusOK, doPost(r, "/v1/escape", "alice", "tenant-a").Code,
			"exempt request %d must not be throttled even at 100x the burst", i+1)
	}
}

// TestExemptRouteIsStillRecorded: exempt from the LIMIT, not from
// visibility. An exempt route is one an attacker may hammer without being
// refused, so the one thing that must not also be true is that nobody can
// see it happening. Without this, "exempt" quietly means "invisible" and
// the exemption list becomes a blind spot rather than a considered trade.
func TestExemptRouteIsStillRecorded(t *testing.T) {
	var logged int
	r := harnessCapturingLogs(t, ratelimit.Config{
		Tenant:    ratelimit.Rule{Rate: 600, Burst: 1, Per: time.Minute},
		Principal: ratelimit.Rule{Rate: 120, Burst: 1, Per: time.Minute},
		Exempt:    map[string]string{"POST /v1/escape": "test exemption"},
	}, &logged)

	for range 5 {
		require.Equal(t, http.StatusOK, doPost(r, "/v1/escape", "alice", "tenant-a").Code)
	}
	require.Equal(t, 5, logged,
		"every exempt request must be recorded; an exemption nobody can observe is a blind spot")
}

// TestThrottledRequestNeverReachesTheHandler is the placement proof in
// miniature: a refused request must not do the work it was refused for.
// Task 4 proves the same property against the real OpenFGA middleware.
func TestThrottledRequestNeverReachesTheHandler(t *testing.T) {
	reached := 0
	r := harnessCounting(t, ratelimit.Config{
		Tenant:    ratelimit.Rule{Rate: 600, Burst: 100, Per: time.Minute},
		Principal: ratelimit.Rule{Rate: 120, Burst: 1, Per: time.Minute},
	}, &reached)

	require.Equal(t, http.StatusOK, do(r, "/v1/things", "alice", "tenant-a").Code)
	require.Equal(t, http.StatusTooManyRequests, do(r, "/v1/things", "alice", "tenant-a").Code)
	require.Equal(t, 1, reached, "the throttled request must not have run the handler")
}

// TestAllowedRequestCarriesRemainingHeader: clients pace themselves on
// these on the happy path, not only when refused.
func TestAllowedRequestCarriesRemainingHeader(t *testing.T) {
	r := harness(t, ratelimit.Config{
		Tenant:    ratelimit.Rule{Rate: 600, Burst: 100, Per: time.Minute},
		Principal: ratelimit.Rule{Rate: 120, Burst: 20, Per: time.Minute},
	})
	w := do(r, "/v1/things", "alice", "tenant-a")
	require.Equal(t, "120", w.Header().Get("RateLimit-Limit"))
	require.Equal(t, "19", w.Header().Get("RateLimit-Remaining"))
}

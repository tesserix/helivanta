package globalapi

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOwnerFailuresLogOnlyCorrelationAndPreserveResponseWithoutRetry(t *testing.T) {
	for _, status := range []int{200, 400, 500, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })
			calls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				body, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				require.Equal(t, `{"password":"synthetic-private-password"}`, string(body))
				w.Header().Set("Content-Type", "text/plain")
				w.Header().Set("Retry-After", "3")
				w.Header().Set("X-Private", "synthetic-private-header")
				w.WriteHeader(status)
				_, err = w.Write([]byte("synthetic-private-response"))
				require.NoError(t, err)
			}))
			t.Cleanup(upstream.Close)
			handler, err := New(Config{Upstreams: map[string]string{"platform": upstream.URL}, Development: true, Timeout: time.Second})
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPost, "/v1/auth/login/password?private=synthetic-private-query", strings.NewReader(`{"password":"synthetic-private-password"}`))
			req.Header.Set("X-Request-ID", "synthetic-correlation-1338")
			req.Header.Set("Cookie", "helivanta_session=synthetic-private-cookie")
			req.Header.Set("Authorization", "Bearer synthetic-private-token")
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)
			require.Equal(t, status, res.Code)
			require.Equal(t, "synthetic-private-response", res.Body.String())
			require.Equal(t, "text/plain", res.Header().Get("Content-Type"))
			require.Equal(t, "3", res.Header().Get("Retry-After"))
			require.Equal(t, "no-store", res.Header().Get("Cache-Control"))
			require.Equal(t, 1, calls)
			if status < 500 {
				require.Empty(t, logs.String())
				return
			}
			var event map[string]any
			require.NoError(t, json.Unmarshal(logs.Bytes(), &event))
			require.ElementsMatch(t, []string{"time", "level", "msg", "product", "request_id", "status"}, mapKeys(event))
			require.Equal(t, "WARN", event["level"])
			require.Equal(t, "global API upstream response failed", event["msg"])
			require.Equal(t, "platform", event["product"])
			require.Equal(t, "synthetic-correlation-1338", event["request_id"])
			require.Equal(t, float64(status), event["status"])
			require.NotContains(t, logs.String(), "synthetic-private")
			require.NotContains(t, logs.String(), upstream.URL)
		})
	}
}

func mapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

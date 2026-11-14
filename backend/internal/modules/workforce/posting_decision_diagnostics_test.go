package workforce

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestPostingDecisionFailureRetainsStatusAndLogsOnlyBoundedDiagnostics(t *testing.T) {
	for _, test := range []struct {
		name, stage string
		err         error
		status      int
		code        string
	}{
		{"deadline", "supervision_sources", context.DeadlineExceeded, 503, "posting_source_unavailable"},
		{"source", "proposal_basis", authz.ErrScopedAuthorizationUnavailable, 503, "posting_source_unavailable"},
		{"storage", "decision_persistence", errors.New("synthetic storage failure"), 500, "internal"},
		{"denied", "scope_authorization", authz.ErrScopedCommandDenied, 403, "forbidden"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("HELIVANTA_TEST_SLOG_CAPTURED", "1")
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest("PUT", "/synthetic-posting-decision", nil)
			private := "SYNTHETIC-PRIVATE-SOURCE-PROOF"
			respondPostingDecisionError(c, errors.Join(test.err, errors.New(private)), test.stage, 1234*time.Millisecond)
			require.Equal(t, test.status, response.Code)
			require.Contains(t, response.Body.String(), `"error":"`+test.code+`"`)
			require.NotContains(t, response.Body.String(), test.stage)
			require.NotContains(t, response.Body.String(), private)
			require.NotContains(t, logs.String(), private)
			if test.status >= 500 {
				require.Contains(t, logs.String(), "posting_stage="+test.stage)
				require.Contains(t, logs.String(), "elapsed_ms=1234")
			}
		})
	}
}

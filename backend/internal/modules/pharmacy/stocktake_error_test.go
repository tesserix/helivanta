package pharmacy

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestStocktakeEvidenceErrorsAreUnavailable(t *testing.T) {
	for _, table := range []string{"audit_events", "outbox_events"} {
		t.Run(table, func(t *testing.T) {
			r := gin.New()
			r.GET("/", func(c *gin.Context) {
				stocktakeError(c, &pgconn.PgError{Code: "23514", TableName: table, ConstraintName: "synthetic_stocktake_evidence_failure", Detail: "TEST private evidence"})
			})
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
			require.Equal(t, 503, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), `"error":"stocktake_evidence_unavailable"`)
			require.NotContains(t, w.Body.String(), "synthetic_stocktake_evidence_failure")
			require.NotContains(t, w.Body.String(), "TEST private evidence")
		})
	}
}

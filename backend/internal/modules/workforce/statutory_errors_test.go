package workforce

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

// The handler pre-checks shadow these constraints, so their mapping is proven here rather than through the API.
func TestStatutoryConstraintErrorsMapToTheirAnswers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for constraint, status := range map[string]int{
		"workforce_statutory_separation":  403,
		"workforce_statutory_one_pending": 409,
		"workforce_statutory_effective":   409,
	} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		respondStatutoryError(c, fmt.Errorf("decide: %w", &pgconn.PgError{Code: "23514", ConstraintName: constraint}))
		require.Equal(t, status, w.Code, constraint)
	}
}

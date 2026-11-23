package iam

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestHospitalStocktakeDiscoveryDependencyErrorsReleaseNoPrivateDetails(t *testing.T) {
	for _, scenario := range []struct {
		name   string
		err    error
		status int
	}{
		{"revoked credentials", authz.ErrScopedCredentialRevoked, 401},
		{"read deadline", context.DeadlineExceeded, 503},
		{"unavailable owner", authz.ErrScopedAuthorizationUnavailable, 503},
		{"failed disclosure constraint", &pgconn.PgError{Code: "23514", ConstraintName: "TEST-private-audit-constraint", Message: "TEST-private-subject"}, 503},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			router := gin.New()
			router.GET("/", func(c *gin.Context) { hospitalStocktakeScopeError(c, scenario.err) })
			result := httptest.NewRecorder()
			router.ServeHTTP(result, httptest.NewRequest(http.MethodGet, "/", nil))
			require.Equal(t, scenario.status, result.Code)
			require.NotContains(t, result.Body.String(), "TEST-private")
			require.NotContains(t, result.Body.String(), "data")
		})
	}
}

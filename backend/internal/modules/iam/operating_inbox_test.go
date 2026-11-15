package iam

import (
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/helivanta/pkg/authz"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOperatingInboxQueryRejectsTargetFiltersAndMalformedQueries(t *testing.T) {
	for _, query := range []string{"?staff=other", "?status=active", "?status=", "?limit=1&limit=2", "?cursor=", "?limit=%zz", "?limit=20;status=all", "?"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/inbox"+query, nil)
		require.False(t, operatingInboxQuery(c), query)
	}
	for _, query := range []string{"", "?limit=20", "?status=all&cursor=opaque"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/inbox"+query, nil)
		require.True(t, operatingInboxQuery(c), query)
	}
}

func TestOperatingInboxFailuresDoNotDiscloseRecordsOrTurnAuditFailureIntoConflict(t *testing.T) {
	for _, test := range []struct {
		name   string
		err    error
		status int
	}{
		{"source", authz.ErrScopedCommandDenied, 404},
		{"hidden", authz.ErrGovernanceScopeHidden, 404},
		{"wrapped_hidden", errors.Join(errors.New("synthetic_private_record"), authz.ErrGovernanceScopeHidden), 404},
		{"credential", authz.ErrScopedCredentialRevoked, 401},
		{"unavailable", authz.ErrScopedAuthorizationUnavailable, 503},
		{"audit", &pgconn.PgError{Code: "23514", ConstraintName: "synthetic_private_record"}, 503},
		{"unexpected", errors.New("synthetic_private_record"), 503},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(response)
			c.Request = httptest.NewRequest("GET", "/inbox", nil)
			operatingInboxError(c, test.err)
			require.Equal(t, test.status, response.Code)
			require.NotContains(t, response.Body.String(), "synthetic_private_record")
		})
	}
}

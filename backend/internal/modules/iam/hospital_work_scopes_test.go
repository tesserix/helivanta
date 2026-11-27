package iam

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestHospitalWorkDiscoveryDependencyErrorsReleaseNoPrivateDetails(t *testing.T) {
	for _, work := range []hospitalWorkScope{hospitalStocktakeScopes, hospitalWardIndentScopes} {
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
			t.Run(string(work.purpose)+"/"+scenario.name, func(t *testing.T) {
				router := gin.New()
				router.GET("/", func(c *gin.Context) { work.error(c, scenario.err) })
				result := httptest.NewRecorder()
				router.ServeHTTP(result, httptest.NewRequest(http.MethodGet, "/", nil))
				require.Equal(t, scenario.status, result.Code)
				require.NotContains(t, result.Body.String(), "TEST-private")
				require.NotContains(t, result.Body.String(), "data")
				if scenario.status == 503 {
					require.Contains(t, result.Body.String(), "hospital "+work.noun+" access")
				}
			})
		}
	}
}

func TestHospitalWorkScopesAdmitOnlyTheirOwnPurposeTemplates(t *testing.T) {
	for _, work := range []struct {
		scope hospitalWorkScope
		keys  []string
		route string
		audit string
	}{
		{hospitalStocktakeScopes, []string{"pharmacy.department_stocktake_counter.v1", "pharmacy.department_stocktake_reader.v1", "pharmacy.department_stocktake_reviewer.v1"}, "/me/hospital-pharmacy-stocktake-scopes", "iam.hospital_stocktake_scope.read"},
		{hospitalWardIndentScopes, []string{"pharmacy.department_ward_indent_reader.v1", "pharmacy.department_ward_indent_requester.v1"}, "/me/hospital-ward-indent-scopes", "iam.hospital_ward_indent_scope.read"},
	} {
		templates := work.scope.templates()
		keys := make([]string, 0, len(templates))
		for key, template := range templates {
			keys = append(keys, key)
			require.Contains(t, template.Permissions, string(work.scope.purpose.ActorPermission()))
		}
		slices.Sort(keys)
		require.Equal(t, work.keys, keys, "%s lists only its own department templates", work.scope.purpose)
		require.Equal(t, work.route, work.scope.route)
		require.Equal(t, work.audit, work.scope.audit)
	}
	unknown := hospitalWorkScope{purpose: "governance", family: "pharmacy_stocktake"}
	require.Empty(t, unknown.templates(), "an unknown purpose admits no template, even a real family")
	crossed := hospitalWorkScope{purpose: authz.WardIndentWork, family: "pharmacy_stocktake"}
	require.Empty(t, crossed.templates(), "the stocktake family cannot serve ward indents")
}

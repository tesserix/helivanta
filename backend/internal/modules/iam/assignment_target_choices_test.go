package iam

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestAssignmentTargetFailureLogsOnlyPhaseAndSQLState(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(previous)
	response := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(response)
	c.Request = httptest.NewRequest("GET", "/targets", nil)
	c.Set("iam.assignment_target_phase", "workforce_candidates")
	assignmentTargetError(c, &pgconn.PgError{Code: "42501", Message: "PRIVATE-IDENTITY-SECRET", Detail: "PRIVATE-IDENTITY-SECRET"})
	require.Equal(t, 503, response.Code)
	require.Contains(t, output.String(), `"phase":"workforce_candidates"`)
	require.Contains(t, output.String(), `"sqlstate":"42501"`)
	require.NotContains(t, output.String(), "PRIVATE-IDENTITY-SECRET")
	require.NotContains(t, response.Body.String(), "PRIVATE-IDENTITY-SECRET")
}

func TestAssignmentTargetQueryRejectsRawIdentityFilters(t *testing.T) {
	for _, query := range []string{"?subject=other", "?staff=other", "?membership_epoch=1", "?status=active", "?limit=1&limit=2", "?cursor=", "?limit=%zz", "?limit=20;status=all", "?"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/targets"+query, nil)
		require.False(t, assignmentTargetQuery(c), query)
	}
	for _, query := range []string{"", "?limit=20", "?cursor=sealed&limit=1", "?status=eligible&limit=20"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/targets"+query, nil)
		require.True(t, assignmentTargetQuery(c), query)
	}
}

func TestAssignmentTargetErrorKeepsPrivateDependenciesAndAuditFailuresUnavailable(t *testing.T) {
	for _, test := range []struct {
		err    error
		status int
	}{
		{authz.ErrGovernanceScopeHidden, 404},
		{errors.Join(errors.New("synthetic_private_record"), authz.ErrScopedCommandDenied), 404},
		{authz.ErrScopedCredentialRevoked, 401},
		{authz.ErrAssignmentTargetChanged, 409},
		{authz.ErrScopedAuthorizationUnavailable, 503},
		{&pgconn.PgError{Code: "23514", ConstraintName: "synthetic_private_record"}, 503},
	} {
		response := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(response)
		c.Request = httptest.NewRequest("GET", "/targets", nil)
		assignmentTargetError(c, test.err)
		require.Equal(t, test.status, response.Code)
		require.NotContains(t, response.Body.String(), "synthetic_private_record")
	}
}

func TestAssignmentTargetWireDoesNotSerializePrivateOwnerFacts(t *testing.T) {
	staff, hospital := uuid.New(), uuid.New()
	row := assignmentTargetChoiceRow{
		Choice:          AssignmentTargetChoice{HospitalID: hospital, StaffID: staff, Subject: "synthetic-target", Name: "Synthetic Staff", Email: "synthetic@example.invalid"},
		Reference:       &authz.AssignmentTargetReference{StaffID: staff, GenerationID: uuid.Must(uuid.NewV7()), EmploymentEpoch: 9},
		MembershipEpoch: 13,
		CreatedAt:       time.Now(),
	}
	data, err := json.Marshal(row)
	require.NoError(t, err)
	require.JSONEq(t, `{"hospital_id":"`+hospital.String()+`","staff_id":"`+staff.String()+`","subject":"synthetic-target","name":"Synthetic Staff","email":"synthetic@example.invalid"}`, string(data))
}

func TestAssignmentTargetChoicesRejectUnauthenticatedRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerGovernanceAssignments(platform.NewRouter(router.Group("/v1/iam"), &CurrentAuthorization{}), platform.Deps{}, nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/v1/iam/hospitals/11111111-1111-4111-8111-111111111111/assignment-target-choices", nil))
	require.Equal(t, 401, response.Code)
}

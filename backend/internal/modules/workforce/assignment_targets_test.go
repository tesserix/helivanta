package workforce

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	gormschema "gorm.io/gorm/schema"
)

func TestAssignmentTargetOwnerFailureReportsOnlyFixedDiagnosticFields(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(previous)
	reader := New().AssignmentTargets(nil)
	_, err := reader.ReadAssignmentTargetCandidates(t.Context(), authz.GovernedAssignmentSelectionRequest{}, pagination.Params{Limit: 20})
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	require.Contains(t, output.String(), `"phase":"scope"`)
	require.Contains(t, output.String(), `"authorization_unavailable":true`)
	require.NotContains(t, output.String(), `"err"`)
}

func TestAssignmentTargetScanMapsOnlyCanonicalDatabaseColumns(t *testing.T) {
	mapping, err := gormschema.Parse(&[]assignmentTargetScanRow{}, &sync.Map{}, gormschema.NamingStrategy{})
	require.NoError(t, err, "GORM must parse the actual bounded Staff scan destination without treating qualification evidence as a relation")
	require.Len(t, mapping.Fields, 2)
	require.NotNil(t, mapping.LookUpField("staff_id"))
	require.NotNil(t, mapping.LookUpField("created_at"))
	require.Empty(t, mapping.Relationships.Relations)
}

func TestAssignmentTargetOwnerCannotReadWithoutItsDatabaseAndHeldSource(t *testing.T) {
	reader := New().AssignmentTargets(nil)
	rows, err := reader.ReadAssignmentTargetCandidates(t.Context(), authz.GovernedAssignmentSelectionRequest{}, pagination.Params{Limit: 20})
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	require.Empty(t, rows)
	err = reader.RecheckAssignmentTargets(t.Context(), authz.GovernedAssignmentSelectionRequest{}, nil)
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
}

func TestAssignmentTargetPurposeKeepsQualificationReadOnlyAndRejectsUnrelatedWorkforceData(t *testing.T) {
	for _, clause := range []string{
		"workforce_assignment_selection_context",
		"'assignment_target_choices'",
		"'iam.governance.assignment.manage'",
		"'iam.scoped_access_reviewer.v1'",
		"'app.actor_subject'",
		"'app.workforce_subject'",
		"'read_expires_at'",
		"'app.workforce_assignment_target_staff'",
		"'app.workforce_assignment_target_generation'",
		"'workforce_employment_generations'",
		"'workforce_employment_review_evidence'",
		"'workforce_person_review_evidence'",
		"'workforce.assignment_target.qualification.read'",
		"assignment_selection_purpose",
		"assignment_selection_delete",
		"workforce_assignment_selection_integrity",
	} {
		require.Contains(t, assignmentTargetsSchema, clause)
	}
	require.Contains(t, assignmentTargetsSchema, "workforce_operating_selection_integrity()IS DISTINCT FROM true")
	require.Contains(t, assignmentTargetsSchema, "ELSE RETURN false;")
	require.NotContains(t, strings.ToUpper(assignmentTargetsSchema), "SECURITY DEFINER")
	require.NotContains(t, strings.ToUpper(assignmentTargetsSchema), "DISABLE ROW LEVEL SECURITY")
}

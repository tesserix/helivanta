package organization_test

import (
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/organization" //nolint:depguard // External test verifies the Organization-owned narrow name boundary.
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"testing"
)

func TestHospitalWorkDepartmentNamesCannotBypassHeldReadAndSource(t *testing.T) {
	reader, ok := organization.New().DepartmentReferences(nil).(authz.HospitalWorkDepartmentNameReader)
	require.True(t, ok, "Organization owns the narrow department display-name projection")
	for _, purpose := range []authz.HospitalWorkPurpose{authz.StocktakeWork, authz.WardIndentWork, "unknown"} {
		name, err := reader.HospitalWorkDepartmentName(t.Context(), purpose, authz.StaffActorScope{})
		require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
		require.Empty(t, name)
	}
}

func TestStocktakeDepartmentChoicesCannotBypassHeldReadAndSource(t *testing.T) {
	reader, ok := organization.New().DepartmentReferences(nil).(authz.StocktakeDepartmentReader)
	require.True(t, ok)
	rows, err := reader.StocktakeDepartments(t.Context(), authz.StaffActorScope{}, pagination.Params{Limit: 20})
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	require.Empty(t, rows)
}

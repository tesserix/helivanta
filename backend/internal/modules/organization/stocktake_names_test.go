package organization_test

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/organization" //nolint:depguard // External test verifies the Organization-owned narrow name boundary.
	"github.com/tesserix/helivanta/pkg/authz"
	"testing"
)

func TestStocktakeDepartmentNamesCannotBypassHeldReadAndSource(t *testing.T) {
	reader, ok := organization.New().DepartmentReferences(nil).(interface {
		StocktakeDepartmentName(context.Context, authz.StaffActorScope) (string, error)
	})
	require.True(t, ok, "Organization owns the narrow department display-name projection")
	name, err := reader.StocktakeDepartmentName(t.Context(), authz.StaffActorScope{})
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	require.Empty(t, name)
}

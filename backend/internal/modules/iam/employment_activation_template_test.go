package iam_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // Exercises this module's public HTTP and migration contracts.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

func TestEmploymentActivationAuthorityHasSeparateClosedCatalog(t *testing.T) {
	router, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {iam.PermAssignmentManage}},
		Modules: []platform.Module{iam.New(nil)},
	})
	response := testutil.Do(router, http.MethodGet, "/v1/iam/staff-access-templates?catalog=employment-activation-v1", "admin", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var templates []iam.TemplateDefinition
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &templates))
	require.Len(t, templates, 6)
	activation := templates[5]
	require.Equal(t, "workforce.employment_activator.v1", activation.Key)
	require.Equal(t, "activation", activation.Family)
	require.Equal(t, []string{"workforce.employment.activate"}, activation.Permissions)
	for catalog, count := range map[string]int{"legacy-v1": 3, "hr-intake-v1": 4, "employment-v1": 5} {
		response := testutil.Do(router, http.MethodGet, "/v1/iam/staff-access-templates?catalog="+catalog, "admin", "")
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var prior []iam.TemplateDefinition
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &prior))
		require.Len(t, prior, count)
		for _, template := range prior {
			require.NotContains(t, template.Permissions, "workforce.employment.activate")
		}
	}
	var permissions []string
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var encoded string
		if err := tx.Raw("SELECT iam_assignment_template_permissions(?)::text", activation.Key).Scan(&encoded).Error; err != nil {
			return err
		}
		return json.Unmarshal([]byte(encoded), &permissions)
	}))
	require.Equal(t, activation.Permissions, permissions)
}

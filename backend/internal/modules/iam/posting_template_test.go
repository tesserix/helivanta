package iam_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // External test exercises this module's public catalog and migration.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

func TestPostingCatalogSeparatesReaderPreparerAndCurrentStaffSource(t *testing.T) {
	router, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"admin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"admin": {iam.PermAssignmentManage}},
		Modules: []platform.Module{iam.New(nil)},
	})
	response := testutil.Do(router, http.MethodGet, "/v1/iam/staff-access-templates?catalog=staff-postings-v1", "admin", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var templates []iam.TemplateDefinition
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &templates))
	require.Len(t, templates, 8)
	for i, expected := range []struct {
		key         string
		permissions []string
	}{
		{"workforce.posting_reader.v1", []string{"workforce.posting.read", "workforce.staff_actor.use"}},
		{"workforce.posting_preparer.v1", []string{"workforce.posting.read", "workforce.posting.prepare", "organization.posting_choices.read", "workforce.staff_actor.use"}},
	} {
		actual := templates[6+i]
		require.Equal(t, expected.key, actual.Key)
		require.Equal(t, "posting", actual.Family)
		require.Equal(t, expected.permissions, actual.Permissions)
		var encoded string
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT iam_assignment_template_permissions(?)::text`, expected.key).Scan(&encoded).Error
		}))
		var installed []string
		require.NoError(t, json.Unmarshal([]byte(encoded), &installed))
		require.Equal(t, actual.Permissions, installed)
	}
	for catalog, count := range map[string]int{"legacy-v1": 3, "hr-intake-v1": 4, "employment-v1": 5, "employment-activation-v1": 6} {
		response := testutil.Do(router, http.MethodGet, "/v1/iam/staff-access-templates?catalog="+catalog, "admin", "")
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var prior []iam.TemplateDefinition
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &prior))
		require.Len(t, prior, count)
		for _, definition := range prior {
			require.NotEqual(t, "posting", definition.Family)
			require.NotContains(t, definition.Permissions, "workforce.staff_actor.use")
		}
	}
}

func TestPostingTemplateScope(t *testing.T) {
	for _, template := range []struct {
		key        string
		permission authz.Permission
	}{
		{"workforce.posting_reader.v1", "workforce.posting.read"},
		{"workforce.posting_preparer.v1", "workforce.posting.prepare"},
	} {
		t.Run(template.key, func(t *testing.T) { exerciseAssignmentProjection(t, template.key, template.permission) })
	}
}

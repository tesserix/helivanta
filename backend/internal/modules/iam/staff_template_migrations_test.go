package iam_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // External test exercises its own module.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestAppendOnlyTemplateSQLPreservesPublishedDefinitions(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {iam.PermAssignmentManage}}, Modules: []platform.Module{iam.New(nil)}})
	published := testutil.Do(r, http.MethodGet, "/v1/iam/staff-access-templates", "admin", "")
	require.Equal(t, http.StatusOK, published.Code, published.Body.String())
	var definitions []iam.TemplateDefinition
	require.NoError(t, json.Unmarshal(published.Body.Bytes(), &definitions))
	require.Len(t, definitions, 3, "legacy consumers retain their closed v1 catalog")
	intakeCatalog := testutil.Do(r, http.MethodGet, "/v1/iam/staff-access-templates?catalog=hr-intake-v1", "admin", "")
	require.Equal(t, http.StatusOK, intakeCatalog.Code, intakeCatalog.Body.String())
	var intakeDefinitions []iam.TemplateDefinition
	require.NoError(t, json.Unmarshal(intakeCatalog.Body.Bytes(), &intakeDefinitions))
	require.Len(t, intakeDefinitions, 4)
	require.Equal(t, "workforce.hr_intake.v1", intakeDefinitions[3].Key)
	for _, query := range []string{"catalog=unknown", "catalog=hr-intake-v1&catalog=legacy-v1"} {
		invalid := testutil.Do(r, http.MethodGet, "/v1/iam/staff-access-templates?"+query, "admin", "")
		require.Equal(t, http.StatusBadRequest, invalid.Code)
	}
	read := func(key string) *string {
		t.Helper()
		var result struct{ Permissions *string }
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT iam_assignment_template_permissions(?)::text AS permissions`, key).Scan(&result).Error
		}))
		return result.Permissions
	}
	const next = "workforce.leave_reviewer.v2"
	require.Nil(t, read(next))
	baseline := map[string]string{}
	for _, definition := range definitions {
		value := read(definition.Key)
		require.NotNil(t, value)
		var permissions []string
		require.NoError(t, json.Unmarshal([]byte(*value), &permissions))
		require.Equal(t, definition.Permissions, permissions)
		baseline[definition.Key] = *value
	}
	sql, err := os.ReadFile("testdata/staff_template_append.sql")
	require.NoError(t, err)
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_iam_template_append", SQL: string(sql)}}))
	for key, expected := range baseline {
		value := read(key)
		require.NotNil(t, value)
		require.JSONEq(t, expected, *value)
	}
	value := read(next)
	require.NotNil(t, value)
	require.JSONEq(t, `["workforce.leave.approve"]`, *value)
	require.Nil(t, read("workforce.unknown.v1"))
	after := testutil.Do(r, http.MethodGet, "/v1/iam/staff-access-templates", "admin", "")
	require.Equal(t, http.StatusOK, after.Code, after.Body.String())
	require.JSONEq(t, published.Body.String(), after.Body.String(), "test-only SQL does not publish an unimplemented application template")
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'user-admin','tenant_admin'),(?,'synthetic-template-target','staff_member')`, testutil.TenantA, testutil.TenantA).Error
	}))
	body := fmt.Sprintf(`{"hospital_id":%q,"subject":"synthetic-template-target","template_key":%q,"reason":"Synthetic SQL must not activate an undeclared application workflow","valid_from":%q,"expires_at":%q}`, uuid.NewString(), next, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Add(time.Hour).Format(time.RFC3339))
	refused := testutil.Do(r, http.MethodPut, "/v1/iam/assignment-proposals/"+uuid.Must(uuid.NewV7()).String(), "admin", body)
	require.Equal(t, http.StatusBadRequest, refused.Code, refused.Body.String())
}

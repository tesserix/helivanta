package iam_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // External test exercises its own append-only template migration.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type iamBeforeMigration struct {
	*iam.Module
	id string
}

func (m iamBeforeMigration) Migrations() []tenantdb.Migration {
	var prior []tenantdb.Migration
	for _, migration := range m.Module.Migrations() {
		if migration.ID == m.id {
			break
		}
		prior = append(prior, migration)
	}
	return prior
}

func TestHospitalWorkTemplateMigrationsPreserveAuthority(t *testing.T) {
	for _, work := range []struct{ id, role string }{
		{"0056_iam_pharmacy_stocktake_templates", "pharmacy.department_stocktake_counter.v1"},
		{"0057_iam_pharmacy_ward_indent_templates", "pharmacy.department_ward_indent_requester.v1"},
	} {
		t.Run(work.id, func(t *testing.T) { assertHospitalWorkTemplateMigration(t, work.id, work.role) })
	}
}

func assertHospitalWorkTemplateMigration(t *testing.T, id, role string) {
	for _, scenario := range []struct{ name, tamper string }{
		{"unchanged authority", ""},
		{"changed template source", `CREATE OR REPLACE FUNCTION iam_assignment_template_scope_kinds(key text)RETURNS text[] LANGUAGE sql IMMUTABLE AS $$SELECT ARRAY['hospital']::text[]$$;`},
		{"changed unrelated proposal guard", `CREATE OR REPLACE FUNCTION iam_assignment_proposal_guard()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RETURN NEW;END$$;`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			module := iam.New(nil)
			_, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{iamBeforeMigration{module, id}}})
			read := func(query string, output any, args ...any) {
				t.Helper()
				require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return tx.Raw(query, args...).Scan(output).Error }))
			}
			var original, after struct{ Assignment, Operating, Selection string }
			definitions := `SELECT pg_get_functiondef('iam_assignment_governance_integrity()'::regprocedure) assignment,pg_get_functiondef('iam_operating_governance_integrity()'::regprocedure) operating,pg_get_functiondef('iam_staff_selection_integrity()'::regprocedure) selection`
			read(definitions, &original)
			var before struct{ Assignment, Operating, Selection bool }
			read(`SELECT iam_assignment_governance_integrity() assignment,iam_operating_governance_integrity() operating,iam_staff_selection_integrity() selection`, &before)
			require.True(t, before.Assignment)
			require.True(t, before.Operating)
			require.True(t, before.Selection)
			type fingerprint struct{ Name, Digest string }
			var fingerprints []fingerprint
			read(`SELECT name,md5(pg_get_functiondef(name::regprocedure)) digest FROM unnest(ARRAY['iam_assignment_template_permissions(text)','iam_assignment_template_scope_kinds(text)','iam_assignment_template_establishment_kinds(text)'])name ORDER BY name`, &fingerprints)
			require.Len(t, fingerprints, 3)
			var migration tenantdb.Migration
			for _, current := range module.Migrations() {
				if current.ID == id {
					migration = current
				}
			}
			require.NotEmpty(t, migration.SQL)
			if scenario.tamper != "" {
				require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_" + id + "_prior_tamper", SQL: scenario.tamper}}))
				err := db.Migrate(ctx, []tenantdb.Migration{migration})
				require.ErrorContains(t, err, "require unchanged prior IAM authority")
				var newRole struct{ Permissions *string }
				read(`SELECT iam_assignment_template_permissions(?)::text permissions`, &newRole, role)
				require.Nil(t, newRole.Permissions, "a refused append must not leave any new definition")
				read(definitions, &after)
				require.Equal(t, original, after, "refusal must preserve the original expected fingerprints")
				return
			}
			require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{migration}))
			var current []fingerprint
			read(`SELECT name,md5(pg_get_functiondef(name::regprocedure)) digest FROM unnest(ARRAY['iam_assignment_template_permissions(text)','iam_assignment_template_scope_kinds(text)','iam_assignment_template_establishment_kinds(text)'])name ORDER BY name`, &current)
			require.Len(t, current, 3)
			expected := original.Assignment
			for i, prior := range fingerprints {
				require.Equal(t, prior.Name, current[i].Name)
				require.NotEqual(t, prior.Digest, current[i].Digest)
				require.Equal(t, 1, strings.Count(expected, "'"+prior.Digest+"'"))
				expected = strings.ReplaceAll(expected, "'"+prior.Digest+"'", "'"+current[i].Digest+"'")
			}
			read(definitions, &after)
			require.Equal(t, expected, after.Assignment, "only the three new template fingerprints may change")
			require.Equal(t, original.Operating, after.Operating)
			require.Equal(t, original.Selection, after.Selection)
			read(`SELECT iam_assignment_governance_integrity() assignment,iam_operating_governance_integrity() operating,iam_staff_selection_integrity() selection`, &before)
			require.True(t, before.Assignment)
			require.True(t, before.Operating)
			require.True(t, before.Selection)
			var added struct{ Permissions *string }
			read(`SELECT iam_assignment_template_permissions(?)::text permissions`, &added, role)
			require.NotNil(t, added.Permissions, "the migration defines its own new role")
		})
	}
}

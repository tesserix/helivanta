package drugmaster_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/drugmaster" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// firstMigrationOnly is the drug master as 0001_drugmaster shipped it.
type firstMigrationOnly struct{ *drugmaster.Module }

func (m firstMigrationOnly) Migrations() []tenantdb.Migration { return m.Module.Migrations()[:1] }

// 0002_drugmaster gives ingredients already published their release and
// keeps them readable: a catalogue published before the change survives it.
func TestReadPoliciesMigrationKeepsPublishedIngredients(t *testing.T) {
	r, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"doctor": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"doctor": {drugmaster.PermCatalogueRead}},
		Modules: []platform.Module{firstMigrationOnly{drugmaster.New()}},
	})
	var name string
	require.NoError(t, db.WithSystem(t.Context(), func(tx *gorm.DB) error { return tx.Raw(`SELECT current_database()`).Scan(&name).Error }))
	stager, err := tenantdb.OpenRuntime(t.Context(), testinfra.DrugStagerDSN(t, name))
	require.NoError(t, err)
	approver, err := tenantdb.OpenRuntime(t.Context(), testinfra.DrugApproverDSN(t, name))
	require.NoError(t, err)
	t.Cleanup(func() { _ = stager.Close(); _ = approver.Close() })

	// Staged by hand in the 0001 shape (no release_id on ingredients).
	file := raw(t, amoxicillin("v1", "2020-01-01", "H", "active"))
	release := uuid.Must(uuid.NewV7())
	molecule, formulation, version := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	require.NoError(t, stager.WithSystem(t.Context(), func(tx *gorm.DB) error {
		for _, q := range []struct {
			sql  string
			args []any
		}{
			{`INSERT INTO drugmaster_releases (id,country,source_name,licence_ref,source_version,file_sha256,staged_by) VALUES (?,'IN','synthetic-test-fixture','TEST-LICENCE','v1',encode(sha256(?::bytea),'hex'),'Asha Stager')`, []any{release, string(file)}},
			{`INSERT INTO drugmaster_molecules (id,release_id,inn_name,atc_code) VALUES (?,?,'Amoxicillin','J01CA04')`, []any{molecule, release}},
			{`INSERT INTO drugmaster_formulations (id,release_id,code) VALUES (?,?,'TEST-AMOX-500-CAP')`, []any{formulation, release}},
			{`INSERT INTO drugmaster_formulation_versions (id,formulation_id,release_id,valid_from,label,dose_form,route,regulatory_class,ndps,teleconsult_prescribable) VALUES (?,?,?,'2020-01-01','Amoxicillin 500 mg capsule','capsule','oral','H',false,true)`, []any{version, formulation, release}},
			{`INSERT INTO drugmaster_formulation_ingredients (version_id,position,molecule_id,strength_value,strength_unit) VALUES (?,1,?,500,'mg')`, []any{version, molecule}},
		} {
			if err := tx.Exec(q.sql, q.args...).Error; err != nil {
				return err
			}
		}
		return nil
	}))
	require.NoError(t, approver.WithSystem(t.Context(), func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE drugmaster_releases SET status='published',decided_by='Ravi Approver',decision_reason='before 0002' WHERE id=?`, release).Error
	}))

	require.NoError(t, db.Migrate(t.Context(), drugmaster.New().Migrations()[1:]))

	var ingredientRelease string
	require.NoError(t, stager.WithSystem(t.Context(), func(tx *gorm.DB) error {
		return tx.Raw(`SELECT release_id::text FROM drugmaster_formulation_ingredients WHERE version_id=?`, version).Scan(&ingredientRelease).Error
	}))
	require.Equal(t, release.String(), ingredientRelease)
	res := testutil.Do(r, http.MethodGet, "/v1/drugmaster/formulations/"+formulation.String(), "doctor", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), `"inn_name":"Amoxicillin"`)
	var enabled string
	require.NoError(t, db.WithSystem(t.Context(), func(tx *gorm.DB) error {
		return tx.Raw(`SELECT tgenabled::text FROM pg_trigger WHERE tgname='drugmaster_formulation_ingredients_immutable'`).Scan(&enabled).Error
	}))
	require.Equal(t, "O", enabled, "the immutability trigger is back on after the backfill")
}

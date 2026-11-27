package iam_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // External test exercises its own append-only migration.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
)

// 0050_iam_operating_governance's guard loop ran over the tables that existed
// then; iam_security_keys (0057) is created later, so it must attach the
// guard itself and re-pin the operating-governance fingerprint to count it.
func TestSecurityKeyRegistryIsGuarded(t *testing.T) {
	_, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{iam.New(nil)}})

	var guards int64
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM pg_trigger WHERE tgrelid='iam_security_keys'::regclass AND tgname='iam_operating_native_global_mutation_guard' AND NOT tgisinternal AND tgenabled IN('O','A')`).Scan(&guards).Error
	}))
	require.Equal(t, int64(1), guards)

	var intact struct{ Assignment, Operating, Selection, Support bool }
	integrity := `SELECT iam_assignment_governance_integrity() assignment,iam_operating_governance_integrity() operating,iam_staff_selection_integrity() selection,iam_operating_governance_support() support`
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error { return tx.Raw(integrity).Scan(&intact).Error }))
	require.Equal(t, struct{ Assignment, Operating, Selection, Support bool }{true, true, true, true}, intact,
		"adding the guard must leave every governance fingerprint intact")

	// The guard's effect, not just its presence: a native duty source may not
	// write a security key.
	err := db.WithSystem(ctx, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.iam_operating_native_authority','test-source',true)`).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO iam_security_keys(subject,key_id,rp_id) VALUES('user-a','key-1','helivanta.test')`).Error
	})
	require.ErrorContains(t, err, "native duty source cannot mutate global identity")
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_security_keys(subject,key_id,rp_id) VALUES('user-a','key-1','helivanta.test')`).Error
	}), "without that authority the sign-in path still writes")

	// The re-pinned fingerprint counts the new trigger: removing it is detected.
	require.NoError(t, db.WithAdmin(ctx, func(tx *gorm.DB) error {
		return tx.Exec(`DROP TRIGGER iam_operating_native_global_mutation_guard ON iam_security_keys`).Error
	}))
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error { return tx.Raw(integrity).Scan(&intact).Error }))
	require.False(t, intact.Operating, "operating governance integrity must notice the guard's removal")
}

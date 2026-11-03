package workforce_test

import (
	"fmt"
	"testing"

	"gorm.io/gorm"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
)

func TestStaffPostingDraftFailsClosedWithoutCanonicalDepartmentOwner(t *testing.T) {
	h := newRequestHarness(t, func(opts *testutil.HarnessOptions) {
		opts.Perms["admin"] = append(opts.Perms["admin"], "workforce.posting.prepare")
	})
	staff := h.staff("SYN-POSTING-OWNER-OUTAGE")
	path := "/staff/" + staff + "/posting-drafts/" + uuid.Must(uuid.NewV7()).String() + "/commands/" + uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"expected_version":0,"staff_version":1,"employment_epoch":1,"department_id":%q,"department_revision_id":%q,"starts_at":"2027-01-01T00:00:00Z","reason":"Synthetic posting preparation"}`, uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String())
	code, out := h.do("PUT", h.base+path, "admin", body)
	require.Equal(t, 503, code, out)
	require.Contains(t, out, `"error":"posting_source_unavailable"`)
	code, _ = h.do("GET", h.base+"/staff/"+staff+"/posting-drafts", "admin", "")
	require.Equal(t, 503, code, "unavailable source must not fabricate an empty current collection")
}

func TestScopedPostingRLSKeepsProfileWritesAndOtherDraftTargetsClosed(t *testing.T) {
	h := newRequestHarness(t)
	target, other := h.staff("SYN-POSTING-RLS-A"), h.staff("SYN-POSTING-RLS-B")
	scope := func(tx *gorm.DB, tenant string, complete bool) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject','synthetic-scoped-posting-actor',true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_posting_staff',?,true),set_config('app.workforce_posting','write',true),set_config('app.workforce_posting_scoped','write',true)`, h.hospital, target).Error; err != nil {
			return err
		}
		if !complete {
			return nil
		}
		return tx.Exec(`SELECT set_config('app.workforce_posting_actor',?,true),set_config('app.workforce_posting_link',?,true),set_config('app.workforce_posting_generation',?,true),set_config('app.workforce_posting_assignment',?,true),set_config('app.workforce_posting_assignment_version','1',true),set_config('app.workforce_posting_scope_epoch','1',true),set_config('app.workforce_posting_membership_epoch','1',true)`, target, uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()).Error
	}
	for _, scenario := range []struct {
		name, tenant string
		complete     bool
		count        int64
	}{{"missing proof", testutil.TenantA, false, 0}, {"exact target", testutil.TenantA, true, 1}, {"foreign tenant", testutil.TenantB, true, 0}} {
		t.Run(scenario.name, func(t *testing.T) {
			require.NoError(t, h.db.WithTenant(t.Context(), scenario.tenant, func(tx *gorm.DB) error {
				require.NoError(t, scope(tx, scenario.tenant, scenario.complete))
				var count int64
				require.NoError(t, tx.Table("workforce_staff").Count(&count).Error)
				require.Equal(t, scenario.count, count)
				return nil
			}))
		})
	}
	require.NoError(t, h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		require.NoError(t, scope(tx, testutil.TenantA, true))
		require.NoError(t, tx.Exec(`SELECT set_config('app.workforce_posting_directory','read',true)`).Error)
		var count int64
		require.NoError(t, tx.Table("workforce_staff").Count(&count).Error)
		require.EqualValues(t, 2, count)
		var allowed bool
		require.NoError(t, tx.Raw(`SELECT workforce_posting_draft_context(?::uuid,?::uuid,?::uuid,true)`, testutil.TenantA, h.hospital, other).Scan(&allowed).Error)
		require.False(t, allowed, "directory purpose cannot broaden draft targets")
		return nil
	}))
	err := h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		require.NoError(t, scope(tx, testutil.TenantA, true))
		return tx.Exec(`UPDATE workforce_staff SET display_name='Synthetic unauthorized change' WHERE id=?::uuid`, target).Error
	})
	require.Error(t, err, "scoped source row locking must not grant Staff UPDATE authority")
	err = h.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		require.NoError(t, scope(tx, testutil.TenantA, true))
		return tx.Exec(`DELETE FROM workforce_staff WHERE id=?::uuid`, target).Error
	})
	require.ErrorContains(t, err, "destructive workforce operation is not supported", "the existing immutable-Staff delete guard remains enforced")

}

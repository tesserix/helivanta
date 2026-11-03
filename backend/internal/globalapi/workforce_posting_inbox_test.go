package globalapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func assertPostingInboxProtection(t *testing.T, db *tenantdb.DB, tenant, hospital, staff, proposal, reviewer, administrator, path string, call func(*testing.T, string, string, string, any, int) map[string]any, unavailable *atomic.Bool) {
	t.Helper()
	var audit struct {
		Changes  string
		Resource uuid.UUID
	}
	require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
		return tx.Table("workforce_audit").Select("changes,resource_id AS resource").Where("actor_subject=? AND action=?", reviewer, "workforce.posting_proposal.inbox_read").Order("created_at DESC,id DESC").Take(&audit).Error
	}))
	require.Equal(t, uuid.MustParse(hospital), audit.Resource)
	var proof map[string]any
	require.NoError(t, json.Unmarshal([]byte(audit.Changes), &proof))
	require.Equal(t, "pending", proof["status"])
	require.EqualValues(t, 1, proof["rows"])
	withRead := func(currentTenant string, action func(*gorm.DB) error) error {
		return db.WithTenant(t.Context(), currentTenant, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.workforce_subject',?,true),set_config('app.workforce_hospital',?,true),
 set_config('app.workforce_posting_staff',?,true),set_config('app.workforce_posting_actor',?,true),
 set_config('app.workforce_posting_link',?,true),set_config('app.workforce_posting_generation',?,true),
 set_config('app.workforce_posting_assignment',?,true),set_config('app.workforce_posting_assignment_version',?,true),
 set_config('app.workforce_posting_scope_epoch',?,true),set_config('app.workforce_posting_membership_epoch',?,true),
 set_config('app.workforce_posting','read',true),set_config('app.workforce_posting_scoped','read',true),
 set_config('app.workforce_posting_review_mode','read',true),set_config('app.workforce_posting_permission','workforce.posting.proposal.read',true),
 set_config('app.workforce_posting_inbox','read',true),set_config('app.workforce_posting_directory','read',true)`,
				reviewer, hospital, proof["actor_staff_id"], proof["actor_staff_id"], proof["actor_link_id"], proof["actor_generation_id"], proof["assignment_id"], fmt.Sprint(proof["assignment_version"]), fmt.Sprint(proof["scope_epoch"]), fmt.Sprint(proof["membership_epoch"])).Error; err != nil {
				return err
			}
			return action(tx)
		})
	}
	require.NoError(t, withRead(tenant, func(tx *gorm.DB) error {
		var index string
		require.NoError(t, tx.Raw(`SELECT indexdef FROM pg_indexes WHERE schemaname='public' AND tablename='workforce_posting_proposals' AND indexname='workforce_posting_proposal_inbox'`).Row().Scan(&index))
		require.Contains(t, index, "(tenant_id, hospital_id, status, created_at DESC, id DESC)")
		var plan string
		err := tx.Raw(`EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)
SELECT p.id,r.id AS revision_id,r.command_id,p.hospital_id,p.staff_id,r.version,r.status,
r.draft_id,r.draft_revision_id,r.draft_version,r.generation_id,r.staff_version,r.employment_epoch,
r.department_id,r.department_revision_id,r.starts_at,r.ends_at,r.eligibility_until,p.created_at,
r.created_at AS recorded_at,staff.employee_code,staff.display_name,(submitted.actor_subject=?) AS is_submitter
FROM workforce_posting_proposals p
JOIN workforce_posting_proposal_revisions r ON (r.tenant_id,r.hospital_id,r.staff_id,r.proposal_id,r.id,r.version)=(p.tenant_id,p.hospital_id,p.staff_id,p.id,p.revision_id,p.version)
JOIN workforce_staff staff ON (staff.tenant_id,staff.hospital_id,staff.id)=(p.tenant_id,p.hospital_id,p.staff_id)
JOIN workforce_posting_proposal_revisions submitted ON (submitted.tenant_id,submitted.hospital_id,submitted.staff_id,submitted.proposal_id)=(p.tenant_id,p.hospital_id,p.staff_id,p.id) AND submitted.version=1
WHERE p.tenant_id=? AND p.hospital_id=? AND p.status='pending'
ORDER BY p.created_at DESC,p.id DESC LIMIT 9`, reviewer, tenant, hospital).Row().Scan(&plan)
		require.NoError(t, err)
		var execution []struct {
			Plan struct {
				NodeType   string `json:"Node Type"`
				ActualRows int    `json:"Actual Rows"`
			} `json:"Plan"`
		}
		require.NoError(t, json.Unmarshal([]byte(plan), &execution))
		require.Len(t, execution, 1)
		require.Equal(t, "Limit", execution[0].Plan.NodeType)
		require.Positive(t, execution[0].Plan.ActualRows)
		require.LessOrEqual(t, execution[0].Plan.ActualRows, 9)
		t.Logf("posting inbox representative full-projection first-page plan on small synthetic fixture: %s", plan)
		return nil
	}))
	require.NoError(t, withRead(tenant, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("workforce_posting_proposals").Count(&count).Error)
		require.Positive(t, count, "read proof must expose real public-workflow proposal heads")
		result := tx.Table("workforce_posting_proposals").Where("tenant_id=? AND hospital_id=? AND staff_id=?", tenant, hospital, staff).Update("status", "withdrawn")
		require.NoError(t, result.Error)
		require.Zero(t, result.RowsAffected, "inbox SELECT purpose cannot update proposal heads")
		var current struct{ Status string }
		require.NoError(t, tx.Table("workforce_posting_proposals").Select("status").Where("tenant_id=? AND id=?", tenant, proposal).Take(&current).Error)
		require.Equal(t, "pending", current.Status, "the visible proposal must retain its actual business state")
		return nil
	}))
	require.ErrorContains(t, withRead(tenant, func(tx *gorm.DB) error {
		return tx.Table("workforce_posting_proposal_revisions").Where("tenant_id=? AND hospital_id=? AND staff_id=?", tenant, hospital, staff).Update("status", "withdrawn").Error
	}), "destructive workforce operation is not supported", "inbox purpose cannot bypass immutable revision protection")
	for _, table := range []string{"workforce_posting_proposals", "workforce_posting_proposal_revisions"} {
		require.ErrorContains(t, withRead(tenant, func(tx *gorm.DB) error {
			return tx.Table(table).Where("tenant_id=? AND hospital_id=? AND staff_id=?", tenant, hospital, staff).Delete(nil).Error
		}), "destructive workforce operation is not supported", "inbox purpose cannot bypass the deletion guard for %s", table)
	}
	require.Error(t, withRead(tenant, func(tx *gorm.DB) error {
		return tx.Exec(`UPDATE workforce_staff SET display_name='Synthetic prohibited inbox edit' WHERE tenant_id=? AND hospital_id=? AND id=?`, tenant, hospital, staff).Error
	}), "directory label visibility must not confer Staff profile editing")
	for _, probe := range []struct{ tenant, setting, value string }{
		{tenant, "app.workforce_posting_assignment", ""},
		{tenant, "app.workforce_posting_inbox", ""},
		{tenant, "app.workforce_posting_actor", uuid.Must(uuid.NewV7()).String()},
		{testutil.TenantB, "", ""},
	} {
		require.NoError(t, withRead(probe.tenant, func(tx *gorm.DB) error {
			if probe.setting != "" {
				require.NoError(t, tx.Exec(`SELECT set_config(?,?,true)`, probe.setting, probe.value).Error)
			}
			var count int64
			require.NoError(t, tx.Table("workforce_posting_proposals").Count(&count).Error)
			require.Zero(t, count, "missing proof, changed actor and foreign tenants cannot borrow the inbox purpose")
			return nil
		}))
	}
	uses := func() int64 {
		var count int64
		require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
			return tx.Table("workforce_audit").Where("actor_subject=? AND action=?", reviewer, "workforce.staff_actor.use").Count(&count).Error
		}))
		return count
	}
	apply := func(sql string) error {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 15*time.Second)
		defer cancel()
		return db.Migrate(ctx, []tenantdb.Migration{{ID: "test_posting_inbox_fault_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}})
	}
	clear := func() {
		if err := apply("ALTER TABLE workforce_audit DROP CONSTRAINT IF EXISTS synthetic_posting_inbox_audit"); err != nil {
			t.Errorf("remove inbox audit fault: %v", err)
		}
	}
	t.Cleanup(clear)
	before := uses()
	require.Positive(t, before)
	require.NoError(t, apply("ALTER TABLE workforce_audit ADD CONSTRAINT synthetic_posting_inbox_audit CHECK(action<>'workforce.posting_proposal.inbox_read') NOT VALID"))
	failure := call(t, "GET", path, reviewer, nil, 500)
	require.NotContains(t, failure, "data")
	require.Equal(t, before, uses(), "failed page audit rolls actor-use evidence back")
	clear()
	func() {
		unavailable.Store(true)
		defer unavailable.Store(false)
		require.NotContains(t, call(t, "GET", path, reviewer, nil, 503), "data")
	}()
	assignment := "/v1/iam/assignment-proposals/" + proof["assignment_id"].(string)
	standing := call(t, "GET", assignment, administrator, nil, 200)
	call(t, "POST", assignment+"/decision", administrator, map[string]any{"expected_version": standing["version"], "decision": "withdraw", "reason": "Synthetic inbox reviewer authority cutoff"}, 200)
	require.NotContains(t, call(t, "GET", path, reviewer, nil, 403), "data", "the same session must lose inbox access immediately")
}

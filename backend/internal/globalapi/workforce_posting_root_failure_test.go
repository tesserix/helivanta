package globalapi

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func assertPostingRootApprovalRollback(t *testing.T, db *tenantdb.DB, tenant, reviewer, root, path string, decision, pending map[string]any, call func(*testing.T, string, string, string, any, int) map[string]any) {
	t.Helper()
	counts := func() [3]int64 {
		var count [3]int64
		require.NoError(t, db.WithTenant(context.Background(), tenant, func(tx *gorm.DB) error {
			for i, action := range []string{"workforce.staff_actor.use", "workforce.posting_proposal.approved"} {
				if err := tx.Table("workforce_audit").Where("actor_subject=? AND action=?", reviewer, action).Count(&count[i]).Error; err != nil {
					return err
				}
			}
			return tx.Table("outbox_events").Where("subject=?", workforcecontract.SubjectPostingProposalChanged).Count(&count[2]).Error
		}))
		return count
	}
	for _, fault := range []struct{ table, constraint, check string }{
		{"workforce_posting_management_reviews", "synthetic_root_review_evidence", "false"},
		{"workforce_audit", "synthetic_root_review_audit", "action<>'workforce.posting_proposal.approved'"},
		{"outbox_events", "synthetic_root_review_outbox", "subject<>'" + workforcecontract.SubjectPostingProposalChanged + "'"},
	} {
		apply := func(sql string) {
			require.NoError(t, db.Migrate(context.Background(), []tenantdb.Migration{{ID: "test_root_approval_fault_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
		}
		clear := func() { apply("ALTER TABLE " + fault.table + " DROP CONSTRAINT IF EXISTS " + fault.constraint) }
		t.Cleanup(clear)
		before := counts()
		require.Positive(t, before[0], "probe must count actual committed Staff actor-use evidence")
		require.Positive(t, before[2], "probe must count actual canonical proposal events")
		apply("ALTER TABLE " + fault.table + " ADD CONSTRAINT " + fault.constraint + " CHECK (" + fault.check + ") NOT VALID")
		call(t, "PUT", path, reviewer, decision, 500)
		require.Equal(t, before, counts(), "failed approval must roll back Staff use, approval audit and outbox")
		clear()
		require.Equal(t, pending, call(t, "GET", root, reviewer, nil, 200))
		call(t, "GET", path, reviewer, nil, 404)
		require.Len(t, call(t, "GET", root+"/history", reviewer, nil, 200)["data"], 1)
	}
}

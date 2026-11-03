package globalapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func assertPostingContextAuditRollback(t *testing.T, db *tenantdb.DB, tenant, reviewer, path string, call func(*testing.T, string, string, string, any, int) map[string]any, reasons []string) {
	t.Helper()
	countUses := func() int64 {
		var count int64
		require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
			return tx.Table("workforce_audit").Where("actor_subject=? AND action=?", reviewer, "workforce.staff_actor.use").Count(&count).Error
		}))
		return count
	}
	apply := func(sql string) error {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 15*time.Second)
		defer cancel()
		return db.Migrate(ctx, []tenantdb.Migration{{ID: "test_posting_context_audit_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}})
	}
	clear := func() {
		if err := apply("ALTER TABLE workforce_audit DROP CONSTRAINT IF EXISTS synthetic_posting_context_audit"); err != nil {
			t.Errorf("remove context audit fault: %v", err)
		}
	}
	t.Cleanup(clear)
	before := countUses()
	require.Positive(t, before, "probe must count committed actor-use evidence")
	require.NoError(t, apply("ALTER TABLE workforce_audit ADD CONSTRAINT synthetic_posting_context_audit CHECK (action<>'workforce.posting_proposal.context_read_review') NOT VALID"))
	failure := call(t, "GET", path, reviewer, nil, 500)
	require.NotContains(t, failure, "data")
	body, err := json.Marshal(failure)
	require.NoError(t, err)
	for _, reason := range reasons {
		require.NotContains(t, string(body), reason, "audit failure must withhold retained private rationale")
	}
	require.Equal(t, before, countUses(), "failed context disclosure must roll back its Staff source-use evidence")
	clear()
}

package globalapi

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func assertPostingRootDepartmentFirst(t *testing.T, db *tenantdb.DB, tenant string, call func(*testing.T, string, string, string, any, int) map[string]any, base, submitter, reviewer, draft string, receipt, source, generation map[string]any, retireDepartment func()) {
	t.Helper()
	root := base + "/posting-proposals/" + uuid.Must(uuid.NewV7()).String()
	pending := call(t, "PUT", root+"/commands/"+uuid.Must(uuid.NewV7()).String(), submitter, map[string]any{"expected_version": 0, "draft_id": draft, "draft_revision_id": receipt["revision_id"], "draft_version": receipt["version"], "generation_id": generation["id"], "staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "reason": "Synthetic root proposal before department retirement", "sources_examined": true, "supervision": map[string]any{"kind": "hospital_management"}}, 200)
	selector := fmt.Sprintf("?purpose=review&expected_version=1&expected_revision_id=%s&supervision_context=v1&management_review=current", pending["revision_id"])
	call(t, "GET", root+"/context"+selector, reviewer, nil, 200)
	retireDepartment()
	current := call(t, "GET", base+"/posting-review-source", reviewer, nil, 200)
	require.Equal(t, "current", current["generation_state"], "department retirement must be the rejected source, not an unrelated target invalidation")
	require.Equal(t, generation["id"], current["generation_id"])
	effects := postingRootApprovalEffects(t, db, tenant, reviewer)
	path := root + "/decisions/" + uuid.Must(uuid.NewV7()).String()
	denied := call(t, "PUT", path, reviewer, map[string]any{"expected_version": 1, "expected_revision_id": pending["revision_id"], "decision": "approve", "reason": "A retired department cannot authorize a new root approval", "sources_examined": true, "hospital_management_reviewed": true}, 409)
	require.Equal(t, "posting_proposal_conflict", denied["error"])
	require.Equal(t, effects, postingRootApprovalEffects(t, db, tenant, reviewer), "department-first failure cannot commit Staff use, approval audit or outbox")
	require.Equal(t, pending, call(t, "GET", root, reviewer, nil, 200))
	call(t, "GET", path, reviewer, nil, 404)
	require.Len(t, call(t, "GET", root+"/history", reviewer, nil, 200)["data"], 1)
	rejected := call(t, "PUT", root+"/decisions/"+uuid.Must(uuid.NewV7()).String(), reviewer, map[string]any{"expected_version": 1, "expected_revision_id": pending["revision_id"], "decision": "reject", "reason": "Independent rejection after the selected department retired", "sources_examined": true}, 200)
	require.Equal(t, "rejected", rejected["status"])
}

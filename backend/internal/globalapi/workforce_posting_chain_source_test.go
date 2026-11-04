package globalapi

import (
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func assertPostingAncestorSourceCutoff(t *testing.T, db *tenantdb.DB, tenant string, call func(*testing.T, string, string, string, any, int) map[string]any, hospitalBase, reviewer, submitter, staff string, supervisor map[string]any, changeSupervisorIdentity func() func()) {
	t.Helper()
	base := hospitalBase + "/posting-staff/" + staff
	source := call(t, "GET", base+"/posting-review-source", submitter, nil, 200)
	require.Equal(t, "current", source["generation_state"])
	draft := uuid.Must(uuid.NewV7()).String()
	receipt := call(t, "PUT", base+"/posting-drafts/"+draft+"/commands/"+uuid.Must(uuid.NewV7()).String(), submitter, map[string]any{"expected_version": 0, "staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "department_id": supervisor["department_id"], "department_revision_id": supervisor["department_revision_id"], "starts_at": supervisor["starts_at"], "ends_at": supervisor["ends_at"], "reason": "Synthetic member before its supervisor source changes"}, 200)
	root := base + "/posting-proposals/" + uuid.Must(uuid.NewV7()).String()
	pending := call(t, "PUT", root+"/commands/"+uuid.Must(uuid.NewV7()).String(), submitter, map[string]any{"expected_version": 0, "draft_id": draft, "draft_revision_id": receipt["revision_id"], "draft_version": receipt["version"], "generation_id": source["generation_id"], "staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "reason": "Synthetic current-chain source cutoff acceptance", "sources_examined": true, "supervision": map[string]any{"kind": "department_member", "supervisor_staff_id": supervisor["staff_id"], "supervisor_proposal_id": supervisor["id"], "supervisor_revision_id": supervisor["revision_id"]}}, 200)
	selector := fmt.Sprintf("/context?purpose=review&expected_version=1&expected_revision_id=%s&supervision_context=v1", pending["revision_id"])
	require.Len(t, call(t, "GET", root+selector+"&chain_review=current", reviewer, nil, 200)["chain"], 2)
	restoreSupervisorIdentity := changeSupervisorIdentity()
	unchangedTarget := call(t, "GET", base+"/posting-review-source", reviewer, nil, 200)
	require.Equal(t, source["staff_version"], unchangedTarget["staff_version"])
	require.Equal(t, "current", unchangedTarget["generation_state"], "the rejected source belongs to the ancestor, not the target or reviewer")
	before := postingRootApprovalEffects(t, db, tenant, reviewer)
	denied := call(t, "GET", root+selector+"&chain_review=current", reviewer, nil, 409)
	require.NotContains(t, denied, "chain")
	require.Equal(t, before, postingRootApprovalEffects(t, db, tenant, reviewer), "a stale ancestor cannot commit current-chain or actor-use evidence")
	path := root + "/decisions/" + uuid.Must(uuid.NewV7()).String()
	call(t, "PUT", path, reviewer, map[string]any{"expected_version": 1, "expected_revision_id": pending["revision_id"], "decision": "approve", "reason": "A stale supervisor cannot authorize a new member approval", "sources_examined": true}, 409)
	require.Equal(t, before, postingRootApprovalEffects(t, db, tenant, reviewer))
	call(t, "GET", path, reviewer, nil, 404)
	restoreSupervisorIdentity()
	restored := call(t, "GET", root+selector+"&chain_review=current", reviewer, nil, 409)
	require.NotContains(t, restored, "chain", "restoring identity fields cannot revive a retained supervisor approval")
	call(t, "PUT", root+"/decisions/"+uuid.Must(uuid.NewV7()).String(), reviewer, map[string]any{"expected_version": 1, "expected_revision_id": pending["revision_id"], "decision": "approve", "reason": "Restored fields do not revive the previous reviewed epoch", "sources_examined": true}, 409)
	require.Equal(t, before, postingRootApprovalEffects(t, db, tenant, reviewer))
	require.Equal(t, pending, call(t, "GET", root, reviewer, nil, 200))
	require.Len(t, call(t, "GET", root+"/history", reviewer, nil, 200)["data"], 1)
	call(t, "GET", root+selector, reviewer, nil, 200)
	rejected := call(t, "PUT", root+"/decisions/"+uuid.Must(uuid.NewV7()).String(), reviewer, map[string]any{"expected_version": 1, "expected_revision_id": pending["revision_id"], "decision": "reject", "reason": "Independent rejection after the supervisor employment source changes", "sources_examined": true}, 200)
	require.Equal(t, "rejected", rejected["status"])
	historical := call(t, "GET", hospitalBase+"/posting-staff/"+supervisor["staff_id"].(string)+"/posting-proposals/"+supervisor["id"].(string), reviewer, nil, 200)
	require.Equal(t, supervisor, historical, "source cutoff preserves the old approved history")
}

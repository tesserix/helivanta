package globalapi

import (
	"fmt"
	"maps"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func assertPostingRootApproval(t *testing.T, call func(*testing.T, string, string, string, any, int) map[string]any, base, submitter, reviewer, draft string, receipt, source, generation map[string]any, enableManagement, enableSubmitterReview func()) {
	t.Helper()
	root := base + "/posting-proposals/" + uuid.Must(uuid.NewV7()).String()
	command := root + "/commands/" + uuid.Must(uuid.NewV7()).String()
	input := map[string]any{"expected_version": 0, "draft_id": draft, "draft_revision_id": receipt["revision_id"], "draft_version": receipt["version"], "generation_id": generation["id"], "staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "reason": "Synthetic explicit hospital management intent", "sources_examined": true, "supervision": map[string]any{"kind": "hospital_management"}}
	pending := call(t, "PUT", command, submitter, input, 200)
	selector := fmt.Sprintf("?purpose=review&expected_version=1&expected_revision_id=%s&supervision_context=v1", pending["revision_id"])
	view := call(t, "GET", root+"/context"+selector, reviewer, nil, 200)
	require.Equal(t, input["supervision"], view["supervision"])
	decision := map[string]any{"expected_version": 1, "expected_revision_id": pending["revision_id"], "decision": "approve", "reason": "Independent synthetic hospital management posting review", "sources_examined": true, "hospital_management_reviewed": true}
	path := root + "/decisions/" + uuid.Must(uuid.NewV7()).String()
	call(t, "PUT", path, reviewer, decision, 403)
	require.Equal(t, pending, call(t, "GET", root, reviewer, nil, 200), "denied management authority cannot mutate the proposal")
	enableManagement()
	unattested := maps.Clone(decision)
	delete(unattested, "hospital_management_reviewed")
	call(t, "PUT", path, reviewer, unattested, 409)
	require.Equal(t, pending, call(t, "GET", root, reviewer, nil, 200), "missing explicit attestation cannot consume a decision command")
	enableSubmitterReview()
	call(t, "PUT", path, submitter, decision, 403)
	require.Equal(t, pending, call(t, "GET", root, reviewer, nil, 200), "a fully entitled target/submitter still cannot approve their own proposal")
	approved := call(t, "PUT", path, reviewer, decision, 200)
	require.Equal(t, "approved", approved["status"])
	require.Equal(t, float64(2), approved["version"])
	require.Equal(t, false, approved["grants_access"], "managerial posting evidence does not grant product, clinical or login access")
	require.Equal(t, approved, call(t, "PUT", path, reviewer, decision, 200))
	require.Equal(t, approved, call(t, "GET", path, reviewer, nil, 200))
	changed := maps.Clone(decision)
	changed["reason"] = "Synthetic changed management attestation under a retained command"
	call(t, "PUT", path, reviewer, changed, 409)
	require.Equal(t, approved, call(t, "GET", root, reviewer, nil, 200))
	history := call(t, "GET", root+"/history?limit=2", reviewer, nil, 200)["data"].([]any)
	require.Equal(t, []any{approved, pending}, history)
}

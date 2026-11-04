package globalapi

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func assertPostingRootApproval(t *testing.T, db *tenantdb.DB, tenant string, call func(*testing.T, string, string, string, any, int) map[string]any, request func(context.Context, string, string, string, []byte, string) *httptest.ResponseRecorder, base, submitter, reviewer, draft string, receipt, source, generation map[string]any, enableManagement, enableSubmitterReview func()) {
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
	call(t, "GET", root+"/context"+selector+"&management_review=current", reviewer, nil, 403)
	enableManagement()
	require.Equal(t, view, call(t, "GET", root+"/context"+selector+"&management_review=current", reviewer, nil, 200), "management readiness retains the exact bounded context envelope")
	assertPostingContextAuditRollback(t, db, tenant, reviewer, root+"/context"+selector+"&management_review=current", "workforce.posting_proposal.context_read_management_review", call, []string{input["reason"].(string)})
	unattested := maps.Clone(decision)
	delete(unattested, "hospital_management_reviewed")
	call(t, "PUT", path, reviewer, unattested, 409)
	require.Equal(t, pending, call(t, "GET", root, reviewer, nil, 200), "missing explicit attestation cannot consume a decision command")
	enableSubmitterReview()
	call(t, "GET", root+"/context"+selector+"&management_review=current", submitter, nil, 403)
	call(t, "PUT", path, submitter, decision, 403)
	require.Equal(t, pending, call(t, "GET", root, reviewer, nil, 200), "a fully entitled target/submitter still cannot approve their own proposal")
	for _, change := range []map[string]any{{"expected_version": 2}, {"expected_revision_id": uuid.Must(uuid.NewV7()).String()}} {
		stale := maps.Clone(decision)
		maps.Copy(stale, change)
		call(t, "PUT", path, reviewer, stale, 409)
		call(t, "GET", path, reviewer, nil, 404)
	}
	assertPostingRootApprovalRollback(t, db, tenant, reviewer, root, path, decision, pending, call)
	beforeEffects := postingRootApprovalEffects(t, db, tenant, reviewer)
	body, err := json.Marshal(decision)
	require.NoError(t, err)
	approved := assertConcurrentPostingRootApproval(t, func(ctx context.Context) *httptest.ResponseRecorder {
		return request(ctx, "PUT", path, reviewer, body, "application/json")
	})
	afterEffects := postingRootApprovalEffects(t, db, tenant, reviewer)
	require.Equal(t, beforeEffects[2]+1, afterEffects[2], "concurrent approvals must publish only one canonical decision event")
	require.Equal(t, beforeEffects[1]+1, afterEffects[1], "concurrent approvals must retain only one approval audit")
	require.Equal(t, "approved", approved["status"])
	terminal := fmt.Sprintf("?purpose=review&expected_version=2&expected_revision_id=%s&supervision_context=v1&management_review=current", approved["revision_id"])
	call(t, "GET", root+"/context"+terminal, reviewer, nil, 409)
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
	pastDraft := uuid.Must(uuid.NewV7()).String()
	pastReceipt := call(t, "PUT", base+"/posting-drafts/"+pastDraft+"/commands/"+uuid.Must(uuid.NewV7()).String(), submitter, map[string]any{"expected_version": 0, "staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "department_id": receipt["department_id"], "department_revision_id": receipt["department_revision_id"], "starts_at": time.Now().UTC().Add(-5 * time.Minute).Format(time.RFC3339), "ends_at": time.Now().UTC().Add(-time.Minute).Format(time.RFC3339), "reason": "Synthetic historical preparation whose nominal posting interval has ended"}, 200)
	pastRoot := base + "/posting-proposals/" + uuid.Must(uuid.NewV7()).String()
	pastPending := call(t, "PUT", pastRoot+"/commands/"+uuid.Must(uuid.NewV7()).String(), submitter, map[string]any{"expected_version": 0, "draft_id": pastDraft, "draft_revision_id": pastReceipt["revision_id"], "draft_version": pastReceipt["version"], "generation_id": generation["id"], "staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "reason": "Synthetic ended nominal root with otherwise current employment sources", "sources_examined": true, "supervision": map[string]any{"kind": "hospital_management"}}, 200)
	pastPath := pastRoot + "/decisions/" + uuid.Must(uuid.NewV7()).String()
	beforePast := postingRootApprovalEffects(t, db, tenant, reviewer)
	ended := call(t, "PUT", pastPath, reviewer, map[string]any{"expected_version": 1, "expected_revision_id": pastPending["revision_id"], "decision": "approve", "reason": "An ended nominal appointment cannot receive a new approval", "sources_examined": true, "hospital_management_reviewed": true}, 409)
	require.Equal(t, "posting_proposal_conflict", ended["error"])
	require.Equal(t, beforePast, postingRootApprovalEffects(t, db, tenant, reviewer))
	require.Equal(t, pastPending, call(t, "GET", pastRoot, reviewer, nil, 200))
	call(t, "GET", pastPath, reviewer, nil, 404)
	rejectedPast := call(t, "PUT", pastRoot+"/decisions/"+uuid.Must(uuid.NewV7()).String(), reviewer, map[string]any{"expected_version": 1, "expected_revision_id": pastPending["revision_id"], "decision": "reject", "reason": "Independent rejection of the synthetic ended appointment", "sources_examined": true}, 200)
	require.Equal(t, "rejected", rejectedPast["status"])

}

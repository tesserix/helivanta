package globalapi

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func assertPostingSupervisedApproval(t *testing.T, db *tenantdb.DB, tenant string, call func(*testing.T, string, string, string, any, int) map[string]any, hospitalBase, childBase, submitter, reviewer, supervisorStaff, rootApprover, rootApproverStaff, organizationMaker, childDraft string, childReceipt, childSource, generation map[string]any, changeSupervisorIdentity func() func()) {
	t.Helper()
	parentBase := hospitalBase + "/posting-staff/" + supervisorStaff
	parentSource := call(t, "GET", parentBase+"/posting-review-source", reviewer, nil, 200)
	require.Equal(t, "current", parentSource["generation_state"])
	parentDraft := uuid.Must(uuid.NewV7()).String()
	parentReceipt := call(t, "PUT", parentBase+"/posting-drafts/"+parentDraft+"/commands/"+uuid.Must(uuid.NewV7()).String(), reviewer, map[string]any{"expected_version": 0, "staff_version": parentSource["staff_version"], "employment_epoch": parentSource["employment_epoch"], "department_id": childReceipt["department_id"], "department_revision_id": childReceipt["department_revision_id"], "starts_at": childReceipt["starts_at"], "ends_at": childReceipt["ends_at"], "reason": "Synthetic canonical supervisor root preparation"}, 200)
	parentRoot := parentBase + "/posting-proposals/" + uuid.Must(uuid.NewV7()).String()
	parentPending := call(t, "PUT", parentRoot+"/commands/"+uuid.Must(uuid.NewV7()).String(), reviewer, map[string]any{"expected_version": 0, "draft_id": parentDraft, "draft_revision_id": parentReceipt["revision_id"], "draft_version": parentReceipt["version"], "generation_id": parentSource["generation_id"], "staff_version": parentSource["staff_version"], "employment_epoch": parentSource["employment_epoch"], "reason": "Synthetic explicit canonical root to supervise a separate Staff posting", "sources_examined": true, "supervision": map[string]any{"kind": "hospital_management"}}, 200)
	parentApproved := call(t, "PUT", parentRoot+"/decisions/"+uuid.Must(uuid.NewV7()).String(), rootApprover, map[string]any{"expected_version": 1, "expected_revision_id": parentPending["revision_id"], "decision": "approve", "reason": "Independent synthetic root management appointment for a supervised department", "sources_examined": true, "hospital_management_reviewed": true}, 200)
	assertPostingSupervisorRootChoice(t, call, childReceipt["hospital_id"].(string), submitter, reviewer, supervisorStaff, parentApproved)
	newChildDraft := func(reason string) (string, map[string]any) {
		id := uuid.Must(uuid.NewV7()).String()
		row := call(t, "PUT", childBase+"/posting-drafts/"+id+"/commands/"+uuid.Must(uuid.NewV7()).String(), submitter, map[string]any{"expected_version": 0, "staff_version": childSource["staff_version"], "employment_epoch": childSource["employment_epoch"], "department_id": childReceipt["department_id"], "department_revision_id": childReceipt["department_revision_id"], "starts_at": childReceipt["starts_at"], "ends_at": childReceipt["ends_at"], "reason": reason}, 200)
		return id, row
	}
	proposeChild := func(root, draft string, receipt map[string]any, intent map[string]any) map[string]any {
		return call(t, "PUT", root+"/commands/"+uuid.Must(uuid.NewV7()).String(), submitter, map[string]any{"expected_version": 0, "draft_id": draft, "draft_revision_id": receipt["revision_id"], "draft_version": receipt["version"], "generation_id": generation["id"], "staff_version": childSource["staff_version"], "employment_epoch": childSource["employment_epoch"], "reason": "Synthetic bounded canonical supervision intent", "sources_examined": true, "supervision": intent}, 200)
	}
	pastDraft := uuid.Must(uuid.NewV7()).String()
	pastStart, err := time.Parse(time.RFC3339Nano, childReceipt["starts_at"].(string))
	require.NoError(t, err)
	pastEnd := pastStart.Add(time.Microsecond)
	require.True(t, time.Now().After(pastEnd))
	pastReceipt := call(t, "PUT", childBase+"/posting-drafts/"+pastDraft+"/commands/"+uuid.Must(uuid.NewV7()).String(), submitter, map[string]any{"expected_version": 0, "staff_version": childSource["staff_version"], "employment_epoch": childSource["employment_epoch"], "department_id": childReceipt["department_id"], "department_revision_id": childReceipt["department_revision_id"], "starts_at": pastStart.Format(time.RFC3339Nano), "ends_at": pastEnd.Format(time.RFC3339Nano), "reason": "Synthetic ended child inside a current supervisor interval"}, 200)
	pastRoot := childBase + "/posting-proposals/" + uuid.Must(uuid.NewV7()).String()
	pastPending := proposeChild(pastRoot, pastDraft, pastReceipt, map[string]any{"kind": "department_supervisor", "supervisor_staff_id": supervisorStaff, "supervisor_proposal_id": parentApproved["id"], "supervisor_revision_id": parentApproved["revision_id"]})
	pastPath := pastRoot + "/decisions/" + uuid.Must(uuid.NewV7()).String()
	beforePast := postingRootApprovalEffects(t, db, tenant, reviewer)
	pastDenied := call(t, "PUT", pastPath, reviewer, map[string]any{"expected_version": 1, "expected_revision_id": pastPending["revision_id"], "decision": "approve", "reason": "A current supervisor cannot authorize an already ended child posting", "sources_examined": true}, 409)
	require.Equal(t, "posting_proposal_conflict", pastDenied["error"])
	require.Equal(t, beforePast, postingRootApprovalEffects(t, db, tenant, reviewer))
	require.Equal(t, pastPending, call(t, "GET", pastRoot, reviewer, nil, 200))
	call(t, "GET", pastPath, reviewer, nil, 404)
	pastRejected := call(t, "PUT", pastRoot+"/decisions/"+uuid.Must(uuid.NewV7()).String(), reviewer, map[string]any{"expected_version": 1, "expected_revision_id": pastPending["revision_id"], "decision": "reject", "reason": "Independent rejection of the ended child posting", "sources_examined": true}, 200)
	require.Equal(t, "rejected", pastRejected["status"])
	selfRootDraft, selfRootReceipt := newChildDraft("Synthetic separate root to prove repeated Staff refusal")
	selfRoot := childBase + "/posting-proposals/" + uuid.Must(uuid.NewV7()).String()
	selfRootPending := proposeChild(selfRoot, selfRootDraft, selfRootReceipt, map[string]any{"kind": "hospital_management"})
	selfRootApproved := call(t, "PUT", selfRoot+"/decisions/"+uuid.Must(uuid.NewV7()).String(), reviewer, map[string]any{"expected_version": 1, "expected_revision_id": selfRootPending["revision_id"], "decision": "approve", "reason": "Independent root appointment does not permit reporting back to the same Staff", "sources_examined": true, "hospital_management_reviewed": true}, 200)
	selfDraft, selfReceipt := newChildDraft("Synthetic repeated Staff department posting")
	selfProposal := childBase + "/posting-proposals/" + uuid.Must(uuid.NewV7()).String()
	selfPending := proposeChild(selfProposal, selfDraft, selfReceipt, map[string]any{"kind": "department_supervisor", "supervisor_staff_id": childReceipt["staff_id"], "supervisor_proposal_id": selfRootApproved["id"], "supervisor_revision_id": selfRootApproved["revision_id"]})
	selfDecision := selfProposal + "/decisions/" + uuid.Must(uuid.NewV7()).String()
	beforeSelf := postingRootApprovalEffects(t, db, tenant, reviewer)
	deniedSelf := call(t, "PUT", selfDecision, reviewer, map[string]any{"expected_version": 1, "expected_revision_id": selfPending["revision_id"], "decision": "approve", "reason": "A valid earlier appointment cannot authorize a repeated Staff chain", "sources_examined": true}, 409)
	require.Equal(t, "posting_proposal_conflict", deniedSelf["error"])
	require.Equal(t, beforeSelf, postingRootApprovalEffects(t, db, tenant, reviewer))
	require.Equal(t, selfPending, call(t, "GET", selfProposal, reviewer, nil, 200))
	call(t, "GET", selfDecision, reviewer, nil, 404)
	withdrawnSelf := call(t, "PUT", selfProposal+"/decisions/"+uuid.Must(uuid.NewV7()).String(), submitter, map[string]any{"expected_version": 1, "expected_revision_id": selfPending["revision_id"], "decision": "withdraw", "reason": "Withdraw the synthetic repeated Staff proposal after refusal", "sources_examined": true}, 200)
	require.Equal(t, "withdrawn", withdrawnSelf["status"])
	childRoot := childBase + "/posting-proposals/" + uuid.Must(uuid.NewV7()).String()
	intent := map[string]any{"kind": "department_supervisor", "supervisor_staff_id": supervisorStaff, "supervisor_proposal_id": parentApproved["id"], "supervisor_revision_id": parentApproved["revision_id"]}
	pending := call(t, "PUT", childRoot+"/commands/"+uuid.Must(uuid.NewV7()).String(), submitter, map[string]any{"expected_version": 0, "draft_id": childDraft, "draft_revision_id": childReceipt["revision_id"], "draft_version": childReceipt["version"], "generation_id": generation["id"], "staff_version": childSource["staff_version"], "employment_epoch": childSource["employment_epoch"], "reason": "Synthetic department supervisor reporting to an exact approved root", "sources_examined": true, "supervision": intent}, 200)
	selector := fmt.Sprintf("?purpose=review&expected_version=1&expected_revision_id=%s&supervision_context=v1", pending["revision_id"])
	require.Equal(t, intent, call(t, "GET", childRoot+"/context"+selector, reviewer, nil, 200)["supervision"])
	chainView := call(t, "GET", childRoot+"/context"+selector+"&chain_review=current", reviewer, nil, 200)
	require.Equal(t, intent, chainView["supervision"])
	nodes := chainView["chain"].([]any)
	require.Len(t, nodes, 1)
	rootNode := nodes[0].(map[string]any)
	require.Equal(t, "hospital_management", rootNode["supervision"].(map[string]any)["kind"])
	rootPosting := rootNode["posting"].(map[string]any)
	require.Equal(t, parentApproved["id"], rootPosting["id"])
	require.Equal(t, supervisorStaff, rootPosting["staff"].(map[string]any)["id"])
	require.NotEmpty(t, rootPosting["staff"].(map[string]any)["display_name"])
	require.NotContains(t, rootPosting, "reason")
	require.NotContains(t, rootPosting, "draft_reason")

	call(t, "GET", childRoot+"/context"+selector+"&chain_review=current", submitter, nil, 403)
	call(t, "GET", childRoot+"/context"+selector+"&chain_review=current&management_review=current", reviewer, nil, 400)
	beforeReadFault := postingRootApprovalEffects(t, db, tenant, reviewer)
	applyReadFault := func(sql string) {
		require.NoError(t, db.Migrate(context.WithoutCancel(t.Context()), []tenantdb.Migration{{ID: "synthetic_chain_context_fault_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
	}
	clearReadFault := func() {
		applyReadFault("ALTER TABLE workforce_audit DROP CONSTRAINT IF EXISTS synthetic_chain_context_read")
	}
	t.Cleanup(clearReadFault)
	applyReadFault("ALTER TABLE workforce_audit ADD CONSTRAINT synthetic_chain_context_read CHECK(action<>'workforce.posting_proposal.context_read_chain_review') NOT VALID")
	failedRead := call(t, "GET", childRoot+"/context"+selector+"&chain_review=current", reviewer, nil, 500)
	require.NotContains(t, failedRead, "chain")
	require.Equal(t, beforeReadFault, postingRootApprovalEffects(t, db, tenant, reviewer), "failed read audit also rolls back Staff actor-use evidence")
	clearReadFault()
	path := childRoot + "/decisions/" + uuid.Must(uuid.NewV7()).String()
	decision := map[string]any{"expected_version": 1, "expected_revision_id": pending["revision_id"], "decision": "approve", "reason": "Independent department appointment under the reviewed canonical root", "sources_examined": true}
	for _, table := range []string{"workforce_posting_chain_reviews", "workforce_posting_chain_sources"} {
		constraint := "synthetic_chain_evidence_write"
		apply := func(sql string) {
			require.NoError(t, db.Migrate(context.WithoutCancel(t.Context()), []tenantdb.Migration{{ID: "synthetic_chain_fault_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
		}
		clear := func() { apply("ALTER TABLE " + table + " DROP CONSTRAINT IF EXISTS " + constraint) }
		t.Cleanup(clear)
		before := postingRootApprovalEffects(t, db, tenant, reviewer)
		apply("ALTER TABLE " + table + " ADD CONSTRAINT " + constraint + " CHECK (false) NOT VALID")
		call(t, "PUT", path, reviewer, decision, 500)
		require.Equal(t, before, postingRootApprovalEffects(t, db, tenant, reviewer), "chain evidence failure rolls back actor use, approval audit and canonical event")
		clear()
		require.Equal(t, pending, call(t, "GET", childRoot, reviewer, nil, 200))
		call(t, "GET", path, reviewer, nil, 404)
		require.Len(t, call(t, "GET", childRoot+"/history", reviewer, nil, 200)["data"], 1)
	}
	before := postingRootApprovalEffects(t, db, tenant, reviewer)
	approved := call(t, "PUT", path, reviewer, decision, 200)
	after := postingRootApprovalEffects(t, db, tenant, reviewer)
	require.Equal(t, before[1]+1, after[1], "successful child approval records exactly one approval audit")
	require.Equal(t, before[2]+1, after[2], "successful child approval publishes exactly one canonical event")
	require.Equal(t, "approved", approved["status"])
	require.Equal(t, false, approved["grants_access"])
	require.Equal(t, approved, call(t, "PUT", path, reviewer, decision, 200))
	require.Equal(t, approved, call(t, "GET", path, reviewer, nil, 200))
	require.Equal(t, []any{approved, pending}, call(t, "GET", childRoot+"/history?limit=2", reviewer, nil, 200)["data"])
	assertPostingDepartmentMember(t, db, tenant, call, hospitalBase, reviewer, rootApprover, rootApproverStaff, organizationMaker, approved)
	assertPostingSupervisorChoiceContinuation(t, db, tenant, call, childReceipt["hospital_id"].(string), submitter, reviewer, approved, selfRootApproved)
	assertPostingAncestorSourceCutoff(t, db, tenant, call, hospitalBase, reviewer, rootApprover, rootApproverStaff, approved, changeSupervisorIdentity)
}

package globalapi

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func postingCutoffEffects(t *testing.T, db *tenantdb.DB, tenant, actor string) [3]int64 {
	t.Helper()
	var counts [3]int64
	require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
		for i, action := range []string{"workforce.staff_actor.use", "workforce.posting_proposal.cutoff_recorded"} {
			if err := tx.Table("workforce_audit").Where("actor_subject=? AND action=?", actor, action).Count(&counts[i]).Error; err != nil {
				return err
			}
		}
		return tx.Table("outbox_events").Where("subject=?", "helivanta.in.workforce.posting_cutoff_changed.v1").Count(&counts[2]).Error
	}))
	return counts
}

func assertPostingRetainedAncestorCutoff(t *testing.T, db *tenantdb.DB, tenant string, call func(*testing.T, string, string, string, any, int) map[string]any, hospitalBase, submitter, reviewer, administrator string, rootTemplate, childTemplate map[string]any) {
	t.Helper()
	prepare := func(template map[string]any, maker string, supervision map[string]any) (string, map[string]any) {
		base := fmt.Sprintf("%s/posting-staff/%s", hospitalBase, template["staff_id"])
		source := call(t, "GET", base+"/posting-review-source", maker, nil, 200)
		require.Equal(t, "current", source["generation_state"])
		draft := uuid.Must(uuid.NewV7()).String()
		drafted := call(t, "PUT", base+"/posting-drafts/"+draft+"/commands/"+uuid.Must(uuid.NewV7()).String(), maker, map[string]any{"expected_version": 0, "staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "department_id": template["department_id"], "department_revision_id": template["department_revision_id"], "starts_at": template["starts_at"], "ends_at": template["ends_at"], "reason": "Synthetic separate appointment for retained-cutoff acceptance"}, 200)
		root := base + "/posting-proposals/" + uuid.Must(uuid.NewV7()).String()
		pending := call(t, "PUT", root+"/commands/"+uuid.Must(uuid.NewV7()).String(), maker, map[string]any{"expected_version": 0, "draft_id": draft, "draft_revision_id": drafted["revision_id"], "draft_version": drafted["version"], "generation_id": source["generation_id"], "staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "reason": "Synthetic exact source pins for retained-cutoff acceptance", "sources_examined": true, "supervision": supervision}, 200)
		return root, pending
	}
	parentRoot, parentPending := prepare(rootTemplate, reviewer, map[string]any{"kind": "hospital_management"})
	rootLifecycleSelector := fmt.Sprintf("/context?purpose=review&expected_version=1&expected_revision_id=%s&supervision_context=v1&lifecycle_review=current&management_review=current", parentPending["revision_id"])
	rootLifecycle := call(t, "GET", parentRoot+rootLifecycleSelector, administrator, nil, 200)
	require.Empty(t, rootLifecycle["chain"])
	require.EqualValues(t, 0, rootLifecycle["cutoff"].(map[string]any)["version"])
	require.Equal(t, "hospital_management", rootLifecycle["supervision"].(map[string]any)["kind"])
	parent := call(t, "PUT", parentRoot+"/decisions/"+uuid.Must(uuid.NewV7()).String(), administrator, map[string]any{"expected_version": 1, "expected_revision_id": parentPending["revision_id"], "decision": "approve", "reason": "Independent synthetic root for retained ancestor cutoff", "sources_examined": true, "hospital_management_reviewed": true}, 200)
	childRoot, child := prepare(childTemplate, submitter, map[string]any{"kind": "department_supervisor", "supervisor_staff_id": parent["staff_id"], "supervisor_proposal_id": parent["id"], "supervisor_revision_id": parent["revision_id"]})
	selector := fmt.Sprintf("/context?purpose=review&expected_version=1&expected_revision_id=%s&supervision_context=v1&chain_review=current", child["revision_id"])
	call(t, "GET", childRoot+selector, reviewer, nil, 200)
	path := parentRoot + "/cutoffs/commands/" + uuid.Must(uuid.NewV7()).String()
	input := map[string]any{"expected_revision_id": parent["revision_id"], "expected_cutoff_version": 0, "mode": "scheduled", "cutoff_at": time.Now().UTC().Add(5 * time.Minute).Format(time.RFC3339Nano), "reason": "Synthetic scheduled ancestor ending requires explicit lifecycle review", "sources_examined": true}
	cutoff := call(t, "PUT", path, administrator, input, 200)
	require.Equal(t, "scheduled", cutoff["mode"])
	before := postingRootApprovalEffects(t, db, tenant, reviewer)
	denied := call(t, "GET", childRoot+selector, reviewer, nil, 409)
	require.Equal(t, "posting_lifecycle_review_required", denied["error"])
	require.NotContains(t, denied, "chain", "a legacy context must not imply its old source deadline remains sufficient")
	decisionPath := childRoot + "/decisions/" + uuid.Must(uuid.NewV7()).String()
	call(t, "PUT", decisionPath, reviewer, map[string]any{"expected_version": 1, "expected_revision_id": child["revision_id"], "decision": "approve", "reason": "A stale chain observation cannot ignore a newly retained cutoff", "sources_examined": true}, 409)
	require.Equal(t, before, postingRootApprovalEffects(t, db, tenant, reviewer), "failed cutoff-context reads and approval retain no actor use, approval audit or event")
	require.Equal(t, child, call(t, "GET", childRoot, reviewer, nil, 200))
	call(t, "GET", decisionPath, reviewer, nil, 404)
	lifecycleSelector := fmt.Sprintf("/context?purpose=review&expected_version=1&expected_revision_id=%s&supervision_context=v1&lifecycle_review=current", child["revision_id"])
	lifecycle := call(t, "GET", childRoot+lifecycleSelector, reviewer, nil, 200)
	require.Equal(t, child, lifecycle["context"].(map[string]any)["proposal"])
	require.Equal(t, cutoff["cutoff_at"], lifecycle["effective_until"], "live eligibility includes the retained ancestor cutoff without rewriting nominal source pins")
	selectedCutoff := lifecycle["cutoff"].(map[string]any)
	require.EqualValues(t, 0, selectedCutoff["version"])
	require.Nil(t, selectedCutoff["id"])
	require.Nil(t, selectedCutoff["cutoff_at"])
	lifecycleChain := lifecycle["chain"].([]any)
	require.Len(t, lifecycleChain, 1)
	ancestor := lifecycleChain[0].(map[string]any)
	require.Equal(t, parent["id"], ancestor["posting"].(map[string]any)["id"])
	ancestorCutoff := ancestor["cutoff"].(map[string]any)
	require.Equal(t, cutoff["id"], ancestorCutoff["id"])
	require.Equal(t, cutoff["version"], ancestorCutoff["version"])
	require.Equal(t, cutoff["cutoff_at"], ancestorCutoff["cutoff_at"])
	require.Len(t, ancestorCutoff, 3, "reviewers do not receive cutoff administrator rationale or actor evidence")
	call(t, "GET", childRoot+lifecycleSelector, submitter, nil, 403)
	applyLifecycleFault := func(sql string) {
		require.NoError(t, db.Migrate(context.WithoutCancel(t.Context()), []tenantdb.Migration{{ID: "synthetic_lifecycle_context_fault_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
	}
	clearLifecycleFault := func() {
		applyLifecycleFault("ALTER TABLE workforce_audit DROP CONSTRAINT IF EXISTS synthetic_lifecycle_context_audit")
	}
	t.Cleanup(clearLifecycleFault)
	beforeLifecycleFault := postingRootApprovalEffects(t, db, tenant, reviewer)
	applyLifecycleFault("ALTER TABLE workforce_audit ADD CONSTRAINT synthetic_lifecycle_context_audit CHECK(action<>'workforce.posting_proposal.context_read_lifecycle_review') NOT VALID")
	failedLifecycle := call(t, "GET", childRoot+lifecycleSelector, reviewer, nil, 500)
	require.NotContains(t, failedLifecycle, "context")
	require.NotContains(t, failedLifecycle, "chain")
	require.Equal(t, beforeLifecycleFault, postingRootApprovalEffects(t, db, tenant, reviewer), "a failed private lifecycle disclosure rolls back actor-use evidence")
	clearLifecycleFault()
	futureTemplate := map[string]any{}
	for key, value := range childTemplate {
		futureTemplate[key] = value
	}
	cutoffInstant, err := time.Parse(time.RFC3339Nano, cutoff["cutoff_at"].(string))
	require.NoError(t, err)
	futureTemplate["starts_at"] = cutoffInstant.Add(time.Minute).Format(time.RFC3339Nano)
	futureRoot, futureChild := prepare(futureTemplate, submitter, map[string]any{"kind": "department_supervisor", "supervisor_staff_id": parent["staff_id"], "supervisor_proposal_id": parent["id"], "supervisor_revision_id": parent["revision_id"]})
	futureSelector := fmt.Sprintf("/context?purpose=review&expected_version=1&expected_revision_id=%s&supervision_context=v1&lifecycle_review=current", futureChild["revision_id"])
	call(t, "GET", futureRoot+futureSelector, reviewer, nil, 409)
	rejected := call(t, "PUT", childRoot+"/decisions/"+uuid.Must(uuid.NewV7()).String(), reviewer, map[string]any{"expected_version": 1, "expected_revision_id": child["revision_id"], "decision": "reject", "reason": "Independent rejection remains available after an ancestor cutoff", "sources_examined": true}, 200)
	require.Equal(t, "rejected", rejected["status"])
	receipts := []map[string]any{cutoff}
	deadline, err := time.Parse(time.RFC3339Nano, cutoff["cutoff_at"].(string))
	require.NoError(t, err)
	for version := 2; version <= 10; version++ {
		instant := deadline.Add(-time.Duration(version-1) * 5 * time.Second).Format(time.RFC3339Nano)
		row := call(t, "PUT", parentRoot+"/cutoffs/commands/"+uuid.Must(uuid.NewV7()).String(), administrator, map[string]any{"expected_revision_id": parent["revision_id"], "expected_cutoff_version": version - 1, "mode": "scheduled", "cutoff_at": instant, "reason": "Synthetic bounded retained cutoff history", "sources_examined": true}, 200)
		receipts = append(receipts, row)
	}
	historyPath := parentRoot + "/cutoffs/history?expected_revision_id=" + parent["revision_id"].(string) + "&limit=8"
	first := call(t, "GET", historyPath, administrator, nil, 200)
	rows := first["data"].([]any)
	require.Len(t, rows, 8)
	for i, row := range rows {
		require.Equal(t, receipts[len(receipts)-1-i], row)
	}
	cursor := first["page"].(map[string]any)["next_cursor"].(string)
	require.True(t, first["page"].(map[string]any)["has_more"].(bool))
	last := call(t, "GET", historyPath+"&cursor="+cursor, administrator, nil, 200)
	require.Equal(t, []any{receipts[1], receipts[0]}, last["data"])
	require.Equal(t, false, last["page"].(map[string]any)["has_more"])
	call(t, "GET", historyPath+"&cursor="+cursor, reviewer, nil, 400)
	call(t, "GET", parentRoot+"/cutoffs/history?expected_revision_id="+parentPending["revision_id"].(string)+"&cursor="+cursor, administrator, nil, 400)
	currentChildRoot, currentChild := prepare(childTemplate, submitter, map[string]any{"kind": "department_supervisor", "supervisor_staff_id": parent["staff_id"], "supervisor_proposal_id": parent["id"], "supervisor_revision_id": parent["revision_id"]})
	currentSelector := fmt.Sprintf("/context?purpose=review&expected_version=1&expected_revision_id=%s&supervision_context=v1&lifecycle_review=current", currentChild["revision_id"])
	currentView := call(t, "GET", currentChildRoot+currentSelector, reviewer, nil, 200)
	require.Equal(t, receipts[9]["cutoff_at"], currentView["effective_until"])
	require.EqualValues(t, 10, currentView["chain"].([]any)[0].(map[string]any)["cutoff"].(map[string]any)["version"])
	call(t, "PUT", parentRoot+"/cutoffs/commands/"+uuid.Must(uuid.NewV7()).String(), administrator, map[string]any{"expected_revision_id": parent["revision_id"], "expected_cutoff_version": 10, "mode": "immediate", "reason": "Synthetic ending blocks new lifecycle review without deleting approvals", "sources_examined": true}, 200)
	beforeEndedContext := postingRootApprovalEffects(t, db, tenant, reviewer)
	call(t, "GET", currentChildRoot+currentSelector, reviewer, nil, 409)
	require.Equal(t, beforeEndedContext, postingRootApprovalEffects(t, db, tenant, reviewer))
}

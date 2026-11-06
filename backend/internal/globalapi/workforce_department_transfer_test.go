package globalapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func assertAtomicDepartmentTransfer(t *testing.T, db *tenantdb.DB, tenant string, call func(*testing.T, string, string, string, any, int) map[string]any, request postingTestRequest, staffBase, submitter, reviewer, organizationMaker string, source, generation, original, intent map[string]any, browser func(string, map[string]any, map[string]any), departmentReviewer string, grantDepartment func(string, string, []string), grantSource func(string)) {
	t.Helper()
	destinationID := uuid.Must(uuid.NewV7()).String()
	destination := call(t, "PUT", "/v1/organization/hospitals/"+original["hospital_id"].(string)+"/departments/"+destinationID+"/revisions/"+uuid.Must(uuid.NewV7()).String(), organizationMaker, map[string]any{"expected_version": 0, "code": "SYN-TRANSFER-DEST", "name": "Synthetic Transfer Destination", "kind": "department", "parent_id": nil, "status": "active", "reason": "Canonical destination for department transfer acceptance"}, 200)
	prepare := func(start any, toDestination bool) (string, map[string]any) {
		department, revision := original["department_id"], original["department_revision_id"]
		if toDestination {
			department, revision = destinationID, destination["revision_id"]
		}
		draft := uuid.Must(uuid.NewV7()).String()
		receipt := call(t, "PUT", staffBase+"/posting-drafts/"+draft+"/commands/"+uuid.Must(uuid.NewV7()).String(), submitter, map[string]any{"expected_version": 0, "staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "department_id": department, "department_revision_id": revision, "starts_at": start, "ends_at": original["ends_at"], "reason": "Synthetic independently coordinated transfer placement"}, 200)
		return draft, receipt
	}
	propose := func(draft string, receipt, supervision map[string]any) (string, map[string]any) {
		root := staffBase + "/posting-proposals/" + uuid.Must(uuid.NewV7()).String()
		pending := call(t, "PUT", root+"/commands/"+uuid.Must(uuid.NewV7()).String(), submitter, map[string]any{"expected_version": 0, "draft_id": draft, "draft_revision_id": receipt["revision_id"], "draft_version": receipt["version"], "generation_id": generation["id"], "staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "reason": "Synthetic independently reviewed transfer responsibility", "sources_examined": true, "supervision": supervision}, 200)
		return root, pending
	}

	require.NotEmpty(t, departmentReviewer, "include the preceding department reviewer activation")
	grantDepartment(departmentReviewer, destinationID, []string{destinationID})
	grantSource(departmentReviewer)
	currentDraft, currentReceipt := prepare(original["starts_at"], true)
	currentRoot, currentPending := propose(currentDraft, currentReceipt, intent)
	current := call(t, "PUT", currentRoot+"/decisions/"+uuid.Must(uuid.NewV7()).String(), reviewer, map[string]any{"expected_version": 1, "expected_revision_id": currentPending["revision_id"], "decision": "approve", "reason": "Reviewed current destination member for scoped roster acceptance", "sources_examined": true}, 200)
	hospitalBase := strings.Split(staffBase, "/posting-staff/")[0]
	roster := hospitalBase + "/department-team?limit=8"
	scoped := call(t, "GET", roster+"&department="+destinationID, departmentReviewer, nil, 200)
	require.Len(t, scoped["data"], 1)
	member := scoped["data"].([]any)[0].(map[string]any)
	require.Equal(t, current["id"], member["posting_id"])
	require.Equal(t, "current", member["state"], "internal source use can validate the management ancestor outside the held department without disclosing it")
	require.Equal(t, destinationID, member["department_id"])
	call(t, "GET", roster, departmentReviewer, nil, 403)
	call(t, "GET", roster+"&department="+original["department_id"].(string), departmentReviewer, nil, 404)
	call(t, "GET", roster+"&department="+uuid.Must(uuid.NewV7()).String(), departmentReviewer, nil, 404)
	call(t, "GET", roster+"&status=approved", reviewer, nil, 400)
	call(t, "GET", hospitalBase+"/department-team?limit=9", reviewer, nil, 400)
	draft, receipt := prepare(original["starts_at"], false)
	oldRoot, oldPending := propose(draft, receipt, map[string]any{"kind": "hospital_management"})
	old := call(t, "PUT", oldRoot+"/decisions/"+uuid.Must(uuid.NewV7()).String(), reviewer, map[string]any{"expected_version": 1, "expected_revision_id": oldPending["revision_id"], "decision": "approve", "hospital_management_reviewed": true, "reason": "Independent original appointment for transfer acceptance", "sources_examined": true}, 200)
	start := time.Now().UTC().Add(2 * time.Minute).Truncate(time.Microsecond).Format(time.RFC3339Nano)
	nextDraft, nextReceipt := prepare(start, true)
	root, pending := propose(nextDraft, nextReceipt, intent)
	params := fmt.Sprintf("?purpose=review&expected_version=1&expected_revision_id=%s&supervision_context=v1&lifecycle_review=current", pending["revision_id"])
	lifecycle := call(t, "GET", root+"/context"+params, reviewer, nil, 200)
	versions := []any{lifecycle["cutoff"].(map[string]any)["version"]}
	for _, source := range lifecycle["chain"].([]any) {
		versions = append(versions, source.(map[string]any)["cutoff"].(map[string]any)["version"])
	}
	decision := map[string]any{"expected_version": 1, "expected_revision_id": pending["revision_id"], "decision": "approve", "reason": "Independent synchronized department transfer", "sources_examined": true, "lifecycle_review": map[string]any{"cutoff_versions": versions, "effective_until": lifecycle["effective_until"]}}
	input := map[string]any{"from_posting_id": old["id"], "from_revision_id": old["revision_id"], "from_version": old["version"], "from_cutoff_version": 0, "decision": decision}
	command := uuid.Must(uuid.NewV7()).String()
	path := root + "/transfers/" + command
	before := postingRootApprovalEffects(t, db, tenant, reviewer)
	cutoffBefore := postingCutoffEffects(t, db, tenant, reviewer)
	call(t, "PUT", path, submitter, input, 403)
	require.Equal(t, before, postingRootApprovalEffects(t, db, tenant, reviewer))
	require.Equal(t, cutoffBefore, postingCutoffEffects(t, db, tenant, reviewer))
	apply := func(sql string) {
		require.NoError(t, db.Migrate(context.WithoutCancel(t.Context()), []tenantdb.Migration{{ID: "synthetic_transfer_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
	}

	for _, fault := range []struct{ table, constraint, check string }{
		{"workforce_audit", "synthetic_transfer_atomicity", "action<>'workforce.posting_proposal.approved'"},
		{"outbox_events", "synthetic_transfer_outbox", "subject<>'" + workforcecontract.SubjectPostingProposalChanged + "'"},
	} {
		approvalBefore := postingRootApprovalEffects(t, db, tenant, reviewer)
		endingBefore := postingCutoffEffects(t, db, tenant, reviewer)
		clear := func() { apply("ALTER TABLE " + fault.table + " DROP CONSTRAINT IF EXISTS " + fault.constraint) }
		t.Cleanup(clear)
		apply("ALTER TABLE " + fault.table + " ADD CONSTRAINT " + fault.constraint + " CHECK(" + fault.check + ") NOT VALID")
		call(t, "PUT", path, reviewer, input, 500)
		clear()
		require.Equal(t, approvalBefore, postingRootApprovalEffects(t, db, tenant, reviewer), "failed replacement rolls back original ending, source audit and approval")
		require.Equal(t, endingBefore, postingCutoffEffects(t, db, tenant, reviewer))
		call(t, "GET", oldRoot+"/cutoffs/commands/"+command, reviewer, nil, 404)
		require.Equal(t, pending, call(t, "GET", root, reviewer, nil, 200))
	}

	encoded, encodeErr := json.Marshal(input)
	require.NoError(t, encodeErr)
	result := assertConcurrentPostingRootApproval(t, func(ctx context.Context) *httptest.ResponseRecorder {
		return request(ctx, "PUT", path, reviewer, encoded, "application/json")
	})
	replacement := result["replacement"].(map[string]any)
	ending := result["ending"].(map[string]any)
	require.Equal(t, "approved", replacement["status"])
	require.Equal(t, destinationID, replacement["department_id"])
	require.NotEqual(t, old["department_id"], replacement["department_id"])
	require.Equal(t, command, replacement["command_id"])
	require.Equal(t, command, ending["id"])
	require.Equal(t, old["id"], ending["proposal_id"])
	require.Equal(t, replacement["starts_at"], ending["cutoff_at"])
	require.Equal(t, false, replacement["grants_access"])
	after := postingRootApprovalEffects(t, db, tenant, reviewer)
	cutoffAfter := postingCutoffEffects(t, db, tenant, reviewer)
	require.Equal(t, result, call(t, "PUT", path, reviewer, input, 200), "exact retry recovers both retained receipts")
	retryEffects := postingRootApprovalEffects(t, db, tenant, reviewer)
	require.Equal(t, after[1:], retryEffects[1:])
	retryCutoffs := postingCutoffEffects(t, db, tenant, reviewer)
	require.Equal(t, cutoffAfter[1:], retryCutoffs[1:])
	changed := map[string]any{}
	for k, v := range input {
		changed[k] = v
	}
	changed["from_cutoff_version"] = 1
	call(t, "PUT", path, reviewer, changed, 409)
	call(t, "PUT", root+"/transfers/"+uuid.Must(uuid.NewV7()).String(), reviewer, input, 409)
	earlier, parseErr := time.Parse(time.RFC3339Nano, start)
	require.NoError(t, parseErr)
	otherDraft, otherReceipt := prepare(earlier.Add(-30*time.Second).Format(time.RFC3339Nano), true)
	otherRoot, otherPending := propose(otherDraft, otherReceipt, intent)
	otherLifecycle := call(t, "GET", otherRoot+"/context"+fmt.Sprintf("?purpose=review&expected_version=1&expected_revision_id=%s&supervision_context=v1&lifecycle_review=current", otherPending["revision_id"]), reviewer, nil, 200)
	otherDecision := map[string]any{}
	for k, v := range decision {
		otherDecision[k] = v
	}
	otherDecision["expected_revision_id"] = otherPending["revision_id"]
	otherDecision["lifecycle_review"] = map[string]any{"cutoff_versions": versions, "effective_until": otherLifecycle["effective_until"]}
	otherInput := map[string]any{}
	for k, v := range input {
		otherInput[k] = v
	}
	otherInput["decision"], otherInput["from_cutoff_version"] = otherDecision, 1
	call(t, "PUT", otherRoot+"/transfers/"+uuid.Must(uuid.NewV7()).String(), reviewer, otherInput, 409)
	require.Equal(t, otherPending, call(t, "GET", otherRoot, reviewer, nil, 200), "a second replacement cannot borrow an already-transferred original posting")

	call(t, "PUT", oldRoot+"/cutoffs/commands/"+uuid.Must(uuid.NewV7()).String(), reviewer, map[string]any{"expected_revision_id": old["revision_id"], "expected_cutoff_version": 1, "mode": "immediate", "reason": "Synthetic independent revocation after the retained scheduled transfer", "sources_examined": true}, 200)
	require.Equal(t, result, call(t, "PUT", path, reviewer, input, 200), "paired receipt recovery survives a later ending of the original source")
	endedPage := call(t, "GET", roster+"&department="+original["department_id"].(string), reviewer, nil, 200)
	endedFound := false
	for _, value := range endedPage["data"].([]any) {
		observation := value.(map[string]any)
		if observation["posting_id"] == old["id"] {
			endedFound = true
			require.Equal(t, "not_current", observation["state"])
			require.Nil(t, observation["source_until"])
		}
	}
	require.True(t, endedFound, "an ended appointment remains visible as an explicitly non-current observation")
	if browser != nil {
		browserDraft, browserReceipt := prepare(original["starts_at"], false)
		browserOldRoot, browserOldPending := propose(browserDraft, browserReceipt, map[string]any{"kind": "hospital_management"})
		browserOld := call(t, "PUT", browserOldRoot+"/decisions/"+uuid.Must(uuid.NewV7()).String(), reviewer, map[string]any{"expected_version": 1, "expected_revision_id": browserOldPending["revision_id"], "decision": "approve", "hospital_management_reviewed": true, "reason": "Independent browser original appointment", "sources_examined": true}, 200)
		browserNextDraft, browserNextReceipt := prepare(time.Now().UTC().Add(5*time.Minute).Truncate(time.Microsecond).Format(time.RFC3339Nano), true)
		_, browserPending := propose(browserNextDraft, browserNextReceipt, intent)
		browser(reviewer, browserOld, browserPending)
	}

}

package globalapi

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func assertPostingLifecycleFarAncestor(t *testing.T, db *tenantdb.DB, tenant string, call func(*testing.T, string, string, string, any, int) map[string]any, prepare postingTestPreparer, rootTemplate, middleTemplate map[string]any, leafStaff, submitter, reviewer, administrator string) {
	t.Helper()
	require.NotEqual(t, rootTemplate["staff_id"], leafStaff)
	require.NotEqual(t, middleTemplate["staff_id"], leafStaff)
	root, pending := prepare(rootTemplate, reviewer, map[string]any{"kind": "hospital_management"})
	parent := call(t, "PUT", root+"/decisions/"+uuid.Must(uuid.NewV7()).String(), administrator, map[string]any{"expected_version": 1, "expected_revision_id": pending["revision_id"], "decision": "approve", "reason": "Independent legacy root for a three-Staff lifecycle chain", "sources_examined": true, "hospital_management_reviewed": true}, 200)
	until, err := time.Parse(time.RFC3339Nano, parent["ends_at"].(string))
	require.NoError(t, err)
	cutoff := call(t, "PUT", root+"/cutoffs/commands/"+uuid.Must(uuid.NewV7()).String(), administrator, map[string]any{"expected_revision_id": parent["revision_id"], "expected_cutoff_version": 0, "mode": "scheduled", "cutoff_at": until.Add(-time.Minute).Format(time.RFC3339Nano), "reason": "Far ancestor ending bounds all descendant reviews", "sources_examined": true}, 200)
	middleRoot, middle := prepare(middleTemplate, submitter, map[string]any{"kind": "department_supervisor", "supervisor_staff_id": parent["staff_id"], "supervisor_proposal_id": parent["id"], "supervisor_revision_id": parent["revision_id"]})
	context := func(root string, pending map[string]any) map[string]any {
		return call(t, "GET", root+"/context?purpose=review&expected_version=1&expected_revision_id="+pending["revision_id"].(string)+"&supervision_context=v1&lifecycle_review=current", reviewer, nil, 200)
	}
	middleView := context(middleRoot, middle)
	middleApproved := call(t, "PUT", middleRoot+"/decisions/"+uuid.Must(uuid.NewV7()).String(), reviewer, map[string]any{"expected_version": 1, "expected_revision_id": middle["revision_id"], "decision": "approve", "reason": "Middle supervisor retains examined root ending", "sources_examined": true, "lifecycle_review": map[string]any{"cutoff_versions": []int{0, 1}, "effective_until": middleView["effective_until"]}}, 200)
	leafTemplate := map[string]any{}
	for key, value := range middleTemplate {
		leafTemplate[key] = value
	}
	leafTemplate["staff_id"] = leafStaff
	leafRoot, leaf := prepare(leafTemplate, submitter, map[string]any{"kind": "department_member", "supervisor_staff_id": middleApproved["staff_id"], "supervisor_proposal_id": middleApproved["id"], "supervisor_revision_id": middleApproved["revision_id"]})
	view := context(leafRoot, leaf)
	require.Len(t, view["chain"], 2)
	require.Equal(t, cutoff["cutoff_at"], view["effective_until"])
	shorter := until.Add(-2 * time.Minute).Format(time.RFC3339Nano)
	call(t, "PUT", root+"/cutoffs/commands/"+uuid.Must(uuid.NewV7()).String(), administrator, map[string]any{"expected_revision_id": parent["revision_id"], "expected_cutoff_version": 1, "mode": "scheduled", "cutoff_at": shorter, "reason": "Far ancestor revision requires renewed descendant examination", "sources_examined": true}, 200)
	body := map[string]any{"expected_version": 1, "expected_revision_id": leaf["revision_id"], "decision": "approve", "reason": "Leaf reviewer examines every canonical ancestor cutoff", "sources_examined": true, "lifecycle_review": map[string]any{"cutoff_versions": []int{0, 0, 1}, "effective_until": shorter}}
	before := postingRootApprovalEffects(t, db, tenant, reviewer)
	call(t, "PUT", leafRoot+"/decisions/"+uuid.Must(uuid.NewV7()).String(), reviewer, body, 409)
	require.Equal(t, before, postingRootApprovalEffects(t, db, tenant, reviewer))
	renewed := context(leafRoot, leaf)
	require.Equal(t, shorter, renewed["effective_until"])
	body["lifecycle_review"] = map[string]any{"cutoff_versions": []int{0, 0, 2}, "effective_until": renewed["effective_until"]}
	assertPostingLifecycleApprovalFaults(t, call, db, tenant, leafRoot, reviewer, leaf, body)
	approved := call(t, "PUT", leafRoot+"/decisions/"+uuid.Must(uuid.NewV7()).String(), reviewer, body, 200)
	require.Equal(t, "approved", approved["status"])
	proof := readRetainedPostingLifecycleProof(t, db, tenant, tenant, leaf["hospital_id"].(string), leafStaff, leaf["id"].(string))
	require.Len(t, proof.Headers, 1)
	require.Len(t, proof.Sources, 3)
	require.EqualValues(t, 3, proof.Headers[0].SourceCount)
	require.EqualValues(t, 0, proof.Sources[1].CutoffVersion)
	require.EqualValues(t, 2, proof.Sources[2].CutoffVersion)
	require.Equal(t, parent["revision_id"], proof.Sources[2].SourceRevisionID.String())
	require.Equal(t, shorter, proof.Headers[0].EffectiveUntil.UTC().Format(time.RFC3339Nano))
}

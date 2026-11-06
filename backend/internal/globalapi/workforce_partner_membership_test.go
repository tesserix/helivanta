package globalapi

import (
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type partnerMembershipActors struct{ Maker, Checker, Preparer, Reviewer, Member, Unassigned string }

func assertHospitalPartnerMembership(t *testing.T, call func(*testing.T, string, string, string, any, int) map[string]any, source map[string]any, actors partnerMembershipActors) (string, map[string]any) {
	t.Helper()
	hospital := source["hospital_id"].(string)
	capabilitiesPath := "/v1/iam/hospitals/" + hospital + "/partner-member-capabilities"
	capabilities := call(t, "GET", capabilitiesPath, actors.Reviewer, nil, 200)
	require.Equal(t, hospital, capabilities["hospital_id"])
	require.Equal(t, false, capabilities["can_prepare"])
	require.Equal(t, true, capabilities["can_review"])
	call(t, "GET", capabilitiesPath, actors.Unassigned, nil, 403)

	groupID := uuid.Must(uuid.NewV7()).String()
	groupPath := "/v1/iam/partner-groups/" + groupID
	prepared := call(t, "PUT", groupPath, actors.Maker, map[string]any{"hospital_id": hospital, "name": "Synthetic independently reviewed receiving team", "purpose": "receiving", "reason": "Explicit hospital-owned recipient group; no supplier staff clone", "valid_from": time.Now().UTC().Add(-time.Hour), "expires_at": time.Now().UTC().Add(15 * time.Minute)}, 200)
	require.Equal(t, false, prepared["external_access_enabled"])
	var group map[string]any
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		group = call(t, "GET", groupPath, actors.Maker, nil, 200)
		assert.Equal(c, "pending_review", group["status"])
	}, 10*time.Second, 50*time.Millisecond)
	publication := call(t, "PUT", groupPath+"/commands/"+uuid.Must(uuid.NewV7()).String(), actors.Checker, map[string]any{"operation": "approve", "expected_version": group["version"], "reason": "Independent publication authority accepts exact hospital scope"}, 200)
	require.Equal(t, "approved", publication["group"].(map[string]any)["status"])
	memberBase := "/v1/iam/hospitals/" + hospital + "/partner-groups/" + groupID + "/member-proposals/"
	candidates := call(t, "GET", "/v1/iam/hospitals/"+hospital+"/partner-member-candidates?limit=50", actors.Preparer, nil, 200)
	found := false
	for _, candidate := range candidates["data"].([]any) {
		row := candidate.(map[string]any)
		require.NotEqual(t, actors.Preparer, row["subject"])
		require.Equal(t, hospital, row["hospital_id"])
		require.Equal(t, "candidate", row["source_state"])
		require.NotContains(t, row, "reason")
		if row["id"] == source["id"] {
			found = true
			require.Equal(t, actors.Member, row["subject"])
			require.Equal(t, source["staff_id"], row["staff_id"])
		}
	}
	require.True(t, found, "owner candidates must include the selected current approved posting")

	sourceEnd, err := time.Parse(time.RFC3339Nano, source["ends_at"].(string))
	require.NoError(t, err)
	require.True(t, sourceEnd.After(time.Now().Add(time.Minute)), "fixture must retain enough current source validity for review")
	input := map[string]any{"kind": "subject", "subject": actors.Member, "staff_id": source["staff_id"], "posting_id": source["id"], "posting_revision_id": source["revision_id"], "posting_version": source["version"], "department_id": source["department_id"], "department_revision_id": source["department_revision_id"], "expected_group_version": publication["group"].(map[string]any)["version"], "valid_from": time.Now().UTC(), "expires_at": sourceEnd.Add(-time.Second), "reason": "Named current reviewed hospital staff for receiving only", "sources_examined": true}
	call(t, "PUT", memberBase+uuid.Must(uuid.NewV7()).String(), actors.Member, input, 403)
	nominationPath := memberBase + uuid.Must(uuid.NewV7()).String()
	nomination := call(t, "PUT", nominationPath, actors.Preparer, input, 200)
	require.Equal(t, "pending_review", nomination["status"])
	require.Equal(t, actors.Member, nomination["input"].(map[string]any)["subject"])
	require.Equal(t, source["staff_id"], nomination["member_staff_id"])
	require.Equal(t, nomination, call(t, "PUT", nominationPath, actors.Preparer, input, 200))
	changed := make(map[string]any, len(input))
	for key, value := range input {
		changed[key] = value
	}
	changed["reason"] = "Another nomination intent"
	call(t, "PUT", nominationPath, actors.Preparer, changed, 409)
	call(t, "GET", memberBase+uuid.Must(uuid.NewV7()).String(), actors.Reviewer, nil, 404)

	call(t, "GET", nominationPath, actors.Unassigned, nil, 403)
	commandPath := nominationPath + "/commands/" + uuid.Must(uuid.NewV7()).String()
	decision := map[string]any{"operation": "approve", "expected_version": nomination["version"], "reason": "Different current Staff independently confirms exact account, posting and purpose", "sources_examined": true, "purpose_eligibility_confirmed": true}
	call(t, "PUT", commandPath, actors.Preparer, decision, 403)
	approved := call(t, "PUT", commandPath, actors.Reviewer, decision, 200)
	require.Equal(t, "approved", approved["member"].(map[string]any)["status"])
	require.Equal(t, approved, call(t, "PUT", commandPath, actors.Reviewer, decision, 200))
	require.Equal(t, approved, call(t, "GET", commandPath, actors.Reviewer, nil, 200))
	changedDecision := make(map[string]any, len(decision))
	for key, value := range decision {
		changedDecision[key] = value
	}
	changedDecision["reason"] = "Changed retained command intent"
	call(t, "PUT", commandPath, actors.Reviewer, changedDecision, 409)
	call(t, "PUT", nominationPath+"/commands/"+uuid.Must(uuid.NewV7()).String(), actors.Reviewer, decision, 409)

	current := call(t, "GET", nominationPath, actors.Reviewer, nil, 200)
	require.Equal(t, "approved", current["status"])
	page := call(t, "GET", memberBase[:len(memberBase)-1]+"?limit=1", actors.Reviewer, nil, 200)
	require.Len(t, page["data"].([]any), 1)
	require.Equal(t, nomination["id"], page["data"].([]any)[0].(map[string]any)["id"])
	return groupID, current

}

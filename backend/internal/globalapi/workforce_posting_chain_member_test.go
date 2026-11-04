package globalapi

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func assertPostingDepartmentMember(t *testing.T, call func(*testing.T, string, string, string, any, int) map[string]any, hospitalBase, reviewer, memberSubject, memberStaff, organizationMaker string, supervisor map[string]any) {
	t.Helper()
	require.NotEmpty(t, memberStaff)
	hospital := supervisor["hospital_id"].(string)
	department := uuid.Must(uuid.NewV7()).String()
	organizationBase := "/v1/organization/hospitals/" + hospital
	departmentInput := map[string]any{"expected_version": 0, "code": "SYN-CHAIN-MEMBER", "name": "Synthetic descendant posting department", "kind": "sub_unit", "parent_id": supervisor["department_id"], "status": "active", "reason": "Synthetic current descendant for a canonical Staff member"}
	declaration := call(t, "PUT", organizationBase+"/departments/"+department+"/revisions/"+uuid.Must(uuid.NewV7()).String(), organizationMaker, departmentInput, 200)
	t.Cleanup(func() {
		departmentInput["expected_version"], departmentInput["status"] = 1, "retired"
		call(t, "PUT", organizationBase+"/departments/"+department+"/revisions/"+uuid.Must(uuid.NewV7()).String(), organizationMaker, departmentInput, 200)
	})
	base := hospitalBase + "/posting-staff/" + memberStaff
	source := call(t, "GET", base+"/posting-review-source", memberSubject, nil, 200)
	require.Equal(t, "current", source["generation_state"])
	draft := uuid.Must(uuid.NewV7()).String()
	receipt := call(t, "PUT", base+"/posting-drafts/"+draft+"/commands/"+uuid.Must(uuid.NewV7()).String(), memberSubject, map[string]any{"expected_version": 0, "staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "department_id": department, "department_revision_id": declaration["revision_id"], "starts_at": supervisor["starts_at"], "ends_at": supervisor["ends_at"], "reason": "Synthetic existing Staff member in a supervised descendant department"}, 200)
	root := base + "/posting-proposals/" + uuid.Must(uuid.NewV7()).String()
	pending := call(t, "PUT", root+"/commands/"+uuid.Must(uuid.NewV7()).String(), memberSubject, map[string]any{"expected_version": 0, "draft_id": draft, "draft_revision_id": receipt["revision_id"], "draft_version": receipt["version"], "generation_id": source["generation_id"], "staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "reason": "Synthetic departmental member under the exact approved supervisor", "sources_examined": true, "supervision": map[string]any{"kind": "department_member", "supervisor_staff_id": supervisor["staff_id"], "supervisor_proposal_id": supervisor["id"], "supervisor_revision_id": supervisor["revision_id"]}}, 200)
	path := root + "/decisions/" + uuid.Must(uuid.NewV7()).String()
	decision := map[string]any{"expected_version": 1, "expected_revision_id": pending["revision_id"], "decision": "approve", "reason": "Independent existing Staff appointment through a complete root and departmental supervisor", "sources_examined": true}
	approved := call(t, "PUT", path, reviewer, decision, 200)
	require.Equal(t, "approved", approved["status"])
	require.Equal(t, memberStaff, approved["staff_id"])
	require.Equal(t, department, approved["department_id"])
	require.Equal(t, false, approved["grants_access"])
	require.Equal(t, approved, call(t, "PUT", path, reviewer, decision, 200))
	require.Equal(t, approved, call(t, "GET", path, reviewer, nil, 200))
	require.Equal(t, []any{approved, pending}, call(t, "GET", root+"/history?limit=2", reviewer, nil, 200)["data"])
}

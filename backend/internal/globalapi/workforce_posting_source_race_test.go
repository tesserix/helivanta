package globalapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func assertPostingSourceUseCutoffRaces(t *testing.T, db *tenantdb.DB, tenant string, call func(*testing.T, string, string, string, any, int) map[string]any, request postingTestRequest, prepare postingTestPreparer, rootTemplate, childTemplate map[string]any, submitter, reviewer, administrator string, use postingSourceUseProbe) {
	t.Helper()
	for _, useFirst := range []bool{true, false} {
		root, pending := prepare(rootTemplate, reviewer, map[string]any{"kind": "hospital_management"})
		parent := call(t, "PUT", root+"/decisions/"+uuid.Must(uuid.NewV7()).String(), administrator, map[string]any{"expected_version": 1, "expected_revision_id": pending["revision_id"], "decision": "approve", "reason": "Synthetic guarded-consumer race root", "sources_examined": true, "hospital_management_reviewed": true}, 200)
		childRoot, child := prepare(childTemplate, submitter, map[string]any{"kind": "department_member", "supervisor_staff_id": parent["staff_id"], "supervisor_proposal_id": parent["id"], "supervisor_revision_id": parent["revision_id"]})
		view := call(t, "GET", childRoot+"/context?purpose=review&expected_version=1&expected_revision_id="+child["revision_id"].(string)+"&supervision_context=v1&lifecycle_review=current", reviewer, nil, 200)
		approved := call(t, "PUT", childRoot+"/decisions/"+uuid.Must(uuid.NewV7()).String(), reviewer, map[string]any{"expected_version": 1, "expected_revision_id": child["revision_id"], "decision": "approve", "reason": "Synthetic source use race placement", "sources_examined": true, "lifecycle_review": map[string]any{"cutoff_versions": []int{0, 0}, "effective_until": view["effective_until"]}}, 200)
		body, err := json.Marshal(map[string]any{"expected_revision_id": parent["revision_id"], "expected_cutoff_version": 0, "mode": "immediate", "reason": "Synthetic source ending races guarded consumption", "sources_examined": true})
		require.NoError(t, err)
		path := root + "/cutoffs/commands/" + uuid.Must(uuid.NewV7()).String()
		consume := func(ctx context.Context) *httptest.ResponseRecorder {
			result := httptest.NewRecorder()
			code := 200
			if e := use(ctx, t, submitter, approved, func(context.Context, authz.StaffPostingSourceReference) error { return nil }); e != nil {
				code = 503
				if errors.Is(e, authz.ErrScopedCommandDenied) {
					code = 403
				}
			}
			result.WriteHeader(code)
			return result
		}
		ending := func(ctx context.Context) *httptest.ResponseRecorder {
			return request(ctx, "PUT", path, administrator, body, "application/json")
		}
		first, second := ending, consume
		if useFirst {
			first, second = consume, ending
		}
		before := postingSourceUseEffects(t, db, tenant, submitter)
		results := racePostingLifecycleCommands(t, db, tenant, parent["hospital_id"].(string), first, second)
		require.Equal(t, 200, results[0].Code, results[0].Body.String())
		after := postingSourceUseEffects(t, db, tenant, submitter)
		if useFirst {
			require.Equal(t, 200, results[1].Code, results[1].Body.String())
			require.Equal(t, [2]int64{before[0] + 1, before[1] + 1}, after)
		} else {
			require.Equal(t, 403, results[1].Code, results[1].Body.String())
			require.Equal(t, before, after)
		}
	}
}

package globalapi

import (
	"context"
	"fmt"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"strings"
	"testing"
	"time"
)

func postingLifecycleHistoryPath(root string, approved map[string]any) string {
	return fmt.Sprintf("%s/context?purpose=review&expected_version=%v&expected_revision_id=%s&supervision_context=v1&lifecycle_review=recorded", root, approved["version"], approved["revision_id"])
}

func assertPostingLifecycleHistory(t *testing.T, call func(*testing.T, string, string, string, any, int) map[string]any, root, reader string, approved map[string]any, proof retainedPostingLifecycleProof) map[string]any {
	t.Helper()
	out := call(t, "GET", postingLifecycleHistoryPath(root, approved), reader, nil, 200)
	require.Equal(t, approved, out["context"].(map[string]any)["proposal"])
	record := out["review"].(map[string]any)
	header := proof.Headers[0]
	require.Equal(t, approved["revision_id"], record["revision_id"])
	require.Equal(t, header.InitialRevisionID.String(), record["initial_revision_id"])
	require.Equal(t, header.ReviewerStaffID.String(), record["reviewer_staff_id"])
	require.Equal(t, header.ReviewerLinkID.String(), record["reviewer_link_id"])
	require.Equal(t, header.ReviewerGenerationID.String(), record["reviewer_generation_id"])
	require.Equal(t, header.ReviewAssignmentID.String(), record["review_assignment_id"])
	require.EqualValues(t, header.ReviewAssignmentVersion, record["review_assignment_version"])
	require.Equal(t, header.SupportPermission, record["support_permission"])
	require.Equal(t, header.SupportAssignmentID.String(), record["support_assignment_id"])
	require.EqualValues(t, header.SupportAssignmentVersion, record["support_assignment_version"])
	require.Equal(t, header.CheckedAt.UTC().Format(time.RFC3339Nano), record["checked_at"])
	require.Equal(t, header.EffectiveUntil.UTC().Format(time.RFC3339Nano), record["effective_until"])
	require.NotContains(t, record, "reviewer_subject")
	sources := record["sources"].([]any)
	require.EqualValues(t, len(proof.Sources), record["source_count"])
	require.Len(t, sources, len(proof.Sources))
	for index, expected := range proof.Sources {
		source := sources[index].(map[string]any)
		require.EqualValues(t, expected.Hop, source["hop"])
		require.Equal(t, expected.SourceStaffID.String(), source["staff_id"])
		require.Equal(t, expected.SourceProposalID.String(), source["proposal_id"])
		require.Equal(t, expected.SourceRevisionID.String(), source["revision_id"])
		cutoff := source["cutoff"].(map[string]any)
		require.EqualValues(t, expected.CutoffVersion, cutoff["version"])
		if expected.CutoffID == nil {
			require.Nil(t, cutoff["id"])
			require.Nil(t, cutoff["cutoff_at"])
		} else {
			require.Equal(t, expected.CutoffID.String(), cutoff["id"])
			require.Equal(t, expected.CutoffAt.UTC().Format(time.RFC3339Nano), cutoff["cutoff_at"])
		}
	}
	return out
}

func assertPostingLifecycleHistoryFailures(t *testing.T, db *tenantdb.DB, tenant string, call func(*testing.T, string, string, string, any, int) map[string]any, root, reader, contributor string, approved map[string]any, proof retainedPostingLifecycleProof) {
	t.Helper()
	path := postingLifecycleHistoryPath(root, approved)
	call(t, "GET", path, contributor, nil, 403)
	call(t, "GET", strings.Replace(path, approved["hospital_id"].(string), uuid.Must(uuid.NewV7()).String(), 1), reader, nil, 403)
	call(t, "GET", strings.Replace(path, approved["staff_id"].(string), uuid.Must(uuid.NewV7()).String(), 1), reader, nil, 404)
	stale := map[string]any{"version": 1, "revision_id": proof.Headers[0].InitialRevisionID.String()}
	call(t, "GET", postingLifecycleHistoryPath(root, stale), reader, nil, 409)
	apply := func(sql string) {
		t.Helper()
		require.NoError(t, db.Migrate(context.WithoutCancel(t.Context()), []tenantdb.Migration{{ID: "synthetic_lifecycle_history_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
	}
	clear := func() {
		apply(`ALTER TABLE workforce_audit DROP CONSTRAINT IF EXISTS synthetic_lifecycle_history_audit;
 DROP POLICY IF EXISTS synthetic_lifecycle_history_sources ON workforce_posting_lifecycle_sources;`)
	}
	t.Cleanup(clear)
	for _, fault := range []struct{ name, sql string }{
		{"read audit", `ALTER TABLE workforce_audit ADD CONSTRAINT synthetic_lifecycle_history_audit CHECK(action<>'workforce.posting_proposal.context_read_lifecycle_history') NOT VALID`},
		{"incomplete retained source visibility", `CREATE POLICY synthetic_lifecycle_history_sources ON workforce_posting_lifecycle_sources AS RESTRICTIVE FOR SELECT USING(hop=0)`},
	} {
		before := postingRootApprovalEffects(t, db, tenant, reader)
		apply(fault.sql)
		failed := call(t, "GET", path, reader, nil, 500)
		require.NotContains(t, failed, "context", fault.name)
		require.NotContains(t, failed, "review", fault.name)
		require.Equal(t, before, postingRootApprovalEffects(t, db, tenant, reader), "failed history reads also roll back actor-use evidence")
		clear()
		assertPostingLifecycleHistory(t, call, root, reader, approved, proof)
	}
}

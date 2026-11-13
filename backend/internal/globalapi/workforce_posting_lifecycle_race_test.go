package globalapi

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type postingTestRequest func(context.Context, string, string, string, []byte, string) *httptest.ResponseRecorder

type postingTestPreparer func(map[string]any, string, map[string]any) (string, map[string]any)

func assertPostingLifecycleCutoffRaces(t *testing.T, db *tenantdb.DB, tenant string, call func(*testing.T, string, string, string, any, int) map[string]any, request postingTestRequest, prepare postingTestPreparer, rootTemplate, childTemplate map[string]any, submitter, reviewer, administrator string) {
	t.Helper()
	for _, approvalFirst := range []bool{true, false} {
		t.Logf("Racing actual cutoff and lifecycle approval; approval queued first: %v", approvalFirst)
		root, pending := prepare(rootTemplate, reviewer, map[string]any{"kind": "hospital_management"})
		parent := call(t, "PUT", root+"/decisions/"+uuid.Must(uuid.NewV7()).String(), administrator, map[string]any{"expected_version": 1, "expected_revision_id": pending["revision_id"], "decision": "approve", "reason": "Legacy root bridges into guarded lifecycle approval", "sources_examined": true, "hospital_management_reviewed": true}, 200)
		childRoot, child := prepare(childTemplate, submitter, map[string]any{"kind": "department_supervisor", "supervisor_staff_id": parent["staff_id"], "supervisor_proposal_id": parent["id"], "supervisor_revision_id": parent["revision_id"]})
		view := call(t, "GET", childRoot+"/context?purpose=review&expected_version=1&expected_revision_id="+child["revision_id"].(string)+"&supervision_context=v1&lifecycle_review=current", reviewer, nil, 200)
		until, err := time.Parse(time.RFC3339Nano, view["effective_until"].(string))
		require.NoError(t, err)
		cutoffAt := until.Add(-time.Minute).UTC()
		require.True(t, cutoffAt.After(time.Now()), "the race must exercise an eligible, changed deadline")
		approveBody, err := json.Marshal(map[string]any{"expected_version": 1, "expected_revision_id": child["revision_id"], "decision": "approve", "reason": "Concurrent approval binds the examined absence and nominal deadline", "sources_examined": true, "lifecycle_review": map[string]any{"cutoff_versions": []int{0, 0}, "effective_until": view["effective_until"]}})
		require.NoError(t, err)
		cutoffBody, err := json.Marshal(map[string]any{"expected_revision_id": parent["revision_id"], "expected_cutoff_version": 0, "mode": "scheduled", "cutoff_at": cutoffAt.Format(time.RFC3339Nano), "reason": "Concurrent ending shortens a canonical source without rewriting its approval", "sources_examined": true})
		require.NoError(t, err)
		approvePath := childRoot + "/decisions/" + uuid.Must(uuid.NewV7()).String()
		cutoffPath := root + "/cutoffs/commands/" + uuid.Must(uuid.NewV7()).String()
		approve := func(ctx context.Context) *httptest.ResponseRecorder {
			return request(ctx, "PUT", approvePath, reviewer, approveBody, "application/json")
		}
		cutoff := func(ctx context.Context) *httptest.ResponseRecorder {
			return request(ctx, "PUT", cutoffPath, administrator, cutoffBody, "application/json")
		}
		first, second := cutoff, approve
		if approvalFirst {
			first, second = approve, cutoff
		}
		before := postingRootApprovalEffects(t, db, tenant, reviewer)
		responses := racePostingLifecycleCommands(t, db, tenant, parent["hospital_id"].(string), first, second)
		require.Equal(t, 200, responses[0].Code, responses[0].Body.String())
		proof := readRetainedPostingLifecycleProof(t, db, tenant, tenant, child["hospital_id"].(string), child["staff_id"].(string), child["id"].(string))
		if approvalFirst {
			require.Equal(t, 200, responses[1].Code, responses[1].Body.String())
			require.Len(t, proof.Headers, 1)
			require.Len(t, proof.Sources, 2)
			require.Zero(t, proof.Sources[1].CutoffVersion, "the first committed approval retains the observed absence")
			require.True(t, proof.Headers[0].EffectiveUntil.Equal(until))
			require.Equal(t, "approved", call(t, "GET", childRoot, reviewer, nil, 200)["status"])
		} else {
			require.Equal(t, 409, responses[1].Code, responses[1].Body.String())
			require.Empty(t, proof.Headers)
			require.Empty(t, proof.Sources)
			require.Equal(t, before, postingRootApprovalEffects(t, db, tenant, reviewer))
			require.Equal(t, child, call(t, "GET", childRoot, reviewer, nil, 200))
			call(t, "GET", approvePath, reviewer, nil, 404)
		}
	}
}

func assertPostingLifecycleExpiredReplay(t *testing.T, db *tenantdb.DB, tenant string, call func(*testing.T, string, string, string, any, int) map[string]any, prepare postingTestPreparer, rootTemplate, childTemplate map[string]any, submitter, reviewer, administrator string) {
	t.Helper()
	root, pending := prepare(rootTemplate, reviewer, map[string]any{"kind": "hospital_management"})
	parent := call(t, "PUT", root+"/decisions/"+uuid.Must(uuid.NewV7()).String(), administrator, map[string]any{"expected_version": 1, "expected_revision_id": pending["revision_id"], "decision": "approve", "reason": "Independent nominal root for committed-expiry recovery", "sources_examined": true, "hospital_management_reviewed": true}, 200)
	until := time.Now().UTC().Add(12 * time.Second).Truncate(time.Microsecond)
	short := map[string]any{}
	for key, value := range childTemplate {
		short[key] = value
	}
	short["ends_at"] = until.Format(time.RFC3339Nano)
	propose := func() (string, map[string]any, map[string]any) {
		childRoot, child := prepare(short, submitter, map[string]any{"kind": "department_supervisor", "supervisor_staff_id": parent["staff_id"], "supervisor_proposal_id": parent["id"], "supervisor_revision_id": parent["revision_id"]})
		view := call(t, "GET", childRoot+"/context?purpose=review&expected_version=1&expected_revision_id="+child["revision_id"].(string)+"&supervision_context=v1&lifecycle_review=current", reviewer, nil, 200)
		require.Equal(t, until.Format(time.RFC3339Nano), view["effective_until"])
		body := map[string]any{"expected_version": 1, "expected_revision_id": child["revision_id"], "decision": "approve", "reason": "Synthetic short-lived posting retains its original approval command", "sources_examined": true, "lifecycle_review": map[string]any{"cutoff_versions": []int{0, 0}, "effective_until": view["effective_until"]}}
		return childRoot, child, body
	}
	committedRoot, committedPending, committedBody := propose()
	uncommittedRoot, uncommittedPending, uncommittedBody := propose()
	command := committedRoot + "/decisions/" + uuid.Must(uuid.NewV7()).String()
	receipt := call(t, "PUT", command, reviewer, committedBody, 200)
	if remaining := time.Until(until.Add(10 * time.Millisecond)); remaining > 0 {
		timer := time.NewTimer(remaining)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-t.Context().Done():
			t.Fatal("expiry recovery test was cancelled")
		}
	}
	beforeReplay := postingRootApprovalEffects(t, db, tenant, reviewer)
	require.Equal(t, receipt, call(t, "PUT", command, reviewer, committedBody, 200), "a committed command survives its original deadline")
	afterReplay := postingRootApprovalEffects(t, db, tenant, reviewer)
	require.Equal(t, beforeReplay[1:], afterReplay[1:])
	proof := readRetainedPostingLifecycleProof(t, db, tenant, tenant, committedPending["hospital_id"].(string), committedPending["staff_id"].(string), committedPending["id"].(string))
	require.Len(t, proof.Headers, 1)
	require.True(t, proof.Headers[0].EffectiveUntil.Equal(until))
	uncommittedCommand := uncommittedRoot + "/decisions/" + uuid.Must(uuid.NewV7()).String()
	beforeExpired := postingRootApprovalEffects(t, db, tenant, reviewer)
	call(t, "PUT", uncommittedCommand, reviewer, uncommittedBody, 409)
	require.Equal(t, beforeExpired, postingRootApprovalEffects(t, db, tenant, reviewer))
	require.Equal(t, uncommittedPending, call(t, "GET", uncommittedRoot, reviewer, nil, 200))
	call(t, "GET", uncommittedCommand, reviewer, nil, 404)
	proof = readRetainedPostingLifecycleProof(t, db, tenant, tenant, uncommittedPending["hospital_id"].(string), uncommittedPending["staff_id"].(string), uncommittedPending["id"].(string))
	require.Empty(t, proof.Headers)
	require.Empty(t, proof.Sources)
	rejected := call(t, "PUT", uncommittedRoot+"/decisions/"+uuid.Must(uuid.NewV7()).String(), reviewer, map[string]any{"expected_version": 1, "expected_revision_id": uncommittedPending["revision_id"], "decision": "reject", "reason": "Expired uncommitted approval can still be independently rejected", "sources_examined": true}, 200)
	require.Equal(t, "rejected", rejected["status"])
}

func racePostingLifecycleCommands(t *testing.T, db *tenantdb.DB, tenant, hospital string, first, second func(context.Context) *httptest.ResponseRecorder) [2]*httptest.ResponseRecorder {
	t.Helper()
	started := time.Now()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	held, unlock := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(unlock) }) }
	defer release()
	locked := make(chan error, 1)
	go func() {
		locked <- db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT workforce_lock_supervision(?::uuid,?::uuid)`, tenant, hospital).Error; err != nil {
				return err
			}
			close(held)
			select {
			case <-unlock:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-held:
	case err := <-locked:
		t.Fatalf("could not hold the real supervision gate: %v", err)
	case <-ctx.Done():
		t.Fatal("supervision gate did not become available")
	}
	responses := [2]chan *httptest.ResponseRecorder{make(chan *httptest.ResponseRecorder, 1), make(chan *httptest.ResponseRecorder, 1)}
	waiters := func(expected int) {
		t.Helper()
		deadline := time.NewTimer(3 * time.Second)
		defer deadline.Stop()
		for {
			var waiting int64
			err := db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
				return tx.Raw(`SELECT count(*) FROM pg_locks WHERE NOT granted AND database=(SELECT oid FROM pg_database WHERE datname=current_database())`).Scan(&waiting).Error
			})
			require.NoError(t, err)
			if waiting >= int64(expected) {
				return
			}
			select {
			case <-time.After(5 * time.Millisecond):
			case <-deadline.C:
				t.Fatalf("only %d of %d commands reached a real PostgreSQL lock wait", waiting, expected)
			case <-ctx.Done():
				t.Fatal("concurrent command budget elapsed")
			}
		}
	}
	go func() { responses[0] <- first(ctx) }()
	waiters(1)
	t.Logf("First lifecycle command reached a PostgreSQL lock wait after %s", time.Since(started))
	go func() { responses[1] <- second(ctx) }()
	waiters(2)
	t.Logf("Both lifecycle commands reached PostgreSQL lock waits after %s; releasing supervision gate", time.Since(started))
	release()
	var result [2]*httptest.ResponseRecorder
	for index := range result {
		select {
		case result[index] = <-responses[index]:
			t.Logf("Lifecycle command %d returned status %d after %s", index, result[index].Code, time.Since(started))
		case <-ctx.Done():
			t.Fatal("serialized lifecycle commands did not finish")
		}
	}
	select {
	case err := <-locked:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("supervision gate holder did not exit")
	}
	return result
}

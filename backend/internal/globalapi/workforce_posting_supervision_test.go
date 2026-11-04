package globalapi

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func assertPostingSupervisionRetention(t *testing.T, ctx context.Context, db *tenantdb.DB, call func(*testing.T, string, string, string, any, int) map[string]any, base, actor, administrator, draft string, saved, source, generation map[string]any) {
	t.Helper()
	proposal, command := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	root := base + "/posting-proposals/" + proposal
	path := root + "/commands/" + command
	intent := map[string]any{"kind": "hospital_management"}
	input := map[string]any{"expected_version": 0, "draft_id": draft, "draft_revision_id": saved["revision_id"], "draft_version": saved["version"], "generation_id": generation["id"], "staff_version": source["staff_version"], "employment_epoch": source["employment_epoch"], "reason": "Synthetic explicit management appointment proposal", "sources_examined": true, "supervision": intent}
	pending := call(t, "PUT", path, actor, input, 200)
	require.Equal(t, "pending", pending["status"])
	require.Equal(t, false, pending["grants_access"])
	selector := fmt.Sprintf("?purpose=submission&expected_version=1&expected_revision_id=%s", pending["revision_id"])
	legacy := call(t, "GET", root+"/context"+selector, actor, nil, 200)
	require.NotContains(t, legacy, "supervision")
	view := call(t, "GET", root+"/context"+selector+"&supervision_context=v1", actor, nil, 200)
	require.Len(t, view, 2)
	require.Equal(t, legacy, view["context"])
	require.Equal(t, intent, view["supervision"], "accepted intent must survive independently of the submitted HTTP body")
	require.Equal(t, pending, call(t, "PUT", path, actor, input, 200))
	require.Equal(t, pending, call(t, "GET", path, actor, nil, 200))
	changed := maps.Clone(input)
	changed["supervision"] = map[string]any{"kind": "department_member", "supervisor_staff_id": pending["staff_id"], "supervisor_proposal_id": uuid.Must(uuid.NewV7()).String(), "supervisor_revision_id": uuid.Must(uuid.NewV7()).String()}
	call(t, "PUT", path, actor, changed, 409)
	for _, flag := range []string{"", "v2", "v1&supervision_context=v1"} {
		denied := call(t, "GET", root+"/context"+selector+"&supervision_context="+flag, actor, nil, 400)
		require.NotContains(t, denied, "supervision")
	}
	call(t, "GET", root+"/context"+selector+"&supervision_context=v1", administrator, nil, 403)
	foreign := strings.Replace(root, "/posting-staff/"+pending["staff_id"].(string)+"/", "/posting-staff/"+uuid.NewString()+"/", 1)
	call(t, "GET", foreign+"/context"+selector+"&supervision_context=v1", actor, nil, 404)
	withdrawn := call(t, "PUT", root+"/decisions/"+uuid.Must(uuid.NewV7()).String(), actor, map[string]any{"expected_version": 1, "expected_revision_id": pending["revision_id"], "decision": "withdraw", "reason": "Synthetic retained management intent withdrawal", "sources_examined": true}, 200)
	after := fmt.Sprintf("?purpose=submission&expected_version=2&expected_revision_id=%s&supervision_context=v1", withdrawn["revision_id"])
	require.Equal(t, intent, call(t, "GET", root+"/context"+after, actor, nil, 200)["supervision"], "withdrawal preserves the original typed intent")
	// Simulate a lost owner write; the deferred constraint must roll the entire command back.
	faultID := "test_supervision_lost_" + uuid.Must(uuid.NewV7()).String()
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: faultID, SQL: `CREATE FUNCTION test_lost_posting_supervision() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL; END $$; CREATE TRIGGER test_lost_posting_supervision BEFORE INSERT ON workforce_posting_supervision_intents FOR EACH ROW EXECUTE FUNCTION test_lost_posting_supervision()`}}))
	clear := func() {
		require.NoError(t, db.Migrate(context.WithoutCancel(ctx), []tenantdb.Migration{{ID: faultID + "_clear", SQL: `DROP FUNCTION IF EXISTS test_lost_posting_supervision() CASCADE`}}))
	}
	t.Cleanup(clear)
	nextRoot := base + "/posting-proposals/" + uuid.Must(uuid.NewV7()).String()
	nextPath := nextRoot + "/commands/" + uuid.Must(uuid.NewV7()).String()
	call(t, "PUT", nextPath, actor, input, 500)
	call(t, "GET", nextPath, actor, nil, 404)
	call(t, "GET", nextRoot, actor, nil, 404)
	clear()
	recovered := call(t, "PUT", nextPath, actor, input, 200)
	require.Equal(t, recovered, call(t, "PUT", nextPath, actor, input, 200))
	recoveredSelector := fmt.Sprintf("?purpose=submission&expected_version=1&expected_revision_id=%s&supervision_context=v1", recovered["revision_id"])
	require.Equal(t, intent, call(t, "GET", nextRoot+"/context"+recoveredSelector, actor, nil, 200)["supervision"])
	call(t, "PUT", nextRoot+"/decisions/"+uuid.Must(uuid.NewV7()).String(), actor, map[string]any{"expected_version": 1, "expected_revision_id": recovered["revision_id"], "decision": "withdraw", "reason": "Synthetic recovered management submission withdrawal", "sources_examined": true}, 200)

	legacyInput := maps.Clone(input)
	delete(legacyInput, "supervision")
	legacyRoot := base + "/posting-proposals/" + uuid.Must(uuid.NewV7()).String()
	legacyPending := call(t, "PUT", legacyRoot+"/commands/"+uuid.Must(uuid.NewV7()).String(), actor, legacyInput, 200)
	legacySelector := fmt.Sprintf("?purpose=submission&expected_version=1&expected_revision_id=%s&supervision_context=v1", legacyPending["revision_id"])
	legacyView := call(t, "GET", legacyRoot+"/context"+legacySelector, actor, nil, 200)
	require.Contains(t, legacyView, "supervision")
	require.Nil(t, legacyView["supervision"], "legacy omission is explicit and cannot imply hospital management")

	legacyWithdrawn := call(t, "PUT", legacyRoot+"/decisions/"+uuid.Must(uuid.NewV7()).String(), actor, map[string]any{"expected_version": 1, "expected_revision_id": legacyPending["revision_id"], "decision": "withdraw", "reason": "Synthetic legacy submission withdrawal", "sources_examined": true}, 200)
	for _, fault := range []struct{ name, statement string }{
		{"historical_backfill", fmt.Sprintf(`IF pg_trigger_depth()=1 THEN INSERT INTO workforce_posting_supervision_intents(tenant_id,hospital_id,staff_id,proposal_id,initial_revision_id,kind) VALUES(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,'%s','%s','hospital_management'); END IF;`, legacyPending["id"], legacyPending["revision_id"])},
		{"historical_rewrite", `UPDATE workforce_posting_supervision_intents SET kind='hospital_management' WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND staff_id=NEW.staff_id;`},
	} {
		t.Run(fault.name, func(t *testing.T) {
			faultID := "test_supervision_" + fault.name + "_" + uuid.Must(uuid.NewV7()).String()
			install := `CREATE FUNCTION test_illegal_posting_supervision() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN ` + fault.statement + ` RETURN NEW; END $$; CREATE TRIGGER test_illegal_posting_supervision BEFORE INSERT ON workforce_posting_supervision_intents FOR EACH ROW EXECUTE FUNCTION test_illegal_posting_supervision()`
			require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: faultID, SQL: install}}))
			t.Cleanup(func() {
				require.NoError(t, db.Migrate(context.WithoutCancel(ctx), []tenantdb.Migration{{ID: faultID + "_clear", SQL: `DROP FUNCTION IF EXISTS test_illegal_posting_supervision() CASCADE`}}))
			})
			attemptRoot := base + "/posting-proposals/" + uuid.Must(uuid.NewV7()).String()
			attemptPath := attemptRoot + "/commands/" + uuid.Must(uuid.NewV7()).String()
			call(t, "PUT", attemptPath, actor, input, 500)
			call(t, "GET", attemptPath, actor, nil, 404)
			selected := fmt.Sprintf("?purpose=submission&expected_version=2&expected_revision_id=%s&supervision_context=v1", legacyWithdrawn["revision_id"])
			require.Nil(t, call(t, "GET", legacyRoot+"/context"+selected, actor, nil, 200)["supervision"])
		})
	}

}

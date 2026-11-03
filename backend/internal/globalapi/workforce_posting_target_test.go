package globalapi

import (
	"context"
	"fmt"
	"maps"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func assertPostingNonDigitalTarget(t *testing.T, db *tenantdb.DB, tenant string, call func(*testing.T, string, string, string, any, int) map[string]any, base, hospital, staff, manager, admin, actor, reviewer string, generation map[string]any, authorizationUnavailable *atomic.Bool, queueReviewer string) {
	t.Helper()
	staffBase := base + "/staff/" + staff
	postingBase := base + "/posting-staff/" + staff
	require.Equal(t, "non_digital", generation["access_intent"])
	require.Empty(t, generation["declared_subject"])
	require.Empty(t, call(t, "GET", staffBase+"/login-links", admin, nil, 200)["data"])
	ready := call(t, "GET", postingBase+"/posting-review-source", actor, nil, 200)
	require.Equal(t, "current", ready["generation_state"])
	require.Equal(t, generation["id"], ready["generation_id"])
	require.Equal(t, staff, ready["staff_id"])
	call(t, "GET", postingBase+"/posting-review-source", admin, nil, 403)

	department, revision := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	organizationBase := "/v1/organization/hospitals/" + hospital
	departmentInput := map[string]any{"expected_version": 0, "code": "SYN-NON-DIGITAL", "name": "Synthetic non-digital worker department", "kind": "department", "parent_id": nil, "status": "active", "reason": "Synthetic canonical placement target"}
	call(t, "PUT", organizationBase+"/departments/"+department+"/revisions/"+revision, admin, departmentInput, 200)
	t.Cleanup(func() {
		departmentInput["expected_version"], departmentInput["status"] = 1, "retired"
		call(t, "PUT", organizationBase+"/departments/"+department+"/revisions/"+uuid.Must(uuid.NewV7()).String(), admin, departmentInput, 200)
	})
	draft := uuid.Must(uuid.NewV7()).String()
	draftBase := postingBase + "/posting-drafts/" + draft
	intent := map[string]any{"expected_version": 0, "staff_version": ready["staff_version"], "employment_epoch": ready["employment_epoch"], "department_id": department, "department_revision_id": revision, "starts_at": time.Now().UTC().Format(time.RFC3339), "ends_at": ready["ends_at"], "reason": "Synthetic separate non-digital worker placement"}
	saved := call(t, "PUT", draftBase+"/commands/"+uuid.Must(uuid.NewV7()).String(), actor, intent, 200)
	proposal := postingBase + "/posting-proposals/" + uuid.Must(uuid.NewV7()).String()
	command := proposal + "/commands/" + uuid.Must(uuid.NewV7()).String()
	input := map[string]any{"expected_version": 0, "draft_id": draft, "draft_revision_id": saved["revision_id"], "draft_version": saved["version"], "generation_id": generation["id"], "staff_version": ready["staff_version"], "employment_epoch": ready["employment_epoch"], "reason": "Synthetic reviewed non-digital target submission", "sources_examined": true}
	wrongGeneration := maps.Clone(input)
	wrongGeneration["generation_id"] = uuid.Must(uuid.NewV7()).String()
	call(t, "PUT", command, actor, wrongGeneration, 404)
	pending := call(t, "PUT", command, actor, input, 200)
	require.Equal(t, staff, pending["staff_id"])
	require.Equal(t, department, pending["department_id"])
	require.Equal(t, generation["id"], pending["generation_id"])
	require.Equal(t, "pending", pending["status"])
	require.Equal(t, false, pending["grants_access"])
	queuePath := base + "/posting-proposal-inbox?status=pending&limit=1"
	queue := call(t, "GET", queuePath, queueReviewer, nil, 200)
	queueRows := queue["data"].([]any)
	require.Len(t, queueRows, 1)
	queueRow := queueRows[0].(map[string]any)
	require.Equal(t, pending, queueRow["proposal"])
	currentStaff := call(t, "GET", staffBase, admin, nil, 200)
	require.Equal(t, map[string]any{"id": staff, "employee_code": currentStaff["employee_code"], "display_name": currentStaff["display_name"]}, queueRow["staff"])
	require.Equal(t, false, queueRow["is_submitter"])
	require.Equal(t, true, queue["page"].(map[string]any)["has_more"])
	cursor := queue["page"].(map[string]any)["next_cursor"].(string)
	next := call(t, "GET", queuePath+"&cursor="+cursor, queueReviewer, nil, 200)["data"].([]any)
	require.Len(t, next, 1)
	require.NotEqual(t, pending["id"], next[0].(map[string]any)["proposal"].(map[string]any)["id"])
	call(t, "GET", strings.Replace(queuePath, "status=pending", "status=rejected", 1)+"&cursor="+cursor, queueReviewer, nil, 400)
	call(t, "GET", queuePath+"&cursor="+cursor, actor, nil, 400)
	call(t, "GET", base+"/posting-proposal-inbox?limit=9", queueReviewer, nil, 400)
	call(t, "GET", queuePath, admin, nil, 403)
	call(t, "PUT", draftBase+"/commands/"+uuid.Must(uuid.NewV7()).String(), queueReviewer, map[string]any{}, 403)
	ownedQueue := call(t, "GET", queuePath, actor, nil, 200)["data"].([]any)
	require.Equal(t, true, ownedQueue[0].(map[string]any)["is_submitter"])
	assertPostingInboxProtection(t, db, tenant, hospital, staff, pending["id"].(string), queueReviewer, admin, queuePath, call, authorizationUnavailable)
	require.Equal(t, pending, call(t, "PUT", command, actor, input, 200))
	contextQuery := "?purpose=review&expected_version=1&expected_revision_id=" + pending["revision_id"].(string)
	contextPath := proposal + "/context" + contextQuery
	protected := call(t, "GET", contextPath, reviewer, nil, 200)
	require.Equal(t, "review", protected["purpose"])
	require.Equal(t, pending, protected["proposal"])
	require.Equal(t, saved, protected["draft"])
	require.Equal(t, intent["reason"], protected["draft_reason"])
	require.Equal(t, input["reason"], protected["submission_reason"])
	require.Nil(t, protected["decision_reason"])
	func() {
		authorizationUnavailable.Store(true)
		defer authorizationUnavailable.Store(false)
		failure := call(t, "GET", contextPath, reviewer, nil, 503)
		require.NotContains(t, failure, "data", "OpenFGA outage must withhold the protected view")
	}()
	call(t, "GET", contextPath, actor, nil, 403)
	call(t, "GET", contextPath, admin, nil, 403)
	submissionContext := strings.Replace(contextPath, "purpose=review", "purpose=submission", 1)
	require.Equal(t, input["reason"], call(t, "GET", submissionContext, actor, nil, 200)["submission_reason"])
	call(t, "GET", submissionContext, reviewer, nil, 403)
	call(t, "GET", strings.Replace(contextPath, "expected_version=1", "expected_version=2", 1), reviewer, nil, 409)
	call(t, "GET", strings.Replace(contextPath, pending["revision_id"].(string), uuid.Must(uuid.NewV7()).String(), 1), reviewer, nil, 409)
	for _, query := range []string{"", contextQuery + "&purpose=submission", contextQuery + "&unexpected=true", strings.Replace(contextQuery, "expected_version=1", "expected_version=01", 1)} {
		call(t, "GET", proposal+"/context"+query, reviewer, nil, 400)
	}
	call(t, "GET", strings.Replace(contextPath, "/posting-staff/"+staff, "/posting-staff/"+uuid.Must(uuid.NewV7()).String(), 1), reviewer, nil, 404)
	call(t, "GET", strings.Replace(contextPath, proposal+"/context", postingBase+"/posting-proposals/"+uuid.Must(uuid.NewV7()).String()+"/context", 1), reviewer, nil, 404)
	assertPostingContextAuditRollback(t, db, tenant, reviewer, contextPath, call, []string{intent["reason"].(string), input["reason"].(string)})
	require.Len(t, call(t, "GET", proposal+"/history", actor, nil, 200)["data"], 1)
	decisionPath := proposal + "/decisions/" + uuid.Must(uuid.NewV7()).String()
	decision := map[string]any{"expected_version": pending["version"], "expected_revision_id": pending["revision_id"], "decision": "reject", "reason": "Independent synthetic placement rejected for correction", "sources_examined": true}
	for _, change := range []map[string]any{{"sources_examined": false}, {"reason": "invalid\ncontrol"}, {"unexpected": true}} {
		invalid := maps.Clone(decision)
		maps.Copy(invalid, change)
		call(t, "PUT", decisionPath, reviewer, invalid, 400)
	}
	for _, change := range []map[string]any{{"expected_version": 2}, {"expected_revision_id": uuid.Must(uuid.NewV7()).String()}} {
		stale := maps.Clone(decision)
		maps.Copy(stale, change)
		call(t, "PUT", decisionPath, reviewer, stale, 409)
	}
	approval := maps.Clone(decision)
	approval["decision"] = "approve"
	require.Equal(t, "posting_supervision_required", call(t, "PUT", decisionPath, reviewer, approval, 409)["error"])
	call(t, "PUT", decisionPath, actor, decision, 403)
	call(t, "PUT", decisionPath, admin, decision, 403)
	for _, fault := range []struct{ install, clear string }{
		{"ALTER TABLE workforce_audit ADD CONSTRAINT synthetic_posting_decision_audit CHECK (action<>'workforce.posting_proposal.rejected') NOT VALID", "ALTER TABLE workforce_audit DROP CONSTRAINT IF EXISTS synthetic_posting_decision_audit"},
		{"ALTER TABLE outbox_events ADD CONSTRAINT synthetic_posting_decision_outbox CHECK (subject<>'" + workforcecontract.SubjectPostingProposalChanged + "') NOT VALID", "ALTER TABLE outbox_events DROP CONSTRAINT IF EXISTS synthetic_posting_decision_outbox"},
	} {
		apply := func(sql string) {
			require.NoError(t, db.Migrate(context.Background(), []tenantdb.Migration{{ID: "test_posting_decision_fault_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
		}
		clear := func() { apply(fault.clear) }
		t.Cleanup(clear)
		counts := func() [2]int64 {
			var count [2]int64
			require.NoError(t, db.WithTenant(context.Background(), tenant, func(tx *gorm.DB) error {
				if err := tx.Table("workforce_audit").Where("actor_subject=? AND action=?", reviewer, "workforce.staff_actor.use").Count(&count[0]).Error; err != nil {
					return err
				}
				return tx.Table("outbox_events").Where("subject=?", workforcecontract.SubjectPostingProposalChanged).Count(&count[1]).Error
			}))
			return count
		}
		before := counts()
		require.Greater(t, before[0], int64(0), "probe must count actual committed Staff actor-use evidence")
		require.Greater(t, before[1], int64(0), "probe must count actual canonical proposal events")
		apply(fault.install)
		call(t, "PUT", decisionPath, reviewer, decision, 500)
		require.Equal(t, before, counts(), "failed decision rolls back actor-use evidence and outbox")
		clear()
		require.Equal(t, "pending", call(t, "GET", proposal, reviewer, nil, 200)["status"])
		call(t, "GET", decisionPath, reviewer, nil, 404)
		require.Len(t, call(t, "GET", proposal+"/history", reviewer, nil, 200)["data"], 1)
	}
	rejected := call(t, "PUT", decisionPath, reviewer, decision, 200)
	require.Equal(t, "rejected", rejected["status"])
	require.EqualValues(t, 2, rejected["version"])
	require.Equal(t, false, rejected["grants_access"])
	terminalContextPath := proposal + "/context?purpose=review&expected_version=2&expected_revision_id=" + rejected["revision_id"].(string)
	terminalContext := call(t, "GET", terminalContextPath, reviewer, nil, 200)
	require.Equal(t, rejected, terminalContext["proposal"])
	require.Equal(t, input["reason"], terminalContext["submission_reason"])
	require.Equal(t, decision["reason"], terminalContext["decision_reason"])
	call(t, "GET", contextPath, reviewer, nil, 409)
	require.Equal(t, rejected, call(t, "PUT", decisionPath, reviewer, decision, 200))
	require.Equal(t, rejected, call(t, "GET", decisionPath, reviewer, nil, 200))
	require.Equal(t, pending, call(t, "GET", command, actor, nil, 200))
	require.Len(t, call(t, "GET", proposal+"/history", reviewer, nil, 200)["data"], 2)
	changedDecision := maps.Clone(decision)
	changedDecision["reason"] = "A different reason must not reuse a retained command"
	call(t, "PUT", decisionPath, reviewer, changedDecision, 409)
	contributedDraft := uuid.Must(uuid.NewV7()).String()
	contributedDraftBase := postingBase + "/posting-drafts/" + contributedDraft
	contributed := call(t, "PUT", contributedDraftBase+"/commands/"+uuid.Must(uuid.NewV7()).String(), reviewer, intent, 200)
	contributedEdit := maps.Clone(intent)
	contributedEdit["expected_version"] = contributed["version"]
	contributedEdit["reason"] = "Another preparer retains the earlier reviewer's draft contribution"
	contributed = call(t, "PUT", contributedDraftBase+"/commands/"+uuid.Must(uuid.NewV7()).String(), actor, contributedEdit, 200)
	contributedInput := maps.Clone(input)
	contributedInput["draft_id"], contributedInput["draft_revision_id"], contributedInput["draft_version"] = contributedDraft, contributed["revision_id"], contributed["version"]
	contributedProposal := postingBase + "/posting-proposals/" + uuid.Must(uuid.NewV7()).String()
	contributedPending := call(t, "PUT", contributedProposal+"/commands/"+uuid.Must(uuid.NewV7()).String(), actor, contributedInput, 200)
	contributedDecision := maps.Clone(decision)
	contributedDecision["expected_version"], contributedDecision["expected_revision_id"] = contributedPending["version"], contributedPending["revision_id"]
	contributedDecisionPath := contributedProposal + "/decisions/" + uuid.Must(uuid.NewV7()).String()
	contributorContextPath := contributedProposal + "/context?purpose=review&expected_version=1&expected_revision_id=" + contributedPending["revision_id"].(string)
	call(t, "GET", contributorContextPath, reviewer, nil, 403)
	laterDraftEdit := maps.Clone(contributedEdit)
	laterDraftEdit["expected_version"], laterDraftEdit["reason"] = contributed["version"], "Later synthetic preparation must not replace submitted rationale"
	call(t, "PUT", contributedDraftBase+"/commands/"+uuid.Must(uuid.NewV7()).String(), actor, laterDraftEdit, 200)
	retainedContext := call(t, "GET", strings.Replace(contributorContextPath, "purpose=review", "purpose=submission", 1), actor, nil, 200)
	require.Equal(t, contributed, retainedContext["draft"], "context must use the submitted immutable revision after a later edit")
	require.Equal(t, contributedEdit["reason"], retainedContext["draft_reason"])
	call(t, "PUT", contributedDecisionPath, reviewer, contributedDecision, 403)
	require.Equal(t, contributedPending, call(t, "GET", contributedProposal, reviewer, nil, 200), "an earlier draft contributor cannot review even after another preparer edits and submits")
	require.Len(t, call(t, "GET", contributedProposal+"/history", reviewer, nil, 200)["data"], 1)
	call(t, "GET", contributedDecisionPath, reviewer, nil, 404)
	contributedDecision["decision"] = "withdraw"
	contributedWithdrawal := call(t, "PUT", contributedDecisionPath, actor, contributedDecision, 200)
	require.Equal(t, "withdrawn", contributedWithdrawal["status"])
	require.EqualValues(t, 2, contributedWithdrawal["version"])
	require.Equal(t, false, contributedWithdrawal["grants_access"])
	aliasDraft := uuid.Must(uuid.NewV7()).String()
	aliasSaved := call(t, "PUT", postingBase+"/posting-drafts/"+aliasDraft+"/commands/"+uuid.Must(uuid.NewV7()).String(), actor, intent, 200)
	aliasInput := maps.Clone(input)
	aliasInput["draft_id"], aliasInput["draft_revision_id"], aliasInput["draft_version"] = aliasDraft, aliasSaved["revision_id"], aliasSaved["version"]
	aliasProposal := postingBase + "/posting-proposals/" + uuid.Must(uuid.NewV7()).String()
	aliasPending := call(t, "PUT", aliasProposal+"/commands/"+uuid.Must(uuid.NewV7()).String(), actor, aliasInput, 200)
	aliasDecision := maps.Clone(decision)
	aliasDecision["expected_version"], aliasDecision["expected_revision_id"] = aliasPending["version"], aliasPending["revision_id"]
	importHistoricalPostingAlias(t, db, tenant, hospital, actor, reviewer, admin)
	aliasDecisionPath := aliasProposal + "/decisions/" + uuid.Must(uuid.NewV7()).String()
	call(t, "GET", aliasProposal+"/context?purpose=review&expected_version=1&expected_revision_id="+aliasPending["revision_id"].(string), reviewer, nil, 403)
	call(t, "PUT", aliasDecisionPath, reviewer, aliasDecision, 403)
	require.Equal(t, aliasPending, call(t, "GET", aliasProposal, reviewer, nil, 200), "a retained account association excludes an otherwise current reviewer without changing the pending proposal")
	require.Len(t, call(t, "GET", aliasProposal+"/history", reviewer, nil, 200)["data"], 1)
	call(t, "GET", aliasDecisionPath, reviewer, nil, 404)
	aliasDecision["decision"] = "withdraw"
	require.Equal(t, "withdrawn", call(t, "PUT", aliasDecisionPath, actor, aliasDecision, 200)["status"])
	withdrawalProposal := postingBase + "/posting-proposals/" + uuid.Must(uuid.NewV7()).String()
	withdrawalPending := call(t, "PUT", withdrawalProposal+"/commands/"+uuid.Must(uuid.NewV7()).String(), actor, input, 200)

	for _, name := range []string{"Synthetic changed non-digital identity", ""} {
		enrollment := call(t, "GET", staffBase+"/enrollment?hr_intake=v1", manager, nil, 200)
		call(t, "PUT", staffBase+"/enrollment?hr_intake=v1", manager, map[string]any{"expected_version": enrollment["version"], "staff": map[string]any{"employee_code": "SYN-ACTIVATION-posting_nondigital", "display_name": "Synthetic Reviewed Digital Joiner", "job_category": "other", "employment_status": "active"}, "profile": map[string]any{"given_name": name}}, 200)
	}
	changed := call(t, "GET", postingBase+"/posting-review-source", actor, nil, 200)
	current := call(t, "GET", postingBase+"/posting-draft-source", actor, nil, 200)
	newIntent := maps.Clone(intent)
	newIntent["staff_version"], newIntent["employment_epoch"] = current["staff_version"], current["employment_epoch"]
	newDraft := uuid.Must(uuid.NewV7()).String()
	newSaved := call(t, "PUT", postingBase+"/posting-drafts/"+newDraft+"/commands/"+uuid.Must(uuid.NewV7()).String(), actor, newIntent, 200)
	newInput := maps.Clone(input)
	newInput["draft_id"], newInput["draft_revision_id"], newInput["draft_version"] = newDraft, newSaved["revision_id"], newSaved["version"]
	newInput["staff_version"], newInput["employment_epoch"] = current["staff_version"], current["employment_epoch"]
	newProposal := postingBase + "/posting-proposals/" + uuid.Must(uuid.NewV7()).String()
	call(t, "PUT", newProposal+"/commands/"+uuid.Must(uuid.NewV7()).String(), actor, newInput, 409)
	require.Equal(t, "source_changed", changed["generation_state"], "restoring a different worker's identity cannot revive their reviewed generation")
	call(t, "GET", newProposal, actor, nil, 404)
	require.Equal(t, pending, call(t, "GET", command, actor, nil, 200), "source invalidation preserves the historical non-operating receipt")
	withdrawal := map[string]any{"expected_version": withdrawalPending["version"], "expected_revision_id": withdrawalPending["revision_id"], "decision": "withdraw", "reason": "Original submitter withdraws after target source correction", "sources_examined": true}
	withdrawalPath := withdrawalProposal + "/decisions/" + uuid.Must(uuid.NewV7()).String()
	call(t, "PUT", withdrawalPath, reviewer, withdrawal, 403)
	withdrawn := call(t, "PUT", withdrawalPath, actor, withdrawal, 200)
	require.Equal(t, "withdrawn", withdrawn["status"])
	require.EqualValues(t, 2, withdrawn["version"])
	require.Equal(t, withdrawn, call(t, "PUT", withdrawalPath, actor, withdrawal, 200))
	require.Equal(t, withdrawn, call(t, "GET", withdrawalPath, actor, nil, 200))
	require.Empty(t, call(t, "GET", staffBase+"/login-links", admin, nil, 200)["data"], "posting work must not provision a login for its non-digital target")
}

// Import a legacy revoked association; current generations and assignments are still created by the public workflow.
func importHistoricalPostingAlias(t *testing.T, db *tenantdb.DB, tenant, hospital, contributor, reviewer, administrator string) {
	t.Helper()
	quoted := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
	link := uuid.Must(uuid.NewV7()).String()
	sql := fmt.Sprintf(`
 SELECT set_config('app.tenant_id',%s,true),set_config('app.workforce_subject',%s,true),set_config('app.workforce_hospital',%s,true),set_config('app.workforce_login_link_access','authorized',true);
 ALTER TABLE workforce_login_links DISABLE TRIGGER workforce_login_link_guard;
 ALTER TABLE workforce_login_links DISABLE TRIGGER workforce_bind_login_generation;
 ALTER TABLE workforce_login_link_history DISABLE TRIGGER workforce_login_link_history_guard;
 DO $fixture$ DECLARE current_link record;BEGIN
 SELECT l.staff_id INTO STRICT current_link FROM workforce_login_links l
 WHERE l.tenant_id=%s::uuid AND l.hospital_id=%s::uuid AND l.subject=%s AND l.status='active' AND l.employment_generation_id IS NOT NULL;
 INSERT INTO workforce_login_links(tenant_id,hospital_id,staff_id,id,subject,status,version,proposed_by_subject,proposal_reason,actor_subject,reason,created_at,updated_at)
 VALUES(%s::uuid,%s::uuid,current_link.staff_id,%s::uuid,%s,'revoked',3,'synthetic-legacy-importer','Imported synthetic historical alias','synthetic-legacy-importer','Imported revoked association',clock_timestamp()-interval '2 hours',clock_timestamp()-interval '118 minutes');
 INSERT INTO workforce_login_link_history(tenant_id,hospital_id,id,version,status,actor_subject,reason,created_at)
 VALUES(%s::uuid,%s::uuid,%s::uuid,1,'pending','synthetic-legacy-importer','Imported synthetic historical alias',clock_timestamp()-interval '2 hours'),
 (%s::uuid,%s::uuid,%s::uuid,2,'active','synthetic-independent-legacy-reviewer','Imported historical independent approval',clock_timestamp()-interval '119 minutes');
 IF (SELECT count(*) FROM workforce_login_links WHERE tenant_id=%s::uuid AND hospital_id=%s::uuid AND staff_id=current_link.staff_id AND status='active')<>1 THEN RAISE EXCEPTION 'fixture changed current login continuity';END IF;
 END $fixture$;
 ALTER TABLE workforce_login_link_history ENABLE TRIGGER workforce_login_link_history_guard;
 ALTER TABLE workforce_login_links ENABLE TRIGGER workforce_bind_login_generation;
 ALTER TABLE workforce_login_links ENABLE TRIGGER workforce_login_link_guard;
 `, quoted(tenant), quoted(administrator), quoted(hospital), quoted(tenant), quoted(hospital), quoted(reviewer), quoted(tenant), quoted(hospital), quoted(link), quoted(contributor), quoted(tenant), quoted(hospital), quoted(link), quoted(tenant), quoted(hospital), quoted(link), quoted(tenant), quoted(hospital))
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_historical_posting_alias_" + link, SQL: sql}}))
}

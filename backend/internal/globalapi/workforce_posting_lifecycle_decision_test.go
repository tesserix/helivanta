package globalapi

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type retainedPostingLifecycleHeader struct {
	InitialRevisionID        uuid.UUID
	ReviewerSubject          string
	ReviewerStaffID          uuid.UUID
	ReviewerLinkID           uuid.UUID
	ReviewerGenerationID     uuid.UUID
	ReviewAssignmentID       uuid.UUID
	ReviewAssignmentVersion  int64
	SupportPermission        string
	SupportAssignmentID      uuid.UUID
	SupportAssignmentVersion int64
	SourceCount              int64
	CheckedAt                time.Time
	EffectiveUntil           time.Time
	CreatedAt                time.Time
}

type retainedPostingLifecycleSource struct {
	Hop              int
	SourceStaffID    uuid.UUID
	SourceProposalID uuid.UUID
	SourceRevisionID uuid.UUID
	CutoffID         *uuid.UUID
	CutoffVersion    int64
	CutoffAt         *time.Time
}

type retainedPostingLifecycleProof struct {
	Headers []retainedPostingLifecycleHeader
	Sources []retainedPostingLifecycleSource
}

func readRetainedPostingLifecycleProof(t *testing.T, db *tenantdb.DB, requestTenant, ownerTenant string, hospital, staff, proposal string) retainedPostingLifecycleProof {
	t.Helper()
	var out retainedPostingLifecycleProof
	err := db.WithTenant(t.Context(), requestTenant, func(tx *gorm.DB) error {
		// Internal RLS probe only; HTTP acceptance uses real scoped IAM/OpenFGA.
		if err := tx.Exec(`SELECT set_config('app.workforce_hospital',?,true),set_config('app.workforce_subject','synthetic-lifecycle-proof-reader',true),set_config('app.workforce_posting','read',true),set_config('app.workforce_posting_scoped','read',true),set_config('app.workforce_posting_staff',?,true),set_config('app.workforce_posting_permission','workforce.posting.proposal.read',true),set_config('app.workforce_posting_actor',?,true),set_config('app.workforce_posting_link',?,true),set_config('app.workforce_posting_generation',?,true),set_config('app.workforce_posting_assignment',?,true),set_config('app.workforce_posting_assignment_version','1',true),set_config('app.workforce_posting_scope_epoch','1',true),set_config('app.workforce_posting_membership_epoch','1',true),set_config('app.workforce_posting_review_mode','read',true)`, hospital, staff, uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()).Error; err != nil {
			return err
		}
		if err := tx.Table("workforce_posting_lifecycle_reviews").Where("tenant_id=? AND proposal_id=?", ownerTenant, proposal).Find(&out.Headers).Error; err != nil {
			return err
		}
		return tx.Table("workforce_posting_lifecycle_sources").Where("tenant_id=? AND proposal_id=?", ownerTenant, proposal).Order("hop").Find(&out.Sources).Error
	})
	require.NoError(t, err)
	return out
}

func assertPostingLifecycleApprovalFaults(t *testing.T, call func(*testing.T, string, string, string, any, int) map[string]any, db *tenantdb.DB, tenant string, root, reviewer string, pending, body map[string]any) {
	t.Helper()
	apply := func(sql string) {
		t.Helper()
		require.NoError(t, db.Migrate(context.WithoutCancel(t.Context()), []tenantdb.Migration{{ID: "synthetic_lifecycle_decision_fault_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
	}
	clear := func() {
		apply(`ALTER TABLE workforce_posting_lifecycle_reviews DROP CONSTRAINT IF EXISTS synthetic_lifecycle_decision_fault;
ALTER TABLE workforce_posting_lifecycle_sources DROP CONSTRAINT IF EXISTS synthetic_lifecycle_decision_fault;
ALTER TABLE workforce_audit DROP CONSTRAINT IF EXISTS synthetic_lifecycle_decision_fault;
ALTER TABLE outbox_events DROP CONSTRAINT IF EXISTS synthetic_lifecycle_decision_fault;
DROP TRIGGER IF EXISTS aaa_synthetic_lifecycle_skip ON workforce_posting_lifecycle_reviews;
DROP TRIGGER IF EXISTS aaa_synthetic_lifecycle_skip ON workforce_posting_lifecycle_sources;
DROP FUNCTION IF EXISTS synthetic_lifecycle_skip_ancestor();
DROP FUNCTION IF EXISTS synthetic_lifecycle_skip_all();
DROP TRIGGER IF EXISTS aaa_synthetic_lifecycle_clear ON workforce_posting_proposal_revisions;
DROP FUNCTION IF EXISTS synthetic_lifecycle_clear_mode();`)
	}
	t.Cleanup(clear)
	for _, fault := range []struct{ name, sql string }{
		{"review evidence", `ALTER TABLE workforce_posting_lifecycle_reviews ADD CONSTRAINT synthetic_lifecycle_decision_fault CHECK(false) NOT VALID`},
		{"source evidence", `ALTER TABLE workforce_posting_lifecycle_sources ADD CONSTRAINT synthetic_lifecycle_decision_fault CHECK(false) NOT VALID`},
		{"approval audit", `ALTER TABLE workforce_audit ADD CONSTRAINT synthetic_lifecycle_decision_fault CHECK(action<>'workforce.posting_proposal.approved') NOT VALID`},
		{"outbox", fmt.Sprintf(`ALTER TABLE outbox_events ADD CONSTRAINT synthetic_lifecycle_decision_fault CHECK(subject<>'%s') NOT VALID`, workforcecontract.SubjectPostingProposalChanged)},
		{"missing ancestor observation", `CREATE FUNCTION synthetic_lifecycle_skip_ancestor() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.hop>0 THEN RETURN NULL;END IF;RETURN NEW;END $$;
CREATE TRIGGER aaa_synthetic_lifecycle_skip BEFORE INSERT ON workforce_posting_lifecycle_sources FOR EACH ROW EXECUTE FUNCTION synthetic_lifecycle_skip_ancestor()`},
		{"missing entire lifecycle proof", `CREATE FUNCTION synthetic_lifecycle_skip_all() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL;END $$;
CREATE TRIGGER aaa_synthetic_lifecycle_skip BEFORE INSERT ON workforce_posting_lifecycle_reviews FOR EACH ROW EXECUTE FUNCTION synthetic_lifecycle_skip_all();
CREATE TRIGGER aaa_synthetic_lifecycle_skip BEFORE INSERT ON workforce_posting_lifecycle_sources FOR EACH ROW EXECUTE FUNCTION synthetic_lifecycle_skip_all()`},

		{"missing proof despite lost opt-in marker", `CREATE FUNCTION synthetic_lifecycle_skip_all() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NULL;END $$;
CREATE TRIGGER aaa_synthetic_lifecycle_skip BEFORE INSERT ON workforce_posting_lifecycle_reviews FOR EACH ROW EXECUTE FUNCTION synthetic_lifecycle_skip_all();
CREATE TRIGGER aaa_synthetic_lifecycle_skip BEFORE INSERT ON workforce_posting_lifecycle_sources FOR EACH ROW EXECUTE FUNCTION synthetic_lifecycle_skip_all();
CREATE FUNCTION synthetic_lifecycle_clear_mode() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.status='approved' THEN PERFORM set_config('app.workforce_posting_lifecycle_approval','',true);END IF;RETURN NEW;END $$;
CREATE TRIGGER aaa_synthetic_lifecycle_clear BEFORE INSERT ON workforce_posting_proposal_revisions FOR EACH ROW EXECUTE FUNCTION synthetic_lifecycle_clear_mode()`},
	} {
		t.Logf("Injecting lifecycle approval failure: %s", fault.name)
		before := postingRootApprovalEffects(t, db, tenant, reviewer)
		apply(fault.sql)
		command := root + "/decisions/" + uuid.Must(uuid.NewV7()).String()
		failed := call(t, "PUT", command, reviewer, body, 500)
		require.NotContains(t, failed, "revision_id", fault.name)
		require.Equal(t, before, postingRootApprovalEffects(t, db, tenant, reviewer), fault.name)
		clear()
		require.Equal(t, pending, call(t, "GET", root, reviewer, nil, 200), fault.name)
		call(t, "GET", command, reviewer, nil, 404)
		proof := readRetainedPostingLifecycleProof(t, db, tenant, tenant, pending["hospital_id"].(string), pending["staff_id"].(string), pending["id"].(string))
		require.Empty(t, proof.Headers, fault.name)
		require.Empty(t, proof.Sources, fault.name)
	}
}

func assertPostingLifecycleInputRefusals(t *testing.T, call func(*testing.T, string, string, string, any, int) map[string]any, db *tenantdb.DB, tenant string, root, reviewer string, body map[string]any) {
	t.Helper()
	valid := body["lifecycle_review"].(map[string]any)
	for _, observation := range []struct {
		value  any
		status int
	}{
		{nil, 400},
		{map[string]any{"cutoff_versions": []any{nil, 1}, "effective_until": valid["effective_until"]}, 400},
		{map[string]any{"cutoff_versions": []int{0, 0}, "effective_until": valid["effective_until"]}, 409},
		{map[string]any{"cutoff_versions": []int{0}, "effective_until": valid["effective_until"]}, 409},
		{map[string]any{"cutoff_versions": []int{0, 1, 0}, "effective_until": valid["effective_until"]}, 409},
	} {
		changed := map[string]any{}
		for key, value := range body {
			changed[key] = value
		}
		changed["lifecycle_review"] = observation.value
		before := postingRootApprovalEffects(t, db, tenant, reviewer)
		call(t, "PUT", fmt.Sprintf("%s/decisions/%s", root, uuid.Must(uuid.NewV7())), reviewer, changed, observation.status)
		require.Equal(t, before, postingRootApprovalEffects(t, db, tenant, reviewer))
	}
}

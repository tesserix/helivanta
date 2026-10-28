package iam_test

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // External test exercises its own module's migration.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type beforeMembershipEpoch struct{ *iam.Module }

func (m beforeMembershipEpoch) Migrations() []tenantdb.Migration {
	all := m.Module.Migrations()
	for i, migration := range all {
		if migration.ID == "0011_iam_assignment_membership" {
			return all[:i]
		}
	}
	panic("membership migration missing")
}

func TestMembershipMigrationPreservesEvidenceWithoutInventingContinuity(t *testing.T) {
	module := iam.New(nil)
	_, db, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{beforeMembershipEpoch{module}}})
	id, request := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	intake := uuid.Must(uuid.NewV7())
	hospital := uuid.New()
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES (?,'historical-target','staff_member'),(?,'historical-target','doctor'),(?,'historical-admin','tenant_admin'),(?,'historical-reviewer','tenant_admin')`, testutil.TenantA, testutil.TenantA, testutil.TenantA, testutil.TenantA).Error; err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.assignment_access','authorized',true),set_config('app.actor_subject','historical-admin',true)`).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO iam_assignment_proposals(tenant_id,id,hospital_id,subject,template_key,reason,valid_from,expires_at,proposed_by,request_id,status,version) VALUES(?,?,?,'historical-target','workforce.leave_reviewer.v1','Synthetic pre-migration evidence',now(),now()+interval '1 hour','historical-admin',?,'pending_scope',1)`, testutil.TenantA, id, hospital, intake).Error; err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.assignment_scope_result',?,true)`, intake.String()).Error; err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE iam_assignment_proposals SET status='pending_review',version=2 WHERE id=?`, id).Error; err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.assignment_command','review',true),set_config('app.actor_subject','historical-reviewer',true)`).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO iam_assignment_decisions(tenant_id,id,proposal_id,request_id,expected_version,decision,reason,result_version,actor_subject) VALUES(?,?,?,?,2,'approve','Independent historical review',3,'historical-reviewer')`, testutil.TenantA, uuid.Must(uuid.NewV7()), id, request).Error; err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE iam_assignment_proposals SET status='pending_decision_scope',version=3,request_id=? WHERE id=?`, request, id).Error; err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.assignment_scope_result',?,true)`, request.String()).Error; err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE iam_assignment_proposals SET status='approved_pending_activation',version=4 WHERE id=?`, id).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) SELECT ?,'synthetic-load-'||n,'staff_member' FROM generate_series(1,100000) n`, testutil.TenantA).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO iam_assignment_scope_epochs(tenant_id,hospital_id,subject,epoch) SELECT ?,?,'synthetic-load-'||n,1 FROM generate_series(1,100000) n`, testutil.TenantA, hospital).Error
	}))
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantB, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES (?,'historical-target','staff_member')`, testutil.TenantB).Error
	}))
	all := module.Migrations()
	require.NoError(t, db.Migrate(t.Context(), all))
	require.NoError(t, db.Migrate(t.Context(), all))
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var state struct {
			Epoch       int64
			MemberCount int64
		}
		if err := tx.Table("iam_subject_membership_epochs").Where("subject=?", "historical-target").Take(&state).Error; err != nil {
			return err
		}
		require.Equal(t, int64(1), state.Epoch)
		require.Equal(t, int64(2), state.MemberCount)
		if err := tx.Exec(`SELECT set_config('app.assignment_access','authorized',true)`).Error; err != nil {
			return err
		}
		for _, table := range []string{"iam_subject_membership_epochs", "iam_assignment_scope_epochs"} {
			var plan string
			if err := tx.Raw("EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) SELECT epoch FROM "+table+" WHERE tenant_id=? AND subject=? FOR UPDATE", testutil.TenantA, "historical-target").Scan(&plan).Error; err != nil {
				return err
			}
			require.Contains(t, plan, "Index Cond", "subject-scoped reads must remain indexed at 100,000 other subjects")
			t.Logf("%s subject lookup at 100,000 synthetic subjects: %s", table, plan)
		}
		for _, table := range []string{"iam_assignment_proposals", "iam_assignment_snapshots"} {
			var row struct {
				MembershipEpoch int64
				Version         int64
			}
			if err := tx.Table(table).Where("id=?", id).Take(&row).Error; err != nil {
				return err
			}
			require.Zero(t, row.MembershipEpoch, "historical continuity is unknown; fresh review is required")
			require.Equal(t, int64(4), row.Version)
		}
		return nil
	}))
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantB, func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("iam_subject_membership_epochs").Where("subject=?", "historical-target").Select("member_count").Scan(&count).Error; err != nil {
			return err
		}
		require.Equal(t, int64(1), count)
		return nil
	}))
	rolledBack := errors.New("synthetic rollback")
	require.ErrorIs(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject','synthetic-migration-test-admin',true)`).Error; err != nil {
			return err
		}
		if err := tx.Exec(`DELETE FROM iam_members WHERE subject='historical-target'`).Error; err != nil {
			return err
		}
		return rolledBack
	}), rolledBack)
	require.NoError(t, db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		var state struct {
			Epoch       int64
			MemberCount int64
		}
		if err := tx.Table("iam_subject_membership_epochs").Where("subject=?", "historical-target").Take(&state).Error; err != nil {
			return err
		}
		require.Equal(t, int64(1), state.Epoch)
		require.Equal(t, int64(2), state.MemberCount)
		if err := tx.Exec(`SELECT set_config('app.assignment_access','authorized',true)`).Error; err != nil {
			return err
		}
		var epoch int64
		if err := tx.Table("iam_assignment_scope_epochs").Select("epoch").Take(&epoch).Error; err != nil {
			return err
		}
		require.Equal(t, int64(1), epoch)
		var revocations int64
		if err := tx.Table("iam_membership_revocations").Count(&revocations).Error; err != nil {
			return err
		}
		require.Zero(t, revocations, "rollback must also roll back the revocation evidence")
		return nil
	}))
	warnings, err := db.LintRLS(t.Context())
	require.NoError(t, err)
	require.Empty(t, warnings, "the backfill must restore forced RLS before committing")
}

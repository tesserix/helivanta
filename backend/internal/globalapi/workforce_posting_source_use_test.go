package globalapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type postingSourceUseProbe func(context.Context, *testing.T, string, map[string]any, func(context.Context, authz.StaffPostingSourceReference) error) error

func newPostingSourceUseProbe(db *tenantdb.DB, authority authz.ScopedAuthorizer, source authz.CurrentStaffPostingSourceGuard, tenant string) postingSourceUseProbe {
	return func(ctx context.Context, t *testing.T, subject string, selected map[string]any, action func(context.Context, authz.StaffPostingSourceReference) error) error {
		t.Helper()
		scope := authz.StaffPostingSourceScope{TenantID: tenant, Subject: subject, HospitalID: uuid.MustParse(selected["hospital_id"].(string)), StaffID: uuid.MustParse(selected["staff_id"].(string)), PostingID: uuid.MustParse(selected["id"].(string)), RevisionID: uuid.MustParse(selected["revision_id"].(string)), Version: int64(selected["version"].(float64)), DepartmentID: uuid.MustParse(selected["department_id"].(string)), DepartmentRevisionID: uuid.MustParse(selected["department_revision_id"].(string))}
		checks := []authz.ScopeCheck{}
		for _, permission := range []authz.Permission{authz.StaffPostingSourceUse, authz.StaffActorUse, "workforce.posting.submit"} {
			request := authz.ScopeRequest{TenantID: tenant, Subject: subject, HospitalID: scope.HospitalID.String(), Permission: permission}
			grant, err := authority.AuthorizeScope(ctx, request)
			if err != nil {
				return err
			}
			checks = append(checks, authz.ScopeCheck{Request: request, Grant: grant})
		}
		return authority.(authz.CompoundScopedCommandGuard).WithScopeGrants(ctx, checks, time.Now().UTC(), func(command context.Context) error { return source.WithCurrentStaffPosting(command, scope, action) })
	}
}

func postingSourceUseEffects(t *testing.T, db *tenantdb.DB, tenant, subject string) [2]int64 {
	t.Helper()
	var counts [2]int64
	require.NoError(t, db.WithTenant(context.WithoutCancel(t.Context()), tenant, func(tx *gorm.DB) error {
		for i, action := range []string{"workforce.staff_actor.use", "workforce.posting_source.use"} {
			if err := tx.Table("workforce_audit").Where("actor_subject=? AND action=?", subject, action).Count(&counts[i]).Error; err != nil {
				return err
			}
		}
		return nil
	}))
	return counts
}

func assertCurrentPostingSourceUse(t *testing.T, db *tenantdb.DB, tenant, subject string, selected map[string]any, use postingSourceUseProbe) authz.StaffPostingSourceReference {
	t.Helper()
	var ref authz.StaffPostingSourceReference
	before := postingSourceUseEffects(t, db, tenant, subject)
	require.NoError(t, use(t.Context(), t, subject, selected, func(ctx context.Context, current authz.StaffPostingSourceReference) error {
		ref = current
		require.Equal(t, selected["staff_id"], current.Actor.StaffID.String(), "ordinary self-operation is not rejected as independent review")
		require.NoError(t, db.WithTenantCommand(ctx, tenant, func(tx *gorm.DB) error {
			var mode string
			if err := tx.Raw(`SELECT COALESCE(current_setting('app.workforce_posting_review_mode',true),'')`).Scan(&mode).Error; err != nil {
				return err
			}
			require.Empty(t, mode, "the consumer must not inherit the owner's source-read context")
			changed := tx.Exec(`UPDATE workforce_posting_proposals SET version=version+1 WHERE id=?`, selected["id"])
			require.NoError(t, changed.Error)
			require.Zero(t, changed.RowsAffected, "source use cannot write or approve a posting")
			return nil
		}))
		return nil
	}))
	require.NotEqual(t, uuid.Nil, ref.UseID)
	require.Equal(t, selected["id"], ref.PostingID.String())
	require.Equal(t, selected["revision_id"], ref.RevisionID.String())
	require.Len(t, ref.SourceHash, 64)
	require.Greater(t, ref.SourceCount, 0)
	require.True(t, ref.CheckedAt.Before(ref.SourceUntil))
	after := postingSourceUseEffects(t, db, tenant, subject)
	require.Equal(t, before[0]+1, after[0])
	require.Equal(t, before[1]+1, after[1])
	failure := errors.New("synthetic consumer persistence failure")
	before = after
	require.ErrorIs(t, use(t.Context(), t, subject, selected, func(context.Context, authz.StaffPostingSourceReference) error { return failure }), failure)
	require.Equal(t, before, postingSourceUseEffects(t, db, tenant, subject), "consumer failure rolls back both source and actor use evidence")

	apply := func(sql string) {
		t.Helper()
		require.NoError(t, db.Migrate(context.WithoutCancel(t.Context()), []tenantdb.Migration{{ID: "synthetic_posting_source_use_" + uuid.Must(uuid.NewV7()).String(), SQL: sql}}))
	}
	clear := func() {
		apply(`ALTER TABLE workforce_audit DROP CONSTRAINT IF EXISTS synthetic_posting_source_use_audit; DROP POLICY IF EXISTS synthetic_posting_source_use_proof ON workforce_posting_chain_sources;`)
	}
	t.Cleanup(clear)
	for _, fault := range []struct {
		name, sql string
		expected  error
	}{
		{"mandatory source audit", `ALTER TABLE workforce_audit ADD CONSTRAINT synthetic_posting_source_use_audit CHECK(action<>'workforce.posting_source.use') NOT VALID`, authz.ErrScopedAuthorizationUnavailable},
		{"missing retained chain proof", `CREATE POLICY synthetic_posting_source_use_proof ON workforce_posting_chain_sources AS RESTRICTIVE FOR SELECT USING(COALESCE(current_setting('app.workforce_posting_review_mode',true),'')<>'source_use')`, authz.ErrScopedCommandDenied},
	} {
		before = postingSourceUseEffects(t, db, tenant, subject)
		called := false
		apply(fault.sql)
		err := use(t.Context(), t, subject, selected, func(context.Context, authz.StaffPostingSourceReference) error { called = true; return nil })
		require.ErrorIs(t, err, fault.expected, fault.name)
		require.False(t, called, fault.name)
		require.Equal(t, before, postingSourceUseEffects(t, db, tenant, subject), fault.name)
		clear()
	}
	return ref
}

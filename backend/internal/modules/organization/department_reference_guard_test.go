package organization_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/organization" //nolint:depguard // Module under test.
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestDepartmentReferenceGuardHoldsCurrentSourceUntilOuterCommit(t *testing.T) {
	r, db, ctx, hospital := departmentHarness(t)
	department, revision := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	w := organizationCall(r, "PUT", departmentPath(hospital, department.String(), revision.String()), "manager", testutil.TenantA, departmentIntent(0, "SYN-GUARD", "Synthetic guarded department", "department", "", "active"))
	require.Equal(t, 200, w.Code, w.Body.String())
	guard := organization.New().DepartmentReferences(db)
	ref := authz.DepartmentReference{TenantID: testutil.TenantA, HospitalID: uuid.MustParse(hospital), DepartmentID: department, DepartmentRevision: revision}
	lockKey := testutil.TenantA + ":organization-departments:" + hospital
	tryRestructure := func() bool {
		var row struct{ Acquired bool }
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT pg_try_advisory_xact_lock(hashtextextended(?,0)) AS acquired`, lockKey).Scan(&row).Error
		}))
		return row.Acquired
	}
	var escaped context.Context
	require.NoError(t, db.WithGuardedTenantCommand(ctx, testutil.TenantA, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
		err := guard.WithCurrentDepartment(commandCtx, ref, func(actionCtx context.Context) error {
			escaped = actionCtx
			require.False(t, tryRestructure(), "the owner restructuring gate remains held inside the action")
			err := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				return tx.Exec(`SELECT id FROM organization_departments WHERE tenant_id=? AND hospital_id=? AND id=? FOR UPDATE NOWAIT`, testutil.TenantA, hospital, department).Error
			})
			require.ErrorContains(t, err, "55P03", "the exact source row remains locked")
			return nil
		})
		require.NoError(t, err)
		require.False(t, tryRestructure(), "returning from the owner guard must not release its lock before the product commits")
		return nil
	}))
	require.True(t, tryRestructure())
	called := false
	action := func(context.Context) error { called = true; return nil }
	require.ErrorIs(t, guard.WithCurrentDepartment(escaped, ref, action), authz.ErrScopedAuthorizationUnavailable)
	require.False(t, called)
	retired := uuid.Must(uuid.NewV7()).String()
	w = organizationCall(r, "PUT", departmentPath(hospital, department.String(), retired), "manager", testutil.TenantA, departmentIntent(1, "SYN-GUARD", "Synthetic guarded department", "department", "", "retired"))
	require.Equal(t, 200, w.Code, w.Body.String())
	err := db.WithGuardedTenantCommand(ctx, testutil.TenantA, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
		return guard.WithCurrentDepartment(commandCtx, ref, action)
	})
	require.ErrorIs(t, err, authz.ErrDepartmentReferenceChanged)
	require.False(t, called)
}

func TestDepartmentReferenceGuardRejectsUnguardedForeignAndDifferentDatabaseContexts(t *testing.T) {
	_, db, ctx, hospital := departmentHarness(t)
	guard := organization.New().DepartmentReferences(db)
	ref := authz.DepartmentReference{TenantID: testutil.TenantA, HospitalID: uuid.MustParse(hospital), DepartmentID: uuid.Must(uuid.NewV7()), DepartmentRevision: uuid.Must(uuid.NewV7())}
	called := false
	action := func(context.Context) error { called = true; return nil }
	require.ErrorIs(t, guard.WithCurrentDepartment(ctx, ref, action), authz.ErrScopedAuthorizationUnavailable)
	err := db.WithGuardedTenantCommand(ctx, testutil.TenantA, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
		otherDB := organization.New().DepartmentReferences(&tenantdb.DB{})
		require.ErrorIs(t, otherDB.WithCurrentDepartment(commandCtx, ref, action), authz.ErrScopedAuthorizationUnavailable)
		foreign := ref
		foreign.TenantID = testutil.TenantB
		require.ErrorIs(t, guard.WithCurrentDepartment(commandCtx, foreign, action), tenantdb.ErrGuardedCommandScope)
		require.ErrorIs(t, guard.WithCurrentDepartment(commandCtx, ref, action), authz.ErrDepartmentReferenceHidden)
		return nil
	})
	require.NoError(t, err)
	require.False(t, called)
}

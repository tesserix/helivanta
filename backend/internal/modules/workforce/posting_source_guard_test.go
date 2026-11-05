package workforce_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // Tests the owner-provided source boundary.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

type deniedPostingDepartmentGuard struct{ calls int }

func (g *deniedPostingDepartmentGuard) WithCurrentDepartment(context.Context, authz.DepartmentReference, func(context.Context) error) error {
	g.calls++
	return authz.ErrScopedAuthorizationUnavailable
}

type deniedPostingActorGuard struct{ calls int }

func (g *deniedPostingActorGuard) WithCurrentStaffActor(context.Context, authz.StaffActorScope, func(context.Context, authz.StaffActorReference) error) error {
	g.calls++
	return authz.ErrScopedAuthorizationUnavailable
}

func TestPostingSourceUsePermissionHasNoLegacyRoleDefault(t *testing.T) {
	count := 0
	for _, grant := range workforce.New().Permissions() {
		if grant.Permission == authz.StaffPostingSourceUse {
			count++
			require.Empty(t, grant.Roles)
		}
	}
	require.Equal(t, 1, count)
}

func TestPostingSourceGuardRejectsUnboundIAMEvidence(t *testing.T) {
	module := workforce.New()
	_, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{module}})
	departments := &deniedPostingDepartmentGuard{}
	actors := &deniedPostingActorGuard{}
	guard := module.PostingSources(db, departments, actors)
	scope := authz.StaffPostingSourceScope{TenantID: testutil.TenantA, Subject: "synthetic-source-consumer", HospitalID: uuid.New(), StaffID: uuid.New(), PostingID: uuid.Must(uuid.NewV7()), RevisionID: uuid.Must(uuid.NewV7()), Version: 2, DepartmentID: uuid.Must(uuid.NewV7()), DepartmentRevisionID: uuid.Must(uuid.NewV7())}
	called := false
	action := func(context.Context, authz.StaffPostingSourceReference) error { called = true; return nil }
	require.ErrorIs(t, guard.WithCurrentStaffPosting(ctx, scope, action), authz.ErrScopedAuthorizationUnavailable)
	err := db.WithGuardedTenantCommand(ctx, scope.TenantID, time.Now().Add(time.Minute), func(*gorm.DB) error { return nil }, func(command context.Context) error { return guard.WithCurrentStaffPosting(command, scope, action) })
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	require.False(t, called)
	require.Zero(t, actors.calls)
	require.Zero(t, departments.calls, "a plain tenant command must not obtain a source or structural capability")
}

package workforce_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // External test exercises its owner source boundary.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

func TestStaffActorUsePermissionIsWorkforceOwned(t *testing.T) {
	var count int
	for _, grant := range workforce.New().Permissions() {
		if grant.Permission == authz.StaffActorUse {
			count++
			require.Empty(t, grant.Roles, "a current Staff actor is not implied by a legacy job role")
		}
	}
	require.Equal(t, 1, count, "the source-use permission must have exactly one owner declaration")
}

func TestStaffActorSourceRejectsCommandsWithoutCurrentIAMEvidence(t *testing.T) {
	module := workforce.New()
	_, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{module}})
	guard := module.StaffActors(db)
	scope := authz.StaffActorScope{TenantID: testutil.TenantA, Subject: "synthetic-unbound-actor", HospitalID: uuid.New()}
	called := false
	action := func(context.Context, authz.StaffActorReference) error { called = true; return nil }
	require.ErrorIs(t, guard.WithCurrentStaffActor(ctx, scope, action), authz.ErrScopedAuthorizationUnavailable)
	err := db.WithGuardedTenantCommand(ctx, scope.TenantID, time.Now().Add(time.Minute), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
		return guard.WithCurrentStaffActor(commandCtx, scope, action)
	})
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	require.False(t, called, "an arbitrary tenant transaction is not current IAM source-use authority")
}

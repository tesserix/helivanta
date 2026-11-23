package workforce_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // External test verifies the Workforce-owned permission contract.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

func TestHospitalStocktakeActorPermissionHasOneOwnerWithoutLegacyGrants(t *testing.T) {
	count := 0
	for _, grant := range workforce.New().Permissions() {
		if grant.Permission == authz.Permission("workforce.stocktake_actor.use") {
			count++
			require.Empty(t, grant.Roles, "legacy roles cannot establish current hospital stocktake Staff eligibility")
		}
	}
	require.Equal(t, 1, count, "hospital stocktake Staff source-use must be declared exactly once by Workforce")
}

func TestHospitalStocktakeActorSourceRejectsCommandsWithoutCurrentIAMEvidence(t *testing.T) {
	module := workforce.New()
	_, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{module}})
	guard := module.StocktakeActors(db)
	scope := authz.StaffActorScope{TenantID: testutil.TenantA, Subject: "TEST-unbound-counter", HospitalID: uuid.Must(uuid.NewV7())}
	called := false
	action := func(context.Context, authz.StaffActorReference) error { called = true; return nil }
	require.ErrorIs(t, guard.WithCurrentStocktakeActor(ctx, scope, action), authz.ErrScopedAuthorizationUnavailable)
	err := db.WithGuardedTenantCommand(ctx, scope.TenantID, time.Now().Add(time.Minute), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
		return guard.WithCurrentStocktakeActor(commandCtx, scope, action)
	})
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	require.False(t, called, "a generic guarded transaction must not manufacture the current stocktake source-use grant")
}

func TestHospitalStocktakeActorCannotManufactureSourceAuthority(t *testing.T) {
	guard := workforce.New().StocktakeActors(nil)
	_, governance := guard.(authz.CurrentStaffActorGuard)
	_, procurement := guard.(authz.CurrentPurchaseOrderActorGuard)
	require.False(t, governance)
	require.False(t, procurement)
	called := false
	err := guard.WithCurrentStocktakeActor(context.Background(), authz.StaffActorScope{}, func(context.Context, authz.StaffActorReference) error {
		called = true
		return nil
	})
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	require.False(t, called)
}

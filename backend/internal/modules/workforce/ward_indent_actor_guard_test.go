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
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestWardIndentActorPermissionHasOneOwnerWithoutLegacyGrants(t *testing.T) {
	count := 0
	for _, grant := range workforce.New().Permissions() {
		if grant.Permission == authz.Permission("workforce.ward_indent_actor.use") {
			count++
			require.Empty(t, grant.Roles, "legacy roles cannot establish current ward indent Staff eligibility")
		}
	}
	require.Equal(t, 1, count, "ward indent Staff source-use must be declared exactly once by Workforce")
}

func TestWardIndentActorSourceRejectsCommandsWithoutCurrentIAMEvidence(t *testing.T) {
	module := workforce.New()
	_, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{module}})
	guard := module.WardIndentActors(db)
	scope := authz.StaffActorScope{TenantID: testutil.TenantA, Subject: "TEST-unbound-requester", HospitalID: uuid.Must(uuid.NewV7())}
	called := false
	action := func(context.Context, authz.StaffActorReference) error { called = true; return nil }
	require.ErrorIs(t, guard.WithCurrentWardIndentActor(ctx, scope, action), authz.ErrScopedAuthorizationUnavailable)
	err := db.WithGuardedTenantCommand(ctx, scope.TenantID, time.Now().Add(time.Minute), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
		return guard.WithCurrentWardIndentActor(commandCtx, scope, action)
	})
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	require.False(t, called, "a generic guarded transaction must not manufacture the current ward indent source-use grant")
}

func TestWardIndentActorCannotStandInForAnotherPurpose(t *testing.T) {
	guard := workforce.New().WardIndentActors(nil)
	_, governance := guard.(authz.CurrentStaffActorGuard)
	_, procurement := guard.(authz.CurrentPurchaseOrderActorGuard)
	_, stocktake := guard.(authz.CurrentStocktakeActorGuard)
	require.False(t, governance)
	require.False(t, procurement)
	require.False(t, stocktake)
	_, wardIndent := workforce.New().StocktakeActors(nil).(authz.CurrentWardIndentActorGuard)
	require.False(t, wardIndent, "the stocktake purpose cannot authorize an indent")
	called := false
	err := guard.WithCurrentWardIndentActor(tenantdb.WithActor(context.Background(), "TEST-requester"), authz.StaffActorScope{}, func(context.Context, authz.StaffActorReference) error {
		called = true
		return nil
	})
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	require.False(t, called)
}

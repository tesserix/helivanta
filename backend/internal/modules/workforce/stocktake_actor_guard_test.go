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

func TestHospitalStocktakeActorHistoryCannotManufactureAuthority(t *testing.T) {
	guard, ok := workforce.New().StocktakeActors(nil).(authz.StocktakeActorHistoryGuard)
	require.True(t, ok, "Workforce must own retained hospital stocktake identity checks")
	reference := func() authz.StaffActorReference {
		return authz.StaffActorReference{StaffID: uuid.New(), LinkID: uuid.Must(uuid.NewV7()), LinkVersion: 2, GenerationID: uuid.Must(uuid.NewV7()), EmploymentEpoch: 2}
	}
	current, prior := reference(), reference()
	called := false
	scope := authz.StaffActorScope{TenantID: testutil.TenantA, Subject: "TEST-current-reviewer", HospitalID: uuid.New(), DepartmentPath: []string{uuid.Must(uuid.NewV7()).String()}}
	err := guard.WithStocktakeActorHistoryReview(tenantdb.WithActor(context.Background(), scope.Subject), scope, current, authz.StocktakeActorObservation{Subject: "TEST-retired-counter", Reference: prior}, func(context.Context) error { called = true; return nil })
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	require.False(t, called, "retained source data cannot establish a current review command")
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

func TestHospitalStocktakeActorHistoryMigrationIsForwardOnly(t *testing.T) {
	var history, index int
	for _, migration := range workforce.New().Migrations() {
		if migration.ID == "0122_workforce_stocktake_actor_history" {
			history++
			require.NotEmpty(t, migration.SQL)
		}
		if migration.ID == "0123_workforce_stocktake_actor_history_index" {
			index++
			require.Len(t, migration.ConcurrentIndexes, 1)
			require.Equal(t, "workforce_login_links", migration.ConcurrentIndexes[0].Table)
		}
	}
	require.Equal(t, 1, history)
	require.Equal(t, 1, index)
}

func TestStocktakeHospitalNamesCannotBypassHeldReadAndStaffSource(t *testing.T) {
	reader, ok := workforce.New().StocktakeActors(nil).(interface {
		StocktakeHospitalName(context.Context, authz.StaffActorScope) (string, error)
	})
	require.True(t, ok, "Workforce owns the narrow hospital display-name projection")
	name, err := reader.StocktakeHospitalName(t.Context(), authz.StaffActorScope{})
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
	require.Empty(t, name)
}

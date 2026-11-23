package platform_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/organization"
	"github.com/tesserix/helivanta/internal/modules/workforce"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestStocktakeActorsRequiresOneWorkforceOwner(t *testing.T) {
	db := &tenantdb.DB{}
	owner := workforce.New()
	for _, scenario := range []struct {
		name     string
		modules  []platform.Module
		database *tenantdb.DB
	}{
		{"missing database", []platform.Module{owner}, nil},
		{"missing owner", []platform.Module{organization.New()}, db},
		{"duplicate owner", []platform.Module{owner, workforce.New()}, db},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			require.Nil(t, platform.StocktakeActors(scenario.modules, scenario.database))
		})
	}
	guard := platform.StocktakeActors([]platform.Module{organization.New(), owner}, db)
	require.NotNil(t, guard)
	called := false
	require.ErrorIs(t, guard.WithCurrentStocktakeActor(context.Background(), authz.StaffActorScope{}, func(context.Context, authz.StaffActorReference) error {
		called = true
		return nil
	}), authz.ErrScopedAuthorizationUnavailable)
	require.False(t, called)
	require.ErrorIs(t, guard.WithStocktakeActorHistoryReview(context.Background(), authz.StaffActorScope{}, authz.StaffActorReference{}, authz.StocktakeActorObservation{}, func(context.Context) error {
		called = true
		return nil
	}), authz.ErrScopedAuthorizationUnavailable)
	require.False(t, called)
	_, procurement := any(guard).(authz.CurrentPurchaseOrderActorGuard)
	require.False(t, procurement, "physical inventory must not borrow procurement source authority")
}

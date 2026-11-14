package workforce_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // External test exercises its own module.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestGovernanceTopologyCountsOnlyHospitalsAndBindsTheExactEmployer(t *testing.T) {
	module := workforce.New()
	_, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{module}})
	first, second, pharmacy := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if err := governanceTopologyFixtureActor(tx); err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workforce_hospitals(tenant_id,id,name,establishment_kind,pharmacy_ownership_type) VALUES(?,?,'Synthetic first hospital','hospital',NULL),(?,?,'Synthetic second hospital','hospital',NULL),(?,?,'Synthetic independent pharmacy','pharmacy','independent')`, testutil.TenantA, first, testutil.TenantA, second, testutil.TenantA, pharmacy).Error
	}))
	guard := platform.GovernanceTopology([]platform.Module{module}, db)
	ctx = tenantdb.WithActor(ctx, "synthetic-governance-owner")
	scope := authz.GovernanceEstablishmentScope{TenantID: testutil.TenantA, Kind: "hospital", ID: &first}
	calls := 0
	err := db.WithGuardedTenantCommand(ctx, testutil.TenantA, time.Now().Add(time.Minute), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
		return guard.WithGovernanceTopology(commandCtx, scope, func(heldCtx context.Context, ref authz.GovernanceTopologyReference) error {
			calls++
			require.EqualValues(t, 2, ref.HospitalCount, "independent pharmacy roots are not hospitals")
			var exclusive bool
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				return tx.Raw(`SELECT pg_try_advisory_xact_lock(hashtextextended(?,0))`, "workforce.governance_topology:"+testutil.TenantA).Scan(&exclusive).Error
			}))
			require.False(t, exclusive, "the source retains the topology gate throughout the consuming command")
			require.ErrorIs(t, db.WithTenantCommand(heldCtx, testutil.TenantB, func(*gorm.DB) error { return nil }), tenantdb.ErrGuardedCommandScope)
			return nil
		})
	})
	require.NoError(t, err)
	require.Equal(t, 1, calls)
	for _, admitted := range []authz.GovernanceEstablishmentScope{
		{TenantID: testutil.TenantA, Kind: "pharmacy", ID: &pharmacy},
		{TenantID: testutil.TenantA, Kind: "tenant"},
	} {
		require.NoError(t, db.WithGuardedTenantCommand(ctx, testutil.TenantA, time.Now().Add(time.Minute), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
			return guard.WithGovernanceTopology(commandCtx, admitted, func(context.Context, authz.GovernanceTopologyReference) error { return nil })
		}))
	}
	for _, kind := range []string{"hospital", "supplier", "tenant"} {
		bad := authz.GovernanceEstablishmentScope{TenantID: testutil.TenantA, Kind: kind, ID: &pharmacy}
		err := db.WithGuardedTenantCommand(ctx, testutil.TenantA, time.Now().Add(time.Minute), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
			return guard.WithGovernanceTopology(commandCtx, bad, func(context.Context, authz.GovernanceTopologyReference) error { calls++; return nil })
		})
		require.Error(t, err, "a root ID cannot cross employer kinds or stand in for tenant scope")
	}
	require.Equal(t, 1, calls)
}

func TestGovernanceTopologyFailsClosedWithoutALiveTenantCommand(t *testing.T) {
	module := workforce.New()
	_, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{module}})
	guard := platform.GovernanceTopology([]platform.Module{module}, db)
	scope := authz.GovernanceEstablishmentScope{TenantID: testutil.TenantA, Kind: "tenant"}
	called := false
	action := func(context.Context, authz.GovernanceTopologyReference) error { called = true; return nil }
	require.ErrorIs(t, guard.WithGovernanceTopology(ctx, scope, action), authz.ErrScopedAuthorizationUnavailable)
	err := db.WithGuardedTenantCommand(ctx, testutil.TenantA, time.Now().Add(time.Minute), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
		return guard.WithGovernanceTopology(commandCtx, scope, action)
	})
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable, "a current attributable actor is required")
	require.False(t, called)
	for _, unavailable := range []authz.CurrentGovernanceTopologyGuard{
		platform.GovernanceTopology(nil, db),
		platform.GovernanceTopology([]platform.Module{module, module}, db),
		platform.GovernanceTopology([]platform.Module{module}, nil),
	} {
		require.ErrorIs(t, unavailable.WithGovernanceTopology(ctx, scope, action), authz.ErrScopedAuthorizationUnavailable)
	}
	require.False(t, called)
}

func TestGovernanceTopologyRejectsForeignOrMissingEmployerAndPreservesCallbackFailure(t *testing.T) {
	module := workforce.New()
	_, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{module}})
	foreign := uuid.Must(uuid.NewV7())
	require.NoError(t, db.WithTenant(ctx, testutil.TenantB, func(tx *gorm.DB) error {
		if err := governanceTopologyFixtureActor(tx); err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO workforce_hospitals(tenant_id,id,name) VALUES(?,?,'Synthetic foreign hospital')`, testutil.TenantB, foreign).Error
	}))
	ctx = tenantdb.WithActor(ctx, "synthetic-governance-owner")
	guard := platform.GovernanceTopology([]platform.Module{module}, db)
	for _, id := range []uuid.UUID{foreign, uuid.Must(uuid.NewV7())} {
		scope := authz.GovernanceEstablishmentScope{TenantID: testutil.TenantA, Kind: "hospital", ID: &id}
		called := false
		err := db.WithGuardedTenantCommand(ctx, testutil.TenantA, time.Now().Add(time.Minute), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
			return guard.WithGovernanceTopology(commandCtx, scope, func(context.Context, authz.GovernanceTopologyReference) error { called = true; return nil })
		})
		require.ErrorIs(t, err, authz.ErrGovernanceScopeHidden)
		require.False(t, called)
	}
	failure := errors.New("synthetic consuming command failure")
	err := db.WithGuardedTenantCommand(ctx, testutil.TenantA, time.Now().Add(time.Minute), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
		return guard.WithGovernanceTopology(commandCtx, authz.GovernanceEstablishmentScope{TenantID: testutil.TenantA, Kind: "tenant"}, func(context.Context, authz.GovernanceTopologyReference) error { return failure })
	})
	require.ErrorIs(t, err, failure)
}

func TestGovernanceTopologyWritersRetainTheExclusiveGateBeforeCommit(t *testing.T) {
	module := workforce.New()
	_, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Modules: []platform.Module{module}})
	id := uuid.Must(uuid.NewV7())
	for _, command := range []string{
		`INSERT INTO workforce_hospitals(tenant_id,id,name) VALUES(?,?,'Synthetic topology writer')`,
		`UPDATE workforce_hospitals SET updated_at=updated_at WHERE tenant_id=? AND id=?`,
		`DELETE FROM workforce_hospitals WHERE tenant_id=? AND id=?`,
	} {
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			if err := governanceTopologyFixtureActor(tx); err != nil {
				return err
			}
			if err := tx.Exec(command, testutil.TenantA, id).Error; err != nil {
				return err
			}
			var shared bool
			if err := db.WithTenant(ctx, testutil.TenantA, func(observer *gorm.DB) error {
				return observer.Raw(`SELECT pg_try_advisory_xact_lock_shared(hashtextextended(?,0))`, "workforce.governance_topology:"+testutil.TenantA).Scan(&shared).Error
			}); err != nil {
				return err
			}
			require.False(t, shared, "each real root writer holds the same exclusive gate until commit")
			return nil
		}))
	}
}

func governanceTopologyFixtureActor(tx *gorm.DB) error {
	return tx.Exec(`SELECT set_config('app.workforce_subject','synthetic-governance-fixture',true)`).Error
}

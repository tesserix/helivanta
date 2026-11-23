package globalapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/workforce"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func assertHospitalStocktakeActorSource(t *testing.T, ctx context.Context, db *tenantdb.DB, authority authz.ScopedAuthorizer, scope authz.StaffActorScope, expected authz.StaffActorReference, governanceUse func(func(context.Context, authz.StaffActorReference) error) error) {
	t.Helper()
	guard := workforce.New().StocktakeActors(db)
	called := false
	// The separate working governance assignment remains current, but its
	// callback does not carry the stocktake source-use grant.
	require.NoError(t, governanceUse(func(governanceCtx context.Context, _ authz.StaffActorReference) error {
		err := guard.WithCurrentStocktakeActor(governanceCtx, scope, func(context.Context, authz.StaffActorReference) error { called = true; return nil })
		require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
		return nil
	}))
	require.False(t, called)
	request := authz.ScopeRequest{TenantID: scope.TenantID, Subject: scope.Subject, HospitalID: scope.HospitalID.String(), DepartmentPath: scope.DepartmentPath, Permission: authz.StocktakeActorUse}
	held, err := authority.AuthorizeScope(ctx, request)
	require.NoError(t, err)
	require.NotEmpty(t, held.AssignmentID)
	require.Equal(t, "department", held.ScopeKind)
	require.Equal(t, scope.DepartmentPath[0], held.DepartmentID)
	compound, ok := authority.(authz.CompoundScopedCommandGuard)
	require.True(t, ok)
	use := func(callback func(context.Context, authz.StaffActorReference) error) error {
		return compound.WithScopeGrants(ctx, []authz.ScopeCheck{{Request: request, Grant: held}}, time.Now(), func(commandCtx context.Context) error {
			return guard.WithCurrentStocktakeActor(commandCtx, scope, callback)
		})
	}
	var escaped context.Context
	require.NoError(t, use(func(commandCtx context.Context, identity authz.StaffActorReference) error {
		escaped = commandCtx
		require.Equal(t, expected, identity)
		require.NotEqual(t, uuid.Nil, identity.LinkID)
		require.NotEqual(t, uuid.Nil, identity.GenerationID)
		require.GreaterOrEqual(t, identity.LinkVersion, int64(2))
		require.Positive(t, identity.EmploymentEpoch)
		for _, changed := range []authz.StaffActorScope{
			{TenantID: "22222222-2222-4222-8222-222222222222", Subject: scope.Subject, HospitalID: scope.HospitalID, DepartmentPath: scope.DepartmentPath},
			{TenantID: scope.TenantID, Subject: "TEST-another-counter", HospitalID: scope.HospitalID, DepartmentPath: scope.DepartmentPath},
			{TenantID: scope.TenantID, Subject: scope.Subject, HospitalID: uuid.Must(uuid.NewV7()), DepartmentPath: scope.DepartmentPath},
			{TenantID: scope.TenantID, Subject: scope.Subject, HospitalID: scope.HospitalID, DepartmentPath: []string{uuid.Must(uuid.NewV7()).String()}},
			{TenantID: scope.TenantID, Subject: scope.Subject, HospitalID: scope.HospitalID},
		} {
			called := false
			err := guard.WithCurrentStocktakeActor(commandCtx, changed, func(context.Context, authz.StaffActorReference) error { called = true; return nil })
			require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
			require.False(t, called)
		}
		for _, wrong := range []func(context.Context, authz.StaffActorScope, func(context.Context, authz.StaffActorReference) error) error{
			workforce.New().StaffActors(db).WithCurrentStaffActor,
			workforce.New().PurchaseOrderActors(db).WithCurrentPurchaseOrderActor,
		} {
			called := false
			err := wrong(commandCtx, scope, func(context.Context, authz.StaffActorReference) error { called = true; return nil })
			require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
			require.False(t, called)
		}
		return nil
	}))
	called = false
	require.ErrorIs(t, guard.WithCurrentStocktakeActor(escaped, scope, func(context.Context, authz.StaffActorReference) error { called = true; return nil }), authz.ErrScopedAuthorizationUnavailable)
	require.False(t, called)
	audits := func() int64 {
		var count int64
		require.NoError(t, db.WithTenant(ctx, scope.TenantID, func(tx *gorm.DB) error {
			return tx.Table("workforce_audit").Where("hospital_id=? AND actor_subject=? AND action=? AND resource_id=?", scope.HospitalID, scope.Subject, string(authz.StocktakeActorUse), expected.StaffID).Count(&count).Error
		}))
		return count
	}
	require.EqualValues(t, 1, audits())
	rollback := errors.New("TEST hospital stocktake consumer rollback")
	require.ErrorIs(t, use(func(context.Context, authz.StaffActorReference) error { return rollback }), rollback)
	require.EqualValues(t, 1, audits(), "failed consumer work must roll back its Workforce source audit too")
}

func assertHospitalStocktakeActorHistory(t *testing.T, ctx context.Context, db *tenantdb.DB, authority authz.ScopedAuthorizer, scope authz.StaffActorScope, current authz.StaffActorReference, participant authz.StocktakeActorObservation) {
	t.Helper()
	require.NotEmpty(t, participant.Subject)
	require.NotEmpty(t, scope.DepartmentPath)
	guard, ok := workforce.New().StocktakeActors(db).(authz.StocktakeActorHistoryGuard)
	require.True(t, ok)
	compound, ok := authority.(authz.CompoundScopedCommandGuard)
	require.True(t, ok)
	ctx = tenantdb.WithActor(ctx, scope.Subject)
	var checks []authz.ScopeCheck
	for _, permission := range []authz.Permission{authz.StocktakeActorUse, "pharmacy.stocktake.review"} {
		request := authz.ScopeRequest{TenantID: scope.TenantID, Subject: scope.Subject, HospitalID: scope.HospitalID.String(), DepartmentPath: scope.DepartmentPath, Permission: permission}
		grant, err := authority.AuthorizeScope(ctx, request)
		require.NoError(t, err)
		require.Equal(t, "department", grant.ScopeKind)
		checks = append(checks, authz.ScopeCheck{Request: request, Grant: grant})
	}
	use := func(proofs []authz.ScopeCheck, expected authz.StaffActorReference, observed authz.StocktakeActorObservation, callback func(context.Context) error) error {
		return compound.WithScopeGrants(ctx, proofs, time.Now(), func(commandCtx context.Context) error {
			return guard.WithStocktakeActorHistoryReview(commandCtx, scope, expected, observed, callback)
		})
	}
	called := false
	require.ErrorIs(t, use(checks[:1], current, participant, func(context.Context) error { called = true; return nil }), authz.ErrScopedAuthorizationUnavailable)
	require.False(t, called, "source eligibility alone cannot review a historical participant")
	for _, change := range []string{"subject", "staff", "link", "version", "generation", "epoch", "self"} {
		observed := participant
		switch change {
		case "subject":
			observed.Subject = "TEST-unrelated-historical-account"
		case "staff":
			observed.Reference.StaffID = uuid.Must(uuid.NewV7())
		case "link":
			observed.Reference.LinkID = uuid.Must(uuid.NewV7())
		case "version":
			observed.Reference.LinkVersion++
		case "generation":
			observed.Reference.GenerationID = uuid.Must(uuid.NewV7())
		case "epoch":
			observed.Reference.EmploymentEpoch++
		case "self":
			observed = authz.StocktakeActorObservation{Subject: scope.Subject, Reference: current}
		}
		called = false
		require.ErrorIs(t, use(checks, current, observed, func(context.Context) error { called = true; return nil }), authz.ErrScopedCommandDenied, change)
		require.False(t, called, change)
	}
	audits := func() int64 {
		var count int64
		require.NoError(t, db.WithTenant(ctx, scope.TenantID, func(tx *gorm.DB) error {
			return tx.Table("workforce_audit").Where("hospital_id=? AND actor_subject=? AND action=? AND resource_id=?", scope.HospitalID, scope.Subject, "workforce.stocktake_actor.review_history", current.StaffID).Count(&count).Error
		}))
		return count
	}
	before := audits()
	var escaped context.Context
	require.NoError(t, use(checks, current, participant, func(commandCtx context.Context) error {
		called = true
		escaped = commandCtx
		return db.WithTenantCommand(commandCtx, scope.TenantID, func(tx *gorm.DB) error {
			var mode string
			if err := tx.Raw(`SELECT COALESCE(current_setting('app.workforce_stocktake_history',true),'')`).Scan(&mode).Error; err != nil {
				return err
			}
			require.Empty(t, mode, "consumer must not inherit Workforce history authority")
			return nil
		})
	}))
	require.True(t, called)
	require.Equal(t, before+1, audits())
	called = false
	require.ErrorIs(t, guard.WithStocktakeActorHistoryReview(escaped, scope, current, participant, func(context.Context) error { called = true; return nil }), authz.ErrScopedAuthorizationUnavailable)
	require.False(t, called)
	rollback := errors.New("TEST hospital stocktake history consumer rollback")
	require.ErrorIs(t, use(checks, current, participant, func(context.Context) error { return rollback }), rollback)
	require.Equal(t, before+1, audits(), "consumer failure must roll back history evidence")
	stale := current
	stale.LinkVersion++
	called = false
	require.ErrorIs(t, use(checks, stale, participant, func(context.Context) error { called = true; return nil }), authz.ErrScopedCommandDenied)
	require.False(t, called)
	require.Equal(t, before+1, audits())
}

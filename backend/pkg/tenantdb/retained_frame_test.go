package tenantdb_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type retainedState struct {
	Tenant string
	Actor  string
	PID    int `gorm:"column:pid"`
}

func retainedScope(tx *gorm.DB) (retainedState, error) {
	var state retainedState
	err := tx.Raw(`SELECT coalesce(current_setting('app.tenant_id',true),'') AS tenant, coalesce(current_setting(?,true),'') AS actor, pg_backend_pid() AS pid`, tenantdb.ActorSetting).Scan(&state).Error
	return state, err
}

func TestRetainedFramesReuseOneBackendAndRestoreForcedRLS(t *testing.T) {
	db := openMigrated(t)
	tenants := []string{uuid.NewString(), uuid.NewString(), ""}
	for _, tenant := range tenants[:2] {
		require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
			return tx.Create(&widget{TenantID: uuid.MustParse(tenant), Name: tenant}).Error
		}))
	}
	pid := 0
	var descend func(context.Context, int) error
	descend = func(ctx context.Context, index int) error {
		tenant := tenants[index%len(tenants)]
		actor := fmt.Sprintf("TEST-source-%d", index)
		check := func(tx *gorm.DB) error {
			state, err := retainedScope(tx)
			if err != nil {
				return err
			}
			if pid == 0 {
				pid = state.PID
			}
			if state != (retainedState{Tenant: tenant, Actor: actor, PID: pid}) {
				return fmt.Errorf("frame %d did not restore scope: %+v", index, state)
			}
			var rows []widget
			if err := tx.Find(&rows).Error; err != nil {
				return err
			}
			if tenant == "" && len(rows) != 0 || tenant != "" && (len(rows) != 1 || rows[0].TenantID.String() != tenant) {
				return errors.New("retained frame crossed forced RLS")
			}
			return nil
		}
		use := func(next context.Context, tx *gorm.DB) error {
			if err := check(tx); err != nil {
				return err
			}
			if index < 999 {
				if err := descend(next, index+1); err != nil {
					return err
				}
			}
			return check(tx)
		}
		ctx = tenantdb.WithActor(ctx, actor)
		if tenant == "" {
			return db.WithRetainedSystemFrame(ctx, use)
		}
		return db.WithRetainedTenantFrame(ctx, tenant, use)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	require.NoError(t, descend(ctx, 0), "1,000 nested tenant/global source frames must use one app connection")
	require.Positive(t, pid)
}

func TestRetainedFramesHoldPriorSourceLocksUntilContinuationFinishes(t *testing.T) {
	db := openMigrated(t)
	a, b := uuid.NewString(), uuid.NewString()
	for _, tenant := range []string{a, b} {
		require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
			return tx.Create(&widget{TenantID: uuid.MustParse(tenant), Name: "TEST-source"}).Error
		}))
	}
	update := func(tenant string) error {
		return db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
			if err := tx.Exec(`SET LOCAL lock_timeout='40ms'`).Error; err != nil {
				return err
			}
			return tx.Exec(`UPDATE widgets SET name='TEST-updated'`).Error
		})
	}
	lock := func(tx *gorm.DB) error { return tx.Exec(`SELECT id FROM widgets FOR SHARE`).Error }
	require.NoError(t, db.WithRetainedTenantFrame(t.Context(), a, func(ctx context.Context, tx *gorm.DB) error {
		if err := lock(tx); err != nil {
			return err
		}
		if err := db.WithRetainedTenantFrame(ctx, b, func(ctx context.Context, tx *gorm.DB) error {
			if err := lock(tx); err != nil {
				return err
			}
			return db.WithRetainedSystemFrame(ctx, func(context.Context, *gorm.DB) error {
				require.ErrorContains(t, update(a), "lock timeout")
				require.ErrorContains(t, update(b), "lock timeout")
				return nil
			})
		}); err != nil {
			return err
		}
		require.ErrorContains(t, update(b), "lock timeout", "unwinding a source frame must not release its row locks")
		return nil
	}))
	require.NoError(t, update(a))
	require.NoError(t, update(b))
}

func TestRetainedFramesRollbackBothTenantsOnFailure(t *testing.T) {
	db := openMigrated(t)
	a, b := uuid.NewString(), uuid.NewString()
	stop := errors.New("TEST late source rejection")
	require.ErrorIs(t, db.WithRetainedTenantFrame(t.Context(), a, func(ctx context.Context, tx *gorm.DB) error {
		if err := tx.Create(&widget{TenantID: uuid.MustParse(a), Name: "TEST-rollback"}).Error; err != nil {
			return err
		}
		return db.WithRetainedTenantFrame(ctx, b, func(_ context.Context, tx *gorm.DB) error {
			if err := tx.Create(&widget{TenantID: uuid.MustParse(b), Name: "TEST-rollback"}).Error; err != nil {
				return err
			}
			return stop
		})
	}), stop)
	for _, tenant := range []string{a, b} {
		require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
			var rows []widget
			if err := tx.Find(&rows).Error; err != nil {
				return err
			}
			require.Empty(t, rows)
			return nil
		}))
	}
}

func TestRetainedFramesRejectWrongDBEscapedAndConcurrentContexts(t *testing.T) {
	db, other := openMigrated(t), openMigrated(t)
	tenant := uuid.NewString()
	var escaped, child context.Context
	called := func(context.Context, *gorm.DB) error { t.Error("rejected frame must not execute"); return nil }
	require.ErrorIs(t, db.WithRetainedTenantFrame(t.Context(), "invalid", called), tenantdb.ErrInvalidTenant)
	require.ErrorIs(t, db.WithRetainedSystemFrame(t.Context(), nil), tenantdb.ErrRetainedFrameScope)
	require.NoError(t, db.WithRetainedTenantFrame(t.Context(), tenant, func(ctx context.Context, tx *gorm.DB) error {
		escaped = ctx
		require.ErrorIs(t, other.WithRetainedTenantFrame(ctx, tenant, called), tenantdb.ErrRetainedFrameScope)
		entered, finish, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
		go func() {
			done <- db.WithRetainedSystemFrame(ctx, func(next context.Context, _ *gorm.DB) error {
				child = next
				close(entered)
				<-finish
				return nil
			})
		}()
		<-entered
		require.ErrorIs(t, db.WithRetainedTenantFrame(ctx, tenant, called), tenantdb.ErrRetainedFrameScope)
		close(finish)
		require.NoError(t, <-done)
		require.ErrorIs(t, db.WithRetainedTenantFrame(child, tenant, called), tenantdb.ErrRetainedFrameScope)
		state, err := retainedScope(tx)
		require.Equal(t, tenant, state.Tenant)
		return err
	}))
	require.ErrorIs(t, db.WithRetainedTenantFrame(escaped, tenant, called), tenantdb.ErrRetainedFrameScope)
}

func TestRetainedFramesCancelAnUnfinishedChild(t *testing.T) {
	db := openMigrated(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	entered, done := make(chan struct{}), make(chan error, 1)
	err := db.WithRetainedTenantFrame(ctx, uuid.NewString(), func(ctx context.Context, _ *gorm.DB) error {
		go func() {
			done <- db.WithRetainedSystemFrame(ctx, func(ctx context.Context, _ *gorm.DB) error {
				close(entered)
				<-ctx.Done()
				return ctx.Err()
			})
		}()
		<-entered
		return nil
	})
	require.ErrorIs(t, err, tenantdb.ErrRetainedFrameScope)
	require.ErrorIs(t, <-done, context.Canceled)
}

func TestRetainedFramesRestoreScopeAndRejectChildAfterPanic(t *testing.T) {
	db := openMigrated(t)
	tenant := uuid.NewString()
	var escaped context.Context
	require.NoError(t, db.WithRetainedTenantFrame(t.Context(), tenant, func(ctx context.Context, tx *gorm.DB) error {
		require.PanicsWithValue(t, "TEST source panic", func() {
			_ = db.WithRetainedSystemFrame(ctx, func(next context.Context, _ *gorm.DB) error {
				escaped = next
				panic("TEST source panic")
			})
		})
		require.ErrorIs(t, db.WithRetainedSystemFrame(escaped, func(context.Context, *gorm.DB) error { t.Error("escaped panic frame executed"); return nil }), tenantdb.ErrRetainedFrameScope)
		state, err := retainedScope(tx)
		require.Equal(t, tenant, state.Tenant)
		return err
	}))
}

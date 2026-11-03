package tenantdb_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestGuardedCommandRollsBackProductWritesWhenTheCommandFails(t *testing.T) {
	db := openMigrated(t)
	tenant := uuid.NewString()
	refused := errors.New("command failed after the product write")
	err := db.WithGuardedTenantCommand(t.Context(), tenant, time.Now().Add(time.Minute), func(*gorm.DB) error { return nil }, func(ctx context.Context) error {
		if err := db.WithTenantCommand(ctx, tenant, func(tx *gorm.DB) error {
			return tx.Create(&widget{TenantID: uuid.MustParse(tenant), Name: "synthetic-command"}).Error
		}); err != nil {
			return err
		}
		return refused
	})
	require.ErrorIs(t, err, refused)
	var count int64
	require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
		return tx.Model(&widget{}).Count(&count).Error
	}))
	require.Zero(t, count, "a failed authorization command must not leave independently committed product writes")
}

func TestSourceExpiryCancelsTheRootCommandEvenWhenTheOwnerIgnoresTheError(t *testing.T) {
	db := openMigrated(t)
	tenant := uuid.NewString()
	err := db.WithGuardedTenantCommand(t.Context(), tenant, time.Now().Add(time.Minute), func(*gorm.DB) error { return nil }, func(ctx context.Context) error {
		if err := db.WithTenantCommand(ctx, tenant, func(tx *gorm.DB) error {
			return tx.Create(&widget{TenantID: uuid.MustParse(tenant), Name: "expired-source"}).Error
		}); err != nil {
			return err
		}
		require.ErrorIs(t, db.ConstrainGuardedCommand(ctx, tenant, time.Now().Add(-time.Second)), tenantdb.ErrGuardedCommandExpired)
		return nil
	})
	require.ErrorIs(t, err, tenantdb.ErrGuardedCommandExpired)
	var count int64
	require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
		return tx.Model(&widget{}).Count(&count).Error
	}))
	require.Zero(t, count, "source expiry must cancel the shared commit, not only the owner's query context")
}

func TestLaterSourceCannotExtendTheEarlierCommandCutoff(t *testing.T) {
	db := openMigrated(t)
	tenant := uuid.NewString()
	err := db.WithGuardedTenantCommand(t.Context(), tenant, time.Now().Add(3*time.Second), func(*gorm.DB) error { return nil }, func(ctx context.Context) error {
		if err := db.WithTenantCommand(ctx, tenant, func(tx *gorm.DB) error {
			return tx.Create(&widget{TenantID: uuid.MustParse(tenant), Name: "earliest-source"}).Error
		}); err != nil {
			return err
		}
		require.NoError(t, db.ConstrainGuardedCommand(ctx, tenant, time.Now().Add(100*time.Millisecond)))
		require.NoError(t, db.ConstrainGuardedCommand(ctx, tenant, time.Now().Add(time.Minute)))
		<-ctx.Done()
		return ctx.Err()
	})
	require.ErrorIs(t, err, tenantdb.ErrGuardedCommandExpired)
	var count int64
	require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
		return tx.Model(&widget{}).Count(&count).Error
	}))
	require.Zero(t, count, "the earliest owner cutoff must roll back the shared write")
}

func TestExpiredSourceCancelsRootDespiteAnAlreadyCanceledOwnerContext(t *testing.T) {
	db := openMigrated(t)
	tenant := uuid.NewString()
	err := db.WithGuardedTenantCommand(t.Context(), tenant, time.Now().Add(time.Minute), func(*gorm.DB) error { return nil }, func(ctx context.Context) error {
		if err := db.WithTenantCommand(ctx, tenant, func(tx *gorm.DB) error {
			return tx.Create(&widget{TenantID: uuid.MustParse(tenant), Name: "canceled-owner-expired-source"}).Error
		}); err != nil {
			return err
		}
		ownerCtx, cancel := context.WithCancel(ctx)
		cancel()
		require.ErrorIs(t, db.ConstrainGuardedCommand(ownerCtx, tenant, time.Now().Add(-time.Second)), tenantdb.ErrGuardedCommandExpired)
		return nil
	})
	require.ErrorIs(t, err, tenantdb.ErrGuardedCommandExpired)
	var count int64
	require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
		return tx.Model(&widget{}).Count(&count).Error
	}))
	require.Zero(t, count)
}

func TestSourceCutoffRemainsArmedDuringDeferredCommit(t *testing.T) {
	db := openMigrated(t)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_deferred_commit_lock", SQL: `
CREATE FUNCTION widget_deferred_lock() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN PERFORM pg_advisory_xact_lock(hashtextextended(NEW.name,0)); RETURN NEW; END $$;
CREATE CONSTRAINT TRIGGER widget_deferred_lock AFTER INSERT ON widgets
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION widget_deferred_lock();`}}))
	tenant, lock := uuid.NewString(), uuid.NewString()
	ready, release := make(chan struct{}), make(chan struct{})
	holder := make(chan error, 1)
	var once sync.Once
	unlock := func() { once.Do(func() { close(release) }) }
	defer unlock()
	go func() {
		holder <- db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, lock).Error; err != nil {
				return err
			}
			close(ready)
			select {
			case <-release:
				return nil
			case <-t.Context().Done():
				return t.Context().Err()
			}
		})
	}()
	select {
	case <-ready:
	case err := <-holder:
		t.Fatalf("commit lock setup failed: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("commit lock did not become ready")
	}
	result, entered := make(chan error, 1), make(chan int, 1)
	go func() {
		result <- db.WithGuardedTenantCommand(t.Context(), tenant, time.Now().Add(10*time.Second), func(*gorm.DB) error { return nil }, func(ctx context.Context) error {
			return db.WithTenantCommand(ctx, tenant, func(tx *gorm.DB) error {
				if err := tx.Create(&widget{TenantID: uuid.MustParse(tenant), Name: lock}).Error; err != nil {
					return err
				}
				var pid int
				if err := tx.Raw(`SELECT pg_backend_pid()`).Scan(&pid).Error; err != nil {
					return err
				}
				if err := db.ConstrainGuardedCommand(ctx, tenant, time.Now().Add(2*time.Second)); err != nil {
					return err
				}
				entered <- pid
				return nil
			})
		})
	}()
	var pid int
	select {
	case pid = <-entered:
	case err := <-result:
		t.Fatalf("owner command did not reach commit: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("owner command did not return its work")
	}
	require.Eventually(t, func() bool {
		var blocked bool
		err := db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT cardinality(pg_blocking_pids(?))>0`, pid).Scan(&blocked).Error
		})
		return err == nil && blocked
	}, time.Second, 10*time.Millisecond, "the deferred trigger must be waiting after the action returned")
	select {
	case err := <-result:
		require.ErrorIs(t, err, tenantdb.ErrGuardedCommandExpired)
	case <-time.After(5 * time.Second):
		t.Fatal("the source cutoff did not cancel deferred commit")
	}
	unlock()
	require.NoError(t, <-holder)
	require.Eventually(t, func() bool {
		var finished bool
		err := db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT NOT EXISTS(SELECT 1 FROM pg_stat_activity WHERE pid=? AND xact_start IS NOT NULL)`, pid).Scan(&finished).Error
		})
		return err == nil && finished
	}, 2*time.Second, 10*time.Millisecond, "inspect persisted state only after the canceled backend transaction ends")
	var count int64
	require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
		return tx.Model(&widget{}).Count(&count).Error
	}))
	require.Zero(t, count, "a source cutoff must remain armed after the owner callback, through PostgreSQL commit")
}

func TestGuardedCommandRejectsOtherTenantAndEscapedContext(t *testing.T) {
	db := openMigrated(t)
	tenant, other := uuid.NewString(), uuid.NewString()
	require.False(t, db.GuardedCommandActive(t.Context()))
	require.ErrorIs(t, db.ConstrainGuardedCommand(t.Context(), tenant, time.Now()), tenantdb.ErrGuardedCommandScope)
	var escaped context.Context
	err := db.WithGuardedTenantCommand(t.Context(), tenant, time.Now().Add(time.Minute), func(*gorm.DB) error { return nil }, func(ctx context.Context) error {
		escaped = ctx
		require.True(t, db.GuardedCommandActive(ctx))
		require.False(t, (&tenantdb.DB{}).GuardedCommandActive(ctx))
		require.ErrorIs(t, db.ConstrainGuardedCommand(ctx, other, time.Now()), tenantdb.ErrGuardedCommandScope)
		require.ErrorIs(t, (&tenantdb.DB{}).ConstrainGuardedCommand(ctx, tenant, time.Now()), tenantdb.ErrGuardedCommandScope)
		require.ErrorIs(t, db.ConstrainGuardedCommand(ctx, tenant, time.Time{}), tenantdb.ErrGuardedCommandScope)
		err := db.WithTenantCommand(ctx, other, func(tx *gorm.DB) error {
			return tx.Create(&widget{TenantID: uuid.MustParse(other), Name: "wrong-scope"}).Error
		})
		require.ErrorIs(t, err, tenantdb.ErrGuardedCommandScope)
		return db.WithTenantCommand(ctx, tenant, func(tx *gorm.DB) error {
			return tx.Create(&widget{TenantID: uuid.MustParse(tenant), Name: "authorized-command"}).Error
		})
	})
	require.NoError(t, err)
	require.False(t, db.GuardedCommandActive(escaped))
	require.ErrorIs(t, db.ConstrainGuardedCommand(escaped, tenant, time.Now()), tenantdb.ErrGuardedCommandClosed)
	err = db.WithTenantCommand(escaped, tenant, func(tx *gorm.DB) error {
		return tx.Create(&widget{TenantID: uuid.MustParse(tenant), Name: "escaped-command"}).Error
	})
	require.ErrorIs(t, err, tenantdb.ErrGuardedCommandClosed)
	var names []string
	require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
		return tx.Model(&widget{}).Pluck("name", &names).Error
	}))
	require.Equal(t, []string{"authorized-command"}, names)
	var count int64
	require.NoError(t, db.WithTenant(t.Context(), other, func(tx *gorm.DB) error {
		return tx.Model(&widget{}).Count(&count).Error
	}))
	require.Zero(t, count)
}

func TestGuardedCommandExpiryRollsBackWhileWaitingForProductLock(t *testing.T) {
	db := openMigrated(t)
	tenant, lock := uuid.NewString(), uuid.NewString()
	ready, release := make(chan struct{}), make(chan struct{})
	holder := make(chan error, 1)
	var once sync.Once
	unlock := func() { once.Do(func() { close(release) }) }
	defer unlock()
	go func() {
		holder <- db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, lock).Error; err != nil {
				return err
			}
			close(ready)
			select {
			case <-release:
				return nil
			case <-t.Context().Done():
				return t.Context().Err()
			}
		})
	}()
	select {
	case <-ready:
	case err := <-holder:
		t.Fatalf("product lock setup failed: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("product lock did not become ready")
	}
	err := db.WithGuardedTenantCommand(t.Context(), tenant, time.Now().Add(time.Second), func(*gorm.DB) error { return nil }, func(ctx context.Context) error {
		return db.WithTenantCommand(ctx, tenant, func(tx *gorm.DB) error {
			if err := tx.Create(&widget{TenantID: uuid.MustParse(tenant), Name: "expired-command"}).Error; err != nil {
				return err
			}
			return tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, lock).Error
		})
	})
	unlock()
	require.NoError(t, <-holder)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	var count int64
	require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
		return tx.Model(&widget{}).Count(&count).Error
	}))
	require.Zero(t, count, "expiry must roll back a write that happened before waiting on the product lock")
}

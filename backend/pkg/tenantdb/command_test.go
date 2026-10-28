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

func TestGuardedCommandRejectsOtherTenantAndEscapedContext(t *testing.T) {
	db := openMigrated(t)
	tenant, other := uuid.NewString(), uuid.NewString()
	var escaped context.Context
	err := db.WithGuardedTenantCommand(t.Context(), tenant, time.Now().Add(time.Minute), func(*gorm.DB) error { return nil }, func(ctx context.Context) error {
		escaped = ctx
		err := db.WithTenantCommand(ctx, other, func(tx *gorm.DB) error {
			return tx.Create(&widget{TenantID: uuid.MustParse(other), Name: "wrong-scope"}).Error
		})
		require.ErrorIs(t, err, tenantdb.ErrGuardedCommandScope)
		return db.WithTenantCommand(ctx, tenant, func(tx *gorm.DB) error {
			return tx.Create(&widget{TenantID: uuid.MustParse(tenant), Name: "authorized-command"}).Error
		})
	})
	require.NoError(t, err)
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

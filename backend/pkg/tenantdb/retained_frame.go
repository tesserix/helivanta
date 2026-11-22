package tenantdb

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var ErrRetainedFrameScope = errors.New("tenantdb: retained source frame is unavailable")

type retainedFrameKey struct{}

type retainedLease struct {
	db     *DB
	tx     *gorm.DB
	cancel context.CancelFunc
	open   atomic.Bool
}

type retainedFrame struct {
	lease     *retainedLease
	available atomic.Bool
}

// WithRetainedTenantFrame retains source-owner locks while synchronous nested
// guards change tenant scope. It conveys no authorization and never joins an
// ordinary consumer transaction. Callers must propagate the callback's context.
func (d *DB) WithRetainedTenantFrame(ctx context.Context, tenant string, fn func(context.Context, *gorm.DB) error) error {
	if _, err := uuid.Parse(tenant); err != nil {
		return ErrInvalidTenant
	}
	return d.withRetainedFrame(ctx, tenant, fn)
}

// WithRetainedSystemFrame checks global source facts on the same app-role
// transaction as nested tenant source guards. Empty tenant scope cannot bypass RLS.
func (d *DB) WithRetainedSystemFrame(ctx context.Context, fn func(context.Context, *gorm.DB) error) error {
	return d.withRetainedFrame(ctx, "", fn)
}

func (d *DB) withRetainedFrame(ctx context.Context, tenant string, fn func(context.Context, *gorm.DB) error) error {
	if d == nil || ctx == nil || fn == nil {
		return ErrRetainedFrameScope
	}
	if frame, _ := ctx.Value(retainedFrameKey{}).(*retainedFrame); frame != nil {
		return d.useRetainedFrame(ctx, frame, tenant, fn)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	return d.WithSystem(ctx, func(tx *gorm.DB) error {
		lease := &retainedLease{db: d, tx: tx, cancel: cancel}
		lease.open.Store(true)
		defer lease.open.Store(false)
		frame := &retainedFrame{lease: lease}
		frame.available.Store(true)
		return d.useRetainedFrame(ctx, frame, tenant, fn)
	})
}

func (d *DB) useRetainedFrame(ctx context.Context, parent *retainedFrame, tenant string, fn func(context.Context, *gorm.DB) error) (err error) {
	lease := parent.lease
	if lease.db != d || !lease.open.Load() || !parent.available.CompareAndSwap(true, false) {
		return ErrRetainedFrameScope
	}
	defer parent.available.Store(true)
	if err := ctx.Err(); err != nil {
		return err
	}
	tx := lease.tx.WithContext(ctx)
	var prior struct{ Tenant, Actor string }
	if err := tx.Raw(`SELECT coalesce(current_setting('app.tenant_id',true),'') AS tenant, coalesce(current_setting(?,true),'') AS actor`, ActorSetting).Scan(&prior).Error; err != nil {
		return err
	}
	defer func() {
		restore := tx.Exec(`SELECT set_config('app.tenant_id',?,true),set_config(?,?,true)`, prior.Tenant, ActorSetting, prior.Actor).Error
		err = errors.Join(err, restore)
	}()
	if err := tx.Exec(`SELECT set_config('app.tenant_id',?,true),set_config(?,?,true)`, tenant, ActorSetting, ActorFrom(ctx)).Error; err != nil {
		return err
	}
	child := &retainedFrame{lease: lease}
	child.available.Store(true)
	defer child.available.Store(false)
	err = fn(context.WithValue(ctx, retainedFrameKey{}, child), tx)
	if !child.available.CompareAndSwap(true, false) {
		lease.cancel()
		return errors.Join(err, ErrRetainedFrameScope)
	}
	return errors.Join(err, ctx.Err())
}

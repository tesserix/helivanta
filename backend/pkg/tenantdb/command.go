package tenantdb

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrGuardedCommandScope  = errors.New("tenantdb: guarded command scope does not match")
	ErrGuardedCommandClosed = errors.New("tenantdb: guarded command has ended")
)

type guardedCommandKey struct{}

type guardedCommand struct {
	db     *DB
	tenant uuid.UUID
	tx     *gorm.DB
	active atomic.Bool
}

// GuardedCommandActive reports an active command on this owner database.
func (d *DB) GuardedCommandActive(ctx context.Context) bool {
	command, ok := ctx.Value(guardedCommandKey{}).(*guardedCommand)
	return ok && command.db == d && command.active.Load()
}

// WithGuardedTenantCommand keeps authorization locks and synchronous product work
// in one transaction. The action context must not escape the callback.
func (d *DB) WithGuardedTenantCommand(ctx context.Context, tenant string, until time.Time, verify func(*gorm.DB) error, action func(context.Context) error) error {
	id, err := uuid.Parse(tenant)
	if err != nil {
		return ErrInvalidTenant
	}
	if ctx.Value(guardedCommandKey{}) != nil || verify == nil || action == nil {
		return ErrGuardedCommandScope
	}
	if until.IsZero() || !time.Now().Before(until) {
		return context.DeadlineExceeded
	}
	ctx, cancel := context.WithDeadline(ctx, until)
	defer cancel()
	return d.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
		if err := verify(tx); err != nil {
			return err
		}
		command := &guardedCommand{db: d, tenant: id, tx: tx}
		command.active.Store(true)
		defer command.active.Store(false)
		if err := action(context.WithValue(ctx, guardedCommandKey{}, command)); err != nil {
			return err
		}
		return ctx.Err()
	})
}

// WithTenantCommand joins a guarded owner command or opens an ordinary tenant transaction.
func (d *DB) WithTenantCommand(ctx context.Context, tenant string, fn func(*gorm.DB) error) error {
	command, ok := ctx.Value(guardedCommandKey{}).(*guardedCommand)
	if ok {
		id, err := uuid.Parse(tenant)
		if err != nil {
			return ErrInvalidTenant
		}
		if command.db != d || command.tenant != id {
			return ErrGuardedCommandScope
		}
		if !command.active.Load() {
			return ErrGuardedCommandClosed
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return fn(command.tx.WithContext(ctx))
	}
	return d.WithTenant(ctx, tenant, fn)
}

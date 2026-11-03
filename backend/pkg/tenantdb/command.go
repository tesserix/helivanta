package tenantdb

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	ErrGuardedCommandScope   = errors.New("tenantdb: guarded command scope does not match")
	ErrGuardedCommandClosed  = errors.New("tenantdb: guarded command has ended")
	ErrGuardedCommandExpired = errors.New("tenantdb: guarded command source has expired")
)

type guardedCommandKey struct{}

type guardedCommand struct {
	db     *DB
	tenant uuid.UUID
	tx     *gorm.DB
	active atomic.Bool
	cancel context.CancelCauseFunc
	mu     sync.Mutex
	until  time.Time
	timer  *time.Timer
}

// ConstrainGuardedCommand limits the shared command to an owner's current source lifetime.
func (d *DB) ConstrainGuardedCommand(ctx context.Context, tenant string, until time.Time) error {
	command, ok := ctx.Value(guardedCommandKey{}).(*guardedCommand)
	id, err := uuid.Parse(tenant)
	if !ok || err != nil || command.db != d || command.tenant != id || until.IsZero() {
		return ErrGuardedCommandScope
	}
	command.mu.Lock()
	defer command.mu.Unlock()
	if !command.active.Load() {
		return ErrGuardedCommandClosed
	}
	if !time.Now().Before(until) {
		command.cancel(ErrGuardedCommandExpired)
		return ErrGuardedCommandExpired
	}
	if err := ctx.Err(); err != nil {
		if errors.Is(context.Cause(ctx), ErrGuardedCommandExpired) {
			return ErrGuardedCommandExpired
		}
		return err
	}
	if until.Before(command.until) {
		command.until = until
		if command.timer != nil {
			command.timer.Stop()
		}
		command.timer = time.AfterFunc(time.Until(until), func() { command.cancel(ErrGuardedCommandExpired) })
	}
	return nil
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
	ctx, cancelSource := context.WithCancelCause(ctx)
	defer cancelSource(nil)
	var command *guardedCommand
	defer func() {
		if command != nil {
			command.mu.Lock()
			defer command.mu.Unlock()
			if command.timer != nil {
				command.timer.Stop()
			}
		}
	}()
	err = d.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
		if err := verify(tx); err != nil {
			return err
		}
		deadline, _ := ctx.Deadline()
		command = &guardedCommand{db: d, tenant: id, tx: tx, cancel: cancelSource, until: deadline}
		command.active.Store(true)
		defer command.active.Store(false)
		if err := action(context.WithValue(ctx, guardedCommandKey{}, command)); err != nil {
			return err
		}
		command.active.Store(false)
		if err := ctx.Err(); err != nil {
			return err
		}
		// Deferred checks must finish before cancellation can leave an in-flight COMMIT.
		if err := tx.Exec(`SET CONSTRAINTS ALL IMMEDIATE`).Error; err != nil {
			return err
		}
		command.mu.Lock()
		cutoff := command.until
		command.mu.Unlock()
		var expired bool
		if err := tx.Raw(`SELECT clock_timestamp()>=?::timestamptz`, cutoff).Scan(&expired).Error; err != nil {
			return err
		}
		if expired {
			if cutoff.Before(deadline) {
				cancelSource(ErrGuardedCommandExpired)
				return ErrGuardedCommandExpired
			}
			return context.DeadlineExceeded
		}
		return ctx.Err()
	})
	if err != nil && errors.Is(context.Cause(ctx), ErrGuardedCommandExpired) {
		return ErrGuardedCommandExpired
	}
	return err
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

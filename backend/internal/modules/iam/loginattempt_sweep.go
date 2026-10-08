package iam

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
	"time"

	"gorm.io/gorm"
)

// This file is #869: login_attempt rows are swept once expired, so an
// abandoned sign-in cannot leave a live Zitadel session token in Postgres
// indefinitely. See docs/superpowers/specs/2026-10-08-login-attempt-sweep-design.md.

// attemptSweepInterval is how often RunAttemptSweeper deletes expired rows
// (spec D2). It follows loginAttemptTTL rather than the outbox pruner's hour:
// the property being bounded is how long a credential lingers at rest, so an
// abandoned token survives at most about two TTLs. One indexed DELETE on a
// table of a few rows per in-flight sign-in costs nothing at this cadence.
const attemptSweepInterval = loginAttemptTTL

// SweepExpired deletes every login_attempt row past its expires_at and
// reports how many it removed (spec D1). Stage and enrolling do not matter:
// Get refuses an expired row at any stage, so none of them is usable.
//
// "Expired" is judged on Postgres's clock (now()), the same clock BumpAndGet
// uses, so the sweep and the readers agree; a row that is still live is never
// touched, and a sweep racing a Get on the same row is benign — whichever
// deletes first, the other finds nothing. The WHERE clause is served by
// login_attempt_expires_at_idx (0004_iam).
func (s *loginAttemptStore) SweepExpired(ctx context.Context) (int64, error) {
	var deleted int64
	err := s.db.WithSystem(ctx, func(tx *gorm.DB) error {
		res := tx.Exec(`DELETE FROM login_attempt WHERE expires_at < now()`)
		if res.Error != nil {
			return res.Error
		}
		deleted = res.RowsAffected
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("sweep expired login attempts: %w", err)
	}
	return deleted, nil
}

// RunAttemptSweeper sweeps expired login attempts once at boot, then every
// attemptSweepInterval, until ctx ends (spec D2). cmd/api/main.go starts it
// with a bare `go`, beside bus.RunPruner. Every replica runs it; the DELETE
// is idempotent, so no coordination is needed.
func (h *LoginUIHandlers) RunAttemptSweeper(ctx context.Context) {
	runAttemptSweeper(ctx, h.store.SweepExpired, attemptSweepInterval)
}

// runAttemptSweeper is RunAttemptSweeper with the sweep and its interval as
// parameters, so the loop itself — boot pass, ticks, a failing pass, stop on
// ctx — is testable without waiting five minutes.
func runAttemptSweeper(ctx context.Context, sweep func(context.Context) (int64, error), interval time.Duration) {
	// At boot: rows left by a previous process (a crashed pod, a long gap
	// between deploys) do not wait a full interval.
	sweepAttemptsSafely(ctx, sweep)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sweepAttemptsSafely(ctx, sweep)
		}
	}
}

// sweepAttemptsSafely runs one pass (spec D3). A panic is contained to this
// pass — the loop runs on a bare goroutine, and an escaped panic would end
// the process — and an error is logged and the loop carries on. Failing in
// that direction is deliberate: a pass that cannot run leaves expired rows
// that Get already refuses, which is no bypass, while taking the API down
// would cost a hospital its sign-in to avoid a delay in deleting unusable
// rows. Every pass that runs logs its count, zero included, so an operator
// can tell a running sweep from a silent one.
func sweepAttemptsSafely(ctx context.Context, sweep func(context.Context) (int64, error)) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("login attempt sweep panic", "panic", fmt.Sprint(r), "stack", string(debug.Stack()))
		}
	}()
	deleted, err := sweep(ctx)
	if err != nil {
		slog.Error("login attempt sweep", "err", err)
		return
	}
	slog.Info("login attempt sweep", "deleted", deleted)
}

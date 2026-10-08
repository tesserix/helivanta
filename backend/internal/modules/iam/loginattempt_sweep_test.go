package iam

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// These tests pin #869
// (docs/superpowers/specs/2026-10-08-login-attempt-sweep-design.md): an
// expired login_attempt row is deleted by the sweep without ever being read,
// a live one is not, and the loop sweeps at boot, on every tick, past a
// failing pass, and stops with its context.

// insertAttemptRow writes a login_attempt row with plain SQL, bypassing Put
// and Get entirely, so a test proves the SWEEP removed it — not an
// expiry-on-read that happened along the way. expiresIn is relative to
// Postgres's own clock, the one the sweep compares against.
func insertAttemptRow(t *testing.T, store *loginAttemptStore, ctx context.Context, id string, stage attemptStage, enrolling bool, expiresIn time.Duration) {
	t.Helper()
	require.NoError(t, store.db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO login_attempt
			(auth_request_id, zitadel_session_id, zitadel_session_token, subject, expires_at, stage, enrolling)
			VALUES (?, 's', 'live-zitadel-session-token', 'u', now() + make_interval(secs => ?), ?, ?)`,
			id, expiresIn.Seconds(), stage, enrolling).Error
	}))
}

// The issue's own acceptance check: a row past its expires_at that nothing
// has read since is deleted, with its session token, by the next sweep.
func TestSweepDeletesAnExpiredRowNobodyRead(t *testing.T) {
	store, ctx := newTestLoginAttemptStore(t)
	insertAttemptRow(t, store, ctx, "V2_abandoned", stageFactor, false, -time.Minute)
	require.Equal(t, int64(1), countLoginAttemptRows(t, store, ctx, "V2_abandoned"), "precondition")

	deleted, err := store.SweepExpired(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)
	require.Zero(t, countLoginAttemptRows(t, store, ctx, "V2_abandoned"),
		"the abandoned row, and the Zitadel session token it carries, is still in Postgres")
}

// A sign-in in progress is never cut short by the sweep.
func TestSweepKeepsALiveRow(t *testing.T) {
	store, ctx := newTestLoginAttemptStore(t)
	insertAttemptRow(t, store, ctx, "V2_live", stageFactor, false, time.Minute)

	deleted, err := store.SweepExpired(ctx)
	require.NoError(t, err)
	require.Zero(t, deleted)
	require.Equal(t, int64(1), countLoginAttemptRows(t, store, ctx, "V2_live"), "the sweep deleted a live attempt")
}

// Expiry, not the step a row waits for, decides: an expired row is unusable
// at every stage, so every one is swept.
func TestSweepDeletesExpiredRowsAtEveryStage(t *testing.T) {
	store, ctx := newTestLoginAttemptStore(t)
	insertAttemptRow(t, store, ctx, "V2_factor", stageFactor, false, -time.Second)
	insertAttemptRow(t, store, ctx, "V2_enrol", stageFactor, true, -time.Second)
	insertAttemptRow(t, store, ctx, "V2_change", stagePasswordChange, false, -time.Second)
	insertAttemptRow(t, store, ctx, "V2_live", stagePasswordChange, false, time.Minute)

	deleted, err := store.SweepExpired(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(3), deleted)
	for _, id := range []string{"V2_factor", "V2_enrol", "V2_change"} {
		require.Zero(t, countLoginAttemptRows(t, store, ctx, id), "%s survived the sweep", id)
	}
	require.Equal(t, int64(1), countLoginAttemptRows(t, store, ctx, "V2_live"))
}

// The production entry point sweeps at boot against the real table: a row
// abandoned before the process started is gone without waiting an interval.
func TestRunAttemptSweeperSweepsTheRealTableAtBoot(t *testing.T) {
	store, ctx := newTestLoginAttemptStore(t)
	insertAttemptRow(t, store, ctx, "V2_left_by_last_pod", stageFactor, false, -time.Hour)
	h := &LoginUIHandlers{store: store}

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { h.RunAttemptSweeper(runCtx); close(done) }()

	require.Eventually(t, func() bool {
		return countLoginAttemptRows(t, store, ctx, "V2_left_by_last_pod") == 0
	}, 10*time.Second, 50*time.Millisecond, "the boot sweep did not delete a row left by a previous process")
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunAttemptSweeper did not return after its context ended")
	}
}

// The loop: one pass at boot (before any tick), one per tick after it, and
// it returns once its context ends.
func TestAttemptSweeperRunsAtBootThenEveryTickAndStops(t *testing.T) {
	var passes atomic.Int32
	sweep := func(context.Context) (int64, error) { passes.Add(1); return 0, nil }

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { runAttemptSweeper(ctx, sweep, time.Hour); close(done) }()
	require.Eventually(t, func() bool { return passes.Load() == 1 }, 2*time.Second, 5*time.Millisecond,
		"no sweep ran at boot: with an hour's interval, only the boot pass can have run")
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the sweeper did not return after its context ended")
	}

	passes.Store(0)
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	go runAttemptSweeper(ctx, sweep, 10*time.Millisecond)
	require.Eventually(t, func() bool { return passes.Load() >= 3 }, 2*time.Second, 5*time.Millisecond,
		"the sweeper did not keep sweeping on its ticks")
}

// A pass that errors or panics is logged and does not stop the loop: the
// next tick sweeps again (spec D3).
func TestAttemptSweeperSurvivesAFailingPass(t *testing.T) {
	logged := &lockedWriter{w: &bytes.Buffer{}}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(logged, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	var passes atomic.Int32
	sweep := func(context.Context) (int64, error) {
		switch passes.Add(1) {
		case 1:
			return 0, errors.New("database unreachable")
		case 2:
			panic("sweep exploded")
		default:
			return 2, nil
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go runAttemptSweeper(ctx, sweep, 10*time.Millisecond)

	require.Eventually(t, func() bool { return passes.Load() >= 3 }, 2*time.Second, 5*time.Millisecond,
		"a failing or panicking pass stopped the sweeper")
	cancel()
	out := logged.String()
	require.Contains(t, out, `"msg":"login attempt sweep","err":"database unreachable"`)
	require.Contains(t, out, `"msg":"login attempt sweep panic"`)
	require.Contains(t, out, `"msg":"login attempt sweep","deleted":2`)
}

// lockedWriter serialises writes to w: the sweeper logs from its own
// goroutine while the test reads the buffer, and the race detector would
// rightly flag an unguarded bytes.Buffer.
type lockedWriter struct {
	mu sync.Mutex
	w  *bytes.Buffer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// String reads the buffer under the same lock the writes take.
func (l *lockedWriter) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.String()
}

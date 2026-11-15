package globalapi

import (
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type postingLifecycleQueryObserver struct {
	active atomic.Pointer[postingLifecycleQueryObservation]
}

type postingLifecycleQueryTiming struct {
	count, failed  int
	total, longest time.Duration
}

type postingLifecycleQueryObservation struct {
	mu            sync.Mutex
	closed        bool
	groups        map[string]postingLifecycleQueryTiming
	lastCompleted time.Time
}

type postingLifecycleQueryStart struct {
	observation *postingLifecycleQueryObservation
	started     time.Time
}

// Register before the harness starts workers; callbacks live with this isolated database.
func newPostingLifecycleQueryObserver(t *testing.T, db *tenantdb.DB, tenant string) *postingLifecycleQueryObserver {
	t.Helper()
	observer := new(postingLifecycleQueryObserver)
	before := func(tx *gorm.DB) {
		if observation := observer.active.Load(); observation != nil {
			tx.Statement.Settings.Store("synthetic_posting_query_started", postingLifecycleQueryStart{observation, time.Now()})
		}
	}
	after := func(tx *gorm.DB) {
		value, found := tx.Statement.Settings.LoadAndDelete("synthetic_posting_query_started")
		if !found {
			return
		}
		start := value.(postingLifecycleQueryStart)
		finished := time.Now()
		elapsed := finished.Sub(start.started)
		phase := postingLifecycleQueryPhase(tx.Statement.SQL.String())
		observation := start.observation
		observation.mu.Lock()
		defer observation.mu.Unlock()
		if observation.closed {
			return
		}
		group := observation.groups[phase]
		group.count++
		if tx.Error != nil {
			group.failed++
		}
		group.total += elapsed
		group.longest = max(group.longest, elapsed)
		observation.groups[phase] = group
		observation.lastCompleted = finished
	}
	require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
		c := tx.Callback()
		for _, processor := range []struct {
			before, after func(string, func(*gorm.DB)) error
		}{
			{c.Raw().Before("*").Register, c.Raw().After("*").Register},
			{c.Row().Before("*").Register, c.Row().After("*").Register},
			{c.Query().Before("*").Register, c.Query().After("*").Register},
			{c.Create().Before("*").Register, c.Create().After("*").Register},
			{c.Update().Before("*").Register, c.Update().After("*").Register},
			{c.Delete().Before("*").Register, c.Delete().After("*").Register},
		} {
			if err := processor.before("synthetic_posting_timing_before", before); err != nil {
				return err
			}
			if err := processor.after("synthetic_posting_timing_after", after); err != nil {
				return err
			}
		}
		return nil
	}))
	return observer
}

func (observer *postingLifecycleQueryObserver) observe(t *testing.T) func() {
	t.Helper()
	started := time.Now()
	observation := &postingLifecycleQueryObservation{groups: make(map[string]postingLifecycleQueryTiming)}
	require.True(t, observer.active.CompareAndSwap(nil, observation), "lifecycle observations must not overlap")
	return func() {
		finished := time.Now()
		require.True(t, observer.active.CompareAndSwap(observation, nil))
		observation.mu.Lock()
		defer observation.mu.Unlock()
		observation.closed = true
		phases := make([]string, 0, len(observation.groups))
		for phase := range observation.groups {
			phases = append(phases, phase)
		}
		sort.Strings(phases)
		for _, phase := range phases {
			group := observation.groups[phase]
			t.Logf("Lifecycle SQL phase=%s count=%d failed=%d total_ms=%d longest_ms=%d", phase, group.count, group.failed, group.total.Milliseconds(), group.longest.Milliseconds())
		}
		if !observation.lastCompleted.IsZero() {
			t.Logf("Lifecycle SQL observation elapsed_ms=%d final_statement_gap_ms=%d", finished.Sub(started).Milliseconds(), max(time.Duration(0), finished.Sub(observation.lastCompleted)).Milliseconds())
		}
	}
}

func postingLifecycleQueryPhase(query string) string {
	if strings.Contains(query, "workforce_lock_supervision") {
		return "supervision_gate"
	}
	if strings.Contains(query, "set_config") {
		return "context"
	}
	for _, table := range []string{"workforce_posting_lifecycle_sources", "workforce_posting_lifecycle_reviews", "workforce_posting_chain_sources", "workforce_posting_chain_reviews", "workforce_posting_management_reviews", "workforce_posting_proposal_revisions", "workforce_posting_proposals", "workforce_posting_cutoffs", "workforce_posting_supervision_intents", "workforce_employment_generations", "workforce_login_links", "workforce_staff", "workforce_audit", "iam_assignment_snapshots", "iam_assignment_scope_epochs", "iam_subject_membership_epochs", "outbox_events"} {
		if strings.Contains(query, table) {
			return table
		}
	}
	return "other"
}

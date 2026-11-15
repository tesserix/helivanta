package globalapi

import (
	"context"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

// Observe only the original race's statements; SQL and bound values never leave the callback.
func observePostingLifecycleQueries(t *testing.T, db *tenantdb.DB, tenant string) func() {
	t.Helper()
	started := time.Now()
	type timing struct {
		count, failed  int
		total, longest time.Duration
	}
	var mu sync.Mutex
	groups := make(map[string]timing)
	var lastCompleted time.Time
	before := func(tx *gorm.DB) { tx.Statement.Settings.Store("synthetic_posting_query_started", time.Now()) }
	after := func(tx *gorm.DB) {
		value, found := tx.Statement.Settings.LoadAndDelete("synthetic_posting_query_started")
		if !found {
			return
		}
		finished := time.Now()
		elapsed := finished.Sub(value.(time.Time))
		phase := postingLifecycleQueryPhase(tx.Statement.SQL.String())
		mu.Lock()
		defer mu.Unlock()
		group := groups[phase]
		group.count++
		if tx.Error != nil {
			group.failed++
		}
		group.total += elapsed
		group.longest = max(group.longest, elapsed)
		groups[phase] = group
		lastCompleted = finished
	}
	type callbacks struct {
		before, after func(string, func(*gorm.DB)) error
		remove        func(string) error
	}
	var registered []callbacks
	require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
		c := tx.Callback()
		registered = []callbacks{
			{c.Raw().Before("*").Register, c.Raw().After("*").Register, c.Raw().Remove},
			{c.Row().Before("*").Register, c.Row().After("*").Register, c.Row().Remove},
			{c.Query().Before("*").Register, c.Query().After("*").Register, c.Query().Remove},
			{c.Create().Before("*").Register, c.Create().After("*").Register, c.Create().Remove},
			{c.Update().Before("*").Register, c.Update().After("*").Register, c.Update().Remove},
			{c.Delete().Before("*").Register, c.Delete().After("*").Register, c.Delete().Remove},
		}
		for _, processor := range registered {
			if err := processor.before("synthetic_posting_timing_before", before); err != nil {
				return err
			}
			if err := processor.after("synthetic_posting_timing_after", after); err != nil {
				return err
			}
		}
		return nil
	}))
	return func() {
		finished := time.Now()
		require.NoError(t, db.WithTenant(context.WithoutCancel(t.Context()), tenant, func(_ *gorm.DB) error {
			for _, processor := range registered {
				if err := processor.remove("synthetic_posting_timing_before"); err != nil {
					return err
				}
				if err := processor.remove("synthetic_posting_timing_after"); err != nil {
					return err
				}
			}
			return nil
		}))
		mu.Lock()
		defer mu.Unlock()
		phases := make([]string, 0, len(groups))
		for phase := range groups {
			phases = append(phases, phase)
		}
		sort.Strings(phases)
		for _, phase := range phases {
			group := groups[phase]
			t.Logf("Lifecycle SQL phase=%s count=%d failed=%d total_ms=%d longest_ms=%d", phase, group.count, group.failed, group.total.Milliseconds(), group.longest.Milliseconds())
		}
		if !lastCompleted.IsZero() {
			t.Logf("Lifecycle SQL observation elapsed_ms=%d final_statement_gap_ms=%d", finished.Sub(started).Milliseconds(), max(time.Duration(0), finished.Sub(lastCompleted)).Milliseconds())
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

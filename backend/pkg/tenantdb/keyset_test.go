package tenantdb_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// keysetMigrations creates a scratch table under the SAME forced-RLS
// boilerplate every module migration uses (see
// internal/modules/medicore/module.go). ApplyKeyset runs inside
// WithTenant in production, so a table without forced RLS would prove
// the keyset works in a query context it never actually runs in.
var keysetMigrations = []tenantdb.Migration{{
	ID: "0002_keyset_rows",
	SQL: `
		CREATE TABLE keyset_rows (
		  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
		  tenant_id uuid NOT NULL,
		  name text NOT NULL,
		  created_at timestamptz NOT NULL DEFAULT now()
		);
		ALTER TABLE keyset_rows ENABLE ROW LEVEL SECURITY;
		ALTER TABLE keyset_rows FORCE ROW LEVEL SECURITY;
		CREATE POLICY tenant_isolation ON keyset_rows
		  USING (hms_tenant_visible(tenant_id))
		  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
		CREATE INDEX ON keyset_rows (tenant_id, created_at DESC, id DESC);`,
}}

type keysetRow struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()"`
	TenantID  uuid.UUID
	Name      string
	CreatedAt time.Time
}

func (keysetRow) TableName() string { return "keyset_rows" }

// keysetHarness opens a fresh Postgres, applies the platform migrations
// (for hms_tenant_visible, which the scratch table's policy calls) and
// the scratch table migration, and hands back a tenant to run
// ApplyKeyset's tests against.
func keysetHarness(t *testing.T) (db *tenantdb.DB, ctx context.Context, tenantID string) {
	t.Helper()
	appDSN, adminDSN, systemDSN := testutil.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)
	ctx = context.Background()
	require.NoError(t, db.Migrate(ctx, tenantdb.Migrations()))
	require.NoError(t, db.Migrate(ctx, keysetMigrations))
	return db, ctx, uuid.NewString()
}

// seq hands out strictly increasing timestamps, one per call. Rows in
// these tests are ordered by relying on this instead of wall-clock
// deltas between Go statements, so the "exactly once" property does not
// become flaky under a slow CI runner.
type seq struct{ n int }

func (s *seq) next() time.Time {
	s.n++
	return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(s.n) * time.Millisecond)
}

// seedRows inserts n rows named r00..r(n-1) with strictly increasing
// created_at (r00 oldest, r(n-1) newest — so r(n-1) sorts first under
// created_at DESC). It returns the seq so a caller can keep inserting
// newer rows that land at the top of the ordering, as
// TestKeysetReturnsEveryRowExactlyOnceWhileRowsAreInserted does.
func seedRows(t *testing.T, db *tenantdb.DB, ctx context.Context, tenantID string, n int) *seq {
	t.Helper()
	s := &seq{}
	for i := range n {
		insertRowAt(t, db, ctx, tenantID, fmt.Sprintf("r%02d", i), s.next())
	}
	return s
}

// insertRowAt inserts one row with an explicit created_at, so the
// same-microsecond test can create rows that share a timestamp exactly.
func insertRowAt(t *testing.T, db *tenantdb.DB, ctx context.Context, tenantID, name string, at time.Time) {
	t.Helper()
	tid := uuid.MustParse(tenantID)
	require.NoError(t, db.WithTenant(ctx, tenantID, func(tx *gorm.DB) error {
		return tx.Create(&keysetRow{TenantID: tid, Name: name, CreatedAt: at}).Error
	}))
}

func deleteRow(t *testing.T, db *tenantdb.DB, ctx context.Context, tenantID, name string) {
	t.Helper()
	require.NoError(t, db.WithTenant(ctx, tenantID, func(tx *gorm.DB) error {
		return tx.Where("name = ?", name).Delete(&keysetRow{}).Error
	}))
}

func fetchPage(t *testing.T, db *tenantdb.DB, ctx context.Context, tenantID string, p pagination.Params) []keysetRow {
	t.Helper()
	var rows []keysetRow
	require.NoError(t, db.WithTenant(ctx, tenantID, func(tx *gorm.DB) error {
		return tenantdb.ApplyKeyset(tx, p).Find(&rows).Error
	}))
	return rows
}

// TestKeysetReturnsEveryRowExactlyOnceWhileRowsAreInserted is why this is
// a keyset and not an offset. A ward's list changes while it is read:
// with OFFSET, one insert before page 2 pushes a row from page 1 into
// page 2 (the client sees it twice) and one delete pulls a row up (the
// client never sees it). Silently dropping a patient from a list is the
// defect #816 exists to fix, reintroduced through the back door.
func TestKeysetReturnsEveryRowExactlyOnceWhileRowsAreInserted(t *testing.T) {
	db, ctx, tenantID := keysetHarness(t)
	s := seedRows(t, db, ctx, tenantID, 25) // ids r00..r24

	seen := map[string]int{}
	var after *pagination.Cursor
	for page := 0; page < 20; page++ {
		rows := fetchPage(t, db, ctx, tenantID, pagination.Params{Limit: 5, After: after})
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			seen[r.Name]++
		}
		// One new row per page, landing at the TOP of the DESC ordering —
		// the position OFFSET paging is most wrong about.
		insertRowAt(t, db, ctx, tenantID, fmt.Sprintf("inserted-%d", page), s.next())
		last := rows[len(rows)-1]
		after = &pagination.Cursor{TenantID: tenantID, CreatedAt: last.CreatedAt, ID: last.ID}
	}

	for i := range 25 {
		name := fmt.Sprintf("r%02d", i)
		require.Equal(t, 1, seen[name],
			"row %s was seen %d times; every pre-existing row must appear exactly once across pages", name, seen[name])
	}
}

// TestKeysetSeparatesRowsSharingATimestamp pairs with the cursor
// precision test in pkg/pagination: that one proves the cursor carries
// microseconds, this one proves the query uses them. Both must hold — a
// precise cursor fed into a comparison that ignores id, or an id
// tiebreaker fed a truncated timestamp, each lose rows.
func TestKeysetSeparatesRowsSharingATimestamp(t *testing.T) {
	db, ctx, tenantID := keysetHarness(t)
	at := time.Date(2026, 8, 13, 6, 12, 9, 123456000, time.UTC)
	// Five rows, identical created_at, distinct ids.
	for i := range 5 {
		insertRowAt(t, db, ctx, tenantID, fmt.Sprintf("same-%d", i), at)
	}

	seen := map[string]int{}
	var after *pagination.Cursor
	for range 10 {
		rows := fetchPage(t, db, ctx, tenantID, pagination.Params{Limit: 2, After: after})
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			seen[r.Name]++
		}
		last := rows[len(rows)-1]
		after = &pagination.Cursor{TenantID: tenantID, CreatedAt: last.CreatedAt, ID: last.ID}
	}

	require.Len(t, seen, 5, "all five rows sharing a timestamp must be reachable")
	for name, n := range seen {
		require.Equal(t, 1, n, "row %s seen %d times", name, n)
	}
}

// TestKeysetSurvivesItsAnchorBeingDeleted proves the predicate is a value
// comparison, not a row lookup. A reviewer will ask; this answers.
func TestKeysetSurvivesItsAnchorBeingDeleted(t *testing.T) {
	db, ctx, tenantID := keysetHarness(t)
	seedRows(t, db, ctx, tenantID, 6)

	first := fetchPage(t, db, ctx, tenantID, pagination.Params{Limit: 3})
	require.Len(t, first, 4, "the +1 probe returns limit+1 rows when more exist")
	anchor := first[2] // the last row the caller would actually be shown

	deleteRow(t, db, ctx, tenantID, anchor.Name)

	rest := fetchPage(t, db, ctx, tenantID, pagination.Params{
		Limit: 10,
		After: &pagination.Cursor{TenantID: tenantID, CreatedAt: anchor.CreatedAt, ID: anchor.ID},
	})
	names := make([]string, 0, len(rest))
	for _, r := range rest {
		names = append(names, r.Name)
	}
	require.Equal(t, []string{"r02", "r01", "r00"}, names,
		"paging must continue from the deleted anchor's position, not fail or restart")
}

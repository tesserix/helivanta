package tenantdb_test

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestConcurrentIndexMigrationBuildsAndRecordsValidIndex(t *testing.T) {
	db := openMigrated(t)
	migration := tenantdb.Migration{ID: "test_widgets_name_concurrent", ConcurrentIndexes: []tenantdb.ConcurrentIndex{{Name: "widgets_name_keyset", Table: "widgets", Columns: []tenantdb.IndexColumn{{Name: "tenant_id"}, {Name: "name"}, {Name: "id", Descending: true}}}}}
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{migration}))
	var valid bool
	require.NoError(t, db.WithAdmin(t.Context(), func(tx *gorm.DB) error {
		return tx.Raw(`SELECT EXISTS(SELECT 1 FROM pg_index i JOIN pg_class c ON c.oid=i.indexrelid WHERE c.relname='widgets_name_keyset' AND i.indisvalid AND i.indisready)`).Scan(&valid).Error
	}))
	require.True(t, valid, "a successful migration marker must represent a usable index")
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{migration}))
}

func TestConcurrentIndexMigrationResumesAfterIndexCommittedBeforeMarker(t *testing.T) {
	db := openMigrated(t)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_interrupted_index_artifact", SQL: `CREATE INDEX widgets_interrupted_keyset ON public.widgets(tenant_id,name,id DESC)`}}))
	migration := tenantdb.Migration{ID: "test_widgets_interrupted_concurrent", ConcurrentIndexes: []tenantdb.ConcurrentIndex{{Name: "widgets_interrupted_keyset", Table: "widgets", Columns: []tenantdb.IndexColumn{{Name: "tenant_id"}, {Name: "name"}, {Name: "id", Descending: true}}}}}
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{migration}))
	var applied bool
	require.NoError(t, db.WithAdmin(t.Context(), func(tx *gorm.DB) error {
		return tx.Raw(`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE id=?)`, migration.ID).Scan(&applied).Error
	}))
	require.True(t, applied)
}

func holdWidgetWriter(t *testing.T, db *tenantdb.DB) func() {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	release := make(chan struct{})
	ready := make(chan error, 1)
	done := make(chan error, 1)
	var once sync.Once
	unlock := func() { once.Do(func() { close(release) }) }
	go func() {
		done <- db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			err := tx.Exec(`INSERT INTO widgets(tenant_id,name) VALUES(?,'synthetic-held-writer')`, testutil.TenantA).Error
			ready <- err
			if err != nil {
				return err
			}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	t.Cleanup(func() {
		unlock()
		defer cancel()
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-ctx.Done():
			t.Error("held writer did not exit")
		}
	})
	select {
	case err := <-ready:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("held writer did not start")
	}
	return unlock
}

func TestConcurrentIndexMigrationRecoversCanceledBuildWithoutBlockingWriters(t *testing.T) {
	db := openMigrated(t)
	release := holdWidgetWriter(t, db)
	m := tenantdb.Migration{ID: "test_widgets_canceled_concurrent", ConcurrentIndexes: []tenantdb.ConcurrentIndex{{Name: "widgets_canceled_keyset", Table: "widgets", Columns: []tenantdb.IndexColumn{{Name: "tenant_id"}, {Name: "name"}, {Name: "id", Descending: true}}}}}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	finished := false
	go func() { done <- db.Migrate(ctx, []tenantdb.Migration{m}) }()
	t.Cleanup(func() {
		cancel()
		if !finished {
			select {
			case <-done:
			case <-time.After(6 * time.Second):
				t.Error("canceled migration did not exit")
			}
		}
	})
	observation, stop := context.WithTimeout(t.Context(), 3*time.Second)
	defer stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	waiting := false
	for !waiting && observation.Err() == nil {
		require.NoError(t, db.WithAdmin(observation, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT EXISTS(SELECT 1 FROM pg_stat_progress_create_index WHERE index_relid=to_regclass('public.widgets_canceled_keyset') AND phase LIKE 'waiting for writers%')`).Scan(&waiting).Error
		}))
		if !waiting {
			select {
			case <-ticker.C:
			case <-observation.Done():
			}
		}
	}
	require.True(t, waiting, "observe an actual committed index waiting for the existing writer before interrupting it")
	writeCtx, writeCancel := context.WithTimeout(t.Context(), time.Second)
	defer writeCancel()
	require.NoError(t, db.WithTenant(writeCtx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO widgets(tenant_id,name) VALUES(?,'synthetic-unblocked-writer')`, testutil.TenantA).Error
	}))
	cancel()
	select {
	case err := <-done:
		finished = true
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(6 * time.Second):
		t.Fatal("migration cancellation did not complete")
	}
	var invalid, applied bool
	require.NoError(t, db.WithAdmin(t.Context(), func(tx *gorm.DB) error {
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM pg_index i WHERE i.indexrelid=to_regclass('public.widgets_canceled_keyset') AND NOT i.indisvalid)`).Scan(&invalid).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE id=?)`, m.ID).Scan(&applied).Error
	}))
	require.True(t, invalid)
	require.False(t, applied)
	release()
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{m}))
	var valid bool
	require.NoError(t, db.WithAdmin(t.Context(), func(tx *gorm.DB) error {
		return tx.Raw(`SELECT EXISTS(SELECT 1 FROM pg_index i WHERE i.indexrelid=to_regclass('public.widgets_canceled_keyset') AND i.indisvalid AND i.indisready)`).Scan(&valid).Error
	}))
	require.True(t, valid)
}

func TestConcurrentIndexMigrationRejectsConflictingDefinitionsWithoutChangingThem(t *testing.T) {
	db := openMigrated(t)
	cases := []struct{ name, sql string }{
		{"columns", `CREATE INDEX widgets_conflict_columns ON widgets(tenant_id,id)`},
		{"order", `CREATE INDEX widgets_conflict_order ON widgets(tenant_id,name,id)`},
		{"nulls", `CREATE INDEX widgets_conflict_nulls ON widgets(tenant_id,name NULLS FIRST,id DESC)`},
		{"predicate", `CREATE INDEX widgets_conflict_predicate ON widgets(tenant_id,name,id DESC) WHERE name<>'synthetic'`},
		{"expression", `CREATE INDEX widgets_conflict_expression ON widgets(tenant_id,lower(name),id DESC)`},
		{"unique", `CREATE UNIQUE INDEX widgets_conflict_unique ON widgets(tenant_id,name,id DESC)`},
		{"include", `CREATE INDEX widgets_conflict_include ON widgets(tenant_id,name,id DESC) INCLUDE(created_at)`},
		{"collation", `CREATE INDEX widgets_conflict_collation ON widgets(tenant_id,name COLLATE "C",id DESC)`},
		{"operator", `CREATE INDEX widgets_conflict_operator ON widgets(tenant_id,name text_pattern_ops,id DESC)`},
		{"table", `CREATE INDEX widgets_conflict_table ON schema_migrations(id,id,applied_at DESC)`},
		{"relation", `CREATE TABLE widgets_conflict_relation(id integer PRIMARY KEY)`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			name := "widgets_conflict_" + c.name
			require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_conflict_setup_" + c.name, SQL: c.sql}}))
			snapshot := func() string {
				var out string
				require.NoError(t, db.WithAdmin(t.Context(), func(tx *gorm.DB) error {
					return tx.Raw(`SELECT oid::text||':'||relkind::text||':'||COALESCE(pg_get_indexdef(oid),'') FROM pg_class WHERE oid=to_regclass(?)`, "public."+name).Scan(&out).Error
				}))
				return out
			}
			before := snapshot()
			m := tenantdb.Migration{ID: "test_conflict_migration_" + c.name, ConcurrentIndexes: []tenantdb.ConcurrentIndex{{Name: name, Table: "widgets", Columns: []tenantdb.IndexColumn{{Name: "tenant_id"}, {Name: "name"}, {Name: "id", Descending: true}}}}}
			require.ErrorIs(t, db.Migrate(t.Context(), []tenantdb.Migration{m}), tenantdb.ErrMigrationIndexConflict)
			require.Equal(t, before, snapshot())
			var applied bool
			require.NoError(t, db.WithAdmin(t.Context(), func(tx *gorm.DB) error {
				return tx.Raw(`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE id=?)`, m.ID).Scan(&applied).Error
			}))
			require.False(t, applied)
		})
	}
}

func TestConcurrentIndexMigrationRejectsMalformedMetadataBeforeApplyingBatch(t *testing.T) {
	db := openMigrated(t)
	valid := tenantdb.Migration{ID: "test_invalid_batch_sentinel", SQL: `CREATE TABLE invalid_batch_sentinel(id integer)`}
	for _, index := range []tenantdb.ConcurrentIndex{
		{Name: "bad;name", Table: "widgets", Columns: []tenantdb.IndexColumn{{Name: "id"}}},
		{Name: "invalid_table", Table: "widgets;DROP", Columns: []tenantdb.IndexColumn{{Name: "id"}}},
		{Name: "invalid_column", Table: "widgets", Columns: []tenantdb.IndexColumn{{Name: "id DESC"}}},
		{Name: "duplicate_key", Table: "widgets", Columns: []tenantdb.IndexColumn{{Name: "id"}, {Name: "id"}}},
		{Name: "missing_key", Table: "widgets"},
		{Name: strings.Repeat("i", 64), Table: "widgets", Columns: []tenantdb.IndexColumn{{Name: "id"}}},
	} {
		require.ErrorIs(t, db.Migrate(t.Context(), []tenantdb.Migration{valid, {ID: "test_invalid_index", ConcurrentIndexes: []tenantdb.ConcurrentIndex{index}}}), tenantdb.ErrInvalidMigration)
	}
	require.ErrorIs(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_mixed_mode", SQL: "SELECT 1", ConcurrentIndexes: []tenantdb.ConcurrentIndex{{Name: "mixed", Table: "widgets", Columns: []tenantdb.IndexColumn{{Name: "id"}}}}}}), tenantdb.ErrInvalidMigration)
	var touched bool
	require.NoError(t, db.WithAdmin(t.Context(), func(tx *gorm.DB) error {
		return tx.Raw(`SELECT to_regclass('public.invalid_batch_sentinel') IS NOT NULL`).Scan(&touched).Error
	}))
	require.False(t, touched)
}

func TestConcurrentIndexMigrationSerializesIndependentAdminPools(t *testing.T) {
	app, admin, system := testutil.StartPostgres(t)
	first, err := tenantdb.OpenWithSystem(app, admin, system)
	require.NoError(t, err)
	second, err := tenantdb.OpenWithSystem(app, admin, system)
	require.NoError(t, err)
	require.NoError(t, first.Migrate(t.Context(), tenantdb.Migrations()))
	require.NoError(t, first.Migrate(t.Context(), testMigrations))
	migrations := []tenantdb.Migration{
		{ID: "test_serialized_side_effect", SQL: `SET LOCAL app.tenant_id='11111111-1111-1111-1111-111111111111'; INSERT INTO widgets(tenant_id,name) VALUES('11111111-1111-1111-1111-111111111111','synthetic-once')`},
		{ID: "test_serialized_concurrent_index", ConcurrentIndexes: []tenantdb.ConcurrentIndex{{Name: "widgets_serialized_keyset", Table: "widgets", Columns: []tenantdb.IndexColumn{{Name: "tenant_id"}, {Name: "name"}, {Name: "id", Descending: true}}}}},
	}
	start := make(chan struct{})
	done := make(chan error, 4)
	for _, db := range []*tenantdb.DB{first, second, first, second} {
		go func() { <-start; done <- db.Migrate(t.Context(), migrations) }()
	}
	close(start)
	outcomes := make([]error, 4)
	for i := range outcomes {
		outcomes[i] = <-done
	}
	for _, err := range outcomes {
		require.NoError(t, err)
	}
	var count int64
	require.NoError(t, first.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Table("widgets").Where("name=?", "synthetic-once").Count(&count).Error
	}))
	require.EqualValues(t, 1, count)
}

func TestConcurrentIndexMigrationKeepsPartialProgressUnmarkedUntilAllIndexesReady(t *testing.T) {
	db := openMigrated(t)
	m := tenantdb.Migration{ID: "test_multi_index_migration", ConcurrentIndexes: []tenantdb.ConcurrentIndex{
		{Name: "widgets_multi_first", Table: "widgets", Columns: []tenantdb.IndexColumn{{Name: "tenant_id"}, {Name: "name"}}},
		{Name: "widgets_multi_second", Table: "widgets", Columns: []tenantdb.IndexColumn{{Name: "future_key"}}},
	}}
	require.Error(t, db.Migrate(t.Context(), []tenantdb.Migration{m}))
	var firstValid, applied bool
	require.NoError(t, db.WithAdmin(t.Context(), func(tx *gorm.DB) error {
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM pg_index WHERE indexrelid=to_regclass('public.widgets_multi_first') AND indisvalid)`).Scan(&firstValid).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE id=?)`, m.ID).Scan(&applied).Error
	}))
	require.True(t, firstValid)
	require.False(t, applied)
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_multi_index_dependency", SQL: `ALTER TABLE widgets ADD COLUMN future_key text`}}))
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{m}))
	require.NoError(t, db.WithAdmin(t.Context(), func(tx *gorm.DB) error {
		return tx.Raw(`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE id=?)`, m.ID).Scan(&applied).Error
	}))
	require.True(t, applied)
}

func TestMigrationRestoresAdminSessionAndTransactionalRollback(t *testing.T) {
	app, admin, _ := testutil.StartPostgres(t)
	parsed, err := url.Parse(admin)
	require.NoError(t, err)
	query := parsed.Query()
	query.Set("lock_timeout", "8000")
	query.Set("statement_timeout", "7000")
	parsed.RawQuery = query.Encode()
	db, err := tenantdb.Open(app, parsed.String())
	require.NoError(t, err)
	require.NoError(t, db.WithAdmin(t.Context(), func(tx *gorm.DB) error {
		pool, err := tx.DB()
		if err != nil {
			return err
		}
		pool.SetMaxOpenConns(1)
		pool.SetMaxIdleConns(1)
		return nil
	}))
	require.NoError(t, db.Migrate(t.Context(), tenantdb.Migrations()))
	require.NoError(t, db.Migrate(t.Context(), testMigrations))
	check := func() {
		require.NoError(t, db.WithAdmin(t.Context(), func(tx *gorm.DB) error {
			var state struct {
				LockTimeout, StatementTimeout string
				Held                          int64
			}
			err := tx.Raw(`SELECT current_setting('lock_timeout') AS lock_timeout,current_setting('statement_timeout') AS statement_timeout,(SELECT count(*) FROM pg_locks WHERE pid=pg_backend_pid() AND locktype='advisory') AS held`).Scan(&state).Error
			require.Equal(t, "8s", state.LockTimeout)
			require.Equal(t, "7s", state.StatementTimeout)
			require.Zero(t, state.Held)
			return err
		}))
	}
	m := tenantdb.Migration{ID: "test_session_index", ConcurrentIndexes: []tenantdb.ConcurrentIndex{{Name: "widgets_session_index", Table: "widgets", Columns: []tenantdb.IndexColumn{{Name: "name"}}}}}
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{m}))
	check()
	failed := tenantdb.Migration{ID: "test_atomic_migration_failure", SQL: `CREATE TABLE migration_rollback_probe(id integer); SELECT 1/0`}
	require.Error(t, db.Migrate(t.Context(), []tenantdb.Migration{failed}))
	check()
	require.NoError(t, db.WithAdmin(t.Context(), func(tx *gorm.DB) error {
		var exists, marked bool
		if err := tx.Raw(`SELECT to_regclass('public.migration_rollback_probe') IS NOT NULL`).Scan(&exists).Error; err != nil {
			return err
		}
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE id=?)`, failed.ID).Scan(&marked).Error; err != nil {
			return err
		}
		require.False(t, exists)
		require.False(t, marked)
		return nil
	}))
}

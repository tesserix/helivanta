package testutil

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// StocktakeDiagnostics records statement timings and error metadata without SQL values.
func StocktakeDiagnostics(t *testing.T, db *tenantdb.DB) {
	t.Helper()
	if os.Getenv("HELIVANTA_STOCKTAKE_DIAGNOSTICS") != "1" {
		return
	}
	const key = "test:stocktake-diagnostic-start"
	var snapshot string
	var args []any
	var settings string
	var capture sync.Mutex
	before := func(tx *gorm.DB) {
		capture.Lock()
		if snapshot == "" && strings.HasPrefix(tx.Statement.SQL.String(), "WITH products AS (") {
			snapshot = tx.Statement.SQL.String()
			args = append([]any(nil), tx.Statement.Vars...)
			err := tx.Statement.ConnPool.QueryRowContext(tx.Statement.Context, `SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM unnest(ARRAY['app.actor_subject','app.pharmacy_subject','app.pharmacy_id','app.pharmacy_outlet','app.pharmacy_stock_access','app.pharmacy_stock_pharmacy','app.pharmacy_stock_outlet','app.pharmacy_stock_permission'])AS key`).Scan(&settings)
			require.NoError(t, err)
		}
		capture.Unlock()
		tx.Statement.Settings.Store(key, time.Now())
	}
	after := func(tx *gorm.DB) {
		start, ok := tx.Statement.Settings.LoadAndDelete(key)
		if !ok {
			return
		}
		elapsed := time.Since(start.(time.Time))
		if elapsed < 100*time.Millisecond && tx.Error == nil {
			return
		}
		state, constraint := "", ""
		var pg *pgconn.PgError
		if errors.As(tx.Error, &pg) {
			state, constraint = pg.Code, pg.ConstraintName
		}
		template := sha256.Sum256([]byte(tx.Statement.SQL.String()))
		t.Logf("stocktake diagnostic table=%s elapsed=%s template=%x error=%T sqlstate=%s constraint=%s", tx.Statement.Table, elapsed, template[:8], tx.Error, state, constraint)
	}
	var callbacks *gorm.DB
	require.NoError(t, db.WithTenant(t.Context(), TenantA, func(tx *gorm.DB) error {
		callbacks = tx
		if err := tx.Callback().Create().Before("gorm:create").Register(key, before); err != nil {
			return err
		}
		if err := tx.Callback().Create().After("gorm:create").Register(key+":after", after); err != nil {
			return err
		}
		if err := tx.Callback().Raw().Before("gorm:raw").Register(key, before); err != nil {
			return err
		}
		if err := tx.Callback().Raw().After("gorm:raw").Register(key+":after", after); err != nil {
			return err
		}
		if err := tx.Callback().Row().Before("gorm:row").Register(key, before); err != nil {
			return err
		}
		return tx.Callback().Row().After("gorm:row").Register(key+":after", after)
	}))
	t.Cleanup(func() {
		for _, name := range []string{key, key + ":after"} {
			require.NoError(t, callbacks.Callback().Create().Remove(name))
			require.NoError(t, callbacks.Callback().Raw().Remove(name))
			require.NoError(t, callbacks.Callback().Row().Remove(name))
		}
		capture.Lock()
		query, parameters, purpose := snapshot, args, settings
		capture.Unlock()
		if query == "" {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := db.WithTenant(ctx, TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, purpose).Error; err != nil {
				return err
			}
			rows, err := tx.Statement.ConnPool.QueryContext(ctx, "EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) "+query, parameters...)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var raw []byte
				if err := rows.Scan(&raw); err != nil {
					return err
				}
				var plan []map[string]any
				if err := json.Unmarshal(raw, &plan); err != nil {
					return err
				}
				var visit func(map[string]any, int)
				visit = func(node map[string]any, depth int) {
					fields := make(map[string]any)
					for _, key := range []string{"Node Type", "Relation Name", "Index Name", "Actual Total Time", "Actual Rows", "Actual Loops", "Rows Removed by Filter", "Shared Hit Blocks", "Shared Read Blocks"} {
						if value, ok := node[key]; ok {
							fields[key] = value
						}
					}
					t.Logf("stocktake diagnostic plan depth=%d metrics=%v", depth, fields)
					if children, ok := node["Plans"].([]any); ok {
						for _, child := range children {
							if child, ok := child.(map[string]any); ok {
								visit(child, depth+1)
							}
						}
					}
				}
				for _, root := range plan {
					if node, ok := root["Plan"].(map[string]any); ok {
						visit(node, 0)
					}
				}
			}
			return rows.Err()
		})
		require.NoError(t, err, "diagnostic EXPLAIN must finish without exposing SQL values")
	})
}

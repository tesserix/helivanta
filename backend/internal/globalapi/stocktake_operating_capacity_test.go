package globalapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestStocktakeReviewedCapacityAt10000Buckets(t *testing.T) {
	if os.Getenv("HELIVANTA_STOCKTAKE_CAPACITY") != "1" {
		t.Skip("run the focused hosted capacity gate")
	}
	f := newPharmacyReceiverFixture(t)
	f.ownerOutletTx(t, func(tx *gorm.DB) error {
		if err := tx.Exec(`INSERT INTO pharmacy_stock_batches(tenant_id,outlet_id,id,product_id,product_version,lot_number,expires_on,mrp_minor)
   SELECT ?,?,('019a2b00-1000-7000-8000-'||lpad(i::text,12,'0'))::uuid,?,1,'TEST-REVIEWED-CAP-'||i,'2030-12-31',12500 FROM generate_series(1,10000)i`, testutil.TenantA, f.outlet, f.product).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO pharmacy_stock_movements(tenant_id,outlet_id,id,location_id,batch_id,kind,quantity_units,reason,source_reference,actor_subject)
   SELECT ?,?,('019a2b01-1000-7000-8000-'||lpad(i::text,12,'0'))::uuid,?,('019a2b00-1000-7000-8000-'||lpad(i::text,12,'0'))::uuid,'receipt',10,'TEST reviewed capacity fixture','TEST-reviewed-capacity:'||i,'user-owner' FROM generate_series(1,10000)i`, testutil.TenantA, f.outlet, f.quarantine).Error
	})
	f.input.TemplateKey = "pharmacy.stocktake_counter.v1"
	f.activate(t)
	if os.Getenv("HELIVANTA_STOCKTAKE_INSERT_PLAN") == "1" {
		require.NoError(t, f.db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_stocktake_capacity_batch_statistics", SQL: "ANALYZE pharmacy_stock_batches"}}))
		diagnoseStocktakeInsert(t, f)
	}
	path := "/v1/pharmacy/pharmacies/" + f.pharmacy + "/operating-locations/" + f.outlet + "/stocktakes/" + v7()
	measure := func(name string, run func() string) string {
		start := time.Now()
		defer func() { t.Logf("reviewed %s elapsed %s", name, time.Since(start)) }()
		result := run()
		require.Less(t, time.Since(start), 5*time.Second, "reviewed %s budget", name)
		return result
	}
	measure("10000-bucket start", func() string {
		return f.call("PUT", path, "receiver", map[string]any{"selection": "all", "product_ids": []string{}, "quarantine_location_id": f.quarantine, "reason": "TEST reviewed maximum count"}, 200)
	})
	entries := make([]map[string]any, 0, 10000)
	seen := make(map[string]bool, 10000)
	var timings []time.Duration
	cursor := ""
	for pages := 0; ; pages++ {
		require.Less(t, pages, 101, "pagination must terminate")
		request := path + "/lines?limit=100"
		if cursor != "" {
			request += "&cursor=" + url.QueryEscape(cursor)
		}
		start := time.Now()
		raw := f.call("GET", request, "receiver", nil, 200)
		timings = append(timings, time.Since(start))
		var page struct {
			Data []struct {
				ID        string `json:"id"`
				BookUnits int64  `json:"book_units"`
			} `json:"data"`
			Page struct {
				Next    string `json:"next_cursor"`
				HasMore bool   `json:"has_more"`
			} `json:"page"`
		}
		require.NoError(t, json.Unmarshal([]byte(raw), &page))
		require.Len(t, page.Data, 100)
		for _, line := range page.Data {
			require.False(t, seen[line.ID])
			seen[line.ID] = true
			require.EqualValues(t, 10, line.BookUnits)
			units := 10
			if len(entries) < 500 {
				units = 9
			}
			entries = append(entries, map[string]any{"line_id": line.ID, "counted_units": units})
		}
		if !page.Page.HasMore {
			break
		}
		cursor = page.Page.Next
		require.NotEmpty(t, cursor)
	}
	require.Len(t, seen, 10000)
	require.Len(t, timings, 100)
	sort.Slice(timings, func(i, j int) bool { return timings[i] < timings[j] })
	p99 := timings[(len(timings)*99+99)/100-1]
	t.Logf("reviewed 100 pages p99 %s", p99)
	require.Less(t, p99, 300*time.Millisecond)
	version := 0
	for i := 0; i < len(entries); i += 100 {
		f.call("PUT", path+"/counts/"+v7(), "receiver", map[string]any{"expected_version": version, "counts": entries[i : i+100], "reason": fmt.Sprintf("TEST reviewed physical observation batch %d", version)}, 200)
		version++
	}
	f.call("PUT", path+"/decisions/"+v7(), "receiver", map[string]any{"decision": "submit", "expected_version": version, "reason": "TEST reviewed all physical observations"}, 200)
	activateReviewedStocktakeReviewer(t, &f)
	approvalPath := path + "/decisions/" + v7()
	approval := map[string]any{"decision": "approve", "expected_version": version + 1, "reason": "TEST independently review maximum corrections", "extra_units_disposition": "no_extra_units"}
	posted := measure("500-adjustment post", func() string { return f.call("PUT", approvalPath, "stock-reviewer", approval, 200) })
	var receipt struct {
		State           string `json:"state"`
		Version         int64  `json:"version"`
		AdjustmentCount int64  `json:"adjustment_count"`
	}
	require.NoError(t, json.Unmarshal([]byte(posted), &receipt))
	require.Equal(t, "posted", receipt.State)
	require.EqualValues(t, version+2, receipt.Version)
	require.EqualValues(t, 500, receipt.AdjustmentCount)
	require.JSONEq(t, posted, f.call("PUT", approvalPath, "stock-reviewer", approval, 200))
	require.JSONEq(t, posted, f.call("GET", approvalPath, "stock-reviewer", nil, 200))
	f.ownerOutletTx(t, func(tx *gorm.DB) error {
		var ledger struct{ Corrections, Units int64 }
		if err := tx.Raw(`SELECT count(*) FILTER(WHERE kind='correction') corrections,COALESCE(sum(quantity_units),0) units FROM pharmacy_stock_movements WHERE tenant_id=? AND outlet_id=?`, testutil.TenantA, f.outlet).Scan(&ledger).Error; err != nil {
			return err
		}
		require.EqualValues(t, 500, ledger.Corrections)
		require.EqualValues(t, 99500, ledger.Units)
		var commands int64
		if err := tx.Table("pharmacy_stocktake_commands").Where("tenant_id=? AND outlet_id=? AND stocktake_id=? AND kind='count' AND actor_subject='user-receiver'", testutil.TenantA, f.outlet, path[len(path)-36:]).Count(&commands).Error; err != nil {
			return err
		}
		require.EqualValues(t, 100, commands)
		return nil
	})
}

func diagnoseStocktakeInsert(t *testing.T, f pharmacyReceiverFixture) {
	t.Helper()
	const callback = "test:stocktake-insert-plan"
	var batches atomic.Int32
	var hooks *gorm.DB
	f.ownerOutletTx(t, func(tx *gorm.DB) error {
		hooks = tx
		before := func(tx *gorm.DB) {
			if !tx.DryRun {
				tx.Statement.Settings.Store(callback+":time", time.Now())
			}
		}
		after := func(tx *gorm.DB) {
			if tx.DryRun {
				return
			}
			if start, ok := tx.Statement.Settings.LoadAndDelete(callback + ":time"); ok {
				elapsed := time.Since(start.(time.Time))
				if elapsed >= 50*time.Millisecond || tx.Error != nil {
					hash := sha256.Sum256([]byte(tx.Statement.SQL.String()))
					t.Logf("diagnostic statement table=%s elapsed=%s template=%x error=%T", tx.Statement.Table, elapsed, hash[:8], tx.Error)
				}
			}
		}
		for _, register := range []func() error{
			func() error { return tx.Callback().Create().Before("*").Register(callback+":before", before) },
			func() error { return tx.Callback().Create().After("*").Register(callback+":after", after) },
			func() error { return tx.Callback().Query().Before("*").Register(callback+":before", before) },
			func() error { return tx.Callback().Query().After("*").Register(callback+":after", after) },
			func() error { return tx.Callback().Row().Before("*").Register(callback+":before", before) },
			func() error { return tx.Callback().Row().After("*").Register(callback+":after", after) },
			func() error { return tx.Callback().Raw().Before("*").Register(callback+":before", before) },
			func() error { return tx.Callback().Raw().After("*").Register(callback+":after", after) },
		} {
			if err := register(); err != nil {
				return err
			}
		}
		return tx.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
			if tx.DryRun || tx.Statement.Table != "pharmacy_stocktake_lines" || batches.Add(1) != 3 {
				return
			}
			func() {
				dry := tx.Session(&gorm.Session{DryRun: true, SkipHooks: true, SkipDefaultTransaction: true}).Create(tx.Statement.Dest)
				if dry.Error != nil {
					t.Errorf("compile diagnostic insert: %T", dry.Error)
					return
				}
				conn := tx.Statement.ConnPool
				if _, err := conn.ExecContext(tx.Statement.Context, "SAVEPOINT diagnostic_stocktake_insert"); err != nil {
					t.Errorf("open diagnostic savepoint: %T", err)
					return
				}
				defer func() {
					ctx, cancel := context.WithTimeout(context.WithoutCancel(tx.Statement.Context), 3*time.Second)
					defer cancel()
					if _, err := conn.ExecContext(ctx, "ROLLBACK TO SAVEPOINT diagnostic_stocktake_insert"); err != nil {
						t.Errorf("rollback diagnostic insert: %T", err)
						return
					}
					if _, err := conn.ExecContext(ctx, "RELEASE SAVEPOINT diagnostic_stocktake_insert"); err != nil {
						t.Errorf("release diagnostic savepoint: %T", err)
					}
				}()
				rows, err := conn.QueryContext(tx.Statement.Context, "EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON) "+dry.Statement.SQL.String(), dry.Statement.Vars...)
				if err != nil {
					t.Errorf("explain diagnostic insert: %T", err)
					return
				}
				defer rows.Close()
				for rows.Next() {
					var raw []byte
					if err := rows.Scan(&raw); err != nil {
						t.Errorf("read diagnostic insert plan: %T", err)
						return
					}
					var plans []map[string]any
					if err := json.Unmarshal(raw, &plans); err != nil {
						t.Errorf("decode diagnostic insert plan: %T", err)
						return
					}
					for _, plan := range plans {
						t.Logf("diagnostic snapshot insert planning_ms=%v execution_ms=%v", plan["Planning Time"], plan["Execution Time"])
						if triggers, ok := plan["Triggers"].([]any); ok {
							for _, trigger := range triggers {
								if trigger, ok := trigger.(map[string]any); ok {
									t.Logf("diagnostic snapshot trigger name=%v time_ms=%v calls=%v", trigger["Trigger Name"], trigger["Time"], trigger["Calls"])
								}
							}
						}
					}
				}
				if err := rows.Err(); err != nil {
					t.Errorf("finish diagnostic insert plan: %T", err)
				}
				if err := rows.Close(); err != nil {
					t.Errorf("close diagnostic insert plan: %T", err)
					return
				}
				lookup, err := conn.QueryContext(tx.Statement.Context, `EXPLAIN (ANALYZE,BUFFERS,FORMAT JSON)
 SELECT id FROM pharmacy_stock_batches WHERE tenant_id=$1 AND outlet_id=$2 AND id=$3 FOR KEY SHARE`, testutil.TenantA, f.outlet, "019a2b00-1000-7000-8000-000000005000")
				if err != nil {
					t.Errorf("explain diagnostic batch lookup: %T", err)
					return
				}
				defer lookup.Close()
				for lookup.Next() {
					var raw []byte
					if err := lookup.Scan(&raw); err != nil {
						t.Errorf("read batch lookup plan: %T", err)
						return
					}
					var plans []map[string]any
					if err := json.Unmarshal(raw, &plans); err != nil {
						t.Errorf("decode batch lookup plan: %T", err)
						return
					}
					var visit func(map[string]any)
					visit = func(node map[string]any) {
						t.Logf("diagnostic batch lookup node=%v index=%v estimate=%v rows=%v loops=%v filtered=%v time_ms=%v", node["Node Type"], node["Index Name"], node["Plan Rows"], node["Actual Rows"], node["Actual Loops"], node["Rows Removed by Filter"], node["Actual Total Time"])
						condition, _ := node["Index Cond"].(string)
						t.Logf("diagnostic batch index identity_predicate=%t tenant_predicate=%t outlet_predicate=%t shared_hits=%v shared_reads=%v", strings.Contains(condition, "(id ="), strings.Contains(condition, "(tenant_id ="), strings.Contains(condition, "(outlet_id ="), node["Shared Hit Blocks"], node["Shared Read Blocks"])
						if children, ok := node["Plans"].([]any); ok {
							for _, child := range children {
								if child, ok := child.(map[string]any); ok {
									visit(child)
								}
							}
						}
					}
					for _, plan := range plans {
						if node, ok := plan["Plan"].(map[string]any); ok {
							visit(node)
						}
					}
				}
				if err := lookup.Err(); err != nil {
					t.Errorf("finish batch lookup plan: %T", err)
				}
				if err := lookup.Close(); err != nil {
					t.Errorf("close batch lookup plan: %T", err)
					return
				}
				stats, err := conn.QueryContext(tx.Statement.Context, `SELECT c.relname,c.relpages,c.reltuples,i.indisvalid,i.indisready FROM pg_class c JOIN pg_index i ON i.indexrelid=c.oid WHERE i.indrelid='pharmacy_stock_batches'::regclass ORDER BY c.relname`)
				if err != nil {
					t.Errorf("read batch index statistics: %T", err)
					return
				}
				defer stats.Close()
				for stats.Next() {
					var name string
					var pages int
					var tuples float64
					var valid, ready bool
					if err := stats.Scan(&name, &pages, &tuples, &valid, &ready); err != nil {
						t.Errorf("read batch index statistics row: %T", err)
						return
					}
					t.Logf("diagnostic batch index name=%s pages=%d tuples=%f valid=%t ready=%t", name, pages, tuples, valid, ready)
				}
				if err := stats.Err(); err != nil {
					t.Errorf("finish batch index statistics: %T", err)
				}
			}()
		})
	})
	t.Cleanup(func() {
		require.NoError(t, hooks.Callback().Create().Remove(callback))
		for _, name := range []string{callback + ":before", callback + ":after"} {
			require.NoError(t, hooks.Callback().Create().Remove(name))
			require.NoError(t, hooks.Callback().Query().Remove(name))
			require.NoError(t, hooks.Callback().Row().Remove(name))
			require.NoError(t, hooks.Callback().Raw().Remove(name))
		}
	})
}

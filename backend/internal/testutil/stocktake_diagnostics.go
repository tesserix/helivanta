package testutil

import (
	"crypto/sha256"
	"errors"
	"os"
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
	before := func(tx *gorm.DB) { tx.Statement.Settings.Store(key, time.Now()) }
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
	})
}

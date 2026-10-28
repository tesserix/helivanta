package tenantdb

import (
	"context"
	"crypto/rand"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"math/big"
	"strings"
	"time"
)

var ErrInvalidMigration = errors.New("tenantdb: invalid migration")

// Migrate applies ordered migrations once on a dedicated, session-affine admin connection.
func (d *DB) Migrate(ctx context.Context, migs []Migration) (err error) {
	if d.admin == nil {
		return errNoSchemaPool
	}
	for _, m := range migs {
		if strings.TrimSpace(m.ID) == "" || (strings.TrimSpace(m.SQL) == "") == (len(m.ConcurrentIndexes) == 0) {
			return ErrInvalidMigration
		}
		for _, index := range m.ConcurrentIndexes {
			if e := validateConcurrentIndex(index); e != nil {
				return e
			}
		}
	}
	pool, err := d.admin.DB()
	if err != nil {
		return err
	}
	conn, err := pool.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if e := conn.Close(); e != nil && !errors.Is(e, sql.ErrConnDone) {
			err = errors.Join(err, e)
		}
	}()
	var lockTimeout, statementTimeout string
	if err = conn.QueryRowContext(ctx, `SELECT current_setting('lock_timeout'),current_setting('statement_timeout')`).Scan(&lockTimeout, &statementTimeout); err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_, e := conn.ExecContext(cleanup, `SELECT set_config('lock_timeout',$1,false),set_config('statement_timeout',$2,false),pg_advisory_unlock(hashtextextended('helivanta.schema_migrations',0))`, lockTimeout, statementTimeout)
		if e != nil {
			// A failed cleanup must not return a session holding a lock/settings to the pool.
			discard := conn.Raw(func(any) error { return driver.ErrBadConn })
			if discard != nil && !errors.Is(discard, driver.ErrBadConn) && !errors.Is(discard, sql.ErrConnDone) {
				e = errors.Join(e, discard)
			}
			err = errors.Join(err, fmt.Errorf("release migration session: %w", e))
		}
	}()
	if _, err = conn.ExecContext(ctx, `SELECT set_config('lock_timeout','5s',false),set_config('statement_timeout','10min',false)`); err != nil {
		return err
	}
	if err = acquireMigrationLock(ctx, conn); err != nil {
		return err
	}
	if _, err = conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (id text PRIMARY KEY,applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	for _, m := range migs {
		if len(m.ConcurrentIndexes) > 0 {
			err = migrateConcurrentIndexes(ctx, conn, m)
		} else {
			err = migrateTransactional(ctx, conn, m)
		}
		if err != nil {
			return fmt.Errorf("migration %s: %w", m.ID, err)
		}
	}
	return nil
}

func migrateTransactional(ctx context.Context, conn *sql.Conn, m Migration) (err error) {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if e := tx.Rollback(); e != nil && !errors.Is(e, sql.ErrTxDone) {
			err = errors.Join(err, e)
		}
	}()
	result, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(id) VALUES($1) ON CONFLICT DO NOTHING`, m.ID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count > 0 {
		if _, err = tx.ExecContext(ctx, m.SQL); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func migrateConcurrentIndexes(ctx context.Context, conn *sql.Conn, m Migration) error {
	var applied bool
	if err := conn.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE id=$1)`, m.ID).Scan(&applied); err != nil {
		return err
	}
	if applied {
		return nil
	}
	for _, index := range m.ConcurrentIndexes {

		exists, usable, err := inspectConcurrentIndex(ctx, conn, index)
		if err != nil {
			return err
		}

		if exists && usable {
			continue
		}
		command := concurrentIndexSQL(index)
		if exists {
			command = "REINDEX INDEX CONCURRENTLY " + pgx.Identifier{"public", index.Name}.Sanitize()
		}
		if _, err := conn.ExecContext(ctx, command); err != nil {
			return err
		}
		_, usable, err = inspectConcurrentIndex(ctx, conn, index)
		if err != nil {
			return err
		}
		if !usable {
			return fmt.Errorf("%w: %s", ErrMigrationIndexConflict, index.Name)
		}
	}
	_, err := conn.ExecContext(ctx, `INSERT INTO schema_migrations(id) VALUES($1)`, m.ID)
	return err
}

func acquireMigrationLock(ctx context.Context, conn *sql.Conn) error {
	wait, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	backoff := 10 * time.Millisecond
	for {
		var locked bool
		// A blocking lock waiter retains a snapshot that concurrent index validation needs.
		if err := conn.QueryRowContext(wait, `SELECT pg_try_advisory_lock(hashtextextended('helivanta.schema_migrations',0))`).Scan(&locked); err != nil {
			return err
		}
		if locked {
			return nil
		}
		jitter, err := rand.Int(rand.Reader, big.NewInt(int64(backoff)))
		if err != nil {
			return fmt.Errorf("migration lock jitter: %w", err)
		}
		timer := time.NewTimer(backoff + time.Duration(jitter.Int64()))
		select {
		case <-timer.C:
		case <-wait.Done():
			timer.Stop()
			return wait.Err()
		}
		backoff = min(backoff*2, 100*time.Millisecond)
	}
}

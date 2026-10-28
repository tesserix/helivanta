package tenantdb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

var ErrMigrationIndexConflict = errors.New("tenantdb: index definition conflicts with migration")
var migrationIdentifier = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

func validateConcurrentIndex(index ConcurrentIndex) error {
	if !migrationIdentifier.MatchString(index.Name) || !migrationIdentifier.MatchString(index.Table) || len(index.Columns) == 0 || len(index.Columns) > 32 {
		return ErrInvalidMigration
	}
	seen := map[string]bool{}
	for _, column := range index.Columns {
		if !migrationIdentifier.MatchString(column.Name) || seen[column.Name] {
			return ErrInvalidMigration
		}
		seen[column.Name] = true
	}
	return nil
}

func concurrentIndexSQL(index ConcurrentIndex) string {
	keys := make([]string, 0, len(index.Columns))
	for _, column := range index.Columns {
		key := pgx.Identifier{column.Name}.Sanitize()
		if column.Descending {
			key += " DESC"
		}
		keys = append(keys, key)
	}
	return "CREATE INDEX CONCURRENTLY " + pgx.Identifier{index.Name}.Sanitize() + " ON " + pgx.Identifier{"public", index.Table}.Sanitize() + " USING btree (" + strings.Join(keys, ",") + ")"
}

func inspectConcurrentIndex(ctx context.Context, conn *sql.Conn, index ConcurrentIndex) (exists bool, usable bool, err error) {
	var kind, method string
	var valid, ready, live, unique, primary, exclusion, plain, tableMatch bool
	var keys, total int
	err = conn.QueryRowContext(ctx, `SELECT c.relkind::text,am.amname,i.indisvalid,i.indisready,i.indislive,i.indisunique,i.indisprimary,i.indisexclusion,i.indpred IS NULL AND i.indexprs IS NULL,i.indrelid=to_regclass($2),i.indnkeyatts,i.indnatts FROM pg_class c JOIN pg_index i ON i.indexrelid=c.oid JOIN pg_am am ON am.oid=c.relam WHERE c.oid=to_regclass($1)`, "public."+index.Name, "public."+index.Table).Scan(&kind, &method, &valid, &ready, &live, &unique, &primary, &exclusion, &plain, &tableMatch, &keys, &total)
	if errors.Is(err, sql.ErrNoRows) {
		var collision bool
		if e := conn.QueryRowContext(ctx, `SELECT to_regclass($1) IS NOT NULL`, "public."+index.Name).Scan(&collision); e != nil {
			return false, false, e
		}
		if collision {
			return true, false, fmt.Errorf("%w: %s", ErrMigrationIndexConflict, index.Name)
		}
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	if kind != "i" || method != "btree" || !live || (valid && !ready) || unique || primary || exclusion || !plain || !tableMatch || keys != len(index.Columns) || total != len(index.Columns) {
		return true, false, fmt.Errorf("%w: %s", ErrMigrationIndexConflict, index.Name)
	}
	rows, err := conn.QueryContext(ctx, `SELECT a.attname,(i.indoption[(k.position-1)::int]&1)=1,(i.indoption[(k.position-1)::int]&2)=2,i.indcollation[(k.position-1)::int]=a.attcollation,o.opcdefault FROM pg_index i CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY k(attnum,position) JOIN pg_attribute a ON a.attrelid=i.indrelid AND a.attnum=k.attnum JOIN pg_opclass o ON o.oid=i.indclass[(k.position-1)::int] WHERE i.indexrelid=to_regclass($1) ORDER BY k.position`, "public."+index.Name)
	if err != nil {
		return true, false, err
	}
	defer func() { err = errors.Join(err, rows.Close()) }()
	count := 0
	for rows.Next() {
		var name string
		var descending, nullsFirst, defaultCollation, defaultOperator bool
		if err := rows.Scan(&name, &descending, &nullsFirst, &defaultCollation, &defaultOperator); err != nil {
			return true, false, err
		}
		if count >= len(index.Columns) || name != index.Columns[count].Name || descending != index.Columns[count].Descending || nullsFirst != descending || !defaultCollation || !defaultOperator {
			return true, false, fmt.Errorf("%w: %s", ErrMigrationIndexConflict, index.Name)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return true, false, err
	}
	if count != len(index.Columns) {
		return true, false, fmt.Errorf("%w: %s", ErrMigrationIndexConflict, index.Name)
	}
	return true, valid && ready, nil
}

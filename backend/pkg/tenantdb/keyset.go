package tenantdb

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tesserix/helivanta/pkg/pagination"
)

// ApplyKeyset adds the ordering, the position predicate and the +1 probe
// to a query.
//
// The +1 is deliberate: fetching one more row than asked answers "is
// there another page" without a second query. A COUNT would be both
// slower and wrong — it races inserts between the two queries.
// platform.ListRoute owns the trim (Task 3), so a handler never sees the
// extra row; this helper deliberately does not trim.
//
// The predicate is a row-value comparison, (created_at, id) < (?, ?),
// not two ANDed comparisons: created_at alone is not unique, so id is a
// required tiebreaker rather than a refinement. Postgres evaluates the
// row-value form as a single lexicographic comparison, which is also
// what lets it use a (created_at DESC, id DESC) index.
//
// It survives its own anchor being deleted: the comparison is on
// values, not on the row still existing, so a cursor whose row was
// deleted between pages still selects exactly the rows that follow that
// position.
//
// tx must already be scoped by WithTenant — this helper adds no tenant
// predicate of its own and relies entirely on forced RLS for isolation.
func ApplyKeyset(tx *gorm.DB, p pagination.Params) *gorm.DB {
	return ApplyOrderedKeyset(tx, p, Keyset{Column: "created_at", Direction: pagination.NewestFirst})
}

// Keyset is a list's ordering: a timestamp column, then id as the
// tiebreaker, in one direction (#1295 D3). The column is a code constant
// at the registering route, never client input.
type Keyset struct {
	Column    string
	Direction pagination.Direction
}

// ApplyOrderedKeyset is ApplyKeyset for any timestamp column and either
// direction. Oldest first is the same row-value comparison mirrored:
// (col, id) > (?, ?) under ORDER BY col ASC, id ASC.
func ApplyOrderedKeyset(tx *gorm.DB, p pagination.Params, k Keyset) *gorm.DB {
	col := clause.Column{Name: k.Column}
	id := clause.Column{Name: "id"}
	desc := k.Direction == pagination.NewestFirst
	q := tx.Order(clause.OrderBy{Columns: []clause.OrderByColumn{
		{Column: col, Desc: desc}, {Column: id, Desc: desc},
	}}).Limit(p.Limit + 1)
	if p.After != nil {
		op := "<"
		if !desc {
			op = ">"
		}
		q = q.Where("(?, ?) "+op+" (?, ?)", col, id, p.After.CreatedAt, p.After.ID)
	}
	return q
}

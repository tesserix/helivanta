package tenantdb

import (
	"gorm.io/gorm"

	"github.com/tesserix/hms/pkg/pagination"
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
	q := tx.Order("created_at DESC, id DESC").Limit(p.Limit + 1)
	if p.After != nil {
		q = q.Where("(created_at, id) < (?, ?)", p.After.CreatedAt, p.After.ID)
	}
	return q
}

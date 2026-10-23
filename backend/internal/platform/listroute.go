package platform

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

// Keyed is what a row must provide to be pageable: the two values the
// keyset orders by.
//
// A constraint rather than reflection over struct tags, so a row type
// that cannot say where it sits in the ordering is a compile error at
// the ListRoute call site rather than a runtime surprise on an empty
// cursor.
type Keyed interface {
	PageKey() (time.Time, uuid.UUID)
}

// ListHandler returns one page of rows. It receives the parsed params
// and returns up to Limit+1 rows — the extra row is the probe, and
// ListRoute trims it.
type ListHandler[T Keyed] func(c *gin.Context, p pagination.Params) ([]T, error)

// ListRoute registers a paginated collection endpoint.
//
// The platform layer owns everything a handler could forget: parsing and
// validating limit and cursor, trimming the probe row, deciding
// has_more, building the next cursor, and shaping the envelope. A
// handler cannot return a bare slice, cannot invent its own limit, and
// cannot report has_more from a stale count — none of those are
// expressible through this signature.
//
// This is the same guarantee *Router's unexported gin group gives for
// permissions (docs/standards/engineering-principles.md §4: prefer
// "impossible to express" over "documented convention").
//
// A package-level function rather than a method because Go methods
// cannot take type parameters.
func ListRoute[T Keyed](r *Router, path string, perm authz.Permission, h ListHandler[T]) {
	listRoute(r, "GET", path, perm, h)
}

// SearchRoute paginates a read-only POST whose sensitive filters belong in its body.
func SearchRoute[T Keyed](r *Router, path string, perm authz.Permission, h ListHandler[T]) {
	listRoute(r, "POST", path, perm, h)
}

func listRoute[T Keyed](r *Router, method, path string, perm authz.Permission, h ListHandler[T]) {
	r.handle(method, path, perm, true, []gin.HandlerFunc{func(c *gin.Context) {
		p, ok := authn.PrincipalFrom(c)
		if !ok {
			respond.Unauthenticated(c, "missing principal")
			return
		}
		params, err := pagination.Parse(c, p.TenantID)
		if err != nil {
			// Both invalid limits and invalid cursors are client errors.
			// The handler never runs — a bad page request must not reach
			// the database.
			respond.BadRequest(c, err)
			return
		}

		rows, err := h(c, params)
		if c.IsAborted() {
			return
		}
		if err != nil {
			respond.InternalErr(c, err, "could not list "+path)
			return
		}

		// The probe: the handler fetched Limit+1. More than Limit rows
		// means another page exists; the extra row is trimmed so the
		// client is never shown a row it did not ask for and the cursor
		// anchors on the last row it WAS shown. Anchoring on the probe
		// row instead would skip it on the next page.
		hasMore := len(rows) > params.Limit
		if hasMore {
			rows = rows[:params.Limit]
		}

		var next *string
		if hasMore && len(rows) > 0 {
			at, id := rows[len(rows)-1].PageKey()
			encoded := pagination.Cursor{TenantID: p.TenantID, CreatedAt: at, ID: id}.Encode()
			next = &encoded
		}

		// A nil slice marshals to null; panels branch on an array. Make
		// it empty rather than letting the zero value through.
		if rows == nil {
			rows = []T{}
		}
		respond.OK(c, gin.H{
			"data": rows,
			"page": gin.H{"next_cursor": next, "has_more": hasMore},
		})
	}})
}

// Scope applies a counted list's filter to a query.
type Scope func(*gorm.DB) *gorm.DB

// CountedListHandler validates the request and returns the list's filter.
// On a bad request it responds itself and returns false; the platform then
// opens no transaction.
type CountedListHandler func(c *gin.Context) (Scope, bool)

// CountedListRoute registers a list whose page carries a total (#1295 D1).
//
// The handler returns one Scope and the platform applies it twice, once to
// count and once with the keyset to fetch the page, so a total that counts
// something the rows do not is not expressible. Both statements run in one
// read-only REPEATABLE READ snapshot (D2), so a row committed between them
// cannot make them disagree. The page also reports offset, the rows before
// it in that snapshot, so a client shows a true range. The ordering, k, is fixed here by the
// registering code; a cursor issued for the other direction is a 400 (D3).
func CountedListRoute[T Keyed](r *Router, db *tenantdb.DB, path string, perm authz.Permission, k tenantdb.Keyset, h CountedListHandler) {
	d := DeclaredRoute{Method: "GET", Path: r.group.BasePath() + path, Permission: perm, Paginated: true, Counted: true}
	r.declare(path, d, []gin.HandlerFunc{func(c *gin.Context) {
		p, ok := authn.PrincipalFrom(c)
		if !ok {
			respond.Unauthenticated(c, "missing principal")
			return
		}
		params, err := pagination.ParseFor(c, p.TenantID, k.Direction)
		if err != nil {
			respond.BadRequest(c, err)
			return
		}
		scope, ok := h(c)
		if !ok || c.IsAborted() {
			return
		}
		var rows []T
		var total, offset int64
		err = db.WithTenantSnapshot(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			if err := scope(tx.Model(new(T))).Count(&total).Error; err != nil {
				return err
			}
			// The rows before this page, from the same snapshot, so the
			// range is right even after earlier rows were removed.
			if params.After != nil {
				if err := tenantdb.KeysetBefore(scope(tx.Model(new(T))), params, k).Count(&offset).Error; err != nil {
					return err
				}
			}
			return tenantdb.ApplyOrderedKeyset(scope(tx.Model(new(T))), params, k).Find(&rows).Error
		})
		if err != nil {
			respond.InternalErr(c, err, "could not list "+path)
			return
		}
		hasMore := len(rows) > params.Limit
		if hasMore {
			rows = rows[:params.Limit]
		}
		var next *string
		if hasMore && len(rows) > 0 {
			at, id := rows[len(rows)-1].PageKey()
			encoded := pagination.Cursor{TenantID: p.TenantID, CreatedAt: at, ID: id, Direction: k.Direction}.Encode()
			next = &encoded
		}
		if rows == nil {
			rows = []T{}
		}
		respond.OK(c, gin.H{
			"data": rows,
			"page": gin.H{"next_cursor": next, "has_more": hasMore, "total": total, "offset": offset},
		})
	}})
}

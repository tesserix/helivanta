package platform

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
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
	r.handle("GET", path, perm, true, []gin.HandlerFunc{func(c *gin.Context) {
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

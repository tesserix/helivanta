package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
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
	r.handle(method, path, perm, true, listHandlers(path, h))
}

// ScopedHospitalListRoute retains list envelopes under the shared hospital-scope gate.
func ScopedHospitalListRoute[T Keyed](r *Router, path string, permission authz.Permission, hospitalParam string, h ListHandler[T]) {
	r.handleHospitalScope("GET", path, permission, hospitalParam, true, listHandlers(path, h)...)
}

// QueryFilter is one cursor-bound list filter and its normalized default.
type QueryFilter struct{ Key, Default string }

// ScopedHospitalQueryListRoute binds new cursors to the caller, hospital and one normalized filter.
func ScopedHospitalQueryListRoute[T Keyed](r *Router, path string, permission authz.Permission, hospitalParam, queryKey, queryDefault string, maxLimit int, h ListHandler[T]) {
	scope := queryCursorScope(hospitalParam, []QueryFilter{{Key: queryKey, Default: queryDefault}}, maxLimit)
	r.handleHospitalScope("GET", path, permission, hospitalParam, true, listHandlersWithScope(path, h, scope, maxLimit)...)
}

// HandlerScopedQueryListRoute binds cursors to the caller, hospital and every filter; the handler proves hospital or department authority.
func HandlerScopedQueryListRoute[T Keyed](r *Router, path string, permission authz.Permission, hospitalParam string, filters []QueryFilter, maxLimit int, h ListHandler[T]) {
	scope := queryCursorScope(hospitalParam, filters, maxLimit)
	r.handleHandlerScope("GET", path, permission, hospitalParam, true, listHandlersWithScope(path, h, scope, maxLimit)...)
}

func queryCursorScope(scopeParam string, filters []QueryFilter, maxLimit int) func(*gin.Context, authn.Principal) string {
	valid := scopeParam != "" && len(filters) > 0 && maxLimit >= 1 && maxLimit <= pagination.MaxLimit
	for _, filter := range filters {
		valid = valid && filter.Key != "" && filter.Key != "cursor" && filter.Key != "limit"
	}
	if !valid {
		panic("platform: scoped query list requires a domain filter")
	}
	return func(c *gin.Context, p authn.Principal) string {
		key := p.Subject + "\x00" + c.FullPath() + "\x00" + strings.ToLower(c.Param(scopeParam))
		for _, filter := range filters {
			value := c.Query(filter.Key)
			if value == "" {
				value = filter.Default
			}
			key += "\x00" + value
		}
		digest := sha256.Sum256([]byte(key))
		return hex.EncodeToString(digest[:])
	}
}

// ScopedHospitalProjectionListRoute preserves list envelopes and legacy admission for opt-in scope consumers.
func ScopedHospitalProjectionListRoute[T Keyed](r *Router, path string, permission authz.Permission, hospitalParam, queryKey, version string, h ListHandler[T]) {
	r.handleHospitalScopeUsing("GET", path, permission, hospitalParam, true, authz.RequireHospitalScopeProjection(permission, hospitalParam, r.scopedAuthorizer, queryKey, version), listHandlers(path, h)...)
}

func listHandlers[T Keyed](path string, h ListHandler[T]) []gin.HandlerFunc {
	return listHandlersWithScope(path, h, nil, 0)
}

func listHandlersWithScope[T Keyed](path string, h ListHandler[T], scopeFor func(*gin.Context, authn.Principal) string, maxLimit int) []gin.HandlerFunc {
	return pageHandlers(path, h, scopeFor, maxLimit, func(rows []T) []T { return rows })
}

// Filtered is one scanned candidate; the cursor passes a withheld row without serving it.
type Filtered[T Keyed] struct {
	Row     T
	Visible bool
}

func (f Filtered[T]) PageKey() (time.Time, uuid.UUID) { return f.Row.PageKey() }

// HandlerScopedFilteredQueryListRoute is HandlerScopedQueryListRoute for rows that pass a per-row check after the scan.
func HandlerScopedFilteredQueryListRoute[T Keyed](r *Router, path string, permission authz.Permission, hospitalParam string, filters []QueryFilter, maxLimit int, h ListHandler[Filtered[T]]) {
	scope := queryCursorScope(hospitalParam, filters, maxLimit)
	r.handleHandlerScope("GET", path, permission, hospitalParam, true, pageHandlers(path, h, scope, maxLimit, visibleRows[T])...)
}

func visibleRows[T Keyed](scanned []Filtered[T]) []T {
	rows := make([]T, 0, len(scanned))
	for _, candidate := range scanned {
		if candidate.Visible {
			rows = append(rows, candidate.Row)
		}
	}
	return rows
}

func pageHandlers[T Keyed, O any](path string, h ListHandler[T], scopeFor func(*gin.Context, authn.Principal) string, maxLimit int, serve func([]T) []O) []gin.HandlerFunc {
	return []gin.HandlerFunc{func(c *gin.Context) {
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
		if maxLimit > 0 {
			if c.Query("limit") == "" {
				params.Limit = maxLimit
			}
			if params.Limit > maxLimit {
				respond.BadRequest(c, pagination.ErrInvalidLimit)
				return
			}
		}
		scope := ""
		if scopeFor != nil {
			scope = scopeFor(c, p)
			if params.After != nil && params.After.Scope != scope {
				respond.BadRequest(c, pagination.ErrInvalidCursor)
				return
			}
		}

		rows, err := h(c, params)
		if c.IsAborted() {
			return
		}
		if err != nil {
			respond.InternalErr(c, err, "could not list "+path)
			return
		}
		if maxLimit > 0 && len(rows) > params.Limit+1 {
			respond.InternalErr(c, errors.New("list page exceeds bound"), "could not list "+path)
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
			encoded := pagination.Cursor{TenantID: p.TenantID, CreatedAt: at, ID: id, Scope: scope}.Encode()
			next = &encoded
		}

		// A nil slice marshals to null; panels branch on an array. Make
		// it empty rather than letting the zero value through.
		served := serve(rows)
		if served == nil {
			served = []O{}
		}
		respond.OK(c, gin.H{
			"data": served,
			"page": gin.H{"next_cursor": next, "has_more": hasMore},
		})
	}}
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
	GuardedCountedListRoute[T](r, db, path, perm, k, func(c *gin.Context) (CountedQuery[T], bool) {
		scope, ok := h(c)
		return CountedQuery[T]{Guard: func(*gorm.DB) (Scope, error) { return scope, nil }}, ok
	})
}

// CountedQuery is a counted list that needs work inside its snapshot
// (#576 D1): an outlet list checks the caller's grant and reads the
// outlet's date there, so the check, the count and the rows see one state.
type CountedQuery[T any] struct {
	// Guard runs first, inside the snapshot, and returns the list's scope.
	// An error goes to Fail; nothing is counted.
	Guard func(tx *gorm.DB) (Scope, error)
	// Rows, if set, applies to the page fetch only: a Select in the shared
	// scope would break the count.
	Rows Scope
	// After, if set, runs on the fetched page inside the snapshot.
	After func(tx *gorm.DB, rows []T) error
	// Fail answers a Guard or After error with the module's own statuses.
	Fail func(c *gin.Context, err error)
}

// GuardedListHandler validates the request and returns its CountedQuery. On
// a bad request it responds itself and returns false; no transaction opens.
type GuardedListHandler[T any] func(c *gin.Context) (CountedQuery[T], bool)

// GuardedCountedListRoute is CountedListRoute for a list whose filter
// depends on work done inside the snapshot. Database errors from the count
// and the fetch stay a 500; errors from Guard and After are the module's.
func GuardedCountedListRoute[T Keyed](r *Router, db *tenantdb.DB, path string, perm authz.Permission, k tenantdb.Keyset, h GuardedListHandler[T]) {
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
		q, ok := h(c)
		if !ok || c.IsAborted() {
			return
		}
		var rows []T
		var total, offset int64
		var hasMore bool
		var moduleErr error
		err = db.WithTenantSnapshot(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			scope, err := q.Guard(tx)
			if err != nil {
				moduleErr = err
				return err
			}
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
			fetch := scope(tx.Model(new(T)))
			if q.Rows != nil {
				fetch = q.Rows(fetch)
			}
			if err := tenantdb.ApplyOrderedKeyset(fetch, params, k).Find(&rows).Error; err != nil {
				return err
			}
			hasMore = len(rows) > params.Limit
			if hasMore {
				rows = rows[:params.Limit]
			}
			if q.After != nil {
				if err := q.After(tx, rows); err != nil {
					moduleErr = err
					return err
				}
			}
			return nil
		})
		if moduleErr != nil {
			q.Fail(c, moduleErr)
			return
		}
		if err != nil {
			respond.InternalErr(c, err, "could not list "+path)
			return
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

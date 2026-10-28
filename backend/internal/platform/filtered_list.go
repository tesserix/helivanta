package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
)

// FilteredListRoute pages candidates before authorization filtering, preserving empty-page continuation.
func FilteredListRoute[T Keyed](r *Router, path string, permission authz.Permission, maxCandidates int, scan ListHandler[T], filter func(*gin.Context, []T) ([]T, error)) {
	if maxCandidates < 1 || maxCandidates > pagination.MaxLimit || scan == nil || filter == nil {
		panic("platform: invalid filtered list registration")
	}
	r.handle("GET", path, permission, true, []gin.HandlerFunc{func(c *gin.Context) {
		principal, ok := authn.PrincipalFrom(c)
		if !ok {
			respond.Unauthenticated(c, "missing principal")
			return
		}
		params, err := pagination.Parse(c, principal.TenantID)
		if err != nil {
			respond.BadRequest(c, err)
			return
		}
		if c.Query("limit") == "" {
			params.Limit = maxCandidates
		}
		if params.Limit > maxCandidates {
			respond.BadRequest(c, pagination.ErrInvalidLimit)
			return
		}
		digest := sha256.Sum256([]byte(principal.Subject + "\x00" + c.FullPath()))
		scope := hex.EncodeToString(digest[:])
		if params.After != nil && params.After.Scope != scope {
			respond.BadRequest(c, pagination.ErrInvalidCursor)
			return
		}
		candidates, err := scan(c, params)
		if c.IsAborted() {
			return
		}
		if err != nil {
			respond.InternalErr(c, err, "could not discover authorized scopes")
			return
		}
		if len(candidates) > params.Limit+1 {
			respond.InternalErr(c, errors.New("candidate page exceeds bound"), "could not discover authorized scopes")
			return
		}
		hasMore := len(candidates) > params.Limit
		if hasMore {
			candidates = candidates[:params.Limit]
		}
		var next *string
		if hasMore {
			at, id := candidates[len(candidates)-1].PageKey()
			cursor := pagination.Cursor{TenantID: principal.TenantID, CreatedAt: at, ID: id, Scope: scope}.Encode()
			next = &cursor
		}
		rows, err := filter(c, candidates)
		if c.IsAborted() {
			return
		}
		if err != nil {
			respond.InternalErr(c, err, "could not discover authorized scopes")
			return
		}
		if len(rows) > len(candidates) {
			respond.InternalErr(c, errors.New("filtered page exceeds candidate bound"), "could not discover authorized scopes")
			return
		}
		if rows == nil {
			rows = []T{}
		}
		respond.OK(c, gin.H{"data": rows, "page": gin.H{"has_more": hasMore, "next_cursor": next}})
	}})
}

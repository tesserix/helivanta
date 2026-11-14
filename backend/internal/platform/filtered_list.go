package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
)

// FilteredListRoute pages candidates before authorization filtering, preserving empty-page continuation.
func FilteredListRoute[T Keyed](r *Router, path string, permission authz.Permission, maxCandidates int, scan ListHandler[T], filter func(*gin.Context, []T) ([]T, error)) {
	filteredListRoute(r, path, permission, maxCandidates, scan, filter, false, nil)
}

// SealedFilteredListRoute hides candidate anchors that authorization may filter out.
func SealedFilteredListRoute[T Keyed](r *Router, path string, permission authz.Permission, maxCandidates int, scan ListHandler[T], filter func(*gin.Context, []T) ([]T, error)) {
	filteredListRoute(r, path, permission, maxCandidates, scan, filter, true, nil)
}

// SealedFilteredQueryListRoute binds private cursors to the caller, resource and filters.
func SealedFilteredQueryListRoute[T Keyed](r *Router, path string, permission authz.Permission, resourceParam string, filters []QueryFilter, maxCandidates int, scan ListHandler[T], filter func(*gin.Context, []T) ([]T, error), extraResources ...string) {
	if !hasSegment(r.group.BasePath()+path, resourceParam) {
		panic("platform: private query list requires its resource parameter")
	}
	scopeFor := queryCursorScope(resourceParam, filters, maxCandidates)
	if len(extraResources) > 0 {
		resources := append([]string(nil), extraResources...)
		seen := map[string]bool{resourceParam: true}
		for _, resource := range resources {
			if seen[resource] || !hasSegment(r.group.BasePath()+path, resource) {
				panic("platform: private query list requires distinct resource parameters")
			}
			seen[resource] = true
		}
		base := scopeFor
		scopeFor = func(c *gin.Context, p authn.Principal) string {
			key := base(c, p)
			for _, resource := range resources {
				key += "\x00" + strings.ToLower(c.Param(resource))
			}
			digest := sha256.Sum256([]byte(key))
			return hex.EncodeToString(digest[:])
		}
	}
	filteredListRoute(r, path, permission, maxCandidates, scan, filter, true, scopeFor)
}

func filteredListRoute[T Keyed](r *Router, path string, permission authz.Permission, maxCandidates int, scan ListHandler[T], filter func(*gin.Context, []T) ([]T, error), sealed bool, scopeFor func(*gin.Context, authn.Principal) string) {
	if maxCandidates < 1 || maxCandidates > pagination.MaxLimit || scan == nil || filter == nil {
		panic("platform: invalid filtered list registration")
	}
	r.handle("GET", path, permission, true, []gin.HandlerFunc{func(c *gin.Context) {
		principal, ok := authn.PrincipalFrom(c)
		if !ok {
			respond.Unauthenticated(c, "missing principal")
			return
		}
		digest := sha256.Sum256([]byte(principal.Subject + "\x00" + c.FullPath()))
		scope := hex.EncodeToString(digest[:])
		if scopeFor != nil {
			scope = scopeFor(c, principal)
		}
		var params pagination.Params
		var err error
		if sealed {
			if r.cursorSealer == nil {
				respond.Error(c, 503, "scoped_authorization_unavailable", "Private scope discovery is unavailable.")
				return
			}
			params, err = pagination.ParseSealed(c, principal.TenantID, scope, r.cursorSealer)
		} else {
			params, err = pagination.Parse(c, principal.TenantID)
		}
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
			anchor := pagination.Cursor{TenantID: principal.TenantID, CreatedAt: at, ID: id, Scope: scope}
			cursor := anchor.Encode()
			if sealed {
				cursor = r.cursorSealer.Seal(anchor)
			}
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

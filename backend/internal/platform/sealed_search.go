package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
)

// SealedSearchRoute binds a private read-only POST page to its normalized body and resource.
func SealedSearchRoute[T Keyed, I any](r *Router, path string, permission authz.Permission, resourceParam string, maxLimit int, prepare func(*gin.Context) (I, bool), scan func(*gin.Context, I, pagination.Params) ([]T, error)) {
	if maxLimit < 1 || maxLimit > pagination.MaxLimit || prepare == nil || scan == nil || !hasSegment(r.group.BasePath()+path, resourceParam) {
		panic("platform: private search requires a bounded body and resource")
	}
	const key = "platform.sealed_search"
	type prepared struct {
		input  I
		digest string
	}
	handlers := []gin.HandlerFunc{func(c *gin.Context) {
		if r.cursorSealer == nil {
			respond.Error(c, 503, "scoped_authorization_unavailable", "Private search is unavailable.")
			return
		}
		for query, values := range c.Request.URL.Query() {
			if (query != "cursor" && query != "limit") || len(values) != 1 {
				respond.BadRequest(c, errors.New("only one cursor and limit are permitted"))
				return
			}
		}
		input, ok := prepare(c)
		if c.IsAborted() {
			return
		}
		if !ok {
			respond.Error(c, 503, "scoped_authorization_unavailable", "Private search is unavailable.")
			return
		}
		raw, err := json.Marshal(input)
		if err != nil {
			respond.InternalErr(c, err, "could not prepare private search")
			return
		}
		digest := sha256.Sum256(raw)
		c.Set(key, prepared{input: input, digest: hex.EncodeToString(digest[:])})
		c.Next()
	}}
	scope := func(c *gin.Context, p authn.Principal) string {
		in := c.MustGet(key).(prepared)
		digest := sha256.Sum256([]byte(p.Subject + "\x00" + c.FullPath() + "\x00" + strings.ToLower(c.Param(resourceParam)) + "\x00" + in.digest))
		return hex.EncodeToString(digest[:])
	}
	page := func(c *gin.Context, params pagination.Params) ([]T, error) {
		return scan(c, c.MustGet(key).(prepared).input, params)
	}
	handlers = append(handlers, pageHandlers(path, page, scope, r.cursorSealer, maxLimit, func(rows []T) []T { return rows })...)
	r.handle("POST", path, permission, true, handlers)
}

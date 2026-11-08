package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
)

// ResourceListRoute binds continuation to the current actor, all resource parameters and filters; the owner still authorizes every page.
func ResourceListRoute[T Keyed](r *Router, path string, permission authz.Permission, maxLimit int, h ListHandler[T]) {
	if maxLimit < 1 || maxLimit > pagination.MaxLimit {
		panic("platform: resource list requires a bounded limit")
	}
	scope := func(c *gin.Context, p authn.Principal) string {
		values := []string{p.Subject, c.FullPath()}
		for _, param := range c.Params {
			values = append(values, param.Key, param.Value)
		}
		query := c.Request.URL.Query()
		query.Del("cursor")
		query.Del("limit")
		values = append(values, query.Encode())
		var key strings.Builder
		for _, value := range values {
			key.WriteString(strconv.Itoa(len(value)))
			key.WriteByte(':')
			key.WriteString(value)
		}
		sum := sha256.Sum256([]byte(key.String()))
		return hex.EncodeToString(sum[:])
	}
	r.handle("GET", path, permission, true, listHandlersWithScope(path, h, scope, maxLimit))
}

package platform

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/tesserix/hms/pkg/authz"
)

// Router wraps a gin route group so that every route must declare the
// permission it requires. This replaces the boot-time check the
// repo-setup spec proposed: an undeclared route is not expressible, so
// the guarantee holds at compile time rather than at startup.
//
// Use authz.Public for deliberately unguarded routes — it is an explicit,
// greppable opt-out rather than an omission.
type Router struct {
	group    *gin.RouterGroup
	declared *[]authz.Permission
}

func NewRouter(g *gin.RouterGroup) *Router {
	return &Router{group: g, declared: &[]authz.Permission{}}
}

// Group returns a nested router that shares the parent's declaration
// list, so Declared() sees every route regardless of nesting.
func (r *Router) Group(prefix string) *Router {
	return &Router{group: r.group.Group(prefix), declared: r.declared}
}

func (r *Router) GET(path string, perm authz.Permission, h ...gin.HandlerFunc) {
	r.handle(http.MethodGet, path, perm, h)
}

func (r *Router) POST(path string, perm authz.Permission, h ...gin.HandlerFunc) {
	r.handle(http.MethodPost, path, perm, h)
}

func (r *Router) PUT(path string, perm authz.Permission, h ...gin.HandlerFunc) {
	r.handle(http.MethodPut, path, perm, h)
}

func (r *Router) PATCH(path string, perm authz.Permission, h ...gin.HandlerFunc) {
	r.handle(http.MethodPatch, path, perm, h)
}

func (r *Router) DELETE(path string, perm authz.Permission, h ...gin.HandlerFunc) {
	r.handle(http.MethodDelete, path, perm, h)
}

func (r *Router) handle(method, path string, perm authz.Permission, h []gin.HandlerFunc) {
	*r.declared = append(*r.declared, perm)
	chain := append([]gin.HandlerFunc{authz.Require(perm)}, h...)
	r.group.Handle(method, path, chain...)
}

// Declared lists every permission declared through this router and its
// nested groups, including authz.Public. The architecture test and the
// adversarial matrix suite both build on it.
func (r *Router) Declared() []authz.Permission {
	return append([]authz.Permission(nil), *r.declared...)
}

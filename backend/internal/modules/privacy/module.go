// Package privacy owns tenant-local privacy casework and officer appointments.
package privacy

import (
	_ "embed"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

//go:embed schema.sql
var schema string

const PermOfficerManage authz.Permission = "privacy.officer.manage"

type Module struct{}

func New() *Module           { return &Module{} }
func (*Module) Name() string { return "privacy" }
func (*Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{ID: "0001_privacy", SQL: schema}}
}
func (*Module) Permissions() []authz.Grant { return []authz.Grant{{Permission: PermOfficerManage}} }
func (*Module) Routes(r *platform.Router, d platform.Deps) {
	h := &handlers{db: d.DB}
	g := r.Group("/privacy")
	g.PUT("/officers/:officer", PermOfficerManage, h.nominate)
	g.POST("/officers/:officer/accept", authz.Public, h.accept)
	g.POST("/officers/:officer/revoke", PermOfficerManage, h.revoke)
	g.GET("/officer", authz.Public, h.officer)
	g.PUT("/requests/:request", authz.Public, h.submit)
	g.GET("/requests/:request", authz.Public, h.get)
	g.PUT("/requests/:request/commands/:command", authz.Public, h.command)
	platform.ListRoute(g, "/requests", authz.Public, h.list)
	platform.ListRoute(g, "/queue", authz.Public, h.queue)
	platform.ListRoute(g, "/requests/:request/commands", authz.Public, h.history)
}
func (*Module) Publishes() []string                         { return nil }
func (*Module) DirectedSubjects() []string                  { return nil }
func (*Module) DirectedWriteTables() []string               { return nil }
func (*Module) Consumers(platform.Deps) []events.Consumer   { return nil }
func (*Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }

// Package practitioner owns tenant-scoped professional identity, separate from HR.
package practitioner

import (
	_ "embed"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const PermRecordManage authz.Permission = "practitioner.record.manage"
const PermRecordRead authz.Permission = "practitioner.record.read"

//go:embed records.sql
var recordsSchema string

//go:embed registrations.sql
var registrationsSchema string

type Module struct{}

func New() *Module           { return &Module{} }
func (*Module) Name() string { return "practitioner" }
func (*Module) Permissions() []authz.Grant {
	return []authz.Grant{{Permission: PermRecordManage}, {Permission: PermRecordRead}}
}
func (*Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{ID: "0001_practitioner", SQL: recordsSchema}, {ID: "0002_practitioner", SQL: registrationsSchema}}
}
func (*Module) Routes(r *platform.Router, d platform.Deps) {
	h := &handlers{db: d.DB}
	g := r.Group("/practitioners")
	g.PUT("/:practitioner", PermRecordManage, h.put)
	g.GET("/:practitioner", PermRecordRead, h.get)
	g.PUT("/:practitioner/registrations/:registration", PermRecordManage, h.putRegistration)
	g.GET("/:practitioner/registrations/:registration", PermRecordRead, h.getRegistration)
	g.PUT("/:practitioner/registrations/:registration/revisions/:revision", PermRecordManage, h.putRegistrationRevision)
	g.GET("/:practitioner/registrations/:registration/revisions/:revision", PermRecordRead, h.getRegistrationRevision)
	platform.ListRoute(g, "/:practitioner/registrations", PermRecordRead, h.listRegistrations)
	platform.ListRoute(g, "/:practitioner/registrations/:registration/revisions", PermRecordRead, h.listRegistrationRevisions)
}
func (*Module) Publishes() []string                         { return nil }
func (*Module) DirectedSubjects() []string                  { return nil }
func (*Module) DirectedWriteTables() []string               { return nil }
func (*Module) Consumers(platform.Deps) []events.Consumer   { return nil }
func (*Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }

// Package privacy owns tenant-local privacy casework and officer appointments.
package privacy

import (
	_ "embed"
	privacycontract "github.com/tesserix/helivanta/internal/modules/privacy/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"strings"
)

//go:embed schema.sql
var schema string

//go:embed deadlines.sql
var deadlineSchema string

const PermOfficerManage authz.Permission = "privacy.officer.manage"
const PermDeadlineManage authz.Permission = "privacy.deadline.manage"

type Module struct{}

func New() *Module           { return &Module{} }
func (*Module) Name() string { return "privacy" }
func (*Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{ID: "0001_privacy", SQL: schema}, {ID: "0002_privacy", SQL: strings.ReplaceAll(deadlineSchema, "${PRIVACY_DEADLINE_CHANGED}", privacycontract.SubjectDeadlineChanged)}}
}
func (*Module) Permissions() []authz.Grant {
	return []authz.Grant{{Permission: PermOfficerManage}, {Permission: PermDeadlineManage}}
}
func (*Module) Routes(r *platform.Router, d platform.Deps) {
	h := &handlers{db: d.DB, deadlinesEnabled: d.PrivacyDeadlinesEnabled && d.Workflows != nil}
	g := r.Group("/privacy")
	g.PUT("/deadline-policies/:policy", PermDeadlineManage, h.proposeDeadlinePolicy)
	g.POST("/deadline-policies/:policy/approve", authz.Public, h.approveDeadlinePolicy)
	g.PUT("/officers/:officer", PermOfficerManage, h.nominate)
	g.POST("/officers/:officer/accept", authz.Public, h.accept)
	g.POST("/officers/:officer/revoke", PermOfficerManage, h.revoke)
	g.GET("/officer", authz.Public, h.officer)
	g.PUT("/requests/:request", authz.Public, h.submit)
	g.GET("/requests/:request", authz.Public, h.get)
	g.GET("/requests/:request/deadline", authz.Public, h.deadline)
	g.PUT("/requests/:request/commands/:command", authz.Public, h.command)
	platform.ListRoute(g, "/requests", authz.Public, h.list)
	platform.ListRoute(g, "/queue", authz.Public, h.queue)
	platform.ListRoute(g, "/requests/:request/commands", authz.Public, h.history)
}
func (*Module) Publishes() []string           { return []string{privacycontract.SubjectDeadlineChanged} }
func (*Module) DirectedSubjects() []string    { return nil }
func (*Module) DirectedWriteTables() []string { return nil }
func (*Module) Consumers(d platform.Deps) []events.Consumer {
	if !d.PrivacyDeadlinesEnabled || d.Workflows == nil {
		return nil
	}
	return []events.Consumer{DeadlineConsumer(d.Workflows)}
}
func (*Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }

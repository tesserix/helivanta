// Package mediconnect owns patient-facing accounts and preferences.
package mediconnect

import (
	_ "embed"
	mediconnectcontract "github.com/tesserix/helivanta/internal/modules/mediconnect/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

//go:embed accounts.sql
var accountSchema string

//go:embed link_requests.sql
var linkRequestSchema string

//go:embed accessibility.sql
var accessibilitySchema string

//go:embed reviews.sql
var reviewSchema string

//go:embed review_source.sql
var reviewSourceSchema string

type Module struct{}

func New() *Module                         { return &Module{} }
func (*Module) Name() string               { return "mediconnect" }
func (*Module) Permissions() []authz.Grant { return nil }
func (*Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{ID: "0001_mediconnect", SQL: accountSchema}, {ID: "0002_mediconnect", SQL: linkRequestSchema}, {ID: "0003_mediconnect", SQL: accessibilitySchema}, {ID: "0004_mediconnect", SQL: reviewSchema}, {ID: "0005_mediconnect", SQL: reviewSourceSchema}}
}
func (*Module) Routes(r *platform.Router, deps platform.Deps) {
	h := &accountHandlers{db: deps.DB, bus: deps.Bus}
	g := r.Group("/mediconnect")
	// Membership is required; handlers scope ownership to the verified subject.
	g.POST("/account", authz.Public, h.create)
	g.GET("/account", authz.Public, h.get)
	g.PATCH("/account/preferences", authz.Public, h.preferences)
	g.PUT("/account/link-requests/:request", authz.Public, h.submitLinkRequest)
	g.GET("/account/link-requests/:request", authz.Public, h.getLinkRequest)
	platform.ListRoute(g, "/account/link-requests", authz.Public, h.listLinkRequests)
	g.POST("/account/link-requests/:request/review", authz.Public, h.submitReview)
	g.GET("/account/link-requests/:request/review", authz.Public, h.getReview)
	g.POST("/account/link-requests/:request/withdraw", authz.Public, h.withdrawLinkRequest)
}
func (*Module) Publishes() []string {
	return []string{mediconnectcontract.SubjectAccountCreated, mediconnectcontract.SubjectReviewRequested, mediconnectcontract.SubjectReviewWithdrawn}
}
func (*Module) DirectedSubjects() []string    { return nil }
func (*Module) DirectedWriteTables() []string { return nil }
func (*Module) Consumers(deps platform.Deps) []events.Consumer {
	return []events.Consumer{reviewResultConsumer(deps.Bus)}
}
func (*Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }

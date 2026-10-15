// Package supply owns private supplier records and procurement workflows.
package supply

import (
	_ "embed"
	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract" //nolint:depguard // own data-only event contract
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

//go:embed parties.sql
var partySchema string

const (
	PermPartyRead          authz.Permission = "supply.party.read"
	PermPartyWrite         authz.Permission = "supply.party.write"
	PermDirectoryRead      authz.Permission = "supply.directory.read"
	PermAccessManage       authz.Permission = "supply.party.access.manage"
	PermOrganizationManage authz.Permission = "supply.organization.manage"
)

type Module struct{}

func New() *Module           { return &Module{} }
func (*Module) Name() string { return "supply" }
func (*Module) Permissions() []authz.Grant {
	return []authz.Grant{
		{Permission: PermDirectoryRead, Roles: []authz.Role{authz.RoleSupplyAdmin, authz.RoleSupplyPurchasing, authz.RoleSupplyFinance, authz.RoleSupplyCompliance, authz.RoleSupplyAuditor, authz.RoleSupplyWarehouse, authz.RoleSupplySales}},
		{Permission: PermPartyRead, Roles: []authz.Role{authz.RoleSupplyAdmin, authz.RoleSupplyPurchasing, authz.RoleSupplyFinance, authz.RoleSupplyCompliance, authz.RoleSupplyAuditor}},
		{Permission: PermPartyWrite, Roles: []authz.Role{authz.RoleSupplyAdmin, authz.RoleSupplyPurchasing}},
		{Permission: PermOrganizationManage, Roles: []authz.Role{authz.RoleSupplyAdmin}},
		{Permission: PermAccessManage, Roles: []authz.Role{authz.RoleSupplyAdmin}},
	}
}
func (*Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{ID: "0001_supply", SQL: partySchema}}
}
func (*Module) Routes(r *platform.Router, deps platform.Deps) {
	h := &handlers{db: deps.DB, bus: deps.Bus}
	g := r.Group("/supply")
	platform.ListRoute(g, "/parties", PermDirectoryRead, h.listParties)
	g.PUT("/parties/:party", PermPartyWrite, h.putParty)
	g.GET("/parties/:party", PermPartyRead, h.getParty)
	platform.ListRoute(g, "/parties/:party/revisions", PermPartyRead, h.listRevisions)
	g.GET("/parties/:party/access", PermDirectoryRead, h.getAccess)
	g.PUT("/parties/:party/access/:subject/:role", PermAccessManage, h.putGrant)
	g.DELETE("/parties/:party/access/:subject/:role", PermAccessManage, h.deleteGrant)
}
func (*Module) Publishes() []string {
	return []string{supplycontract.SubjectPartyUpdated, supplycontract.SubjectPartyAccessChanged}
}
func (*Module) DirectedSubjects() []string                  { return nil }
func (*Module) DirectedWriteTables() []string               { return nil }
func (*Module) Consumers(platform.Deps) []events.Consumer   { return nil }
func (*Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }

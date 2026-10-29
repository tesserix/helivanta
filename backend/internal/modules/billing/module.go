// Package billing owns the hospital's billing ledger (#193). This slice
// keeps admission accounts (#197): what an admitted patient owes the
// hospital, posted by other modules through their published events and
// never by writing these tables.
package billing

import (
	_ "embed"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const (
	// PermAccountRead reads an admission's account at a hospital where the
	// caller holds a billing grant (ward staff answer a relative's question).
	PermAccountRead authz.Permission = "billing.account.read"
	// PermAccessManage grants and revokes billing access at a hospital.
	PermAccessManage authz.Permission = "billing.access.manage"
	// PermRateManage sets a hospital's bed-charging timezone and bed rates.
	PermRateManage authz.Permission = "billing.rate.manage"
)

//go:embed accounts.sql
var accountsSchema string

//go:embed bed_days.sql
var bedDaysSchema string

type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Name() string { return "billing" }

// Permissions: ward nurses and the front desk read accounts; only tenant
// administrators (implicitly) manage access and rates.
func (m *Module) Permissions() []authz.Grant {
	return []authz.Grant{
		{Permission: PermAccountRead, Roles: []authz.Role{authz.RoleNurse, authz.RoleReceptionist}},
		{Permission: PermAccessManage},
		{Permission: PermRateManage},
	}
}

func (m *Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{ID: "0001_billing", SQL: accountsSchema}, {ID: "0002_billing", SQL: bedDaysSchema}}
}

func (m *Module) Routes(r *platform.Router, deps platform.Deps) {
	h := &handlers{db: deps.DB}
	g := r.Group("/billing")
	hp := "/hospitals/:hospital"
	g.PUT(hp+"/access/:subject", PermAccessManage, h.grantAccess)
	g.DELETE(hp+"/access/:subject", PermAccessManage, h.revokeAccess)
	g.GET(hp+"/access", PermAccountRead, h.myAccess)
	g.GET(hp+"/admissions/:admission/account", PermAccountRead, h.account)
	platform.ListRoute(g, hp+"/admissions/:admission/entries", PermAccountRead, h.entries)
	g.PUT(hp+"/bed-charging", PermRateManage, h.setBedCharging)
	g.GET(hp+"/bed-charging", PermAccountRead, h.bedChargingSetup)
	g.PUT(hp+"/bed-rates/:class/:effective_from", PermRateManage, h.setBedRate)
	platform.ListRoute(g, hp+"/bed-rates", PermAccountRead, h.bedRates)
}

// Publishes declares none in this slice: nothing consumes billing yet.
func (m *Module) Publishes() []string { return nil }

// DirectedSubjects declares none: billing publishes nothing that creates
// data in another tenant.
func (m *Module) DirectedSubjects() []string { return nil }

// DirectedWriteTables declares none: billing writes no table on behalf of
// another tenant.
func (m *Module) DirectedWriteTables() []string { return nil }

func (m *Module) Consumers(platform.Deps) []events.Consumer { return consumers() }

// Broadcasts declares none: billing has no per-replica cache.
func (m *Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }

// Package drugmaster owns the drug master (#476): one global, versioned
// catalogue of molecules, formulations and products that prescribing,
// dispensing and supply read. See
// docs/superpowers/specs/2026-10-27-drug-master-design.md.
package drugmaster

import (
	_ "embed"

	"github.com/tesserix/helivanta/internal/modules/drugmaster/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// PermCatalogueRead reads published catalogue content. Nothing in the API
// writes it: content arrives only as an approved release (spec D3).
const PermCatalogueRead authz.Permission = "drugmaster.catalogue.read"

//go:embed drugmaster.sql
var schema string

type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Name() string { return "drugmaster" }

func (m *Module) Permissions() []authz.Grant {
	return []authz.Grant{{Permission: PermCatalogueRead, Roles: []authz.Role{authz.RoleDoctor, authz.RoleNurse, authz.RolePharmacist}}}
}

func (m *Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{ID: "0001_drugmaster", SQL: schema}}
}

func (m *Module) Routes(r *platform.Router, deps platform.Deps) {
	g := r.Group("/drugmaster")
	h := &readHandlers{db: deps.DB}
	g.GET("/search", PermCatalogueRead, h.search)
	g.GET("/formulations/:id", PermCatalogueRead, h.formulation)
	g.GET("/products/:id", PermCatalogueRead, h.product)
	g.GET("/products/by-gtin/:gtin", PermCatalogueRead, h.productByGTIN)
}

// Publishes declares the release events. They are written by
// cmd/drugmaster-release as helivanta_drug_approver, not by a request.
func (m *Module) Publishes() []string {
	return []string{contract.SubjectCataloguePublished, contract.SubjectFormulationReclassified, contract.SubjectProductStatusChanged}
}

// DirectedSubjects declares none: the catalogue's events carry no tenant.
func (m *Module) DirectedSubjects() []string { return nil }

// DirectedWriteTables declares none: no tenant data is written.
func (m *Module) DirectedWriteTables() []string { return nil }

// Consumers declares none: the drug master consumes nothing.
func (m *Module) Consumers(platform.Deps) []events.Consumer { return nil }

// Broadcasts declares none: reads go to Postgres, so there is no cache.
func (m *Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }

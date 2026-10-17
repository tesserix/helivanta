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

//go:embed branches.sql
var branchSchema string

//go:embed bins.sql
var binSchema string

//go:embed parties.sql
var partySchema string

//go:embed registration.sql
var registrationSchema string

//go:embed evidence.sql
var evidenceSchema string

const (
	PermBinRead            authz.Permission = "supply.bin.read"
	PermBinWrite           authz.Permission = "supply.bin.write"
	PermPartyRead          authz.Permission = "supply.party.read"
	PermPartyWrite         authz.Permission = "supply.party.write"
	PermDirectoryRead      authz.Permission = "supply.directory.read"
	PermAccessManage       authz.Permission = "supply.party.access.manage"
	PermPremiseRead        authz.Permission = "supply.premise.read"
	PermPremiseWrite       authz.Permission = "supply.premise.write"
	PermLicenseReview      authz.Permission = "supply.license.review"
	PermLicenseSubmit      authz.Permission = "supply.license.submit"
	PermLicenseRead        authz.Permission = "supply.license.read"
	PermLicenseWrite       authz.Permission = "supply.license.write"
	PermEvidenceRead       authz.Permission = "supply.evidence.read"
	PermEvidenceWrite      authz.Permission = "supply.evidence.write"
	PermOrganizationManage authz.Permission = "supply.organization.manage"
)

type Module struct{}

func New() *Module           { return &Module{} }
func (*Module) Name() string { return "supply" }
func (*Module) Permissions() []authz.Grant {
	return []authz.Grant{
		{Permission: PermBinRead, Roles: []authz.Role{authz.RoleSupplyAdmin, authz.RoleSupplyPurchasing, authz.RoleSupplyCompliance, authz.RoleSupplyAuditor}},
		{Permission: PermBinWrite, Roles: []authz.Role{authz.RoleSupplyAdmin, authz.RoleSupplyPurchasing}},
		{Permission: PermLicenseReview, Roles: []authz.Role{authz.RoleSupplyCompliance}},
		{Permission: PermLicenseSubmit, Roles: []authz.Role{authz.RoleSupplyAdmin, authz.RoleSupplyPurchasing, authz.RoleSupplyCompliance}},
		{Permission: PermDirectoryRead, Roles: []authz.Role{authz.RoleSupplyAdmin, authz.RoleSupplyPurchasing, authz.RoleSupplyFinance, authz.RoleSupplyCompliance, authz.RoleSupplyAuditor, authz.RoleSupplyWarehouse, authz.RoleSupplySales}},
		{Permission: PermPartyRead, Roles: []authz.Role{authz.RoleSupplyAdmin, authz.RoleSupplyPurchasing, authz.RoleSupplyFinance, authz.RoleSupplyCompliance, authz.RoleSupplyAuditor}},
		{Permission: PermPartyWrite, Roles: []authz.Role{authz.RoleSupplyAdmin, authz.RoleSupplyPurchasing}},
		{Permission: PermOrganizationManage, Roles: []authz.Role{authz.RoleSupplyAdmin}},
		{Permission: PermAccessManage, Roles: []authz.Role{authz.RoleSupplyAdmin}},
		{Permission: PermPremiseRead, Roles: []authz.Role{authz.RoleSupplyAdmin, authz.RoleSupplyPurchasing, authz.RoleSupplyFinance, authz.RoleSupplyCompliance, authz.RoleSupplyAuditor}},
		{Permission: PermPremiseWrite, Roles: []authz.Role{authz.RoleSupplyAdmin, authz.RoleSupplyPurchasing}},
		{Permission: PermLicenseRead, Roles: []authz.Role{authz.RoleSupplyAdmin, authz.RoleSupplyPurchasing, authz.RoleSupplyFinance, authz.RoleSupplyCompliance, authz.RoleSupplyAuditor}},
		{Permission: PermEvidenceRead, Roles: []authz.Role{authz.RoleSupplyAdmin, authz.RoleSupplyPurchasing, authz.RoleSupplyFinance, authz.RoleSupplyCompliance, authz.RoleSupplyAuditor}},
		{Permission: PermEvidenceWrite, Roles: []authz.Role{authz.RoleSupplyAdmin, authz.RoleSupplyPurchasing, authz.RoleSupplyCompliance}},
		{Permission: PermLicenseWrite, Roles: []authz.Role{authz.RoleSupplyAdmin, authz.RoleSupplyPurchasing, authz.RoleSupplyCompliance}},
	}
}
func (*Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{ID: "0001_supply", SQL: partySchema}, {ID: "0002_supply", SQL: registrationSchema}, {ID: "0003_supply", SQL: evidenceSchema}, {ID: "0004_supply", SQL: reviewSchema}, {ID: "0005_supply", SQL: binSchema}, {ID: "0006_supply", SQL: branchSchema}}
}
func (*Module) Routes(r *platform.Router, deps platform.Deps) {
	h := &handlers{db: deps.DB, bus: deps.Bus, identity: deps.OrganizationIdentity, roles: deps.Roles}
	g := r.Group("/supply")
	g.PUT("/parties/:party/premises/:premise/bins/:bin", PermBinWrite, h.putBin)
	g.GET("/parties/:party/premises/:premise/bins/:bin", PermBinRead, h.getBin)
	platform.ListRoute(g, "/parties/:party/premises/:premise/bins", PermBinRead, h.listBins)
	platform.ListRoute(g, "/parties/:party/premises/:premise/bins/:bin/revisions", PermBinRead, h.listBinRevisions)
	g.GET("/parties/:party/platform-identity", PermPartyRead, h.getPlatformIdentity)
	g.PUT("/parties/:party/licenses/:license/reviews/:review", PermLicenseSubmit, h.putLicenseReview)
	g.GET("/parties/:party/licenses/:license/review", PermLicenseRead, h.getLicenseReview)
	platform.ListRoute(g, "/parties/:party/licenses/:license/reviews", PermLicenseRead, h.listLicenseReviews)
	evidence := &evidenceHandlers{handlers: h, files: deps.Documents}
	g.PUT("/parties/:party/licenses/:license/evidence/:file", PermEvidenceWrite, evidence.putEvidence)
	g.PUT("/parties/:party/licenses/:license/evidence/:file/content", PermEvidenceWrite, evidence.uploadEvidence)
	g.GET("/parties/:party/licenses/:license/evidence/:file/download", PermEvidenceRead, evidence.downloadEvidence)
	g.POST("/parties/:party/licenses/:license/evidence/:file/revoke", PermEvidenceWrite, evidence.revokeEvidence)
	g.POST("/parties/:party/licenses/:license/evidence/:file/retry/:attempt", PermEvidenceWrite, evidence.retryEvidence)
	platform.ListRoute(g, "/parties/:party/licenses/:license/evidence", PermEvidenceRead, evidence.listEvidence)
	platform.ListRoute(g, "/parties", PermDirectoryRead, h.listParties)
	g.PUT("/parties/:party", PermPartyWrite, h.putParty)
	g.GET("/parties/:party", PermPartyRead, h.getParty)
	platform.ListRoute(g, "/parties/:party/revisions", PermPartyRead, h.listRevisions)
	g.GET("/parties/:party/access", PermDirectoryRead, h.getAccess)
	g.PUT("/parties/:party/access/:subject/:role", PermAccessManage, h.putGrant)
	g.DELETE("/parties/:party/access/:subject/:role", PermAccessManage, h.deleteGrant)
	g.PUT("/parties/:party/premises/:premise", PermPremiseWrite, h.putPremise)
	g.GET("/parties/:party/premises/:premise", PermPremiseRead, h.getPremise)
	platform.ListRoute(g, "/parties/:party/premises", PermPremiseRead, h.listPremises)
	platform.ListRoute(g, "/parties/:party/premises/:premise/revisions", PermPremiseRead, h.listPremiseRevisions)
	g.PUT("/parties/:party/licenses/:license", PermLicenseWrite, h.putLicense)
	g.GET("/parties/:party/licenses/:license", PermLicenseRead, h.getLicense)
	platform.ListRoute(g, "/parties/:party/licenses", PermLicenseRead, h.listLicenses)
	platform.ListRoute(g, "/parties/:party/licenses/:license/revisions", PermLicenseRead, h.listLicenseRevisions)
}
func (*Module) Publishes() []string {
	return []string{supplycontract.SubjectBinUpdated, supplycontract.SubjectPartyUpdated, supplycontract.SubjectPartyAccessChanged, supplycontract.SubjectPremiseUpdated, supplycontract.SubjectLicenseDeclared, supplycontract.SubjectEvidenceScanRequested, supplycontract.SubjectEvidenceChanged, supplycontract.SubjectLicenseReviewChanged}
}
func (*Module) DirectedSubjects() []string    { return nil }
func (*Module) DirectedWriteTables() []string { return nil }
func (*Module) Consumers(deps platform.Deps) []events.Consumer {
	if deps.Workflows == nil {
		return nil
	}
	return []events.Consumer{EvidenceScanConsumer(deps.Workflows)}
}
func (*Module) Broadcasts(platform.Deps) []events.Broadcast { return nil }

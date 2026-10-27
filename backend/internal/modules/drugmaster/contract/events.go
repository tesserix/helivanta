// Package contract is drugmaster's published event interface (#476 D6).
// Data only. Every event is tenant-less reference data: no patient, no
// tenant, so consumers re-read the catalogue rather than trust a payload.
package contract

// SubjectCataloguePublished is published once per approved release.
const SubjectCataloguePublished = "helivanta.in.drugmaster.catalogue_published.v1"

// SubjectFormulationReclassified is published for each formulation whose
// regulatory class or NDPS control a release changed.
const SubjectFormulationReclassified = "helivanta.in.drugmaster.formulation_reclassified.v1"

// SubjectProductStatusChanged is published for each product a release
// made recalled or discontinued, or returned to active.
const SubjectProductStatusChanged = "helivanta.in.drugmaster.product_status_changed.v1"

// CataloguePublishedData is the v1 payload of SubjectCataloguePublished.
type CataloguePublishedData struct {
	ReleaseID           string `json:"release_id"`
	Sequence            int64  `json:"sequence"`
	FormulationVersions int    `json:"formulation_versions"`
	ProductVersions     int    `json:"product_versions"`
}

// FormulationReclassifiedData is the v1 payload of SubjectFormulationReclassified.
type FormulationReclassifiedData struct {
	FormulationID string `json:"formulation_id"`
	VersionID     string `json:"version_id"`
	ValidFrom     string `json:"valid_from"`
	FromClass     string `json:"from_class"`
	ToClass       string `json:"to_class"`
	FromNDPS      bool   `json:"from_ndps"`
	ToNDPS        bool   `json:"to_ndps"`
}

// ProductStatusChangedData is the v1 payload of SubjectProductStatusChanged.
type ProductStatusChangedData struct {
	ProductID string `json:"product_id"`
	VersionID string `json:"version_id"`
	ValidFrom string `json:"valid_from"`
	Status    string `json:"status"`
	ReleaseID string `json:"release_id"`
}

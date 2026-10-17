// Package contract declares SupplyConnect's minimized published references.
package contract

const SubjectPartyUpdated = "helivanta.in.supply.party_updated.v1"

type PartyUpdatedData struct {
	PartyID string `json:"party_id"`
	Version int64  `json:"version"`
}

const SubjectPartyAccessChanged = "helivanta.in.supply.party_access_changed.v1"

type PartyAccessChangedData struct {
	PartyID   string `json:"party_id"`
	AccessID  string `json:"access_id"`
	Operation string `json:"operation"`
}

const SubjectPremiseUpdated = "helivanta.in.supply.premise_updated.v1"
const SubjectLicenseDeclared = "helivanta.in.supply.license_declared.v1"

type PremiseUpdatedData struct {
	PartyID   string `json:"party_id"`
	PremiseID string `json:"premise_id"`
	Version   int64  `json:"version"`
}
type LicenseDeclaredData struct {
	PartyID   string `json:"party_id"`
	LicenseID string `json:"license_id"`
	Version   int64  `json:"version"`
}

const SubjectEvidenceScanRequested = "helivanta.in.supply.evidence_scan_requested.v1"
const SubjectEvidenceChanged = "helivanta.in.supply.evidence_changed.v1"

type EvidenceScanRequestedData struct {
	PartyID     string `json:"party_id"`
	LicenseID   string `json:"license_id"`
	FileID      string `json:"file_id"`
	ScanAttempt int64  `json:"scan_attempt"`
}

type EvidenceScanInput struct {
	TenantID    string `json:"tenant_id"`
	PartyID     string `json:"party_id"`
	LicenseID   string `json:"license_id"`
	FileID      string `json:"file_id"`
	ScanAttempt int64  `json:"scan_attempt"`
}

type EvidenceChangedData struct {
	EvidenceScanRequestedData
	Status string `json:"status"`
}

const SubjectLicenseReviewChanged = "helivanta.in.supply.license_review_changed.v1"

type LicenseReviewChangedData struct {
	PartyID   string `json:"party_id"`
	LicenseID string `json:"license_id"`
	ReviewID  string `json:"review_id"`
	Version   int64  `json:"version"`
}

const SubjectBinUpdated = "helivanta.in.supply.bin_updated.v1"

type BinUpdatedData struct {
	PartyID   string `json:"party_id"`
	PremiseID string `json:"premise_id"`
	BinID     string `json:"bin_id"`
	Version   int64  `json:"version"`
}

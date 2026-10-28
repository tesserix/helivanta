// Package contract owns practitioner event wire data.
package contract

const EvidenceScanRequested = "helivanta.in.practitioner.evidence_scan_requested.v1"

// EvidenceScanRequestedData requires exactly one credential ID; bytes stay private.
type EvidenceScanRequestedData struct {
	PractitionerID  string `json:"practitioner_id"`
	RegistrationID  string `json:"registration_id,omitempty"`
	QualificationID string `json:"qualification_id,omitempty"`
	RevisionID      string `json:"revision_id"`
	FileID          string `json:"file_id"`
	ScanAttempt     int64  `json:"scan_attempt"`
}

const EvidenceChanged = "helivanta.in.practitioner.evidence_changed.v1"

// EvidenceScanInput pins one object and exactly one registration or qualification ID.
type EvidenceScanInput struct {
	TenantID        string `json:"tenant_id"`
	PractitionerID  string `json:"practitioner_id"`
	RegistrationID  string `json:"registration_id,omitempty"`
	QualificationID string `json:"qualification_id,omitempty"`
	RevisionID      string `json:"revision_id"`
	FileID          string `json:"file_id"`
	ScanAttempt     int64  `json:"scan_attempt"`
}

// EvidenceChangedData requires exactly one credential ID and omits credential content.
type EvidenceChangedData struct {
	PractitionerID  string `json:"practitioner_id"`
	RegistrationID  string `json:"registration_id,omitempty"`
	QualificationID string `json:"qualification_id,omitempty"`
	RevisionID      string `json:"revision_id"`
	FileID          string `json:"file_id"`
	ScanAttempt     int64  `json:"scan_attempt"`
	Status          string `json:"status"`
}

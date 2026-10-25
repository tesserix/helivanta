// Package contract owns practitioner event wire data.
package contract

const EvidenceScanRequested = "helivanta.in.practitioner.evidence_scan_requested.v1"

// EvidenceScanRequestedData contains only opaque scope; bytes and metadata stay private.
type EvidenceScanRequestedData struct {
	PractitionerID string `json:"practitioner_id"`
	RegistrationID string `json:"registration_id"`
	RevisionID     string `json:"revision_id"`
	FileID         string `json:"file_id"`
}

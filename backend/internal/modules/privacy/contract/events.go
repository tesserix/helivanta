// Package contract defines the privacy owner's metadata-only integration contracts.
package contract

// DeadlineWorkflowInput identifies one tenant-local clock without case content.
type DeadlineWorkflowInput struct {
	TenantID  string `json:"tenant_id"`
	RequestID string `json:"request_id"`
}

const SubjectDeadlineChanged = "helivanta.in.privacy.deadline_changed.v1"

// DeadlineChangedData carries only committed clock lineage.
type DeadlineChangedData struct {
	RequestID string `json:"request_id"`
	Version   int64  `json:"version"`
}

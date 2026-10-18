// Package contract owns emergency's published, metadata-only event interface.
package contract

const SubjectCaseReceived = "helivanta.in.emergency.case_received.v1"
const SubjectCaseClosed = "helivanta.in.emergency.case_closed.v1"
const SubjectFleetChanged = "helivanta.in.emergency.fleet_changed.v1"
const SubjectClinicalAccessChanged = "helivanta.in.emergency.clinical_access_changed.v1"
const SubjectCaseAssessed = "helivanta.in.emergency.case_assessed.v1"

type ClinicalAccessChangedData struct {
	BaseID  string `json:"base_id"`
	GrantID string `json:"grant_id"`
	Version int64  `json:"verification_version"`
}
type CaseAssessedData struct {
	BaseID       string `json:"base_id"`
	CaseID       string `json:"case_id"`
	AssessmentID string `json:"assessment_id"`
	Sequence     int64  `json:"assessment_sequence"`
}

type FleetChangedData struct {
	BaseID     string `json:"base_id"`
	ResourceID string `json:"resource_id"`
	Operation  string `json:"operation"`
}

type CaseChangedData struct {
	BaseID  string `json:"base_id"`
	CaseID  string `json:"case_id"`
	Version int64  `json:"case_version"`
	Status  string `json:"status"`
}

const SubjectDispatchChanged = "helivanta.in.emergency.dispatch_changed.v1"

type DispatchChangedData struct {
	BaseID     string `json:"base_id"`
	DispatchID string `json:"dispatch_id"`
	Version    int64  `json:"version"`
}

// DispatchWorkflowInput deliberately excludes patient, contact and location payloads.
type DispatchWorkflowInput struct {
	TenantID   string `json:"tenant_id"`
	BaseID     string `json:"base_id"`
	DispatchID string `json:"dispatch_id"`
}

// SubjectReceivingChanged directs an operational receipt notification back to its provider.
const SubjectReceivingChanged = "helivanta.in.emergency.receiving_changed.v1"

type ReceivingChangedData struct {
	BaseID     string `json:"base_id"`
	DispatchID string `json:"dispatch_id"`
	RequestID  string `json:"request_id"`
	Version    int64  `json:"version"`
}

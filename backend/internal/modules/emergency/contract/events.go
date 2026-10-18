// Package contract owns emergency's published, metadata-only event interface.
package contract

const SubjectCaseReceived = "helivanta.in.emergency.case_received.v1"
const SubjectCaseClosed = "helivanta.in.emergency.case_closed.v1"
const SubjectFleetChanged = "helivanta.in.emergency.fleet_changed.v1"

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

package contract

// ClaimCheckRequest is a private owner-to-owner lookup, never an event payload.
type ClaimCheckRequest struct {
	Relationship string `json:"relationship,omitempty"`
	TenantID     string `json:"tenant_id"`
	RequestID    string `json:"request_id"`
	HospitalID   string `json:"hospital_id"`
	PatientID    string `json:"patient_id"`
	Subject      string `json:"subject"`
}

// ClaimCheckResponse observes current intake state and confers no clinical authority.
type ClaimCheckResponse struct {
	Current bool `json:"current"`
}

// CurrentClaimPath is an internal owner endpoint, excluded from Global API routing.
const CurrentClaimPath = "/internal/v1/mediconnect/current-claim"

// ClaimWorkloadAudience scopes projected credentials to this owner operation.
const ClaimWorkloadAudience = "helivanta-mediconnect-claim"

// ClaimWorkloadHeader separates workload credentials from browser authorization.
const ClaimWorkloadHeader = "X-MediConnect-Workload-Token"

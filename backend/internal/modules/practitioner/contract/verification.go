package contract

const RegistrationPolicyChanged = "helivanta.in.practitioner.registration_policy_changed.v1"

// RegistrationPolicyChangedData requests an owner refresh without disclosing policy rules.
type RegistrationPolicyChangedData struct {
	PolicyID string `json:"policy_id"`
	Version  int64  `json:"version"`
}

const RegistrationVerificationChanged = "helivanta.in.practitioner.registration_verification_changed.v1"

// RegistrationVerificationChangedData requests a fresh owner read, never an access grant.
type RegistrationVerificationChangedData struct {
	PractitionerID string `json:"practitioner_id"`
	RegistrationID string `json:"registration_id"`
	VerificationID string `json:"verification_id"`
	Version        int64  `json:"version"`
}

const RegistrationSourceReconciled = "helivanta.in.practitioner.registration_source_reconciled.v1"

// RegistrationSourceReconciledData requests an owner refresh without exposing observations.
type RegistrationSourceReconciledData struct {
	PractitionerID   string `json:"practitioner_id"`
	RegistrationID   string `json:"registration_id"`
	ReconciliationID string `json:"reconciliation_id"`
}

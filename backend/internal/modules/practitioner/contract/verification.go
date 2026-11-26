package contract

const SubjectRegistrationPolicyChanged = "helivanta.in.practitioner.registration_policy_changed.v1"
const RegistrationPolicyChanged = SubjectRegistrationPolicyChanged

// RegistrationPolicyChangedData requests an owner refresh without disclosing policy rules.
type RegistrationPolicyChangedData struct {
	PolicyID string `json:"policy_id"`
	Version  int64  `json:"version"`
}

const SubjectRegistrationVerificationChanged = "helivanta.in.practitioner.registration_verification_changed.v1"
const RegistrationVerificationChanged = SubjectRegistrationVerificationChanged

// RegistrationVerificationChangedData requests a fresh owner read, never an access grant.
type RegistrationVerificationChangedData struct {
	PractitionerID string `json:"practitioner_id"`
	RegistrationID string `json:"registration_id"`
	VerificationID string `json:"verification_id"`
	Version        int64  `json:"version"`
}

const SubjectRegistrationSourceReconciled = "helivanta.in.practitioner.registration_source_reconciled.v1"
const RegistrationSourceReconciled = SubjectRegistrationSourceReconciled

// RegistrationSourceReconciledData requests an owner refresh without exposing observations.
type RegistrationSourceReconciledData struct {
	PractitionerID   string `json:"practitioner_id"`
	RegistrationID   string `json:"registration_id"`
	ReconciliationID string `json:"reconciliation_id"`
}

const SubjectRegistrationReconciliationWithdrawn = "helivanta.in.practitioner.registration_reconciliation_withdrawn.v1"
const RegistrationReconciliationWithdrawn = SubjectRegistrationReconciliationWithdrawn

// RegistrationReconciliationWithdrawnData requests a fresh owner read after trust withdrawal.
type RegistrationReconciliationWithdrawnData struct {
	PractitionerID   string `json:"practitioner_id"`
	RegistrationID   string `json:"registration_id"`
	ReconciliationID string `json:"reconciliation_id"`
	WithdrawalID     string `json:"withdrawal_id"`
}

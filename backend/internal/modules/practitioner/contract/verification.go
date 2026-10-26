package contract

const RegistrationPolicyChanged = "helivanta.in.practitioner.registration_policy_changed.v1"

// RegistrationPolicyChangedData requests an owner refresh without disclosing policy rules.
type RegistrationPolicyChangedData struct {
	PolicyID string `json:"policy_id"`
	Version  int64  `json:"version"`
}

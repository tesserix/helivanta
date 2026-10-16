package contract

const SubjectCountryPolicyChanged = "helivanta.in.workspace.country_policy_changed.v1"

type CountryPolicyChangedData struct {
	ScopeID   string  `json:"scope_id"`
	VersionID *string `json:"version_id"`
	Action    string  `json:"action"`
	Revision  int64   `json:"revision"`
}

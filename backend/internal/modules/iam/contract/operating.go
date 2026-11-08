package contract

const SubjectOperatingDutyChanged = "helivanta.in.iam.operating_assignment_changed.v1"

type OperatingDutyChangedData struct {
	DutyID  string `json:"duty_id"`
	Version int64  `json:"version"`
}

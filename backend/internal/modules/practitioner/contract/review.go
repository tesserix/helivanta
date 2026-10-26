package contract

const ReviewRecorded = "helivanta.in.practitioner.registration_review_recorded.v1"

// ReviewRecordedData signals an authorized owner metadata refresh; findings stay private.
type ReviewRecordedData struct {
	PractitionerID string `json:"practitioner_id"`
	RegistrationID string `json:"registration_id"`
	RevisionID     string `json:"revision_id"`
	ReviewID       string `json:"review_id"`
}

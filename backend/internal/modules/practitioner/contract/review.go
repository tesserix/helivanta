package contract

const SubjectReviewRecorded = "helivanta.in.practitioner.registration_review_recorded.v1"
const ReviewRecorded = SubjectReviewRecorded

// ReviewRecordedData has exactly one credential ID; source findings remain private.
type ReviewRecordedData struct {
	PractitionerID  string `json:"practitioner_id"`
	RegistrationID  string `json:"registration_id,omitempty"`
	QualificationID string `json:"qualification_id,omitempty"`
	RevisionID      string `json:"revision_id"`
	ReviewID        string `json:"review_id"`
}

const SubjectQualificationReviewRecorded = "helivanta.in.practitioner.qualification_review_recorded.v1"
const QualificationReviewRecorded = SubjectQualificationReviewRecorded

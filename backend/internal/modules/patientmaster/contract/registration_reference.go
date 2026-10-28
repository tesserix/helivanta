package contract

// RegistrationReference is a current scoped identity reference, never a record-access grant.
type RegistrationReference struct {
	PatientID              string `json:"patient_id"`
	HospitalID             string `json:"hospital_id"`
	RecordAccessAuthorized bool   `json:"record_access_authorized"`
}

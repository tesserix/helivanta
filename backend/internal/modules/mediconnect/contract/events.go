// Package contract defines MediConnect's published account events.
package contract

// SubjectAccountCreated carries only an opaque internal account reference.
const SubjectAccountCreated = "helivanta.in.mediconnect.account_created.v1"

// AccountCreatedData identifies the account created in the envelope's tenant.
type AccountCreatedData struct {
	AccountID string `json:"account_id"`
}

// Package contract is reference's published event interface. See
// medicore/contract for why this package exists and why it is data only.
package contract

// SubjectPinged is published when a ping is recorded. reference consumes
// its own event — it is the module that proves the platform wiring end
// to end (issue #2).
const SubjectPinged = "helivanta.in.reference.pinged.v1"

// PingedData is the v1 payload of SubjectPinged.
type PingedData struct {
	PingID string `json:"ping_id"`
}

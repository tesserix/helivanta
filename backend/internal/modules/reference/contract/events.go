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

// SubjectPingForwarded is published when a ping is forwarded to another
// tenant. It is the reference module's proof of the directed
// cross-tenant path (#932): the consumer writes into the event's
// DESTINATION tenant, not its origin.
const SubjectPingForwarded = "helivanta.in.reference.ping_forwarded.v1"

// PingForwardedData is the v1 payload of SubjectPingForwarded.
//
// Purpose-limited by construction: it carries what the receiving tenant
// needs to act, and nothing else about the origin's ping.
type PingForwardedData struct {
	PingID  string `json:"ping_id"`
	Message string `json:"message"`
}

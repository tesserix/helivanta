// Package contract is lab's published event interface. See
// medicore/contract for why this package exists and why it is data only.
package contract

// SubjectResultReady is published when a lab result is recorded. No
// module consumes it today.
const SubjectResultReady = "helivanta.in.lab.result_ready.v1"

// ResultReadyData is the v1 payload of SubjectResultReady.
type ResultReadyData struct {
	OrderID string `json:"order_id"`
	VisitID string `json:"visit_id"`
}

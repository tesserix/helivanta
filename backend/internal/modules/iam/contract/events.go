// Package contract is iam's published event interface. See
// medicore/contract for why this package exists and why it is data only.
package contract

const (
	// SubjectMemberGranted and SubjectMemberRevoked drive the FGA sync
	// consumers that keep OpenFGA's tuples matching Postgres.
	SubjectMemberGranted = "hms.in.iam.member_granted.v1"
	SubjectMemberRevoked = "hms.in.iam.member_revoked.v1"

	// SubjectCredentialRevoked is a broadcast, not a work queue: every
	// replica receives it and drops its cached revocation watermark
	// (#781). The durable truth is Postgres, so a dropped delivery
	// degrades to the cache TTL rather than to incorrectness.
	SubjectCredentialRevoked = "hms.in.iam.credential_revoked.v1" //nolint:gosec // an event subject name, not a credential value

)

// MemberChangedData is the v1 payload of both member_granted and
// member_revoked — the two events carry the same shape and differ only
// in what the consumer does with it.
type MemberChangedData struct {
	Subject string `json:"subject"`
	RoleKey string `json:"role_key"`
}

// CredentialRevokedData is the v1 payload of credential_revoked. It
// carries only the subject: every replica needs to know which cache
// entry to drop, and nothing else about a revocation belongs on a bus.
type CredentialRevokedData struct {
	Subject string `json:"subject"`
}

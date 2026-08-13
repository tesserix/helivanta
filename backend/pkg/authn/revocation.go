package authn

import (
	"context"
	"time"
)

// RevocationChecker reports the instant before which every credential
// for a subject is refused — the subject's revocation watermark.
//
// A zero time means "never revoked". Implementations MUST return an
// error rather than the zero time when they cannot answer: the two are
// indistinguishable to the caller, and only one of them should admit a
// request.
//
// Implemented by the iam module, which owns the table. Deliberately
// declared here and implemented there: pkg/ must not depend on
// internal/, and this package has no business knowing about Postgres.
type RevocationChecker interface {
	RevokedAfter(ctx context.Context, subject string) (time.Time, error)
}

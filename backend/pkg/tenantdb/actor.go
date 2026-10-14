package tenantdb

import "context"

// ActorSetting is the transaction-local GUC the audit change trigger reads (#54).
const ActorSetting = "app.actor_subject"

type actorKey struct{}

// WithActor records who is acting so WithTenant can stamp audited writes.
func WithActor(ctx context.Context, subject string) context.Context {
	return context.WithValue(ctx, actorKey{}, subject)
}

// ActorFrom returns the acting subject, or "" when none was recorded.
func ActorFrom(ctx context.Context) string {
	s, _ := ctx.Value(actorKey{}).(string)
	return s
}

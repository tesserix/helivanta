package authz

import (
	"context"
	openfga "github.com/openfga/go-sdk"
)

type currentAuthorizationKey struct{}

// WithCurrentAuthorization requests authoritative decisions at sensitive release boundaries.
func WithCurrentAuthorization(ctx context.Context) context.Context {
	return context.WithValue(ctx, currentAuthorizationKey{}, true)
}
func consistencyPreference(ctx context.Context) *openfga.ConsistencyPreference {
	if current, _ := ctx.Value(currentAuthorizationKey{}).(bool); !current {
		return nil
	}
	preference := openfga.CONSISTENCYPREFERENCE_HIGHER_CONSISTENCY
	return &preference
}

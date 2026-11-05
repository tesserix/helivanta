package authz

import (
	"context"
	"slices"
	"sync/atomic"
)

type scopedCommandEvidenceKey struct{}

type scopedCommandEvidence struct {
	checks []ScopeCheck
	done   <-chan struct{}
	active atomic.Bool
}

// WithScopedCommandEvidence is the IAM-only issuer after locking current authority.
func WithScopedCommandEvidence(ctx context.Context, checks []ScopeCheck, action func(context.Context) error) error {
	if action == nil || len(checks) < 1 || len(checks) > MaxScopedCommandChecks {
		return ErrScopedAuthorizationUnavailable
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	checks = slices.Clone(checks)
	for i := range checks {
		checks[i].Request.DepartmentPath = slices.Clone(checks[i].Request.DepartmentPath)
	}
	evidence := &scopedCommandEvidence{checks: checks, done: ctx.Done()}
	evidence.active.Store(true)
	defer evidence.active.Store(false)
	return action(context.WithValue(ctx, scopedCommandEvidenceKey{}, evidence))
}

// ScopedCommandGrant reads exact IAM evidence inside the active owner callback.
func ScopedCommandGrant(ctx context.Context, request ScopeRequest) (ScopeGrant, bool) {
	evidence, ok := ctx.Value(scopedCommandEvidenceKey{}).(*scopedCommandEvidence)
	if !ok || !evidence.active.Load() || ctx.Err() != nil {
		return ScopeGrant{}, false
	}
	select {
	case <-evidence.done:
		return ScopeGrant{}, false
	default:
	}
	for _, check := range evidence.checks {
		if check.Request.Same(request) {
			return check.Grant, true
		}
	}
	return ScopeGrant{}, false
}

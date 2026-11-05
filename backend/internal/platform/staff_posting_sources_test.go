package platform

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type sourceProviderProbe struct {
	Module
	name  string
	calls int
	guard authz.CurrentStaffPostingSourceGuard
}

func (m *sourceProviderProbe) Name() string { return m.name }
func (m *sourceProviderProbe) PostingSources(*tenantdb.DB, authz.DepartmentReferenceGuard, authz.CurrentStaffActorGuard) authz.CurrentStaffPostingSourceGuard {
	m.calls++
	return m.guard
}

type unavailablePostingSourceProbe struct{}

func (*unavailablePostingSourceProbe) WithCurrentStaffPosting(context.Context, authz.StaffPostingSourceScope, func(context.Context, authz.StaffPostingSourceReference) error) error {
	return authz.ErrScopedAuthorizationUnavailable
}

func TestStaffPostingSourceProviderRequiresWorkforceOwnership(t *testing.T) {
	guard := &unavailablePostingSourceProbe{}
	other := &sourceProviderProbe{name: "pharmacy", guard: guard}
	require.Nil(t, StaffPostingSources([]Module{other}, nil))
	require.Zero(t, other.calls)
	owner := &sourceProviderProbe{name: "workforce", guard: guard}
	require.Same(t, guard, StaffPostingSources([]Module{other, owner}, nil))
	require.Zero(t, other.calls)
	require.Equal(t, 1, owner.calls)
	require.Nil(t, StaffPostingSources(nil, nil))
}

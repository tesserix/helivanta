package globalapi

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/pharmacy"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
)

func (f supplierCompoundFixture) assertHospitalBuyerOwnership(t *testing.T, actor operatingDutyFixture, buyer suppliertrade.Endpoint, department uuid.UUID) {
	t.Helper()
	modules := f.registry.All()
	endpoints := platform.TradingEndpoints(modules, f.db, platform.DepartmentReferences(modules, f.db))
	departments, ok := endpoints.(suppliertrade.HospitalDepartmentGuard)
	require.True(t, ok)
	administration := iam.NewSourceAdministrationGuard(f.db, f.graph)
	cases := []struct {
		name   string
		change func(*suppliertrade.Endpoint)
	}{
		{"current owner", func(*suppliertrade.Endpoint) {}},
		{"wrong organization", func(e *suppliertrade.Endpoint) { e.OrganizationID = uuid.Must(uuid.NewV7()) }},
		{"wrong outlet", func(e *suppliertrade.Endpoint) { e.LocationID = uuid.Must(uuid.NewV7()) }},
		{"wrong hospital", func(e *suppliertrade.Endpoint) { id := uuid.Must(uuid.NewV7()); e.HospitalID = &id }},
		{"wrong binding", func(e *suppliertrade.Endpoint) { id := uuid.Must(uuid.NewV7()); e.BindingID = &id }},
		{"stale binding version", func(e *suppliertrade.Endpoint) { version := *e.BindingVersion - 1; e.BindingVersion = &version }},
	}
	for _, check := range []string{"trading endpoint", "hospital department"} {
		for _, tc := range cases {
			require.True(t, t.Run(check+" "+tc.name, func(t *testing.T) {
				endpoint := buyer
				tc.change(&endpoint)
				called := false
				err := administration.WithSourceAdministration(t.Context(), actor.principal, pharmacy.PermTradingScopeRead, func(ctx context.Context) error {
					callback := func(current context.Context) error {
						require.True(t, f.db.GuardedCommandActive(current))
						called = true
						return nil
					}
					if check == "hospital department" {
						return departments.WithHospitalDepartment(ctx, endpoint, department, callback)
					}
					return endpoints.WithCurrentTradingEndpoint(ctx, endpoint, "prescription", "order", func(current context.Context, ref suppliertrade.EndpointReference) error {
						require.True(t, suppliertrade.EndpointEqual(buyer, ref.Endpoint))
						return callback(current)
					})
				})
				if tc.name == "current owner" {
					require.NoError(t, err)
					require.True(t, called)
				} else {
					require.ErrorIs(t, err, suppliertrade.ErrIneligible)
					require.False(t, called)
				}
			}))
		}
	}
	require.True(t, t.Run("hospital department rejects another department", func(t *testing.T) {
		called := false
		err := administration.WithSourceAdministration(t.Context(), actor.principal, pharmacy.PermTradingScopeRead, func(ctx context.Context) error {
			return departments.WithHospitalDepartment(ctx, buyer, uuid.Must(uuid.NewV7()), func(context.Context) error { called = true; return nil })
		})
		require.ErrorIs(t, err, suppliertrade.ErrIneligible)
		require.False(t, called)
	}))
}

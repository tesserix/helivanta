package platform

import (
	"context"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"testing"
	"time"
)

type locationChoiceOwner struct {
	rows  []authz.OperatingLocationChoice
	calls int
}

func (o *locationChoiceOwner) CurrentOperatingLocation(context.Context, authz.OperatingLocationScope) (authz.OperatingLocationReference, error) {
	return authz.OperatingLocationReference{}, authz.ErrScopedAuthorizationUnavailable
}
func (o *locationChoiceOwner) OperatingLocationChoices(context.Context, authz.GovernedIndependentEmployerRequest, pagination.Params) ([]authz.OperatingLocationChoice, error) {
	o.calls++
	return o.rows, nil
}
func locationChoiceSource() (authz.GovernedIndependentEmployerRequest, authz.OperatingLocationChoice) {
	now := time.Now()
	employer := uuid.Must(uuid.NewV7())
	tenant := uuid.NewString()
	request := authz.GovernedIndependentEmployerRequest{Principal: authn.Principal{TenantID: tenant, Subject: "synthetic-location-preparer", AuthTime: now.Add(-time.Minute), IdleDeadline: now.Add(time.Hour)}, Scope: authz.IndependentEmployerScope{TenantID: tenant, EmployerKind: "pharmacy", EmployerID: employer}, Capability: authz.GovernanceOperatingManage, Source: authz.GovernanceSourceEvidence{Kind: authz.GovernanceOwnerSource, OwnerMembershipID: uuid.New(), OwnerCount: 2, MembershipEpoch: 1, Scope: authz.GovernanceEstablishmentScope{TenantID: tenant, Kind: "pharmacy", ID: &employer}, ExpiresAt: now.Add(time.Hour)}}
	row := authz.OperatingLocationChoice{Reference: authz.OperatingLocationReference{Scope: authz.OperatingLocationScope{TenantID: tenant, EmployerKind: "pharmacy", EmployerID: employer, LocationID: uuid.Must(uuid.NewV7())}, EmployerVersion: 1, LocationVersion: 1, Country: "IN"}, Code: "SYN-CHOICE", Name: "Synthetic choice", CreatedAt: now.Add(-time.Minute)}
	return request, row
}
func TestOperatingLocationChoicesRefusesForeignCursorBeforeOwner(t *testing.T) {
	request, _ := locationChoiceSource()
	for name, page := range map[string]pagination.Params{
		"another tenant":   {Limit: 20, After: &pagination.Cursor{TenantID: uuid.NewString()}},
		"reverse ordering": {Limit: 20, Direction: pagination.OldestFirst},
	} {
		t.Run(name, func(t *testing.T) {
			owner := &locationChoiceOwner{}
			guard := operatingLocations{sources: map[string]authz.CurrentOperatingLocationGuard{"pharmacy": owner}}
			rows, err := guard.OperatingLocationChoices(t.Context(), request, page)
			require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
			require.Nil(t, rows)
			require.Zero(t, owner.calls)
		})
	}
}
func TestOperatingLocationChoicesValidatesOwnerProjection(t *testing.T) {
	request, row := locationChoiceSource()
	owner := &locationChoiceOwner{rows: []authz.OperatingLocationChoice{row}}
	guard := operatingLocations{sources: map[string]authz.CurrentOperatingLocationGuard{"pharmacy": owner}}
	rows, err := guard.OperatingLocationChoices(t.Context(), request, pagination.Params{Limit: 20})
	require.NoError(t, err)
	require.Equal(t, owner.rows, rows)
	for name, change := range map[string]func(*authz.OperatingLocationChoice){
		"tenant":         func(r *authz.OperatingLocationChoice) { r.Reference.Scope.TenantID = uuid.NewString() },
		"employer":       func(r *authz.OperatingLocationChoice) { r.Reference.Scope.EmployerID = uuid.Must(uuid.NewV7()) },
		"kind":           func(r *authz.OperatingLocationChoice) { r.Reference.Scope.EmployerKind = "supplier" },
		"location":       func(r *authz.OperatingLocationChoice) { r.Reference.Scope.LocationID = uuid.Nil },
		"country":        func(r *authz.OperatingLocationChoice) { r.Reference.Country = "PK" },
		"version":        func(r *authz.OperatingLocationChoice) { r.Reference.LocationVersion = 0 },
		"unsafe version": func(r *authz.OperatingLocationChoice) { r.Reference.EmployerVersion = 9007199254740992 },
		"missing name":   func(r *authz.OperatingLocationChoice) { r.Name = " " },
		"control name":   func(r *authz.OperatingLocationChoice) { r.Name = "synthetic\x00choice" },
		"missing anchor": func(r *authz.OperatingLocationChoice) { r.CreatedAt = time.Time{} },
	} {
		t.Run(name, func(t *testing.T) {
			changed := row
			change(&changed)
			owner.rows = []authz.OperatingLocationChoice{changed}
			rows, err := guard.OperatingLocationChoices(t.Context(), request, pagination.Params{Limit: 20})
			require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
			require.Nil(t, rows)
		})
	}
	t.Run("duplicate locations", func(t *testing.T) {
		owner.rows = []authz.OperatingLocationChoice{row, row}
		rows, err := guard.OperatingLocationChoices(t.Context(), request, pagination.Params{Limit: 20})
		require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
		require.Nil(t, rows)
	})
	t.Run("missing owner", func(t *testing.T) {
		missing := operatingLocations{}
		rows, err := missing.OperatingLocationChoices(t.Context(), request, pagination.Params{Limit: 20})
		require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable)
		require.Nil(t, rows)
	})
}

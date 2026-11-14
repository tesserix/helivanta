package platform

import (
	"context"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type operatingLocationProvider interface {
	OperatingLocations(*tenantdb.DB) authz.CurrentOperatingLocationGuard
}
type operatingLocations struct {
	sources map[string]authz.CurrentOperatingLocationGuard
}

func OperatingLocations(modules []Module, db *tenantdb.DB) authz.CurrentOperatingLocationGuard {
	sources := map[string]authz.CurrentOperatingLocationGuard{}
	for kind, name := range map[string]string{"supplier": "supply", "pharmacy": "pharmacy"} {
		if owner, ok := soleOwner(modules, db, name).(operatingLocationProvider); ok {
			sources[kind] = owner.OperatingLocations(db)
		}
	}
	return operatingLocations{sources: sources}
}
func (g operatingLocations) CurrentOperatingLocation(ctx context.Context, scope authz.OperatingLocationScope) (authz.OperatingLocationReference, error) {
	guard := g.sources[scope.EmployerKind]
	if guard == nil || !authz.ValidOperatingLocationScope(scope) {
		return authz.OperatingLocationReference{}, authz.ErrScopedAuthorizationUnavailable
	}
	result, err := guard.CurrentOperatingLocation(ctx, scope)
	if err != nil {
		return authz.OperatingLocationReference{}, err
	}
	if result.Scope != scope || result.Country != "IN" || result.EmployerVersion < 1 || result.LocationVersion < 1 {
		return authz.OperatingLocationReference{}, authz.ErrScopedAuthorizationUnavailable
	}
	return result, nil
}

func (g operatingLocations) OperatingLocationChoices(ctx context.Context, request authz.GovernedIndependentEmployerRequest, page pagination.Params) ([]authz.OperatingLocationChoice, error) {
	if _, err := authz.OperatingLocationChoiceProof(request, time.Now()); err != nil {
		return nil, err
	}
	guard := g.sources[request.Scope.EmployerKind]
	if guard == nil || page.Limit < 1 || page.Limit > 20 || page.Direction != pagination.NewestFirst || page.After != nil && (page.After.TenantID != request.Scope.TenantID || page.After.Direction != pagination.NewestFirst) {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	rows, err := guard.OperatingLocationChoices(ctx, request, page)
	if err != nil {
		return nil, err
	}
	if len(rows) > page.Limit+1 {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	seen := make(map[uuid.UUID]bool, len(rows))
	for i, row := range rows {
		ref := row.Reference
		if ref.Scope.TenantID != request.Scope.TenantID || ref.Scope.EmployerKind != request.Scope.EmployerKind || ref.Scope.EmployerID != request.Scope.EmployerID || !authz.ValidOperatingLocationScope(ref.Scope) || ref.Country != "IN" || ref.EmployerVersion < 1 || ref.EmployerVersion > 9007199254740991 || ref.LocationVersion < 1 || ref.LocationVersion > 9007199254740991 || row.CreatedAt.IsZero() || seen[ref.Scope.LocationID] || !operatingChoiceLabel(row.Code, 40) || !operatingChoiceLabel(row.Name, 200) {
			return nil, authz.ErrScopedAuthorizationUnavailable
		}
		if i > 0 && (rows[i-1].CreatedAt.Before(row.CreatedAt) || rows[i-1].CreatedAt.Equal(row.CreatedAt) && rows[i-1].Reference.Scope.LocationID.String() <= ref.Scope.LocationID.String()) {
			return nil, authz.ErrScopedAuthorizationUnavailable
		}
		seen[ref.Scope.LocationID] = true
	}
	return rows, nil
}
func operatingChoiceLabel(value string, limit int) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= limit && strings.IndexFunc(value, func(r rune) bool { return unicode.IsControl(r) }) < 0
}

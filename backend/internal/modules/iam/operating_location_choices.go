package iam

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"gorm.io/gorm"
)

type operatingLocationChoiceRow struct {
	Choice    OperatingLocationChoice
	CreatedAt time.Time
}

func (r operatingLocationChoiceRow) MarshalJSON() ([]byte, error) { return json.Marshal(r.Choice) }
func (r operatingLocationChoiceRow) PageKey() (time.Time, uuid.UUID) {
	return r.CreatedAt, r.Choice.LocationID
}

func registerOperatingLocationChoices(r *platform.Router, h *operatingHandlers) {
	platform.SealedFilteredQueryListRoute(r, "/operating-governance-employers/:kind/:employer/location-choices", authz.Permission(authz.GovernanceOperatingManage), "employer", []platform.QueryFilter{{Key: "country", Default: "IN"}}, 20, func(c *gin.Context, page pagination.Params) ([]operatingLocationChoiceRow, error) {
		native, ok := h.nativeHandler(c)
		if !ok {
			return nil, nil
		}
		return native.locationChoices(c, page)
	}, func(_ *gin.Context, rows []operatingLocationChoiceRow) ([]operatingLocationChoiceRow, error) {
		return rows, nil
	}, "kind")
}
func operatingLocationChoiceQuery(c *gin.Context) bool {
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil || c.Request.URL.ForceQuery && c.Request.URL.RawQuery == "" {
		return false
	}
	for key, values := range query {
		if (key != "country" && key != "limit" && key != "cursor") || len(values) != 1 || values[0] == "" || key == "country" && values[0] != "IN" {
			return false
		}
	}
	return true
}
func (h *operatingHandlers) locationChoices(c *gin.Context, page pagination.Params) ([]operatingLocationChoiceRow, error) {
	if _, _, ok := authn.TenantPrincipal(c); !ok {
		return nil, nil
	}
	p, _, ok := h.scope(c, PermOperatingRead)
	if !ok {
		return nil, nil
	}
	if !operatingLocationChoiceQuery(c) {
		respond.Error(c, 400, "invalid_request", "Choose one location page for the supported country.")
		return nil, nil
	}
	if h.native == nil || h.locations == nil || p.IdleDeadline.IsZero() {
		operatingError(c, authz.ErrScopedAuthorizationUnavailable)
		return nil, nil
	}
	deadline := time.Now().Add(5 * time.Second)
	if p.IdleDeadline.Before(deadline) {
		deadline = p.IdleDeadline
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), deadline)
	defer cancel()
	proof, err := h.actorProof(ctx, p, PermOperatingRead)
	if err != nil {
		h.operatingError(c, err)
		return nil, nil
	}
	tenant, err := uuid.Parse(p.TenantID)
	if err != nil || tenant == uuid.Nil {
		operatingError(c, authz.ErrScopedAuthorizationUnavailable)
		return nil, nil
	}
	rows := []operatingLocationChoiceRow{}
	callbacks := 0
	var callbackErr error
	err = h.withAdministration(ctx, p, proof, "read", func(commandCtx context.Context, _ *gorm.DB) error {
		callbacks++
		if callbacks != 1 || h.native.active == nil {
			callbackErr = authz.ErrScopedAuthorizationUnavailable
			return callbackErr
		}
		scope := OperatingDutyScope{EmployerKind: h.native.scope.EmployerKind, EmployerID: h.native.scope.EmployerID}
		callbackErr = h.governedEmployerAccess(commandCtx, p.TenantID, scope, func(ownerCtx context.Context) error {
			native := h.native.active
			selected, err := h.locations.OperatingLocationChoices(ownerCtx, authz.GovernedIndependentEmployerRequest{Principal: p, Scope: authz.IndependentEmployerScope{TenantID: p.TenantID, EmployerKind: scope.EmployerKind, EmployerID: scope.EmployerID}, Capability: native.capability, Source: native.source}, page)
			if err != nil {
				return err
			}
			for _, location := range selected {
				ref := location.Reference
				rows = append(rows, operatingLocationChoiceRow{Choice: OperatingLocationChoice{OwnerTenantID: tenant, EmployerKind: ref.Scope.EmployerKind, EmployerID: ref.Scope.EmployerID, LocationID: ref.Scope.LocationID, Code: location.Code, Name: location.Name, CountryCode: ref.Country, EmployerVersion: ref.EmployerVersion, LocationVersion: ref.LocationVersion}, CreatedAt: location.CreatedAt})
			}
			return nil
		})
		return callbackErr
	})
	if callbackErr != nil {
		err = callbackErr
	}
	if err == nil && callbacks != 1 {
		err = authz.ErrScopedAuthorizationUnavailable
	}
	if err != nil {
		switch {
		case errors.Is(err, authz.ErrGovernanceScopeHidden), errors.Is(err, authz.ErrScopedCommandDenied), errors.Is(err, gorm.ErrRecordNotFound):
			respond.NotFound(c, "operating employer")
		case errors.Is(err, authz.ErrScopedCredentialRevoked):
			operatingError(c, err)
		default:
			operatingError(c, authz.ErrScopedAuthorizationUnavailable)
		}
		return nil, nil
	}
	return rows, nil
}

package workspace

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tradingpolicy"
	"gorm.io/gorm"
)

func decodePolicy(c *gin.Context, target any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		respond.Error(c, 400, "invalid_request", "provide a complete country policy within 128 KiB")
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		respond.Error(c, 400, "invalid_request", "expected one country policy object")
		return false
	}
	return true
}

func (h handler) listCountryPolicies(c *gin.Context, page pagination.Params) ([]tradingpolicy.PolicyScope, error) {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	var scopes []tradingpolicy.PolicyScope
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		var err error
		scopes, err = tradingpolicy.ListScopes(tx, p.TenantID, p.Subject, page)
		return err
	})
	if err != nil {
		countryPolicyError(c, err)
		return nil, nil
	}
	c.Header("Cache-Control", "no-store")
	return scopes, nil
}

func (h handler) listCountryPolicyVersions(c *gin.Context, page pagination.Params) ([]tradingpolicy.VersionSummary, error) {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	if !validPolicyScopeID(c) {
		return nil, nil
	}
	var versions []tradingpolicy.VersionSummary
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		var err error
		versions, err = tradingpolicy.ListVersions(tx, p.TenantID, p.Subject, c.Param("scope"), page)
		return err
	})
	if err != nil {
		countryPolicyError(c, err)
		return nil, nil
	}
	c.Header("Cache-Control", "no-store")
	return versions, nil
}

func (h handler) listCountryPolicyActions(c *gin.Context, page pagination.Params) ([]tradingpolicy.Receipt, error) {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	if !validPolicyScopeID(c) {
		return nil, nil
	}
	var actions []tradingpolicy.Receipt
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		var err error
		actions, err = tradingpolicy.ListActions(tx, p.TenantID, p.Subject, c.Param("scope"), page)
		return err
	})
	if err != nil {
		countryPolicyError(c, err)
		return nil, nil
	}
	c.Header("Cache-Control", "no-store")
	return actions, nil
}

func validPolicyScopeID(c *gin.Context) bool {
	id, err := uuid.Parse(c.Param("scope"))
	if err != nil || id.Version() != 7 {
		respond.NotFound(c, "country policy")
		return false
	}
	return true
}

func countryPolicyError(c *gin.Context, err error) {
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) && databaseError.Code == "23505" {
		err = tradingpolicy.ErrConflict
	}
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		respond.NotFound(c, "country policy")
	case errors.Is(err, tradingpolicy.ErrInvalid):
		respond.Error(c, 400, "invalid_country_policy", "complete the country scope, effective dates, reviewed sources and every requirement")
	case errors.Is(err, tradingpolicy.ErrConflict):
		respond.Error(c, 409, "country_policy_conflict", "refresh the policy revision; use the original payload when retrying a request")
	case errors.Is(err, tradingpolicy.ErrIndependentReview):
		respond.Error(c, 403, "independent_review_required", "the policy author or submitter cannot approve or reject their own version")
	default:
		respond.InternalErr(c, errors.New("country policy persistence unavailable"), "could not access country policy")
	}
}

func (h handler) changeCountryPolicyAccess(c *gin.Context) {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	var in tradingpolicy.AccessChange
	if !decodePolicy(c, &in) {
		return
	}
	var receipt tradingpolicy.Receipt
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		var err error
		receipt, err = tradingpolicy.ChangeAccess(tx, p.TenantID, p.Subject, c.Param("scope"), in, time.Now().UTC())
		return err
	})
	if err != nil {
		countryPolicyError(c, err)
		return
	}
	respond.Accepted(c, receipt)
}

func (h handler) reviewCountryPolicy(c *gin.Context) {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	var in tradingpolicy.Review
	if !decodePolicy(c, &in) {
		return
	}
	var receipt tradingpolicy.Receipt
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		var err error
		receipt, err = tradingpolicy.Decide(tx, p.TenantID, p.Subject, c.Param("scope"), in, time.Now().UTC())
		return err
	})
	if err != nil {
		countryPolicyError(c, err)
		return
	}
	respond.Accepted(c, receipt)
}

func (h handler) getCountryPolicyVersion(c *gin.Context) {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	for _, param := range []string{"scope", "version"} {
		id, err := uuid.Parse(c.Param(param))
		if err != nil || id.Version() != 7 {
			respond.NotFound(c, "country policy")
			return
		}
	}
	var version tradingpolicy.Version
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		var err error
		version, err = tradingpolicy.ReadVersion(tx, p.TenantID, p.Subject, c.Param("scope"), c.Param("version"))
		return err
	})
	if err != nil {
		countryPolicyError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	respond.OK(c, version)
}

func (h handler) submitCountryPolicy(c *gin.Context) {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	var in tradingpolicy.Submission
	if !decodePolicy(c, &in) {
		return
	}
	var receipt tradingpolicy.Receipt
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		var err error
		receipt, err = tradingpolicy.Submit(tx, p.TenantID, p.Subject, in, time.Now().UTC())
		return err
	})
	if err != nil {
		countryPolicyError(c, err)
		return
	}
	respond.Accepted(c, receipt)
}

func (h handler) getCountryPolicy(c *gin.Context) {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("scope"))
	if err != nil || id.Version() != 7 {
		respond.NotFound(c, "country policy")
		return
	}
	var scope tradingpolicy.PolicyScope
	err = h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		var err error
		scope, err = tradingpolicy.ReadScope(tx, p.TenantID, p.Subject, id.String())
		if err != nil {
			return err
		}
		decision, err := tradingpolicy.Check(tx, tradingpolicy.Scope{Tenant: p.TenantID, Country: scope.Country, Jurisdiction: scope.Jurisdiction, Category: scope.Category, Operation: scope.Operation, Currency: scope.Currency}, time.Now().UTC())
		if err != nil {
			return err
		}
		scope.PolicyDecision = &decision
		return nil
	})
	if err != nil {
		countryPolicyError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	respond.OK(c, scope)
}

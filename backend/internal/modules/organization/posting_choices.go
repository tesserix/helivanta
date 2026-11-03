package organization

import (
	"context"
	"errors"
	"net/url"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (h *handlers) listPostingDepartmentChoices(c *gin.Context, page pagination.Params) ([]departmentRecord, error) {
	c.Header("Cache-Control", "no-store")
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		respond.BadRequest(c, errors.New("invalid posting department query"))
		return nil, nil
	}
	for key, values := range query {
		if (key != "limit" && key != "cursor") || len(values) != 1 || values[0] == "" {
			respond.BadRequest(c, errors.New("provide only one limit and one optional cursor"))
			return nil, nil
		}
	}
	if page.Limit > 50 {
		respond.BadRequest(c, errors.New("posting department choices are limited to 50 per page"))
		return nil, nil
	}
	if !authn.Refresh(c) {
		return nil, nil
	}
	p, hospital, ok := departmentHospital(c)
	if !ok {
		return nil, nil
	}
	guard, supported := h.scopeAuthority.(authz.CompoundScopedCommandGuard)
	if !supported || h.staffActors == nil || p.IdleDeadline.IsZero() {
		respond.Error(c, 503, "scoped_authorization_unavailable", "Current posting access could not be verified.")
		return nil, nil
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), p.IdleDeadline)
	defer cancel()
	checks := make([]authz.ScopeCheck, 0, 2)
	for _, permission := range []authz.Permission{PermPostingChoicesRead, authz.StaffActorUse} {
		request := authz.ScopeRequest{TenantID: p.TenantID, Subject: p.Subject, HospitalID: hospital.String(), Permission: permission}
		grant, err := h.scopeAuthority.AuthorizeScope(ctx, request)
		if err != nil {
			respond.Error(c, 503, "scoped_authorization_unavailable", "Current posting access could not be verified.")
			return nil, nil
		}
		if grant.AssignmentID == "" {
			respond.Forbidden(c, "Posting department choices are not permitted.")
			return nil, nil
		}
		if admitted, exists := authz.HospitalScopeGrant(c, permission, hospital.String()); exists && admitted != grant {
			respond.Forbidden(c, "Posting access changed. Reopen the workspace.")
			return nil, nil
		}
		checks = append(checks, authz.ScopeCheck{Request: request, Grant: grant})
	}
	rows := []departmentRecord{}
	err = guard.WithScopeGrants(ctx, checks, p.AuthTime, func(commandCtx context.Context) error {
		return h.staffActors.WithCurrentStaffActor(commandCtx, authz.StaffActorScope{TenantID: p.TenantID, Subject: p.Subject, HospitalID: hospital}, func(actorCtx context.Context, _ authz.StaffActorReference) error {
			return h.db.WithTenantCommand(actorCtx, p.TenantID, func(tx *gorm.DB) error {
				if err := requireDepartmentHospital(tx, p.TenantID, hospital); err != nil {
					return err
				}
				if err := tenantdb.ApplyKeyset(tx, page).Where("tenant_id=? AND hospital_id=? AND status='active' AND effective_at<=clock_timestamp()", p.TenantID, hospital).Clauses(clause.Locking{Strength: "SHARE"}).Find(&rows).Error; err != nil {
					return err
				}
				return audit(tx, p.TenantID, p.Subject, "organization.posting_choices.read", "organization_departments", hospital)
			})
		})
	})
	if err != nil {
		switch {
		case errors.Is(err, authz.ErrScopedCredentialRevoked):
			respond.Unauthenticated(c, "credential revoked")
		case errors.Is(err, authz.ErrScopedCommandDenied):
			respond.Forbidden(c, "Posting department choices are not permitted.")
		case errors.Is(err, authz.ErrScopedAuthorizationUnavailable), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
			respond.Error(c, 503, "scoped_authorization_unavailable", "Current posting access could not be verified.")
		default:
			respondDepartmentError(c, err)
		}
		return nil, nil
	}
	return rows, nil
}

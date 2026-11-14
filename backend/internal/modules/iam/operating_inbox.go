package iam

import (
	"context"
	"errors"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"gorm.io/gorm"
)

func registerOperatingDutyInbox(r *platform.Router, h *operatingHandlers) {
	platform.SealedFilteredQueryListRoute(r, "/operating-governance-employers/:kind/:employer/duty-inbox", authz.Permission(authz.GovernanceOperatingManage), "employer", []platform.QueryFilter{{Key: "status", Default: "all"}}, 20, func(c *gin.Context, page pagination.Params) ([]OperatingDuty, error) {
		native, ok := h.nativeHandler(c)
		if !ok {
			return nil, nil
		}
		return native.listOperatingDutyInbox(c, page)
	}, func(_ *gin.Context, rows []OperatingDuty) ([]OperatingDuty, error) { return rows, nil }, "kind")
}

func operatingInboxQuery(c *gin.Context) bool {
	values, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil || c.Request.URL.ForceQuery {
		return false
	}
	for key, entries := range values {
		if (key != "limit" && key != "cursor" && key != "status") || len(entries) != 1 || entries[0] == "" || key == "status" && entries[0] != "all" {
			return false
		}
	}
	return true
}

func (h *operatingHandlers) listOperatingDutyInbox(c *gin.Context, page pagination.Params) ([]OperatingDuty, error) {
	if h.native == nil {
		respond.Forbidden(c, "Current employer duty management is required.")
		return nil, nil
	}
	p, _, ok := h.scope(c, PermOperatingRead)
	if !ok {
		return nil, nil
	}
	if !operatingInboxQuery(c) || page.Limit < 1 || page.Limit > 20 {
		respond.Error(c, 400, "invalid_request", "Use one bounded existing-duty page without target filters.")
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	proof, err := h.actorProof(ctx, p, PermOperatingRead)
	if err != nil {
		operatingInboxError(c, err)
		return nil, nil
	}
	result := []OperatingDuty{}
	err = h.withAdministration(ctx, p, proof, "read", func(commandCtx context.Context, tx *gorm.DB) error {
		employer := h.native.scope
		return h.employerAccess(commandCtx, p.TenantID, OperatingDutyScope{EmployerKind: employer.EmployerKind, EmployerID: employer.EmployerID}, "reader", func(context.Context) error {
			var rows []operatingDutyRow
			query := tx.Table("iam_operating_duties").Select("tenant_id,id,input,prepared_by,status,membership_epoch,version,authority_epoch,created_at,updated_at,native_administration").Where("tenant_id=? AND employer_kind=? AND employer_id=? AND native_administration=true", p.TenantID, employer.EmployerKind, employer.EmployerID).Order("created_at DESC,id DESC").Limit(page.Limit + 1)
			if page.After != nil {
				query = query.Where("(created_at,id)<(?,?)", page.After.CreatedAt, page.After.ID)
			}
			if err := query.Find(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				view, err := operatingDutyView(row, false)
				if err != nil {
					return err
				}
				result = append(result, view)
			}
			return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'iam.operating.employer_inbox.read','iam_operating_employers',?)`, p.TenantID, p.Subject, employer.EmployerID).Error
		})
	})
	if err != nil {
		operatingInboxError(c, err)
		return nil, nil
	}
	return result, nil
}

func operatingInboxError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, authz.ErrScopedCredentialRevoked):
		respond.Error(c, 401, "credentials_revoked", "Your credentials changed. Sign in again.")
	case errors.Is(err, authz.ErrGovernanceScopeHidden), errors.Is(err, authz.ErrScopedCommandDenied), errors.Is(err, gorm.ErrRecordNotFound):
		respond.NotFound(c, "Existing duties are not available for this employer.")
	case errors.Is(err, errAssignmentDenied):
		respond.Forbidden(c, "Current employer management access is required.")
	default:
		respond.Error(c, 503, "authorization_unavailable", "Existing duty authority, records or read audit are unavailable.")
	}
}

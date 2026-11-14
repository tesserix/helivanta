package iam

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

// capabilities supplies UI hints; commands always reauthorize their own complete admission.
func (h *partnerMemberHandlers) capabilities(c *gin.Context) {
	p, r, ok := partnerMemberPrincipal(c)
	if !ok {
		return
	}
	if h.authority == nil {
		partnerMemberError(c, authz.ErrScopedAuthorizationUnavailable)
		return
	}
	ctx, cancel := membershipCommandContext(c, p)
	defer cancel()
	result := PartnerMemberCapabilities{HospitalID: r.hospital}
	for _, permission := range []authz.Permission{PermPartnerGroupMemberPrepare, PermPartnerGroupMemberReview} {
		grant, err := h.authority.AuthorizeScope(ctx, authz.ScopeRequest{TenantID: p.TenantID, Subject: p.Subject, HospitalID: r.hospital.String(), Permission: permission})
		if err != nil {
			partnerMemberError(c, authz.ErrScopedAuthorizationUnavailable)
			return
		}
		allowed := grant.AssignmentID != "" && grant.ScopeKind == "hospital"
		if permission == PermPartnerGroupMemberPrepare {
			result.CanPrepare = allowed
		} else {
			result.CanReview = allowed
		}
	}
	err := h.transaction(c, p, r, PermPartnerGroupMemberRead, func(_ context.Context, tx *gorm.DB, _ authz.StaffActorReference) error {
		return partnerGroupReceiptAudit(tx, p, r.hospital, "partner_member_capabilities_read")
	})
	if err != nil {
		partnerMemberError(c, err)
		return
	}
	respond.OK(c, result)
}

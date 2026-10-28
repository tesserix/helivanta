package workforce

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r leaveReviewRequest) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

func (h *handlers) withLeaveReviewRead(c *gin.Context, p authn.Principal, hospital uuid.UUID, kind string, staffID, requestID uuid.UUID, read func(*gorm.DB, reviewedLeaveActor, authz.ScopeGrant) error) error {
	grant, err := authz.RecheckHospitalScope(c, PermLeaveApprove, hospital.String())
	if err != nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if grant.AssignmentID == "" {
		return authz.ErrScopedCommandDenied
	}
	return authz.WithHospitalScopeCommand(c, PermLeaveApprove, hospital.String(), func(ctx context.Context) error {
		return h.transaction(ctx, p, hospital, func(tx *gorm.DB) error {
			actor, e := lockReviewedLeaveActor(tx, p, hospital)
			if e != nil {
				return e
			}
			if e := tx.Exec(`SELECT set_config('app.workforce_leave_review_read','authorized',true),set_config('app.workforce_leave_review_read_kind',?,true),set_config('app.workforce_leave_review_read_staff',?,true),set_config('app.workforce_leave_review_read_request',?,true)`, kind, staffID.String(), requestID.String()).Error; e != nil {
				return e
			}
			return read(tx, actor, grant)
		})
	})
}

func leaveReviewReadAudit(tx *gorm.DB, p authn.Principal, hospital, request uuid.UUID, kind string, actor reviewedLeaveActor, grant authz.ScopeGrant) error {
	return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,jsonb_build_object('assignment_id',?::text,'assignment_version',?::bigint,'scope_epoch',?::bigint,'membership_epoch',?::bigint,'actor_link_id',?::text,'actor_link_version',?::bigint))`, p.TenantID, hospital, p.Subject, "workforce.leave_review."+kind, request, grant.AssignmentID, grant.Version, grant.ScopeEpoch, grant.MembershipEpoch, actor.ID.String(), actor.Version).Error
}

func (h *handlers) listLeaveReviewRequests(c *gin.Context, page pagination.Params) ([]leaveReviewRequest, error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	if page.Limit > 100 {
		respond.Error(c, 400, "invalid_request", "Use a page limit no greater than 100.")
		return nil, nil
	}
	rows := []leaveReviewRequest{}
	err := h.withLeaveReviewRead(c, p, hospital, "queue", uuid.Nil, uuid.Nil, func(tx *gorm.DB, actor reviewedLeaveActor, grant authz.ScopeGrant) error {
		if e := tenantdb.ApplyKeyset(tx, page).Table(leaveRequestView).Select("id,staff_id,employee_code,display_name,code,name,leave_year,unit,start_on,end_on,session,minutes,half_days,status,version,created_at").Where("tenant_id=? AND hospital_id=? AND status='pending' AND staff_id<>? AND requested_by_subject<>?", p.TenantID, hospital, actor.StaffID, p.Subject).Find(&rows).Error; e != nil {
			return e
		}
		for _, row := range rows[:min(len(rows), page.Limit)] {
			if e := leaveReviewReadAudit(tx, p, hospital, row.ID, "queue_read", actor, grant); e != nil {
				return e
			}
		}
		return nil
	})
	if err != nil {
		respondLeaveRequestError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) getLeaveReviewRequest(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staffID, requestID, ok := leaveRequestIDs(c)
	if !ok {
		return
	}
	var result leaveRequestDetail
	err := h.withLeaveReviewRead(c, p, hospital, "detail", staffID, requestID, func(tx *gorm.DB, actor reviewedLeaveActor, grant authz.ScopeGrant) error {
		var request struct{ ID uuid.UUID }
		if e := tx.Table("workforce_leave_requests").Select("id").Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND id=? AND staff_id=? AND status='pending' AND staff_id<>? AND requested_by_subject<>?", p.TenantID, hospital, requestID, staffID, actor.StaffID, p.Subject).Take(&request).Error; e != nil {
			return e
		}
		var e error
		result, e = loadLeaveRequestDetail(tx, p.TenantID, hospital, staffID, requestID)
		if e != nil {
			return e
		}
		return leaveReviewReadAudit(tx, p, hospital, requestID, "detail_read", actor, grant)
	})
	if err != nil {
		respondLeaveRequestError(c, err)
		return
	}
	respond.OK(c, result)
}

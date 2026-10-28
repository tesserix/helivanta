package workforce

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

type leaveReviewScopeRow struct {
	Scope     leaveReviewScope `gorm:"embedded" json:"-"`
	ID        uuid.UUID        `json:"-"`
	StaffID   uuid.UUID        `json:"-"`
	Version   int64            `json:"-"`
	CreatedAt time.Time        `json:"-"`
}

func (s leaveReviewScopeRow) MarshalJSON() ([]byte, error) { return json.Marshal(s.Scope) }

func (s leaveReviewScopeRow) PageKey() (time.Time, uuid.UUID) { return s.CreatedAt, s.ID }

func (h *handlers) leaveReviewScopeCandidates(c *gin.Context, page pagination.Params) ([]leaveReviewScopeRow, error) {
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	var rows []leaveReviewScopeRow
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject',?,true),set_config('app.workforce_leave_discovery','authorized',true)`, p.Subject).Error; err != nil {
			return err
		}
		query := tx.Table("workforce_login_links l").Select("l.id,l.staff_id,l.version,l.created_at,l.hospital_id,h.name").
			Joins("JOIN workforce_hospitals h ON h.tenant_id=l.tenant_id AND h.id=l.hospital_id").
			Where("l.tenant_id=? AND l.subject=? AND l.status='active'", p.TenantID, p.Subject)
		if page.After != nil {
			query = query.Where("(l.created_at,l.id)<(?,?)", page.After.CreatedAt, page.After.ID)
		}
		return query.Order("l.created_at DESC,l.id DESC").Limit(page.Limit + 1).Find(&rows).Error
	})
	return rows, err
}

func (h *handlers) authorizedLeaveReviewScopes(c *gin.Context, candidates []leaveReviewScopeRow) ([]leaveReviewScopeRow, error) {
	p, ok := authn.PrincipalFrom(c)
	if !ok {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	guard, ok := h.scopeAuthority.(authz.ScopedCommandGuard)
	if !ok {
		respondLeaveRequestError(c, authz.ErrScopedAuthorizationUnavailable)
		return nil, nil
	}
	allowed := make([]bool, len(candidates))
	group, ctx := errgroup.WithContext(c.Request.Context())
	group.SetLimit(4)
	for i, candidate := range candidates {
		group.Go(func() error {
			request := authz.ScopeRequest{TenantID: p.TenantID, Subject: p.Subject, HospitalID: candidate.Scope.HospitalID.String(), Permission: PermLeaveApprove}
			grant, err := h.scopeAuthority.AuthorizeScope(ctx, request)
			if err != nil {
				return authz.ErrScopedAuthorizationUnavailable
			}
			if grant.AssignmentID == "" {
				return nil
			}
			err = guard.WithScopeGrant(ctx, request, grant, p.AuthTime, func(commandCtx context.Context) error {
				return h.transaction(commandCtx, p, candidate.Scope.HospitalID, func(tx *gorm.DB) error {
					actor, e := lockReviewedLeaveActor(tx, p, candidate.Scope.HospitalID)
					if errors.Is(e, gorm.ErrRecordNotFound) {
						return nil
					}
					if e != nil {
						return e
					}
					if actor.ID != candidate.ID || actor.StaffID != candidate.StaffID || actor.Version != candidate.Version {
						return nil
					}
					if e := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.leave_review_scope.read',?,jsonb_build_object('assignment_id',?::text,'assignment_version',?::bigint,'actor_link_version',?::bigint))`, p.TenantID, candidate.Scope.HospitalID, p.Subject, candidate.Scope.HospitalID, grant.AssignmentID, grant.Version, actor.Version).Error; e != nil {
						return e
					}
					allowed[i] = true
					return nil
				})
			})
			if errors.Is(err, authz.ErrScopedCommandDenied) {
				allowed[i] = false
				return nil
			}
			return err
		})
	}
	if err := group.Wait(); err != nil {
		respondLeaveRequestError(c, err)
		return nil, nil
	}
	rows := make([]leaveReviewScopeRow, 0, len(candidates))
	for i, row := range candidates {
		if allowed[i] {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

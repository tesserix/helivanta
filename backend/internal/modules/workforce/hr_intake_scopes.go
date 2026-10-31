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

type hrIntakeScopeRow struct {
	Scope     hrIntakeScope       `json:"-"`
	Candidate leaveReviewScopeRow `json:"-"`
}

func (s hrIntakeScopeRow) MarshalJSON() ([]byte, error)    { return json.Marshal(s.Scope) }
func (s hrIntakeScopeRow) PageKey() (time.Time, uuid.UUID) { return s.Candidate.PageKey() }

func (h *handlers) hrIntakeScopeCandidates(c *gin.Context, page pagination.Params) ([]hrIntakeScopeRow, error) {
	_, _, ok := staffPrincipal(c)
	if !ok {
		return nil, nil
	}
	if len(c.Request.Header.Values("X-Helivanta-Expected-Tenant")) != 1 || len(c.Request.Header.Values("X-Helivanta-Expected-Subject")) != 1 {
		respondIntakeError(c, errIntakeWorkspaceRequired)
		return nil, nil
	}
	if !authn.Refresh(c) {
		return nil, nil
	}
	candidates, err := h.leaveReviewScopeCandidates(c, page)
	if err != nil {
		return nil, err
	}
	rows := make([]hrIntakeScopeRow, 0, len(candidates))
	for _, candidate := range candidates {
		rows = append(rows, hrIntakeScopeRow{Scope: hrIntakeScope{HospitalID: candidate.Scope.HospitalID, Name: candidate.Scope.Name}, Candidate: candidate})
	}
	return rows, nil
}

func (h *handlers) authorizedHRIntakeScopes(c *gin.Context, candidates []hrIntakeScopeRow) ([]hrIntakeScopeRow, error) {
	p, ok := authn.PrincipalFrom(c)
	guard, supported := h.scopeAuthority.(authz.CompoundScopedCommandGuard)
	if !ok || !supported || p.IdleDeadline.IsZero() {
		respondIntakeError(c, authz.ErrScopedAuthorizationUnavailable)
		return nil, nil
	}
	if !authn.Refresh(c) {
		return nil, nil
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), p.IdleDeadline)
	defer cancel()
	ctx, budget := context.WithTimeout(ctx, 5*time.Second)
	defer budget()
	allowed := make([]bool, len(candidates))
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(4)
	for i, candidate := range candidates {
		group.Go(func() error {
			hospital := candidate.Scope.HospitalID
			checks := make([]authz.ScopeCheck, 0, 2)
			for _, permission := range []authz.Permission{PermEnrollmentManage, PermDocumentManage} {
				request := authz.ScopeRequest{TenantID: p.TenantID, Subject: p.Subject, HospitalID: hospital.String(), Permission: permission}
				grant, err := h.scopeAuthority.AuthorizeScope(ctx, request)
				if err != nil {
					return authz.ErrScopedAuthorizationUnavailable
				}
				if grant.AssignmentID == "" {
					return nil
				}
				checks = append(checks, authz.ScopeCheck{Request: request, Grant: grant})
			}
			err := guard.WithScopeGrants(ctx, checks, p.AuthTime, func(commandCtx context.Context) error {
				return h.transaction(commandCtx, p, hospital, func(tx *gorm.DB) error {
					if err := tx.Exec(`SELECT workforce_lock_supervision(?::uuid,?::uuid)`, p.TenantID, hospital).Error; err != nil {
						return err
					}
					actor, err := lockReviewedLeaveActor(tx, p, hospital)
					if errors.Is(err, gorm.ErrRecordNotFound) {
						return nil
					}
					if err != nil {
						return err
					}
					original := candidate.Candidate
					if actor.ID != original.ID || actor.StaffID != original.StaffID || actor.Version != original.Version {
						return nil
					}
					if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.hr_intake_scope.read',?,jsonb_build_object('actor_link_version',?::bigint))`, p.TenantID, hospital, p.Subject, hospital, actor.Version).Error; err != nil {
						return err
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
		respondIntakeError(c, err)
		return nil, nil
	}
	rows := make([]hrIntakeScopeRow, 0, len(candidates))
	for i, row := range candidates {
		if allowed[i] {
			row.Scope.CanPrepareStaff = true
			row.Scope.CanManageStaffDocuments = true
			rows = append(rows, row)
		}
	}
	return rows, nil
}

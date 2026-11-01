package workforce

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

type employmentReviewScopeRow struct {
	Scope     EmploymentReviewHospitalScope
	Candidate leaveReviewScopeRow
}

func (s employmentReviewScopeRow) MarshalJSON() ([]byte, error) { return json.Marshal(s.Scope) }
func (s employmentReviewScopeRow) PageKey() (time.Time, uuid.UUID) {
	return s.Candidate.PageKey()
}

func (h *handlers) employmentReviewScopeCandidates(c *gin.Context, page pagination.Params) ([]employmentReviewScopeRow, error) {
	if _, _, ok := staffPrincipal(c); !ok {
		return nil, nil
	}
	if len(c.Request.Header.Values("X-Helivanta-Expected-Tenant")) != 1 || len(c.Request.Header.Values("X-Helivanta-Expected-Subject")) != 1 {
		respond.Error(c, 409, "session_changed", "Refresh your current workspace before opening employment reviews.")
		return nil, nil
	}
	if !authn.Refresh(c) {
		return nil, nil
	}
	candidates, err := h.leaveReviewScopeCandidates(c, page)
	if err != nil {
		return nil, err
	}
	rows := make([]employmentReviewScopeRow, 0, len(candidates))
	for _, candidate := range candidates {
		rows = append(rows, employmentReviewScopeRow{Scope: EmploymentReviewHospitalScope{HospitalID: candidate.Scope.HospitalID, Name: candidate.Scope.Name}, Candidate: candidate})
	}
	return rows, nil
}

func (h *handlers) authorizedEmploymentReviewScopes(c *gin.Context, candidates []employmentReviewScopeRow) ([]employmentReviewScopeRow, error) {
	p, ok := authn.PrincipalFrom(c)
	guard, supported := h.scopeAuthority.(authz.CompoundScopedCommandGuard)
	if !ok || !supported || p.IdleDeadline.IsZero() {
		employmentReviewError(c, authz.ErrScopedAuthorizationUnavailable)
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
	for i := range candidates {
		group.Go(func() error {
			row := &candidates[i]
			hospital := row.Scope.HospitalID
			preparer := make([]authz.ScopeCheck, 0, 2)
			reviewer := make([]authz.ScopeCheck, 0, 1)
			for _, permission := range []authz.Permission{PermEnrollmentManage, PermDocumentManage, PermEmploymentReview} {
				request := authz.ScopeRequest{TenantID: p.TenantID, Subject: p.Subject, HospitalID: hospital.String(), Permission: permission}
				grant, err := h.scopeAuthority.AuthorizeScope(ctx, request)
				if err != nil {
					return authz.ErrScopedAuthorizationUnavailable
				}
				if grant.AssignmentID == "" {
					continue
				}
				check := authz.ScopeCheck{Request: request, Grant: grant}
				if permission == PermEmploymentReview {
					reviewer = append(reviewer, check)
				} else {
					preparer = append(preparer, check)
				}
			}
			for _, capability := range []struct {
				checks []authz.ScopeCheck
				count  int
				submit bool
			}{{preparer, 2, true}, {reviewer, 1, false}} {
				if len(capability.checks) != capability.count {
					continue
				}
				verified := false
				err := guard.WithScopeGrants(ctx, capability.checks, p.AuthTime, func(commandCtx context.Context) error {
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
						original := row.Candidate
						if actor.ID != original.ID || actor.StaffID != original.StaffID || actor.Version != original.Version {
							return nil
						}
						if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.employment_review_scope.read',?,jsonb_build_object('actor_link_version',?::bigint,'can_submit',?::boolean,'can_review',?::boolean))`, p.TenantID, hospital, p.Subject, hospital, actor.Version, capability.submit, !capability.submit).Error; err != nil {
							return err
						}
						verified = true
						return nil
					})
				})
				if errors.Is(err, authz.ErrScopedCommandDenied) {
					continue
				}
				if err != nil {
					return err
				}
				if capability.submit {
					row.Scope.CanSubmit = verified
				} else {
					row.Scope.CanReview = verified
				}
			}
			allowed[i] = row.Scope.CanSubmit || row.Scope.CanReview
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		employmentReviewError(c, err)
		return nil, nil
	}
	rows := make([]employmentReviewScopeRow, 0, len(candidates))
	for i, row := range candidates {
		if allowed[i] {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

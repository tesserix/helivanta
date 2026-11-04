package workforce

import (
	"context"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

const scopedPostingKey = "workforce.scoped_posting"

func markScopedPosting(c *gin.Context) { c.Set(scopedPostingKey, true) }

func (h *handlers) postingTransaction(c *gin.Context, p authn.Principal, scope postingDraftScope, writing bool, action func(context.Context, *gorm.DB, EmploymentPeriodSource) error) error {
	if !c.GetBool(scopedPostingKey) {
		var source EmploymentPeriodSource
		verify := func(tx *gorm.DB) error {
			var err error
			source, err = postingOwnerContext(tx, p, scope, writing, false)
			return err
		}
		if writing {
			return h.db.WithGuardedTenantCommand(c.Request.Context(), p.TenantID, time.Now().Add(5*time.Second), verify, func(ctx context.Context) error {
				return h.db.WithTenantCommand(ctx, p.TenantID, func(tx *gorm.DB) error { return action(ctx, tx, source) })
			})
		}
		return h.db.WithTenantCommand(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			if err := verify(tx); err != nil {
				return err
			}
			return action(c.Request.Context(), tx, source)
		})
	}
	permission := PermPostingRead
	if writing {
		permission = PermPostingPrepare
	}
	return h.scopedPostingTransaction(c, p, scope, writing, permission, action)
}

func (h *handlers) scopedPostingTransaction(c *gin.Context, p authn.Principal, scope postingDraftScope, writing bool, permission authz.Permission, action func(context.Context, *gorm.DB, EmploymentPeriodSource) error, additional ...authz.Permission) error {
	if !c.GetBool(scopedPostingKey) {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if !authn.Refresh(c) {
		return authz.ErrScopedAuthorizationUnavailable
	}
	current, ok := authn.PrincipalFrom(c)
	if !ok || current.TenantID != p.TenantID || current.Subject != p.Subject {
		return authz.ErrScopedCommandDenied
	}
	p = current
	guard, supported := h.scopeAuthority.(authz.CompoundScopedCommandGuard)
	if !supported || h.staffActors == nil || p.IdleDeadline.IsZero() {
		return authz.ErrScopedAuthorizationUnavailable
	}
	mode := "read"
	if writing {
		mode = "write"
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), p.IdleDeadline)
	defer cancel()
	checks := make([]authz.ScopeCheck, 0, 2+len(additional))
	requiredPermissions := append([]authz.Permission{permission, authz.StaffActorUse}, additional...)
	for _, required := range requiredPermissions {
		request := authz.ScopeRequest{TenantID: p.TenantID, Subject: p.Subject, HospitalID: scope.hospital.String(), Permission: required}
		grant, err := h.scopeAuthority.AuthorizeScope(ctx, request)
		if err != nil {
			return authz.ErrScopedAuthorizationUnavailable
		}
		if grant.AssignmentID == "" {
			return authz.ErrScopedCommandDenied
		}
		if admitted, exists := authz.HospitalScopeGrant(c, required, scope.hospital.String()); exists && admitted != grant {
			return authz.ErrScopedCommandDenied
		}
		checks = append(checks, authz.ScopeCheck{Request: request, Grant: grant})
	}
	return guard.WithScopeGrants(ctx, checks, p.AuthTime, func(commandCtx context.Context) error {
		if writing {
			// Acquiring the write gate before actor rows avoids a shared-to-exclusive lock upgrade.
			if err := h.db.WithTenantCommand(commandCtx, p.TenantID, func(tx *gorm.DB) error {
				return tx.Exec(`SELECT workforce_lock_supervision(?::uuid,?::uuid)`, p.TenantID, scope.hospital).Error
			}); err != nil {
				return err
			}
		}
		return h.staffActors.WithCurrentStaffActor(commandCtx, authz.StaffActorScope{TenantID: p.TenantID, Subject: p.Subject, HospitalID: scope.hospital}, func(actorCtx context.Context, actor authz.StaffActorReference) error {
			return h.db.WithTenantCommand(actorCtx, p.TenantID, func(tx *gorm.DB) error {
				grant := checks[0].Grant
				var management authz.ScopeGrant
				for _, check := range checks {
					if check.Request.Permission == PermPostingManagementReview {
						management = check.Grant
					}
				}
				if err := tx.Exec(`SELECT set_config('app.workforce_posting_management_assignment',?,true),set_config('app.workforce_posting_management_assignment_version',?,true),set_config('app.workforce_posting_management_scope_epoch',?,true),set_config('app.workforce_posting_management_membership_epoch',?,true),set_config('app.workforce_posting_root_approval','',true)`, management.AssignmentID, strconv.FormatInt(management.Version, 10), strconv.FormatInt(management.ScopeEpoch, 10), strconv.FormatInt(management.MembershipEpoch, 10)).Error; err != nil {
					return err
				}

				if err := tx.Exec(`SELECT set_config('app.workforce_posting_permission',?,true),set_config('app.workforce_posting_scoped',?,true),set_config('app.workforce_posting_actor',?,true),set_config('app.workforce_posting_link',?,true),set_config('app.workforce_posting_generation',?,true),set_config('app.workforce_posting_assignment',?,true),set_config('app.workforce_posting_assignment_version',?,true),set_config('app.workforce_posting_scope_epoch',?,true),set_config('app.workforce_posting_membership_epoch',?,true)`, string(permission), mode, actor.StaffID.String(), actor.LinkID.String(), actor.GenerationID.String(), grant.AssignmentID, strconv.FormatInt(grant.Version, 10), strconv.FormatInt(grant.ScopeEpoch, 10), strconv.FormatInt(grant.MembershipEpoch, 10)).Error; err != nil {
					return err
				}
				source, err := postingOwnerContext(tx, p, scope, writing, true)
				if err != nil {
					return err
				}
				return action(actorCtx, tx, source)
			})
		})
	})
}

package workforce

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
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
	path, pathErr := h.postingDepartmentPath(ctx, p, scope)
	if pathErr != nil {
		// A hint naming no current department fails closed even for a hospital-wide grant.
		return h.deniedPostingDepartment(ctx, scope, authz.ScopeRequest{}, pathErr)
	}
	checks := make([]authz.ScopeCheck, 0, 2+len(additional))
	requiredPermissions := append([]authz.Permission{permission, authz.StaffActorUse}, additional...)
	var departments []uuid.UUID
	for index, required := range requiredPermissions {
		request := authz.ScopeRequest{TenantID: p.TenantID, Subject: p.Subject, HospitalID: scope.hospital.String(), Permission: required, DepartmentPath: path}
		grant, err := h.scopeAuthority.AuthorizeScope(ctx, request)
		if err != nil {
			return authz.ErrScopedAuthorizationUnavailable
		}
		if grant.AssignmentID == "" {
			if index == 0 {
				return h.deniedPostingDepartment(ctx, scope, request, nil)
			}
			return authz.ErrScopedCommandDenied
		}
		if admitted, exists := authz.HospitalScopeGrant(c, required, scope.hospital.String()); exists && admitted != grant {
			return authz.ErrScopedCommandDenied
		}
		if grant.ScopeKind == "department" {
			department, err := uuid.Parse(grant.DepartmentID)
			if err != nil || !slices.Contains(path, grant.DepartmentID) {
				return authz.ErrScopedAuthorizationUnavailable
			}
			if !slices.Contains(departments, department) {
				departments = append(departments, department)
			}
		}
		checks = append(checks, authz.ScopeCheck{Request: request, Grant: grant})
	}
	kind, target, held, reviewMode := "hospital", "", "", ""
	if len(departments) > 0 {
		// Department Staff visibility follows the bound proposal, so the owner read must already see proposal revisions.
		kind, target, held, reviewMode = "department", scope.department.String(), departments[0].String(), "read"
	}
	return guard.WithScopeGrants(ctx, checks, p.AuthTime, func(commandCtx context.Context) error {
		if writing || slices.Contains(additional, authz.StaffPostingSourceUse) {
			// Live source reads and writes take the hospital gate before department/actor locks.
			if err := h.db.WithTenantCommand(commandCtx, p.TenantID, func(tx *gorm.DB) error {
				return tx.Exec(`SELECT workforce_lock_supervision(?::uuid,?::uuid)`, p.TenantID, scope.hospital).Error
			}); err != nil {
				return err
			}
		}
		return h.withinPostingDepartments(commandCtx, p.TenantID, scope, departments, func(heldCtx context.Context) error {
			return h.staffActors.WithCurrentStaffActor(heldCtx, authz.StaffActorScope{TenantID: p.TenantID, Subject: p.Subject, HospitalID: scope.hospital, DepartmentPath: path}, func(actorCtx context.Context, actor authz.StaffActorReference) error {
				return h.db.WithTenantCommand(actorCtx, p.TenantID, func(tx *gorm.DB) error {
					grant := checks[0].Grant
					var management authz.ScopeGrant
					for _, check := range checks {
						if check.Request.Permission == PermPostingManagementReview {
							management = check.Grant
						}
					}
					if err := tx.Exec(`SELECT set_config('app.workforce_posting_management_assignment',?,true),set_config('app.workforce_posting_management_assignment_version',?,true),set_config('app.workforce_posting_management_scope_epoch',?,true),set_config('app.workforce_posting_management_membership_epoch',?,true),set_config('app.workforce_posting_lifecycle_approval','',true),set_config('app.workforce_posting_root_approval','',true),set_config('app.workforce_posting_chain_approval','',true),set_config('app.workforce_posting_chain_until','',true)`, management.AssignmentID, strconv.FormatInt(management.Version, 10), strconv.FormatInt(management.ScopeEpoch, 10), strconv.FormatInt(management.MembershipEpoch, 10)).Error; err != nil {
						return err
					}
					if err := tx.Exec(`SELECT set_config('app.workforce_posting_scope_kind',?,true),set_config('app.workforce_posting_department_target',?,true),set_config('app.workforce_posting_department_scope',?,true),set_config('app.workforce_posting_department_subtree','',true),set_config('app.workforce_posting_review_mode',?,true)`, kind, target, held, reviewMode).Error; err != nil {
						return err
					}
					if err := tx.Exec(`SELECT set_config('app.workforce_posting_permission',?,true),set_config('app.workforce_posting_scoped',?,true),set_config('app.workforce_posting_actor',?,true),set_config('app.workforce_posting_link',?,true),set_config('app.workforce_posting_generation',?,true),set_config('app.workforce_posting_assignment',?,true),set_config('app.workforce_posting_assignment_version',?,true),set_config('app.workforce_posting_scope_epoch',?,true),set_config('app.workforce_posting_membership_epoch',?,true)`, string(permission), mode, actor.StaffID.String(), actor.LinkID.String(), actor.GenerationID.String(), grant.AssignmentID, strconv.FormatInt(grant.Version, 10), strconv.FormatInt(grant.ScopeEpoch, 10), strconv.FormatInt(grant.MembershipEpoch, 10)).Error; err != nil {
						return err
					}
					source, err := postingOwnerContext(tx, p, scope, writing, true)
					if err != nil {
						return err
					}
					if err := action(actorCtx, tx, source); err != nil {
						return err
					}
					// Only a fully guarded success may release a handler-scoped route's response.
					authz.AdmitHandlerScope(actorCtx, c, checks[0].Request)
					return nil
				})
			})
		})
	})
}

// errDepartmentScopeChanged means the target's path moved between the hint read and the lock; the client re-reads.
var errDepartmentScopeChanged = errors.New("posting department scope changed")

// postingDepartmentPath selects IAM candidates only; the locked path and RLS decide.
func (h *handlers) postingDepartmentPath(ctx context.Context, p authn.Principal, scope postingDraftScope) ([]string, error) {
	if scope.department == uuid.Nil {
		return nil, nil
	}
	if h.departmentScopes == nil {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	ids, err := h.departmentScopes.CurrentPath(ctx, p.TenantID, scope.hospital, scope.department)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 || len(ids) > authz.MaxDepartmentPath || ids[0] != scope.department {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	path := make([]string, len(ids))
	for i, id := range ids {
		path[i] = id.String()
	}
	return path, nil
}

// deniedPostingDepartment returns 404 outside every held subtree and 403 when review read is held there (ADR-0055).
func (h *handlers) deniedPostingDepartment(ctx context.Context, scope postingDraftScope, request authz.ScopeRequest, pathErr error) error {
	if scope.department == uuid.Nil {
		return authz.ErrScopedCommandDenied
	}
	if pathErr != nil {
		if errors.Is(pathErr, authz.ErrDepartmentReferenceHidden) || errors.Is(pathErr, authz.ErrDepartmentReferenceChanged) {
			return authz.ErrDepartmentReferenceHidden
		}
		return pathErr
	}
	if request.Permission == PermPostingProposalRead {
		return authz.ErrDepartmentReferenceHidden
	}
	request.Permission = PermPostingProposalRead
	grant, err := h.scopeAuthority.AuthorizeScope(ctx, request)
	if err != nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if grant.AssignmentID == "" {
		return authz.ErrDepartmentReferenceHidden
	}
	return authz.ErrScopedCommandDenied
}

// withinPostingDepartments holds each department grant's locked path through the command commit.
func (h *handlers) withinPostingDepartments(ctx context.Context, tenant string, scope postingDraftScope, departments []uuid.UUID, action func(context.Context) error) error {
	if len(departments) == 0 {
		return action(ctx)
	}
	if h.departmentScopes == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	entered := false
	err := h.departmentScopes.WithinCurrentDepartment(ctx, tenant, scope.hospital, scope.department, departments[0], func(held context.Context) error {
		entered = true
		return h.withinPostingDepartments(held, tenant, scope, departments[1:], action)
	})
	if !entered && errors.Is(err, authz.ErrDepartmentReferenceChanged) {
		return errors.Join(errDepartmentScopeChanged, err)
	}
	return err
}

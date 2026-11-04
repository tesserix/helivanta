package organization

import (
	"context"
	"errors"
	"net/url"
	"slices"

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

// errChoiceScopeChanged means the hinted path moved between the hint read and the lock; the client re-reads.
var errChoiceScopeChanged = errors.New("posting department choice scope changed")

func (h *handlers) listPostingDepartmentChoices(c *gin.Context, page pagination.Params) ([]departmentRecord, error) {
	c.Header("Cache-Control", "no-store")
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		respond.BadRequest(c, errors.New("invalid posting department query"))
		return nil, nil
	}
	for key, values := range query {
		if (key != "limit" && key != "cursor" && key != "department") || len(values) != 1 || values[0] == "" {
			respond.BadRequest(c, errors.New("provide only one limit, one optional cursor and one optional department"))
			return nil, nil
		}
	}
	hint := uuid.Nil
	if values, present := query["department"]; present {
		id, err := uuid.Parse(values[0])
		if err != nil || id.Version() != 7 || id.String() != values[0] {
			respond.BadRequest(c, errors.New("choose one department from the posting review scope"))
			return nil, nil
		}
		hint = id
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
	if !supported || h.staffActors == nil || h.db == nil || p.IdleDeadline.IsZero() {
		respondPostingChoicesError(c, authz.ErrScopedAuthorizationUnavailable)
		return nil, nil
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), p.IdleDeadline)
	defer cancel()
	scopes := &departmentScopeGuard{db: h.db}
	var path []string
	if hint != uuid.Nil {
		ids, err := scopes.CurrentPath(ctx, p.TenantID, hospital, hint)
		if errors.Is(err, authz.ErrDepartmentReferenceChanged) {
			err = authz.ErrDepartmentReferenceHidden
		}
		if err == nil && (len(ids) == 0 || len(ids) > authz.MaxDepartmentPath || ids[0] != hint) {
			err = authz.ErrScopedAuthorizationUnavailable
		}
		if err != nil {
			respondPostingChoicesError(c, err)
			return nil, nil
		}
		for _, id := range ids {
			path = append(path, id.String())
		}
	}
	checks := make([]authz.ScopeCheck, 0, 2)
	var departments []uuid.UUID
	for index, permission := range []authz.Permission{PermPostingChoicesRead, authz.StaffActorUse} {
		request := authz.ScopeRequest{TenantID: p.TenantID, Subject: p.Subject, HospitalID: hospital.String(), Permission: permission, DepartmentPath: path}
		grant, err := h.scopeAuthority.AuthorizeScope(ctx, request)
		if err != nil {
			respondPostingChoicesError(c, authz.ErrScopedAuthorizationUnavailable)
			return nil, nil
		}
		if grant.AssignmentID == "" {
			// Without choice authority on the hinted path the department is outside every held subtree.
			if index == 0 && hint != uuid.Nil {
				respondPostingChoicesError(c, authz.ErrDepartmentReferenceHidden)
				return nil, nil
			}
			respond.Forbidden(c, "Posting department choices are not permitted.")
			return nil, nil
		}
		if admitted, exists := authz.HospitalScopeGrant(c, permission, hospital.String()); exists && admitted != grant {
			respond.Forbidden(c, "Posting access changed. Reopen the workspace.")
			return nil, nil
		}
		if grant.ScopeKind == "department" {
			department, err := uuid.Parse(grant.DepartmentID)
			if err != nil || !slices.Contains(path, grant.DepartmentID) {
				respondPostingChoicesError(c, authz.ErrScopedAuthorizationUnavailable)
				return nil, nil
			}
			if !slices.Contains(departments, department) {
				departments = append(departments, department)
			}
		}
		checks = append(checks, authz.ScopeCheck{Request: request, Grant: grant})
	}
	rows := []departmentRecord{}
	err = guard.WithScopeGrants(ctx, checks, p.AuthTime, func(commandCtx context.Context) error {
		return withinChoiceDepartments(commandCtx, scopes, p.TenantID, hospital, hint, departments, func(heldCtx context.Context) error {
			return h.staffActors.WithCurrentStaffActor(heldCtx, authz.StaffActorScope{TenantID: p.TenantID, Subject: p.Subject, HospitalID: hospital, DepartmentPath: path}, func(actorCtx context.Context, _ authz.StaffActorReference) error {
				return h.db.WithTenantCommand(actorCtx, p.TenantID, func(tx *gorm.DB) error {
					if err := requireDepartmentHospital(tx, p.TenantID, hospital); err != nil {
						return err
					}
					q := tenantdb.ApplyKeyset(tx, page).Where("tenant_id=? AND hospital_id=? AND status='active' AND effective_at<=clock_timestamp()", p.TenantID, hospital)
					target := hospital
					if hint != uuid.Nil {
						subtree, err := scopes.CurrentSubtree(actorCtx, p.TenantID, hospital, []uuid.UUID{hint})
						if err != nil {
							return err
						}
						if !slices.Contains(subtree, hint) {
							return errChoiceScopeChanged
						}
						q, target = q.Where("id IN ?", subtree), hint
					}
					if err := q.Clauses(clause.Locking{Strength: "SHARE"}).Find(&rows).Error; err != nil {
						return err
					}
					if err := audit(tx, p.TenantID, p.Subject, "organization.posting_choices.read", "organization_departments", target); err != nil {
						return err
					}
					// Only a fully guarded read may release the handler-scoped response.
					if !authz.AdmitHandlerScope(actorCtx, c, checks[0].Request) {
						return authz.ErrScopedAuthorizationUnavailable
					}
					return nil
				})
			})
		})
	})
	if err != nil {
		respondPostingChoicesError(c, err)
		return nil, nil
	}
	return rows, nil
}

// withinChoiceDepartments holds each department grant's locked path to the hint through the read commit.
func withinChoiceDepartments(ctx context.Context, scopes *departmentScopeGuard, tenant string, hospital, hint uuid.UUID, departments []uuid.UUID, action func(context.Context) error) error {
	if len(departments) == 0 {
		return action(ctx)
	}
	entered := false
	err := scopes.WithinCurrentDepartment(ctx, tenant, hospital, hint, departments[0], func(held context.Context) error {
		entered = true
		return withinChoiceDepartments(held, scopes, tenant, hospital, hint, departments[1:], action)
	})
	if !entered && errors.Is(err, authz.ErrDepartmentReferenceChanged) {
		return errors.Join(errChoiceScopeChanged, err)
	}
	return err
}

func respondPostingChoicesError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, authz.ErrScopedCredentialRevoked):
		respond.Unauthenticated(c, "credential revoked")
	case errors.Is(err, authz.ErrScopedCommandDenied):
		respond.Forbidden(c, "Posting department choices are not permitted.")
	case errors.Is(err, errChoiceScopeChanged):
		respond.Error(c, 409, "department_scope_changed", "The department structure changed. Reload the posting review scope.")
	case errors.Is(err, authz.ErrDepartmentReferenceHidden), errors.Is(err, authz.ErrDepartmentReferenceChanged):
		respond.NotFound(c, "Posting department")
	case errors.Is(err, authz.ErrDepartmentSubtreeLimit), errors.Is(err, authz.ErrScopedAuthorizationUnavailable), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		respond.Error(c, 503, "scoped_authorization_unavailable", "Current posting access could not be verified.")
	default:
		respondDepartmentError(c, err)
	}
}

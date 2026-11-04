package workforce

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"gorm.io/gorm"
)

type postingReviewScopeRow struct {
	Scope     StaffPostingReviewHospitalScope
	Candidate leaveReviewScopeRow
}

func (r postingReviewScopeRow) MarshalJSON() ([]byte, error)    { return json.Marshal(r.Scope) }
func (r postingReviewScopeRow) PageKey() (time.Time, uuid.UUID) { return r.Candidate.PageKey() }

func (h *handlers) postingReviewScopeCandidates(c *gin.Context, page pagination.Params) ([]postingReviewScopeRow, error) {
	c.Header("Cache-Control", "no-store")
	if _, ok := postingAccount(c); !ok {
		return nil, nil
	}
	if _, ok := h.scopeAuthority.(authz.CompoundScopedCommandGuard); !ok || h.staffActors == nil {
		respondPostingProposalError(c, authz.ErrScopedAuthorizationUnavailable)
		return nil, nil
	}
	candidates, err := h.postingScopeCandidates(c, page)
	if err != nil {
		return nil, err
	}
	rows := make([]postingReviewScopeRow, 0, len(candidates))
	for _, candidate := range candidates {
		rows = append(rows, postingReviewScopeRow{Scope: StaffPostingReviewHospitalScope{HospitalID: candidate.Scope.HospitalID, Name: candidate.Scope.Name}, Candidate: candidate.Candidate})
	}
	return rows, nil
}

func (h *handlers) authorizedPostingReviewScopes(c *gin.Context, candidates []postingReviewScopeRow) ([]postingReviewScopeRow, error) {
	p, ok := postingAccount(c)
	if !ok {
		return nil, nil
	}
	markScopedPosting(c)
	rows := make([]postingReviewScopeRow, 0, len(candidates))
	for _, row := range candidates {
		scope := postingDraftScope{hospital: row.Scope.HospitalID, staff: row.Candidate.StaffID}
		row.Scope.Departments = []uuid.UUID{}
		permitted, err := h.postingReviewScopeAllows(c, p, scope, PermPostingProposalRead)
		if err == nil && !permitted {
			row.Scope.Departments, err = h.postingReviewDepartments(c, p, scope)
			permitted = len(row.Scope.Departments) > 0
		}
		if err == nil && permitted {
			row.Scope.CanSubmit, err = h.postingReviewCapability(c, p, scope, row.Scope.Departments, PermPostingSubmit)
		}
		if err == nil && permitted {
			row.Scope.CanReview, err = h.postingReviewCapability(c, p, scope, row.Scope.Departments, PermPostingReview)
		}
		if err != nil {
			respondPostingProposalError(c, err)
			return nil, nil
		}
		if permitted {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// postingReviewScopeAllows runs the guarded read; a denied, hidden or moved scope is absent, never an error.
func (h *handlers) postingReviewScopeAllows(c *gin.Context, p authn.Principal, scope postingDraftScope, permission authz.Permission) (bool, error) {
	err := h.scopedPostingTransaction(c, p, scope, false, permission, func(_ context.Context, tx *gorm.DB, _ EmploymentPeriodSource) error {
		return postingProposalAudit(tx, p, scope, "scope_read", 0)
	})
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, authz.ErrScopedCommandDenied), errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, authz.ErrDepartmentReferenceHidden), errors.Is(err, authz.ErrDepartmentReferenceChanged), errors.Is(err, errDepartmentScopeChanged):
		return false, nil
	default:
		return false, err
	}
}

// postingReviewDepartments lists held departments whose current guarded read succeeds (ADR-0055).
func (h *handlers) postingReviewDepartments(c *gin.Context, p authn.Principal, scope postingDraftScope) ([]uuid.UUID, error) {
	lister, supported := h.scopeAuthority.(authz.DepartmentScopeLister)
	if !supported {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	grants, err := lister.DepartmentScopes(c.Request.Context(), authz.ScopeRequest{TenantID: p.TenantID, Subject: p.Subject, HospitalID: scope.hospital.String(), Permission: PermPostingProposalRead})
	if err != nil {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	departments := []uuid.UUID{}
	for _, grant := range grants {
		department, err := uuid.Parse(grant.DepartmentID)
		if err != nil || grant.ScopeKind != "department" {
			return nil, authz.ErrScopedAuthorizationUnavailable
		}
		if slices.Contains(departments, department) {
			continue
		}
		held := scope
		held.department = department
		allowed, err := h.postingReviewScopeAllows(c, p, held, PermPostingProposalRead)
		if err != nil {
			return nil, err
		}
		if allowed {
			departments = append(departments, department)
		}
	}
	return departments, nil
}

// postingReviewCapability is true when the hospital, or any listed department, currently holds permission.
func (h *handlers) postingReviewCapability(c *gin.Context, p authn.Principal, scope postingDraftScope, departments []uuid.UUID, permission authz.Permission) (bool, error) {
	if len(departments) == 0 {
		return h.postingReviewScopeAllows(c, p, scope, permission)
	}
	for _, department := range departments {
		held := scope
		held.department = department
		allowed, err := h.postingReviewScopeAllows(c, p, held, permission)
		if err != nil || allowed {
			return allowed, err
		}
	}
	return false, nil
}

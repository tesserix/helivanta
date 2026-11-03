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
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type postingScopeRow struct {
	Scope     StaffPostingHospitalScope
	Candidate leaveReviewScopeRow
}

func (r postingScopeRow) MarshalJSON() ([]byte, error)    { return json.Marshal(r.Scope) }
func (r postingScopeRow) PageKey() (time.Time, uuid.UUID) { return r.Candidate.PageKey() }

type postingTargetRow struct {
	StaffPostingTarget
	CreatedAt time.Time `json:"-"`
}

func (r postingTargetRow) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

func postingAccount(c *gin.Context) (authn.Principal, bool) {
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return p, false
	}
	tenants, subjects := c.Request.Header.Values("X-Helivanta-Expected-Tenant"), c.Request.Header.Values("X-Helivanta-Expected-Subject")
	if len(tenants) != 1 || tenants[0] != p.TenantID || len(subjects) != 1 || subjects[0] != p.Subject {
		respond.Error(c, 409, "session_changed", "Reopen the current posting workspace.")
		return p, false
	}
	return p, true
}
func (h *handlers) postingScopeCandidates(c *gin.Context, page pagination.Params) ([]postingScopeRow, error) {
	c.Header("Cache-Control", "no-store")
	if _, ok := postingAccount(c); !ok {
		return nil, nil
	}
	if !authn.Refresh(c) {
		return nil, nil
	}
	candidates, err := h.leaveReviewScopeCandidates(c, page)
	if err != nil {
		return nil, err
	}
	rows := make([]postingScopeRow, 0, len(candidates))
	for _, candidate := range candidates {
		rows = append(rows, postingScopeRow{Scope: StaffPostingHospitalScope{HospitalID: candidate.Scope.HospitalID, Name: candidate.Scope.Name}, Candidate: candidate})
	}
	return rows, nil
}
func (h *handlers) authorizedPostingScopes(c *gin.Context, candidates []postingScopeRow) ([]postingScopeRow, error) {
	p, ok := postingAccount(c)
	if !ok {
		return nil, nil
	}
	markScopedPosting(c)
	rows := make([]postingScopeRow, 0, len(candidates))
	for _, row := range candidates {
		scope := postingDraftScope{hospital: row.Scope.HospitalID, staff: row.Candidate.StaffID}
		for _, writing := range []bool{false, true} {
			err := h.postingTransaction(c, p, scope, writing, func(_ context.Context, tx *gorm.DB, _ EmploymentPeriodSource) error {
				return postingDraftAudit(tx, p, scope, "scope_read", 0)
			})
			if errors.Is(err, authz.ErrScopedCommandDenied) || errors.Is(err, gorm.ErrRecordNotFound) {
				if !writing {
					break
				}
				continue
			}
			if err != nil {
				respondPostingDraftError(c, err)
				return nil, nil
			}
			if !writing {
				rows = append(rows, row)
			} else {
				rows[len(rows)-1].Scope.CanPrepare = true
			}
		}
	}
	return rows, nil
}
func (h *handlers) listPostingTargets(c *gin.Context, page pagination.Params) ([]postingTargetRow, error) {
	c.Header("Cache-Control", "no-store")
	p, ok := postingAccount(c)
	if !ok {
		return nil, nil
	}
	hospital, err := uuid.Parse(c.Param("hospital"))
	if err != nil || hospital == uuid.Nil {
		respond.NotFound(c, "Staff posting hospital")
		return nil, nil
	}
	if page.Limit > 50 {
		respond.BadRequest(c, errors.New("posting targets are limited to 50 per page"))
		return nil, nil
	}
	actor, err := h.postingActorForHospital(c, p, hospital)
	if err != nil {
		respondPostingDraftError(c, err)
		return nil, nil
	}
	markScopedPosting(c)
	scope := postingDraftScope{hospital: hospital, staff: actor}
	rows := []postingTargetRow{}
	err = h.postingTransaction(c, p, scope, false, func(_ context.Context, tx *gorm.DB, _ EmploymentPeriodSource) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_posting_directory','read',true)`).Error; err != nil {
			return err
		}
		q := tx.Table("workforce_staff").Select("id,employee_code,display_name,created_at").Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital)
		if err := tenantdb.ApplyKeyset(q, page).Find(&rows).Error; err != nil {
			return err
		}
		return postingDraftAudit(tx, p, scope, "targets_read", 0)
	})
	if err != nil {
		respondPostingDraftError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) postingActorForHospital(c *gin.Context, p authn.Principal, hospital uuid.UUID) (uuid.UUID, error) {
	if h.db == nil {
		return uuid.Nil, authz.ErrScopedAuthorizationUnavailable
	}
	var actor struct{ StaffID uuid.UUID }
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_subject',?,true),set_config('app.workforce_leave_discovery','authorized',true)`, p.Subject).Error; err != nil {
			return err
		}
		return tx.Table("workforce_login_links").Select("staff_id").Where("tenant_id=? AND hospital_id=? AND subject=? AND status='active'", p.TenantID, hospital, p.Subject).Take(&actor).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) || err == nil && actor.StaffID == uuid.Nil {
		return uuid.Nil, authz.ErrScopedCommandDenied
	}
	return actor.StaffID, err
}

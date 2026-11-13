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

const personAuthorityDiscoveryDeadline = "workforce.person_authority_discovery_deadline"

type personAuthorityScopeRow struct {
	Scope     PersonReviewHospitalScope `gorm:"embedded"`
	CreatedAt time.Time
}

func (s personAuthorityScopeRow) MarshalJSON() ([]byte, error) { return json.Marshal(s.Scope) }
func (s personAuthorityScopeRow) PageKey() (time.Time, uuid.UUID) {
	return s.CreatedAt, s.Scope.HospitalID
}

func (h *handlers) personAuthorityScopeCandidates(c *gin.Context, page pagination.Params) ([]personAuthorityScopeRow, error) {
	p, _, ok := staffPrincipal(c)
	if !ok {
		return nil, nil
	}
	if len(c.Request.Header.Values("X-Helivanta-Expected-Tenant")) != 1 || len(c.Request.Header.Values("X-Helivanta-Expected-Subject")) != 1 {
		respond.Error(c, 409, "session_changed", "Refresh your current workspace before opening person reviews.")
		return nil, nil
	}
	if !authn.Refresh(c) {
		return nil, nil
	}
	if p.IdleDeadline.IsZero() {
		personReviewError(c, authz.ErrScopedAuthorizationUnavailable)
		return nil, nil
	}
	deadline := minDeadline(p.IdleDeadline, time.Now().Add(5*time.Second))
	c.Set(personAuthorityDiscoveryDeadline, deadline)
	ctx, cancel := context.WithDeadline(c.Request.Context(), deadline)
	defer cancel()
	rows := []personAuthorityScopeRow{}
	err := h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		query := tx.Table("workforce_hospitals").Select("id AS hospital_id,name,created_at").Where("tenant_id=? AND establishment_kind=?", p.TenantID, hospitalEstablishmentKind)
		if page.After != nil {
			query = query.Where("(created_at,id)<(?,?)", page.After.CreatedAt, page.After.ID)
		}
		return query.Order("created_at DESC,id DESC").Limit(page.Limit + 1).Find(&rows).Error
	})
	return rows, err
}

func minDeadline(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func (h *handlers) authorizedPersonAuthorityScopes(c *gin.Context, candidates []personAuthorityScopeRow) ([]personAuthorityScopeRow, error) {
	p, ok := authn.PrincipalFrom(c)
	deadline, valid := c.Get(personAuthorityDiscoveryDeadline)
	at, typed := deadline.(time.Time)
	if !ok || !valid || !typed {
		personReviewError(c, authz.ErrScopedAuthorizationUnavailable)
		return nil, nil
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), at)
	defer cancel()
	// Refresh mutable request state once; workers only use immutable snapshots.
	permissions, ok := authz.RefreshPermissions(c)
	if !ok {
		return nil, nil
	}
	allowed := make([]bool, len(candidates))
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(4)
	for i := range candidates {
		group.Go(func() error {
			row := &candidates[i]
			for _, permission := range []authz.Permission{PermIdentityChecksManage, PermIdentityChecksReview} {
				err := h.withPersonReviewAuthorityContext(ctx, p, row.Scope.HospitalID, permission, permissions.Has(permission), func(commandCtx context.Context, authority personReviewAuthority) error {
					return h.transaction(commandCtx, p, row.Scope.HospitalID, func(tx *gorm.DB) error {
						var root struct{ Name string }
						if err := tx.Raw(`SELECT name FROM workforce_hospitals WHERE tenant_id=? AND id=? AND establishment_kind=? FOR SHARE`, p.TenantID, row.Scope.HospitalID, hospitalEstablishmentKind).Take(&root).Error; err != nil {
							return err
						}
						row.Scope.Name = root.Name
						return personReviewAudit(tx, p, row.Scope.HospitalID, row.Scope.HospitalID, "workforce.person_review_authority_scope.read", 0, authority)
					})
				})
				if errors.Is(err, authz.ErrScopedCommandDenied) || errors.Is(err, gorm.ErrRecordNotFound) {
					continue
				}
				if err != nil {
					return err
				}
				row.Scope.CanSubmit = row.Scope.CanSubmit || permission == PermIdentityChecksManage
				row.Scope.CanReview = row.Scope.CanReview || permission == PermIdentityChecksReview
				allowed[i] = true
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		personReviewError(c, err)
		return nil, nil
	}
	rows := make([]personAuthorityScopeRow, 0, len(candidates))
	for i, row := range candidates {
		if allowed[i] {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

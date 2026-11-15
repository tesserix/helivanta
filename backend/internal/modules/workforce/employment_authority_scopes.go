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
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

const employmentDiscoveryDeadline = "workforce.employment_discovery_deadline"

type employmentAuthorityScopeRow struct {
	Scope     EmploymentReviewHospitalScope `gorm:"embedded"`
	CreatedAt time.Time
}

func (s employmentAuthorityScopeRow) MarshalJSON() ([]byte, error) { return json.Marshal(s.Scope) }
func (s employmentAuthorityScopeRow) PageKey() (time.Time, uuid.UUID) {
	return s.CreatedAt, s.Scope.HospitalID
}

func employmentDiscoveryProof(p authn.Principal, hospital uuid.UUID, permission authz.Permission, source authz.GovernanceSourceEvidence) (map[string]any, error) {
	capability, template := employmentReviewCapability(permission)
	now := time.Now()
	if template == "" || p.TenantID == "" || p.Subject == "" || hospital == uuid.Nil || p.AuthTime.IsZero() || p.AuthTime.After(now) || !p.IdleDeadline.After(now) || !source.ExpiresAt.After(now) || !validWorkforceGovernanceSource(p, establishment{kind: hospitalEstablishmentKind, id: hospital}, source, template) {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	proof := map[string]any{"purpose": "employment_discovery", "tenant_id": p.TenantID, "hospital_id": hospital, "actor_subject": p.Subject, "capability": capability, "authority_kind": source.Kind, "membership_epoch": source.MembershipEpoch, "owner_count": source.OwnerCount, "source_expires_at": source.ExpiresAt, "authenticated_at": p.AuthTime, "idle_deadline": p.IdleDeadline}
	if source.Kind == authz.GovernanceOwnerSource {
		proof["owner_membership_id"] = source.OwnerMembershipID
	} else {
		proof["source_grant_id"], proof["source_grant_version"], proof["template_key"] = source.GrantID, source.Version, source.TemplateKey
	}
	return proof, nil
}

func (h *handlers) employmentAuthorityScopeCandidates(c *gin.Context, page pagination.Params) ([]employmentAuthorityScopeRow, error) {
	p, _, ok := staffPrincipal(c)
	if !ok {
		return nil, nil
	}
	tenants, subjects := c.Request.Header.Values("X-Helivanta-Expected-Tenant"), c.Request.Header.Values("X-Helivanta-Expected-Subject")
	if len(tenants) != 1 || tenants[0] != p.TenantID || len(subjects) != 1 || subjects[0] != p.Subject {
		respond.Error(c, 409, "session_changed", "Refresh your current workspace before opening employment reviews.")
		return nil, nil
	}
	if !authn.Refresh(c) {
		return nil, nil
	}
	if p.IdleDeadline.IsZero() {
		employmentReviewError(c, authz.ErrScopedAuthorizationUnavailable)
		return nil, nil
	}
	deadline := minDeadline(p.IdleDeadline, time.Now().Add(5*time.Second))
	c.Set(employmentDiscoveryDeadline, deadline)
	ctx, cancel := context.WithDeadline(c.Request.Context(), deadline)
	defer cancel()
	rows := []employmentAuthorityScopeRow{}
	err := h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		query := tx.Table("workforce_hospitals").Select("id AS hospital_id,name,created_at").Where("tenant_id=? AND establishment_kind=?", p.TenantID, hospitalEstablishmentKind)
		if page.After != nil {
			query = query.Where("(created_at,id)<(?,?)", page.After.CreatedAt, page.After.ID)
		}
		return query.Order("created_at DESC,id DESC").Limit(page.Limit + 1).Find(&rows).Error
	})
	return rows, err
}

func (h *handlers) authorizedEmploymentAuthorityScopes(c *gin.Context, candidates []employmentAuthorityScopeRow) ([]employmentAuthorityScopeRow, error) {
	p, ok := authn.PrincipalFrom(c)
	value, present := c.Get(employmentDiscoveryDeadline)
	deadline, typed := value.(time.Time)
	if !ok || !present || !typed || h.governanceSources == nil {
		employmentReviewError(c, authz.ErrScopedAuthorizationUnavailable)
		return nil, nil
	}
	permissions, ok := authz.RefreshPermissions(c)
	if !ok {
		return nil, nil
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), deadline)
	defer cancel()
	allowed := make([]bool, len(candidates))
	group, ctx := errgroup.WithContext(ctx)
	group.SetLimit(4)
	for i := range candidates {
		group.Go(func() error {
			row := &candidates[i]
			hospital := row.Scope.HospitalID
			for _, permission := range []authz.Permission{PermEmploymentPrepare, PermEmploymentReview} {
				if !permissions.Has(permission) {
					continue
				}
				capability, _ := employmentReviewCapability(permission)
				callbacks := 0
				var callbackErr error
				err := h.governanceSources.WithGovernanceSource(ctx, authz.GovernanceSourceRequest{Principal: p, Capability: capability, Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: hospitalEstablishmentKind, ID: &hospital}}, func(commandCtx context.Context, source authz.GovernanceSourceEvidence) error {
					callbacks++
					if callbacks != 1 || !h.db.GuardedCommandActive(commandCtx) || tenantdb.ActorFrom(commandCtx) != p.Subject {
						callbackErr = authz.ErrScopedAuthorizationUnavailable
						return callbackErr
					}
					proof, err := employmentDiscoveryProof(p, row.Scope.HospitalID, permission, source)
					if err != nil {
						callbackErr = err
						return err
					}
					callbackErr = h.transaction(commandCtx, p, row.Scope.HospitalID, func(tx *gorm.DB) error {
						var root struct{ Name string }
						if err := tx.Raw("SELECT name FROM workforce_hospitals WHERE tenant_id=? AND id=? AND establishment_kind=? FOR SHARE", p.TenantID, row.Scope.HospitalID, hospitalEstablishmentKind).Take(&root).Error; err != nil {
							return err
						}
						row.Scope.Name = root.Name
						raw, err := json.Marshal(proof)
						if err != nil {
							return err
						}
						return tx.Exec("INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.employment_authority_scope.read',?,?::jsonb)", p.TenantID, row.Scope.HospitalID, p.Subject, row.Scope.HospitalID, string(raw)).Error
					})
					return callbackErr
				})
				if callbackErr != nil {
					return callbackErr
				}
				if callbacks == 0 && (errors.Is(err, authz.ErrScopedCommandDenied) || errors.Is(err, authz.ErrGovernanceScopeHidden)) {
					continue
				}
				if err != nil {
					return err
				}
				if callbacks != 1 {
					return authz.ErrScopedAuthorizationUnavailable
				}
				row.Scope.CanSubmit = row.Scope.CanSubmit || permission == PermEmploymentPrepare
				row.Scope.CanReview = row.Scope.CanReview || permission == PermEmploymentReview
				allowed[i] = true
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		employmentReviewError(c, err)
		return nil, nil
	}
	rows := make([]employmentAuthorityScopeRow, 0, len(candidates))
	for i, row := range candidates {
		if allowed[i] {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

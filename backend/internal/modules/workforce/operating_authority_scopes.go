package workforce

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

const operatingDiscoveryDeadline = "workforce.operating_discovery_deadline"

type operatingAuthorityScopeRow struct {
	Scope     workforcecontract.OperatingGovernanceEmployerScope `gorm:"embedded"`
	CreatedAt time.Time
}

func (row operatingAuthorityScopeRow) MarshalJSON() ([]byte, error) { return json.Marshal(row.Scope) }
func (row operatingAuthorityScopeRow) PageKey() (time.Time, uuid.UUID) {
	return row.CreatedAt, row.Scope.EmployerID
}

func (h *handlers) operatingAuthorityScopeCandidates(c *gin.Context, page pagination.Params) ([]operatingAuthorityScopeRow, error) {
	p, _, ok := staffPrincipal(c)
	if !ok {
		return nil, nil
	}
	tenants, subjects := c.Request.Header.Values("X-Helivanta-Expected-Tenant"), c.Request.Header.Values("X-Helivanta-Expected-Subject")
	if len(tenants) != 1 || tenants[0] != p.TenantID || len(subjects) != 1 || subjects[0] != p.Subject {
		respond.Error(c, 409, "session_changed", "Refresh your current workspace before opening staff access.")
		return nil, nil
	}
	if !authn.Refresh(c) {
		return nil, nil
	}
	current, ok := authn.PrincipalFrom(c)
	if !ok || current.TenantID != p.TenantID || current.Subject != p.Subject {
		respond.Error(c, 409, "session_changed", "Refresh your current workspace before opening staff access.")
		return nil, nil
	}
	p = current
	permissions, ok := authz.RefreshPermissions(c)
	if !ok {
		return nil, nil
	}
	if !permissions.Has(authz.Permission(authz.GovernanceOperatingManage)) && !permissions.Has(authz.Permission(authz.GovernanceOperatingReview)) {
		respond.Forbidden(c, "Independent employer duty administration is not available for this account.")
		return nil, nil
	}
	if h.db == nil || h.governanceSources == nil || p.IdleDeadline.IsZero() {
		operatingDiscoveryError(c, authz.ErrScopedAuthorizationUnavailable)
		return nil, nil
	}
	deadline := minDeadline(p.IdleDeadline, time.Now().Add(5*time.Second))
	c.Set(operatingDiscoveryDeadline, deadline)
	ctx, cancel := context.WithDeadline(c.Request.Context(), deadline)
	defer cancel()
	rows := []operatingAuthorityScopeRow{}
	err := h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		query := tx.Table("workforce_hospitals").Select("id AS employer_id,establishment_kind AS employer_kind,created_at").Where("tenant_id=? AND establishment_kind IN ?", p.TenantID, []string{pharmacyEstablishmentKind, supplierEstablishmentKind})
		if page.After != nil {
			query = query.Where("(created_at,id)<(?,?)", page.After.CreatedAt, page.After.ID)
		}
		return query.Order("created_at DESC,id DESC").Limit(page.Limit + 1).Find(&rows).Error
	})
	return rows, err
}

func (h *handlers) authorizedOperatingAuthorityScopes(c *gin.Context, candidates []operatingAuthorityScopeRow) ([]operatingAuthorityScopeRow, error) {
	p, ok := authn.PrincipalFrom(c)
	value, present := c.Get(operatingDiscoveryDeadline)
	deadline, valid := value.(time.Time)
	if !ok || !present || !valid || h.governanceSources == nil {
		operatingDiscoveryError(c, authz.ErrScopedAuthorizationUnavailable)
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
	for index := range candidates {
		group.Go(func() error {
			row := &candidates[index]
			employer := row.Scope.EmployerID
			for _, capability := range []authz.GovernanceCapability{authz.GovernanceOperatingManage, authz.GovernanceOperatingReview} {
				if !permissions.Has(authz.Permission(capability)) {
					continue
				}
				callbacks := 0
				var callbackErr error
				err := h.governanceSources.WithGovernanceSource(ctx, authz.GovernanceSourceRequest{Principal: p, Capability: capability, Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: row.Scope.EmployerKind, ID: &employer}}, func(commandCtx context.Context, source authz.GovernanceSourceEvidence) error {
					callbacks++
					if callbacks != 1 || !h.db.GuardedCommandActive(commandCtx) || tenantdb.ActorFrom(commandCtx) != p.Subject || !source.ExpiresAt.After(time.Now()) || !validWorkforceGovernanceSource(p, establishment{kind: row.Scope.EmployerKind, id: employer}, source, "iam.operating_access_reviewer.v1") {
						callbackErr = authz.ErrScopedAuthorizationUnavailable
						return callbackErr
					}
					callbackErr = h.transaction(commandCtx, p, employer, func(tx *gorm.DB) error {
						var root struct{ Name string }
						if err := tx.Raw(`SELECT name FROM workforce_hospitals WHERE tenant_id=? AND id=? AND establishment_kind=? FOR SHARE`, p.TenantID, employer, row.Scope.EmployerKind).Take(&root).Error; err != nil {
							return err
						}
						if !validBenefitText(root.Name, 1, 200) {
							return authz.ErrScopedAuthorizationUnavailable
						}
						row.Scope.Name = root.Name
						return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.operating_authority_scope.read',?,jsonb_build_object('capability',?::text,'authority_kind',?::text,'membership_epoch',?::bigint))`, p.TenantID, employer, p.Subject, employer, string(capability), string(source.Kind), source.MembershipEpoch).Error
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
				row.Scope.CanManage = row.Scope.CanManage || capability == authz.GovernanceOperatingManage
				row.Scope.CanReview = row.Scope.CanReview || capability == authz.GovernanceOperatingReview
				allowed[index] = true
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		operatingDiscoveryError(c, err)
		return nil, nil
	}
	rows := make([]operatingAuthorityScopeRow, 0, len(candidates))
	for index, row := range candidates {
		if allowed[index] {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func operatingDiscoveryError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, authz.ErrScopedCredentialRevoked):
		respond.Unauthenticated(c, "Sign in again before opening staff access.")
	case errors.Is(err, authz.ErrScopedCommandDenied):
		respond.Forbidden(c, "Current independent employer duty administration is required.")
	default:
		respond.Error(c, 503, "assignment_source_unavailable", "Staff access could not be confirmed. Try again.")
	}
}

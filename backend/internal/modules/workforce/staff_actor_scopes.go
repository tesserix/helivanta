package workforce

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
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

type staffActorScopeRow struct {
	Scope     StaffActorHospitalChoice
	Candidate leaveReviewScopeRow
}

func (r staffActorScopeRow) MarshalJSON() ([]byte, error)    { return json.Marshal(r.Scope) }
func (r staffActorScopeRow) PageKey() (time.Time, uuid.UUID) { return r.Candidate.PageKey() }

func (h *handlers) staffActorScopeCandidates(c *gin.Context, page pagination.Params) ([]staffActorScopeRow, error) {
	p, _, ok := staffPrincipal(c)
	if !ok {
		return nil, nil
	}
	tenants, subjects := c.Request.Header.Values("X-Helivanta-Expected-Tenant"), c.Request.Header.Values("X-Helivanta-Expected-Subject")
	if len(tenants) != 1 || tenants[0] != p.TenantID || len(subjects) != 1 || subjects[0] != p.Subject {
		respond.Error(c, 409, "session_changed", "Reopen the current Staff workspace.")
		return nil, nil
	}
	values, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil {
		respond.Error(c, 400, "invalid_request", "Choose one bounded hospital page.")
		return nil, nil
	}
	for key, value := range values {
		if (key != "limit" && key != "cursor") || len(value) != 1 {
			respond.Error(c, 400, "invalid_request", "Choose one bounded hospital page.")
			return nil, nil
		}
	}
	if _, ok := h.scopeAuthority.(authz.ScopedCommandGuard); !ok || h.staffActors == nil || h.db == nil {
		staffActorScopeError(c, authz.ErrScopedAuthorizationUnavailable)
		return nil, nil
	}
	if !authn.Refresh(c) {
		return nil, nil
	}
	candidates, err := h.leaveReviewScopeCandidates(c, page)
	if err != nil {
		staffActorScopeError(c, err)
		return nil, nil
	}
	rows := make([]staffActorScopeRow, 0, len(candidates))
	for _, candidate := range candidates {
		rows = append(rows, staffActorScopeRow{Scope: StaffActorHospitalChoice{HospitalID: candidate.Scope.HospitalID, Name: candidate.Scope.Name}, Candidate: candidate})
	}
	return rows, nil
}

func (h *handlers) authorizedStaffActorScopes(c *gin.Context, candidates []staffActorScopeRow) ([]staffActorScopeRow, error) {
	p, _, ok := staffPrincipal(c)
	if !ok {
		return nil, nil
	}
	guard, supported := h.scopeAuthority.(authz.ScopedCommandGuard)
	if !supported || h.staffActors == nil || p.IdleDeadline.IsZero() {
		staffActorScopeError(c, authz.ErrScopedAuthorizationUnavailable)
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
			request := authz.ScopeRequest{TenantID: p.TenantID, Subject: p.Subject, HospitalID: hospital.String(), Permission: authz.StaffActorUse}
			grant, err := h.scopeAuthority.AuthorizeScope(ctx, request)
			if err != nil {
				return authz.ErrScopedAuthorizationUnavailable
			}
			if grant.AssignmentID == "" {
				return nil
			}
			err = guard.WithScopeGrant(ctx, request, grant, p.AuthTime, func(commandCtx context.Context) error {
				return h.staffActors.WithCurrentStaffActor(commandCtx, authz.StaffActorScope{TenantID: p.TenantID, Subject: p.Subject, HospitalID: hospital}, func(actorCtx context.Context, actor authz.StaffActorReference) error {
					if actor.LinkID != candidate.Candidate.ID || actor.StaffID != candidate.Candidate.StaffID || actor.LinkVersion != candidate.Candidate.Version {
						return authz.ErrScopedCommandDenied
					}
					return h.transaction(actorCtx, p, hospital, func(tx *gorm.DB) error {
						var current struct{ Name string }
						if err := tx.Table("workforce_hospitals").Select("name").Where("tenant_id=? AND id=?", p.TenantID, hospital).Take(&current).Error; err != nil {
							return err
						}
						if err := tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.staff_actor_scope.read',?,jsonb_build_object('assignment_id',?::text,'assignment_version',?::bigint,'actor_link_version',?::bigint))`, p.TenantID, hospital, p.Subject, hospital, grant.AssignmentID, grant.Version, actor.LinkVersion).Error; err != nil {
							return err
						}
						candidates[i].Scope.Name = current.Name
						allowed[i] = true
						return nil
					})
				})
			})
			if errors.Is(err, authz.ErrScopedCommandDenied) || errors.Is(err, gorm.ErrRecordNotFound) {
				allowed[i] = false
				return nil
			}
			return err
		})
	}
	if err := group.Wait(); err != nil {
		staffActorScopeError(c, err)
		return nil, nil
	}
	rows := make([]staffActorScopeRow, 0, len(candidates))
	for i, row := range candidates {
		if allowed[i] {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func staffActorScopeError(c *gin.Context, err error) {
	if errors.Is(err, authz.ErrScopedCredentialRevoked) {
		respond.Unauthenticated(c, "credential revoked")
		return
	}
	respond.Error(c, 503, "scoped_authorization_unavailable", "Current Staff hospital access could not be verified.")
}

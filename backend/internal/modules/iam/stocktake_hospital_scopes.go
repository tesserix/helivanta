package iam

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type hospitalStocktakeScopeHandlers struct {
	db          *tenantdb.DB
	graph       authz.ScopedAssignmentChecker
	authority   authz.CompoundScopedCommandGuard
	hospitals   authz.StocktakeHospitalNameReader
	departments authz.StocktakeDepartmentNameReader
	paths       authz.DepartmentScopeGuard
}

type hospitalStocktakeScopeRow struct {
	Candidate   authorityCandidate `gorm:"embedded"`
	CreatedAt   time.Time
	TemplateKey string
	ExpiresAt   time.Time
	Deadline    time.Time                    `gorm:"-"`
	Choice      HospitalStocktakeScopeChoice `gorm:"-"`
}

func (r hospitalStocktakeScopeRow) MarshalJSON() ([]byte, error) { return json.Marshal(r.Choice) }
func (r hospitalStocktakeScopeRow) PageKey() (time.Time, uuid.UUID) {
	return r.CreatedAt, r.Candidate.Snapshot.ID
}

func registerHospitalStocktakeScopes(r *platform.Router, deps platform.Deps) {
	h := &hospitalStocktakeScopeHandlers{db: deps.DB, paths: deps.DepartmentScopes}
	h.graph, _ = deps.Authz.(authz.ScopedAssignmentChecker)
	h.authority, _ = deps.ScopedAuthorizer.(authz.CompoundScopedCommandGuard)
	h.hospitals, _ = deps.StocktakeActors.(authz.StocktakeHospitalNameReader)
	h.departments, _ = deps.DepartmentReferences.(authz.StocktakeDepartmentNameReader)
	platform.SealedFilteredListRoute(r, "/me/hospital-pharmacy-stocktake-scopes", authz.Public, 20, h.scan, h.filter)
}

func (h *hospitalStocktakeScopeHandlers) scan(c *gin.Context, page pagination.Params) ([]hospitalStocktakeScopeRow, error) {
	c.Header("Cache-Control", "private, no-store")
	if !authn.Refresh(c) {
		return nil, nil
	}
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	tenants, subjects := c.Request.Header.Values("X-Helivanta-Expected-Tenant"), c.Request.Header.Values("X-Helivanta-Expected-Subject")
	if len(tenants) != 1 || tenants[0] != p.TenantID || len(subjects) != 1 || subjects[0] != p.Subject {
		respond.Error(c, 409, "session_changed", "Your signed-in account changed. Reload before choosing a department.")
		return nil, nil
	}
	if !stocktakeWorkerQuery(c) {
		respond.Error(c, 400, "invalid_request", "Choose one current department page.")
		return nil, nil
	}
	if h.db == nil || h.graph == nil || h.authority == nil || h.hospitals == nil || h.departments == nil || h.paths == nil || p.IdleDeadline.IsZero() {
		hospitalStocktakeScopeError(c, authz.ErrScopedAuthorizationUnavailable)
		return nil, nil
	}
	deadline := time.Now().Add(4 * time.Second)
	if p.IdleDeadline.Before(deadline) {
		deadline = p.IdleDeadline
	}
	ctx, cancel := context.WithDeadline(tenantdb.WithActor(c.Request.Context(), p.Subject), deadline)
	defer cancel()
	rows := []hospitalStocktakeScopeRow{}
	err := h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := departmentAwareAccess(tx); err != nil {
			return err
		}
		query := `SELECT s.id,s.version,s.hospital_id,s.subject,s.permissions,s.scope_kind,s.department_id,s.membership_epoch,q.epoch AS scope_epoch,p.created_at,p.template_key,s.expires_at
FROM iam_assignment_snapshots s
JOIN iam_assignment_proposals p ON p.tenant_id=s.tenant_id AND p.id=s.id AND p.version=s.version+1 AND p.status='active' AND p.membership_epoch=s.membership_epoch
JOIN iam_subject_membership_epochs m ON m.tenant_id=s.tenant_id AND m.subject=s.subject AND m.epoch=s.membership_epoch AND m.member_count>0
JOIN iam_assignment_scope_epochs q ON q.tenant_id=s.tenant_id AND q.hospital_id=s.hospital_id AND q.subject=s.subject AND q.epoch=q.acknowledged_epoch
WHERE s.tenant_id=? AND s.subject=? AND s.scope_kind='department' AND s.department_id IS NOT NULL
AND p.template_key IN('pharmacy.department_stocktake_reader.v1','pharmacy.department_stocktake_counter.v1','pharmacy.department_stocktake_reviewer.v1')
AND s.valid_from<=clock_timestamp() AND s.expires_at>clock_timestamp()`
		args := []any{p.TenantID, p.Subject}
		if page.After != nil {
			query += ` AND(p.created_at,s.id)>(?,?)`
			args = append(args, page.After.CreatedAt, page.After.ID)
		}
		query += ` ORDER BY p.created_at,s.id LIMIT ?`
		args = append(args, page.Limit+1)
		return tx.Raw(query, args...).Scan(&rows).Error
	})
	if err != nil {
		hospitalStocktakeScopeError(c, authz.ErrScopedAuthorizationUnavailable)
		return nil, nil
	}
	for i := range rows {
		rows[i].Deadline = deadline
	}
	return rows, nil
}

func (h *hospitalStocktakeScopeHandlers) filter(c *gin.Context, candidates []hospitalStocktakeScopeRow) ([]hospitalStocktakeScopeRow, error) {
	rows := make([]hospitalStocktakeScopeRow, 0, len(candidates))
	if len(candidates) == 0 {
		return rows, nil
	}
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	ctx, cancel := context.WithDeadline(tenantdb.WithActor(c.Request.Context(), p.Subject), candidates[0].Deadline)
	defer cancel()
	for _, candidate := range candidates {
		snapshot := candidate.Candidate.Snapshot
		var definition TemplateDefinition
		for _, template := range staffAccessTemplates() {
			if template.Key == candidate.TemplateKey && template.Family == "pharmacy_stocktake" {
				definition = template
				break
			}
		}
		assigned := make([]string, len(snapshot.Permissions))
		for i, value := range snapshot.Permissions {
			assigned[i] = string(value)
		}
		if definition.Key == "" || !slices.Equal(assigned, definition.Permissions) || snapshot.Subject != p.Subject || snapshot.ScopeKind != "department" || snapshot.DepartmentID == nil {
			hospitalStocktakeScopeError(c, authz.ErrScopedAuthorizationUnavailable)
			return nil, nil
		}
		allowed := true
		for _, permission := range []authz.Permission{stocktakeWorkerRead, authz.StocktakeActorUse} {
			current, err := h.graph.CheckScopedAssignment(ctx, snapshot.scoped(p.TenantID), p.Subject, permission)
			if err != nil {
				hospitalStocktakeScopeError(c, authz.ErrScopedAuthorizationUnavailable)
				return nil, nil
			}
			allowed = allowed && current
		}
		if !allowed {
			continue
		}
		path, err := h.paths.CurrentPath(ctx, p.TenantID, snapshot.HospitalID, *snapshot.DepartmentID)
		if errors.Is(err, authz.ErrDepartmentReferenceHidden) || errors.Is(err, authz.ErrDepartmentReferenceChanged) {
			continue
		}
		if err != nil {
			hospitalStocktakeScopeError(c, authz.ErrScopedAuthorizationUnavailable)
			return nil, nil
		}
		scope := authz.StaffActorScope{TenantID: p.TenantID, Subject: p.Subject, HospitalID: snapshot.HospitalID, DepartmentPath: make([]string, len(path))}
		for i, id := range path {
			scope.DepartmentPath[i] = id.String()
		}
		checks := make([]authz.ScopeCheck, 0, 2)
		for _, permission := range []authz.Permission{stocktakeWorkerRead, authz.StocktakeActorUse} {
			checks = append(checks, authz.ScopeCheck{Request: authz.ScopeRequest{TenantID: p.TenantID, Subject: p.Subject, HospitalID: snapshot.HospitalID.String(), DepartmentPath: slices.Clone(scope.DepartmentPath), Permission: permission}, Grant: candidate.Candidate.grant()})
		}
		err = h.authority.WithScopeGrants(ctx, checks, p.AuthTime, func(commandCtx context.Context) error {
			hospitalName, err := h.hospitals.StocktakeHospitalName(commandCtx, scope)
			if err != nil {
				return err
			}
			departmentName, err := h.departments.StocktakeDepartmentName(commandCtx, scope)
			if err != nil {
				return err
			}
			if !stocktakeWorkerText(hospitalName, 150) || !stocktakeWorkerText(departmentName, 120) || !stocktakeWorkerText(definition.Name, 200) {
				return authz.ErrScopedAuthorizationUnavailable
			}
			if err := h.db.WithTenantCommand(commandCtx, p.TenantID, func(tx *gorm.DB) error {
				return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'iam.hospital_stocktake_scope.read','iam_assignment_snapshots',?)`, p.TenantID, p.Subject, snapshot.ID).Error
			}); err != nil {
				return err
			}
			owner, err := uuid.Parse(p.TenantID)
			if err != nil {
				return authz.ErrScopedAuthorizationUnavailable
			}
			candidate.Choice = HospitalStocktakeScopeChoice{OwnerTenantID: owner, ID: snapshot.ID, HospitalID: snapshot.HospitalID, DepartmentID: *snapshot.DepartmentID, HospitalName: hospitalName, DepartmentName: departmentName, TemplateKey: definition.Key, TemplateLabel: definition.Name, AssignedPermissions: assigned, SnapshotVersion: snapshot.Version, AuthorityEpoch: candidate.Candidate.ScopeEpoch, ExpiresAt: candidate.ExpiresAt}
			return nil
		})
		if errors.Is(err, authz.ErrScopedCommandDenied) || errors.Is(err, authz.ErrDepartmentReferenceHidden) || errors.Is(err, authz.ErrDepartmentReferenceChanged) || errors.Is(err, gorm.ErrRecordNotFound) {
			continue
		}
		if err != nil {
			hospitalStocktakeScopeError(c, err)
			return nil, nil
		}
		rows = append(rows, candidate)
	}
	return rows, nil
}

func hospitalStocktakeScopeError(c *gin.Context, err error) {
	if errors.Is(err, authz.ErrScopedCredentialRevoked) {
		respond.Error(c, 401, "credentials_revoked", "Your credentials changed. Sign in again.")
		return
	}
	respond.Error(c, 503, "scoped_authorization_unavailable", "Current hospital stocktake access or disclosure evidence is unavailable.")
}

package organization

import (
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func (g *departmentReferenceGuard) StocktakeDepartments(ctx context.Context, scope authz.StaffActorScope, page pagination.Params) ([]authz.StocktakeDepartment, error) {
	checks, err := authz.StocktakeReadScopeChecks(ctx, scope)
	if err != nil || g == nil || g.db == nil || tenantdb.ActorFrom(ctx) != scope.Subject || !g.db.GuardedCommandActive(ctx) || page.Limit < 1 || page.Limit > 20 {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	if page.After != nil && (page.After.TenantID != scope.TenantID || page.After.ID.Version() != 7 || page.After.CreatedAt.IsZero()) {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	root := uuid.MustParse(scope.DepartmentPath[0])
	structure := &departmentScopeGuard{db: g.db}
	rows := []authz.StocktakeDepartment{}
	var read func(context.Context, int) error
	read = func(held context.Context, index int) error {
		if index < len(checks) {
			return structure.WithinCurrentDepartment(held, scope.TenantID, scope.HospitalID, root, uuid.MustParse(checks[index].Grant.DepartmentID), func(current context.Context) error { return read(current, index+1) })
		}
		return g.db.WithTenantCommand(held, scope.TenantID, func(tx *gorm.DB) error {
			path, err := readDepartmentPath(tx, scope.TenantID, scope.HospitalID, root)
			if err != nil {
				return err
			}
			current := make([]string, len(path))
			for i, row := range path {
				current[i] = row.ID.String()
			}
			if !slices.Equal(current, scope.DepartmentPath) {
				return authz.ErrDepartmentReferenceChanged
			}
			subtree, err := structure.CurrentSubtree(held, scope.TenantID, scope.HospitalID, []uuid.UUID{root})
			if err != nil {
				return err
			}
			query := tx.Table("organization_departments").Select("id,revision_id,name,code,created_at").Where("tenant_id=? AND hospital_id=? AND id IN ? AND status='active'", scope.TenantID, scope.HospitalID, subtree)
			if page.After != nil {
				query = query.Where("(created_at,id)<(?,?)", page.After.CreatedAt, page.After.ID)
			}
			if err := query.Order("created_at DESC,id DESC").Limit(page.Limit + 1).Scan(&rows).Error; err != nil {
				return err
			}
			for _, row := range rows {
				if row.ID.Version() != 7 || row.RevisionID.Version() != 7 || row.CreatedAt.IsZero() || !validDepartmentText(row.Name, 200) || !departmentCode.MatchString(row.Code) {
					return authz.ErrScopedAuthorizationUnavailable
				}
			}
			return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'organization.stocktake_departments.read','organization_departments',?)`, scope.TenantID, scope.Subject, root).Error
		})
	}
	if err := read(ctx, 0); err != nil {
		return nil, errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
	}
	return rows, nil
}

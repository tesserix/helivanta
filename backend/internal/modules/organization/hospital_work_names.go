package organization

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"slices"
)

func (g *departmentReferenceGuard) HospitalWorkDepartmentName(ctx context.Context, purpose authz.HospitalWorkPurpose, scope authz.StaffActorScope) (string, error) {
	checks, err := purpose.ReadScopeChecks(ctx, scope)
	if err != nil || g == nil || g.db == nil || tenantdb.ActorFrom(ctx) != scope.Subject || !g.db.GuardedCommandActive(ctx) {
		return "", authz.ErrScopedAuthorizationUnavailable
	}
	target := uuid.MustParse(scope.DepartmentPath[0])
	structure := &departmentScopeGuard{db: g.db}
	var name string
	var read func(int) error
	read = func(index int) error {
		if index < len(checks) {
			return structure.WithinCurrentDepartment(ctx, scope.TenantID, scope.HospitalID, target, uuid.MustParse(checks[index].Grant.DepartmentID), func(context.Context) error { return read(index + 1) })
		}
		return g.db.WithTenantCommand(ctx, scope.TenantID, func(tx *gorm.DB) error {
			current, err := readDepartmentPath(tx, scope.TenantID, scope.HospitalID, target)
			if err != nil {
				return err
			}
			path := make([]string, len(current))
			for i, row := range current {
				path[i] = row.ID.String()
			}
			if !slices.Equal(path, scope.DepartmentPath) {
				return authz.ErrDepartmentReferenceChanged
			}
			var row struct{ Name string }
			if err := tx.Table("organization_departments").Select("name").Where("tenant_id=? AND hospital_id=? AND id=? AND status='active'", scope.TenantID, scope.HospitalID, target).Take(&row).Error; err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,?,'organization_departments',?)`, scope.TenantID, scope.Subject, "organization."+string(purpose)+"_department_name.read", target).Error; err != nil {
				return err
			}
			name = row.Name
			return nil
		})
	}
	if err := read(0); err != nil {
		return "", errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
	}
	return name, nil
}

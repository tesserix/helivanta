package organization

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type departmentReferenceGuard struct{ db *tenantdb.DB }

// DepartmentReferences exposes current structural validation without sharing a repository.
func (*Module) DepartmentReferences(db *tenantdb.DB) authz.DepartmentReferenceGuard {
	return &departmentReferenceGuard{db: db}
}

func (g *departmentReferenceGuard) WithCurrentDepartment(ctx context.Context, ref authz.DepartmentReference, action func(context.Context) error) error {
	tenant, err := uuid.Parse(ref.TenantID)
	if err != nil || tenant == uuid.Nil || tenant.String() != ref.TenantID || ref.HospitalID == uuid.Nil || ref.DepartmentID.Version() != 7 || ref.DepartmentRevision.Version() != 7 || action == nil || g.db == nil || !g.db.GuardedCommandActive(ctx) {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return g.db.WithTenantCommand(ctx, ref.TenantID, func(tx *gorm.DB) error {
		// Shared with the owner's exclusive restructuring gate, through the caller's commit.
		if err := tx.Exec(`SELECT pg_advisory_xact_lock_shared(hashtextextended(?,0))`, ref.TenantID+":organization-departments:"+ref.HospitalID.String()).Error; err != nil {
			return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
		}
		if err := requireDepartmentHospital(tx, ref.TenantID, ref.HospitalID); err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, errConflict) {
				return authz.ErrDepartmentReferenceHidden
			}
			return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
		}
		var row departmentRecord
		err := tx.Select("id,revision_id,status").Where("tenant_id=? AND hospital_id=? AND id=?", ref.TenantID, ref.HospitalID, ref.DepartmentID).Clauses(clause.Locking{Strength: "SHARE"}).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return authz.ErrDepartmentReferenceHidden
		}
		if err != nil {
			return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
		}
		if row.Status != "active" || row.RevisionID != ref.DepartmentRevision {
			return authz.ErrDepartmentReferenceChanged
		}
		return action(ctx)
	})
}

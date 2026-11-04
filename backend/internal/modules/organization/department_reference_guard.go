package organization

import (
	"bytes"
	"context"
	"errors"
	"slices"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type departmentReferenceGuard struct{ db *tenantdb.DB }

// Matches the persisted hierarchy limit in departments.sql.
const departmentReferenceMaxDepth = 8

const departmentAncestryQuery = `WITH RECURSIVE ancestry AS (
 SELECT id,parent_id,status,ARRAY[id] AS path,1 AS depth
 FROM organization_departments WHERE tenant_id=? AND hospital_id=? AND id=?
 UNION ALL
 SELECT p.id,p.parent_id,p.status,a.path||p.id,a.depth+1
 FROM ancestry a JOIN LATERAL (
  SELECT id,parent_id,status FROM organization_departments
  WHERE tenant_id=? AND hospital_id=? AND id=a.parent_id LIMIT 1
 ) p ON true
 WHERE a.depth<? AND NOT (p.id=ANY(a.path))
)
SELECT COALESCE(bool_or(id=?),false)
 AND COALESCE(bool_or(parent_id IS NULL),false)
 AND COALESCE(bool_and(status='active'),false) FROM ancestry`

// DepartmentReferences exposes current structural validation without sharing a repository.
func (*Module) DepartmentReferences(db *tenantdb.DB) authz.DepartmentReferenceGuard {
	return &departmentReferenceGuard{db: db}
}

func (g *departmentReferenceGuard) WithCurrentDepartment(ctx context.Context, ref authz.DepartmentReference, action func(context.Context) error) error {
	tenant, err := uuid.Parse(ref.TenantID)
	hasAncestor := ref.AncestorID != uuid.Nil
	if err != nil || tenant == uuid.Nil || tenant.String() != ref.TenantID || ref.HospitalID == uuid.Nil || ref.DepartmentID.Version() != 7 || ref.DepartmentRevision.Version() != 7 ||
		(ref.AncestorID == uuid.Nil) != (ref.AncestorRevision == uuid.Nil) || hasAncestor && (ref.AncestorID.Version() != 7 || ref.AncestorRevision.Version() != 7) ||
		action == nil || g.db == nil || !g.db.GuardedCommandActive(ctx) {
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
		ids := []uuid.UUID{ref.DepartmentID}
		if hasAncestor && ref.AncestorID != ref.DepartmentID {
			ids = append(ids, ref.AncestorID)
		}
		slices.SortFunc(ids, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
		rows := make(map[uuid.UUID]departmentRecord, len(ids))
		for _, id := range ids {
			var row departmentRecord
			err := tx.Select("id,revision_id,status").Where("tenant_id=? AND hospital_id=? AND id=?", ref.TenantID, ref.HospitalID, id).Clauses(clause.Locking{Strength: "SHARE"}).Take(&row).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return authz.ErrDepartmentReferenceHidden
			}
			if err != nil {
				return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
			}
			rows[id] = row
		}
		row := rows[ref.DepartmentID]
		if row.Status != "active" || row.RevisionID != ref.DepartmentRevision {
			return authz.ErrDepartmentReferenceChanged
		}
		if hasAncestor {
			ancestor := rows[ref.AncestorID]
			if ancestor.Status != "active" || ancestor.RevisionID != ref.AncestorRevision {
				return authz.ErrDepartmentReferenceChanged
			}
			var within bool
			if err := tx.Raw(departmentAncestryQuery, ref.TenantID, ref.HospitalID, ref.DepartmentID, ref.TenantID, ref.HospitalID, departmentReferenceMaxDepth, ref.AncestorID).Scan(&within).Error; err != nil {
				return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
			}
			if !within {
				return authz.ErrDepartmentReferenceChanged
			}
		}
		return action(ctx)
	})
}

package organization

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type departmentScopeGuard struct{ db *tenantdb.DB }

const departmentPathQuery = departmentAncestryCTE + `SELECT id,parent_id,status FROM ancestry ORDER BY depth`

// UNION discards revisited nodes, so the outer LIMIT can stop the walk early.
const departmentSubtreeQuery = `WITH RECURSIVE subtree(id) AS (
 SELECT id FROM organization_departments
 WHERE tenant_id=? AND hospital_id=? AND id=ANY(?::uuid[]) AND status='active'
 UNION
 SELECT d.id FROM subtree s JOIN organization_departments d
 ON d.tenant_id=? AND d.hospital_id=? AND d.parent_id=s.id AND d.status='active'
)
SELECT id FROM subtree LIMIT ?`

type departmentPathRow struct {
	ID       uuid.UUID
	ParentID *uuid.UUID
	Status   string
}

// DepartmentScopes exposes current department ancestry without sharing a repository.
func (*Module) DepartmentScopes(db *tenantdb.DB) authz.DepartmentScopeGuard {
	return &departmentScopeGuard{db: db}
}

func canonicalTenant(tenantID string) bool {
	tenant, err := uuid.Parse(tenantID)
	return err == nil && tenant != uuid.Nil && tenant.String() == tenantID
}

func (g *departmentScopeGuard) CurrentPath(ctx context.Context, tenantID string, hospitalID, departmentID uuid.UUID) ([]uuid.UUID, error) {
	if g.db == nil || !canonicalTenant(tenantID) || hospitalID == uuid.Nil || departmentID.Version() != 7 {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	var path []uuid.UUID
	err := g.db.WithTenant(ctx, tenantID, func(tx *gorm.DB) error {
		if err := departmentScopeHospital(tx, tenantID, hospitalID); err != nil {
			return err
		}
		rows, err := readDepartmentPath(tx, tenantID, hospitalID, departmentID)
		if err != nil {
			return err
		}
		path = pathIDs(rows)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return path, nil
}

func (g *departmentScopeGuard) CurrentSubtree(ctx context.Context, tenantID string, hospitalID uuid.UUID, roots []uuid.UUID) ([]uuid.UUID, error) {
	if g.db == nil || !canonicalTenant(tenantID) || hospitalID == uuid.Nil || len(roots) > authz.DepartmentScopeRootLimit {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	unique := make([]string, 0, len(roots))
	for _, root := range roots {
		if root.Version() != 7 {
			return nil, authz.ErrScopedAuthorizationUnavailable
		}
		if !slices.Contains(unique, root.String()) {
			unique = append(unique, root.String())
		}
	}
	rootArray := "{" + strings.Join(unique, ",") + "}"
	var ids []uuid.UUID
	err := g.db.WithTenant(ctx, tenantID, func(tx *gorm.DB) error {
		if err := departmentScopeHospital(tx, tenantID, hospitalID); err != nil {
			return err
		}
		if err := tx.Raw(departmentSubtreeQuery, tenantID, hospitalID, rootArray, tenantID, hospitalID, authz.DepartmentSubtreeLimit+1).Scan(&ids).Error; err != nil {
			return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
		}
		// Fail closed: a truncated subtree would silently hide in-scope work.
		if len(ids) > authz.DepartmentSubtreeLimit {
			return authz.ErrDepartmentSubtreeLimit
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	return ids, nil
}

func (g *departmentScopeGuard) WithinCurrentDepartment(ctx context.Context, tenantID string, hospitalID, target, scope uuid.UUID, action func(context.Context) error) error {
	if g.db == nil || action == nil || !canonicalTenant(tenantID) || hospitalID == uuid.Nil || target.Version() != 7 || scope.Version() != 7 || !g.db.GuardedCommandActive(ctx) {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return g.db.WithTenantCommand(ctx, tenantID, func(tx *gorm.DB) error {
		// Shared with the owner's exclusive restructuring gate, through the caller's commit.
		if err := tx.Exec(`SELECT pg_advisory_xact_lock_shared(hashtextextended(?,0))`, tenantID+":organization-departments:"+hospitalID.String()).Error; err != nil {
			return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
		}
		if err := departmentScopeHospital(tx, tenantID, hospitalID); err != nil {
			return err
		}
		hint, err := readDepartmentPath(tx, tenantID, hospitalID, target)
		if err != nil {
			return err
		}
		locked := pathIDs(hint)
		slices.SortFunc(locked, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
		for _, id := range locked {
			var row departmentRecord
			err := tx.Select("id").Where("tenant_id=? AND hospital_id=? AND id=?", tenantID, hospitalID, id).Clauses(clause.Locking{Strength: "SHARE"}).Take(&row).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return authz.ErrDepartmentReferenceChanged
			}
			if err != nil {
				return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
			}
		}
		current, err := readDepartmentPath(tx, tenantID, hospitalID, target)
		if err != nil {
			return err
		}
		if !slices.Equal(pathIDs(current), pathIDs(hint)) || !slices.Contains(pathIDs(current), scope) {
			return authz.ErrDepartmentReferenceChanged
		}
		return action(ctx)
	})
}

func departmentScopeHospital(tx *gorm.DB, tenantID string, hospitalID uuid.UUID) error {
	err := requireDepartmentHospital(tx, tenantID, hospitalID)
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, errConflict) {
		return authz.ErrDepartmentReferenceHidden
	}
	if err != nil {
		return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
	}
	return nil
}

// readDepartmentPath returns the node-to-root path only when every node on it is active.
func readDepartmentPath(tx *gorm.DB, tenantID string, hospitalID, departmentID uuid.UUID) ([]departmentPathRow, error) {
	var rows []departmentPathRow
	if err := tx.Raw(departmentPathQuery, tenantID, hospitalID, departmentID, tenantID, hospitalID, departmentReferenceMaxDepth).Scan(&rows).Error; err != nil {
		return nil, errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
	}
	if len(rows) == 0 || rows[0].ID != departmentID {
		return nil, authz.ErrDepartmentReferenceHidden
	}
	for _, row := range rows {
		if row.Status != "active" {
			return nil, authz.ErrDepartmentReferenceChanged
		}
	}
	// A path that does not reach a root within the persisted depth is an integrity failure.
	if rows[len(rows)-1].ParentID != nil {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	return rows, nil
}

func pathIDs(rows []departmentPathRow) []uuid.UUID {
	ids := make([]uuid.UUID, len(rows))
	for i, row := range rows {
		ids[i] = row.ID
	}
	return ids
}

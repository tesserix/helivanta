package iam

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type assignmentAuthority struct {
	db    *tenantdb.DB
	graph authz.ScopedAssignmentChecker
}

type authorityCandidate struct {
	Snapshot        assignmentSnapshot `gorm:"embedded"`
	MembershipEpoch int64
	ScopeEpoch      int64
}

func (c authorityCandidate) grant() authz.ScopeGrant {
	department := ""
	if c.Snapshot.DepartmentID != nil {
		department = c.Snapshot.DepartmentID.String()
	}
	return authz.ScopeGrant{AssignmentID: c.Snapshot.ID.String(), Version: c.Snapshot.Version, ScopeEpoch: c.ScopeEpoch, MembershipEpoch: c.MembershipEpoch, ScopeKind: c.Snapshot.ScopeKind, DepartmentID: department}
}

// NewScopedAuthorizer builds the IAM-owned current-state authorization provider.
// Products must additionally enforce their own live object/staff/link invariants.
func NewScopedAuthorizer(db *tenantdb.DB, graph authz.ScopedAssignmentChecker) authz.ScopedAuthorizer {
	return &assignmentAuthority{db: db, graph: graph}
}

// authorityCandidates sees department rows, so every caller binds scope_kind itself (ADR-0055).
func authorityCandidates(tx *gorm.DB, r authz.ScopeRequest) *gorm.DB {
	return tx.Table("iam_assignment_snapshots s").Select("s.id,s.version,s.hospital_id,s.subject,s.permissions,s.scope_kind,s.department_id,s.membership_epoch,q.epoch AS scope_epoch").
		Joins("JOIN iam_assignment_proposals p ON p.tenant_id=s.tenant_id AND p.id=s.id AND p.version=s.version+1 AND p.status='active' AND p.membership_epoch=s.membership_epoch").
		Joins("JOIN iam_subject_membership_epochs m ON m.tenant_id=s.tenant_id AND m.subject=s.subject AND m.epoch=s.membership_epoch AND m.member_count>0").
		Joins("JOIN iam_assignment_scope_epochs q ON q.tenant_id=s.tenant_id AND q.hospital_id=s.hospital_id AND q.subject=s.subject AND q.epoch=q.acknowledged_epoch").
		Where("s.tenant_id=? AND s.hospital_id=? AND s.subject=? AND s.valid_from<=clock_timestamp() AND s.expires_at>clock_timestamp() AND s.permissions @> jsonb_build_array(?::text)", r.TenantID, r.HospitalID, r.Subject, string(r.Permission)).Where(`NOT EXISTS(SELECT 1 FROM iam_employment_bases b WHERE b.tenant_id=s.tenant_id AND b.hospital_id=s.hospital_id AND b.subject=s.subject AND (b.valid_until<=clock_timestamp() OR NOT EXISTS(SELECT 1 FROM iam_assignment_employment_bases e WHERE e.tenant_id=s.tenant_id AND e.proposal_id=s.id AND e.generation_id=b.generation_id)))`)
}

// departmentCandidates reads one past the bound so a subject over it fails closed instead of being truncated.
func departmentCandidates(tx *gorm.DB, r authz.ScopeRequest) *gorm.DB {
	return authorityCandidates(tx, r).Where("s.scope_kind='department'").Order("s.id").Limit(authz.MaxDepartmentScopes + 1)
}

// validScopeRequest accepts canonical IDs and a bounded, distinct UUIDv7 path that never names the hospital.
func validScopeRequest(r authz.ScopeRequest) bool {
	for _, value := range []string{r.TenantID, r.HospitalID} {
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil || id.String() != value {
			return false
		}
	}
	if !proposalSubject(r.Subject) || !staffAccessPermissionSupported(r.Permission) || len(r.DepartmentPath) > authz.MaxDepartmentPath {
		return false
	}
	for i, value := range r.DepartmentPath {
		id, err := uuid.Parse(value)
		if err != nil || id.String() != value || id.Version() != 7 || id.Variant() != uuid.RFC4122 || value == r.HospitalID || slices.Contains(r.DepartmentPath[:i], value) {
			return false
		}
	}
	return true
}

// nearestOnPath keeps the first grant per path node, nearest node first, bounding graph checks by the path length.
func nearestOnPath(candidates []authorityCandidate, path []string) []authorityCandidate {
	out := make([]authorityCandidate, 0, len(path))
	for _, node := range path {
		for _, c := range candidates {
			if c.Snapshot.DepartmentID != nil && c.Snapshot.DepartmentID.String() == node {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

func (h *assignmentAuthority) AuthorizeScope(ctx context.Context, r authz.ScopeRequest) (authz.ScopeGrant, error) {
	empty := authz.ScopeGrant{}
	if !validScopeRequest(r) {
		return empty, nil
	}
	if h.db == nil || h.graph == nil {
		return empty, authz.ErrScopedAuthorizationUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var hospitals, departments []authorityCandidate
	err := h.db.WithTenant(ctx, r.TenantID, func(tx *gorm.DB) error {
		if err := departmentAwareAccess(tx); err != nil {
			return err
		}
		if err := authorityCandidates(tx, r).Where("s.scope_kind='hospital'").Order("s.id").Limit(201).Find(&hospitals).Error; err != nil {
			return err
		}
		if len(r.DepartmentPath) == 0 {
			return nil
		}
		return departmentCandidates(tx, r).Find(&departments).Error
	})
	if err != nil || len(hospitals) > 200 || len(departments) > authz.MaxDepartmentScopes {
		return empty, authz.ErrScopedAuthorizationUnavailable
	}
	for _, candidate := range append(hospitals, nearestOnPath(departments, r.DepartmentPath)...) {
		allowed, err := h.graph.CheckScopedAssignment(ctx, candidate.Snapshot.scoped(r.TenantID), r.Subject, r.Permission)
		if err != nil {
			return empty, authz.ErrScopedAuthorizationUnavailable
		}
		if !allowed {
			continue
		}
		var current []authorityCandidate
		err = h.db.WithTenant(ctx, r.TenantID, func(tx *gorm.DB) error {
			if err := departmentAwareAccess(tx); err != nil {
				return err
			}
			if err := authorityCandidates(tx, r).Where("s.id=? AND s.version=? AND s.membership_epoch=? AND q.epoch=?", candidate.Snapshot.ID, candidate.Snapshot.Version, candidate.MembershipEpoch, candidate.ScopeEpoch).Limit(1).Find(&current).Error; err != nil {
				return err
			}
			if len(current) == 0 {
				return nil
			}
			return authorityAudit(tx, r, "allowed", candidate.Snapshot.ID)
		})
		if err != nil {
			return empty, authz.ErrScopedAuthorizationUnavailable
		}
		if len(current) == 1 {
			return candidate.grant(), nil
		}
	}
	err = h.db.WithTenant(ctx, r.TenantID, func(tx *gorm.DB) error { return authorityAudit(tx, r, "denied", uuid.MustParse(r.HospitalID)) })
	if err != nil {
		return empty, authz.ErrScopedAuthorizationUnavailable
	}
	return empty, nil
}

// DepartmentScopes lists current department grants for discovery; a command still selects through AuthorizeScope.
func (h *assignmentAuthority) DepartmentScopes(ctx context.Context, r authz.ScopeRequest) ([]authz.ScopeGrant, error) {
	if !validScopeRequest(r) || len(r.DepartmentPath) != 0 {
		return nil, nil
	}
	if h.db == nil || h.graph == nil {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var candidates []authorityCandidate
	err := h.db.WithTenant(ctx, r.TenantID, func(tx *gorm.DB) error {
		if err := departmentAwareAccess(tx); err != nil {
			return err
		}
		return departmentCandidates(tx, r).Find(&candidates).Error
	})
	if err != nil || len(candidates) > authz.MaxDepartmentScopes {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	checked := make(map[uuid.UUID]authz.ScopeGrant, len(candidates))
	ids := make([]uuid.UUID, 0, len(candidates))
	for _, candidate := range candidates {
		allowed, err := h.graph.CheckScopedAssignment(ctx, candidate.Snapshot.scoped(r.TenantID), r.Subject, r.Permission)
		if err != nil {
			return nil, authz.ErrScopedAuthorizationUnavailable
		}
		if allowed {
			checked[candidate.Snapshot.ID] = candidate.grant()
			ids = append(ids, candidate.Snapshot.ID)
		}
	}
	var grants []authz.ScopeGrant
	err = h.db.WithTenant(ctx, r.TenantID, func(tx *gorm.DB) error {
		if err := departmentAwareAccess(tx); err != nil {
			return err
		}
		var current []authorityCandidate
		if len(ids) > 0 {
			if err := authorityCandidates(tx, r).Where("s.scope_kind='department' AND s.id IN ?", ids).Order("s.id").Find(&current).Error; err != nil {
				return err
			}
		}
		// A grant whose version or either epoch moved after its graph check is dropped, not refreshed.
		for _, c := range current {
			if grant := c.grant(); grant == checked[c.Snapshot.ID] {
				grants = append(grants, grant)
			}
		}
		return authorityAudit(tx, r, "listed", uuid.MustParse(r.HospitalID))
	})
	if err != nil {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	return grants, nil
}

func authorityAudit(tx *gorm.DB, r authz.ScopeRequest, outcome string, id uuid.UUID) error {
	resource := "iam_assignment_scope"
	if outcome == "allowed" {
		resource = "iam_assignment_snapshots"
	}
	return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,?,?,?)`, r.TenantID, r.Subject, strings.Join([]string{"scope", outcome, string(r.Permission)}, ":"), resource, id).Error
}

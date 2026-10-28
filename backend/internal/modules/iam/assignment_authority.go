package iam

import (
	"context"
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

// NewScopedAuthorizer builds the IAM-owned current-state authorization provider.
// Products must additionally enforce their own live object/staff/link invariants.
func NewScopedAuthorizer(db *tenantdb.DB, graph authz.ScopedAssignmentChecker) authz.ScopedAuthorizer {
	return &assignmentAuthority{db: db, graph: graph}
}

func authorityCandidates(tx *gorm.DB, r authz.ScopeRequest) *gorm.DB {
	return tx.Table("iam_assignment_snapshots s").Select("s.id,s.version,s.hospital_id,s.subject,s.permissions,s.membership_epoch,q.epoch AS scope_epoch").
		Joins("JOIN iam_assignment_proposals p ON p.tenant_id=s.tenant_id AND p.id=s.id AND p.version=s.version+1 AND p.status='active' AND p.membership_epoch=s.membership_epoch").
		Joins("JOIN iam_subject_membership_epochs m ON m.tenant_id=s.tenant_id AND m.subject=s.subject AND m.epoch=s.membership_epoch AND m.member_count>0").
		Joins("JOIN iam_assignment_scope_epochs q ON q.tenant_id=s.tenant_id AND q.hospital_id=s.hospital_id AND q.subject=s.subject AND q.epoch=q.acknowledged_epoch").
		Where("s.tenant_id=? AND s.hospital_id=? AND s.subject=? AND s.valid_from<=clock_timestamp() AND s.expires_at>clock_timestamp() AND s.permissions @> jsonb_build_array(?::text)", r.TenantID, r.HospitalID, r.Subject, string(r.Permission))
}

func (h *assignmentAuthority) AuthorizeScope(ctx context.Context, r authz.ScopeRequest) (authz.ScopeGrant, error) {
	empty := authz.ScopeGrant{}
	for _, value := range []string{r.TenantID, r.HospitalID} {
		id, err := uuid.Parse(value)
		if err != nil || id == uuid.Nil || id.String() != value {
			return empty, nil
		}
	}
	if !proposalSubject(r.Subject) || !staffAccessPermissionSupported(r.Permission) {
		return empty, nil
	}
	if h.db == nil || h.graph == nil {
		return empty, authz.ErrScopedAuthorizationUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var candidates []authorityCandidate
	err := h.db.WithTenant(ctx, r.TenantID, func(tx *gorm.DB) error {
		if err := assignmentAccess(tx); err != nil {
			return err
		}
		return authorityCandidates(tx, r).Order("s.id").Limit(201).Find(&candidates).Error
	})
	if err != nil || len(candidates) > 200 {
		return empty, authz.ErrScopedAuthorizationUnavailable
	}
	for _, candidate := range candidates {
		snapshot := authz.ScopedAssignment{TenantID: r.TenantID, HospitalID: r.HospitalID, ScopeKind: "hospital", ScopeID: r.HospitalID, ID: candidate.Snapshot.ID.String(), Version: candidate.Snapshot.Version, Subject: r.Subject, Permissions: candidate.Snapshot.Permissions}
		allowed, err := h.graph.CheckScopedAssignment(ctx, snapshot, r.Subject, r.Permission)
		if err != nil {
			return empty, authz.ErrScopedAuthorizationUnavailable
		}
		if !allowed {
			continue
		}
		var current []authorityCandidate
		err = h.db.WithTenant(ctx, r.TenantID, func(tx *gorm.DB) error {
			if err := assignmentAccess(tx); err != nil {
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
			return authz.ScopeGrant{AssignmentID: candidate.Snapshot.ID.String(), Version: candidate.Snapshot.Version, ScopeEpoch: candidate.ScopeEpoch, MembershipEpoch: candidate.MembershipEpoch}, nil
		}
	}
	err = h.db.WithTenant(ctx, r.TenantID, func(tx *gorm.DB) error { return authorityAudit(tx, r, "denied", uuid.MustParse(r.HospitalID)) })
	if err != nil {
		return empty, authz.ErrScopedAuthorizationUnavailable
	}
	return empty, nil
}

func authorityAudit(tx *gorm.DB, r authz.ScopeRequest, outcome string, id uuid.UUID) error {
	resource := "iam_assignment_scope"
	if outcome == "allowed" {
		resource = "iam_assignment_snapshots"
	}
	return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,?,?,?)`, r.TenantID, r.Subject, strings.Join([]string{"scope", outcome, string(r.Permission)}, ":"), resource, id).Error
}

package iam

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type governanceReviewAuthority struct {
	Kind                          string
	ExpiresAt                     time.Time
	GrantID                       *uuid.UUID
	GrantVersion, MembershipEpoch int64
}

type governanceReviewSource struct {
	ID                       uuid.UUID
	IssuedBy, Status         string
	Version, MembershipEpoch int64
	ExpiresAt                time.Time
}

func (h *governanceHandlers) withReviewer(ctx context.Context, p authn.Principal, parent uuid.UUID, writing bool, permission authz.Permission, action func(context.Context, *gorm.DB, []string, governanceReviewAuthority) error) error {
	if permission != PermGovernanceRead && permission != PermGovernanceReview || parent == uuid.Nil || action == nil {
		return errGovernanceDenied
	}
	return h.withActor(ctx, p, func(commandCtx context.Context, tx *gorm.DB, owners []string) error {
		if slices.Contains(owners, p.Subject) {
			if err := tx.Exec(`SELECT set_config('app.iam_governance_access','owner',true)`).Error; err != nil {
				return err
			}
			if err := lockGovernanceRows(tx, p.TenantID, []uuid.UUID{parent}, writing); err != nil {
				return err
			}
			epoch, err := proposalMembershipEpoch(tx, p.TenantID, p.Subject)
			if err != nil {
				return err
			}
			return action(commandCtx, tx, owners, governanceReviewAuthority{Kind: "owner", MembershipEpoch: epoch})
		}
		return h.withCurrentGovernanceSource(commandCtx, tx, p, owners, "iam.governance_auditor.v1", authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: "tenant"}, []uuid.UUID{parent}, writing, func(heldCtx context.Context, tx *gorm.DB, owners []string, authority governanceReviewAuthority) error {
			if err := tx.Exec(`SELECT set_config('app.iam_governance_access','audit',true)`).Error; err != nil {
				return err
			}
			authority.Kind = "auditor"
			return action(heldCtx, tx, owners, authority)
		})
	})
}

func (h *governanceHandlers) withCurrentGovernanceSource(commandCtx context.Context, tx *gorm.DB, p authn.Principal, owners []string, template string, scope authz.GovernanceEstablishmentScope, targets []uuid.UUID, writing bool, action func(context.Context, *gorm.DB, []string, governanceReviewAuthority) error) error {
	if h.topology == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if len(owners) == 0 {
		return errGovernanceDenied
	}
	if err := tx.Exec(`SELECT set_config('app.iam_governance_access','source',true)`).Error; err != nil {
		return err
	}
	var candidates []uuid.UUID
	if err := tx.Table("iam_governance_grants g").Select("g.id").Joins("JOIN iam_subject_membership_epochs e ON e.tenant_id=g.tenant_id AND e.subject=g.subject AND e.epoch=g.membership_epoch AND e.member_count>0").Where("g.tenant_id=? AND g.subject=? AND g.template_key=? AND g.scope_kind=? AND g.scope_id IS NOT DISTINCT FROM ?::uuid AND g.status IN('active','provisional_active') AND g.valid_from<=clock_timestamp() AND g.expires_at>clock_timestamp()", p.TenantID, p.Subject, template, scope.Kind, scope.ID).Order("g.id").Limit(33).Pluck("g.id", &candidates).Error; err != nil {
		return err
	}
	if len(candidates) == 0 {
		return errGovernanceDenied
	}
	if len(candidates) > 32 {
		return authz.ErrScopedAuthorizationUnavailable
	}
	ids := append(append([]uuid.UUID(nil), targets...), candidates...)
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	ids = slices.Compact(ids)
	values := make([]string, len(ids))
	for i, id := range ids {
		values[i] = id.String()
	}
	if err := tx.Exec(`SELECT set_config('app.iam_governance_access','lock',true),set_config('app.iam_governance_lock_ids',?,true)`, strings.Join(values, ",")).Error; err != nil {
		return err
	}
	if err := lockGovernanceRows(tx, p.TenantID, ids, writing); err != nil {
		return err
	}
	// Grant locks precede membership locks so revocation can delete its routing row safely.
	var members []member
	if err := tx.Select("id,role_key").Where("tenant_id=? AND subject=?", p.TenantID, p.Subject).Order("role_key").Limit(len(authz.SystemRoles()) + 1).Clauses(clause.Locking{Strength: "SHARE"}).Find(&members).Error; err != nil {
		return err
	}
	if len(members) > len(authz.SystemRoles()) {
		return authz.ErrScopedAuthorizationUnavailable
	}
	managed := false
	for _, m := range members {
		managed = managed || m.RoleKey == string(authz.RoleGovernanceDelegate)
	}
	if !managed {
		return errGovernanceDenied
	}
	epoch, err := proposalMembershipEpoch(tx, p.TenantID, p.Subject)
	if err != nil {
		return err
	}
	var sources []governanceReviewSource
	if err := tx.Table("iam_governance_grants").Select("id,issued_by,status,version,membership_epoch,expires_at").Where("tenant_id=? AND subject=? AND id IN ? AND membership_epoch=? AND status IN('active','provisional_active') AND valid_from<=clock_timestamp() AND expires_at>clock_timestamp()", p.TenantID, p.Subject, candidates, epoch).Order("id").Find(&sources).Error; err != nil {
		return err
	}
	for _, source := range sources {
		if source.Status == "provisional_active" && (len(owners) != 1 || owners[0] != source.IssuedBy) {
			continue
		}
		if source.Status == "active" {
			var approved bool
			if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM iam_governance_decisions WHERE tenant_id=? AND grant_id=? AND decision='approve' AND result_version<=?)`, p.TenantID, source.ID, source.Version).Scan(&approved).Error; err != nil {
				return err
			}
			if !approved {
				continue
			}
		}
		if err := h.db.ConstrainGuardedCommand(commandCtx, p.TenantID, source.ExpiresAt); err != nil {
			return err
		}
		return h.topology.WithGovernanceTopology(commandCtx, scope, func(heldCtx context.Context, ref authz.GovernanceTopologyReference) error {
			if ref.HospitalCount > 1 && len(owners) < 2 {
				return errGovernanceDenied
			}
			return action(heldCtx, tx, owners, governanceReviewAuthority{Kind: "source", GrantID: &source.ID, GrantVersion: source.Version, MembershipEpoch: epoch, ExpiresAt: source.ExpiresAt})
		})
	}
	return errGovernanceDenied
}

func lockGovernanceRows(tx *gorm.DB, tenant string, ids []uuid.UUID, writing bool) error {
	strength := "SHARE"
	if writing {
		strength = "UPDATE"
	}
	var rows []struct{ ID uuid.UUID }
	err := tx.Table("iam_governance_grants").Select("id").Where("tenant_id=? AND id IN ?", tenant, ids).Order("id").Clauses(clause.Locking{Strength: strength}).Find(&rows).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errGovernanceDenied
	}
	return err
}

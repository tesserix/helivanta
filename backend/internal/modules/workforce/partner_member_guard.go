package workforce

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type partnerMemberGuard struct {
	db      *tenantdb.DB
	sources authz.CurrentStaffPostingSourceGuard
	actors  authz.CurrentStaffActorGuard
}

func (*Module) PartnerMembers(db *tenantdb.DB, actors authz.CurrentStaffActorGuard, sources authz.CurrentStaffPostingSourceGuard) authz.CurrentPartnerMemberGuard {
	return &partnerMemberGuard{db: db, actors: actors, sources: sources}
}
func (g *partnerMemberGuard) WithCurrentPartnerActor(ctx context.Context, scope authz.StaffActorScope, action func(context.Context, authz.StaffActorReference) error) error {
	if g.db == nil || g.actors == nil || action == nil || (scope.HospitalID == uuid.Nil || !validLoginSubject(scope.Subject)) || !g.db.GuardedCommandActive(ctx) {
		return authz.ErrScopedAuthorizationUnavailable
	}
	request := authz.ScopeRequest{TenantID: scope.TenantID, Subject: scope.Subject, HospitalID: scope.HospitalID.String(), Permission: authz.PartnerMemberSourceUse}
	grant, ok := authz.ScopedCommandGrant(ctx, request)
	if !ok || grant.AssignmentID == "" || grant.ScopeKind != "hospital" {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return g.db.WithTenantCommand(ctx, scope.TenantID, func(tx *gorm.DB) error {
		// Match posting-source ordering before retaining actor-link locks or entering another owner.
		if err := tx.Exec(`SELECT workforce_lock_supervision(?::uuid,?::uuid)`, scope.TenantID, scope.HospitalID).Error; err != nil {
			return err
		}
		return g.actors.WithCurrentStaffActor(ctx, scope, action)
	})
}
func (g *partnerMemberGuard) WithCurrentPartnerMember(ctx context.Context, scope authz.PartnerMemberScope, action func(context.Context, authz.PartnerMemberReference) error) error {
	if g.db == nil || g.sources == nil || action == nil || !validPostingSourceScope(scope.Posting) || !validLoginSubject(scope.MemberSubject) || !g.db.GuardedCommandActive(ctx) {
		return authz.ErrScopedAuthorizationUnavailable
	}
	request := authz.ScopeRequest{TenantID: scope.Posting.TenantID, Subject: scope.Posting.Subject, HospitalID: scope.Posting.HospitalID.String(), DepartmentPath: scope.Posting.DepartmentPath, Permission: authz.PartnerMemberSourceUse}
	grant, ok := authz.ScopedCommandGrant(ctx, request)
	if !ok || grant.AssignmentID == "" || grant.ScopeKind != "hospital" {
		return authz.ErrScopedAuthorizationUnavailable
	}
	invoked := false
	err := g.sources.WithCurrentStaffPosting(ctx, scope.Posting, func(current context.Context, source authz.StaffPostingSourceReference) error {
		return g.db.WithTenantCommand(current, scope.Posting.TenantID, func(tx *gorm.DB) (err error) {
			restore, err := savePostingSourceFrame(tx, []string{"app.workforce_subject", "app.workforce_hospital", "app.workforce_leave_actor_check", "app.workforce_leave_review_actor"})
			if err != nil {
				return err
			}
			restored := false
			defer func() {
				if !restored {
					err = errors.Join(err, restore())
				}
			}()
			// This is a purpose-scoped fact selector; request identity and the audit actor remain unchanged.
			peerIdentity := authn.Principal{TenantID: scope.Posting.TenantID, Subject: scope.MemberSubject}
			if err := tx.Exec(`SELECT set_config('app.workforce_subject',?,true),set_config('app.workforce_hospital',?,true)`, scope.MemberSubject, scope.Posting.HospitalID.String()).Error; err != nil {
				return err
			}
			peer, err := lockStaffActorLink(tx, peerIdentity, scope.Posting.HospitalID)
			if err != nil {
				return err
			}
			if peer.StaffID != source.StaffID || peer.GenerationID != source.GenerationID || peer.GenerationID == uuid.Nil {
				return authz.ErrScopedCommandDenied
			}
			generation, until, err := currentDigitalActorGeneration(tx, peerIdentity, scope.Posting.HospitalID, peer)
			if err != nil {
				return err
			}
			if err := g.db.ConstrainGuardedCommand(current, scope.Posting.TenantID, until); err != nil {
				return err
			}
			var displayName string
			if err := tx.Table("workforce_staff").Select("display_name").Where("tenant_id=? AND hospital_id=? AND id=?", scope.Posting.TenantID, scope.Posting.HospitalID, peer.StaffID).Scan(&displayName).Error; err != nil {
				return err
			}
			if displayName == "" {
				return authz.ErrScopedAuthorizationUnavailable
			}
			id, err := uuid.NewV7()
			if err != nil {
				return err
			}
			if source.SourceUntil.Before(until) {
				until = source.SourceUntil
			}
			reference := authz.PartnerMemberReference{UseID: id, Subject: scope.MemberSubject, DisplayName: displayName, Member: authz.StaffActorReference{StaffID: peer.StaffID, LinkID: peer.ID, GenerationID: generation.ID, LinkVersion: peer.Version, EmploymentEpoch: generation.EmploymentEpoch}, Source: source, CheckedAt: time.Now().UTC(), SourceUntil: until}
			data, err := json.Marshal(map[string]any{"permission": string(authz.PartnerMemberSourceUse), "assignment_id": grant.AssignmentID, "assignment_version": grant.Version, "scope_epoch": grant.ScopeEpoch, "membership_epoch": grant.MembershipEpoch, "actor_staff_id": source.Actor.StaffID, "actor_link_id": source.Actor.LinkID, "actor_generation_id": source.Actor.GenerationID, "posting_source_use_id": source.UseID, "posting_source_hash": source.SourceHash, "member_staff_id": peer.StaffID, "member_link_id": peer.ID, "member_link_version": peer.Version, "member_generation_id": generation.ID, "member_employment_epoch": generation.EmploymentEpoch, "checked_at": reference.CheckedAt, "source_until": reference.SourceUntil})
			if err != nil {
				return err
			}
			if err = restore(); err != nil {
				return err
			}
			restored = true
			if err = tx.Exec(`INSERT INTO workforce_audit(id,tenant_id,hospital_id,actor_subject,action,resource_id,changes)VALUES(?,?,?,?,'workforce.partner_member_source.use',?,?::jsonb)`, id, scope.Posting.TenantID, scope.Posting.HospitalID, scope.Posting.Subject, peer.StaffID, string(data)).Error; err != nil {
				return err
			}
			invoked = true
			return action(current, reference)
		})
	})
	if invoked || err == nil {
		return err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, authz.ErrScopedCommandDenied) || errors.Is(err, tenantdb.ErrGuardedCommandExpired) {
		return authz.ErrScopedCommandDenied
	}
	return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
}

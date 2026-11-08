package workforce

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

//go:embed partner_member_candidates.sql
var partnerMemberCandidatesSchema string

func (g *partnerMemberGuard) WithPartnerMemberCandidates(ctx context.Context, scope authz.StaffActorScope, page authz.PartnerMemberCandidatePage, action func(context.Context, []authz.PartnerMemberCandidate) error) error {
	if page.Limit < 1 || page.Limit > 50 || action == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return g.WithCurrentPartnerActor(ctx, scope, func(current context.Context, actor authz.StaffActorReference) error {
		grant, ok := authz.ScopedCommandGrant(current, authz.ScopeRequest{TenantID: scope.TenantID, Subject: scope.Subject, HospitalID: scope.HospitalID.String(), Permission: authz.PartnerMemberSourceUse})
		if !ok || grant.ScopeKind != "hospital" {
			return authz.ErrScopedAuthorizationUnavailable
		}
		return g.db.WithTenantCommand(current, scope.TenantID, func(tx *gorm.DB) (err error) {
			keys := []string{"app.workforce_partner_candidates", "app.workforce_partner_hospital", "app.workforce_partner_subject", "app.workforce_partner_actor", "app.workforce_partner_link", "app.workforce_partner_generation", "app.workforce_partner_assignment", "app.workforce_partner_assignment_version"}
			restore, err := savePostingSourceFrame(tx, keys)
			if err != nil {
				return err
			}
			restored := false
			defer func() {
				if !restored {
					err = errors.Join(err, restore())
				}
			}()
			if err = tx.Exec(`SELECT set_config('app.workforce_partner_candidates','read',true),set_config('app.workforce_partner_hospital',?,true),set_config('app.workforce_partner_subject',?,true),set_config('app.workforce_partner_actor',?,true),set_config('app.workforce_partner_link',?,true),set_config('app.workforce_partner_generation',?,true),set_config('app.workforce_partner_assignment',?,true),set_config('app.workforce_partner_assignment_version',?,true)`, scope.HospitalID.String(), scope.Subject, actor.StaffID.String(), actor.LinkID.String(), actor.GenerationID.String(), grant.AssignmentID, fmt.Sprint(grant.Version)).Error; err != nil {
				return err
			}
			rows := []authz.PartnerMemberCandidate{}
			query := tx.Table("workforce_posting_proposals p").Joins("JOIN workforce_posting_proposal_revisions r ON (r.tenant_id,r.hospital_id,r.staff_id,r.proposal_id,r.id,r.version)=(p.tenant_id,p.hospital_id,p.staff_id,p.id,p.revision_id,p.version)").Joins("JOIN workforce_staff s ON (s.tenant_id,s.hospital_id,s.id)=(p.tenant_id,p.hospital_id,p.staff_id)").Joins("JOIN workforce_login_links l ON (l.tenant_id,l.hospital_id,l.staff_id,l.employment_generation_id)=(r.tenant_id,r.hospital_id,r.staff_id,r.generation_id) AND l.status='active'").Select("p.id,p.hospital_id,p.staff_id,p.revision_id AS posting_revision_id,p.version AS posting_version,r.department_id,r.department_revision_id,r.starts_at,LEAST(COALESCE(r.ends_at,r.eligibility_until),r.eligibility_until)AS nominal_until,p.created_at,l.subject,s.display_name").Where("p.tenant_id=? AND p.hospital_id=? AND p.status='approved' AND s.employment_status='active' AND s.id<>? AND l.subject<>? AND r.starts_at<=clock_timestamp() AND r.eligibility_until>clock_timestamp() AND (r.ends_at IS NULL OR r.ends_at>clock_timestamp())", scope.TenantID, scope.HospitalID, actor.StaffID, scope.Subject).Order("p.created_at DESC,p.id DESC").Limit(page.Limit + 1)
			if !page.AfterCreatedAt.IsZero() {
				query = query.Where("(p.created_at,p.id)<(?,?)", page.AfterCreatedAt, page.AfterID)
			}
			if err = query.Find(&rows).Error; err != nil {
				return err
			}
			if err = restore(); err != nil {
				return err
			}
			restored = true
			changes, err := json.Marshal(map[string]any{"rows": min(len(rows), page.Limit), "assignment_id": grant.AssignmentID, "assignment_version": grant.Version, "actor_staff_id": actor.StaffID})
			if err != nil {
				return err
			}
			if err = tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes)VALUES(?,?,?,'workforce.partner_member_candidates.read',?,?::jsonb)`, scope.TenantID, scope.HospitalID, scope.Subject, scope.HospitalID, string(changes)).Error; err != nil {
				return err
			}
			return action(current, rows)
		})
	})
}

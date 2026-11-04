package workforce

import (
	"encoding/json"
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

func prepareLeaveReview(tx *gorm.DB, c *gin.Context, p authn.Principal, hospital, staffID, requestID uuid.UUID, decides bool) ([]byte, error) {
	permissions, _ := authz.PermissionsFrom(c)
	legacy := false
	if permissions.Has(PermLeaveApprove) || !decides {
		err := lockHospitalManager(tx, p, hospital, "app.workforce_leave_ledger", leaveLedgerLock)
		if err == nil {
			legacy = true
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	grant, scoped := authz.HospitalScopeGrant(c, PermLeaveApprove, hospital.String())
	if decides && scoped {
		evidence, err := prepareScopedLeaveReview(tx, p, hospital, staffID, requestID, grant)
		if err == nil {
			return evidence, nil
		}
		if !legacy || !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	if legacy && decides {
		return deskRouteEvidence(tx, p, hospital, staffID, requestID)
	}
	if legacy {
		return []byte("{}"), nil
	}
	if authz.HospitalScopeUnavailable(c) {
		return nil, authz.ErrScopedAuthorizationUnavailable
	}
	return nil, gorm.ErrRecordNotFound
}

type reviewedLeaveActor struct {
	ID           uuid.UUID
	StaffID      uuid.UUID
	Version      int64
	GenerationID uuid.UUID
}

func lockReviewedLeaveActor(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) (reviewedLeaveActor, error) {
	actor, err := lockStaffActorLink(tx, p, hospital)
	if err != nil {
		return actor, err
	}
	return actor, validateGenerationActor(tx, p, hospital, actor)
}

func lockStaffActorLink(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) (reviewedLeaveActor, error) {
	var first, current reviewedLeaveActor
	if err := tx.Exec(`SELECT set_config('app.workforce_leave_actor_check','authorized',true)`).Error; err != nil {
		return current, err
	}
	query := func() *gorm.DB {
		return tx.Table("workforce_login_links").Select("id,staff_id,version,COALESCE(to_jsonb(workforce_login_links)->>'employment_generation_id','00000000-0000-0000-0000-000000000000')::uuid AS generation_id").Where("tenant_id=? AND hospital_id=? AND subject=? AND status='active'", p.TenantID, hospital, p.Subject)
	}
	if err := query().Take(&first).Error; err != nil {
		return current, err
	}
	if err := tx.Exec(`SELECT set_config('app.workforce_leave_review_actor',?,true)`, first.StaffID.String()).Error; err != nil {
		return current, err
	}
	var actor struct {
		ID               uuid.UUID
		EmploymentStatus string
	}
	if err := tx.Table("workforce_staff").Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, first.StaffID).Take(&actor).Error; err != nil {
		return current, err
	}
	if actor.EmploymentStatus != "active" {
		return current, gorm.ErrRecordNotFound
	}
	if err := query().Clauses(clause.Locking{Strength: "SHARE"}).Take(&current).Error; err != nil {
		return current, err
	}
	if current != first {
		return current, gorm.ErrRecordNotFound
	}
	return current, nil
}

func prepareScopedLeaveReview(tx *gorm.DB, p authn.Principal, hospital, staffID, requestID uuid.UUID, grant authz.ScopeGrant) ([]byte, error) {
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, p.TenantID+":"+hospital.String()+":"+leaveLedgerLock).Error; err != nil {
		return nil, err
	}
	actor, err := lockReviewedLeaveActor(tx, p, hospital)
	if err != nil {
		return nil, err
	}
	if actor.StaffID == staffID {
		return nil, errLeaveSelfDecision
	}
	var ledger string
	if err := tx.Raw(`SELECT COALESCE(current_setting('app.workforce_leave_ledger',true),'')`).Scan(&ledger).Error; err != nil {
		return nil, err
	}
	if err := tx.Exec(`SELECT set_config('app.workforce_leave_review_staff',?,true),set_config('app.workforce_leave_review_request',?,true),set_config('app.workforce_leave_review','authorized',true),set_config('app.workforce_leave_ledger','authorized',true)`, staffID.String(), requestID.String()).Error; err != nil {
		return nil, err
	}
	// Out of route: clear the scoped flags so a desk manager falls back to the desk path.
	var route struct {
		Routed         bool
		RoutingVersion *int
		RouteLevel     *int
	}
	found := tx.Raw(`SELECT r.routed,r.routing_version,(SELECT s.level::int FROM workforce_leave_route_steps s WHERE (s.tenant_id,s.hospital_id,s.request_id)=(r.tenant_id,r.hospital_id,r.id) AND s.staff_id=? AND s.opens_at<=now()) AS route_level
 FROM workforce_leave_requests r WHERE r.tenant_id=? AND r.hospital_id=? AND r.id=? AND r.staff_id=?`, actor.StaffID, p.TenantID, hospital, requestID, staffID).Scan(&route)
	if found.Error != nil {
		return nil, found.Error
	}
	if found.RowsAffected == 0 || route.Routed && route.RouteLevel == nil {
		if err := tx.Exec(`SELECT set_config('app.workforce_leave_review','',true),set_config('app.workforce_leave_review_staff','',true),set_config('app.workforce_leave_review_request','',true),set_config('app.workforce_leave_ledger',?,true)`, ledger).Error; err != nil {
			return nil, err
		}
		return nil, gorm.ErrRecordNotFound
	}
	evidence := map[string]any{"assignment_id": grant.AssignmentID, "assignment_version": grant.Version, "scope_epoch": grant.ScopeEpoch, "membership_epoch": grant.MembershipEpoch, "actor_staff_id": actor.StaffID, "actor_link_id": actor.ID, "actor_link_version": actor.Version}
	if route.Routed {
		evidence["route_level"], evidence["routing_version"] = *route.RouteLevel, *route.RoutingVersion
	}
	return json.Marshal(evidence)
}

func reviewedLeaveStanding(tx *gorm.DB, c *gin.Context, p authn.Principal, r leaveRequestRow) (bool, error) {
	if _, ok := authz.HospitalScopeGrant(c, PermLeaveApprove, r.HospitalID.String()); !ok {
		return false, nil
	}
	var allowed bool
	err := tx.Raw(`SELECT COALESCE(workforce_leave_review_context(?::uuid,?::uuid),false)
 AND NULLIF(current_setting('app.workforce_leave_review_staff',true),'')::uuid=?::uuid
 AND NULLIF(current_setting('app.workforce_leave_review_request',true),'')::uuid=?::uuid`, p.TenantID, r.HospitalID, r.StaffID, r.ID).Scan(&allowed).Error
	return allowed, err
}

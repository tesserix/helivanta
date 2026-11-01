package workforce

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

const employmentActivationQuery = "employment_activation"

func (h *handlers) activationAvailable(c *gin.Context) bool {
	if h.employmentActivationEnabled {
		return true
	}
	if _, _, ok := authn.TenantPrincipal(c); ok {
		c.Header("Cache-Control", "no-store")
		respond.Error(c, 503, "employment_activation_unavailable", "Employment activation is not available. Complete the required independent reviews before continuing.")
	}
	return false
}

func (h *handlers) employmentActivationTransaction(c *gin.Context, p authn.Principal, hospital, staff, review uuid.UUID, fn func(*gorm.DB, IdentitySource, *intakeAuthority) error) error {
	guard, _ := h.scopeAuthority.(authz.CompoundScopedCommandGuard)
	return h.employmentActivationWithGuard(c, p, hospital, staff, review, guard, fn)
}

func (h *handlers) employmentActivationWithGuard(c *gin.Context, p authn.Principal, hospital, staff, review uuid.UUID, guard authz.CompoundScopedCommandGuard, fn func(*gorm.DB, IdentitySource, *intakeAuthority) error) error {
	query := c.Request.URL.Query()
	if len(query) != 1 || len(query[employmentActivationQuery]) != 1 || query.Get(employmentActivationQuery) != "v1" {
		return errIntakeProjection
	}
	tenants := c.Request.Header.Values("X-Helivanta-Expected-Tenant")
	subjects := c.Request.Header.Values("X-Helivanta-Expected-Subject")
	if len(tenants) != 1 || tenants[0] != p.TenantID || len(subjects) != 1 || subjects[0] != p.Subject {
		return errIntakeWorkspaceRequired
	}
	return h.intakeAuthorityWithGuard(c, p, hospital, []authz.Permission{PermEmploymentActivate, PermEnrollmentManage}, true, guard, func(tx *gorm.DB, scoped bool) error {
		value, present := c.Get(intakeAuthorityKey)
		authority, ok := value.(*intakeAuthority)
		if !scoped || !present || !ok || authority == nil || !authority.Scoped {
			return authz.ErrScopedCommandDenied
		}
		source, err := loadIdentitySource(tx, p, hospital, staff)
		if err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.workforce_employment_review','manage',true),set_config('app.workforce_employment_review_id',?,true),set_config('app.workforce_employment_evidence','read',true)`, review.String()).Error; err != nil {
			return err
		}
		return fn(tx, source, authority)
	})
}

type activationCommandGuard struct {
	owner  authz.EmploymentActivationGuard
	writer authz.EmploymentBasisWriter
	ctx    context.Context
}

func (g *activationCommandGuard) WithScopeGrants(ctx context.Context, checks []authz.ScopeCheck, authTime time.Time, action func(context.Context) error) error {
	return g.owner.WithEmploymentActivation(ctx, checks, authTime, func(commandCtx context.Context, writer authz.EmploymentBasisWriter) error {
		g.writer, g.ctx = writer, commandCtx
		defer func() { g.writer, g.ctx = nil, nil }()
		return action(commandCtx)
	})
}

func employmentActivationSource(tx *gorm.DB, p authn.Principal, source IdentitySource, row employmentReviewRecord, actor intakeActorReference) (EmploymentActivationSource, error) {
	review := row.EmploymentReview
	result := EmploymentActivationSource{
		HospitalID: source.HospitalID, StaffID: source.StaffID,
		ReviewID: review.ID, ReviewRevisionID: review.RevisionID, ReviewVersion: review.Version,
		StaffVersion: source.StaffVersion, EmploymentEpoch: source.EmploymentEpoch,
		StartsAt: review.Terms.StartsAt, EndsAt: review.Terms.EndsAt,
		CurrentState: "not_approved",
	}
	if review.Status != "approved" {
		return result, nil
	}
	if source.EmploymentStatus == "active" {
		if err := generationContext(tx, source.StaffID, "read"); err != nil {
			return result, err
		}
		var prior employmentGenerationRecord
		err := tx.Table("workforce_employment_generations").Select(employmentGenerationColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND review_id=? AND review_version=? AND review_revision_id=? AND employment_epoch=?", p.TenantID, source.HospitalID, source.StaffID, review.ID, review.Version, review.RevisionID, source.EmploymentEpoch).Take(&prior).Error
		if err == nil {
			state, _, err := employmentGenerationState(tx, p, source, prior)
			if err != nil {
				return result, err
			}
			if state == "effective" {
				state = "consumed"
			}
			result.CurrentState = state
			return result, nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return result, err
		}
	}
	if row.IdentityEpoch == nil {
		result.CurrentState = "identity_unavailable"
		return result, nil
	}
	if err := tx.Exec(`SELECT set_config('app.workforce_generation','read',true),set_config('app.workforce_generation_staff',?,true)`, source.StaffID.String()).Error; err != nil {
		return result, err
	}
	var basis struct{ Epoch int64 }
	if err := tx.Raw(`SELECT epoch FROM workforce_identity_epochs WHERE tenant_id=? AND hospital_id=? AND staff_id=? FOR SHARE`, p.TenantID, source.HospitalID, source.StaffID).Scan(&basis).Error; err != nil {
		return result, err
	}
	if basis.Epoch == 0 {
		result.CurrentState = "identity_unavailable"
		return result, nil
	}
	if basis.Epoch != *row.IdentityEpoch {
		result.CurrentState = "identity_changed"
		return result, nil
	}
	state, err := employmentReviewState(tx, p, source, review)
	if err != nil {
		return result, err
	}
	if state != "approved" {
		result.CurrentState = state
		return result, nil
	}
	state, err = employmentPersonIdentityBasis(tx, p, source, review, basis.Epoch)
	if err != nil {
		return result, err
	}
	if state != "approved" {
		result.CurrentState = state
		return result, nil
	}
	if source.EmploymentStatus != "inactive" {
		result.CurrentState = "already_active"
		return result, nil
	}
	var associated bool
	if actor.Staff == source.StaffID || review.DeclaredSubject == p.Subject {
		associated = true
	} else if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM workforce_login_links WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND subject=?)`, p.TenantID, source.HospitalID, source.StaffID, p.Subject).Scan(&associated).Error; err != nil {
		return result, err
	}
	if associated {
		result.CurrentState = "associated_actor"
		return result, nil
	}
	var now time.Time
	if err := tx.Raw("SELECT clock_timestamp()").Scan(&now).Error; err != nil {
		return result, err
	}
	if now.Before(result.StartsAt) {
		result.CurrentState = "not_started"
		return result, nil
	}
	if result.EndsAt != nil && !now.Before(*result.EndsAt) {
		result.CurrentState = "expired"
		return result, nil
	}
	result.CurrentState, result.CanActivate = "eligible", true
	return result, nil
}

func employmentPersonIdentityBasis(tx *gorm.DB, p authn.Principal, source IdentitySource, review EmploymentReview, epoch int64) (string, error) {
	if err := tx.Exec(`SELECT set_config('app.workforce_employment_person_review',?,true)`, review.PersonReviewID.String()).Error; err != nil {
		return "", err
	}
	var finding struct{ Epoch *int64 }
	err := tx.Table("workforce_person_reviews r").Select("(to_jsonb(r)->>'identity_epoch')::bigint AS epoch").Where("r.tenant_id=? AND r.hospital_id=? AND r.staff_id=? AND r.id=? AND r.version=? AND r.revision_id=? AND r.status='approved'", p.TenantID, source.HospitalID, source.StaffID, review.PersonReviewID, review.PersonReviewVersion, review.PersonReviewRevisionID).Take(&finding).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "person_unavailable", nil
	}
	if err != nil {
		return "", err
	}
	if finding.Epoch == nil {
		return "identity_unavailable", nil
	}
	if *finding.Epoch != epoch {
		return "identity_changed", nil
	}
	return "approved", nil
}

func (h *handlers) getEmploymentActivationSource(c *gin.Context) {
	if !h.activationAvailable(c) {
		return
	}
	p, hospital, staff, review, ok := personReviewParams(c)
	if !ok {
		return
	}
	var result EmploymentActivationSource
	err := h.employmentActivationTransaction(c, p, hospital, staff, review, func(tx *gorm.DB, source IdentitySource, authority *intakeAuthority) error {
		row, err := loadEmploymentReview(tx, p, hospital, staff, review, uuid.Nil)
		if err != nil {
			return err
		}
		result, err = employmentActivationSource(tx, p, source, row, authority.Actor)
		if err != nil {
			return err
		}
		return employmentActivationAudit(tx, p, hospital, staff, review, row.Version, "workforce.employment_activation.source.read", authority)
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			respond.NotFound(c, "employment review")
			return
		}
		employmentReviewError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	respond.OK(c, result)
}

func employmentActivationAudit(tx *gorm.DB, p authn.Principal, hospital, staff, resource uuid.UUID, version int64, action string, authority *intakeAuthority) error {
	activation := authority.Grants[PermEmploymentActivate]
	enrollment := authority.Grants[PermEnrollmentManage]
	changes, err := json.Marshal(map[string]any{
		"staff_id": staff, "version": version,
		"activation_assignment_id": activation.AssignmentID, "activation_assignment_version": activation.Version,
		"enrollment_assignment_id": enrollment.AssignmentID, "enrollment_assignment_version": enrollment.Version,
		"scope_epoch": activation.ScopeEpoch, "membership_epoch": activation.MembershipEpoch,
		"actor_staff_id": authority.Actor.Staff, "actor_link_id": authority.Actor.Link,
		"actor_link_version": authority.Actor.LinkVersion, "actor_employment_epoch": authority.Actor.EmploymentEpoch,
	})
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,?::jsonb)`, p.TenantID, hospital, p.Subject, action, resource, string(changes)).Error
}

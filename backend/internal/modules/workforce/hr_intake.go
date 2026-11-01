package workforce

import (
	"context"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

var errIntakeEmploymentReview = errors.New("employment status requires a reviewed employment workflow")
var errIntakeProjection = errors.New("unsupported Staff intake projection")
var errIntakeWorkspaceRequired = errors.New("the intake workspace must bind the original tenant and account")

const intakeAuthorityKey = "workforce.intake_authority"

type intakeActorReference struct {
	Staff, Link     uuid.UUID
	LinkVersion     int64
	EmploymentEpoch int64
}

type intakeAuthority struct {
	Tenant, Subject string
	Hospital        uuid.UUID
	Scoped          bool
	Grants          map[authz.Permission]authz.ScopeGrant
	Actor           intakeActorReference
}

func intakeDocumentGate(gate documentGate) bool {
	switch gate.(type) {
	case staffDocuments, employmentEvidenceGate:
		return true
	default:
		return false
	}
}

func (h *handlers) documentTransaction(c *gin.Context, gate documentGate, p authn.Principal, hospital uuid.UUID, writing bool, fn func(*gorm.DB) error) error {
	if c.Query(hrIntakeQuery) != hrIntakeVersion || !intakeDocumentGate(gate) {
		return h.transaction(c.Request.Context(), p, hospital, fn)
	}
	primary, additional := gate.permissions(writing)
	if primary == authz.Public {
		return authz.ErrScopedAuthorizationUnavailable
	}
	required := []authz.Permission{primary}
	if additional != authz.Public {
		required = append(required, additional)
	}
	return h.intakeTransaction(c, p, hospital, required, func(tx *gorm.DB, _ bool) error { return fn(tx) })
}

func (h *handlers) intakeReadTransaction(c *gin.Context, p authn.Principal, hospital uuid.UUID, fn func(*gorm.DB) error) error {
	versions, present := c.Request.URL.Query()[hrIntakeQuery]
	if !present {
		return h.transaction(c.Request.Context(), p, hospital, fn)
	}
	if len(versions) != 1 || versions[0] != hrIntakeVersion {
		return errIntakeProjection
	}
	return h.intakeTransaction(c, p, hospital, []authz.Permission{PermEnrollmentManage}, func(tx *gorm.DB, _ bool) error {
		return fn(tx)
	})
}

func (h *handlers) intakeTransaction(c *gin.Context, p authn.Principal, hospital uuid.UUID, required []authz.Permission, fn func(*gorm.DB, bool) error) error {
	return h.intakeAuthorityTransaction(c, p, hospital, required, false, fn)
}

func (h *handlers) intakeAuthorityTransaction(c *gin.Context, p authn.Principal, hospital uuid.UUID, required []authz.Permission, scopedOnly bool, fn func(*gorm.DB, bool) error) error {
	if c.Query(hrIntakeQuery) == hrIntakeVersion || c.Query("employment_review") == "v1" {
		tenants := c.Request.Header.Values("X-Helivanta-Expected-Tenant")
		subjects := c.Request.Header.Values("X-Helivanta-Expected-Subject")
		if len(tenants) != 1 || tenants[0] != p.TenantID || len(subjects) != 1 || subjects[0] != p.Subject {
			return errIntakeWorkspaceRequired
		}
	}
	if len(required) < 1 || len(required) > 2 {
		return authz.ErrScopedAuthorizationUnavailable
	}
	var original *intakeAuthority
	if value, present := c.Get(intakeAuthorityKey); present {
		var ok bool
		original, ok = value.(*intakeAuthority)
		if !ok || original == nil || original.Tenant != p.TenantID || original.Subject != p.Subject || original.Hospital != hospital || len(original.Grants) != len(required) {
			return authz.ErrScopedCommandDenied
		}
		for _, permission := range required {
			if _, ok := original.Grants[permission]; !ok {
				return authz.ErrScopedCommandDenied
			}
		}
	}
	if !authn.Refresh(c) {
		return authz.ErrScopedAuthorizationUnavailable
	}
	permissions, ok := authz.RefreshPermissions(c)
	if !ok {
		return authz.ErrScopedAuthorizationUnavailable
	}
	legacy := !scopedOnly && len(required) > 0
	for _, permission := range required {
		legacy = legacy && permissions.Has(permission)
	}
	if original != nil {
		if !original.Scoped && !legacy {
			return authz.ErrScopedCommandDenied
		}
		if original.Scoped {
			legacy = false
		}
	}
	if legacy {
		return h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
			if err := enrollmentScope(tx, p, hospital); err != nil {
				return err
			}
			if original == nil {
				grants := make(map[authz.Permission]authz.ScopeGrant, len(required))
				for _, permission := range required {
					grants[permission] = authz.ScopeGrant{}
				}
				c.Set(intakeAuthorityKey, &intakeAuthority{Tenant: p.TenantID, Subject: p.Subject, Hospital: hospital, Grants: grants})
			}
			return fn(tx, false)
		})
	}
	guard, supported := h.scopeAuthority.(authz.CompoundScopedCommandGuard)
	if !supported || p.IdleDeadline.IsZero() || len(required) < 1 || len(required) > 2 {
		return authz.ErrScopedAuthorizationUnavailable
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), p.IdleDeadline)
	defer cancel()
	ctx, budget := context.WithTimeout(ctx, 5*time.Second)
	defer budget()
	checks := make([]authz.ScopeCheck, 0, len(required))
	for _, permission := range required {
		request := authz.ScopeRequest{TenantID: p.TenantID, Subject: p.Subject, HospitalID: hospital.String(), Permission: permission}
		grant, err := h.scopeAuthority.AuthorizeScope(ctx, request)
		if err != nil {
			return authz.ErrScopedAuthorizationUnavailable
		}
		if grant.AssignmentID == "" {
			return authz.ErrScopedCommandDenied
		}
		if original != nil && original.Grants[permission] != grant {
			return authz.ErrScopedCommandDenied
		}
		if original == nil {
			if admitted, ok := authz.HospitalScopeGrant(c, permission, hospital.String()); ok && admitted != grant {
				return authz.ErrScopedCommandDenied
			}
		}
		checks = append(checks, authz.ScopeCheck{Request: request, Grant: grant})
	}
	return guard.WithScopeGrants(ctx, checks, p.AuthTime, func(commandCtx context.Context) error {
		return h.transaction(commandCtx, p, hospital, func(tx *gorm.DB) error {
			// Profile writers take this lock before Staff rows, including the actor.
			if err := tx.Exec(`SELECT workforce_lock_supervision(?::uuid,?::uuid)`, p.TenantID, hospital).Error; err != nil {
				return err
			}
			actor, err := lockReviewedLeaveActor(tx, p, hospital)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return authz.ErrScopedCommandDenied
			}
			if err != nil {
				return err
			}
			var staffEpoch struct{ EmploymentEpoch int64 }
			if err := tx.Table("workforce_staff").Select("employment_epoch").Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, actor.StaffID).Take(&staffEpoch).Error; err != nil {
				return err
			}
			actorReference := intakeActorReference{Staff: actor.StaffID, Link: actor.ID, LinkVersion: actor.Version, EmploymentEpoch: staffEpoch.EmploymentEpoch}
			if original != nil && original.Actor != actorReference {
				return authz.ErrScopedCommandDenied
			}
			if original == nil {
				grants := make(map[authz.Permission]authz.ScopeGrant, len(checks))
				for _, check := range checks {
					grants[check.Request.Permission] = check.Grant
				}
				c.Set(intakeAuthorityKey, &intakeAuthority{Tenant: p.TenantID, Subject: p.Subject, Hospital: hospital, Scoped: true, Grants: grants, Actor: actorReference})
			}
			if len(required) == 1 && required[0] == PermEmploymentReview {
				return fn(tx, true)
			}
			if err := tx.Exec(`SELECT set_config('app.workforce_hr_intake','authorized',true),set_config('app.workforce_hr_intake_actor',?,true),set_config('app.workforce_hr_intake_link',?,true),set_config('app.workforce_enrollment','authorized',true)`, actor.StaffID.String(), actor.ID.String()).Error; err != nil {
				return err
			}
			return fn(tx, true)
		})
	})
}

func respondIntakeError(c *gin.Context, err error) {
	if c.IsAborted() {
		return
	}
	switch {
	case errors.Is(err, errIntakeProjection):
		respond.Error(c, 400, "invalid_request", "This Staff intake view is not supported. Refresh and retry.")
	case errors.Is(err, authz.ErrScopedCredentialRevoked):
		respond.Unauthenticated(c, "credential revoked")
	case errors.Is(err, authz.ErrScopedCommandDenied):
		respond.Forbidden(c, "Staff action not permitted.")
	case errors.Is(err, authz.ErrScopedAuthorizationUnavailable), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		respond.Error(c, 503, "scoped_authorization_unavailable", "Staff access could not be verified. Refresh and retry.")
	case errors.Is(err, errIntakeEmploymentReview):
		respond.Error(c, 409, "employment_review_required", "Employment status changes require an independently reviewed employment workflow.")
	case errors.Is(err, errIntakeWorkspaceRequired):
		respond.Error(c, 409, "workspace_changed", "Refresh the Staff workspace and retry this action.")
	default:
		respondError(c, err)
	}
}

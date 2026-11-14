package workforce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (h *handlers) identityScopeTransaction(c *gin.Context, p authn.Principal, hospital uuid.UUID, permission authz.Permission, fn func(*gorm.DB, reviewedLeaveActor, authz.ScopeGrant) error) error {
	return h.withIdentityScopeGrant(c, p, hospital, permission, func(commandCtx context.Context, grant authz.ScopeGrant) error {
		return h.transaction(commandCtx, p, hospital, func(tx *gorm.DB) error {
			actor, err := lockReviewedLeaveActor(tx, p, hospital)
			if err != nil {
				return err
			}
			return fn(tx, actor, grant)
		})
	})
}

func (h *handlers) withIdentityScopeGrant(c *gin.Context, p authn.Principal, hospital uuid.UUID, permission authz.Permission, fn func(context.Context, authz.ScopeGrant) error) error {
	ctx, cancel := context.WithDeadline(c.Request.Context(), p.IdleDeadline)
	defer cancel()
	ctx, budget := context.WithTimeout(ctx, 5*time.Second)
	defer budget()
	return h.withIdentityScopeGrantContext(ctx, p, hospital, permission, fn)
}

func (h *handlers) withIdentityScopeGrantContext(ctx context.Context, p authn.Principal, hospital uuid.UUID, permission authz.Permission, fn func(context.Context, authz.ScopeGrant) error) error {
	guard, ok := h.scopeAuthority.(authz.ScopedCommandGuard)
	if !ok || h.scopeAuthority == nil || p.IdleDeadline.IsZero() {
		return authz.ErrScopedAuthorizationUnavailable
	}
	request := authz.ScopeRequest{TenantID: p.TenantID, Subject: p.Subject, HospitalID: hospital.String(), Permission: permission}
	grant, err := h.scopeAuthority.AuthorizeScope(ctx, request)
	if err != nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if grant.AssignmentID == "" {
		return authz.ErrScopedCommandDenied
	}
	return guard.WithScopeGrant(ctx, request, grant, p.AuthTime, func(commandCtx context.Context) error {
		return fn(commandCtx, grant)
	})
}

func (h *handlers) getIdentitySource(c *gin.Context) {
	p, hospital, ok := profileScope(c)
	if !ok {
		return
	}
	projected := false
	if c.Request.URL.RawQuery != "" {
		query := c.Request.URL.Query()
		values := query[IdentitySourceProjectionQuery]
		if len(query) != 1 || len(values) != 1 || values[0] != IdentitySourceDraftProjection {
			respond.Error(c, 400, "invalid_request", "Unsupported identity-source projection.")
			return
		}
		projected = true
	}
	staffID, err := uuid.Parse(c.Param("id"))
	if err != nil || staffID == uuid.Nil {
		respond.NotFound(c, "staff")
		return
	}
	var result IdentitySource
	canManage := true
	read := func(tx *gorm.DB, source IdentitySource, authority personReviewAuthority) error {
		result = source
		if projected {
			result.ManageChecksAllowed = &canManage
		}
		return personReviewAudit(tx, p, hospital, staffID, "workforce.identity_source.read", source.StaffVersion, authority)
	}
	transaction := func(permission authz.Permission) error {
		if !projected {
			return h.personReviewTransaction(c, p, hospital, staffID, permission, read)
		}
		return h.identityScopeTransaction(c, p, hospital, permission, func(tx *gorm.DB, actor reviewedLeaveActor, grant authz.ScopeGrant) error {
			source, err := loadIdentitySource(tx, p, hospital, staffID)
			if err != nil {
				return err
			}
			return read(tx, source, personReviewAuthority{Staff: &actor, Assignment: grant})
		})
	}
	err = transaction(PermIdentityChecksManage)
	hiddenActor := errors.Is(err, errPersonReviewActorHidden)
	if errors.Is(err, authz.ErrScopedCommandDenied) {
		canManage = false
		err = transaction(PermIdentityChecksReview)
	}
	if err != nil {
		if errors.Is(err, errPersonReviewActorHidden) || (hiddenActor && errors.Is(err, authz.ErrScopedCommandDenied)) {
			err = gorm.ErrRecordNotFound
		}
		respondIdentityError(c, err)
		return
	}
	respond.OK(c, result)
}

func respondIdentityError(c *gin.Context, err error) {
	if c.IsAborted() {
		return
	}
	switch {
	case errors.Is(err, errIntakeWorkspaceRequired):
		respond.Error(c, 409, "session_changed", "Refresh the current Staff workspace and retry.")
	case errors.Is(err, authz.ErrScopedCredentialRevoked):
		respond.Error(c, 401, "unauthenticated", "Invalid credentials.")
	case errors.Is(err, authz.ErrScopedCommandDenied):
		respond.Forbidden(c, "Staff identity action not permitted.")
	case errors.Is(err, authz.ErrScopedAuthorizationUnavailable), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		respond.Error(c, 503, "scoped_authorization_unavailable", "Scoped authorization unavailable.")
	default:
		respondError(c, err)
	}
}

func loadIdentitySource(tx *gorm.DB, p authn.Principal, hospital, staffID uuid.UUID) (IdentitySource, error) {
	result := IdentitySource{HospitalID: hospital, StaffID: staffID}
	if err := tx.Exec(`SELECT set_config('app.workforce_identity_source','authorized',true),set_config('app.workforce_identity_staff',?,true)`, staffID.String()).Error; err != nil {
		return result, err
	}
	if err := tx.Table("workforce_staff").Select("employee_code,display_name,employment_status,employment_epoch,version AS staff_version").Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, staffID).Take(&result).Error; err != nil {
		return result, err
	}
	var profile struct {
		GivenName   string
		FamilyName  string
		DateOfBirth string
	}
	err := tx.Table("workforce_enrollment_profile").Select("given_name,family_name,COALESCE(to_char(date_of_birth,'YYYY-MM-DD'),'') AS date_of_birth").Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND staff_id=?", p.TenantID, hospital, staffID).Take(&profile).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return result, err
	}
	result.GivenName, result.FamilyName, result.DateOfBirth = profile.GivenName, profile.FamilyName, profile.DateOfBirth
	material, err := json.Marshal([]string{p.TenantID, hospital.String(), staffID.String(), result.EmployeeCode, result.DisplayName, result.GivenName, result.FamilyName, result.DateOfBirth})
	if err != nil {
		return result, err
	}
	digest := sha256.Sum256(material)
	result.IdentityHash = hex.EncodeToString(digest[:])
	return result, nil
}

package organization

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

//go:embed assignment_choices.sql
var assignmentChoicesSchema string

func assignmentDepartmentChoiceQuery(c *gin.Context) bool {
	values, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil || c.Request.URL.ForceQuery {
		return false
	}
	for key, entries := range values {
		if (key != "limit" && key != "cursor" && key != "status") || len(entries) != 1 || entries[0] == "" || key == "status" && entries[0] != "active" {
			return false
		}
	}
	return true
}

func (h *handlers) listAssignmentDepartmentChoices(c *gin.Context, page pagination.Params) ([]departmentRecord, error) {
	p, hospital, ok := departmentHospital(c)
	if !ok {
		return nil, nil
	}
	if !assignmentDepartmentChoiceQuery(c) || page.Limit < 1 || page.Limit > 20 {
		respond.Error(c, 400, "invalid_request", "Choose a current department page without extra filters.")
		return nil, nil
	}
	if hospital.String() != c.Param("hospital") {
		respond.NotFound(c, "hospital")
		return nil, nil
	}
	if !authn.Refresh(c) {
		return nil, nil
	}
	current, ok := authn.PrincipalFrom(c)
	if !ok || current.TenantID != p.TenantID || current.Subject != p.Subject {
		respond.Error(c, 409, "session_changed", "Refresh the current account before selecting a department.")
		return nil, nil
	}
	p = current
	permissions, ok := authz.RefreshPermissions(c)
	if !ok {
		return nil, nil
	}
	if !permissions.Has(authz.Permission(authz.GovernanceAssignmentManage)) {
		respond.Forbidden(c, "Hospital assignment management is required.")
		return nil, nil
	}
	if h.db == nil || h.governanceSources == nil || p.IdleDeadline.IsZero() {
		assignmentChoiceError(c, authz.ErrScopedAuthorizationUnavailable)
		return nil, nil
	}
	deadline := time.Now().Add(5 * time.Second)
	if p.IdleDeadline.Before(deadline) {
		deadline = p.IdleDeadline
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), deadline)
	defer cancel()
	rows := []departmentRecord{}
	callbacks := 0
	completed := false
	var callbackErr error
	err := h.governanceSources.WithGovernanceSource(ctx, authz.GovernanceSourceRequest{Principal: p, Capability: authz.GovernanceAssignmentManage, Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: "hospital", ID: &hospital}}, func(held context.Context, source authz.GovernanceSourceEvidence) error {
		callbacks++
		if callbacks != 1 || !h.db.GuardedCommandActive(held) || tenantdb.ActorFrom(held) != p.Subject {
			return authz.ErrScopedAuthorizationUnavailable
		}
		proof, err := authz.AssignmentDepartmentChoiceProof(authz.GovernedAssignmentSelectionRequest{Principal: p, HospitalID: hospital, Capability: authz.GovernanceAssignmentManage, Source: source}, time.Now())
		if err != nil {
			return err
		}
		raw, err := json.Marshal(proof)
		if err != nil {
			return err
		}
		callbackErr = h.db.WithTenantCommand(held, p.TenantID, func(tx *gorm.DB) (err error) {
			var supported bool
			if err = tx.Raw(`SELECT CASE WHEN to_regprocedure('public.organization_assignment_selection_support()') IS NULL THEN false ELSE public.organization_assignment_selection_support() END`).Scan(&supported).Error; err != nil || !supported {
				return authz.ErrScopedAuthorizationUnavailable
			}
			var previous string
			if err = tx.Raw(`SELECT COALESCE(current_setting('app.organization_assignment_selection',true),'')`).Scan(&previous).Error; err != nil {
				return err
			}
			hidden := tx.Session(&gorm.Session{Logger: logger.Discard})
			defer func() {
				err = errors.Join(err, hidden.Exec(`SELECT set_config('app.organization_assignment_selection',?,true)`, previous).Error)
			}()
			if err = hidden.Exec(`SELECT set_config('app.organization_assignment_selection',?,true)`, string(raw)).Error; err != nil {
				return err
			}
			if err = tx.Exec(`SELECT pg_advisory_xact_lock_shared(hashtextextended(?,0))`, p.TenantID+":organization-departments:"+hospital.String()).Error; err != nil {
				return err
			}
			if err = requireDepartmentHospital(tx, p.TenantID, hospital); err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, errConflict) {
					return authz.ErrScopedAuthorizationUnavailable
				}
				return err
			}
			query := tenantdb.ApplyKeyset(tx, page).Select("tenant_id,hospital_id,id,revision_id,version,code,name,kind,parent_id,status,effective_at,created_at").Where("tenant_id=? AND hospital_id=? AND status='active'", p.TenantID, hospital).Clauses(clause.Locking{Strength: "SHARE"})
			if err = query.Find(&rows).Error; err != nil {
				return err
			}
			return audit(tx, p.TenantID, p.Subject, "organization.assignment_department_choices.read", "organization_hospitals", hospital)
		})
		completed = callbackErr == nil
		return callbackErr
	})
	if callbackErr != nil {
		err = callbackErr
	}
	if err == nil && (callbacks != 1 || !completed) {
		err = authz.ErrScopedAuthorizationUnavailable
	}
	if err != nil {
		assignmentChoiceError(c, err)
		return nil, nil
	}
	return rows, nil
}

func assignmentChoiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, authz.ErrScopedCredentialRevoked):
		respond.Unauthenticated(c, "credentials changed")
	case errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, errConflict), errors.Is(err, authz.ErrGovernanceScopeHidden), errors.Is(err, authz.ErrScopedCommandDenied):
		respond.NotFound(c, "hospital department choices")
	default:
		respond.Error(c, 503, "assignment_selection_unavailable", "Current department choices could not be verified.")
	}
}

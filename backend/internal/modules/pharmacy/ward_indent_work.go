package pharmacy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

var (
	errWardIndentIdentity = errors.New("ward indent command already exists with different details")
	errWardIndentStay     = errors.New("ward indent stay is not open")
	errWardIndentEvidence = errors.New("ward indent evidence unavailable")
)

// wardIndentUse is one held requester purpose at one outlet, with the names a raise records.
type wardIndentUse struct {
	hospitalWorkUse
	outlet                       uuid.UUID
	hospitalName, departmentName string
}

func (u wardIndentUse) basis() ([]byte, error) {
	return json.Marshal(map[string]any{"operation": u.operation, "source": u.source, "actor": u.actor, "department_path": u.path})
}

// wardIndentWork holds the requester's department authority, then runs action in one tenant transaction inside the ward indent frame.
func (h *catalogueHandlers) wardIndentWork(ctx context.Context, p authn.Principal, hospital, department string, outlet uuid.UUID, permission authz.Permission, names bool, action func(*gorm.DB, wardIndentUse) error) error {
	if action == nil || (permission != PermIndentRead && permission != PermIndentRaise) {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return h.withHospitalWorkActor(ctx, p, authz.WardIndentWork, hospital, department, permission, func(actorCtx context.Context, work hospitalWorkUse) error {
		use := wardIndentUse{hospitalWorkUse: work, outlet: outlet}
		if names {
			hospitals, hospitalOK := h.wardIndentActors.(authz.HospitalWorkHospitalNameReader)
			departments, departmentOK := h.stocktakeReferences.(authz.HospitalWorkDepartmentNameReader)
			if !hospitalOK || !departmentOK {
				return authz.ErrScopedAuthorizationUnavailable
			}
			scope := authz.StaffActorScope{TenantID: p.TenantID, Subject: p.Subject, HospitalID: work.hospital, DepartmentPath: work.path}
			var err error
			if use.hospitalName, err = hospitals.HospitalWorkHospitalName(actorCtx, authz.WardIndentWork, scope); err != nil {
				return err
			}
			if use.departmentName, err = departments.HospitalWorkDepartmentName(actorCtx, authz.WardIndentWork, scope); err != nil {
				return err
			}
			if !wardIndentName(use.hospitalName) || !wardIndentName(use.departmentName) {
				return authz.ErrScopedAuthorizationUnavailable
			}
		}
		return h.db.WithTenantCommand(actorCtx, p.TenantID, func(tx *gorm.DB) error {
			var intact bool
			if err := tx.Raw(`SELECT pharmacy_stocktake_hospital_integrity()`).Scan(&intact).Error; err != nil {
				return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
			}
			if !intact {
				return authz.ErrScopedAuthorizationUnavailable
			}
			return wardIndentFrame(tx, p, use, func() error { return action(tx, use) })
		})
	})
}

func wardIndentName(value string) bool {
	return strings.TrimSpace(value) == value && utf8.RuneCountInString(value) <= 200 && stocktakeText(value, 200)
}

func wardIndentFrame(tx *gorm.DB, p authn.Principal, use wardIndentUse, action func() error) (err error) {
	basis, err := use.basis()
	if err != nil || len(basis) > 4096 {
		return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
	}
	var previous string
	if err = tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM unnest(ARRAY['app.pharmacy_indent_outlet','app.pharmacy_indent_hospital','app.pharmacy_indent_department','app.pharmacy_indent_subject','app.pharmacy_indent_permission','app.pharmacy_indent_basis'])AS key`).Scan(&previous).Error; err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, previous).Error)
	}()
	outlet := ""
	if use.outlet != uuid.Nil {
		outlet = use.outlet.String()
	}
	if err = tx.Exec(`SELECT set_config('app.pharmacy_indent_outlet',?,true),set_config('app.pharmacy_indent_hospital',?,true),set_config('app.pharmacy_indent_department',?,true),set_config('app.pharmacy_indent_subject',?,true),set_config('app.pharmacy_indent_permission',?,true),set_config('app.pharmacy_indent_basis',?,true)`,
		outlet, use.hospital.String(), use.department.String(), p.Subject, string(use.permission), string(basis)).Error; err != nil {
		return err
	}
	return action()
}

func wardIndentError(c *gin.Context, err error) {
	var audit operatingAuditFailure
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, errWardIndentEvidence), errors.As(err, &audit):
		requestid.Logger(c).ErrorContext(c.Request.Context(), "ward indent evidence unavailable", "err", err)
		respond.Error(c, http.StatusServiceUnavailable, "audit_unavailable", "Evidence could not be recorded; nothing was saved.")
	case errors.Is(err, authz.ErrDepartmentReferenceHidden), errors.Is(err, authz.ErrScopedCommandDenied), errors.Is(err, gorm.ErrRecordNotFound):
		respond.NotFound(c, "ward indent resource")
	case errors.Is(err, authz.ErrDepartmentReferenceChanged):
		respond.Error(c, http.StatusConflict, "source_changed", "The department changed. Reload before continuing.")
	case errors.Is(err, authz.ErrScopedCredentialRevoked), errors.Is(err, authz.ErrInvalidScopedAssignment):
		respond.Error(c, http.StatusUnauthorized, "session_changed", "Sign in again before continuing.")
	case errors.Is(err, authz.ErrScopedAuthorizationUnavailable):
		respond.Error(c, http.StatusServiceUnavailable, "authorization_unavailable", "Ward indent authority could not be verified.")
	case errors.Is(err, errWardIndentStay):
		respond.Error(c, http.StatusConflict, "stay_not_open", "The patient is no longer in that bed. Look the bed up again.")
	case errors.Is(err, errWardIndentIdentity):
		respond.Error(c, http.StatusConflict, "indent_identity", "This indent already exists with different details.")
	case errors.As(err, &pg):
		wardIndentConstraint(c, pg)
	default:
		respond.Error(c, http.StatusServiceUnavailable, "authorization_unavailable", "Ward indent authority could not be verified.")
	}
}

func wardIndentConstraint(c *gin.Context, pg *pgconn.PgError) {
	switch {
	case pg.ConstraintName == "pharmacy_ward_indent_stay_open":
		respond.Error(c, http.StatusConflict, "stay_not_open", "The patient is no longer in that bed. Look the bed up again.")
	case pg.ConstraintName == "pharmacy_ward_indent_affiliated":
		respond.Error(c, http.StatusConflict, "not_affiliated", "The hospital has not verified this pharmacy.")
	case pg.ConstraintName == "pharmacy_ward_indent_product_active":
		respond.Error(c, http.StatusConflict, "product_unavailable", "A requested product is no longer active at this pharmacy.")
	case pg.ConstraintName == "pharmacy_ward_indent_issued":
		respond.Error(c, http.StatusConflict, "already_issued", "Part of this indent has been issued, so it can no longer be cancelled.")
	case pg.ConstraintName == "pharmacy_ward_indent_transition":
		respond.Error(c, http.StatusConflict, "indent_closed", "This indent is no longer open.")
	case pg.Code == "23505":
		respond.Error(c, http.StatusConflict, "indent_identity", "This indent already exists with different details.")
	default:
		respond.Error(c, http.StatusServiceUnavailable, "authorization_unavailable", "Ward indent authority could not be verified.")
	}
}

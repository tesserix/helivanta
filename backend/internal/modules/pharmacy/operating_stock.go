package pharmacy

import (
	"context"
	_ "embed"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

//go:embed operating_receipts.sql
var operatingReceiptsSchema string

// operatingStockFailure marks PharmaConnect's own errors so IAM failures stay distinguishable (fail closed to 503).
type operatingStockFailure struct{ err error }

func (f operatingStockFailure) Error() string { return f.err.Error() }
func (f operatingStockFailure) Unwrap() error { return f.err }

type operatingStockUse struct {
	tenant, subject  string
	pharmacy, outlet uuid.UUID
	grant            authz.OperatingScopeGrant
}

// operatingStock admits one reviewed duty at one exact outlet; Postgres trusts these settings only inside this IAM command (ADR-0064).
func (h *catalogueHandlers) operatingStock(ctx context.Context, p authn.Principal, pharmacy, outlet uuid.UUID, permission authz.Permission, action func(*gorm.DB, operatingStockUse) error) error {
	if h.operating == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	scope := authz.OperatingLocationScope{TenantID: p.TenantID, EmployerKind: "pharmacy", EmployerID: pharmacy, LocationID: outlet}
	request := authz.OperatingScopeRequest{Principal: p, EmployerKind: "pharmacy", EmployerID: pharmacy, LocationID: outlet, Permission: permission}
	return h.operating.WithOperatingScope(tenantdb.WithActor(ctx, p.Subject), request, func(ctx context.Context, grant authz.OperatingScopeGrant) error {
		if grant.Scope != scope || grant.Permission != permission {
			return authz.ErrScopedAuthorizationUnavailable
		}
		err := h.db.WithTenantCommand(ctx, p.TenantID, func(tx *gorm.DB) (err error) {
			var previous string
			if err = tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM unnest(ARRAY['app.pharmacy_subject','app.pharmacy_stock_access','app.pharmacy_stock_pharmacy','app.pharmacy_stock_outlet','app.pharmacy_stock_permission'])AS key`).Scan(&previous).Error; err != nil {
				return err
			}
			defer func() {
				err = errors.Join(err, tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, previous).Error)
			}()
			if err = tx.Exec(`SELECT set_config('app.pharmacy_subject',?,true),set_config('app.pharmacy_stock_access','authorized',true),set_config('app.pharmacy_stock_pharmacy',?,true),set_config('app.pharmacy_stock_outlet',?,true),set_config('app.pharmacy_stock_permission',?,true)`, p.Subject, pharmacy.String(), outlet.String(), string(permission)).Error; err != nil {
				return err
			}
			return action(tx, operatingStockUse{tenant: p.TenantID, subject: p.Subject, pharmacy: pharmacy, outlet: outlet, grant: grant})
		})
		if err != nil {
			return operatingStockFailure{err}
		}
		return nil
	})
}

// operatingAuditFailure fails the whole command closed: evidence that cannot be written means nothing is saved or shown.
type operatingAuditFailure struct{ err error }

func (f operatingAuditFailure) Error() string { return f.err.Error() }
func (f operatingAuditFailure) Unwrap() error { return f.err }

// audit appends the duty evidence in the same transaction as the read or write it describes.
func (u operatingStockUse) audit(tx *gorm.DB, action, kind string, resource *uuid.UUID) error {
	id, err := uuid.NewV7()
	if err != nil {
		return operatingAuditFailure{err}
	}
	g := u.grant
	if err := tx.Exec(`INSERT INTO pharmacy_operating_stock_audit(tenant_id,id,pharmacy_id,outlet_id,action,resource_kind,resource_id,permission,duty_id,snapshot_version,authority_epoch,staff_id,staff_version,link_id,link_version,actor_subject)VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		u.tenant, id, u.pharmacy, u.outlet, action, kind, resource, string(g.Permission), g.DutyID, g.SnapshotVersion, g.AuthorityEpoch,
		g.Identity.StaffID, g.Identity.StaffVersion, g.Identity.LinkID, g.Identity.LinkVersion, u.subject).Error; err != nil {
		return operatingAuditFailure{err}
	}
	return nil
}

// operatingStockError never answers 403: refusal is indistinguishable from an unknown outlet.
func operatingStockError(c *gin.Context, err error) {
	var failure operatingStockFailure
	var audit operatingAuditFailure
	switch {
	case errors.As(err, &audit):
		requestid.Logger(c).ErrorContext(c.Request.Context(), "operating stock evidence unavailable", "err", audit.err)
		respond.Error(c, http.StatusServiceUnavailable, "audit_unavailable", "Duty evidence could not be recorded; nothing was saved.")
	case errors.Is(err, authz.ErrScopedCommandDenied), errors.Is(err, gorm.ErrRecordNotFound):
		respond.NotFound(c, "pharmacy resource")
	case errors.Is(err, authz.ErrScopedCredentialRevoked), errors.Is(err, authz.ErrInvalidScopedAssignment):
		respond.Error(c, http.StatusUnauthorized, "session_changed", "Sign in again before continuing.")
	case errors.As(err, &failure):
		stockError(c, failure.err)
	default:
		respond.Error(c, http.StatusServiceUnavailable, "authorization_unavailable", "Operating authority could not be verified.")
	}
}

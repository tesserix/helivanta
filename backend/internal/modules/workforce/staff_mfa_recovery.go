package workforce

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/onboarding-service/api/customercontract"
	"github.com/tesserix/onboarding-service/api/staffclient"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func registerStaffMFARecovery(r *platform.Router, h *handlers) {
	for _, family := range []struct {
		kind, path string
		permission authz.Permission
	}{
		{hospitalEstablishmentKind, "hospitals", PermLoginLinkManage},
		{pharmacyEstablishmentKind, "pharmacies", PermIndependentLoginManage},
		{supplierEstablishmentKind, "suppliers", PermIndependentLoginManage},
	} {
		root := r.Group("/workforce/"+family.path).Establishment(family.kind, family.kind, h.establishmentAdmission(family.kind))
		base := "/:" + family.kind + "/staff/:id/mfa-recoveries/:command"
		root.PUT(base, family.permission, h.resetStaffMFA)
		root.GET(base, family.permission, h.readStaffMFA)
		root.POST(base+"/cancel", family.permission, h.cancelStaffMFA)
	}
}

func (h *handlers) resetStaffMFA(c *gin.Context) { h.staffMFA(c, "reset") }
func (h *handlers) readStaffMFA(c *gin.Context)  { h.staffMFA(c, "read") }

func staffMFAScope(c *gin.Context) (authn.Principal, establishment, uuid.UUID, uuid.UUID, bool) {
	var empty establishment
	if !authn.Refresh(c) {
		return authn.Principal{}, empty, uuid.Nil, uuid.Nil, false
	}
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return p, empty, uuid.Nil, uuid.Nil, false
	}
	for header, expected := range map[string]string{"X-Helivanta-Expected-Tenant": p.TenantID, "X-Helivanta-Expected-Subject": p.Subject} {
		values := c.Request.Header.Values(header)
		if len(values) != 1 || values[0] != expected {
			respond.Error(c, 409, "session_changed", "Refresh your account and workspace before resetting an authenticator.")
			return p, empty, uuid.Nil, uuid.Nil, false
		}
	}
	kind := hospitalEstablishmentKind
	if c.Param("pharmacy") != "" {
		kind = pharmacyEstablishmentKind
	} else if c.Param("supplier") != "" {
		kind = supplierEstablishmentKind
	}
	employer, err := uuid.Parse(c.Param(kind))
	staff, staffErr := uuid.Parse(c.Param("id"))
	command, commandErr := uuid.Parse(c.Param("command"))
	if err != nil || employer == uuid.Nil || employer.String() != c.Param(kind) || staffErr != nil || staff == uuid.Nil || staff.String() != c.Param("id") || commandErr != nil || command.Version() != 7 || command.Variant() != uuid.RFC4122 || command.String() != c.Param("command") {
		respond.NotFound(c, "staff recovery")
		return p, empty, uuid.Nil, uuid.Nil, false
	}
	return p, establishment{kind: kind, id: employer}, staff, command, true
}

func (h *handlers) cancelStaffMFA(c *gin.Context) { h.staffMFA(c, "cancel") }

func (h *handlers) staffMFA(c *gin.Context, operation string) {
	reset := operation == "reset"
	c.Header("Cache-Control", "no-store")
	p, root, staff, command, ok := staffMFAScope(c)
	if !ok {
		return
	}
	if operation != "read" {
		media, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
		if err != nil || media != "application/json" {
			respond.Error(c, 415, "invalid_request", "Use JSON for an authenticator recovery command.")
			return
		}
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if err != nil || c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		respond.Error(c, 400, "invalid_request", "Use one bounded recovery command without query parameters.")
		return
	}
	var input customercontract.StaffMFAResetInput
	if reset {
		if json.Unmarshal(raw, &input) != nil || input.CommandID != command.String() {
			respond.Error(c, 400, "invalid_request", "Provide the current reviewed link, Staff version and a reason.")
			return
		}
	} else if operation == "cancel" {
		var body map[string]json.RawMessage
		if json.Unmarshal(raw, &body) != nil || body == nil || len(body) != 0 {
			respond.Error(c, 400, "invalid_request", "Cancellation requires an empty object.")
			return
		}
	} else if len(raw) != 0 {
		respond.Error(c, 400, "invalid_request", "A recovery status read has no request body.")
		return
	}
	if h.staffMFARecovery == nil {
		staffMFAError(c, staffclient.ErrUnavailable)
		return
	}
	source := func(ctx context.Context, intent customercontract.StaffMFAResetInput) (customercontract.StaffMFARecoveryTarget, error) {
		return h.staffMFATarget(ctx, p, root, staff, intent)
	}
	var receipt customercontract.StaffMFARecoveryReceipt
	if reset {
		receipt, err = h.staffMFARecovery.Reset(c.Request.Context(), p, input, source)
	} else if operation == "cancel" {
		receipt, err = h.staffMFARecovery.Cancel(c.Request.Context(), p, command.String(), platform.StaffMFARecoveryScope{EmployerKind: root.kind, EmployerID: root.id, StaffID: staff})
	} else {
		receipt, err = h.staffMFARecovery.Read(c.Request.Context(), p, command.String(), platform.StaffMFARecoveryScope{EmployerKind: root.kind, EmployerID: root.id, StaffID: staff}, source)
	}
	if err != nil {
		staffMFAError(c, err)
		return
	}
	if reset {
		respond.Accepted(c, receipt)
	} else {
		respond.OK(c, receipt)
	}
}

func (h *handlers) staffMFATarget(ctx context.Context, p authn.Principal, root establishment, staffID uuid.UUID, intent customercontract.StaffMFAResetInput) (customercontract.StaffMFARecoveryTarget, error) {
	var target customercontract.StaffMFARecoveryTarget
	if h.db == nil || intent.Validate() != nil {
		return target, staffclient.ErrUnavailable
	}
	err := h.transaction(ctx, p, root.id, func(tx *gorm.DB) error {
		if err := root.requireKind(tx, p.TenantID); err != nil {
			return err
		}
		if err := lockDirectoryManager(tx, p, root.id); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.workforce_login_link_access','authorized',true)`).Error; err != nil {
			return err
		}
		var person struct {
			ID               uuid.UUID
			Version          int64
			EmploymentStatus string
		}
		if err := tx.Table("workforce_staff").Clauses(clause.Locking{Strength: "SHARE"}).Select("id,version,employment_status").Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, root.id, staffID).Take(&person).Error; err != nil {
			return err
		}
		if person.EmploymentStatus != "active" {
			return gorm.ErrRecordNotFound
		}
		if person.Version != intent.ExpectedStaffVersion {
			return staffclient.ErrConflict
		}
		linkID := uuid.MustParse(intent.ExpectedLinkID)
		var link struct {
			ID              uuid.UUID
			Subject, Status string
			Version         int64
		}
		err := tx.Table("workforce_login_links").Clauses(clause.Locking{Strength: "SHARE"}).Select("id,subject,status,version").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, root.id, staffID, linkID).Take(&link).Error
		if err != nil {
			return err
		}
		if link.Status != "active" {
			return staffclient.ErrConflict
		}
		if link.Version != intent.ExpectedLinkVersion {
			return staffclient.ErrConflict
		}
		target = customercontract.StaffMFARecoveryTarget{TenantID: p.TenantID, EmployerKind: root.kind, EmployerID: root.id.String(), StaffID: staffID.String(), StaffVersion: person.Version, LinkID: link.ID.String(), LinkVersion: link.Version, Subject: link.Subject}
		if target.Validate() != nil {
			return staffclient.ErrUnavailable
		}
		return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'staff_mfa_recovery_target_read',?,'{}'::jsonb)`, p.TenantID, root.id, p.Subject, staffID).Error
	})
	return target, err
}

func staffMFAError(c *gin.Context, err error) {
	if p, ok := authn.PrincipalFrom(c); ok && !time.Now().Before(p.IdleDeadline) {
		err = authz.ErrScopedCredentialRevoked
	}
	switch {
	case errors.Is(err, authz.ErrScopedCredentialRevoked):
		respond.Error(c, 401, "fresh_auth_required", "Sign in again before resetting a Staff authenticator.")
	case errors.Is(err, authz.ErrScopedCommandDenied):
		respond.Forbidden(c, "Current tenant administration is required. Owner and privileged accounts use the separate recovery process.")
	case errors.Is(err, staffclient.ErrNotFound), errors.Is(err, gorm.ErrRecordNotFound):
		respond.NotFound(c, "staff recovery")
	case errors.Is(err, staffclient.ErrInvalid):
		respond.Error(c, 400, "invalid_request", "Check the recovery command and reviewed Staff link.")
	case errors.Is(err, staffclient.ErrConflict):
		respond.Conflict(c, "The Staff account or recovery command changed. Refresh before continuing.")
	default:
		respond.Error(c, 503, "recovery_unavailable", "Authenticator recovery could not be confirmed. Check the same command's status before retrying.")
	}
}

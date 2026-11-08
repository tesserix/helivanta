package workforce

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const loginLinkColumns = "id,staff_id,subject,status,version,proposed_by_subject,proposal_reason,actor_subject,reason,created_at,updated_at"

var errLoginLinkConflict = errors.New("login link state changed or command was reused")

func (l loginLink) PageKey() (time.Time, uuid.UUID)        { return l.CreatedAt, l.ID }
func (l loginLinkHistory) PageKey() (time.Time, uuid.UUID) { return l.CreatedAt, l.ID }

// linkEstablishment reads the establishment from its own route family, so a handler admits only that family's parameter.
type linkEstablishment func(*gin.Context) (authn.Principal, establishment, bool)

func hospitalLinkEstablishment(c *gin.Context) (authn.Principal, establishment, bool) {
	p, hospital, ok := scope(c)
	return p, establishment{kind: hospitalEstablishmentKind, id: hospital}, ok
}

func pharmacyLinkEstablishment(c *gin.Context) (authn.Principal, establishment, bool) {
	p, pharmacy, ok := pharmacyScope(c)
	return p, pharmacy.establishment(), ok
}

func loginLinkScope(c *gin.Context, admit linkEstablishment) (authn.Principal, establishment, uuid.UUID, bool) {
	p, e, ok := admit(c)
	if !ok {
		return p, e, uuid.Nil, false
	}
	set, _ := authz.PermissionsFrom(c)
	if !set.Has(PermLoginLinkManage) && !set.Has(PermLoginLinkReview) {
		respond.Forbidden(c, "Account link access is required")
		return p, e, uuid.Nil, false
	}
	if expected := c.Request.Header.Values("X-Helivanta-Expected-Tenant"); len(expected) > 0 {
		if len(expected) != 1 || expected[0] != p.TenantID {
			respond.NotFound(c, "staff account link")
			return p, e, uuid.Nil, false
		}
	}
	staff, err := uuid.Parse(c.Param("id"))
	if err != nil || staff == uuid.Nil {
		respond.NotFound(c, "staff")
		return p, e, uuid.Nil, false
	}
	return p, e, staff, true
}

func loginLinkID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("link"))
	if err != nil || id.Version() != 7 {
		respond.NotFound(c, "staff account link")
		return uuid.Nil, false
	}
	return id, true
}

func validLoginSubject(subject string) bool {
	return validBenefitText(subject, 1, 200) && !strings.ContainsAny(subject, "*#:") && strings.IndexFunc(subject, unicode.IsSpace) < 0
}

func (h *handlers) loginCandidate(c *gin.Context, p authn.Principal, subject string) bool {
	if h.roles == nil {
		respond.Error(c, 503, "identity_unavailable", "Account membership is temporarily unavailable")
		return false
	}
	roles, err := h.roles.ListRoles(c.Request.Context(), subject)
	if err != nil {
		respond.Error(c, 503, "identity_unavailable", "Account membership is temporarily unavailable")
		return false
	}
	for _, role := range roles {
		if role.TenantID == p.TenantID {
			return true
		}
	}
	respond.NotFound(c, "account")
	return false
}

// loginLinkTransaction replays the route family's kind first, because admission ran before the transaction.
func (h *handlers) loginLinkTransaction(c *gin.Context, p authn.Principal, e establishment, staffID uuid.UUID, command string, fn func(*gorm.DB, staff) error) error {
	return h.transaction(c.Request.Context(), p, e.id, func(tx *gorm.DB) error {
		if err := e.requireKind(tx, p.TenantID); err != nil {
			return err
		}
		if err := requireStaffAccess(tx, p.TenantID, e.id, true); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.workforce_login_link_access','authorized',true),set_config('app.workforce_login_link_command',?,true)`, command).Error; err != nil {
			return err
		}
		query := tx.Table("workforce_staff").Select(staffColumns).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, e.id, staffID)
		if command != "" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		var person staff
		if err := query.Take(&person).Error; err != nil {
			return err
		}
		return fn(tx, person)
	})
}

func loadLoginLink(tx *gorm.DB, p authn.Principal, hospital, staffID, id uuid.UUID) (loginLink, error) {
	var row loginLink
	err := tx.Table("workforce_login_links").Select(loginLinkColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staffID, id).Take(&row).Error
	return row, err
}

// publishLoginLink names the establishment under its own family's subject; an unknown kind fails the command.
func (h *handlers) publishLoginLink(tx *gorm.DB, p authn.Principal, e establishment, row loginLink) error {
	var subject string
	var data any
	switch e.kind {
	case hospitalEstablishmentKind:
		subject = LoginLinkChanged
		data = workforcecontract.LoginLinkChangedData{HospitalID: e.id.String(), StaffID: row.StaffID.String(), LinkID: row.ID.String(), Version: row.Version, Status: row.Status}
	case pharmacyEstablishmentKind:
		subject = PharmacyLoginLinkChanged
		data = workforcecontract.PharmacyLoginLinkChangedData{PharmacyID: e.id.String(), StaffID: row.StaffID.String(), LinkID: row.ID.String(), Version: row.Version, Status: row.Status}
	default:
		return fmt.Errorf("workforce: no login link subject for establishment kind %q", e.kind)
	}
	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, subject, events.Event{Type: subject, Version: 1, TenantID: p.TenantID, Data: payload})
}

func respondLoginLinkError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23514" && pg.ConstraintName == "workforce_login_generation" {
		respond.Error(c, 409, "employment_generation_changed", "Review the current digital employment generation before creating or approving a new login link.")
		return
	}
	if errors.Is(err, errLoginLinkConflict) || (errors.As(err, &pg) && pg.Code == "23505") {
		respond.Error(c, 409, "link_conflict", "The account link changed. Refresh its history before continuing.")
		return
	}
	respondError(c, err)
}

func (h *handlers) proposeLoginLink(admit linkEstablishment) gin.HandlerFunc {
	return func(c *gin.Context) { h.proposeEstablishmentLoginLink(c, admit) }
}

func (h *handlers) proposeEstablishmentLoginLink(c *gin.Context, admit linkEstablishment) {
	p, e, staffID, ok := loginLinkScope(c, admit)
	if !ok {
		return
	}
	id, ok := loginLinkID(c)
	if !ok {
		return
	}
	var in loginLinkInput
	if !decode(c, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if !validLoginSubject(in.Subject) || !validBenefitText(in.Reason, 1, 500) {
		respond.Error(c, 400, "invalid_request", "Enter a valid account ID and reason")
		return
	}
	if !h.loginCandidate(c, p, in.Subject) {
		return
	}
	var result loginLink
	err := h.loginLinkTransaction(c, p, e, staffID, "manage", func(tx *gorm.DB, person staff) error {
		current, err := loadLoginLink(tx, p, e.id, staffID, id)
		if err == nil {
			if current.Subject != in.Subject || current.ProposalReason != in.Reason || current.ProposedBySubject != p.Subject {
				return errLoginLinkConflict
			}
			result = current
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if person.EmploymentStatus != "active" {
			return errLoginLinkConflict
		}
		if err := tx.Exec(`INSERT INTO workforce_login_links(tenant_id,hospital_id,staff_id,id,subject,status,version,proposed_by_subject,proposal_reason,actor_subject,reason) VALUES(?,?,?,?,?,'pending',1,?,?,?,?)`, p.TenantID, e.id, staffID, id, in.Subject, p.Subject, in.Reason, p.Subject, in.Reason).Error; err != nil {
			return err
		}
		result, err = loadLoginLink(tx, p, e.id, staffID, id)
		if err != nil {
			return err
		}
		return h.publishLoginLink(tx, p, e, result)
	})
	if err != nil {
		respondLoginLinkError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) decideLoginLink(admit linkEstablishment) gin.HandlerFunc {
	return func(c *gin.Context) { h.decideEstablishmentLoginLink(c, admit) }
}

func (h *handlers) decideEstablishmentLoginLink(c *gin.Context, admit linkEstablishment) {
	p, e, staffID, ok := loginLinkScope(c, admit)
	if !ok {
		return
	}
	id, ok := loginLinkID(c)
	if !ok {
		return
	}
	var in loginLinkDecisionInput
	if !decode(c, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if !validBenefitText(in.Reason, 1, 500) {
		respond.Error(c, 400, "invalid_request", "Enter a reason")
		return
	}
	command := "manage"
	permission := PermLoginLinkManage
	if in.Decision == "approve" || in.Decision == "reject" {
		command = "review"
		permission = PermLoginLinkReview
	}
	set, _ := authz.PermissionsFrom(c)
	if !set.Has(permission) {
		respond.Forbidden(c, "Account link action is not permitted")
		return
	}
	var current loginLink
	err := h.loginLinkTransaction(c, p, e, staffID, "", func(tx *gorm.DB, _ staff) error {
		var err error
		current, err = loadLoginLink(tx, p, e.id, staffID, id)
		return err
	})
	if err != nil {
		respondLoginLinkError(c, err)
		return
	}
	if (command == "review" && (current.ProposedBySubject == p.Subject || current.Subject == p.Subject)) || (in.Decision == "withdraw" && current.ProposedBySubject != p.Subject) {
		respond.Forbidden(c, "This action requires a different authorized person")
		return
	}
	if in.Decision == "approve" && !h.loginCandidate(c, p, current.Subject) {
		return
	}
	next := map[string]string{"approve": "active", "reject": "rejected", "withdraw": "withdrawn", "revoke": "revoked"}[in.Decision]
	var result loginLink
	err = h.loginLinkTransaction(c, p, e, staffID, command, func(tx *gorm.DB, person staff) error {
		row, err := loadLoginLink(tx, p, e.id, staffID, id)
		if err != nil {
			return err
		}
		if row.Version == *in.ExpectedVersion+1 && row.Status == next && row.ActorSubject == p.Subject && row.Reason == in.Reason {
			result = row
			return nil
		}
		if row.Version != *in.ExpectedVersion || (row.Status != "pending" && (row.Status != "active" || next != "revoked")) {
			return errLoginLinkConflict
		}
		if next == "active" && person.EmploymentStatus != "active" {
			return errLoginLinkConflict
		}
		if err := tx.Exec(`UPDATE workforce_login_links SET status=?,version=version+1,actor_subject=?,reason=? WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND id=?`, next, p.Subject, in.Reason, p.TenantID, e.id, staffID, id).Error; err != nil {
			return err
		}
		result, err = loadLoginLink(tx, p, e.id, staffID, id)
		if err != nil {
			return err
		}
		return h.publishLoginLink(tx, p, e, result)
	})
	if err != nil {
		respondLoginLinkError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) listLoginLinks(admit linkEstablishment) func(*gin.Context, pagination.Params) ([]loginLink, error) {
	return func(c *gin.Context, params pagination.Params) ([]loginLink, error) {
		return h.listEstablishmentLoginLinks(c, admit, params)
	}
}

func (h *handlers) listEstablishmentLoginLinks(c *gin.Context, admit linkEstablishment, params pagination.Params) ([]loginLink, error) {
	p, e, staffID, ok := loginLinkScope(c, admit)
	if !ok {
		return nil, nil
	}
	var rows []loginLink
	err := h.loginLinkTransaction(c, p, e, staffID, "", func(tx *gorm.DB, _ staff) error {
		if err := tenantdb.ApplyKeyset(tx, params).Table("workforce_login_links").Select(loginLinkColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=?", p.TenantID, e.id, staffID).Find(&rows).Error; err != nil {
			return err
		}
		return loginLinkReadAudit(tx, p, e.id, staffID, "list")
	})
	if err != nil {
		respondLoginLinkError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) loginLinkHistory(admit linkEstablishment) func(*gin.Context, pagination.Params) ([]loginLinkHistory, error) {
	return func(c *gin.Context, params pagination.Params) ([]loginLinkHistory, error) {
		return h.establishmentLoginLinkHistory(c, admit, params)
	}
}

func (h *handlers) establishmentLoginLinkHistory(c *gin.Context, admit linkEstablishment, params pagination.Params) ([]loginLinkHistory, error) {
	p, e, staffID, ok := loginLinkScope(c, admit)
	if !ok {
		return nil, nil
	}
	id, ok := loginLinkID(c)
	if !ok {
		return nil, nil
	}
	var rows []loginLinkHistory
	err := h.loginLinkTransaction(c, p, e, staffID, "", func(tx *gorm.DB, _ staff) error {
		if _, err := loadLoginLink(tx, p, e.id, staffID, id); err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx, params).Table("workforce_login_link_history").Select("id,version,status,actor_subject,reason,created_at").Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, e.id, id).Find(&rows).Error; err != nil {
			return err
		}
		return loginLinkReadAudit(tx, p, e.id, id, "history")
	})
	if err != nil {
		respondLoginLinkError(c, err)
		return nil, nil
	}
	return rows, nil
}

func loginLinkReadAudit(tx *gorm.DB, p authn.Principal, hospital, id uuid.UUID, kind string) error {
	return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,'workforce.login_link.read',?,jsonb_build_object('view',?::text))`, p.TenantID, hospital, p.Subject, id, kind).Error
}

func (h *handlers) revokeInactiveStaffLinks(tx *gorm.DB, p authn.Principal, e establishment, staffID uuid.UUID) error {
	if err := tx.Exec(`SELECT set_config('app.workforce_login_link_access','authorized',true),set_config('app.workforce_login_link_command','inactivate',true)`).Error; err != nil {
		return err
	}
	var rows []loginLink
	if err := tx.Raw(`UPDATE workforce_login_links SET status='revoked',version=version+1,actor_subject=?,reason='Staff made inactive'
 WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND status IN ('pending','active') RETURNING `+loginLinkColumns,
		p.Subject, p.TenantID, e.id, staffID).Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if err := h.publishLoginLink(tx, p, e, row); err != nil {
			return err
		}
	}
	return nil
}

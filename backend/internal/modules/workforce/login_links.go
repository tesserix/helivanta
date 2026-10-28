package workforce

import (
	"encoding/json"
	"errors"
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

func loginLinkScope(c *gin.Context) (authn.Principal, uuid.UUID, uuid.UUID, bool) {
	p, hospital, ok := scope(c)
	if !ok {
		return p, hospital, uuid.Nil, false
	}
	set, _ := authz.PermissionsFrom(c)
	if !set.Has(PermLoginLinkManage) && !set.Has(PermLoginLinkReview) {
		respond.Forbidden(c, "Account link access is required")
		return p, hospital, uuid.Nil, false
	}
	if expected := c.Request.Header.Values("X-Helivanta-Expected-Tenant"); len(expected) > 0 {
		if len(expected) != 1 || expected[0] != p.TenantID {
			respond.NotFound(c, "staff account link")
			return p, hospital, uuid.Nil, false
		}
	}
	staff, err := uuid.Parse(c.Param("id"))
	if err != nil || staff == uuid.Nil {
		respond.NotFound(c, "staff")
		return p, hospital, uuid.Nil, false
	}
	return p, hospital, staff, true
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

func (h *handlers) loginLinkTransaction(c *gin.Context, p authn.Principal, hospital, staffID uuid.UUID, command string, fn func(*gorm.DB, staff) error) error {
	return h.transaction(c.Request.Context(), p, hospital, func(tx *gorm.DB) error {
		if err := requireStaffAccess(tx, p.TenantID, hospital, true); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.workforce_login_link_access','authorized',true),set_config('app.workforce_login_link_command',?,true)`, command).Error; err != nil {
			return err
		}
		query := tx.Table("workforce_staff").Select(staffColumns).Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, staffID)
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

func (h *handlers) publishLoginLink(tx *gorm.DB, p authn.Principal, hospital uuid.UUID, row loginLink) error {
	data, err := json.Marshal(workforcecontract.LoginLinkChangedData{HospitalID: hospital.String(), StaffID: row.StaffID.String(), LinkID: row.ID.String(), Version: row.Version, Status: row.Status})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, LoginLinkChanged, events.Event{Type: LoginLinkChanged, Version: 1, TenantID: p.TenantID, Data: data})
}

func respondLoginLinkError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	if errors.Is(err, errLoginLinkConflict) || (errors.As(err, &pg) && pg.Code == "23505") {
		respond.Error(c, 409, "link_conflict", "The account link changed. Refresh its history before continuing.")
		return
	}
	respondError(c, err)
}

func (h *handlers) proposeLoginLink(c *gin.Context) {
	p, hospital, staffID, ok := loginLinkScope(c)
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
	err := h.loginLinkTransaction(c, p, hospital, staffID, "manage", func(tx *gorm.DB, person staff) error {
		current, err := loadLoginLink(tx, p, hospital, staffID, id)
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
		if err := tx.Exec(`INSERT INTO workforce_login_links(tenant_id,hospital_id,staff_id,id,subject,status,version,proposed_by_subject,proposal_reason,actor_subject,reason) VALUES(?,?,?,?,?,'pending',1,?,?,?,?)`, p.TenantID, hospital, staffID, id, in.Subject, p.Subject, in.Reason, p.Subject, in.Reason).Error; err != nil {
			return err
		}
		result, err = loadLoginLink(tx, p, hospital, staffID, id)
		if err != nil {
			return err
		}
		return h.publishLoginLink(tx, p, hospital, result)
	})
	if err != nil {
		respondLoginLinkError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) decideLoginLink(c *gin.Context) {
	p, hospital, staffID, ok := loginLinkScope(c)
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
	err := h.loginLinkTransaction(c, p, hospital, staffID, "", func(tx *gorm.DB, _ staff) error {
		var err error
		current, err = loadLoginLink(tx, p, hospital, staffID, id)
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
	err = h.loginLinkTransaction(c, p, hospital, staffID, command, func(tx *gorm.DB, person staff) error {
		row, err := loadLoginLink(tx, p, hospital, staffID, id)
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
		if err := tx.Exec(`UPDATE workforce_login_links SET status=?,version=version+1,actor_subject=?,reason=? WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND id=?`, next, p.Subject, in.Reason, p.TenantID, hospital, staffID, id).Error; err != nil {
			return err
		}
		result, err = loadLoginLink(tx, p, hospital, staffID, id)
		if err != nil {
			return err
		}
		return h.publishLoginLink(tx, p, hospital, result)
	})
	if err != nil {
		respondLoginLinkError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) listLoginLinks(c *gin.Context, params pagination.Params) ([]loginLink, error) {
	p, hospital, staffID, ok := loginLinkScope(c)
	if !ok {
		return nil, nil
	}
	var rows []loginLink
	err := h.loginLinkTransaction(c, p, hospital, staffID, "", func(tx *gorm.DB, _ staff) error {
		if err := tenantdb.ApplyKeyset(tx, params).Table("workforce_login_links").Select(loginLinkColumns).Where("tenant_id=? AND hospital_id=? AND staff_id=?", p.TenantID, hospital, staffID).Find(&rows).Error; err != nil {
			return err
		}
		return loginLinkReadAudit(tx, p, hospital, staffID, "list")
	})
	if err != nil {
		respondLoginLinkError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) loginLinkHistory(c *gin.Context, params pagination.Params) ([]loginLinkHistory, error) {
	p, hospital, staffID, ok := loginLinkScope(c)
	if !ok {
		return nil, nil
	}
	id, ok := loginLinkID(c)
	if !ok {
		return nil, nil
	}
	var rows []loginLinkHistory
	err := h.loginLinkTransaction(c, p, hospital, staffID, "", func(tx *gorm.DB, _ staff) error {
		if _, err := loadLoginLink(tx, p, hospital, staffID, id); err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx, params).Table("workforce_login_link_history").Select("id,version,status,actor_subject,reason,created_at").Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, hospital, id).Find(&rows).Error; err != nil {
			return err
		}
		return loginLinkReadAudit(tx, p, hospital, id, "history")
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

func (h *handlers) revokeInactiveStaffLinks(tx *gorm.DB, p authn.Principal, hospital, staffID uuid.UUID) error {
	if err := tx.Exec(`SELECT set_config('app.workforce_login_link_access','authorized',true),set_config('app.workforce_login_link_command','inactivate',true)`).Error; err != nil {
		return err
	}
	var rows []loginLink
	if err := tx.Raw(`UPDATE workforce_login_links SET status='revoked',version=version+1,actor_subject=?,reason='Staff made inactive'
 WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND status IN ('pending','active') RETURNING `+loginLinkColumns,
		p.Subject, p.TenantID, hospital, staffID).Scan(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		if err := h.publishLoginLink(tx, p, hospital, row); err != nil {
			return err
		}
	}
	return nil
}

package workforce

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const PermIndependentLoginManage authz.Permission = "workforce.independent_login.manage"
const PermIndependentLoginReview authz.Permission = "workforce.independent_login.review"

//go:embed independent_identity.sql
var independentIdentitySchema string

func registerIndependentIdentityRoutes(r *platform.Router, h *handlers) {
	for _, kind := range []string{pharmacyEstablishmentKind, supplierEstablishmentKind} {
		family := r.Group("/workforce/" + map[string]string{"pharmacy": "pharmacies", "supplier": "suppliers"}[kind])
		root := family.Establishment(kind, kind, h.establishmentAdmission(kind))
		platform.ListRoute(root, "/:"+kind+"/staff/:id/account-links", authz.Public, h.listIndependentLinks)
		base := "/:" + kind + "/staff/:id/account-links/:link"
		root.PUT(base, PermIndependentLoginManage, h.proposeIndependentLink)
		root.GET(base, authz.Public, h.getIndependentLink)
		root.GET(base+"/proposal-receipt", authz.Public, h.getIndependentProposal)
		root.PUT(base+"/commands/:command", authz.Public, h.decideIndependentLink)
		root.GET(base+"/commands/:command", authz.Public, h.getIndependentCommand)
	}
}

func independentScope(c *gin.Context) (authn.Principal, establishment, uuid.UUID, bool) {
	c.Header("Cache-Control", "no-store")
	if !authn.Refresh(c) {
		return authn.Principal{}, establishment{}, uuid.Nil, false
	}
	p, _, ok := staffPrincipal(c)
	if !ok {
		return p, establishment{}, uuid.Nil, false
	}
	for header, expected := range map[string]string{"X-Helivanta-Expected-Tenant": p.TenantID, "X-Helivanta-Expected-Subject": p.Subject} {
		if values := c.Request.Header.Values(header); len(values) > 0 && (len(values) != 1 || values[0] != expected) {
			respond.NotFound(c, "staff account link")
			return p, establishment{}, uuid.Nil, false
		}
	}
	kind := supplierEstablishmentKind
	if c.Param("pharmacy") != "" {
		kind = pharmacyEstablishmentKind
	}
	id, err := uuid.Parse(c.Param(kind))
	staffID, staffErr := uuid.Parse(c.Param("id"))
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 || staffErr != nil || staffID == uuid.Nil || staffID.Variant() != uuid.RFC4122 {
		respond.NotFound(c, "staff account link")
		return p, establishment{}, uuid.Nil, false
	}
	set, refreshed := authz.RefreshPermissions(c)
	if !refreshed {
		return p, establishment{}, uuid.Nil, false
	}
	if !set.Has(PermIndependentLoginManage) && !set.Has(PermIndependentLoginReview) {
		respond.Forbidden(c, "Account link access is required")
		return p, establishment{}, uuid.Nil, false
	}
	return p, establishment{kind: kind, id: id}, staffID, true
}
func independentIdentityScope(p authn.Principal, root establishment, staffID uuid.UUID) IndependentIdentityScope {
	return IndependentIdentityScope{OwnerTenantID: uuid.MustParse(p.TenantID), EmployerKind: root.kind, EmployerID: root.id, StaffID: staffID}
}
func (h *handlers) independentTransaction(c *gin.Context, p authn.Principal, root establishment, staffID uuid.UUID, command string, fn func(*gorm.DB, staff) error) error {
	return h.transaction(c.Request.Context(), p, root.id, func(tx *gorm.DB) error {
		if err := root.requireKind(tx, p.TenantID); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "workforce.independent_identity:"+p.TenantID+":"+root.id.String()).Error; err != nil {
			return err
		}
		if err := lockDirectoryManager(tx, p, root.id); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.workforce_login_link_access','authorized',true),set_config('app.workforce_login_link_command',?,true)`, command).Error; err != nil {
			return err
		}
		query := tx.Table("workforce_staff").Select(staffColumns+",version").Where("tenant_id=? AND hospital_id=? AND id=?", p.TenantID, root.id, staffID)
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

func independentError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	if errors.Is(err, errLoginLinkConflict) || (errors.As(err, &pg) && (pg.Code == "23514" || pg.Code == "23505")) {
		respond.Error(c, 409, "link_conflict", "Refresh the Staff identity and account link before continuing.")
		return
	}
	respondError(c, err)
}
func (h *handlers) independentCandidate(c *gin.Context, p authn.Principal, subject string) bool {
	if h.membership == nil {
		respond.Error(c, 503, "identity_unavailable", "Account membership is unavailable.")
		return false
	}
	member, err := h.membership.IsMember(c.Request.Context(), subject, p.TenantID)
	if err != nil {
		respond.Error(c, 503, "identity_unavailable", "Account membership is unavailable.")
		return false
	}
	if !member {
		respond.NotFound(c, "account")
		return false
	}
	return true
}
func loadIndependentLink(tx *gorm.DB, p authn.Principal, root establishment, staffID, id uuid.UUID) (IndependentLinkView, error) {
	row, err := loadLoginLink(tx, p, root.id, staffID, id)
	if err != nil {
		return IndependentLinkView{}, err
	}
	var version int64
	err = tx.Table("workforce_login_links").Select("independent_staff_version").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, root.id, staffID, id).Take(&version).Error
	return IndependentLinkView{Scope: independentIdentityScope(p, root, staffID), StaffVersion: version, Link: IndependentLinkData(row)}, err
}
func independentProposal(view IndependentLinkView) IndependentLinkProposal {
	row := view.Link
	return IndependentLinkProposal{ID: row.ID, Scope: view.Scope, Input: IndependentLinkInput{ExpectedStaffVersion: view.StaffVersion, Subject: row.Subject, Reason: row.ProposalReason}, PreparedBy: row.ProposedBySubject, CreatedAt: row.CreatedAt}
}
func (h *handlers) proposeIndependentLink(c *gin.Context) {
	p, root, staffID, ok := independentScope(c)
	if !ok {
		return
	}
	id, ok := loginLinkID(c)
	if !ok {
		return
	}
	var in IndependentLinkInput
	if !decode(c, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.ExpectedStaffVersion < 1 || in.ExpectedStaffVersion > 9007199254740991 || !validLoginSubject(in.Subject) || !validBenefitText(in.Reason, 1, 500) {
		respond.Error(c, 400, "invalid_request", "Enter the current Staff version, account and reason.")
		return
	}

	if !authz.Refresh(c, PermIndependentLoginManage) {
		return
	}
	var result IndependentLinkProposal
	err := h.independentTransaction(c, p, root, staffID, "", func(tx *gorm.DB, _ staff) error {
		view, err := loadIndependentLink(tx, p, root, staffID, id)
		if err != nil {
			return err
		}
		result = independentProposal(view)
		if result.Input != in || result.PreparedBy != p.Subject {
			return errLoginLinkConflict
		}
		return loginLinkReadAudit(tx, p, root.id, id, "proposal_recovery")
	})
	if err == nil {
		respond.OK(c, result)
		return
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		independentError(c, err)
		return
	}
	if !h.independentCandidate(c, p, in.Subject) || !h.independentCandidate(c, p, p.Subject) || !authn.Refresh(c) || !authz.Refresh(c, PermIndependentLoginManage) {
		return
	}
	created := false
	err = h.independentTransaction(c, p, root, staffID, "manage", func(tx *gorm.DB, person staff) error {
		view, err := loadIndependentLink(tx, p, root, staffID, id)
		if err == nil {
			result = independentProposal(view)
			if result.Input != in || result.PreparedBy != p.Subject {
				return errLoginLinkConflict
			}
			return loginLinkReadAudit(tx, p, root.id, id, "proposal_recovery")
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if person.Version != in.ExpectedStaffVersion || person.EmploymentStatus != "active" {
			return errLoginLinkConflict
		}
		if err := tx.Exec(`INSERT INTO workforce_login_links(tenant_id,hospital_id,staff_id,id,subject,status,version,proposed_by_subject,proposal_reason,actor_subject,reason,independent_staff_version)VALUES(?,?,?,?,?,'pending',1,?,?,?,?,?)`, p.TenantID, root.id, staffID, id, in.Subject, p.Subject, in.Reason, p.Subject, in.Reason, in.ExpectedStaffVersion).Error; err != nil {
			return err
		}
		view, err = loadIndependentLink(tx, p, root, staffID, id)
		if err != nil {
			return err
		}
		result = independentProposal(view)
		created = true
		return h.publishLoginLink(tx, p, root.id, loginLink(view.Link))
	})
	if err != nil {
		independentError(c, err)
		return
	}
	if created {
		respond.Created(c, result)
	} else {
		respond.OK(c, result)
	}
}
func (h *handlers) getIndependentLink(c *gin.Context)     { h.readIndependentLink(c, false) }
func (h *handlers) getIndependentProposal(c *gin.Context) { h.readIndependentLink(c, true) }
func (h *handlers) readIndependentLink(c *gin.Context, proposal bool) {
	p, root, staffID, ok := independentScope(c)
	if !ok {
		return
	}
	id, ok := loginLinkID(c)
	if !ok {
		return
	}
	var result IndependentLinkView
	err := h.independentTransaction(c, p, root, staffID, "", func(tx *gorm.DB, _ staff) error {
		var err error
		result, err = loadIndependentLink(tx, p, root, staffID, id)
		if err != nil {
			return err
		}
		return loginLinkReadAudit(tx, p, root.id, id, "independent_identity")
	})
	if err != nil {
		independentError(c, err)
		return
	}
	if proposal {
		respond.OK(c, independentProposal(result))
	} else {
		respond.OK(c, result)
	}
}
func independentCommandID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("command"))
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 {
		respond.NotFound(c, "account link command")
		return uuid.Nil, false
	}
	return id, true
}
func readIndependentCommand(tx *gorm.DB, p authn.Principal, root establishment, staffID, linkID, id uuid.UUID) (IndependentLinkCommand, error) {
	var row struct {
		ActorSubject string
		Receipt      json.RawMessage
	}
	err := tx.Table("workforce_independent_link_commands").Select("actor_subject,receipt").Where("tenant_id=? AND employer_id=? AND employer_kind=? AND staff_id=? AND link_id=? AND id=?", p.TenantID, root.id, root.kind, staffID, linkID, id).Take(&row).Error
	if err != nil {
		return IndependentLinkCommand{}, err
	}
	if row.ActorSubject != p.Subject {
		return IndependentLinkCommand{}, gorm.ErrRecordNotFound
	}
	var result IndependentLinkCommand
	err = json.Unmarshal(row.Receipt, &result)
	return result, err
}
func (h *handlers) getIndependentCommand(c *gin.Context) {
	p, root, staffID, ok := independentScope(c)
	if !ok {
		return
	}
	linkID, ok := loginLinkID(c)
	if !ok {
		return
	}
	id, ok := independentCommandID(c)
	if !ok {
		return
	}
	var result IndependentLinkCommand
	err := h.independentTransaction(c, p, root, staffID, "", func(tx *gorm.DB, _ staff) error {
		var err error
		result, err = readIndependentCommand(tx, p, root, staffID, linkID, id)
		if err != nil {
			return err
		}
		return loginLinkReadAudit(tx, p, root.id, id, "independent_command_recovery")
	})
	if err != nil {
		independentError(c, err)
		return
	}
	respond.OK(c, result)
}
func (h *handlers) decideIndependentLink(c *gin.Context) {
	p, root, staffID, ok := independentScope(c)
	if !ok {
		return
	}
	linkID, ok := loginLinkID(c)
	if !ok {
		return
	}
	id, ok := independentCommandID(c)
	if !ok {
		return
	}
	var in IndependentLinkDecisionInput
	if !decode(c, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.ExpectedVersion < 1 || in.ExpectedVersion > 9007199254740990 || !validBenefitText(in.Reason, 1, 500) {
		respond.Error(c, 400, "invalid_request", "Enter the current link version and reason.")
		return
	}
	permission := PermIndependentLoginManage
	command := "manage"
	if in.Decision == "approve" || in.Decision == "reject" {
		permission = PermIndependentLoginReview
		command = "review"
	}
	set, _ := authz.PermissionsFrom(c)
	if !set.Has(permission) {
		respond.Forbidden(c, "Account link action is not permitted")
		return
	}
	// Recover durable commands before rechecking mutable target membership.
	var result IndependentLinkCommand
	err := h.independentTransaction(c, p, root, staffID, "", func(tx *gorm.DB, _ staff) error {
		var err error
		result, err = readIndependentCommand(tx, p, root, staffID, linkID, id)
		return err
	})
	if err == nil {
		if result.Input != in {
			independentError(c, errLoginLinkConflict)
			return
		}
		respond.OK(c, result)
		return
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		independentError(c, err)
		return
	}
	var current IndependentLinkView
	err = h.independentTransaction(c, p, root, staffID, "", func(tx *gorm.DB, _ staff) error {
		var err error
		current, err = loadIndependentLink(tx, p, root, staffID, linkID)
		return err
	})
	if err != nil {
		independentError(c, err)
		return
	}
	if in.Decision == "approve" && !h.independentCandidate(c, p, current.Link.Subject) {
		return
	}
	if !h.independentCandidate(c, p, p.Subject) || !authn.Refresh(c) || !authz.Refresh(c, permission) {
		return
	}
	created := false
	err = h.independentTransaction(c, p, root, staffID, command, func(tx *gorm.DB, person staff) error {
		existing, err := readIndependentCommand(tx, p, root, staffID, linkID, id)
		if err == nil {
			if existing.Input != in {
				return errLoginLinkConflict
			}
			result = existing
			return loginLinkReadAudit(tx, p, root.id, id, "independent_command_replay")
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		view, err := loadIndependentLink(tx, p, root, staffID, linkID)
		if err != nil {
			return err
		}
		row := view.Link
		next := map[string]string{"approve": "active", "reject": "rejected", "withdraw": "withdrawn", "revoke": "revoked"}[in.Decision]
		if next == "" || row.Version != in.ExpectedVersion || (row.Status != "pending" && (row.Status != "active" || next != "revoked")) {
			return errLoginLinkConflict
		}
		if command == "review" && (row.Subject == p.Subject || row.ProposedBySubject == p.Subject) {
			return errLoginLinkConflict
		}
		if in.Decision == "withdraw" && row.ProposedBySubject != p.Subject {
			return errLoginLinkConflict
		}
		if next == "active" && (person.EmploymentStatus != "active" || person.Version != view.StaffVersion) {
			return errLoginLinkConflict
		}
		if err := tx.Exec(`UPDATE workforce_login_links SET status=?,version=version+1,actor_subject=?,reason=? WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND id=?`, next, p.Subject, in.Reason, p.TenantID, root.id, staffID, linkID).Error; err != nil {
			return err
		}
		view, err = loadIndependentLink(tx, p, root, staffID, linkID)
		if err != nil {
			return err
		}
		result = IndependentLinkCommand{ID: id, LinkID: linkID, Scope: view.Scope, Input: in, Result: view, ActorSubject: p.Subject, CreatedAt: view.Link.UpdatedAt}
		receipt, err := json.Marshal(result)
		if err != nil {
			return err
		}
		input, err := json.Marshal(in)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(input)
		if err := tx.Exec(`INSERT INTO workforce_independent_link_commands(tenant_id,employer_id,employer_kind,staff_id,link_id,id,actor_subject,expected_version,input_hash,receipt,created_at)VALUES(?,?,?,?,?,?,?,?,?,?::jsonb,?)`, p.TenantID, root.id, root.kind, staffID, linkID, id, p.Subject, in.ExpectedVersion, hex.EncodeToString(hash[:]), string(receipt), result.CreatedAt).Error; err != nil {
			return err
		}
		created = true
		return h.publishLoginLink(tx, p, root.id, loginLink(view.Link))
	})
	if err != nil {
		independentError(c, err)
		return
	}
	if created {
		respond.Created(c, result)
	} else {
		respond.OK(c, result)
	}
}

func (v IndependentLinkView) PageKey() (time.Time, uuid.UUID) { return v.Link.CreatedAt, v.Link.ID }
func (h *handlers) listIndependentLinks(c *gin.Context, page pagination.Params) ([]IndependentLinkView, error) {
	p, root, staffID, ok := independentScope(c)
	if !ok {
		return nil, nil
	}
	var rows []struct {
		Link         IndependentLinkData `gorm:"embedded"`
		StaffVersion int64
	}
	err := h.independentTransaction(c, p, root, staffID, "", func(tx *gorm.DB, _ staff) error {
		if err := tenantdb.ApplyKeyset(tx, page).Table("workforce_login_links").Select(loginLinkColumns+",independent_staff_version AS staff_version").Where("tenant_id=? AND hospital_id=? AND staff_id=?", p.TenantID, root.id, staffID).Find(&rows).Error; err != nil {
			return err
		}
		return loginLinkReadAudit(tx, p, root.id, staffID, "independent_list")
	})
	if err != nil {
		independentError(c, err)
		return nil, nil
	}
	result := make([]IndependentLinkView, 0, len(rows))
	for _, row := range rows {
		result = append(result, IndependentLinkView{Scope: independentIdentityScope(p, root, staffID), StaffVersion: row.StaffVersion, Link: row.Link})
	}
	return result, nil
}

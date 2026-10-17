package supply

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	supplycontract "github.com/tesserix/helivanta/internal/modules/supply/contract" //nolint:depguard // own data-only event contract
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type supportAccess struct {
	ID                 uuid.UUID `json:"id"`
	TenantID           uuid.UUID `json:"-"`
	PartyID            uuid.UUID `json:"party_id"`
	PremiseID          uuid.UUID `json:"premise_id"`
	RequesterSubject   string    `json:"requester_subject"`
	BeneficiarySubject string    `json:"beneficiary_subject"`
	TicketReference    string    `json:"ticket_reference"`
	Reason             string    `json:"reason"`
	ValidFrom          time.Time `json:"valid_from"`
	ExpiresAt          time.Time `json:"expires_at"`
	Status             string    `json:"status"`
	ApproverSubject    *string   `json:"approver_subject"`
	DecisionActor      string    `json:"decision_actor"`
	DecisionReason     string    `json:"decision_reason"`
	Version            int64     `json:"version"`
	CreatedAt          time.Time `json:"created_at" gorm:"autoCreateTime:false;default:now()"`
	UpdatedAt          time.Time `json:"updated_at" gorm:"autoUpdateTime:false;default:now()"`
	EffectiveStatus    string    `json:"effective_status" gorm:"-"`
	AllowedActions     []string  `json:"allowed_actions" gorm:"-"`
}

func (supportAccess) TableName() string                 { return "supply_support_access" }
func (r supportAccess) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

const supportColumns = "id,tenant_id,party_id,premise_id,requester_subject,beneficiary_subject,ticket_reference,reason,valid_from,expires_at,status,approver_subject,decision_actor,decision_reason,version,created_at,updated_at"

var errSupportIndependent = errors.New("independent support reviewer required")
var errSupportMembership = errors.New("support beneficiary membership unavailable")

func registerSupportAccessRoutes(g *platform.Router, h *handlers) {
	g.PUT("/parties/:party/support/:support", PermSupportManage, h.putSupportAccess)
	g.POST("/parties/:party/support/:support/decision", PermSupportManage, h.decideSupportAccess)
	g.POST("/parties/:party/support/:support/revoke", PermSupportRevoke, h.revokeSupportAccess)
	g.GET("/parties/:party/support/:support", PermSupportRead, h.getSupportAccess)
	g.GET("/parties/:party/support/:support/diagnostics", PermSupportRead, h.getSupportDiagnostics)
	platform.ListRoute(g, "/parties/:party/support", PermSupportRead, h.listSupportAccess)
	platform.ListRoute(g, "/parties/:party/support/:support/revisions", PermSupportRead, h.listSupportRevisions)
}
func supportError(c *gin.Context, e error) {
	if errors.Is(e, errSupportIndependent) {
		respond.Forbidden(c, "An independent organisation administrator must review this request.")
		return
	}
	if errors.Is(e, errSupportMembership) {
		respond.Error(c, 503, "identity_unavailable", "Support beneficiary verification is temporarily unavailable.")
		return
	}
	var pg *pgconn.PgError
	if errors.As(e, &pg) && pg.ConstraintName == "supply_support_transition" {
		respond.Conflict(c, "The support request changed or this transition is unavailable.")
		return
	}
	supplierError(c, e)
}
func (h *handlers) verifySupportMember(c *gin.Context, p authn.Principal, subject string) error {
	if h.roles == nil {
		return errSupportMembership
	}
	roles, e := h.roles.ListRoles(c.Request.Context(), subject)
	if e != nil {
		return errSupportMembership
	}
	for _, role := range roles {
		if role.TenantID == p.TenantID {
			return nil
		}
	}
	return gorm.ErrRecordNotFound
}

func (h *handlers) verifySupportApprover(c *gin.Context, p authn.Principal, subject string) error {
	if h.roles == nil {
		return errSupportMembership
	}
	bindings, err := h.roles.ListRoles(c.Request.Context(), subject)
	if err != nil {
		return errSupportMembership
	}
	for _, binding := range bindings {
		if binding.TenantID != p.TenantID {
			continue
		}
		if binding.Role == authz.RoleTenantAdmin {
			return nil
		}
		for _, permission := range New().Permissions() {
			if permission.Permission != PermSupportManage {
				continue
			}
			for _, role := range permission.Roles {
				if binding.Role == role {
					return nil
				}
			}
		}
	}
	return gorm.ErrRecordNotFound
}

func (h *handlers) refreshSupportApproval(c *gin.Context, p authn.Principal, row *supportAccess, verified map[string]bool) error {
	if row.ApproverSubject == nil || (row.EffectiveStatus != "active" && row.EffectiveStatus != "scheduled") {
		return nil
	}
	subject := *row.ApproverSubject
	allowed, found := verified[subject]
	if !found {
		err := h.verifySupportApprover(c, p, subject)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		allowed = err == nil
		verified[subject] = allowed
	}
	if !allowed {
		row.EffectiveStatus = "approval_withdrawn"
		actions := row.AllowedActions[:0]
		for _, action := range row.AllowedActions {
			if action != "read_diagnostics" {
				actions = append(actions, action)
			}
		}
		row.AllowedActions = actions
	}
	return nil
}
func supportFromInput(in supportRequestInput) (supportAccess, bool) {
	from, e := time.Parse(time.RFC3339, in.ValidFrom)
	if e != nil {
		return supportAccess{}, false
	}
	until, e := time.Parse(time.RFC3339, in.ExpiresAt)
	if e != nil {
		return supportAccess{}, false
	}
	valid := in.ExpectedVersion != nil && *in.ExpectedVersion >= 0 && cleanText(&in.TicketReference, &in.Reason) && binding.Validator.ValidateStruct(&in) == nil && in.PremiseID.Version() == 7 && in.PremiseID.Variant() == uuid.RFC4122 && subjectID.MatchString(in.BeneficiarySubject) && until.After(from) && until.Sub(from) <= 24*time.Hour
	return supportAccess{PremiseID: in.PremiseID, BeneficiarySubject: in.BeneficiarySubject, TicketReference: in.TicketReference, Reason: in.Reason, ValidFrom: from.UTC(), ExpiresAt: until.UTC()}, valid
}
func sameSupportRequest(row, desired supportAccess) bool {
	return row.PremiseID == desired.PremiseID && row.BeneficiarySubject == desired.BeneficiarySubject && row.TicketReference == desired.TicketReference && row.Reason == desired.Reason && row.ValidFrom.Equal(desired.ValidFrom) && row.ExpiresAt.Equal(desired.ExpiresAt)
}
func (h *handlers) supportSources(tx *gorm.DB, p authn.Principal, partyID, premiseID uuid.UUID) error {
	var header struct{ ID uuid.UUID }
	if e := tx.Table("supply_parties").Select("id").Where("tenant_id=? AND id=? AND record_kind='owned_organisation'", p.TenantID, partyID).First(&header).Error; e != nil {
		return e
	}
	var location struct{ ID uuid.UUID }
	return tx.Table("supply_premises").Select("id").Where("tenant_id=? AND party_id=? AND id=? AND status='active'", p.TenantID, partyID, premiseID).First(&location).Error
}
func (h *handlers) putSupportAccess(c *gin.Context) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "support")
	if !ok {
		return
	}
	var in supportRequestInput
	if !decode(c, &in) {
		return
	}
	desired, valid := supportFromInput(in)
	if !valid {
		respond.Error(c, 400, "invalid_request", "Provide a verified support beneficiary, location, ticket, reason and access window up to 24 hours.")
		return
	}
	e := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if e := requireAccessAdministrator(tx, p, partyID); e != nil {
			return e
		}
		return h.supportSources(tx, p, partyID, desired.PremiseID)
	})
	if e != nil {
		supportError(c, e)
		return
	}
	if e := h.verifySupportMember(c, p, desired.BeneficiarySubject); e != nil {
		supportError(c, e)
		return
	}
	var saved supportAccess
	e = h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if e := lockAccessAdministration(tx, p, partyID); e != nil {
			return e
		}
		if e := requireAccessAdministrator(tx, p, partyID); e != nil {
			return e
		}
		e := tx.Select(supportColumns).Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, id).First(&saved).Error
		if e == nil {
			if saved.RequesterSubject == p.Subject && sameSupportRequest(saved, desired) && *in.ExpectedVersion == 0 {
				return h.decorateSupport(tx, p, &saved)
			}
			return errConflict
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		if *in.ExpectedVersion != 0 {
			return gorm.ErrRecordNotFound
		}
		if !desired.ExpiresAt.After(time.Now()) {
			return errConflict
		}
		if e := h.supportSources(tx, p, partyID, desired.PremiseID); e != nil {
			return e
		}
		desired.ID = id
		desired.TenantID = uuid.MustParse(p.TenantID)
		desired.PartyID = partyID
		desired.RequesterSubject = p.Subject
		desired.Status = "pending"
		desired.Version = 1
		if e := tx.Create(&desired).Error; e != nil {
			return e
		}
		if e := tx.Select(supportColumns).Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, id).First(&saved).Error; e != nil {
			return e
		}
		if e := h.recordSupport(tx, p, saved, "requested"); e != nil {
			return e
		}
		return h.decorateSupport(tx, p, &saved)
	})
	if e != nil {
		supportError(c, e)
		return
	}
	respond.OK(c, saved)
}
func (h *handlers) loadSupport(tx *gorm.DB, p authn.Principal, partyID, id uuid.UUID, lock bool) (supportAccess, error) {
	var row supportAccess
	q := tx
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	e := q.Select(supportColumns).Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, id).First(&row).Error
	return row, e
}
func supportDecisionIndependent(row supportAccess, actor, action string) error {
	if (actor == row.BeneficiarySubject && action != "revoke") || ((action == "approve" || action == "reject") && actor == row.RequesterSubject) {
		return errSupportIndependent
	}
	return nil
}
func (h *handlers) decideSupportAccess(c *gin.Context) { h.applySupportDecision(c, false) }
func (h *handlers) revokeSupportAccess(c *gin.Context) { h.applySupportDecision(c, true) }
func (h *handlers) applySupportDecision(c *gin.Context, beneficiaryRevoke bool) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "support")
	if !ok {
		return
	}
	var in supportDecisionInput
	if beneficiaryRevoke {
		var revoke supportRevokeInput
		if !decode(c, &revoke) {
			return
		}
		in = supportDecisionInput{ExpectedVersion: revoke.ExpectedVersion, Action: "revoke", Reason: revoke.Reason}
	} else {
		if !decode(c, &in) {
			return
		}
	}
	if !cleanText(&in.Reason) || binding.Validator.ValidateStruct(&in) != nil {
		respond.Error(c, 400, "invalid_request", "Provide the current version, decision and a review reason.")
		return
	}

	if in.Action == "approve" {
		var preflight supportAccess
		e := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
			if e := requireAccessAdministrator(tx, p, partyID); e != nil {
				return e
			}
			var e error
			preflight, e = h.loadSupport(tx, p, partyID, id, false)
			if e != nil {
				return e
			}
			return supportDecisionIndependent(preflight, p.Subject, in.Action)
		})
		if e != nil {
			supportError(c, e)
			return
		}
		if e := h.verifySupportMember(c, p, preflight.BeneficiarySubject); e != nil {
			supportError(c, e)
			return
		}
	}
	var saved supportAccess
	e := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if e := lockAccessAdministration(tx, p, partyID); e != nil {
			return e
		}
		var e error
		saved, e = h.loadSupport(tx, p, partyID, id, true)
		if e != nil {
			return e
		}
		if beneficiaryRevoke && saved.BeneficiarySubject == p.Subject {
			if e := tx.Exec(`SELECT set_config('app.supply_support_revoke',?,true)`, id.String()).Error; e != nil {
				return e
			}
		} else {
			if e := requireAccessAdministrator(tx, p, partyID); e != nil {
				return e
			}
		}

		if e := supportDecisionIndependent(saved, p.Subject, in.Action); e != nil {
			return e
		}
		target := map[string]string{"approve": "approved", "reject": "rejected", "revoke": "revoked"}[in.Action]
		if saved.Status == target && saved.DecisionActor == p.Subject && saved.DecisionReason == in.Reason && *in.ExpectedVersion == saved.Version-1 {
			return h.decorateSupport(tx, p, &saved)
		}
		canDecide := saved.Status == "pending" || (saved.Status == "approved" && target == "revoked")
		if *in.ExpectedVersion != saved.Version || !canDecide {
			return errConflict
		}
		updates := map[string]any{"status": target, "decision_actor": p.Subject, "decision_reason": in.Reason, "version": saved.Version + 1}
		if in.Action == "approve" {
			if e := boundDelegatedAccess(tx, p, partyID, saved.ExpiresAt); e != nil {
				return e
			}
			if !saved.ExpiresAt.After(time.Now()) {
				return errConflict
			}
			if e := h.supportSources(tx, p, partyID, saved.PremiseID); e != nil {
				return e
			}
			updates["approver_subject"] = p.Subject
		}
		if e := tx.Model(&supportAccess{}).Where("tenant_id=? AND party_id=? AND id=? AND version=?", p.TenantID, partyID, id, saved.Version).Updates(updates).Error; e != nil {
			return e
		}
		saved, e = h.loadSupport(tx, p, partyID, id, false)
		if e != nil {
			return e
		}
		if e := h.recordSupport(tx, p, saved, target); e != nil {
			return e
		}
		return h.decorateSupport(tx, p, &saved)
	})
	if e != nil {
		supportError(c, e)
		return
	}
	respond.OK(c, saved)
}
func (h *handlers) decorateSupport(tx *gorm.DB, p authn.Principal, row *supportAccess) error {
	row.EffectiveStatus = row.Status
	row.AllowedActions = []string{}
	now := time.Now()
	approved := false
	if row.Status == "approved" {
		if row.ApproverSubject == nil {
			return errors.New("support approval unavailable")
		}
		if e := tx.Raw(`SELECT supply_effective_role(?,?,?,ARRAY['administrator'],NULL)`, p.TenantID, row.PartyID, *row.ApproverSubject).Scan(&approved).Error; e != nil {
			return e
		}
		switch {
		case !row.ExpiresAt.After(now):
			row.EffectiveStatus = "expired"
		case !approved:
			row.EffectiveStatus = "approval_withdrawn"
		case row.ValidFrom.After(now):
			row.EffectiveStatus = "scheduled"
		default:
			row.EffectiveStatus = "active"
		}
	} else if row.Status == "pending" && !row.ExpiresAt.After(now) {
		row.EffectiveStatus = "expired"
	}
	var administrator bool
	if e := tx.Raw(`SELECT supply_effective_role(?,?,?,ARRAY['administrator'],NULL)`, p.TenantID, row.PartyID, p.Subject).Scan(&administrator).Error; e != nil {
		return e
	}
	if administrator && p.Subject != row.BeneficiarySubject {
		if row.Status == "pending" && p.Subject != row.RequesterSubject {
			if row.ExpiresAt.After(now) {
				row.AllowedActions = append(row.AllowedActions, "approve")
			}
			row.AllowedActions = append(row.AllowedActions, "reject")
		}
		if row.Status == "pending" || row.Status == "approved" {
			row.AllowedActions = append(row.AllowedActions, "revoke")
		}
	}
	if (row.Status == "pending" || row.Status == "approved") && p.Subject == row.BeneficiarySubject {
		row.AllowedActions = append(row.AllowedActions, "revoke")
	}
	if row.EffectiveStatus == "active" && p.Subject == row.BeneficiarySubject {
		row.AllowedActions = append(row.AllowedActions, "read_diagnostics")
	}
	return nil
}
func (h *handlers) recordSupport(tx *gorm.DB, p authn.Principal, row supportAccess, operation string) error {
	snapshot, e := json.Marshal(row)
	if e != nil {
		return e
	}
	var fields map[string]any
	if e := json.Unmarshal(snapshot, &fields); e != nil {
		return e
	}
	delete(fields, "allowed_actions")
	delete(fields, "effective_status")
	snapshot, e = json.Marshal(fields)
	if e != nil {
		return e
	}
	revision, e := uuid.NewV7()
	if e != nil {
		return e
	}
	if e := tx.Table("supply_support_access_revisions").Create(map[string]any{"tenant_id": p.TenantID, "party_id": row.PartyID, "support_id": row.ID, "id": revision, "version": row.Version, "snapshot": string(snapshot), "actor_subject": p.Subject}).Error; e != nil {
		return e
	}
	data, e := json.Marshal(supplycontract.SupportAccessChangedData{PartyID: row.PartyID.String(), SupportID: row.ID.String(), Version: row.Version, Operation: operation})
	if e != nil {
		return e
	}
	return h.bus.Publish(tx, supplycontract.SubjectSupportAccessChanged, events.Event{Type: "SupplySupportAccessChanged", Version: 1, TenantID: p.TenantID, Data: data})
}
func (h *handlers) getSupportAccess(c *gin.Context) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "support")
	if !ok {
		return
	}
	var row supportAccess
	e := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		var e error
		row, e = h.loadSupport(tx, p, partyID, id, false)
		if e != nil {
			return e
		}
		if e := h.decorateSupport(tx, p, &row); e != nil {
			return e
		}
		return supportReadAudit(tx, p, row.ID, "support_metadata_read")
	})
	if e != nil {
		supportError(c, e)
		return
	}
	if e := h.refreshSupportApproval(c, p, &row, map[string]bool{}); e != nil {
		supportError(c, e)
		return
	}
	respond.OK(c, row)
}
func (h *handlers) listSupportAccess(c *gin.Context, params pagination.Params) ([]supportAccess, error) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return nil, nil
	}
	var rows []supportAccess
	e := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if e := tenantdb.ApplyKeyset(tx, params).Select(supportColumns).Where("tenant_id=? AND party_id=?", p.TenantID, partyID).Find(&rows).Error; e != nil {
			return e
		}
		for i := range rows {
			if e := h.decorateSupport(tx, p, &rows[i]); e != nil {
				return e
			}
			if e := supportReadAudit(tx, p, rows[i].ID, "support_metadata_read"); e != nil {
				return e
			}
		}
		return nil
	})
	if e == nil {
		verified := map[string]bool{}
		for i := range rows {
			if e = h.refreshSupportApproval(c, p, &rows[i], verified); e != nil {
				break
			}
		}
	}
	if e != nil {
		supportError(c, e)
		return nil, e
	}
	return rows, nil
}
func (h *handlers) listSupportRevisions(c *gin.Context, params pagination.Params) ([]registryRevision, error) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return nil, nil
	}
	id, ok := resourceID(c, "support")
	if !ok {
		return nil, nil
	}
	var rows []registryRevision
	e := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if e := requireAccessAdministrator(tx, p, partyID); e != nil {
			return e
		}
		if _, e := h.loadSupport(tx, p, partyID, id, false); e != nil {
			return e
		}
		if e := supportReadAudit(tx, p, id, "support_review_read"); e != nil {
			return e
		}
		return tenantdb.ApplyKeyset(tx, params).Table("supply_support_access_revisions").Select("id,party_id,support_id AS record_id,version,snapshot,actor_subject,created_at").Where("tenant_id=? AND party_id=? AND support_id=?", p.TenantID, partyID, id).Find(&rows).Error
	})
	if e != nil {
		supportError(c, e)
	}
	return rows, nil
}

type supportDiagnostics struct {
	SupportID          uuid.UUID `json:"support_id"`
	PartyID            uuid.UUID `json:"party_id"`
	PremiseID          uuid.UUID `json:"premise_id"`
	PremiseKind        string    `json:"premise_kind"`
	PremiseStatus      string    `json:"premise_status"`
	BinCount           int64     `json:"bin_count"`
	ActiveBinCount     int64     `json:"active_bin_count"`
	QuarantineBinCount int64     `json:"quarantine_bin_count"`
}

func (h *handlers) supportSession(tx *gorm.DB, p authn.Principal, partyID, id uuid.UUID) (supportAccess, error) {
	row, e := h.loadSupport(tx, p, partyID, id, false)
	if e != nil {
		return row, e
	}
	if row.BeneficiarySubject != p.Subject {
		return row, gorm.ErrRecordNotFound
	}
	if e := h.decorateSupport(tx, p, &row); e != nil {
		return row, e
	}
	if row.EffectiveStatus != "active" {
		return row, gorm.ErrRecordNotFound
	}
	return row, nil
}
func (h *handlers) getSupportDiagnostics(c *gin.Context) {
	p, partyID, ok := registryScope(c)
	if !ok {
		return
	}
	id, ok := resourceID(c, "support")
	if !ok {
		return
	}
	var preflight supportAccess
	e := h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error { var e error; preflight, e = h.supportSession(tx, p, partyID, id); return e })
	if e != nil {
		supportError(c, e)
		return
	}
	if e := h.verifySupportMember(c, p, p.Subject); e != nil {
		supportError(c, e)
		return
	}
	if preflight.ApproverSubject == nil {
		respond.NotFound(c, "support")
		return
	}
	if e := h.verifySupportApprover(c, p, *preflight.ApproverSubject); e != nil {
		supportError(c, e)
		return
	}

	var result supportDiagnostics
	e = h.transaction(c.Request.Context(), p, partyID, false, false, func(tx *gorm.DB) error {
		if e := lockAccessAdministration(tx, p, partyID); e != nil {
			return e
		}
		row, e := h.supportSession(tx, p, partyID, id)
		if e != nil {
			return e
		}
		if e := tx.Exec(`SELECT set_config('app.supply_support_id',?,true)`, row.ID.String()).Error; e != nil {
			return e
		}
		result = supportDiagnostics{SupportID: row.ID, PartyID: partyID, PremiseID: row.PremiseID}
		var location struct{ Kind, Status string }
		if e := tx.Table("supply_premises").Select("kind,status").Where("tenant_id=? AND party_id=? AND id=?", p.TenantID, partyID, row.PremiseID).First(&location).Error; e != nil {
			return e
		}
		result.PremiseKind = location.Kind
		result.PremiseStatus = location.Status
		var counts struct{ BinCount, ActiveBinCount, QuarantineBinCount int64 }
		if e := tx.Table("supply_bins").Select("count(*) AS bin_count,count(*) FILTER(WHERE status='active') AS active_bin_count,count(*) FILTER(WHERE storage_class='quarantine') AS quarantine_bin_count").Where("tenant_id=? AND party_id=? AND premise_id=?", p.TenantID, partyID, row.PremiseID).Scan(&counts).Error; e != nil {
			return e
		}
		result.BinCount = counts.BinCount
		result.ActiveBinCount = counts.ActiveBinCount
		result.QuarantineBinCount = counts.QuarantineBinCount
		return supportReadAudit(tx, p, row.ID, "support_read")
	})
	if e != nil {
		supportError(c, e)
		return
	}
	respond.OK(c, result)
}

func supportReadAudit(tx *gorm.DB, p authn.Principal, id uuid.UUID, action string) error {
	return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,?,'supply_support_access',?)`, p.TenantID, p.Subject, action, id).Error
}

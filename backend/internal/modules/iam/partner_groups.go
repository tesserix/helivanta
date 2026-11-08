package iam

import (
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

//go:embed partner_groups.sql
var partnerGroupsSchema string

var errPartnerGroupConflict = errors.New("partner group proposal content changed")

type partnerGroupHandlers struct {
	db  *tenantdb.DB
	bus *events.Bus
}

func (m *Module) registerPartnerGroups(r *platform.Router, deps platform.Deps) {
	h := &partnerGroupHandlers{db: deps.DB, bus: deps.Bus}
	r.PUT("/partner-groups/:id", PermAssignmentManage, h.put)
	r.GET("/partner-groups/:id", PermAssignmentManage, h.get)
	platform.ListRoute(r, "/partner-groups", PermAssignmentManage, h.list)
	r.PUT("/partner-groups/:id/commands/:command", authz.Public, h.putCommand)
	r.GET("/partner-groups/:id/commands/:command", PermAssignmentManage, h.getCommand)
}

func partnerGroupText(s string, max int) bool {
	return utf8.ValidString(s) && s != "" && s == strings.TrimSpace(s) && utf8.RuneCountInString(s) <= max && strings.IndexFunc(s, unicode.IsControl) < 0
}

func partnerGroupInputValid(in PartnerGroupProposalInput, now time.Time) bool {
	return in.HospitalID != uuid.Nil && partnerGroupText(in.Name, 120) && partnerGroupText(in.Reason, 500) && (in.Purpose == "procurement" || in.Purpose == "receiving" || in.Purpose == "finance") && !in.ValidFrom.IsZero() && in.ExpiresAt.After(now) && in.ExpiresAt.After(in.ValidFrom) && in.ExpiresAt.Sub(in.ValidFrom) <= 366*24*time.Hour
}

func partnerGroupPrincipal(c *gin.Context) (authn.Principal, uuid.UUID, bool) {
	if !authn.Refresh(c) {
		return authn.Principal{}, uuid.Nil, false
	}
	p, _, ok := membershipCommandPrincipal(c)
	if !ok {
		return p, uuid.Nil, false
	}
	if c.Param("id") == "" {
		return p, uuid.Nil, true
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 || id.String() != c.Param("id") {
		respond.NotFound(c, "partner group")
		return p, uuid.Nil, false
	}
	return p, id, true
}

func partnerGroupError(c *gin.Context, err error) {
	var pg *pgconn.PgError
	switch {
	case errors.Is(err, authz.ErrScopedCredentialRevoked):
		respond.Error(c, 401, "session_revoked", "Sign in again before managing partner groups.")
	case errors.Is(err, errAssignmentDenied):
		respond.Forbidden(c, "An independent current access authority is required for this decision.")
	case errors.As(err, &pg) && (pg.Code == "23505" || pg.ConstraintName == "iam_partner_group_command_guard" || pg.ConstraintName == "iam_partner_member_command_guard"):
		respond.Conflict(c, "The group or command changed. Recover the retained command before continuing.")
	case errors.Is(err, gorm.ErrRecordNotFound):
		respond.NotFound(c, "partner group")
	case errors.Is(err, errPartnerGroupConflict):
		respond.Conflict(c, "This group identifier already records a different proposal. Reopen its retained details.")
	case errors.Is(err, errRevocationActorNotAdmin):
		respond.Forbidden(c, "Current access administration authority is required.")
	default:
		respond.Error(c, 503, "partner_group_unavailable", "Partner group authority or evidence is unavailable. Retry the same proposal identifier.")
	}
}

func partnerGroupQuery(tx *gorm.DB) *gorm.DB {
	return tx.Table("iam_partner_groups g").Select("g.id,g.membership_policy_version,g.hospital_id,g.name,g.purpose,g.reason,g.valid_from,g.expires_at,COALESCE(c.result_status,CASE WHEN o.group_id IS NULL THEN g.status WHEN o.observed_exists THEN 'pending_review' ELSE 'scope_rejected' END) AS status,COALESCE(c.result_version,CASE WHEN o.group_id IS NULL THEN g.version ELSE 2 END) AS version,g.proposed_by,g.created_at,COALESCE(c.created_at,o.updated_at,g.updated_at) AS updated_at").Joins("LEFT JOIN iam_partner_group_scope_observations o ON o.tenant_id=g.tenant_id AND o.group_id=g.id AND o.hospital_id=g.hospital_id").Joins("LEFT JOIN LATERAL(SELECT result_status,result_version,created_at FROM iam_partner_group_commands c WHERE c.tenant_id=g.tenant_id AND c.group_id=g.id ORDER BY c.result_version DESC LIMIT 1)c ON true")
}
func (g PartnerGroup) PageKey() (time.Time, uuid.UUID) { return g.CreatedAt, g.ID }
func readPartnerGroup(tx *gorm.DB, tenant string, id uuid.UUID) (PartnerGroup, error) {
	var row PartnerGroup
	err := partnerGroupQuery(tx).Where("g.tenant_id=? AND g.id=?", tenant, id).Take(&row).Error
	return row, err
}
func (h *partnerGroupHandlers) list(c *gin.Context, params pagination.Params) ([]PartnerGroup, error) {
	p, _, ok := partnerGroupPrincipal(c)
	if !ok {
		return nil, nil
	}
	for key, values := range c.Request.URL.Query() {
		if (key != "cursor" && key != "limit") || len(values) != 1 || values[0] == "" {
			respond.Error(c, 400, "invalid_request", "Use one bounded limit and optional continuation cursor.")
			return nil, nil
		}
	}
	ctx, cancel := membershipCommandContext(c, p)
	defer cancel()
	rows := []PartnerGroup{}
	err := h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := membershipActor(tx, p, p.Subject); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.iam_partner_groups','read',true)`).Error; err != nil {
			return err
		}
		query := partnerGroupQuery(tx).Where("g.tenant_id=?", p.TenantID).Order("g.created_at DESC,g.id DESC").Limit(params.Limit + 1)
		if params.After != nil {
			query = query.Where("(g.created_at,g.id)<(?,?)", params.After.CreatedAt, params.After.ID)
		}
		if err := query.Find(&rows).Error; err != nil {
			return err
		}
		return partnerGroupReceiptAudit(tx, p, uuid.MustParse(p.TenantID), "partner_group_list_read")
	})
	if err != nil {
		partnerGroupError(c, err)
	}
	return rows, err
}

func partnerGroupReceiptAudit(tx *gorm.DB, p authn.Principal, id uuid.UUID, action string) error {
	return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,?,'iam_partner_groups',?)`, p.TenantID, p.Subject, action, id).Error
}

func (h *partnerGroupHandlers) put(c *gin.Context) {
	p, id, ok := partnerGroupPrincipal(c)
	if !ok {
		return
	}
	var in PartnerGroupProposalInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		respond.Error(c, 400, "invalid_request", "Provide one bounded partner group proposal.")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		respond.Error(c, 400, "invalid_request", "Provide one partner group proposal.")
		return
	}
	in.ValidFrom = in.ValidFrom.UTC().Truncate(time.Microsecond)
	in.ExpiresAt = in.ExpiresAt.UTC().Truncate(time.Microsecond)
	if !partnerGroupInputValid(in, time.Now()) {
		respond.Error(c, 400, "invalid_request", "Provide a hospital, team name, supported purpose, reason and validity of at most 366 days.")
		return
	}
	ctx, cancel := membershipCommandContext(c, p)
	defer cancel()
	var saved PartnerGroup
	err := h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := membershipActor(tx, p, p.Subject); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.iam_partner_groups','write',true),pg_advisory_xact_lock(hashtextextended(?,0))`, p.TenantID+":partner-group:"+id.String()).Error; err != nil {
			return err
		}
		epoch, err := proposalMembershipEpoch(tx, p.TenantID, p.Subject)
		if err != nil {
			return err
		}
		if epoch == 0 {
			return errRevocationActorNotAdmin
		}
		saved, err = readPartnerGroup(tx, p.TenantID, id)
		if err == nil {
			var originalEpoch int64
			if err := tx.Table("iam_partner_groups").Select("proposer_membership_epoch").Where("tenant_id=? AND id=?", p.TenantID, id).Scan(&originalEpoch).Error; err != nil {
				return err
			}
			if originalEpoch != epoch || saved.ProposedBy != p.Subject || saved.HospitalID != in.HospitalID || saved.Name != in.Name || saved.Purpose != in.Purpose || saved.Reason != in.Reason || !saved.ValidFrom.Equal(in.ValidFrom) || !saved.ExpiresAt.Equal(in.ExpiresAt) {
				return errPartnerGroupConflict
			}
			return partnerGroupReceiptAudit(tx, p, id, "partner_group_receipt_read")
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Exec(`INSERT INTO iam_partner_groups(tenant_id,id,hospital_id,name,purpose,reason,valid_from,expires_at,proposed_by,proposer_membership_epoch) VALUES(?,?,?,?,?,?,?,?,?,?)`, p.TenantID, id, in.HospitalID, in.Name, in.Purpose, in.Reason, in.ValidFrom, in.ExpiresAt, p.Subject, epoch).Error; err != nil {
			return err
		}
		saved, err = readPartnerGroup(tx, p.TenantID, id)
		if err != nil {
			return err
		}
		if err := partnerGroupReceiptAudit(tx, p, id, "partner_group_prepared"); err != nil {
			return err
		}
		if h.bus == nil {
			return errors.New("partner group source request unavailable")
		}
		payload, err := json.Marshal(iamcontract.PartnerGroupHospitalScopeRequestedData{GroupID: id, HospitalID: in.HospitalID})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, iamcontract.SubjectPartnerGroupHospitalScopeRequested, events.Event{Type: iamcontract.SubjectPartnerGroupHospitalScopeRequested, Version: 1, TenantID: p.TenantID, Data: payload})
	})
	if err != nil {
		partnerGroupError(c, err)
		return
	}
	respond.OK(c, saved)
}

func (h *partnerGroupHandlers) get(c *gin.Context) {
	p, id, ok := partnerGroupPrincipal(c)
	if !ok {
		return
	}
	ctx, cancel := membershipCommandContext(c, p)
	defer cancel()
	var saved PartnerGroup
	err := h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := membershipActor(tx, p, p.Subject); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.iam_partner_groups','read',true)`).Error; err != nil {
			return err
		}
		var err error
		saved, err = readPartnerGroup(tx, p.TenantID, id)
		if err != nil {
			return err
		}
		return partnerGroupReceiptAudit(tx, p, id, "partner_group_read")
	})
	if err != nil {
		partnerGroupError(c, err)
		return
	}
	respond.OK(c, saved)
}

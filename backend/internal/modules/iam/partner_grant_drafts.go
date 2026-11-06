package iam

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"gorm.io/gorm"
)

//go:embed partner_grant_drafts.sql
var partnerGrantDraftsSchema string

func (m *Module) registerPartnerGrantDrafts(r *platform.Router, deps platform.Deps) {
	h := &partnerGroupHandlers{db: deps.DB, bus: deps.Bus}
	r.PUT("/partner-grant-drafts/:id", PermAssignmentManage, h.putGrantDraft)
	r.GET("/partner-grant-drafts/:id", PermAssignmentManage, h.getGrantDraft)
	platform.ListRoute(r, "/partner-grant-drafts", PermAssignmentManage, h.listGrantDrafts)
}

func validPartnerGrantDraft(in PartnerGrantDraftInput) bool {
	for key, value := range map[string]string{"owner_product": in.OwnerProduct, "recipient_kind": in.RecipientKind, "template_key": in.TemplateKey, "source_jurisdiction": in.SourceJurisdiction, "destination_jurisdiction": in.DestinationJurisdiction} {
		if !slices.Contains(partnerGrantInputEnums[key], value) {
			return false
		}
	}
	for _, id := range []uuid.UUID{in.OwnerOrganizationID, in.SourceLocationID, in.RelationshipID} {
		if id.Version() != 7 || id.Variant() != uuid.RFC4122 {
			return false
		}
	}
	for _, id := range []uuid.UUID{in.RecipientTenantID, in.RecipientHospitalID} {
		if id == uuid.Nil {
			return false
		}
	}
	if in.RelationshipVersion < 1 || in.RelationshipVersion > 9007199254740990 || !partnerGroupText(in.Reason, 500) || in.ValidFrom.IsZero() || !in.ExpiresAt.After(in.ValidFrom) || in.ExpiresAt.Sub(in.ValidFrom) > time.Duration(partnerGrantMaximumValidityDays)*24*time.Hour {
		return false
	}
	if in.RecipientKind == "group" {
		return in.RecipientSubject == nil && in.RecipientGroupID != nil && in.RecipientGroupID.Version() == 7 && in.RecipientGroupID.Variant() == uuid.RFC4122
	}
	return in.RecipientGroupID == nil && in.RecipientSubject != nil && proposalSubject(*in.RecipientSubject) && strings.IndexFunc(*in.RecipientSubject, unicode.IsControl) < 0
}

func (h *partnerGroupHandlers) grantDraftTransaction(c *gin.Context, p authn.Principal, write bool, action func(*gorm.DB) error) error {
	ctx, cancel := membershipCommandContext(c, p)
	defer cancel()
	return h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := membershipActor(tx, p, p.Subject); err != nil {
			return err
		}
		purpose := "read"
		if write {
			purpose = "write"
		}
		if err := tx.Exec(`SELECT set_config('app.iam_partner_grant_drafts',?,true)`, purpose).Error; err != nil {
			return err
		}
		return action(tx)
	})
}

func grantDraftAudit(tx *gorm.DB, p authn.Principal, id uuid.UUID, action string) error {
	return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,?,'iam_partner_grant_drafts',?)`, p.TenantID, p.Subject, action, id).Error
}

func readPartnerGrantDraft(tx *gorm.DB, tenant string, id uuid.UUID) (PartnerGrantDraft, string, int64, error) {
	var row struct {
		Document                []byte
		InputHash               string
		ProposerMembershipEpoch int64
	}
	var draft PartnerGrantDraft
	if err := tx.Table("iam_partner_grant_drafts").Select("document,input_hash,proposer_membership_epoch").Where("tenant_id=? AND id=?", tenant, id).Take(&row).Error; err != nil {
		return draft, "", 0, err
	}
	if err := json.Unmarshal(row.Document, &draft); err != nil {
		return draft, "", 0, err
	}
	if draft.ID != id || draft.OwnerTenantID.String() != tenant || draft.Status != "pending_verification" || draft.OperatingAccessEnabled {
		return draft, "", 0, errors.New("invalid grant draft evidence")
	}
	return draft, row.InputHash, row.ProposerMembershipEpoch, nil
}

func (h *partnerGroupHandlers) putGrantDraft(c *gin.Context) {
	p, id, ok := partnerGroupPrincipal(c)
	if !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	raw, readError := io.ReadAll(c.Request.Body)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var in PartnerGrantDraftInput
	var extra any
	if readError != nil || decoder.Decode(&in) != nil || !errors.Is(decoder.Decode(&extra), io.EOF) || !validPartnerGrantDraftPresence(raw) || !validPartnerGrantDraft(in) {
		respond.Error(c, 400, "invalid_request", "Choose one typed recipient, a supported owner and template, exact relationship and location references, and bounded India validity.")
		return
	}
	in.ValidFrom = in.ValidFrom.UTC()
	in.ExpiresAt = in.ExpiresAt.UTC()
	hash, err := partnerMemberHash(p.Subject, in)
	if err != nil {
		partnerGrantDraftError(c, err)
		return
	}
	var draft PartnerGrantDraft
	err = h.grantDraftTransaction(c, p, true, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, p.TenantID+":partner-grant-draft:"+id.String()).Error; err != nil {
			return err
		}
		epoch, err := proposalMembershipEpoch(tx, p.TenantID, p.Subject)
		if err != nil {
			return err
		}
		if epoch == 0 {
			return errRevocationActorNotAdmin
		}
		saved, previousHash, previousEpoch, err := readPartnerGrantDraft(tx, p.TenantID, id)
		if err == nil {
			if previousHash != hash || previousEpoch != epoch {
				return errPartnerGroupConflict
			}
			draft = saved
			return grantDraftAudit(tx, p, id, "partner_grant_draft_receipt_read")
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if !in.ExpiresAt.After(time.Now()) {
			return errPartnerGrantDraftExpired
		}
		draft = PartnerGrantDraft{ID: id, OwnerTenantID: uuid.MustParse(p.TenantID), Input: in, Status: "pending_verification", PreparedBy: p.Subject, CreatedAt: time.Now().UTC().Truncate(time.Microsecond)}
		raw, err := json.Marshal(draft)
		if err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO iam_partner_grant_drafts(tenant_id,id,input_hash,proposer_membership_epoch,document,created_at)VALUES(?,?,?,?,?::jsonb,?)`, p.TenantID, id, hash, epoch, string(raw), draft.CreatedAt).Error; err != nil {
			return err
		}
		return grantDraftAudit(tx, p, id, "partner_grant_draft_prepared")
	})
	if errors.Is(err, errPartnerGrantDraftExpired) {
		respond.Error(c, 400, "invalid_request", "Choose future expiry when preparing a new draft.")
		return
	}
	if err != nil {
		partnerGrantDraftError(c, err)
		return
	}
	respond.OK(c, draft)
}

var errPartnerGrantDraftExpired = errors.New("new partner grant draft has expired")

func (h *partnerGroupHandlers) getGrantDraft(c *gin.Context) {
	p, id, ok := partnerGroupPrincipal(c)
	if !ok {
		return
	}
	var draft PartnerGrantDraft
	err := h.grantDraftTransaction(c, p, false, func(tx *gorm.DB) error {
		var err error
		draft, _, _, err = readPartnerGrantDraft(tx, p.TenantID, id)
		if err != nil {
			return err
		}
		return grantDraftAudit(tx, p, id, "partner_grant_draft_read")
	})
	if err != nil {
		partnerGrantDraftError(c, err)
		return
	}
	respond.OK(c, draft)
}

func (d PartnerGrantDraft) PageKey() (time.Time, uuid.UUID) { return d.CreatedAt, d.ID }
func (h *partnerGroupHandlers) listGrantDrafts(c *gin.Context, page pagination.Params) ([]PartnerGrantDraft, error) {
	p, _, ok := partnerGroupPrincipal(c)
	if !ok {
		return nil, nil
	}
	for key, values := range c.Request.URL.Query() {
		if (key != "limit" && key != "cursor") || len(values) != 1 || values[0] == "" {
			respond.Error(c, 400, "invalid_request", "Use one bounded page limit and optional continuation cursor.")
			return nil, nil
		}
	}
	drafts := []PartnerGrantDraft{}
	err := h.grantDraftTransaction(c, p, false, func(tx *gorm.DB) error {
		var rows []struct {
			ID       uuid.UUID
			Document []byte
		}
		query := tx.Table("iam_partner_grant_drafts").Select("id,document").Where("tenant_id=?", p.TenantID).Order("created_at DESC,id DESC").Limit(page.Limit + 1)
		if page.After != nil {
			query = query.Where("(created_at,id)<(?,?)", page.After.CreatedAt, page.After.ID)
		}
		if err := query.Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			var draft PartnerGrantDraft
			if err := json.Unmarshal(row.Document, &draft); err != nil {
				return err
			}
			if draft.OwnerTenantID.String() != p.TenantID || draft.ID != row.ID || draft.Status != "pending_verification" || draft.OperatingAccessEnabled {
				return errors.New("invalid grant draft evidence")
			}
			drafts = append(drafts, draft)
		}
		return grantDraftAudit(tx, p, uuid.MustParse(p.TenantID), "partner_grant_draft_list_read")
	})
	if err != nil {
		partnerGrantDraftError(c, err)
	}
	return drafts, err
}

func partnerGrantDraftError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, authz.ErrScopedCredentialRevoked):
		respond.Error(c, 401, "session_revoked", "Sign in again before managing supplier access proposals.")
	case errors.Is(err, gorm.ErrRecordNotFound):
		respond.NotFound(c, "supplier access proposal")
	case errors.Is(err, errPartnerGroupConflict):
		respond.Conflict(c, "This proposal already retains different intent or account evidence. Recover the original proposal before continuing.")
	case errors.Is(err, errRevocationActorNotAdmin):
		respond.Forbidden(c, "Current access administration authority is required.")
	default:
		respond.Error(c, 503, "partner_grant_draft_unavailable", "Supplier access proposal authority or evidence is unavailable. Recover or retry the same proposal identifier.")
	}
}

func validPartnerGrantDraftPresence(raw []byte) bool {
	var supplied map[string]json.RawMessage
	if json.Unmarshal(raw, &supplied) != nil {
		return false
	}
	for _, field := range partnerGrantNonNullableOptionalFields {
		if value, present := supplied[field]; present && bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return false
		}
	}
	return true
}

package iam

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/supplieraccess"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"gorm.io/gorm"
)

type supplierHomeBasis struct {
	GroupVersion    int64                                            `json:"group_version,omitempty"`
	Named           *supplieraccess.SupplierGrantRecipientBasisInput `json:"named,omitempty"`
	Identity        *authz.StaffActorReference                       `json:"identity,omitempty"`
	MembershipEpoch int64                                            `json:"membership_epoch,omitempty"`
	Actor           authz.StaffActorReference                        `json:"actor"`
}

func supplierHomePrincipal(c *gin.Context) (authn.Principal, uuid.UUID, uuid.UUID, bool) {
	c.Header("Cache-Control", "no-store")
	if !authn.Refresh(c) {
		return authn.Principal{}, uuid.Nil, uuid.Nil, false
	}
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return p, uuid.Nil, uuid.Nil, false
	}
	for key, expected := range map[string]string{"X-Helivanta-Expected-Tenant": p.TenantID, "X-Helivanta-Expected-Subject": p.Subject} {
		values := c.Request.Header.Values(key)
		if len(values) != 1 || values[0] != expected {
			respond.Error(c, 409, "context_changed", "Your account or hospital changed. Reopen the supplier offer.")
			return p, uuid.Nil, uuid.Nil, false
		}
	}
	hospital, err := uuid.Parse(c.Param("hospital"))
	if err != nil || hospital == uuid.Nil || hospital.String() != c.Param("hospital") {
		respond.NotFound(c, "hospital")
		return p, uuid.Nil, uuid.Nil, false
	}
	id := uuid.Nil
	if c.Param("id") != "" {
		id, ok = grantRecordID(c, "id")
		if !ok {
			return p, hospital, uuid.Nil, false
		}
	}
	return p, hospital, id, true
}
func (h *supplierGrantHandlers) homeTransaction(c *gin.Context, p authn.Principal, hospital uuid.UUID, permission authz.Permission, action func(context.Context, *gorm.DB, authz.StaffActorReference) error) error {
	if h.members == nil {
		return suppliertrade.ErrUnavailable
	}
	return h.members.transaction(c, p, partnerMemberRoute{hospital: hospital}, permission, func(ctx context.Context, tx *gorm.DB, actor authz.StaffActorReference) error {
		if e := supplierGrantFrame(tx, "hospital", hospital); e != nil {
			return e
		}
		return action(ctx, tx, actor)
	})
}
func homeGrant(tx *gorm.DB, p authn.Principal, hospital, id uuid.UUID) (supplieraccess.SupplierGrant, error) {
	grant, err := loadSupplierGrant(tx, p.TenantID, id)
	if err != nil {
		return grant, err
	}
	if grant.Side != "hospital" || grant.Scope.HospitalID != hospital || grant.Scope.RecipientTenantID.String() != p.TenantID {
		return supplieraccess.SupplierGrant{}, gorm.ErrRecordNotFound
	}
	return grant, nil
}
func (h *supplierGrantHandlers) getHospitalOffer(c *gin.Context) {
	p, hospital, id, ok := supplierHomePrincipal(c)
	if !ok {
		return
	}
	var result supplieraccess.SupplierGrant
	err := h.homeTransaction(c, p, hospital, PermSupplierGrantAccept, func(_ context.Context, tx *gorm.DB, _ authz.StaffActorReference) error {
		var e error
		result, e = homeGrant(tx, p, hospital, id)
		if e != nil {
			return e
		}
		return grantReadAudit(tx, p, id, "supplier_grant_offer_read")
	})
	if err != nil {
		supplierGrantError(c, err)
		return
	}
	respond.OK(c, result)
}
func (h *supplierGrantHandlers) listHospitalOffers(c *gin.Context, page pagination.Params) ([]supplierGrantPage, error) {
	p, hospital, _, ok := supplierHomePrincipal(c)
	if !ok {
		return nil, nil
	}
	for key, values := range c.Request.URL.Query() {
		if (key != "limit" && key != "cursor") || len(values) != 1 || values[0] == "" {
			respond.BadRequest(c, supplieraccess.ErrInvalid)
			return nil, nil
		}
	}
	result := []supplierGrantPage{}
	err := h.homeTransaction(c, p, hospital, PermSupplierGrantAccept, func(_ context.Context, tx *gorm.DB, _ authz.StaffActorReference) error {
		var rows []struct{ Document []byte }
		q := tx.Table("iam_supplier_grants").Select("document").Where("tenant_id=? AND hospital_id=? AND document->>'side'='hospital'", p.TenantID, hospital).Order("created_at DESC,id DESC").Limit(page.Limit + 1)
		if page.After != nil {
			q = q.Where("(created_at,id)<(?,?)", page.After.CreatedAt, page.After.ID)
		}
		if e := q.Find(&rows).Error; e != nil {
			return e
		}
		for _, row := range rows {
			var g supplieraccess.SupplierGrant
			if suppliertrade.DecodeObject(row.Document, &g) != nil || supplieraccess.ValidateRecord(g) != nil {
				return suppliertrade.ErrUnavailable
			}
			result = append(result, supplierGrantPage{g})
		}
		return grantReadAudit(tx, p, hospital, "supplier_grant_offer_directory_read")
	})
	if err != nil {
		supplierGrantError(c, err)
		return nil, nil
	}
	return result, nil
}
func (h *supplierGrantHandlers) hospitalReceipt(c *gin.Context) {
	p, hospital, id, ok := supplierHomePrincipal(c)
	if !ok {
		return
	}
	command, ok := grantRecordID(c, "command")
	if !ok {
		return
	}
	var result supplieraccess.SupplierGrantCommandReceipt
	err := h.homeTransaction(c, p, hospital, PermSupplierGrantAccept, func(_ context.Context, tx *gorm.DB, _ authz.StaffActorReference) error {
		if _, e := homeGrant(tx, p, hospital, id); e != nil {
			return e
		}
		var e error
		result, e = readSupplierGrantCommand(tx, p, id, command, "")
		if e != nil {
			return e
		}
		return grantReadAudit(tx, p, id, "supplier_grant_hospital_command_read")
	})
	if err != nil {
		supplierGrantError(c, err)
		return
	}
	respond.OK(c, result)
}
func basisDigest(basis supplierHomeBasis) (string, []byte, error) {
	raw, err := json.Marshal(basis)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), raw, nil
}
func (h *supplierGrantHandlers) approveHomeBasis(ctx context.Context, tx *gorm.DB, p authn.Principal, actor authz.StaffActorReference, grant supplieraccess.SupplierGrant, input supplieraccess.SupplierGrantHospitalDecisionInput, action func(supplierHomeBasis) error) error {
	if grant.RemoteSignedBy == nil || *grant.RemoteSignedBy == p.Subject {
		return supplieraccess.ErrConflict
	}
	if grant.Input.RecipientKind == "group" {
		if input.RecipientBasis != nil || input.ExpectedGroupVersion == nil || grant.Input.RecipientGroupID == nil {
			return supplieraccess.ErrInvalid
		}
		groupID := *grant.Input.RecipientGroupID
		if e := lockPartnerMemberGroup(tx, p.TenantID, partnerMemberRoute{hospital: grant.Scope.HospitalID, group: groupID}); e != nil {
			return e
		}
		group, e := readPartnerGroup(tx, p.TenantID, groupID)
		if e != nil {
			return e
		}
		template, ok := supplieraccess.TemplateFor(grant.Input.TemplateKey)
		if !ok || group.HospitalID != grant.Scope.HospitalID || group.Status != "approved" || group.MembershipPolicyVersion != 1 || group.Version != *input.ExpectedGroupVersion || !slices.Contains(template.GroupPurposes, group.Purpose) || grant.Input.ValidFrom.Before(group.ValidFrom) || grant.Input.ExpiresAt.After(group.ExpiresAt) {
			return supplieraccess.ErrIneligible
		}
		var self bool
		if e = tx.Raw(`SELECT EXISTS(SELECT 1 FROM iam_partner_members WHERE tenant_id=? AND hospital_id=? AND group_id=? AND(subject=? OR staff_id=?)AND status='approved')`, p.TenantID, grant.Scope.HospitalID, groupID, p.Subject, actor.StaffID).Scan(&self).Error; e != nil {
			return e
		}
		if self {
			return supplieraccess.ErrConflict
		}
		return action(supplierHomeBasis{GroupVersion: group.Version, Actor: actor})
	}
	if input.ExpectedGroupVersion != nil || input.RecipientBasis == nil || grant.Input.RecipientSubject == nil || *grant.Input.RecipientSubject == p.Subject {
		return supplieraccess.ErrInvalid
	}
	selected := *input.RecipientBasis
	if selected.StaffID == uuid.Nil || selected.StaffID == actor.StaffID || selected.PostingID.Version() != 7 || selected.PostingRevisionID.Version() != 7 || selected.DepartmentID.Version() != 7 || selected.DepartmentRevisionID.Version() != 7 || selected.PostingVersion < 2 || selected.PostingVersion > 9007199254740991 {
		return supplieraccess.ErrInvalid
	}
	scope := authz.PartnerMemberScope{MemberSubject: *grant.Input.RecipientSubject, Posting: authz.StaffPostingSourceScope{TenantID: p.TenantID, Subject: p.Subject, HospitalID: grant.Scope.HospitalID, StaffID: selected.StaffID, PostingID: selected.PostingID, RevisionID: selected.PostingRevisionID, Version: selected.PostingVersion, DepartmentID: selected.DepartmentID, DepartmentRevisionID: selected.DepartmentRevisionID}}
	if h.members.source == nil {
		return suppliertrade.ErrUnavailable
	}
	return h.members.source.WithCurrentPartnerMember(ctx, scope, func(_ context.Context, reference authz.PartnerMemberReference) error {
		if reference.Subject != *grant.Input.RecipientSubject || reference.Member.StaffID != selected.StaffID || reference.Member.StaffID == actor.StaffID {
			return supplieraccess.ErrIneligible
		}
		epoch, e := proposalMembershipEpoch(tx, p.TenantID, reference.Subject)
		if e != nil {
			return e
		}
		identity := reference.Member
		return action(supplierHomeBasis{Named: &selected, Identity: &identity, MembershipEpoch: epoch, Actor: actor})
	})
}
func (h *supplierGrantHandlers) decideHospital(c *gin.Context) {
	p, hospital, id, ok := supplierHomePrincipal(c)
	if !ok {
		return
	}
	command, ok := grantRecordID(c, "command")
	if !ok {
		return
	}
	var input supplieraccess.SupplierGrantHospitalDecisionInput
	if !grantBody(c, &input) {
		return
	}
	if input.ExpectedVersion < 1 || input.ExpectedVersion >= 9007199254740991 || !suppliertrade.Text(input.Reason, 500) || !slices.Contains([]string{"approve", "reject", "suspend"}, input.Operation) || (input.Operation != "approve" && (input.RecipientBasis != nil || input.ExpectedGroupVersion != nil)) {
		respond.BadRequest(c, supplieraccess.ErrInvalid)
		return
	}
	hash, err := supplierCommandHash(p.Subject, input)
	if err != nil {
		supplierGrantError(c, err)
		return
	}
	var original supplieraccess.SupplierGrant
	var result supplieraccess.SupplierGrantCommandReceipt
	recovered := false
	basisHash := ""
	err = h.homeTransaction(c, p, hospital, PermSupplierGrantAccept, func(ctx context.Context, tx *gorm.DB, actor authz.StaffActorReference) error {
		row, e := homeGrant(tx, p, hospital, id)
		if e != nil {
			return e
		}
		original = row
		receipt, e := readSupplierGrantCommand(tx, p, id, command, hash)
		if e == nil {
			result = receipt
			recovered = true
			return grantReadAudit(tx, p, id, "supplier_grant_hospital_command_recovery")
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		decide := func() error {
			_, _, e := supplieraccess.Decide(row, input.ExpectedVersion, input.Operation, p.Subject, basisHash, time.Now())
			return e
		}
		if input.Operation != "approve" {
			return decide()
		}
		return h.approveHomeBasis(ctx, tx, p, actor, row, input, func(basis supplierHomeBasis) error {
			var e error
			basisHash, _, e = basisDigest(basis)
			if e != nil {
				return e
			}
			return decide()
		})
	})
	if err != nil {
		supplierGrantError(c, err)
		return
	}
	if recovered {
		respond.OK(c, result)
		return
	}
	var proof suppliertrade.RelationshipAuthorization
	if input.Operation == "approve" {
		if h.relationships == nil {
			supplierGrantError(c, suppliertrade.ErrUnavailable)
			return
		}
		proof, err = h.relationships.VerifyRelationship(c.Request.Context(), suppliertrade.RelationshipScope{TenantID: p.TenantID, ID: original.Input.RelationshipID, TermsSha256: original.Input.TermsSha256, Purpose: "new_business"})
		if err != nil {
			supplierGrantError(c, err)
			return
		}
	}
	err = h.homeTransaction(c, p, hospital, PermSupplierGrantAccept, func(ctx context.Context, tx *gorm.DB, actor authz.StaffActorReference) error {
		if e := supplierGrantLock(tx, p.TenantID, id); e != nil {
			return e
		}
		receipt, e := readSupplierGrantCommand(tx, p, id, command, hash)
		if e == nil {
			result = receipt
			return grantReadAudit(tx, p, id, "supplier_grant_hospital_command_recovery")
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		row, e := homeGrant(tx, p, hospital, id)
		if e != nil {
			return e
		}
		commit := func() error {
			next, event, e := supplieraccess.Decide(row, input.ExpectedVersion, input.Operation, p.Subject, basisHash, time.Now())
			if e != nil {
				return e
			}
			if e = saveSupplierGrant(tx, next, p.Subject, nil, false); e != nil {
				return e
			}
			result, e = saveSupplierGrantCommand(tx, p, command, hash, next, supplieraccess.SupplierGrantDecisionInput{ExpectedVersion: input.ExpectedVersion, Operation: input.Operation, Reason: input.Reason})
			if e != nil {
				return e
			}
			return enqueueSupplierGrant(tx, event)
		}
		if proof == nil {
			return commit()
		}
		return h.relationships.WithVerifiedRelationship(ctx, proof, func(_ context.Context, relationship suppliertrade.Relationship) error {
			if relationship.Side != "buyer" || relationship.Terms.Buyer.HospitalID == nil || *relationship.Terms.Buyer.HospitalID != hospital || relationship.Terms.Seller.TenantID != row.Scope.SourceTenantID || relationship.Terms.Seller.Product != row.Scope.SourceProduct || relationship.Terms.Seller.OrganizationID != row.Scope.SourceOrganizationID || relationship.Terms.Seller.LocationID != row.Scope.SourceLocationID {
				return supplieraccess.ErrIneligible
			}
			return h.approveHomeBasis(ctx, tx, p, actor, row, input, func(basis supplierHomeBasis) error {
				digest, raw, e := basisDigest(basis)
				if e != nil {
					return e
				}
				if digest != basisHash {
					return supplieraccess.ErrConflict
				}
				now := time.Now().UTC()
				if e = tx.Exec(`INSERT INTO iam_supplier_grant_bases(tenant_id,grant_id,hospital_id,document,basis_hash,actor_subject,created_at,updated_at)VALUES(?,?,?,?::jsonb,?,?,?,?)`, p.TenantID, id, hospital, string(raw), digest, p.Subject, now, now).Error; e != nil {
					return e
				}
				return commit()
			})
		})
	})
	if err != nil {
		supplierGrantError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *supplierGrantHandlers) getHospitalGrant(c *gin.Context) {
	p, hospital, id, ok := supplierHomePrincipal(c)
	if !ok {
		return
	}
	if c.Request.URL.RawQuery != "" {
		respond.BadRequest(c, supplieraccess.ErrInvalid)
		return
	}
	if h.recipients == nil {
		supplierGrantError(c, authz.ErrScopedAuthorizationUnavailable)
		return
	}
	var record supplieraccess.SupplierGrant
	err := h.homeTransaction(c, p, hospital, PermSupplierGrantInbox, func(_ context.Context, tx *gorm.DB, _ authz.StaffActorReference) error {
		var e error
		record, e = homeGrant(tx, p, hospital, id)
		return e
	})
	if err != nil {
		supplierGrantError(c, err)
		return
	}
	template, ok := supplieraccess.TemplateFor(record.Input.TemplateKey)
	if !ok {
		supplierGrantError(c, authz.ErrScopedAuthorizationUnavailable)
		return
	}
	err = h.recipients.WithCurrentPartnerRecipient(c.Request.Context(), authz.PartnerScopeRequest{Principal: p, HospitalID: hospital, GrantID: id, Product: record.Scope.SourceProduct, Permission: authz.Permission(template.Permission)}, func(context.Context) error { return nil })
	if err != nil {
		supplierGrantError(c, err)
		return
	}
	respond.OK(c, supplieraccess.SupplierGrantSummary{ID: record.ID, Scope: record.Scope, Input: record.Input, ConsentStatus: "approved", Version: record.Version, SourceAccessState: "not_evaluated", CreatedAt: record.CreatedAt, UpdatedAt: record.UpdatedAt})
}

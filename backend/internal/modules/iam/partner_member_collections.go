package iam

import (
	"context"
	"encoding/json"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"gorm.io/gorm"
	"time"
)

func (m PartnerGroupMemberProposal) PageKey() (time.Time, uuid.UUID) { return m.CreatedAt, m.ID }
func partnerMemberListParameters(c *gin.Context) bool {
	for key, values := range c.Request.URL.Query() {
		if (key != "cursor" && key != "limit") || len(values) != 1 || values[0] == "" {
			respond.Error(c, 400, "invalid_request", "Use one bounded limit and optional continuation cursor.")
			return false
		}
	}
	return true
}
func (h *partnerMemberHandlers) listGroups(c *gin.Context, params pagination.Params) ([]PartnerGroup, error) {
	p, r, ok := partnerMemberPrincipal(c)
	if !ok || !partnerMemberListParameters(c) {
		return nil, nil
	}
	rows := []PartnerGroup{}
	err := h.transaction(c, p, r, PermPartnerGroupMemberRead, func(_ context.Context, tx *gorm.DB, _ authz.StaffActorReference) error {
		query := partnerGroupQuery(tx).Where("g.tenant_id=? AND g.hospital_id=?", p.TenantID, r.hospital).Order("g.created_at DESC,g.id DESC").Limit(params.Limit + 1)
		if params.After != nil {
			query = query.Where("(g.created_at,g.id)<(?,?)", params.After.CreatedAt, params.After.ID)
		}
		if err := query.Find(&rows).Error; err != nil {
			return err
		}
		return partnerGroupReceiptAudit(tx, p, r.hospital, "partner_group_scoped_list_read")
	})
	if err != nil {
		partnerMemberError(c, err)
	}
	return rows, err
}
func (h *partnerMemberHandlers) listMembers(c *gin.Context, params pagination.Params) ([]PartnerGroupMemberProposal, error) {
	p, r, ok := partnerMemberPrincipal(c)
	if !ok || !partnerMemberListParameters(c) {
		return nil, nil
	}
	members := []PartnerGroupMemberProposal{}
	err := h.transaction(c, p, r, PermPartnerGroupMemberRead, func(_ context.Context, tx *gorm.DB, _ authz.StaffActorReference) error {
		group, err := readPartnerGroup(tx, p.TenantID, r.group)
		if err != nil {
			return err
		}
		if group.HospitalID != r.hospital {
			return gorm.ErrRecordNotFound
		}
		rows := []struct{ Document []byte }{}
		query := tx.Table("iam_partner_members").Select("document").Where("tenant_id=? AND hospital_id=? AND group_id=?", p.TenantID, r.hospital, r.group).Order("created_at DESC,id DESC").Limit(params.Limit + 1)
		if params.After != nil {
			query = query.Where("(created_at,id)<(?,?)", params.After.CreatedAt, params.After.ID)
		}
		if err := query.Find(&rows).Error; err != nil {
			return err
		}
		for _, row := range rows {
			var member PartnerGroupMemberProposal
			if err := json.Unmarshal(row.Document, &member); err != nil {
				return err
			}
			members = append(members, member)
		}
		return partnerGroupReceiptAudit(tx, p, r.group, "partner_member_list_read")
	})
	if err != nil {
		partnerMemberError(c, err)
	}
	return members, err
}

func (m PartnerMemberCandidate) PageKey() (time.Time, uuid.UUID) { return m.CreatedAt, m.ID }
func (h *partnerMemberHandlers) listCandidates(c *gin.Context, params pagination.Params) ([]PartnerMemberCandidate, error) {
	p, r, ok := partnerMemberPrincipal(c)
	if !ok || !partnerMemberListParameters(c) {
		return nil, nil
	}
	if params.Limit > 50 {
		respond.Error(c, 400, "invalid_request", "Choose at most 50 candidate postings per page.")
		return nil, nil
	}
	rows := []PartnerMemberCandidate{}
	err := h.transaction(c, p, r, PermPartnerGroupMemberRead, func(ctx context.Context, tx *gorm.DB, _ authz.StaffActorReference) error {
		page := authz.PartnerMemberCandidatePage{Limit: params.Limit}
		if params.After != nil {
			page.AfterCreatedAt = params.After.CreatedAt
			page.AfterID = params.After.ID
		}
		return h.source.WithPartnerMemberCandidates(ctx, authz.StaffActorScope{TenantID: p.TenantID, Subject: p.Subject, HospitalID: r.hospital}, page, func(_ context.Context, candidates []authz.PartnerMemberCandidate) error {
			for _, candidate := range candidates {
				rows = append(rows, PartnerMemberCandidate{ID: candidate.ID, HospitalID: candidate.HospitalID, StaffID: candidate.StaffID, Subject: candidate.Subject, DisplayName: candidate.DisplayName, PostingRevisionID: candidate.PostingRevisionID, PostingVersion: candidate.PostingVersion, DepartmentID: candidate.DepartmentID, DepartmentRevisionID: candidate.DepartmentRevisionID, StartsAt: candidate.StartsAt, NominalUntil: candidate.NominalUntil, CreatedAt: candidate.CreatedAt, SourceState: "candidate"})
			}
			return partnerGroupReceiptAudit(tx, p, r.hospital, "partner_member_candidates_read")
		})
	})
	if err != nil {
		partnerMemberError(c, err)
	}
	return rows, err
}

package iam

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

func readPartnerMemberCommand(tx *gorm.DB, tenant string, r partnerMemberRoute, hash string) (PartnerMemberCommandReceipt, error) {
	var row struct {
		InputHash string
		Receipt   []byte
	}
	var receipt PartnerMemberCommandReceipt
	err := tx.Table("iam_partner_member_commands").Select("input_hash,receipt").Where("tenant_id=? AND hospital_id=? AND group_id=? AND member_id=? AND id=?", tenant, r.hospital, r.group, r.member, r.command).Take(&row).Error
	if err != nil {
		return receipt, err
	}
	if hash != "" && hash != row.InputHash {
		return receipt, errPartnerGroupConflict
	}
	err = json.Unmarshal(row.Receipt, &receipt)
	return receipt, err
}
func (h *partnerMemberHandlers) getMemberCommand(c *gin.Context) {
	p, r, ok := partnerMemberPrincipal(c)
	if !ok {
		return
	}
	var saved PartnerMemberCommandReceipt
	err := h.transaction(c, p, r, PermPartnerGroupMemberRead, func(_ context.Context, tx *gorm.DB, _ authz.StaffActorReference) error {
		var err error
		saved, err = readPartnerMemberCommand(tx, p.TenantID, r, "")
		if err != nil {
			return err
		}
		return partnerGroupReceiptAudit(tx, p, r.member, "partner_member_command_receipt_read")
	})
	if err != nil {
		partnerMemberError(c, err)
		return
	}
	respond.OK(c, saved)
}
func partnerMemberTransition(member PartnerGroupMemberProposal, actor authz.StaffActorReference, p authn.Principal, in PartnerMemberCommandInput) (string, error) {
	if member.Version != in.ExpectedVersion {
		return "", errPartnerGroupConflict
	}
	switch in.Operation {
	case "approve", "reject":
		if actor.StaffID == member.PreparedByStaffID || actor.StaffID == member.MemberStaffID || p.Subject == member.PreparedBy || p.Subject == member.Input.Subject {
			return "", errAssignmentDenied
		}
		if member.Status != "pending_review" {
			return "", errPartnerGroupConflict
		}
		if in.Operation == "approve" {
			return "approved", nil
		}
		return "rejected", nil
	case "withdraw":
		if actor.StaffID != member.PreparedByStaffID || p.Subject != member.PreparedBy {
			return "", errAssignmentDenied
		}
		if member.Status != "pending_review" {
			return "", errPartnerGroupConflict
		}
		return "withdrawn", nil
	case "suspend":
		if member.Status != "approved" {
			return "", errPartnerGroupConflict
		}
		return "suspended", nil
	}
	return "", errPartnerGroupConflict
}
func (h *partnerMemberHandlers) putMemberCommand(c *gin.Context) {
	p, r, ok := partnerMemberPrincipal(c)
	if !ok {
		return
	}
	var in PartnerMemberCommandInput
	if !partnerMemberDecode(c, &in) {
		return
	}
	if !partnerGroupText(in.Reason, 500) || in.ExpectedVersion < 1 || in.ExpectedVersion > 9007199254740990 || (in.Operation != "approve" && in.Operation != "reject" && in.Operation != "withdraw" && in.Operation != "suspend") || (in.Operation == "approve" && (in.SourcesExamined == nil || !*in.SourcesExamined || in.PurposeEligibilityConfirmed == nil || !*in.PurposeEligibilityConfirmed)) {
		respond.Error(c, 400, "invalid_request", "Provide an exact membership decision, version, reason and approval attestations.")
		return
	}
	permission := PermPartnerGroupMemberReview
	if in.Operation == "withdraw" {
		permission = PermPartnerGroupMemberPrepare
	}
	hash, err := partnerMemberHash(p.Subject, in)
	if err != nil {
		partnerMemberError(c, err)
		return
	}
	var saved PartnerMemberCommandReceipt
	err = h.transaction(c, p, r, permission, func(ctx context.Context, tx *gorm.DB, actor authz.StaffActorReference) error {
		receipt, e := readPartnerMemberCommand(tx, p.TenantID, r, hash)
		if e == nil {
			saved = receipt
			return partnerGroupReceiptAudit(tx, p, r.member, "partner_member_command_receipt_read")
		}
		if !errors.Is(e, gorm.ErrRecordNotFound) {
			return e
		}
		member, e := readPartnerMember(tx, p.TenantID, r)
		if e != nil {
			return e
		}
		if _, e = partnerMemberTransition(member, actor, p, in); e != nil {
			return e
		}
		apply := func(ref *authz.PartnerMemberReference) error {
			if e := lockPartnerMemberGroup(tx, p.TenantID, r); e != nil {
				return e
			}
			receipt, e = readPartnerMemberCommand(tx, p.TenantID, r, hash)
			if e == nil {
				saved = receipt
				return partnerGroupReceiptAudit(tx, p, r.member, "partner_member_command_receipt_read")
			}
			if !errors.Is(e, gorm.ErrRecordNotFound) {
				return e
			}
			member, e = readPartnerMember(tx, p.TenantID, r)
			if e != nil {
				return e
			}
			status, e := partnerMemberTransition(member, actor, p, in)
			if e != nil {
				return e
			}
			if ref != nil {
				if e = memberGroupCurrent(tx, p.TenantID, r, member.Input); e != nil {
					return e
				}
				epoch, e := proposalMembershipEpoch(tx, p.TenantID, member.Input.Subject)
				if e != nil {
					return e
				}
				if ref.Member.StaffID != member.MemberStaffID || ref.Member.LinkID != member.MemberLinkID || ref.Member.LinkVersion != member.MemberLinkVersion || ref.Member.GenerationID != member.MemberGenerationID || ref.Member.EmploymentEpoch != member.MemberEmploymentEpoch || epoch != member.MemberMembershipEpoch || member.Input.ExpiresAt.After(ref.SourceUntil) {
					return authz.ErrScopedCommandDenied
				}
			}
			var cohort int64
			if e = tx.Raw(`INSERT INTO iam_partner_group_cohorts(tenant_id,hospital_id,group_id,epoch)VALUES(?,?,?,1) ON CONFLICT(tenant_id,group_id)DO UPDATE SET epoch=iam_partner_group_cohorts.epoch+1,updated_at=clock_timestamp() RETURNING epoch`, p.TenantID, r.hospital, r.group).Scan(&cohort).Error; e != nil {
				return e
			}
			now := time.Now().UTC().Truncate(time.Microsecond)
			member.Status = status
			member.Version++
			member.UpdatedAt = now
			saved = PartnerMemberCommandReceipt{ID: r.command, MemberID: r.member, Input: in, ResultVersion: member.Version, ActorSubject: p.Subject, ActorStaffID: actor.StaffID, ActorLinkID: actor.LinkID, ActorGenerationID: actor.GenerationID, Member: member, CohortEpoch: cohort, CreatedAt: now}
			if ref != nil {
				saved.ReviewedSourceUseID = &ref.UseID
			}
			doc, e := json.Marshal(saved)
			if e != nil {
				return e
			}
			if e = tx.Exec(`INSERT INTO iam_partner_member_commands(tenant_id,hospital_id,group_id,member_id,id,input_hash,expected_version,result_version,result_status,receipt,created_at,updated_at)VALUES(?,?,?,?,?,?,?,?,?,?::jsonb,?,?)`, p.TenantID, r.hospital, r.group, r.member, r.command, hash, in.ExpectedVersion, member.Version, status, string(doc), now, now).Error; e != nil {
				return e
			}
			memberDoc, e := json.Marshal(member)
			if e != nil {
				return e
			}
			result := tx.Exec(`UPDATE iam_partner_members SET document=?::jsonb,status=?,version=?,updated_at=? WHERE tenant_id=? AND hospital_id=? AND group_id=? AND id=? AND version=?`, string(memberDoc), status, member.Version, now, p.TenantID, r.hospital, r.group, r.member, in.ExpectedVersion)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errPartnerGroupConflict
			}
			return h.memberChanged(tx, p, member, cohort, "partner_member_"+in.Operation)
		}
		if in.Operation != "approve" {
			return apply(nil)
		}
		return h.source.WithCurrentPartnerMember(ctx, memberScope(p, r, member.Input), func(_ context.Context, ref authz.PartnerMemberReference) error { return apply(&ref) })
	})
	if err != nil {
		partnerMemberError(c, err)
		return
	}
	respond.OK(c, saved)
}

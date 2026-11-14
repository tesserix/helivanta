package iam

import (
	"context"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (h *assignmentProposalHandlers) account(c *gin.Context) {
	p, _, ok := h.proposalPrincipal(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id.Version() != 7 || c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		respond.Error(c, 400, "invalid_request", "Use a current assignment proposal without query parameters.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	var before assignmentProposal
	var source assignmentGovernanceProof
	var currentMember bool
	read := func(tx *gorm.DB, row *assignmentProposal) (*assignmentGovernanceProof, error) {
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND id=? AND hospital_id=?", p.TenantID, id, c.Param("hospital")).Take(row).Error; err != nil {
			return nil, err
		}
		proof, err := currentAssignmentGovernanceProof(tx)
		if err != nil || proof == nil {
			return nil, errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
		}
		return proof, nil
	}
	err = h.assignmentTransaction(c, p, authz.GovernanceAssignmentManage, func(tx *gorm.DB) error {
		proof, err := read(tx, &before)
		if err != nil {
			return err
		}
		source = *proof
		currentMember, err = proposalMember(tx, p.TenantID, before.Subject, before.MembershipEpoch)
		return err
	})
	if err != nil {
		if ctx.Err() != nil {
			respond.Error(c, 503, "unavailable", "Could not verify the current account identity.")
		} else {
			proposalError(c, err)
		}
		return
	}
	out := AssignmentAccountPresentation{ProposalID: id, ProposalVersion: before.Version, Subject: before.Subject}
	if !currentMember {
		respond.OK(c, out)
		return
	}
	if h.identities == nil {
		respond.Error(c, 503, "unavailable", "Could not verify the current account identity.")
		return
	}
	candidate, err := h.identities.AssignmentCandidateIdentity(ctx, p.TenantID, before.Subject)
	if err != nil || ctx.Err() != nil || (candidate != nil && candidate.Validate() != nil) {
		respond.Error(c, 503, "unavailable", "Could not verify the current account identity.")
		return
	}
	err = h.assignmentTransaction(c, p, authz.GovernanceAssignmentManage, func(tx *gorm.DB) error {
		var after assignmentProposal
		proof, err := read(tx, &after)
		if err != nil {
			return err
		}
		if !sameAssignmentSource(source, *proof) || after.Version != before.Version || after.Subject != before.Subject || after.MembershipEpoch != before.MembershipEpoch {
			return errProposalConflict
		}
		member, err := proposalMember(tx, p.TenantID, after.Subject, after.MembershipEpoch)
		if err != nil {
			return err
		}
		if member && candidate != nil {
			name, email := candidate.DisplayName, candidate.Email
			out.Available = true
			out.Name = &name
			out.Email = &email
		}
		return nil
	})
	if err != nil {
		if ctx.Err() != nil {
			respond.Error(c, 503, "unavailable", "Could not verify the current account identity.")
		} else {
			proposalError(c, err)
		}
		return
	}
	respond.OK(c, out)
}

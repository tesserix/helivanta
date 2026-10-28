package iam

import (
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PermAssignmentReview independently reviews proposals without granting effective access.
const PermAssignmentReview authz.Permission = "iam.assignment.review"

//go:embed assignment_decisions.sql
var assignmentDecisionsSchema string
var errAssignmentDenied = errors.New("assignment administration unavailable to caller")

type assignmentDecision struct {
	ProposalDecisionRecord `gorm:"embedded"`
	RequestID              uuid.UUID `json:"-"`
}

func (assignmentDecision) TableName() string                 { return "iam_assignment_decisions" }
func (d assignmentDecision) PageKey() (time.Time, uuid.UUID) { return d.CreatedAt, d.ID }

// The initial administrative permissions belong only to tenant_admin. Consult
// IAM's current row as well as FGA so delayed role events cannot revive authority.
func assignmentAdministrator(tx *gorm.DB, tenant, subject string) (bool, error) {
	var row struct{ ID uuid.UUID }
	err := tx.Table("iam_members").Clauses(clause.Locking{Strength: "KEY SHARE"}).Select("id").Where("tenant_id=? AND subject=? AND role_key=?", tenant, subject, string(authz.RoleTenantAdmin)).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return err == nil, err
}
func assignmentAccessFor(tx *gorm.DB, p authn.Principal) error {
	if err := assignmentAccess(tx); err != nil {
		return err
	}
	allowed, err := assignmentAdministrator(tx, p.TenantID, p.Subject)
	if err != nil {
		return err
	}
	if !allowed {
		return errAssignmentDenied
	}
	return nil
}
func (h *assignmentProposalHandlers) decide(c *gin.Context) {
	p, id, ok := proposalScope(c)
	if !ok {
		return
	}
	var in ProposalDecisionInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		respond.Error(c, 400, "invalid_request", "Invalid proposal decision.")
		return
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		respond.Error(c, 400, "invalid_request", "Expected one decision.")
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	if in.ExpectedVersion < 1 || in.ExpectedVersion > 9007199254740991 || (in.Decision != "approve" && in.Decision != "reject" && in.Decision != "withdraw") || in.Reason == "" || !utf8.ValidString(in.Reason) || utf8.RuneCountInString(in.Reason) > 500 || strings.ContainsRune(in.Reason, 0) {
		respond.Error(c, 400, "invalid_request", "Provide a decision, current version and reason.")
		return
	}
	permission := PermAssignmentReview
	command := "review"
	if in.Decision == "withdraw" {
		permission = PermAssignmentManage
		command = "withdraw"
	}
	permissions, _ := authz.PermissionsFrom(c)
	if !permissions.Has(permission) {
		respond.Forbidden(c, "Proposal decision is not permitted.")
		return
	}
	var saved assignmentProposal
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := assignmentAccessFor(tx, p); err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND id=?", p.TenantID, id).Take(&saved).Error; err != nil {
			return err
		}
		if (command == "withdraw" && p.Subject != saved.ProposedBy) || (command == "review" && (p.Subject == saved.ProposedBy || p.Subject == saved.Subject)) {
			return errAssignmentDenied
		}
		if in.Decision == "approve" {
			member, err := proposalMember(tx, p.TenantID, saved.Subject, saved.MembershipEpoch)
			if err != nil {
				return err
			}
			if !member {
				return gorm.ErrRecordNotFound
			}
			if !saved.ExpiresAt.After(time.Now()) {
				return errProposalConflict
			}
		}
		var previous assignmentDecision
		err := tx.Where("tenant_id=? AND proposal_id=? AND expected_version=?", p.TenantID, id, in.ExpectedVersion).Take(&previous).Error
		if err == nil {
			if previous.ActorSubject != p.Subject || previous.Decision != in.Decision || previous.Reason != in.Reason {
				return errProposalConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if saved.Version != in.ExpectedVersion {
			return errProposalConflict
		}
		wasApproved := saved.Status == "approved_pending_activation" || saved.Status == "active"
		next := "pending_decision_scope"
		if command == "withdraw" {
			switch saved.Status {
			case "pending_scope", "pending_review", "pending_decision_scope", "approved_pending_activation", "active":
				next = "withdrawn"
			default:
				return errProposalConflict
			}
		} else if saved.Status != "pending_review" {
			return errProposalConflict
		}
		requestID, err := uuid.NewV7()
		if err != nil {
			return err
		}
		decisionID, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true),set_config('app.assignment_command',?,true)`, p.Subject, command).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO iam_assignment_decisions(tenant_id,id,proposal_id,request_id,expected_version,result_version,decision,actor_subject,reason) VALUES(?,?,?,?,?,?,?,?,?)`, p.TenantID, decisionID, id, requestID, in.ExpectedVersion, in.ExpectedVersion+1, in.Decision, p.Subject, in.Reason).Error; err != nil {
			return err
		}
		if err := tx.Model(&assignmentProposal{}).Where("tenant_id=? AND id=?", p.TenantID, id).Updates(map[string]any{"request_id": requestID, "status": next, "version": saved.Version + 1}).Error; err != nil {
			return err
		}
		if err := tx.Where("tenant_id=? AND id=?", p.TenantID, id).Take(&saved).Error; err != nil {
			return err
		}
		if command == "withdraw" {
			if wasApproved {
				return queueAssignmentProjection(tx, h.bus, p.TenantID, id)
			}
			return nil
		}
		data, err := json.Marshal(iamcontract.AssignmentScopeRequestedData{ProposalID: id, RequestID: requestID, HospitalID: saved.HospitalID, ActorSubject: p.Subject})
		if err != nil {
			return err
		}
		eventID, err := uuid.NewV7()
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, iamcontract.SubjectAssignmentScopeRequested, events.Event{ID: eventID.String(), Type: iamcontract.SubjectAssignmentScopeRequested, Version: 1, TenantID: p.TenantID, OccurredAt: time.Now(), Data: data})
	})
	if err != nil {
		proposalError(c, err)
		return
	}
	respond.OK(c, saved.output())
}
func (h *assignmentProposalHandlers) decisions(c *gin.Context, params pagination.Params) ([]assignmentDecision, error) {
	p, id, ok := proposalScope(c)
	if !ok {
		return nil, nil
	}
	var rows []assignmentDecision
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := assignmentAccessFor(tx, p); err != nil {
			return err
		}
		var proposal assignmentProposal
		if err := tx.Select("id").Where("tenant_id=? AND id=?", p.TenantID, id).Take(&proposal).Error; err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx, params).Where("tenant_id=? AND proposal_id=?", p.TenantID, id).Find(&rows).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'decisions_read','iam_assignment_proposals',?)`, p.TenantID, p.Subject, id).Error
	})
	if err != nil {
		proposalError(c, err)
	}
	return rows, err
}
func decisionScopeStatus(tx *gorm.DB, tenant string, row assignmentProposal, eligible bool) (string, error) {
	var decision assignmentDecision
	if err := tx.Where("tenant_id=? AND proposal_id=? AND request_id=?", tenant, row.ID, row.RequestID).Take(&decision).Error; err != nil {
		return "", err
	}
	authorized, err := assignmentAdministrator(tx, tenant, decision.ActorSubject)
	if err != nil {
		return "", err
	}
	if !eligible || !authorized {
		return "pending_review", nil
	}
	if decision.Decision == "reject" {
		return "rejected", nil
	}
	if decision.Decision != "approve" {
		return "", errors.New("unexpected scope decision")
	}
	member, err := proposalMember(tx, tenant, row.Subject, row.MembershipEpoch)
	if err != nil {
		return "", err
	}
	if !member || !row.ExpiresAt.After(time.Now()) {
		return "scope_rejected", nil
	}
	return "approved_pending_activation", nil
}

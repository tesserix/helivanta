package iam

import (
	"context"
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
	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PermAssignmentManage administers proposals, never grants the proposed permission.
const PermAssignmentManage authz.Permission = "iam.assignment.manage"

//go:embed assignment_proposals.sql
var assignmentProposalsSchema string

type assignmentProposal struct {
	ProposalInput   `gorm:"embedded"`
	ID              uuid.UUID `json:"id"`
	ProposedBy      string    `json:"proposed_by"`
	RequestID       uuid.UUID `json:"-"`
	MembershipEpoch int64     `json:"-"`
	Status          string    `json:"status"`
	Version         int64     `json:"version"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (assignmentProposal) TableName() string                 { return "iam_assignment_proposals" }
func (p assignmentProposal) PageKey() (time.Time, uuid.UUID) { return p.CreatedAt, p.ID }

type assignmentProposalHistory ProposalHistory

func (p assignmentProposalHistory) PageKey() (time.Time, uuid.UUID) { return p.CreatedAt, p.ID }

type assignmentProposalHandlers struct {
	db  *tenantdb.DB
	bus *events.Bus
}

func assignmentAccess(tx *gorm.DB) error {
	return tx.Exec(`SELECT set_config('app.assignment_access','authorized',true)`).Error
}
func proposalMember(tx *gorm.DB, tenant, subject string, expected int64) (bool, error) {
	epoch, err := proposalMembershipEpoch(tx, tenant, subject)
	return epoch > 0 && epoch == expected, err
}
func proposalSubject(s string) bool {
	return utf8.ValidString(s) && utf8.RuneCountInString(s) > 0 && utf8.RuneCountInString(s) <= 200 && !strings.ContainsAny(s, "*#:\x00") && strings.IndexFunc(s, unicode.IsSpace) < 0
}
func proposalScope(c *gin.Context) (authn.Principal, uuid.UUID, bool) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return p, uuid.Nil, false
	}
	if values := c.Request.Header.Values("X-Helivanta-Expected-Tenant"); len(values) > 1 || (len(values) == 1 && values[0] != p.TenantID) {
		respond.Error(c, 409, "tenant_changed", "Your workspace changed. Reload before continuing.")
		return p, uuid.Nil, false
	}
	permissions, _ := authz.PermissionsFrom(c)
	if !permissions.Has(PermAssignmentManage) && !permissions.Has(PermAssignmentReview) {
		respond.Forbidden(c, "Proposal access is not permitted.")
		return p, uuid.Nil, false
	}
	if c.Param("id") == "" {
		return p, uuid.Nil, true
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id.Version() != 7 || id.String() != c.Param("id") {
		respond.NotFound(c, "assignment proposal")
		return p, uuid.Nil, false
	}
	return p, id, true
}
func proposalError(c *gin.Context, err error) {
	if errors.Is(err, errAssignmentDenied) {
		respond.Forbidden(c, "Current assignment administration authority is required.")
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		respond.NotFound(c, "assignment proposal")
		return
	}
	if errors.Is(err, errProposalConflict) {
		respond.Conflict(c, "The proposal changed or this version already has a different command. Reload before continuing.")
		return
	}
	respond.InternalErr(c, errors.New("assignment proposal operation failed"), "Could not process assignment proposal.")
}

var errProposalConflict = errors.New("proposal conflict")

func (h *assignmentProposalHandlers) put(c *gin.Context) {
	p, id, ok := proposalScope(c)
	if !ok {
		return
	}
	var in ProposalInput
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8192)
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(&in); err != nil {
		respond.Error(c, 400, "invalid_request", "Invalid proposal.")
		return
	}
	var trailing any
	if err := d.Decode(&trailing); !errors.Is(err, io.EOF) {
		respond.Error(c, 400, "invalid_request", "Expected one proposal.")
		return
	}
	in.ValidFrom = in.ValidFrom.UTC().Truncate(time.Microsecond)
	in.ExpiresAt = in.ExpiresAt.UTC().Truncate(time.Microsecond)
	if in.HospitalID == uuid.Nil || !proposalSubject(in.Subject) || !proposalSubject(p.Subject) || in.TemplateKey != "workforce.leave_reviewer.v1" || !utf8.ValidString(in.Reason) || strings.TrimSpace(in.Reason) == "" || utf8.RuneCountInString(in.Reason) > 500 || strings.ContainsRune(in.Reason, 0) || in.ValidFrom.IsZero() || !in.ExpiresAt.After(in.ValidFrom) || in.ExpiresAt.Sub(in.ValidFrom) > 366*24*time.Hour || !in.ExpiresAt.After(time.Now()) {
		respond.Error(c, 400, "invalid_request", "Provide a member, hospital, supported template, reason and bounded validity.")
		return
	}
	var saved assignmentProposal
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := assignmentAccessFor(tx, p); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0)),set_config('app.actor_subject',?,true)`, p.TenantID+id.String(), p.Subject).Error; err != nil {
			return err
		}
		membershipEpoch, err := proposalMembershipEpoch(tx, p.TenantID, in.Subject)
		if err != nil {
			return err
		}
		if membershipEpoch == 0 {
			return gorm.ErrRecordNotFound
		}
		err = tx.Where("tenant_id=? AND id=?", p.TenantID, id).Take(&saved).Error
		if err == nil {
			if saved.MembershipEpoch != membershipEpoch || saved.ProposedBy != p.Subject || !sameProposal(saved.ProposalInput, in) {
				return errProposalConflict
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		requestID, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO iam_assignment_proposals(tenant_id,id,hospital_id,subject,template_key,reason,valid_from,expires_at,proposed_by,request_id,status,version) VALUES(?,?,?,?,?,?,?,?,?,?,'pending_scope',1)`, p.TenantID, id, in.HospitalID, in.Subject, in.TemplateKey, in.Reason, in.ValidFrom, in.ExpiresAt, p.Subject, requestID).Error; err != nil {
			return err
		}
		if err := tx.Where("tenant_id=? AND id=?", p.TenantID, id).Take(&saved).Error; err != nil {
			return err
		}
		data, err := json.Marshal(iamcontract.AssignmentScopeRequestedData{ProposalID: id, RequestID: requestID, HospitalID: in.HospitalID, ActorSubject: p.Subject})
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
func (p assignmentProposal) output() ProposalOutput {
	return ProposalOutput{ID: p.ID, Input: p.ProposalInput, ProposedBy: p.ProposedBy, Status: p.Status, Version: p.Version, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
}
func sameProposal(a, b ProposalInput) bool {
	sameTimes := a.ValidFrom.Equal(b.ValidFrom) && a.ExpiresAt.Equal(b.ExpiresAt)
	a.ValidFrom = b.ValidFrom
	a.ExpiresAt = b.ExpiresAt
	return sameTimes && a == b
}
func (h *assignmentProposalHandlers) get(c *gin.Context) {
	p, id, ok := proposalScope(c)
	if !ok {
		return
	}
	var row assignmentProposal
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := assignmentAccessFor(tx, p); err != nil {
			return err
		}
		if err := tx.Where("tenant_id=? AND id=?", p.TenantID, id).Take(&row).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'read','iam_assignment_proposals',?)`, p.TenantID, p.Subject, id).Error
	})
	if err != nil {
		proposalError(c, err)
		return
	}
	respond.OK(c, row.output())
}
func (h *assignmentProposalHandlers) history(c *gin.Context, params pagination.Params) ([]assignmentProposalHistory, error) {
	p, id, ok := proposalScope(c)
	if !ok {
		return nil, nil
	}
	var rows []assignmentProposalHistory
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := assignmentAccessFor(tx, p); err != nil {
			return err
		}
		var exists assignmentProposal
		if err := tx.Select("id").Where("tenant_id=? AND id=?", p.TenantID, id).Take(&exists).Error; err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx, params).Table("iam_assignment_proposal_history").Where("tenant_id=? AND id=?", p.TenantID, id).Find(&rows).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'history_read','iam_assignment_proposals',?)`, p.TenantID, p.Subject, id).Error
	})
	if err != nil {
		proposalError(c, err)
	}
	return rows, err
}

const assignmentScopeVerified = workforcecontract.SubjectAssignmentScopeVerified

func proposalScopeConsumer(deps platform.Deps) events.Consumer {
	return events.Consumer{Name: "iam-assignment-scope", Subject: assignmentScopeVerified, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
		var result workforcecontract.AssignmentScopeVerifiedData
		if evt.Version != 1 || evt.DestinationTenantID != "" {
			return errors.New("invalid assignment scope envelope")
		}
		if err := json.Unmarshal(evt.Data, &result); err != nil {
			return errors.New("invalid assignment scope result")
		}
		if err := assignmentAccess(tx); err != nil {
			return err
		}
		var row assignmentProposal
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND id=?", evt.TenantID, result.ProposalID).Take(&row).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if (row.Status != "pending_scope" && row.Status != "pending_decision_scope") || row.RequestID != result.RequestID || row.HospitalID != result.HospitalID {
			return nil
		}
		member, err := proposalMember(tx, evt.TenantID, row.Subject, row.MembershipEpoch)
		if err != nil {
			return err
		}
		status := "scope_rejected"
		if result.Eligible && member && row.ExpiresAt.After(time.Now()) {
			status = "pending_review"
		}
		if row.Status == "pending_decision_scope" {
			status, err = decisionScopeStatus(tx, evt.TenantID, row, result.Eligible)
			if err != nil {
				return err
			}
		}
		if err := tx.Exec(`SELECT set_config('app.assignment_scope_result',?,true)`, row.RequestID.String()).Error; err != nil {
			return err
		}
		if err := tx.Model(&assignmentProposal{}).Where("tenant_id=? AND id=?", evt.TenantID, row.ID).Updates(map[string]any{"status": status, "version": row.Version + 1}).Error; err != nil {
			return err
		}
		if status == "approved_pending_activation" {
			return queueAssignmentProjection(tx, deps.Bus, evt.TenantID, row.ID)
		}
		return nil
	}}
}
func (m *Module) registerAssignmentProposals(g *platform.Router, deps platform.Deps) {
	h := &assignmentProposalHandlers{db: deps.DB, bus: deps.Bus}
	g.POST("/assignment-proposals/:id/decision", authz.Public, h.decide)
	platform.ListRoute(g, "/assignment-proposals/:id/decisions", authz.Public, h.decisions)
	platform.ListRoute(g, "/assignment-proposals", authz.Public, h.list)
	g.PUT("/assignment-proposals/:id", PermAssignmentManage, h.put)
	g.GET("/assignment-proposals/:id", authz.Public, h.get)
	platform.ListRoute(g, "/assignment-proposals/:id/history", authz.Public, h.history)
}

func (h *assignmentProposalHandlers) list(c *gin.Context, params pagination.Params) ([]assignmentProposal, error) {
	p, _, ok := proposalScope(c)
	if !ok {
		return nil, nil
	}
	var rows []assignmentProposal
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := assignmentAccessFor(tx, p); err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx, params).Where("tenant_id=?", p.TenantID).Find(&rows).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'list','iam_assignment_proposals',?)`, p.TenantID, p.Subject, p.TenantID).Error
	})
	if err != nil {
		proposalError(c, err)
	}
	return rows, err
}
func (p assignmentProposal) MarshalJSON() ([]byte, error) { return json.Marshal(p.output()) }

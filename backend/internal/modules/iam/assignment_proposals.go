package iam

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"slices"
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

//go:embed assignment_templates_generated.sql
var assignmentTemplatesSchema string

//go:embed assignment_hr_intake_templates_generated.sql
var hrIntakeTemplatesSchema string

//go:embed assignment_employment_templates_generated.sql
var employmentTemplatesSchema string

//go:embed assignment_employment_activation_templates_generated.sql
var employmentActivationTemplatesSchema string

//go:embed assignment_staff_posting_templates_generated.sql
var staffPostingTemplatesSchema string

//go:embed assignment_staff_posting_review_templates_generated.sql
var staffPostingReviewTemplatesSchema string

//go:embed assignment_posting_management_templates_generated.sql
var staffPostingManagementTemplatesSchema string

//go:embed assignment_posting_cutoff_templates_generated.sql
var staffPostingCutoffTemplatesSchema string

//go:embed assignment_posting_source_templates_generated.sql
var staffPostingSourceTemplatesSchema string

//go:embed assignment_pharmacy_department_binding_templates_generated.sql
var pharmacyDepartmentBindingTemplatesSchema string

//go:embed assignment_department_posting_review_templates_generated.sql
var departmentPostingReviewTemplatesSchema string

//go:embed assignment_department_scope.sql
var departmentAssignmentScopeSchema string

//go:embed assignment_department_leave_review_templates_generated.sql
var departmentLeaveReviewTemplatesSchema string

//go:embed assignment_establishment_root.sql
var establishmentRootSchema string

type assignmentProposal struct {
	ProposalInput   `gorm:"embedded"`
	ID              uuid.UUID `json:"id"`
	ScopeKind       string    `json:"scope_kind"`
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

// assignmentDepartmentAccess reveals department rows; only department-aware paths may call it (ADR-0055).
func assignmentDepartmentAccess(tx *gorm.DB) error {
	return tx.Exec(`SELECT set_config('app.assignment_department_scope','enabled',true)`).Error
}

func departmentAwareAccess(tx *gorm.DB) error {
	if err := assignmentAccess(tx); err != nil {
		return err
	}
	return assignmentDepartmentAccess(tx)
}

// proposalScopeKind binds the template to the chosen scope; an unknown template allows none.
func proposalScopeKind(in ProposalInput) (string, bool) {
	kind := "hospital"
	if in.DepartmentID != nil {
		if in.DepartmentID.Version() != 7 || in.DepartmentID.Variant() != uuid.RFC4122 || *in.DepartmentID == in.HospitalID {
			return "", false
		}
		kind = "department"
	}
	for _, template := range staffAccessTemplates() {
		if template.Key == in.TemplateKey {
			return kind, slices.Contains(template.ScopeKinds, kind)
		}
	}
	return "", false
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
	if values := c.Request.Header.Values("X-Helivanta-Expected-Subject"); len(values) > 1 || (len(values) == 1 && values[0] != p.Subject) {
		respond.Error(c, 409, "session_changed", "Your signed-in account changed. Reload before continuing.")
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
	scopeKind, scopeAllowed := proposalScopeKind(in)
	if in.HospitalID == uuid.Nil || !scopeAllowed || !proposalSubject(in.Subject) || !proposalSubject(p.Subject) || staffAccessTemplatePermissions(in.TemplateKey) == nil || !utf8.ValidString(in.Reason) || strings.TrimSpace(in.Reason) == "" || utf8.RuneCountInString(in.Reason) > 500 || strings.ContainsRune(in.Reason, 0) || in.ValidFrom.IsZero() || !in.ExpiresAt.After(in.ValidFrom) || in.ExpiresAt.Sub(in.ValidFrom) > 366*24*time.Hour || !in.ExpiresAt.After(time.Now()) {
		respond.Error(c, 400, "invalid_request", "Provide a member, hospital, supported template for the chosen scope, reason and bounded validity.")
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
		if err := tx.Exec(`SELECT pg_advisory_xact_lock_shared(hashtextextended(?,0))`, "iam.employment:"+p.TenantID+":"+in.HospitalID.String()).Error; err != nil {
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
		if err := tx.Exec(`INSERT INTO iam_assignment_proposals(tenant_id,id,hospital_id,subject,template_key,reason,valid_from,expires_at,proposed_by,request_id,status,version,scope_kind,department_id) VALUES(?,?,?,?,?,?,?,?,?,?,'pending_scope',1,?,?)`, p.TenantID, id, in.HospitalID, in.Subject, in.TemplateKey, in.Reason, in.ValidFrom, in.ExpiresAt, p.Subject, requestID, scopeKind, in.DepartmentID).Error; err != nil {
			return err
		}
		if err := tx.Where("tenant_id=? AND id=?", p.TenantID, id).Take(&saved).Error; err != nil {
			return err
		}
		return requestProposalScope(tx, h.bus, p.TenantID, saved, p.Subject)
	})
	if err != nil {
		proposalError(c, err)
		return
	}
	respond.OK(c, saved.output())
}

// requestProposalScope asks every scope owner about the current request in the caller's transaction.
func requestProposalScope(tx *gorm.DB, bus *events.Bus, tenant string, row assignmentProposal, actor string) error {
	requests := []struct {
		subject string
		data    any
	}{{iamcontract.SubjectAssignmentScopeRequested, iamcontract.AssignmentScopeRequestedData{ProposalID: row.ID, RequestID: row.RequestID, HospitalID: row.HospitalID, ActorSubject: actor}}}
	if row.ScopeKind == "department" {
		if row.DepartmentID == nil {
			return errors.New("department proposal without department")
		}
		requests = append(requests, struct {
			subject string
			data    any
		}{iamcontract.SubjectAssignmentDepartmentScopeRequested, iamcontract.AssignmentDepartmentScopeRequestedData{ProposalID: row.ID, RequestID: row.RequestID, HospitalID: row.HospitalID, DepartmentID: *row.DepartmentID}})
	}
	for _, request := range requests {
		data, err := json.Marshal(request.data)
		if err != nil {
			return err
		}
		eventID, err := uuid.NewV7()
		if err != nil {
			return err
		}
		if err := bus.Publish(tx, request.subject, events.Event{ID: eventID.String(), Type: request.subject, Version: 1, TenantID: tenant, OccurredAt: time.Now(), Data: data}); err != nil {
			return err
		}
	}
	return nil
}
func (p assignmentProposal) output() ProposalOutput {
	return ProposalOutput{ID: p.ID, Input: p.ProposalInput, ScopeKind: p.ScopeKind, ProposedBy: p.ProposedBy, Status: p.Status, Version: p.Version, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
}
func sameProposal(a, b ProposalInput) bool {
	sameTimes := a.ValidFrom.Equal(b.ValidFrom) && a.ExpiresAt.Equal(b.ExpiresAt)
	sameDepartment := (a.DepartmentID == nil) == (b.DepartmentID == nil) && (a.DepartmentID == nil || *a.DepartmentID == *b.DepartmentID)
	a.ValidFrom, a.ExpiresAt, a.DepartmentID = b.ValidFrom, b.ExpiresAt, b.DepartmentID
	return sameTimes && sameDepartment && a == b
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
		row, current, err := lockPendingProposal(tx, evt.TenantID, result.ProposalID, result.RequestID, result.HospitalID)
		if err != nil || !current {
			return err
		}
		if row.ScopeKind == "department" {
			return recordDepartmentAnswer(tx, deps, evt.TenantID, row, "workforce", result.Eligible)
		}
		return settleProposalScope(tx, deps, evt.TenantID, row, result.Eligible)
	}}
}

// lockPendingProposal reports current only for a pending proposal still waiting on this request.
func lockPendingProposal(tx *gorm.DB, tenant string, id, request, hospital uuid.UUID) (assignmentProposal, bool, error) {
	var row assignmentProposal
	if err := departmentAwareAccess(tx); err != nil {
		return row, false, err
	}
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND id=?", tenant, id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return row, false, nil
	}
	if err != nil {
		return row, false, err
	}
	return row, (row.Status == "pending_scope" || row.Status == "pending_decision_scope") && row.RequestID == request && row.HospitalID == hospital, nil
}

// settleProposalScope applies the combined owner answer for the current request.
func settleProposalScope(tx *gorm.DB, deps platform.Deps, tenant string, row assignmentProposal, eligible bool) error {
	member, err := proposalMember(tx, tenant, row.Subject, row.MembershipEpoch)
	if err != nil {
		return err
	}
	status := "scope_rejected"
	if eligible && member && row.ExpiresAt.After(time.Now()) {
		status = "pending_review"
	}
	if row.Status == "pending_decision_scope" {
		status, err = decisionScopeStatus(tx, tenant, row, eligible)
		if err != nil {
			return err
		}
	}
	if err := tx.Exec(`SELECT set_config('app.assignment_scope_result',?,true)`, row.RequestID.String()).Error; err != nil {
		return err
	}
	if err := tx.Model(&assignmentProposal{}).Where("tenant_id=? AND id=?", tenant, row.ID).Updates(map[string]any{"status": status, "version": row.Version + 1}).Error; err != nil {
		return err
	}
	if status == "approved_pending_activation" {
		return queueAssignmentProjection(tx, deps.Bus, tenant, row.ID)
	}
	return nil
}
func (m *Module) registerAssignmentProposals(g *platform.Router, deps platform.Deps) {
	h := &assignmentProposalHandlers{db: deps.DB, bus: deps.Bus}
	g.POST("/assignment-proposals/:id/decision", authz.Public, h.decide)
	platform.ListRoute(g, "/assignment-proposals/:id/decisions", authz.Public, h.decisions)
	platform.ListRoute(g, "/assignment-proposals", authz.Public, h.list)
	platform.ListRoute(g, "/identity-assignment-proposals", authz.Public, h.listIdentity)
	platform.ListRoute(g, "/intake-assignment-proposals", authz.Public, h.listIntake)
	platform.ListRoute(g, "/employment-assignment-proposals", authz.Public, h.listEmployment)
	platform.ListRoute(g, "/employment-activation-assignment-proposals", authz.Public, h.listEmploymentActivation)
	platform.ListRoute(g, "/posting-assignment-proposals", authz.Public, h.listPosting)
	platform.ListRoute(g, "/pharmacy-binding-assignment-proposals", authz.Public, h.listPharmacyBinding)
	platform.ListRoute(g, "/partner-membership-assignment-proposals", authz.Public, func(c *gin.Context, p pagination.Params) ([]assignmentProposal, error) {
		return h.listFamily(c, p, "partner_membership")
	})
	g.GET("/staff-access-templates", authz.Public, func(c *gin.Context) {
		if _, _, ok := proposalScope(c); !ok {
			return
		}
		query, err := url.ParseQuery(c.Request.URL.RawQuery)
		if err != nil {
			respond.Error(c, 400, "invalid_request", "Invalid query.")
			return
		}
		key := ""
		if values, present := query["catalog"]; present {
			if len(values) != 1 || values[0] == "" {
				respond.Error(c, 400, "invalid_request", "Choose one supported Staff access catalog.")
				return
			}
			key = values[0]
		}
		templates, supported := staffAccessTemplateCatalog(key)
		if !supported {
			respond.Error(c, 400, "invalid_request", "This Staff access catalog is not supported.")
			return
		}
		respond.OK(c, templates)
	})
	g.PUT("/assignment-proposals/:id", PermAssignmentManage, h.put)
	g.GET("/assignment-proposals/:id", authz.Public, h.get)
	platform.ListRoute(g, "/assignment-proposals/:id/history", authz.Public, h.history)
}

func (h *assignmentProposalHandlers) list(c *gin.Context, params pagination.Params) ([]assignmentProposal, error) {
	return h.listFamily(c, params, "leave")
}

func (h *assignmentProposalHandlers) listIdentity(c *gin.Context, params pagination.Params) ([]assignmentProposal, error) {
	return h.listFamily(c, params, "identity")
}

func (h *assignmentProposalHandlers) listIntake(c *gin.Context, params pagination.Params) ([]assignmentProposal, error) {
	return h.listFamily(c, params, "intake")
}

func (h *assignmentProposalHandlers) listEmployment(c *gin.Context, params pagination.Params) ([]assignmentProposal, error) {
	return h.listFamily(c, params, "employment")
}

func (h *assignmentProposalHandlers) listEmploymentActivation(c *gin.Context, params pagination.Params) ([]assignmentProposal, error) {
	return h.listFamily(c, params, "activation")
}

func (h *assignmentProposalHandlers) listPosting(c *gin.Context, params pagination.Params) ([]assignmentProposal, error) {
	return h.listFamily(c, params, "posting")
}

func (h *assignmentProposalHandlers) listPharmacyBinding(c *gin.Context, params pagination.Params) ([]assignmentProposal, error) {
	return h.listFamily(c, params, "pharmacy_binding")
}

func (h *assignmentProposalHandlers) listFamily(c *gin.Context, params pagination.Params, family string) ([]assignmentProposal, error) {
	p, _, ok := proposalScope(c)
	if !ok {
		return nil, nil
	}
	var rows []assignmentProposal
	var keys []string
	for _, template := range staffAccessTemplates() {
		if template.Family == family {
			keys = append(keys, template.Key)
		}
	}
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := assignmentAccessFor(tx, p); err != nil {
			return err
		}
		if err := tenantdb.ApplyKeyset(tx, params).Where("tenant_id=? AND template_key IN ?", p.TenantID, keys).Find(&rows).Error; err != nil {
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

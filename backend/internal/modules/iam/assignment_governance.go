package iam

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

//go:embed assignment_governance.sql
var assignmentGovernanceSchema string

const (
	PermGovernanceAssignmentManage authz.Permission = "iam.governance.assignment.manage"
	PermGovernanceAssignmentReview authz.Permission = "iam.governance.assignment.review"
)

type assignmentGovernanceProof struct {
	TenantID          string                     `json:"tenant_id"`
	HospitalID        uuid.UUID                  `json:"hospital_id"`
	ActorSubject      string                     `json:"actor_subject"`
	Capability        authz.GovernanceCapability `json:"capability"`
	Kind              authz.GovernanceSourceKind `json:"authority_kind"`
	OwnerMembershipID *uuid.UUID                 `json:"owner_membership_id,omitempty"`
	GrantID           *uuid.UUID                 `json:"source_grant_id,omitempty"`
	GrantVersion      *int64                     `json:"source_grant_version,omitempty"`
	MembershipEpoch   int64                      `json:"membership_epoch"`
	AuthenticatedAt   time.Time                  `json:"authenticated_at"`
	IdleDeadline      time.Time                  `json:"idle_deadline"`
	ExpiresAt         time.Time                  `json:"expires_at"`
}

func assignmentProof(p authn.Principal, hospital uuid.UUID, capability authz.GovernanceCapability, source authz.GovernanceSourceEvidence) (assignmentGovernanceProof, error) {
	proof := assignmentGovernanceProof{TenantID: p.TenantID, HospitalID: hospital, ActorSubject: p.Subject, Capability: capability, Kind: source.Kind, MembershipEpoch: source.MembershipEpoch, AuthenticatedAt: p.AuthTime, IdleDeadline: p.IdleDeadline, ExpiresAt: source.ExpiresAt}
	if (capability != authz.GovernanceAssignmentManage && capability != authz.GovernanceAssignmentReview) || p.AuthTime.IsZero() || !p.IdleDeadline.After(time.Now()) || !source.ExpiresAt.After(time.Now()) || source.Scope.TenantID != p.TenantID || source.Scope.Kind != "hospital" || source.Scope.ID == nil || *source.Scope.ID != hospital || source.MembershipEpoch < 1 {
		return proof, authz.ErrScopedAuthorizationUnavailable
	}
	switch source.Kind {
	case authz.GovernanceOwnerSource:
		if source.OwnerMembershipID == uuid.Nil || source.GrantID != uuid.Nil || source.Version != 0 || source.TemplateKey != "" {
			return proof, authz.ErrScopedAuthorizationUnavailable
		}
		id := source.OwnerMembershipID
		proof.OwnerMembershipID = &id
	case authz.GovernanceGrantSource:
		if source.OwnerMembershipID != uuid.Nil || !validV7(source.GrantID) || source.Version < 1 || source.Version > 9007199254740991 || source.TemplateKey != "iam.scoped_access_reviewer.v1" {
			return proof, authz.ErrScopedAuthorizationUnavailable
		}
		id, version := source.GrantID, source.Version
		proof.GrantID, proof.GrantVersion = &id, &version
	default:
		return proof, authz.ErrScopedAuthorizationUnavailable
	}
	return proof, nil
}

func sameAssignmentSource(a, b assignmentGovernanceProof) bool {
	if a.TenantID != b.TenantID || a.HospitalID != b.HospitalID || a.ActorSubject != b.ActorSubject || a.Capability != b.Capability || a.Kind != b.Kind || a.MembershipEpoch != b.MembershipEpoch {
		return false
	}
	switch a.Kind {
	case authz.GovernanceOwnerSource:
		return a.OwnerMembershipID != nil && b.OwnerMembershipID != nil && *a.OwnerMembershipID == *b.OwnerMembershipID
	case authz.GovernanceGrantSource:
		return a.GrantID != nil && b.GrantID != nil && a.GrantVersion != nil && b.GrantVersion != nil && *a.GrantID == *b.GrantID && *a.GrantVersion == *b.GrantVersion
	}
	return false
}

func assignmentGovernanceSupport(tx *gorm.DB) error {
	var installed bool
	if err := tx.Raw(`SELECT to_regclass('public.iam_assignment_governance_requests') IS NOT NULL AND to_regclass('public.iam_assignment_governance_verifications') IS NOT NULL AND to_regprocedure('public.iam_assignment_governance_support()') IS NOT NULL`).Scan(&installed).Error; err != nil {
		return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
	}
	if !installed {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if err := tx.Raw(`SELECT iam_assignment_governance_support()`).Scan(&installed).Error; err != nil {
		return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
	}
	if !installed {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return nil
}

func assignmentGovernanceContext(tx *gorm.DB, hospital uuid.UUID, proposal string, proof *assignmentGovernanceProof) error {
	if err := assignmentGovernanceSupport(tx); err != nil {
		return err
	}
	raw := ""
	if proof != nil {
		encoded, err := json.Marshal(proof)
		if err != nil {
			return err
		}
		raw = string(encoded)
	}
	return tx.Exec(`SELECT set_config('app.iam_assignment_native_hospital',?,true),set_config('app.iam_assignment_native_proposal',?,true),set_config('app.iam_assignment_native_authority',?,true)`, hospital.String(), proposal, raw).Error
}

func (h *assignmentProposalHandlers) assignmentTransaction(c *gin.Context, p authn.Principal, capability authz.GovernanceCapability, action func(*gorm.DB) error) error {
	if !h.native {
		return h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			if err := assignmentAccessFor(tx, p); err != nil {
				return err
			}
			return action(tx)
		})
	}
	if h.sources == nil || h.db == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	for _, header := range []string{"X-Helivanta-Expected-Tenant", "X-Helivanta-Expected-Subject"} {
		if len(c.Request.Header.Values(header)) != 1 {
			respond.Error(c, 409, "assignment_session_context_required", "Reload the current workspace before managing assignments.")
			return errProposalConflict
		}
	}
	hospital, err := uuid.Parse(c.Param("hospital"))
	if err != nil || hospital == uuid.Nil || hospital.String() != c.Param("hospital") {
		return gorm.ErrRecordNotFound
	}
	if !authn.Refresh(c) {
		return authz.ErrScopedAuthorizationUnavailable
	}
	current, ok := authn.PrincipalFrom(c)
	if !ok || current.TenantID != p.TenantID || current.Subject != p.Subject {
		return authz.ErrScopedCommandDenied
	}
	req := authz.GovernanceSourceRequest{Principal: current, Capability: capability, Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: "hospital", ID: &hospital}}
	err = h.sources.WithGovernanceSource(c.Request.Context(), req, func(commandCtx context.Context, source authz.GovernanceSourceEvidence) error {
		proof, err := assignmentProof(current, hospital, capability, source)
		if err != nil {
			return err
		}
		return h.db.WithTenantCommand(commandCtx, p.TenantID, func(tx *gorm.DB) error {
			if err := departmentAwareAccess(tx); err != nil {
				return err
			}
			if err := assignmentGovernanceContext(tx, hospital, c.Param("id"), &proof); err != nil {
				return err
			}
			if err := action(tx); err != nil {
				return err
			}
			if c.Request.Method == "GET" {
				resource, kind := hospital, "workforce_hospitals"
				if c.Param("id") != "" {
					resource, err = uuid.Parse(c.Param("id"))
					if err != nil {
						return err
					}
					kind = "iam_assignment_proposals"
				}
				return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'iam.assignment.governance.read',?,?)`, p.TenantID, p.Subject, kind, resource).Error
			}
			return nil
		})
	})
	if c.Request.Method == "GET" && errors.Is(err, authz.ErrScopedCommandDenied) {
		return gorm.ErrRecordNotFound
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) && !errors.Is(err, errProposalConflict) && !errors.Is(err, authz.ErrScopedCommandDenied) && !errors.Is(err, authz.ErrScopedCredentialRevoked) && !errors.Is(err, authz.ErrGovernanceScopeHidden) {
		return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
	}
	return err
}

func currentAssignmentGovernanceProof(tx *gorm.DB) (*assignmentGovernanceProof, error) {
	var raw string
	if err := tx.Raw(`SELECT COALESCE(current_setting('app.iam_assignment_native_authority',true),'')`).Scan(&raw).Error; err != nil || raw == "" {
		return nil, err
	}
	var proof assignmentGovernanceProof
	if err := json.Unmarshal([]byte(raw), &proof); err != nil {
		return nil, err
	}
	return &proof, nil
}

func retainAssignmentGovernanceRequest(tx *gorm.DB, tenant string, row assignmentProposal, actor string) (bool, error) {
	proof, err := currentAssignmentGovernanceProof(tx)
	if err != nil || proof == nil {
		return false, err
	}
	if proof.TenantID != tenant || proof.HospitalID != row.HospitalID || proof.ActorSubject != actor {
		return false, gorm.ErrRecordNotFound
	}
	raw, err := json.Marshal(proof)
	if err != nil {
		return false, err
	}
	kind := "proposal"
	if row.Version > 1 {
		kind = "decision"
	}
	return true, tx.Exec(`INSERT INTO iam_assignment_governance_requests(tenant_id,request_id,proposal_id,hospital_id,proposal_version,kind,actor_subject,authority) VALUES(?,?,?,?,?,?,?,?::jsonb)`, tenant, row.RequestID, row.ID, row.HospitalID, row.Version, kind, actor, string(raw)).Error
}

func assignmentGovernanceReplay(tx *gorm.DB, tenant string, row assignmentProposal, request *uuid.UUID) error {
	current, err := currentAssignmentGovernanceProof(tx)
	if err != nil || current == nil {
		return err
	}
	var stored struct{ Authority string }
	query := tx.Table("iam_assignment_governance_requests").Select("authority::text AS authority").Where("tenant_id=? AND proposal_id=?", tenant, row.ID)
	if request == nil {
		query = query.Where("kind='proposal'")
	} else {
		query = query.Where("request_id=?", *request)
	}
	if err := query.Take(&stored).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return errProposalConflict
		}
		return err
	}
	var original assignmentGovernanceProof
	if json.Unmarshal([]byte(stored.Authority), &original) != nil || !sameAssignmentSource(original, *current) {
		return errProposalConflict
	}
	return nil
}

func registerGovernanceAssignments(g *platform.Router, deps platform.Deps) {
	h := &assignmentProposalHandlers{db: deps.DB, bus: deps.Bus, native: true, sources: deps.GovernanceSources}
	root := "/hospitals/:hospital/assignment-proposals"
	g.GET("/hospitals/:hospital/assignment-templates", authz.Public, h.templates)
	g.PUT(root+"/:id", PermGovernanceAssignmentManage, h.put)
	g.GET(root+"/:id", authz.Public, h.get)
	g.POST(root+"/:id/decision", authz.Public, h.decide)
	platform.ResourceListRoute(g, root, authz.Public, 50, h.list)
	platform.ResourceListRoute(g, root+"/:id/history", authz.Public, 50, h.history)
	platform.ResourceListRoute(g, root+"/:id/decisions", authz.Public, 50, h.decisions)
}

func (h *assignmentProposalHandlers) proposalPrincipal(c *gin.Context) (authn.Principal, uuid.UUID, bool) {
	if h.native {
		return proposalScopeWithPermissions(c, PermGovernanceAssignmentManage, PermGovernanceAssignmentReview)
	}
	return proposalScope(c)
}

func (h *assignmentProposalHandlers) templates(c *gin.Context) {
	p, _, ok := h.proposalPrincipal(c)
	if !ok {
		return
	}
	if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		respond.Error(c, 400, "invalid_request", "The canonical assignment catalog accepts no query parameters.")
		return
	}
	var templates []TemplateDefinition
	err := h.assignmentTransaction(c, p, authz.GovernanceAssignmentManage, func(_ *gorm.DB) error {
		templates = staffAccessTemplates()
		return nil
	})
	if err != nil {
		proposalError(c, err)
		return
	}
	respond.OK(c, templates)
}

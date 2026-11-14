package authz

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
)

// GovernedAssignmentSelectionRequest carries a held IAM source to a private hospital owner read.
type GovernedAssignmentSelectionRequest struct {
	Principal  authn.Principal
	HospitalID uuid.UUID
	Capability GovernanceCapability
	Source     GovernanceSourceEvidence
}

// AssignmentTargetChoiceProof is private read evidence; callers must hold the verified IAM source.
func AssignmentTargetChoiceProof(request GovernedAssignmentSelectionRequest, now time.Time) (map[string]any, error) {
	return assignmentSelectionProof(request, "assignment_target_choices", now)
}

// AssignmentDepartmentChoiceProof separates Organization selection from Staff target disclosure.
func AssignmentDepartmentChoiceProof(request GovernedAssignmentSelectionRequest, now time.Time) (map[string]any, error) {
	return assignmentSelectionProof(request, "assignment_department_choices", now)
}

func assignmentSelectionProof(request GovernedAssignmentSelectionRequest, purpose string, now time.Time) (map[string]any, error) {
	p, source := request.Principal, request.Source
	tenant, err := uuid.Parse(p.TenantID)
	if err != nil || tenant == uuid.Nil || tenant.String() != p.TenantID || request.HospitalID == uuid.Nil || request.Capability != GovernanceAssignmentManage || source.Scope.TenantID != p.TenantID || source.Scope.Kind != "hospital" || source.Scope.ID == nil || *source.Scope.ID != request.HospitalID {
		return nil, ErrScopedAuthorizationUnavailable
	}
	if p.AuthTime.IsZero() || p.AuthTime.After(now) || !p.IdleDeadline.After(now) || !source.ExpiresAt.After(now) || source.MembershipEpoch < 1 || source.MembershipEpoch > 9007199254740991 || source.OwnerCount < 1 || source.OwnerCount > 1000 {
		return nil, ErrScopedAuthorizationUnavailable
	}
	if !utf8.ValidString(p.Subject) || utf8.RuneCountInString(p.Subject) < 1 || utf8.RuneCountInString(p.Subject) > 200 || strings.ContainsAny(p.Subject, "*#:") || strings.IndexFunc(p.Subject, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return nil, ErrScopedAuthorizationUnavailable
	}
	proof := map[string]any{"purpose": purpose, "tenant_id": p.TenantID, "hospital_id": request.HospitalID, "actor_subject": p.Subject, "capability": request.Capability, "authority_kind": source.Kind, "membership_epoch": source.MembershipEpoch, "owner_count": source.OwnerCount, "authenticated_at": p.AuthTime, "idle_deadline": p.IdleDeadline, "source_expires_at": source.ExpiresAt}
	switch source.Kind {
	case GovernanceOwnerSource:
		if source.OwnerMembershipID == uuid.Nil || source.OwnerMembershipID.Variant() != uuid.RFC4122 || source.GrantID != uuid.Nil || source.Version != 0 || source.TemplateKey != "" {
			return nil, ErrScopedAuthorizationUnavailable
		}
		proof["owner_membership_id"] = source.OwnerMembershipID
	case GovernanceGrantSource:
		if source.OwnerMembershipID != uuid.Nil || source.GrantID.Version() != 7 || source.GrantID.Variant() != uuid.RFC4122 || source.Version < 1 || source.Version > 9007199254740991 || source.TemplateKey != "iam.scoped_access_reviewer.v1" {
			return nil, ErrScopedAuthorizationUnavailable
		}
		proof["source_grant_id"], proof["source_grant_version"], proof["template_key"] = source.GrantID, source.Version, source.TemplateKey
	default:
		return nil, ErrScopedAuthorizationUnavailable
	}
	return proof, nil
}

package authz

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

// OperatingLocationChoice is owner-supplied structural selection metadata, not operating authority.
type OperatingLocationChoice struct {
	Reference  OperatingLocationReference
	Code, Name string
	CreatedAt  time.Time
}

// OperatingLocationChoiceProof binds private read evidence to the current native employer source.
func OperatingLocationChoiceProof(request GovernedIndependentEmployerRequest, now time.Time) (map[string]any, error) {
	p, scope, source := request.Principal, request.Scope, request.Source
	tenant, err := uuid.Parse(p.TenantID)
	if err != nil || tenant == uuid.Nil || tenant.String() != p.TenantID || scope.TenantID != p.TenantID || scope.EmployerID.Version() != 7 || scope.EmployerID.Variant() != uuid.RFC4122 || (scope.EmployerKind != "pharmacy" && scope.EmployerKind != "supplier") || request.Capability != GovernanceOperatingManage || source.Scope.TenantID != p.TenantID || source.Scope.Kind != scope.EmployerKind || source.Scope.ID == nil || *source.Scope.ID != scope.EmployerID || source.MembershipEpoch < 1 || source.MembershipEpoch > 9007199254740991 || source.OwnerCount < 1 || source.OwnerCount > 1000 || p.AuthTime.IsZero() || p.AuthTime.After(now) || !p.IdleDeadline.After(now) || !source.ExpiresAt.After(now) || !utf8.ValidString(p.Subject) || utf8.RuneCountInString(p.Subject) < 1 || utf8.RuneCountInString(p.Subject) > 200 || (strings.ContainsAny(p.Subject, "*#:") || strings.IndexFunc(p.Subject, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0) {
		return nil, ErrScopedAuthorizationUnavailable
	}
	proof := map[string]any{"purpose": "operating_location_choices", "tenant_id": p.TenantID, "employer_kind": scope.EmployerKind, "employer_id": scope.EmployerID, "actor_subject": p.Subject, "capability": request.Capability, "authority_kind": source.Kind, "membership_epoch": source.MembershipEpoch, "owner_count": source.OwnerCount, "authenticated_at": p.AuthTime, "idle_deadline": p.IdleDeadline, "source_expires_at": source.ExpiresAt}
	switch source.Kind {
	case GovernanceOwnerSource:
		if source.OwnerMembershipID == uuid.Nil || source.OwnerMembershipID.Variant() != uuid.RFC4122 || source.GrantID != uuid.Nil || source.Version != 0 || source.TemplateKey != "" {
			return nil, ErrScopedAuthorizationUnavailable
		}
		proof["owner_membership_id"] = source.OwnerMembershipID
	case GovernanceGrantSource:
		if source.OwnerMembershipID != uuid.Nil || source.GrantID.Version() != 7 || source.GrantID.Variant() != uuid.RFC4122 || source.Version < 1 || source.Version > 9007199254740991 || source.TemplateKey != "iam.operating_access_reviewer.v1" {
			return nil, ErrScopedAuthorizationUnavailable
		}
		proof["source_grant_id"], proof["source_grant_version"], proof["template_key"] = source.GrantID, source.Version, source.TemplateKey
	default:
		return nil, ErrScopedAuthorizationUnavailable
	}
	return proof, nil
}

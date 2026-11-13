package workforce

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestLoginReviewSourceBindsCanonicalEmployerAndProvenance(t *testing.T) {
	root := establishment{kind: hospitalEstablishmentKind, id: uuid.Must(uuid.NewV7())}
	p := authn.Principal{TenantID: uuid.NewString(), Subject: "synthetic-reviewer"}
	base := authz.GovernanceSourceEvidence{Kind: authz.GovernanceGrantSource, GrantID: uuid.Must(uuid.NewV7()), Version: 2, MembershipEpoch: 1, OwnerCount: 2, TemplateKey: "workforce.account_link_reviewer.v1", Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: root.kind, ID: &root.id}, ExpiresAt: time.Now().UTC().Add(time.Hour)}
	require.True(t, validLoginReviewSource(p, root, base))
	owner := base
	owner.Kind = authz.GovernanceOwnerSource
	owner.GrantID = uuid.Nil
	owner.Version = 0
	owner.TemplateKey = ""
	owner.OwnerMembershipID = uuid.New()
	require.True(t, validLoginReviewSource(p, root, owner))
	for _, test := range []struct {
		name   string
		change func(*authz.GovernanceSourceEvidence)
	}{
		{"foreign tenant", func(v *authz.GovernanceSourceEvidence) { v.Scope.TenantID = uuid.NewString() }},
		{"sibling employer", func(v *authz.GovernanceSourceEvidence) { id := uuid.Must(uuid.NewV7()); v.Scope.ID = &id }},
		{"another employer kind", func(v *authz.GovernanceSourceEvidence) { v.Scope.Kind = "pharmacy" }},
		{"missing employer", func(v *authz.GovernanceSourceEvidence) { v.Scope.ID = nil }},
		{"unknown authority", func(v *authz.GovernanceSourceEvidence) { v.Kind = "role_label" }},
		{"missing membership", func(v *authz.GovernanceSourceEvidence) { v.MembershipEpoch = 0 }},
		{"unsafe membership epoch", func(v *authz.GovernanceSourceEvidence) { v.MembershipEpoch = 9007199254740992 }},
		{"missing owner census", func(v *authz.GovernanceSourceEvidence) { v.OwnerCount = 0 }},
		{"unbounded owner census", func(v *authz.GovernanceSourceEvidence) { v.OwnerCount = 1001 }},
		{"missing expiry", func(v *authz.GovernanceSourceEvidence) { v.ExpiresAt = time.Time{} }},
		{"missing grant", func(v *authz.GovernanceSourceEvidence) { v.GrantID = uuid.Nil }},
		{"wrong grant id version", func(v *authz.GovernanceSourceEvidence) { v.GrantID = uuid.New() }},
		{"invalid grant id variant", func(v *authz.GovernanceSourceEvidence) { v.GrantID[8] &= 0x3f }},
		{"missing grant version", func(v *authz.GovernanceSourceEvidence) { v.Version = 0 }},
		{"unsafe grant version", func(v *authz.GovernanceSourceEvidence) { v.Version = 9007199254740992 }},
		{"different capability template", func(v *authz.GovernanceSourceEvidence) { v.TemplateKey = "iam.governance_auditor.v1" }},
		{"mixed grant and owner", func(v *authz.GovernanceSourceEvidence) { v.OwnerMembershipID = uuid.New() }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := base
			test.change(&changed)
			require.False(t, validLoginReviewSource(p, root, changed))
		})
	}
	for _, test := range []struct {
		name   string
		change func(*authz.GovernanceSourceEvidence)
	}{
		{"missing owner membership", func(v *authz.GovernanceSourceEvidence) { v.OwnerMembershipID = uuid.Nil }},
		{"invalid owner membership variant", func(v *authz.GovernanceSourceEvidence) { v.OwnerMembershipID[8] &= 0x3f }},
		{"invented owner grant", func(v *authz.GovernanceSourceEvidence) { v.GrantID = uuid.Must(uuid.NewV7()) }},
		{"invented owner grant version", func(v *authz.GovernanceSourceEvidence) { v.Version = 1 }},
		{"invented owner template", func(v *authz.GovernanceSourceEvidence) { v.TemplateKey = "workforce.account_link_reviewer.v1" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := owner
			test.change(&changed)
			require.False(t, validLoginReviewSource(p, root, changed))
		})
	}
}
